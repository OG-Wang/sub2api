package service

import (
	"context"
	"fmt"
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
	Availability7d   float64
	ExtraModels      []ExtraModelStatus
	LastCheckedAt    *time.Time

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

	Timeline []UserMonitorTimelinePoint
}

// ListProviderHallView 组装大厅列表。
//
// 复用既有的批量聚合避免 N+1：1 次查监控项，1 次批量 latest，
// 1 次批量可用率，1 次批量 timeline。
func (s *ChannelMonitorService) ListProviderHallView(ctx context.Context) ([]*ProviderHallView, error) {
	rows, err := s.repo.ListProviderHall(ctx)
	if err != nil {
		return nil, fmt.Errorf("list provider hall monitors: %w", err)
	}
	if len(rows) == 0 {
		return []*ProviderHallView{}, nil
	}

	ids := make([]int64, 0, len(rows))
	primaryByID := make(map[int64]string, len(rows))
	extrasByID := make(map[int64][]string, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
		primaryByID[r.ID] = r.PrimaryModel
		extrasByID[r.ID] = r.ExtraModels
	}

	summaries := s.BatchMonitorStatusSummary(ctx, ids, primaryByID, extrasByID)
	latestMap := s.batchLatest(ctx, ids)
	timelineMap := s.batchTimeline(ctx, ids, primaryByID)

	views := make([]*ProviderHallView, 0, len(rows))
	for _, r := range rows {
		views = append(views, buildProviderHallView(
			r, summaries[r.ID], pickLatest(latestMap[r.ID], r.PrimaryModel), timelineMap[r.ID],
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
		Availability7d:      summary.Availability7d,
		ExtraModels:         summary.ExtraModels,
		LastCheckedAt:       row.LastCheckedAt,
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

func buildHallTimeline(entries []*ChannelMonitorHistoryEntry) []UserMonitorTimelinePoint {
	points := make([]UserMonitorTimelinePoint, 0, len(entries))
	for _, e := range entries {
		points = append(points, UserMonitorTimelinePoint{
			Status:            e.Status,
			LatencyMs:         e.LatencyMs,
			PingLatencyMs:     e.PingLatencyMs,
			CheckedAt:         e.CheckedAt,
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
