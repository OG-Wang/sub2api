package service

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateChannelOnboardingRequest(t *testing.T) {
	base := ChannelOnboardingRequest{
		Name:            "primary",
		Platform:        MonitorProviderOpenAI,
		RateMultiplier:  1,
		UpstreamBaseURL: "https://api.example.com",
		UpstreamAPIKey:  "secret",
		PrimaryModel:    "gpt-4o-mini",
		MonitorEndpoint: "https://service.example.com",
	}

	tests := []struct {
		name   string
		mutate func(*ChannelOnboardingRequest)
	}{
		{name: "missing name", mutate: func(r *ChannelOnboardingRequest) { r.Name = " " }},
		{name: "invalid platform", mutate: func(r *ChannelOnboardingRequest) { r.Platform = "antigravity" }},
		{name: "invalid rate", mutate: func(r *ChannelOnboardingRequest) { r.RateMultiplier = 0 }},
		{name: "nan rate", mutate: func(r *ChannelOnboardingRequest) { r.RateMultiplier = math.NaN() }},
		{name: "missing base url", mutate: func(r *ChannelOnboardingRequest) { r.UpstreamBaseURL = "" }},
		{name: "missing upstream key", mutate: func(r *ChannelOnboardingRequest) { r.UpstreamAPIKey = "" }},
		{name: "missing model", mutate: func(r *ChannelOnboardingRequest) { r.PrimaryModel = "" }},
		{name: "interval too short", mutate: func(r *ChannelOnboardingRequest) { n := 14; r.IntervalSeconds = &n }},
		{name: "interval too long", mutate: func(r *ChannelOnboardingRequest) { n := 3601; r.IntervalSeconds = &n }},
		{name: "invalid expected tokens", mutate: func(r *ChannelOnboardingRequest) { n := 0; r.ExpectedInputTokens = &n }},
		{name: "missing monitor endpoint", mutate: func(r *ChannelOnboardingRequest) { r.MonitorEndpoint = "" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := base
			tt.mutate(&req)
			if err := validateChannelOnboardingRequest(req); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}

	if err := validateChannelOnboardingRequest(base); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	customInterval := 120
	base.IntervalSeconds = &customInterval
	if err := validateChannelOnboardingRequest(base); err != nil {
		t.Fatalf("custom interval rejected: %v", err)
	}
}

func TestOnboardingMonitorAPIMode(t *testing.T) {
	if got := onboardingMonitorAPIMode(MonitorProviderOpenAI); got != MonitorAPIModeResponses {
		t.Fatalf("OpenAI mode = %q, want %q", got, MonitorAPIModeResponses)
	}
	if got := onboardingMonitorAPIMode(MonitorProviderAnthropic); got != MonitorAPIModeChatCompletions {
		t.Fatalf("non-OpenAI mode = %q, want %q", got, MonitorAPIModeChatCompletions)
	}
}

func TestValidateChannelOnboardingRequestAllowsPositiveRatesWithoutScaleOrSizeLimit(t *testing.T) {
	base := ChannelOnboardingRequest{
		Name:            "primary",
		Platform:        MonitorProviderOpenAI,
		UpstreamBaseURL: "https://api.example.com",
		UpstreamAPIKey:  "secret",
		PrimaryModel:    "gpt-4o-mini",
		MonitorEndpoint: "https://service.example.com",
	}

	for _, rate := range []float64{0.001, 0.000001, 1000000000} {
		base.RateMultiplier = rate
		if err := validateChannelOnboardingRequest(base); err != nil {
			t.Fatalf("rate %v rejected: %v", rate, err)
		}
	}
}

func TestMaskOnboardingAPIKey(t *testing.T) {
	if got := maskOnboardingAPIKey("sk-test-key"); got != "sk-t***" {
		t.Fatalf("masked key = %q", got)
	}
	if got := maskOnboardingAPIKey("abc"); got != "***" {
		t.Fatalf("short key = %q", got)
	}
}

func TestEnableOnboardingUpstreamRateSync(t *testing.T) {
	for _, platform := range []string{
		PlatformOpenAI,
		PlatformAnthropic,
		PlatformGemini,
		PlatformGrok,
		PlatformKimi,
		PlatformZhipu,
		PlatformDeepseek,
	} {
		t.Run(platform, func(t *testing.T) {
			account := &Account{Platform: platform, Type: AccountTypeAPIKey}
			require.NoError(t, enableOnboardingUpstreamRateSync(account))
			require.Equal(t, true, account.Extra[UpstreamBillingProbeEnabledExtraKey])
			require.Equal(t, true, account.Extra[UpstreamBillingRateSyncEnabledExtraKey])
		})
	}

	require.ErrorIs(t, enableOnboardingUpstreamRateSync(&Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
	}), ErrUpstreamBillingProbeAccountInvalid)
}
