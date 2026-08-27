package service

import (
	"context"
	"math"
	"net/url"
	"sort"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// SchedulerAccountCache is intentionally narrow: the outbox remains the
// source of truth for bucket rebuilds, while this hook makes the new account
// immediately visible to the per-account scheduler cache after commit.
type SchedulerAccountCache interface {
	UpdateAccountInCache(context.Context, *Account) error
}

// onboardingNameChecker is satisfied by both the account and the channel
// monitor repository. Neither table has a unique index on name (only
// groups_name_unique_active exists), so this duplicate-name rule lives at the
// workflow boundary and is best-effort against concurrent submissions.
type onboardingNameChecker interface {
	ExistsByName(context.Context, string) (bool, error)
}

// onboardingSettingReader reads the admin-configured API base URL so the
// monitor endpoint does not have to be derived from request headers.
type onboardingSettingReader interface {
	GetValue(ctx context.Context, key string) (string, error)
}

// onboardingAPIKeyCreator is the slice of APIKeyService this workflow needs.
// Narrowing it keeps the orchestration assertable without standing up the
// full API key service dependency graph.
type onboardingAPIKeyCreator interface {
	Create(ctx context.Context, userID int64, req CreateAPIKeyRequest) (*APIKey, error)
	InvalidateAuthCacheByKey(ctx context.Context, key string)
}

type ChannelOnboardingRequest struct {
	Name                string
	Platform            string
	RateMultiplier      float64
	UpstreamBaseURL     string
	UpstreamAPIKey      string
	PrimaryModel        string
	IntervalSeconds     *int
	ExpectedInputTokens *int
	// RequestOrigin is the scheme://host the admin reached this server on. It
	// is only a fallback: the configured api_base_url wins, because request
	// headers (X-Forwarded-Host) are attacker-influenceable and this value
	// becomes the address the monitor periodically calls with a live API key.
	RequestOrigin string
}

const (
	defaultChannelOnboardingIntervalSeconds = 900
	onboardingAccountConcurrency            = 10
	onboardingAccountPriority               = 1
)

type ChannelOnboardingResult struct {
	GroupID         int64   `json:"group_id"`
	AccountID       int64   `json:"account_id"`
	APIKeyID        int64   `json:"api_key_id"`
	MonitorID       int64   `json:"monitor_id"`
	APIKeyMasked    string  `json:"api_key_masked"`
	GroupName       string  `json:"group_name"`
	AccountName     string  `json:"account_name"`
	MonitorName     string  `json:"monitor_name"`
	Platform        string  `json:"platform"`
	RateMultiplier  float64 `json:"rate_multiplier"`
	IntervalSeconds int     `json:"interval_seconds"`
	PublicVisible   bool    `json:"public_visible"`
	ExpectedTokens  *int    `json:"expected_input_tokens,omitempty"`
}

// ChannelOnboardingService atomically creates the four resources required to
// bring a new upstream channel online.
type ChannelOnboardingService struct {
	entClient             *dbent.Client
	adminService          AdminService
	groupRepo             GroupRepository
	accountRepo           AdminAccountRepository
	apiKeyService         onboardingAPIKeyCreator
	monitorService        *ChannelMonitorService
	settingReader         onboardingSettingReader
	schedulerAccountCache SchedulerAccountCache
}

func NewChannelOnboardingService(
	entClient *dbent.Client,
	adminService AdminService,
	groupRepo GroupRepository,
	accountRepo AdminAccountRepository,
	apiKeyService *APIKeyService,
	monitorService *ChannelMonitorService,
	settingRepo SettingRepository,
	schedulerAccountCache SchedulerAccountCache,
) *ChannelOnboardingService {
	svc := &ChannelOnboardingService{
		entClient:             entClient,
		adminService:          adminService,
		groupRepo:             groupRepo,
		accountRepo:           accountRepo,
		monitorService:        monitorService,
		schedulerAccountCache: schedulerAccountCache,
	}
	// Assign through the nil checks so a typed-nil dependency does not become
	// a non-nil interface that only panics at request time.
	if apiKeyService != nil {
		svc.apiKeyService = apiKeyService
	}
	if settingRepo != nil {
		svc.settingReader = settingRepo
	}
	return svc
}

func (s *ChannelOnboardingService) Create(ctx context.Context, adminID int64, req ChannelOnboardingRequest) (*ChannelOnboardingResult, error) {
	if s == nil || s.entClient == nil || s.adminService == nil || s.groupRepo == nil ||
		s.accountRepo == nil || s.apiKeyService == nil || s.monitorService == nil {
		return nil, infraerrors.InternalServer("CHANNEL_ONBOARDING_NOT_CONFIGURED", "channel onboarding is not configured")
	}
	if err := validateChannelOnboardingRequest(req); err != nil {
		return nil, err
	}
	if adminID <= 0 {
		return nil, infraerrors.BadRequest("CHANNEL_ONBOARDING_ADMIN_REQUIRED", "admin user is required")
	}

	// validateEndpoint resolves the hostname (up to 5s) and fails closed, so it
	// must run before the transaction opens: a DNS stall would otherwise hold
	// an open transaction, and an unusable endpoint would only be discovered
	// after three inserts had already been written.
	endpoint := s.resolveMonitorEndpoint(ctx, req.RequestOrigin)
	if err := validateEndpoint(endpoint); err != nil {
		return nil, err
	}

	plan := newChannelOnboardingPlan(req, endpoint)
	// The account shape depends on nothing the transaction produces, so build
	// (and reject) it before opening one.
	account, err := plan.buildAccount()
	if err != nil {
		return nil, err
	}
	if err := s.ensureNamesAvailable(ctx, plan.Name); err != nil {
		return nil, err
	}

	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	txCtx := dbent.NewTxContext(ctx, tx)
	rollback := true
	defer func() {
		if rollback {
			_ = tx.Rollback()
		}
	}()

	// The group must be created first. groups.name carries the
	// groups_name_unique_active partial unique index, so a concurrent
	// same-name submission fails here and rolls the whole workflow back;
	// accounts and channel_monitors have no such index, which makes
	// ensureNamesAvailable alone insufficient. Do not reorder.
	group, err := s.adminService.CreateGroup(txCtx, plan.groupInput())
	if err != nil {
		return nil, err
	}

	if err := s.accountRepo.CreateWithAccountGroups(txCtx, account, []AccountGroup{{GroupID: group.ID, Priority: onboardingAccountPriority}}); err != nil {
		return nil, err
	}

	// Reusing APIKeyService.Create keeps key generation, name escaping, group
	// binding checks and auth-cache invalidation in one place. It resolves the
	// just-created group through GroupRepository.GetByIDLite, which reads the
	// transaction client from the context.
	apiKey, err := s.apiKeyService.Create(txCtx, adminID, CreateAPIKeyRequest{
		Name:    plan.Name,
		GroupID: &group.ID,
	})
	if err != nil {
		return nil, err
	}

	monitor, err := s.monitorService.Create(txCtx, plan.monitorParams(group.ID, adminID, apiKey.Key))
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	rollback = false

	// APIKeyService.Create already invalidated the auth cache inside the
	// transaction; repeating it once the row is visible keeps the guarantee
	// independent of when concurrent readers happened to look.
	s.apiKeyService.InvalidateAuthCacheByKey(ctx, apiKey.Key)
	if s.schedulerAccountCache != nil {
		_ = s.schedulerAccountCache.UpdateAccountInCache(ctx, account)
	}
	s.scheduleMonitorAfterCommit(monitor)

	return plan.result(group.ID, account.ID, apiKey.ID, monitor.ID, apiKey.Key), nil
}

// scheduleMonitorAfterCommit publishes the monitor to the runtime scheduler.
// ChannelMonitorService.Create skips scheduling while a transaction is in the
// context, because a transaction-scoped monitor must not become runnable
// before its row exists outside the transaction.
func (s *ChannelOnboardingService) scheduleMonitorAfterCommit(m *ChannelMonitor) {
	if s == nil || m == nil || s.monitorService == nil || s.monitorService.scheduler == nil {
		return
	}
	s.monitorService.scheduler.Schedule(m)
}

// resolveMonitorEndpoint prefers the admin-configured api_base_url over the
// origin derived from the incoming request headers.
func (s *ChannelOnboardingService) resolveMonitorEndpoint(ctx context.Context, requestOrigin string) string {
	if s.settingReader != nil {
		if configured, err := s.settingReader.GetValue(ctx, SettingKeyAPIBaseURL); err == nil {
			if origin := onboardingOriginFromURL(configured); origin != "" {
				return origin
			}
		}
	}
	return onboardingOriginFromURL(requestOrigin)
}

func (s *ChannelOnboardingService) ensureNamesAvailable(ctx context.Context, name string) error {
	exists, err := s.groupRepo.ExistsByName(ctx, name)
	if err != nil {
		return err
	}
	if exists {
		return ErrGroupExists
	}
	if err := checkOnboardingNameFree(ctx, s.accountRepo, name,
		"ACCOUNT_EXISTS", "account name already exists"); err != nil {
		return err
	}
	return checkOnboardingNameFree(ctx, s.monitorService.repo, name,
		"CHANNEL_MONITOR_EXISTS", "channel monitor name already exists")
}

// checkOnboardingNameFree skips the check when the repository does not expose
// ExistsByName; the check is an extra guard, not the correctness boundary.
func checkOnboardingNameFree(ctx context.Context, repo any, name, code, message string) error {
	checker, ok := repo.(onboardingNameChecker)
	if !ok {
		return nil
	}
	exists, err := checker.ExistsByName(ctx, name)
	if err != nil {
		return err
	}
	if exists {
		return infraerrors.Conflict(code, message)
	}
	return nil
}

// channelOnboardingPlan is the fully normalized shape of one onboarding
// request. Everything that decides what gets written lives here so it can be
// asserted without a database.
type channelOnboardingPlan struct {
	Name                string
	Platform            string
	RateMultiplier      float64
	UpstreamBaseURL     string
	UpstreamAPIKey      string
	PrimaryModel        string
	IntervalSeconds     int
	ExpectedInputTokens *int
	Endpoint            string
}

func newChannelOnboardingPlan(req ChannelOnboardingRequest, endpoint string) channelOnboardingPlan {
	intervalSeconds := defaultChannelOnboardingIntervalSeconds
	if req.IntervalSeconds != nil {
		intervalSeconds = *req.IntervalSeconds
	}
	return channelOnboardingPlan{
		Name:                strings.TrimSpace(req.Name),
		Platform:            strings.TrimSpace(req.Platform),
		RateMultiplier:      req.RateMultiplier,
		UpstreamBaseURL:     strings.TrimRight(strings.TrimSpace(req.UpstreamBaseURL), "/"),
		UpstreamAPIKey:      strings.TrimSpace(req.UpstreamAPIKey),
		PrimaryModel:        strings.TrimSpace(req.PrimaryModel),
		IntervalSeconds:     intervalSeconds,
		ExpectedInputTokens: req.ExpectedInputTokens,
		Endpoint:            endpoint,
	}
}

func (p channelOnboardingPlan) groupInput() *CreateGroupInput {
	return &CreateGroupInput{
		Name:             p.Name,
		Platform:         p.Platform,
		RateMultiplier:   p.RateMultiplier,
		IsExclusive:      false,
		SubscriptionType: SubscriptionTypeStandard,
	}
}

func (p channelOnboardingPlan) buildAccount() (*Account, error) {
	credentials := map[string]any{
		"base_url": p.UpstreamBaseURL,
		"api_key":  p.UpstreamAPIKey,
	}
	if err := NormalizeHeaderOverrideCredentials(credentials); err != nil {
		return nil, err
	}
	credentials = SanitizeStoredCredentials(p.Platform, credentials)

	extra, err := normalizeOpenAILongContextBillingExtra(p.Platform, nil)
	if err != nil {
		return nil, err
	}
	extra, err = normalizeGrokMediaEligibilityExtra(p.Platform, extra)
	if err != nil {
		return nil, err
	}
	extra, err = normalizeOpenAIAutoResetCreditExtra(p.Platform, AccountTypeAPIKey, false, extra)
	if err != nil {
		return nil, err
	}

	// Group binding is not expressed through CreateAccountInput.GroupIDs here:
	// buildAccountForCreate ignores that field (only adminServiceImpl.CreateAccount
	// reads it) and the caller binds the group atomically through
	// CreateWithAccountGroups. The mixed-channel check is likewise irrelevant —
	// the group was created empty moments ago inside the same transaction.
	probeEnabled := true
	account, err := buildAccountForCreate(&CreateAccountInput{
		Name:         p.Name,
		Platform:     p.Platform,
		Type:         AccountTypeAPIKey,
		Credentials:  credentials,
		Extra:        extra,
		Concurrency:  onboardingAccountConcurrency,
		Priority:     onboardingAccountPriority,
		ProbeEnabled: &probeEnabled,
	}, extra)
	if err != nil {
		return nil, err
	}
	enableOnboardingUpstreamRateSync(account)
	return account, nil
}

func (p channelOnboardingPlan) monitorParams(groupID, adminID int64, key string) ChannelMonitorCreateParams {
	return ChannelMonitorCreateParams{
		Name:                p.Name,
		Provider:            p.Platform,
		APIMode:             onboardingMonitorAPIMode(p.Platform),
		Endpoint:            p.Endpoint,
		APIKey:              key,
		PrimaryModel:        p.PrimaryModel,
		GroupName:           p.Name,
		Enabled:             true,
		IntervalSeconds:     p.IntervalSeconds,
		CreatedBy:           adminID,
		GroupID:             &groupID,
		PublicVisible:       true,
		ExpectedInputTokens: p.ExpectedInputTokens,
	}
}

func (p channelOnboardingPlan) result(groupID, accountID, apiKeyID, monitorID int64, key string) *ChannelOnboardingResult {
	return &ChannelOnboardingResult{
		GroupID:         groupID,
		AccountID:       accountID,
		APIKeyID:        apiKeyID,
		MonitorID:       monitorID,
		APIKeyMasked:    maskOnboardingAPIKey(key),
		GroupName:       p.Name,
		AccountName:     p.Name,
		MonitorName:     p.Name,
		Platform:        p.Platform,
		RateMultiplier:  p.RateMultiplier,
		IntervalSeconds: p.IntervalSeconds,
		PublicVisible:   true,
		ExpectedTokens:  p.ExpectedInputTokens,
	}
}

// enableOnboardingUpstreamRateSync only adds the rate-sync switch.
// buildAccountForCreate already turned the probe on (and rejected account
// identities that cannot probe) via CreateAccountInput.ProbeEnabled; upstream
// deliberately has no create-time entry point for the sync switch, so this is
// the one deviation onboarding makes from the normal create path.
func enableOnboardingUpstreamRateSync(account *Account) {
	if account.Extra == nil {
		account.Extra = make(map[string]any, 2)
	}
	account.Extra[UpstreamBillingRateSyncEnabledExtraKey] = true
}

// ChannelOnboardingPlatforms lists the platforms one-click onboarding accepts,
// derived from the single upstream source of truth. A provider without a probe
// adapter (antigravity) cannot back an active monitor, which is the whole point
// of the workflow, so it is excluded automatically.
func ChannelOnboardingPlatforms() []string {
	platforms := make([]string, 0, len(probeCapableProviders))
	for provider := range probeCapableProviders {
		platforms = append(platforms, provider)
	}
	sort.Strings(platforms)
	return platforms
}

func isChannelOnboardingPlatform(platform string) bool {
	_, ok := probeCapableProviders[platform]
	return ok
}

func validateChannelOnboardingRequest(req ChannelOnboardingRequest) error {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return infraerrors.BadRequest("CHANNEL_ONBOARDING_NAME_REQUIRED", "name is required")
	}
	if len([]rune(name)) > maxChannelMonitorNameRunes {
		return infraerrors.BadRequest("CHANNEL_ONBOARDING_NAME_TOO_LONG", "name is too long")
	}
	if !isChannelOnboardingPlatform(strings.TrimSpace(req.Platform)) {
		return ErrChannelMonitorInvalidProvider
	}
	if math.IsNaN(req.RateMultiplier) || math.IsInf(req.RateMultiplier, 0) || req.RateMultiplier <= 0 {
		return infraerrors.BadRequest("CHANNEL_ONBOARDING_RATE_INVALID", "rate_multiplier must be a finite number greater than zero")
	}
	if strings.TrimSpace(req.UpstreamBaseURL) == "" {
		return infraerrors.BadRequest("CHANNEL_ONBOARDING_BASE_URL_REQUIRED", "upstream_base_url is required")
	}
	if strings.TrimSpace(req.UpstreamAPIKey) == "" {
		return infraerrors.BadRequest("CHANNEL_ONBOARDING_API_KEY_REQUIRED", "upstream_api_key is required")
	}
	if strings.TrimSpace(req.PrimaryModel) == "" {
		return ErrChannelMonitorMissingPrimaryModel
	}
	if req.IntervalSeconds != nil {
		if err := validateInterval(*req.IntervalSeconds); err != nil {
			return err
		}
	}
	if req.ExpectedInputTokens != nil && *req.ExpectedInputTokens <= 0 {
		return infraerrors.BadRequest("CHANNEL_ONBOARDING_EXPECTED_TOKENS_INVALID", "expected_input_tokens must be greater than zero")
	}
	return nil
}

// onboardingOriginFromURL reduces a URL to scheme://host. validateEndpoint
// rejects endpoints carrying a path, so a configured api_base_url such as
// https://api.example.com/v1 still yields a usable monitor endpoint.
func onboardingOriginFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

func onboardingMonitorAPIMode(platform string) string {
	if platform == MonitorProviderOpenAI {
		return MonitorAPIModeResponses
	}
	return MonitorAPIModeChatCompletions
}

func maskOnboardingAPIKey(key string) string {
	if len(key) <= 4 {
		return "***"
	}
	return key[:4] + "***"
}
