//go:build unit

package service

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	dbgroup "github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

// A literal public IP keeps validateEndpoint on its net.ParseIP fast path, so
// these tests never touch DNS.
const onboardingTestEndpoint = "https://93.184.216.34"

func newOnboardingTestClient(t *testing.T) *dbent.Client {
	t.Helper()

	// A DSN per test keeps the shared-cache in-memory database isolated:
	// the rollback assertions must not see rows left by another test.
	dsn := "file:channel_onboarding_" + url.QueryEscape(t.Name()) + "?mode=memory&cache=shared&_fk=1"
	db, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// Every stub embeds its interface so only the methods this workflow actually
// calls are implemented; anything else panics loudly instead of silently
// returning a zero value.

type onboardingAdminServiceStub struct {
	AdminService
	sawTx     bool
	created   *CreateGroupInput
	failWith  error
	nextID    int64
	entClient *dbent.Client
}

func (s *onboardingAdminServiceStub) CreateGroup(ctx context.Context, input *CreateGroupInput) (*Group, error) {
	s.sawTx = dbent.TxFromContext(ctx) != nil
	s.created = input
	if s.failWith != nil {
		return nil, s.failWith
	}
	// Write a real row through the context transaction so rollback is
	// observable rather than merely assumed.
	row, err := clientFromOnboardingCtx(ctx, s.entClient).Group.Create().
		SetName(input.Name).
		SetPlatform(input.Platform).
		SetRateMultiplier(input.RateMultiplier).
		SetStatus(StatusActive).
		SetSubscriptionType(input.SubscriptionType).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	s.nextID = row.ID
	return &Group{ID: row.ID, Name: input.Name, Platform: input.Platform}, nil
}

func clientFromOnboardingCtx(ctx context.Context, fallback *dbent.Client) *dbent.Client {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return tx.Client()
	}
	return fallback
}

type onboardingGroupRepoStub struct {
	GroupRepository
	exists bool
	err    error
}

func (s *onboardingGroupRepoStub) ExistsByName(context.Context, string) (bool, error) {
	return s.exists, s.err
}

type onboardingAccountRepoStub struct {
	AdminAccountRepository
	sawTx   bool
	created *Account
	groups  []AccountGroup
	exists  bool
}

func (s *onboardingAccountRepoStub) ExistsByName(context.Context, string) (bool, error) {
	return s.exists, nil
}

func (s *onboardingAccountRepoStub) CreateWithAccountGroups(ctx context.Context, account *Account, groups []AccountGroup) error {
	s.sawTx = dbent.TxFromContext(ctx) != nil
	account.ID = 4242
	account.GroupIDs = []int64{}
	for _, g := range groups {
		account.GroupIDs = append(account.GroupIDs, g.GroupID)
	}
	s.created = account
	s.groups = groups
	return nil
}

type onboardingAPIKeyStub struct {
	sawTx        bool
	req          CreateAPIKeyRequest
	userID       int64
	invalidated  []string
	failWith     error
	generatedKey string
}

func (s *onboardingAPIKeyStub) Create(ctx context.Context, userID int64, req CreateAPIKeyRequest) (*APIKey, error) {
	s.sawTx = dbent.TxFromContext(ctx) != nil
	s.userID = userID
	s.req = req
	if s.failWith != nil {
		return nil, s.failWith
	}
	return &APIKey{ID: 77, UserID: userID, Key: s.generatedKey, GroupID: req.GroupID, Status: StatusActive}, nil
}

func (s *onboardingAPIKeyStub) InvalidateAuthCacheByKey(_ context.Context, key string) {
	s.invalidated = append(s.invalidated, key)
}

type onboardingMonitorRepoStub struct {
	ChannelMonitorRepository
	sawTx bool
	// storedAPIKey is captured at persist time: the service overwrites
	// m.APIKey with the plaintext afterwards, so reading it later would hide
	// whether the row was actually written encrypted.
	storedAPIKey string
	created      *ChannelMonitor
	exists       bool
	failWith     error
}

func (s *onboardingMonitorRepoStub) ExistsByName(context.Context, string) (bool, error) {
	return s.exists, nil
}

func (s *onboardingMonitorRepoStub) Create(ctx context.Context, m *ChannelMonitor) error {
	s.sawTx = dbent.TxFromContext(ctx) != nil
	if s.failWith != nil {
		return s.failWith
	}
	m.ID = 909
	s.storedAPIKey = m.APIKey
	s.created = m
	return nil
}

type onboardingEncryptorStub struct{}

func (onboardingEncryptorStub) Encrypt(plaintext string) (string, error) {
	return "enc:" + plaintext, nil
}

func (onboardingEncryptorStub) Decrypt(ciphertext string) (string, error) { return ciphertext, nil }

type onboardingSchedulerStub struct {
	scheduled []*ChannelMonitor
}

func (s *onboardingSchedulerStub) Schedule(m *ChannelMonitor) { s.scheduled = append(s.scheduled, m) }
func (s *onboardingSchedulerStub) Unschedule(int64)           {}

type onboardingAccountCacheStub struct {
	updated []*Account
}

func (s *onboardingAccountCacheStub) UpdateAccountInCache(_ context.Context, a *Account) error {
	s.updated = append(s.updated, a)
	return nil
}

type onboardingHarness struct {
	svc       *ChannelOnboardingService
	client    *dbent.Client
	admin     *onboardingAdminServiceStub
	groups    *onboardingGroupRepoStub
	accounts  *onboardingAccountRepoStub
	apiKeys   *onboardingAPIKeyStub
	monitors  *onboardingMonitorRepoStub
	scheduler *onboardingSchedulerStub
	cache     *onboardingAccountCacheStub
}

func newOnboardingHarness(t *testing.T) *onboardingHarness {
	t.Helper()

	client := newOnboardingTestClient(t)
	h := &onboardingHarness{
		client:    client,
		admin:     &onboardingAdminServiceStub{entClient: client},
		groups:    &onboardingGroupRepoStub{},
		accounts:  &onboardingAccountRepoStub{},
		apiKeys:   &onboardingAPIKeyStub{generatedKey: "sk-generated-secret"},
		monitors:  &onboardingMonitorRepoStub{},
		scheduler: &onboardingSchedulerStub{},
		cache:     &onboardingAccountCacheStub{},
	}

	monitorService := NewChannelMonitorService(h.monitors, onboardingEncryptorStub{})
	monitorService.SetScheduler(h.scheduler)

	h.svc = &ChannelOnboardingService{
		entClient:             client,
		adminService:          h.admin,
		groupRepo:             h.groups,
		accountRepo:           h.accounts,
		apiKeyService:         h.apiKeys,
		monitorService:        monitorService,
		settingReader:         onboardingSettingReaderStub{value: onboardingTestEndpoint},
		schedulerAccountCache: h.cache,
	}
	return h
}

func onboardingCreateRequest() ChannelOnboardingRequest {
	concurrency := 32
	return ChannelOnboardingRequest{
		Name:            "primary",
		Concurrency:     &concurrency,
		Platform:        MonitorProviderOpenAI,
		RateMultiplier:  1.25,
		UpstreamBaseURL: "https://api.upstream.example/",
		UpstreamAPIKey:  "upstream-secret",
		PrimaryModel:    "gpt-4o-mini",
		RequestOrigin:   "https://ignored.example",
	}
}

func TestChannelOnboardingCreateRunsInOneTransactionAndPublishesAfterCommit(t *testing.T) {
	h := newOnboardingHarness(t)
	ctx := context.Background()

	result, err := h.svc.Create(ctx, 42, onboardingCreateRequest())
	require.NoError(t, err)

	// Every write joined the caller-owned transaction.
	require.True(t, h.admin.sawTx, "group create must run inside the transaction")
	require.True(t, h.accounts.sawTx, "account create must run inside the transaction")
	require.True(t, h.apiKeys.sawTx, "api key create must run inside the transaction")
	require.True(t, h.monitors.sawTx, "monitor create must run inside the transaction")

	// The group row survived the commit.
	exists, err := h.client.Group.Query().Where(dbgroup.NameEQ("primary")).Exist(ctx)
	require.NoError(t, err)
	require.True(t, exists)

	// The account is bound to the group that was just created.
	require.Equal(t, []AccountGroup{{GroupID: h.admin.nextID, Priority: onboardingAccountPriority}}, h.accounts.groups)
	// The concurrency the admin typed reaches the account, not the default.
	require.Equal(t, 32, h.accounts.created.Concurrency)
	require.Equal(t, 32, result.Concurrency)

	require.NotNil(t, h.apiKeys.req.GroupID)
	require.Equal(t, h.admin.nextID, *h.apiKeys.req.GroupID)
	require.Equal(t, int64(42), h.apiKeys.userID)
	require.Equal(t, "primary", h.apiKeys.req.Name)

	// The monitor points at the resolved endpoint and stores the new key
	// encrypted rather than in the clear.
	require.Equal(t, onboardingTestEndpoint, h.monitors.created.Endpoint)
	require.Equal(t, "enc:sk-generated-secret", h.monitors.storedAPIKey)
	require.True(t, h.monitors.created.Enabled)
	require.True(t, h.monitors.created.PublicVisible)

	// Post-commit side effects ran exactly once each.
	require.Len(t, h.scheduler.scheduled, 1)
	require.Equal(t, int64(909), h.scheduler.scheduled[0].ID)
	require.Len(t, h.cache.updated, 1)
	require.Equal(t, int64(4242), h.cache.updated[0].ID)
	require.Equal(t, []string{"sk-generated-secret"}, h.apiKeys.invalidated)

	require.Equal(t, "sk-g***", result.APIKeyMasked)
	require.Equal(t, int64(909), result.MonitorID)
}

func TestChannelOnboardingCreateRollsBackAndSkipsSideEffectsWhenMonitorFails(t *testing.T) {
	h := newOnboardingHarness(t)
	h.monitors.failWith = errors.New("monitor repository is down")
	ctx := context.Background()

	_, err := h.svc.Create(ctx, 42, onboardingCreateRequest())
	require.Error(t, err)

	// The group row written earlier in the transaction is gone.
	exists, err := h.client.Group.Query().Where(dbgroup.NameEQ("primary")).Exist(ctx)
	require.NoError(t, err)
	require.False(t, exists, "a failed onboarding must not leave a group behind")

	// Nothing was published to the runtime.
	require.Empty(t, h.scheduler.scheduled, "a rolled back monitor must never be scheduled")
	require.Empty(t, h.cache.updated)
	require.Empty(t, h.apiKeys.invalidated)
}

func TestChannelOnboardingCreateRejectsDuplicateNamesBeforeOpeningTransaction(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*onboardingHarness)
	}{
		{name: "group", setup: func(h *onboardingHarness) { h.groups.exists = true }},
		{name: "account", setup: func(h *onboardingHarness) { h.accounts.exists = true }},
		{name: "monitor", setup: func(h *onboardingHarness) { h.monitors.exists = true }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newOnboardingHarness(t)
			tc.setup(h)

			_, err := h.svc.Create(context.Background(), 42, onboardingCreateRequest())
			require.Error(t, err)
			require.False(t, h.admin.sawTx, "the transaction must not open on a name conflict")
			require.Nil(t, h.admin.created)
		})
	}
}

// The endpoint is fully validated before the transaction opens, so a bad
// endpoint never costs an insert or holds a transaction open across DNS.
func TestChannelOnboardingCreateValidatesEndpointBeforeOpeningTransaction(t *testing.T) {
	for _, endpoint := range []string{
		"http://93.184.216.34", // plain HTTP
		"https://127.0.0.1",    // loopback
		"https://10.0.0.1",     // private range
		"",                     // nothing configured and no usable request origin
	} {
		h := newOnboardingHarness(t)
		h.svc.settingReader = onboardingSettingReaderStub{value: endpoint}
		req := onboardingCreateRequest()
		req.RequestOrigin = ""

		_, err := h.svc.Create(context.Background(), 42, req)
		require.Error(t, err, endpoint)
		require.Nil(t, h.admin.created, endpoint)
	}
}

func TestChannelOnboardingCreateRequiresConfiguredDependencies(t *testing.T) {
	_, err := (&ChannelOnboardingService{}).Create(context.Background(), 42, onboardingCreateRequest())
	require.Error(t, err)

	h := newOnboardingHarness(t)
	_, err = h.svc.Create(context.Background(), 0, onboardingCreateRequest())
	require.Error(t, err)
	require.Nil(t, h.admin.created)
}
