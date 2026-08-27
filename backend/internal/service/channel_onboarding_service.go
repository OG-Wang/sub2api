package service

import (
	"context"
	"html"
	"math"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// APIKeyGenerator is the small part of APIKeyService needed by onboarding.
// Keeping it as a port makes the orchestration service easy to unit test.
type APIKeyGenerator interface {
	GenerateKey() (string, error)
}

// SchedulerAccountCache is intentionally narrow: the outbox remains the
// source of truth for bucket rebuilds, while this hook makes the new account
// immediately visible to the per-account scheduler cache after commit.
type SchedulerAccountCache interface {
	UpdateAccountInCache(context.Context, *Account) error
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
	MonitorEndpoint     string
}

const defaultChannelOnboardingIntervalSeconds = 900

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

type channelOnboardingAccountNamer interface {
	ExistsByName(context.Context, string) (bool, error)
}

type channelOnboardingMonitorNamer interface {
	ExistsByName(context.Context, string) (bool, error)
}

// ChannelOnboardingService atomically creates the four resources required to
// bring a new upstream channel online.
type ChannelOnboardingService struct {
	entClient             *dbent.Client
	adminService          AdminService
	groupRepo             GroupRepository
	accountRepo           AdminAccountRepository
	apiKeyRepo            APIKeyRepository
	monitorService        *ChannelMonitorService
	userRepo              UserRepository
	apiKeyGenerator       APIKeyGenerator
	authCacheInvalidator  APIKeyAuthCacheInvalidator
	schedulerAccountCache SchedulerAccountCache
}

func NewChannelOnboardingService(
	entClient *dbent.Client,
	adminService AdminService,
	groupRepo GroupRepository,
	accountRepo AdminAccountRepository,
	apiKeyRepo APIKeyRepository,
	monitorService *ChannelMonitorService,
	userRepo UserRepository,
	apiKeyGenerator APIKeyGenerator,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	schedulerAccountCache SchedulerAccountCache,
) *ChannelOnboardingService {
	return &ChannelOnboardingService{
		entClient:             entClient,
		adminService:          adminService,
		groupRepo:             groupRepo,
		accountRepo:           accountRepo,
		apiKeyRepo:            apiKeyRepo,
		monitorService:        monitorService,
		userRepo:              userRepo,
		apiKeyGenerator:       apiKeyGenerator,
		authCacheInvalidator:  authCacheInvalidator,
		schedulerAccountCache: schedulerAccountCache,
	}
}

func (s *ChannelOnboardingService) Create(ctx context.Context, adminID int64, req ChannelOnboardingRequest) (*ChannelOnboardingResult, error) {
	if err := validateChannelOnboardingRequest(req); err != nil {
		return nil, err
	}
	if adminID <= 0 {
		return nil, infraerrors.BadRequest("CHANNEL_ONBOARDING_ADMIN_REQUIRED", "admin user is required")
	}
	if s == nil || s.entClient == nil || s.adminService == nil || s.groupRepo == nil || s.accountRepo == nil || s.apiKeyRepo == nil || s.monitorService == nil || s.userRepo == nil || s.apiKeyGenerator == nil {
		return nil, infraerrors.InternalServer("CHANNEL_ONBOARDING_NOT_CONFIGURED", "channel onboarding is not configured")
	}
	if _, err := s.userRepo.GetByID(ctx, adminID); err != nil {
		return nil, err
	}

	name := strings.TrimSpace(req.Name)
	platform := strings.TrimSpace(req.Platform)
	intervalSeconds := defaultChannelOnboardingIntervalSeconds
	if req.IntervalSeconds != nil {
		intervalSeconds = *req.IntervalSeconds
	}
	if exists, err := s.groupRepo.ExistsByName(ctx, name); err != nil {
		return nil, err
	} else if exists {
		return nil, ErrGroupExists
	}
	if namer, ok := s.accountRepo.(channelOnboardingAccountNamer); ok {
		if exists, err := namer.ExistsByName(ctx, name); err != nil {
			return nil, err
		} else if exists {
			return nil, infraerrors.Conflict("ACCOUNT_EXISTS", "account name already exists")
		}
	}
	if namer, ok := s.monitorService.repo.(channelOnboardingMonitorNamer); ok {
		if exists, err := namer.ExistsByName(ctx, name); err != nil {
			return nil, err
		} else if exists {
			return nil, infraerrors.Conflict("CHANNEL_MONITOR_EXISTS", "channel monitor name already exists")
		}
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

	group, err := s.adminService.CreateGroup(txCtx, &CreateGroupInput{
		Name:             name,
		Platform:         platform,
		RateMultiplier:   req.RateMultiplier,
		IsExclusive:      false,
		SubscriptionType: SubscriptionTypeStandard,
	})
	if err != nil {
		return nil, err
	}

	credentials := map[string]any{
		"base_url": strings.TrimRight(strings.TrimSpace(req.UpstreamBaseURL), "/"),
		"api_key":  strings.TrimSpace(req.UpstreamAPIKey),
	}
	if err := NormalizeHeaderOverrideCredentials(credentials); err != nil {
		return nil, err
	}
	credentials = SanitizeStoredCredentials(platform, credentials)
	extra, err := normalizeOpenAILongContextBillingExtra(platform, nil)
	if err != nil {
		return nil, err
	}
	extra, err = normalizeGrokMediaEligibilityExtra(platform, extra)
	if err != nil {
		return nil, err
	}
	extra, err = normalizeOpenAIAutoResetCreditExtra(platform, AccountTypeAPIKey, false, extra)
	if err != nil {
		return nil, err
	}
	account, err := buildAccountForCreate(&CreateAccountInput{
		Name:                  name,
		Platform:              platform,
		Type:                  AccountTypeAPIKey,
		Credentials:           credentials,
		Extra:                 extra,
		Concurrency:           10,
		Priority:              1,
		GroupIDs:              []int64{group.ID},
		SkipDefaultGroupBind:  true,
		SkipMixedChannelCheck: true,
	}, extra)
	if err != nil {
		return nil, err
	}
	if err := enableOnboardingUpstreamRateSync(account); err != nil {
		return nil, err
	}
	if err := s.accountRepo.CreateWithAccountGroups(txCtx, account, []AccountGroup{{GroupID: group.ID, Priority: 1}}); err != nil {
		return nil, err
	}

	key, err := s.apiKeyGenerator.GenerateKey()
	if err != nil {
		return nil, err
	}
	apiKey := &APIKey{
		UserID:  adminID,
		Key:     key,
		Name:    html.EscapeString(name),
		GroupID: onboardingInt64Ptr(group.ID),
		Status:  StatusActive,
	}
	if err := s.apiKeyRepo.Create(txCtx, apiKey); err != nil {
		return nil, err
	}

	monitor, err := s.monitorService.Create(txCtx, ChannelMonitorCreateParams{
		Name:                name,
		Provider:            platform,
		APIMode:             onboardingMonitorAPIMode(platform),
		Endpoint:            req.MonitorEndpoint,
		APIKey:              key,
		PrimaryModel:        strings.TrimSpace(req.PrimaryModel),
		GroupName:           name,
		Enabled:             true,
		IntervalSeconds:     intervalSeconds,
		CreatedBy:           adminID,
		GroupID:             onboardingInt64Ptr(group.ID),
		PublicVisible:       true,
		ExpectedInputTokens: req.ExpectedInputTokens,
	})
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	rollback = false

	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByKey(ctx, key)
	}
	if s.schedulerAccountCache != nil {
		_ = s.schedulerAccountCache.UpdateAccountInCache(ctx, account)
	}
	s.monitorService.ScheduleAfterCommit(monitor)

	return &ChannelOnboardingResult{
		GroupID:         group.ID,
		AccountID:       account.ID,
		APIKeyID:        apiKey.ID,
		MonitorID:       monitor.ID,
		APIKeyMasked:    maskOnboardingAPIKey(key),
		GroupName:       name,
		AccountName:     name,
		MonitorName:     name,
		Platform:        platform,
		RateMultiplier:  req.RateMultiplier,
		IntervalSeconds: intervalSeconds,
		PublicVisible:   true,
		ExpectedTokens:  req.ExpectedInputTokens,
	}, nil
}

func enableOnboardingUpstreamRateSync(account *Account) error {
	if !isUpstreamBillingProbeAccount(account) {
		return ErrUpstreamBillingProbeAccountInvalid
	}
	if account.Extra == nil {
		account.Extra = make(map[string]any, 2)
	}
	account.Extra[UpstreamBillingProbeEnabledExtraKey] = true
	account.Extra[UpstreamBillingRateSyncEnabledExtraKey] = true
	return nil
}

func validateChannelOnboardingRequest(req ChannelOnboardingRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return infraerrors.BadRequest("CHANNEL_ONBOARDING_NAME_REQUIRED", "name is required")
	}
	if len([]rune(strings.TrimSpace(req.Name))) > maxChannelMonitorNameRunes {
		return infraerrors.BadRequest("CHANNEL_ONBOARDING_NAME_TOO_LONG", "name is too long")
	}
	switch strings.TrimSpace(req.Platform) {
	case MonitorProviderOpenAI,
		MonitorProviderAnthropic,
		MonitorProviderGemini,
		MonitorProviderGrok,
		MonitorProviderKimi,
		MonitorProviderZhipu,
		MonitorProviderDeepseek:
	default:
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
	if strings.TrimSpace(req.MonitorEndpoint) == "" {
		return ErrChannelMonitorInvalidEndpoint
	}
	return nil
}

func onboardingMonitorAPIMode(platform string) string {
	if platform == MonitorProviderOpenAI {
		return MonitorAPIModeResponses
	}
	return MonitorAPIModeChatCompletions
}

func onboardingInt64Ptr(v int64) *int64 { return &v }

func maskOnboardingAPIKey(key string) string {
	if len(key) <= 4 {
		return "***"
	}
	return key[:4] + "***"
}
