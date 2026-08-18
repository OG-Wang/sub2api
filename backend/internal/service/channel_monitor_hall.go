package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// 供应商大厅（Provider Hall）的用户只读视图。
//
// 与既有的 ListUserView 分开：那个服务的是「渠道监控」页，列出全部启用的监控项；
// 大厅只列勾选了 public_visible 的，且要多带分组关联、备注、报告链接与探测用量。
// 分开实现避免改动既有接口语义（加 public_visible 过滤会让原页面直接变空）。

// ChannelMonitorHallRow repository 层返回的原始行：监控项 + 关联分组信息。
type ChannelMonitorHallRow struct {
	ID           int64
	Name         string
	Provider     string
	APIMode      string
	PrimaryModel string
	ExtraModels  []string
	// GroupName 优先取关联分组的真实名字，分组不存在时回退到监控项里的字符串快照。
	GroupName           string
	GroupID             *int64
	PublicNote          string
	ReportURL           string
	ExpectedInputTokens *int
	LastCheckedAt       *time.Time
	// GroupPlatform 关联分组的平台，分组已删除或未关联时为空串。
	GroupPlatform string
	// IntervalSeconds 探测间隔。前端拿它判断曲线上哪里是真的断了：
	// 相邻两点间隔明显超过它，说明中间那段根本没探测，不能连线。
	IntervalSeconds int
}

// HallTimelineWindow 大厅曲线的时间轴定义。
//
// 刻意不在这里自己定义 range → 窗口 的映射，而是由 handler 调
// ChannelMonitorV2Service.ParseFilter 算好传进来：探测线（V1）和用户线（V2）
// 必须落在同一条时间轴上才谈得上对比，两处各算一遍迟早会漂。
//
// Bucket 是 V2 用户线的桶粒度，探测线**不分桶**（每一次探测都是一个点），
// 这里带着它只是为了原样透给前端，用来判断用户线上哪里断了。
type HallTimelineWindow struct {
	Start  time.Time
	End    time.Time
	Bucket time.Duration
}

// hallTimelineMaxPoints 单个监控在一个窗口内最多返回的探测点数。
//
// 这不是「为了好看而抽稀」的阈值，而是防炸的上限：曲线要求一次探测一个点，
// 按默认 300s 间隔，最长的 7d 窗口也只有 2016 点，取不满这个数。
// 只有把探测间隔调到下限（15s，7d = 40320 点/监控）时才会截断——
// 那种配置下 20 个监控就是几十万行、几十 MB 的响应，必须有个头。
//
// 传输成本按实测：7d 全量点 21 个监控约 6.4MB，线上 Caddy 开了
// zstd+gzip6，压缩比 18x，实际过网 352KB，页面加载付得起。
const hallTimelineMaxPoints = 4000

// ProviderHallTimelinePoint 大厅曲线的一个点，即一次探测。
//
// 与 UserMonitorTimelinePoint 的区别只是去掉了曲线用不到的 ping_latency_ms，
// 并保证时间已归一到 UTC。刻意不做任何聚合：桶内平均会把一次真实故障
// 抹进周围的正常样本里，而这页存在的意义就是让人看见故障。
type ProviderHallTimelinePoint struct {
	CheckedAt         time.Time `json:"checked_at"`
	Status            string    `json:"status"`
	LatencyMs         *int      `json:"latency_ms"`
	TTFTMs            *int      `json:"ttft_ms"`
	InputTokens       *int      `json:"input_tokens"`
	CachedInputTokens *int      `json:"cached_input_tokens"`
	OutputTokens      *int      `json:"output_tokens"`
}

// ProviderHallView 大厅单行的完整视图。
type ProviderHallView struct {
	ID        int64
	Name      string
	Provider  string
	GroupName string
	GroupID   *int64
	// Platform 用于页面顶部的 OpenAI / Anthropic / 其他 分页签过滤。
	// 以关联分组的 platform 为准，而不是 Provider——后者是探测协议，
	// 两者可能不一致（如 antigravity 分组用 anthropic 协议探测）。
	Platform     string
	PublicNote   string
	ReportURL    string
	PrimaryModel string

	PrimaryStatus    string
	PrimaryLatencyMs *int
	// Availability 主模型在**当前窗口**内的探测可用率（0-100），窗口内没有探测时为 nil。
	// 跟着 window 走，不是固定 7 天——见 hallAvailability。
	Availability  *float64
	ExtraModels   []ExtraModelStatus
	LastCheckedAt *time.Time
	// IntervalSeconds 探测间隔，透给前端做曲线断点判定。
	IntervalSeconds int

	// 最近一次探测的指标。
	PrimaryTTFTMs *int
	// InputTokens 上游报告的总输入（含缓存命中）；CachedInputTokens 是其中的缓存部分。
	// 两者相减即网关用量记录里的 input 口径。
	InputTokens       *int
	CachedInputTokens *int
	OutputTokens      *int
	// ExpectedInputTokens 本地算出的应计输入 token。
	ExpectedInputTokens *int
	// InputTokensDeviated 上游报数与真值对不上（多报少报都算）。
	InputTokensDeviated bool
	// InputTokensExcess 相对真值的偏离量，正数为多报、负数为少报。
	InputTokensExcess int

	Timeline []ProviderHallTimelinePoint
}

// ListProviderHallView 组装大厅列表。
//
// window 决定曲线的时间跨度，同时也是可用率的统计区间——两者用的是同一批探测点，
// 所以列里的数字和图里的线永远对得上（见 hallAvailability）。
//
// 3 次查询，无 N+1：1 次查监控项，1 次批量 latest，1 次批量窗口内探测点。
func (s *ChannelMonitorService) ListProviderHallView(
	ctx context.Context,
	window HallTimelineWindow,
) ([]*ProviderHallView, error) {
	rows, err := s.repo.ListProviderHall(ctx)
	if err != nil {
		return nil, fmt.Errorf("list provider hall monitors: %w", err)
	}
	if len(rows) == 0 {
		return []*ProviderHallView{}, nil
	}

	ids := make([]int64, 0, len(rows))
	primaryByID := make(map[int64]string, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
		primaryByID[r.ID] = r.PrimaryModel
	}

	latestMap := s.batchLatest(ctx, ids)
	timelineMap := s.batchHallTimeline(ctx, ids, primaryByID, window)

	views := make([]*ProviderHallView, 0, len(rows))
	for _, r := range rows {
		latest := latestMap[r.ID]
		// 刻意不走 BatchMonitorStatusSummary：那个方法内部会再查一次 latest
		// （这里已经取过）外加一次固定 7 天的可用率，而大厅的可用率改从窗口内的
		// 探测点算，那次查询的结果会被整个丢掉。直接复用它那个无 IO 的组装函数，
		// 可用率字典传 nil——summary.Availability7d 大厅不读。
		summary := buildStatusSummary(indexLatestByModel(latest), nil, r.PrimaryModel, r.ExtraModels)
		views = append(views, buildProviderHallView(
			r, summary, pickLatest(latest, r.PrimaryModel), timelineMap[r.ID],
		))
	}
	return views, nil
}

func buildProviderHallView(
	row *ChannelMonitorHallRow,
	summary MonitorStatusSummary,
	latest *ChannelMonitorLatest,
	timeline []*ChannelMonitorHistoryEntry,
) *ProviderHallView {
	view := &ProviderHallView{
		ID:                  row.ID,
		Name:                row.Name,
		Provider:            row.Provider,
		GroupName:           row.GroupName,
		GroupID:             row.GroupID,
		Platform:            normalizeHallPlatform(row.GroupPlatform),
		PublicNote:          row.PublicNote,
		ReportURL:           row.ReportURL,
		PrimaryModel:        row.PrimaryModel,
		PrimaryStatus:       summary.PrimaryStatus,
		PrimaryLatencyMs:    summary.PrimaryLatencyMs,
		Availability:        hallAvailability(timeline),
		ExtraModels:         summary.ExtraModels,
		LastCheckedAt:       row.LastCheckedAt,
		IntervalSeconds:     row.IntervalSeconds,
		ExpectedInputTokens: row.ExpectedInputTokens,
		Timeline:            buildHallTimeline(timeline),
	}

	if latest != nil {
		view.PrimaryTTFTMs = latest.TTFTMs
		view.InputTokens = latest.InputTokens
		view.CachedInputTokens = latest.CachedInputTokens
		view.OutputTokens = latest.OutputTokens
	}

	// 输入 token 比对用的参考值：优先本地计算，管理员手填时以手填为准。
	// 这里只能用 PrimaryModel 重算 prompt，与探测时一致。
	ref := monitorExpectedInputTokens(row.Provider, row.APIMode, row.PrimaryModel, monitorChallengeReferencePrompt())
	if verdict := evaluateMonitorInputTokens(ref, view.InputTokens, row.ExpectedInputTokens); verdict != nil {
		view.InputTokensDeviated = verdict.Deviated
		view.InputTokensExcess = verdict.ExcessTokens
		if view.ExpectedInputTokens == nil {
			expected := verdict.Reference.Expected
			view.ExpectedInputTokens = &expected
		}
	}
	return view
}

// batchHallTimeline 批量取窗口内的探测点。
// 失败仅日志、返回空 map：曲线画不出来不该让整页 500，与同层其它聚合一致。
func (s *ChannelMonitorService) batchHallTimeline(
	ctx context.Context,
	ids []int64,
	primaryByID map[int64]string,
	window HallTimelineWindow,
) map[int64][]*ChannelMonitorHistoryEntry {
	entries, err := s.repo.ListHistoryInWindowForMonitors(
		ctx, ids, primaryByID, window.Start, window.End, hallTimelineMaxPoints,
	)
	if err != nil {
		slog.Warn("channel_monitor: provider hall timeline failed", "error", err)
		return map[int64][]*ChannelMonitorHistoryEntry{}
	}
	return entries
}

// hallAvailability 主模型在窗口内的探测可用率（0-100）。
//
// 存在的理由是「可用率必须跟着时间窗口变」。上游 BatchMonitorStatusSummary 给的是
// 写死 7 天的数，窗口选到 90 分钟它也纹丝不动，等于把「刚刚恢复」和「此刻正挂着」
// 一起抹平。本地实测两种都真实出现过：某渠道 7 天 60.64%，而最近 24 小时是 0%
// （正在挂，列里却写着 60.64%）；另一个 7 天 69.11%，最近 24 小时 100%
// （早就恢复了，列里还挂着旧账）。
//
// 直接数曲线上的点，不再单独查库：这些点就是同一份响应里画出来的那条线，
// 数字和图形因此永远一致，不会出现「图上一排红点、可用率却是 100%」。
// 口径与上游 ComputeAvailabilityForMonitors 完全一致——operational 与 degraded
// 都算可用（degraded 是「慢但成功」），其余算不可用；本地对齐验证过 6 个渠道的
// 7d 取值与上游 SQL 逐一相符。
//
// 注意 timeline 受 hallTimelineMaxPoints 截断（只在探测间隔远低于默认 300s 时触发），
// 那种情况下曲线本身也只画得出最近的一段，两者仍然同源，不会互相打架。
//
// 窗口内一次探测都没有时返回 nil 而非 0：0% 会被读成「全挂」，
// 而「没探测过」和「探测全失败」是两回事，前端要显示的是「—」。
func hallAvailability(entries []*ChannelMonitorHistoryEntry) *float64 {
	if len(entries) == 0 {
		return nil
	}
	ok := 0
	for _, e := range entries {
		if e.Status == MonitorStatusOperational || e.Status == MonitorStatusDegraded {
			ok++
		}
	}
	pct := float64(ok) / float64(len(entries)) * 100
	return &pct
}

func buildHallTimeline(entries []*ChannelMonitorHistoryEntry) []ProviderHallTimelinePoint {
	points := make([]ProviderHallTimelinePoint, 0, len(entries))
	for _, e := range entries {
		points = append(points, ProviderHallTimelinePoint{
			// 统一成 UTC 再序列化：driver 会按会话时区把 timestamptz 还原成带偏移的
			// time.Time（容器 TZ=Asia/Shanghai 时就是 +08:00），而同一响应里的
			// window / generated_at / last_checked_at 都是 Z 结尾。
			// 时刻本身不受影响，但同一份 JSON 里两种写法会让人误以为口径不同。
			CheckedAt:         e.CheckedAt.UTC(),
			Status:            e.Status,
			LatencyMs:         e.LatencyMs,
			TTFTMs:            e.TTFTMs,
			InputTokens:       e.InputTokens,
			CachedInputTokens: e.CachedInputTokens,
			OutputTokens:      e.OutputTokens,
		})
	}
	return points
}

// normalizeHallPlatform 归一分组平台。
// 空值按上游规则（group.go 的默认值）视为 anthropic。
func normalizeHallPlatform(platform string) string {
	p := strings.ToLower(strings.TrimSpace(platform))
	if p == "" {
		return PlatformAnthropic
	}
	return p
}
