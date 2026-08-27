package service

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func validChannelOnboardingRequest() ChannelOnboardingRequest {
	return ChannelOnboardingRequest{
		Name:            "primary",
		Platform:        MonitorProviderOpenAI,
		RateMultiplier:  1,
		UpstreamBaseURL: "https://api.example.com",
		UpstreamAPIKey:  "secret",
		PrimaryModel:    "gpt-4o-mini",
		RequestOrigin:   "https://service.example.com",
	}
}

func TestValidateChannelOnboardingRequest(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ChannelOnboardingRequest)
	}{
		{name: "missing name", mutate: func(r *ChannelOnboardingRequest) { r.Name = " " }},
		{name: "name too long", mutate: func(r *ChannelOnboardingRequest) {
			r.Name = string(make([]rune, maxChannelMonitorNameRunes+1))
		}},
		{name: "invalid platform", mutate: func(r *ChannelOnboardingRequest) { r.Platform = "not-a-platform" }},
		{name: "antigravity has no probe adapter", mutate: func(r *ChannelOnboardingRequest) {
			r.Platform = MonitorProviderAntigravity
		}},
		{name: "invalid rate", mutate: func(r *ChannelOnboardingRequest) { r.RateMultiplier = 0 }},
		{name: "nan rate", mutate: func(r *ChannelOnboardingRequest) { r.RateMultiplier = math.NaN() }},
		{name: "inf rate", mutate: func(r *ChannelOnboardingRequest) { r.RateMultiplier = math.Inf(1) }},
		{name: "missing base url", mutate: func(r *ChannelOnboardingRequest) { r.UpstreamBaseURL = "" }},
		{name: "missing upstream key", mutate: func(r *ChannelOnboardingRequest) { r.UpstreamAPIKey = "" }},
		{name: "missing model", mutate: func(r *ChannelOnboardingRequest) { r.PrimaryModel = "" }},
		{name: "interval too short", mutate: func(r *ChannelOnboardingRequest) { n := 14; r.IntervalSeconds = &n }},
		{name: "interval too long", mutate: func(r *ChannelOnboardingRequest) { n := 3601; r.IntervalSeconds = &n }},
		{name: "invalid expected tokens", mutate: func(r *ChannelOnboardingRequest) { n := 0; r.ExpectedInputTokens = &n }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validChannelOnboardingRequest()
			tt.mutate(&req)
			require.Error(t, validateChannelOnboardingRequest(req))
		})
	}

	require.NoError(t, validateChannelOnboardingRequest(validChannelOnboardingRequest()))

	req := validChannelOnboardingRequest()
	customInterval := 120
	req.IntervalSeconds = &customInterval
	require.NoError(t, validateChannelOnboardingRequest(req))
}

// The endpoint is resolved and validated by Create, not by the pure request
// validator, so an empty request origin must not be rejected here: a configured
// api_base_url can still supply a usable endpoint.
func TestValidateChannelOnboardingRequestIgnoresRequestOrigin(t *testing.T) {
	req := validChannelOnboardingRequest()
	req.RequestOrigin = ""
	require.NoError(t, validateChannelOnboardingRequest(req))
}

func TestChannelOnboardingPlatformsMatchProbeCapableProviders(t *testing.T) {
	platforms := ChannelOnboardingPlatforms()
	require.Len(t, platforms, len(probeCapableProviders))
	require.NotContains(t, platforms, MonitorProviderAntigravity)
	for _, platform := range platforms {
		require.True(t, isChannelOnboardingPlatform(platform))
	}
	// Sorted output keeps the list stable for API responses and assertions.
	for i := 1; i < len(platforms); i++ {
		require.Less(t, platforms[i-1], platforms[i])
	}
	// Every accepted platform must also be able to hold an API key account
	// with upstream billing probing, which is what onboarding turns on.
	for _, platform := range platforms {
		require.True(t, IsUpstreamBillingProbeIdentity(platform, AccountTypeAPIKey), platform)
	}
}

func TestValidateChannelOnboardingRequestAllowsPositiveRatesWithoutScaleOrSizeLimit(t *testing.T) {
	req := validChannelOnboardingRequest()
	for _, rate := range []float64{0.001, 0.000001, 1000000000} {
		req.RateMultiplier = rate
		require.NoError(t, validateChannelOnboardingRequest(req), rate)
	}
}

func TestOnboardingMonitorAPIMode(t *testing.T) {
	require.Equal(t, MonitorAPIModeResponses, onboardingMonitorAPIMode(MonitorProviderOpenAI))
	for _, platform := range []string{
		MonitorProviderAnthropic,
		MonitorProviderGemini,
		MonitorProviderGrok,
		MonitorProviderKimi,
		MonitorProviderZhipu,
		MonitorProviderDeepseek,
	} {
		require.Equal(t, MonitorAPIModeChatCompletions, onboardingMonitorAPIMode(platform), platform)
	}
}

func TestMaskOnboardingAPIKey(t *testing.T) {
	require.Equal(t, "sk-t***", maskOnboardingAPIKey("sk-test-key"))
	require.Equal(t, "***", maskOnboardingAPIKey("abc"))
	require.Equal(t, "***", maskOnboardingAPIKey(""))
}

func TestOnboardingOriginFromURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "https://api.example.com", want: "https://api.example.com"},
		{in: " https://api.example.com/ ", want: "https://api.example.com"},
		{in: "https://api.example.com/v1", want: "https://api.example.com"},
		{in: "https://api.example.com:8443/v1?x=1", want: "https://api.example.com:8443"},
		{in: "http://api.example.com", want: "http://api.example.com"},
		{in: "api.example.com", want: ""},
		{in: "", want: ""},
		{in: "   ", want: ""},
		{in: "://broken", want: ""},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, onboardingOriginFromURL(tt.in), tt.in)
	}
}

var errOnboardingSettingUnavailable = errors.New("setting store unavailable")

type onboardingSettingReaderStub struct {
	value string
	err   error
}

func (s onboardingSettingReaderStub) GetValue(_ context.Context, _ string) (string, error) {
	return s.value, s.err
}

func TestResolveMonitorEndpointPrefersConfiguredAPIBaseURL(t *testing.T) {
	ctx := context.Background()

	// Configured api_base_url wins over the request-derived origin, so a
	// spoofed X-Forwarded-Host cannot redirect the monitor's probes.
	svc := &ChannelOnboardingService{settingReader: onboardingSettingReaderStub{value: "https://api.configured.example/v1"}}
	require.Equal(t, "https://api.configured.example", svc.resolveMonitorEndpoint(ctx, "https://attacker.example"))

	// Unset or unusable settings fall back to the request origin.
	for _, reader := range []onboardingSettingReaderStub{
		{value: ""},
		{value: "not-a-url"},
		{err: errOnboardingSettingUnavailable},
	} {
		svc := &ChannelOnboardingService{settingReader: reader}
		require.Equal(t, "https://request.example", svc.resolveMonitorEndpoint(ctx, "https://request.example/"))
	}

	// No reader at all still works.
	svc = &ChannelOnboardingService{}
	require.Equal(t, "https://request.example", svc.resolveMonitorEndpoint(ctx, "https://request.example"))
}

func TestNewChannelOnboardingPlanNormalizes(t *testing.T) {
	expected := 4096
	interval := 120
	req := ChannelOnboardingRequest{
		Name:                "  primary  ",
		Platform:            "  openai  ",
		RateMultiplier:      1.25,
		UpstreamBaseURL:     "  https://api.example.com///  ",
		UpstreamAPIKey:      "  upstream-secret  ",
		PrimaryModel:        "  gpt-4o-mini  ",
		IntervalSeconds:     &interval,
		ExpectedInputTokens: &expected,
	}

	plan := newChannelOnboardingPlan(req, "https://service.example.com")
	require.Equal(t, "primary", plan.Name)
	require.Equal(t, MonitorProviderOpenAI, plan.Platform)
	require.Equal(t, "https://api.example.com", plan.UpstreamBaseURL)
	require.Equal(t, "upstream-secret", plan.UpstreamAPIKey)
	require.Equal(t, "gpt-4o-mini", plan.PrimaryModel)
	require.Equal(t, 120, plan.IntervalSeconds)
	require.Equal(t, &expected, plan.ExpectedInputTokens)

	req.IntervalSeconds = nil
	require.Equal(t, defaultChannelOnboardingIntervalSeconds, newChannelOnboardingPlan(req, "").IntervalSeconds)
}

func TestChannelOnboardingPlanBuildAccountEnablesProbeAndRateSync(t *testing.T) {
	for _, platform := range ChannelOnboardingPlatforms() {
		t.Run(platform, func(t *testing.T) {
			plan := newChannelOnboardingPlan(ChannelOnboardingRequest{
				Name:            "primary",
				Platform:        platform,
				RateMultiplier:  1,
				UpstreamBaseURL: "https://api.example.com/",
				UpstreamAPIKey:  "upstream-secret",
				PrimaryModel:    "model",
			}, "https://service.example.com")

			account, err := plan.buildAccount()
			require.NoError(t, err)
			require.Equal(t, "primary", account.Name)
			require.Equal(t, platform, account.Platform)
			require.Equal(t, AccountTypeAPIKey, account.Type)
			require.Equal(t, StatusActive, account.Status)
			require.Equal(t, "https://api.example.com", account.Credentials["base_url"])
			require.Equal(t, "upstream-secret", account.Credentials["api_key"])
			// buildAccountForCreate deletes both switches for ordinary creates;
			// onboarding must end up with both on.
			require.Equal(t, true, account.Extra[UpstreamBillingProbeEnabledExtraKey])
			require.Equal(t, true, account.Extra[UpstreamBillingRateSyncEnabledExtraKey])
			// The page multiplier belongs to the group only: the account keeps
			// the database default until the first successful probe syncs it.
			require.Nil(t, account.RateMultiplier)
		})
	}
}

// Onboarding no longer guards the probe identity itself; it relies on
// CreateAccountInput.ProbeEnabled, so that guard has to stay effective.
func TestOnboardingProbeEnabledGuardRejectsNonAPIKeyIdentity(t *testing.T) {
	probeEnabled := true
	_, err := buildAccountForCreate(&CreateAccountInput{
		Name:         "primary",
		Platform:     PlatformOpenAI,
		Type:         AccountTypeOAuth,
		ProbeEnabled: &probeEnabled,
	}, nil)
	require.ErrorIs(t, err, ErrUpstreamBillingProbeAccountInvalid)
}

func TestChannelOnboardingPlanGroupInputIsStandardAndShared(t *testing.T) {
	plan := newChannelOnboardingPlan(validChannelOnboardingRequest(), "https://service.example.com")
	input := plan.groupInput()

	require.Equal(t, "primary", input.Name)
	require.Equal(t, MonitorProviderOpenAI, input.Platform)
	require.Equal(t, 1.0, input.RateMultiplier)
	require.False(t, input.IsExclusive)
	require.Equal(t, SubscriptionTypeStandard, input.SubscriptionType)
}

func TestChannelOnboardingPlanMonitorParams(t *testing.T) {
	expected := 2048
	req := validChannelOnboardingRequest()
	req.ExpectedInputTokens = &expected
	plan := newChannelOnboardingPlan(req, "https://service.example.com")

	params := plan.monitorParams(11, 42, "sk-generated")
	require.Equal(t, "primary", params.Name)
	require.Equal(t, MonitorProviderOpenAI, params.Provider)
	require.Equal(t, MonitorAPIModeResponses, params.APIMode)
	require.Equal(t, "https://service.example.com", params.Endpoint)
	require.Equal(t, "sk-generated", params.APIKey)
	require.Equal(t, "gpt-4o-mini", params.PrimaryModel)
	require.Equal(t, "primary", params.GroupName)
	require.True(t, params.Enabled)
	require.True(t, params.PublicVisible)
	require.Equal(t, defaultChannelOnboardingIntervalSeconds, params.IntervalSeconds)
	require.Equal(t, int64(42), params.CreatedBy)
	require.NotNil(t, params.GroupID)
	require.Equal(t, int64(11), *params.GroupID)
	require.Equal(t, &expected, params.ExpectedInputTokens)
}

func TestChannelOnboardingPlanResultNeverExposesPlaintextKey(t *testing.T) {
	plan := newChannelOnboardingPlan(validChannelOnboardingRequest(), "https://service.example.com")

	result := plan.result(1, 2, 3, 4, "sk-plaintext-secret")
	require.Equal(t, int64(1), result.GroupID)
	require.Equal(t, int64(2), result.AccountID)
	require.Equal(t, int64(3), result.APIKeyID)
	require.Equal(t, int64(4), result.MonitorID)
	require.Equal(t, "sk-p***", result.APIKeyMasked)
	require.NotContains(t, result.APIKeyMasked, "plaintext")
	require.True(t, result.PublicVisible)
}
