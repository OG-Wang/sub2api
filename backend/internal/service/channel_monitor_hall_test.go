package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 大厅曲线的窗口与时间线转换。
//
// 单独成文件：与 channel_monitor_hall.go 一样是本站扩展，rebase 冲突面最小。

// TestHallTimelineWindowIsWholeBuckets 钉住「两条线同轴」的前提。
//
// 探测线不分桶（一次探测一个点），但 V2 用户线是按桶聚合的。两条线共用
// handler 一次 ParseFilter 算出的 [start, end)，前端按时间戳在窗口里的相对
// 位置定横坐标，同一个 x 才是同一时刻。
//
// 这里断言窗口两端都压在桶边界上、且是整数个桶：一旦上游改成不整除的窗口，
// 用户线最后一个桶会有半个露在窗口外，横坐标随之偏移，且没有任何报错。
//
// 大厅界面目前只开放 90m/24h/7d（30d 数据量太大），但这里仍把 ParseFilter
// 支持的四档全测：接口不拒绝 30d，以后若放开，这条性质必须照样成立。
func TestHallTimelineWindowIsWholeBuckets(t *testing.T) {
	svc := NewChannelMonitorV2Service(nil)

	for _, rangeValue := range []string{"90m", "24h", "7d", "30d"} {
		t.Run(rangeValue, func(t *testing.T) {
			filter, err := svc.ParseFilter(rangeValue, nil, nil, nil)
			require.NoError(t, err)

			bucketSeconds := int64(filter.Bucket / time.Second)
			require.Positive(t, bucketSeconds)

			require.Zero(t, filter.Start.Unix()%bucketSeconds, "start 未对齐到桶边界")
			require.Zero(t, filter.End.Unix()%bucketSeconds, "end 未对齐到桶边界")
			require.Zero(t, int64(filter.End.Sub(filter.Start)/time.Second)%bucketSeconds,
				"窗口必须是整数个桶")
		})
	}
}

// TestHallTimelineMaxPointsCoversDefaultInterval 上限不能把正常配置也截断。
//
// hallTimelineMaxPoints 是防炸用的，不是抽稀阈值。按后台默认的 300s 间隔，
// 最长的 7d 窗口需要 2016 个点——上限必须明显高于它，否则「展示所有探测点」
// 这个前提在最常见的配置下就已经不成立了。
func TestHallTimelineMaxPointsCoversDefaultInterval(t *testing.T) {
	const defaultIntervalSeconds = 300
	sevenDays := int((7 * 24 * time.Hour) / time.Second)
	pointsAtDefault := sevenDays / defaultIntervalSeconds

	require.Equal(t, 2016, pointsAtDefault, "默认间隔下 7d 的点数变了，重新评估上限")
	require.Greater(t, hallTimelineMaxPoints, pointsAtDefault,
		"上限低于默认配置的点数，正常部署也会被截断")
}

func TestProviderHallMonitorVisible(t *testing.T) {
	tests := []struct {
		name   string
		status []string
		want   bool
	}{
		{name: "fewer than three results stays visible", status: []string{MonitorStatusFailed, MonitorStatusError}, want: true},
		{name: "three failures hides", status: []string{MonitorStatusFailed, MonitorStatusError, MonitorStatusFailed}, want: false},
		{name: "latest operational reopens", status: []string{MonitorStatusOperational, MonitorStatusFailed, MonitorStatusFailed}, want: true},
		{name: "success resets consecutive failure count", status: []string{MonitorStatusFailed, MonitorStatusFailed, MonitorStatusOperational}, want: true},
		{name: "degraded counts as usable", status: []string{MonitorStatusError, MonitorStatusError, MonitorStatusDegraded}, want: true},
		{name: "older success does not break three latest failures", status: []string{MonitorStatusFailed, MonitorStatusError, MonitorStatusFailed, MonitorStatusOperational}, want: false},
		{name: "unknown result stays visible", status: []string{MonitorStatusFailed, MonitorStatusFailed, ""}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries := make([]*ChannelMonitorHistoryEntry, 0, len(tt.status))
			for _, status := range tt.status {
				entries = append(entries, &ChannelMonitorHistoryEntry{Status: status})
			}
			require.Equal(t, tt.want, providerHallMonitorVisible(entries, 3))
		})
	}
}

func TestProviderHallMonitorVisibleUsesConfiguredThreshold(t *testing.T) {
	failures := []*ChannelMonitorHistoryEntry{
		{Status: MonitorStatusFailed},
		{Status: MonitorStatusError},
		{Status: MonitorStatusFailed},
		{Status: MonitorStatusError},
	}

	require.False(t, providerHallMonitorVisible(failures, 4))
	require.True(t, providerHallMonitorVisible(failures, 5))
	require.False(t, providerHallMonitorVisible(failures[:1], 0), "invalid low thresholds clamp to 1")
}

// TestBuildHallTimelinePreservesEveryProbe 转换层不重排、不合并、不丢点。
//
// 这是「不聚合」的回归防线：曾经这里按时间桶取平均，把一次真实故障
// 抹进了周围的正常样本里。
func TestBuildHallTimelinePreservesEveryProbe(t *testing.T) {
	// 故意用非 UTC 时区构造，模拟 driver 按会话时区（容器 TZ=Asia/Shanghai）
	// 还原 timestamptz 的情形。
	shanghai := time.FixedZone("CST", 8*3600)
	base := time.Date(2026, 8, 18, 8, 0, 0, 0, shanghai)
	ttft := 300
	entries := []*ChannelMonitorHistoryEntry{
		{CheckedAt: base, Status: MonitorStatusOperational, TTFTMs: &ttft},
		{CheckedAt: base.Add(5 * time.Minute), Status: MonitorStatusFailed},
		{CheckedAt: base.Add(10 * time.Minute), Status: MonitorStatusDegraded},
		{CheckedAt: base.Add(15 * time.Minute), Status: MonitorStatusOperational},
	}

	points := buildHallTimeline(entries)

	require.Len(t, points, 4, "一次探测必须对应一个点")
	require.Equal(t, MonitorStatusOperational, points[0].Status)
	require.Equal(t, &ttft, points[0].TTFTMs)
	// 中间那次失败必须原样保留，不能被前后的正常样本平均掉。
	require.Equal(t, MonitorStatusFailed, points[1].Status)
	require.Equal(t, MonitorStatusDegraded, points[2].Status)

	// 序列化前归一到 UTC：同一响应里 window / generated_at 都是 Z 结尾，
	// 只有 timeline 带 +08:00 会让人误以为口径不同。
	require.Equal(t, time.UTC, points[0].CheckedAt.Location())
	require.True(t, points[0].CheckedAt.Equal(base), "归一时区不能改变时刻")

	// 空输入返回空切片而不是 nil：handler 直接序列化，nil 会变成 JSON null。
	require.NotNil(t, buildHallTimeline(nil))
	require.Empty(t, buildHallTimeline(nil))
}

// TestHallAvailabilityCountsDegradedAsAvailable 可用率口径必须与上游 SQL 一致。
//
// 上游 ComputeAvailabilityForMonitors 的判据是
// `COUNT(*) FILTER (WHERE status IN ('operational','degraded'))`——degraded 是
// 「慢但成功」，算可用。这里在 Go 里重算，两边一旦分叉，同一个渠道在
// 「渠道监控」页和大厅页会显示两个不同的可用率，而且没人会想到去对。
func TestHallAvailabilityCountsDegradedAsAvailable(t *testing.T) {
	entries := []*ChannelMonitorHistoryEntry{
		{Status: MonitorStatusOperational},
		{Status: MonitorStatusDegraded},
		{Status: MonitorStatusFailed},
		{Status: MonitorStatusError},
	}

	got := hallAvailability(entries)
	require.NotNil(t, got)
	require.InDelta(t, 50.0, *got, 1e-9, "operational + degraded 记为可用")
}

// TestHallAvailabilityNilWhenNoProbes 「没探测」不能显示成 0%。
//
// 0% 会被读成「这个渠道全挂了」，而窗口内一次都没探测（新建的监控、
// 或者刚把间隔调长）是完全不同的一件事。返回 nil 让前端显示「—」。
func TestHallAvailabilityNilWhenNoProbes(t *testing.T) {
	require.Nil(t, hallAvailability(nil))
	require.Nil(t, hallAvailability([]*ChannelMonitorHistoryEntry{}))

	// 对照：真的全失败才是 0%。
	allFailed := hallAvailability([]*ChannelMonitorHistoryEntry{{Status: MonitorStatusFailed}})
	require.NotNil(t, allFailed)
	require.Zero(t, *allFailed)
}

// TestHallAvailabilityFollowsWindow 同一批探测、不同窗口必须算出不同的数。
//
// 这是本次修复的核心性质：可用率写死 7 天时，一个此刻正挂着的渠道在列里
// 仍显示 60%，而窗口选的是最近 24 小时。这里用「前半段全挂、后半段全好」
// 的序列，模拟「已恢复」——取全量是 50%，只取后半段必须是 100%。
func TestHallAvailabilityFollowsWindow(t *testing.T) {
	recovered := []*ChannelMonitorHistoryEntry{
		{Status: MonitorStatusFailed},
		{Status: MonitorStatusFailed},
		{Status: MonitorStatusOperational},
		{Status: MonitorStatusOperational},
	}

	full := hallAvailability(recovered)
	recent := hallAvailability(recovered[2:])
	require.NotNil(t, full)
	require.NotNil(t, recent)
	require.InDelta(t, 50.0, *full, 1e-9)
	require.InDelta(t, 100.0, *recent, 1e-9, "窄窗口必须只统计窗口内的探测")
}
