package service

import "testing"

// TestNormalizeChannelMonitorModeHybrid 确认 hybrid 被识别为合法模式，
// 且非法值仍回落到 v1（安全默认）。
func TestNormalizeChannelMonitorModeHybrid(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"hybrid", ChannelMonitorModeHybrid},
		{"HYBRID", ChannelMonitorModeHybrid},
		{"  hybrid  ", ChannelMonitorModeHybrid},
		{"v1", ChannelMonitorModeV1},
		{"v2", ChannelMonitorModeV2},
		{"", ChannelMonitorModeV1},
		{"v3", ChannelMonitorModeV1},
	}
	for _, tc := range cases {
		if got := normalizeChannelMonitorMode(tc.raw); got != tc.want {
			t.Errorf("normalizeChannelMonitorMode(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

// TestChannelMonitorRuntimeHybridGates 确认 hybrid 下两套监控同时放行，
// 且 v1/v2 的互斥语义保持不变，disabled 一律关闭。
func TestChannelMonitorRuntimeHybridGates(t *testing.T) {
	cases := []struct {
		name        string
		runtime     ChannelMonitorRuntime
		wantActive  bool
		wantPassive bool
	}{
		{"hybrid enabled", ChannelMonitorRuntime{Enabled: true, Mode: ChannelMonitorModeHybrid}, true, true},
		{"v1 enabled", ChannelMonitorRuntime{Enabled: true, Mode: ChannelMonitorModeV1}, true, false},
		{"v2 enabled", ChannelMonitorRuntime{Enabled: true, Mode: ChannelMonitorModeV2}, false, true},
		{"hybrid disabled", ChannelMonitorRuntime{Enabled: false, Mode: ChannelMonitorModeHybrid}, false, false},
		{"v1 disabled", ChannelMonitorRuntime{Enabled: false, Mode: ChannelMonitorModeV1}, false, false},
		{"v2 disabled", ChannelMonitorRuntime{Enabled: false, Mode: ChannelMonitorModeV2}, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.runtime.ActiveProbesAllowed(); got != tc.wantActive {
				t.Errorf("ActiveProbesAllowed() = %v, want %v", got, tc.wantActive)
			}
			if got := tc.runtime.PassiveAggregationAllowed(); got != tc.wantPassive {
				t.Errorf("PassiveAggregationAllowed() = %v, want %v", got, tc.wantPassive)
			}
		})
	}
}
