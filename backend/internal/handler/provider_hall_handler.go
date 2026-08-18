package handler

import (
	"log/slog"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// ProviderHallHandler 供应商大厅（用户只读）。
//
// 刻意不复用 /channel-monitors：那个接口服务的是既有「渠道监控」页，
// 列出全部启用的监控项。若给它加上 public_visible 过滤，
// 原页面会因为新字段默认 false 而直接变空。
type ProviderHallHandler struct {
	monitorService *service.ChannelMonitorService
	settingService *service.SettingService
	// v2Service 只用来把 range 解析成时间窗口与分桶粒度。
	// 复用它而不是自己算一份，是为了让探测曲线和 V2 用户曲线严格同轴：
	// 两处各维护一份 range→窗口 的映射，上游一改就会静默错位。
	v2Service *service.ChannelMonitorV2Service
}

// NewProviderHallHandler 创建 handler。
func NewProviderHallHandler(
	monitorService *service.ChannelMonitorService,
	settingService *service.SettingService,
	v2Service *service.ChannelMonitorV2Service,
) *ProviderHallHandler {
	return &ProviderHallHandler{
		monitorService: monitorService,
		settingService: settingService,
		v2Service:      v2Service,
	}
}

// providerHallItem 大厅单行响应。
//
// 不含倍率：倍率是用户专属的（分组可能配了针对该用户的独立倍率），
// 前端用 /groups/available + /groups/rates 按 group_id join，
// 保证每个用户看到的是自己的价格。
type providerHallItem struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	GroupName string `json:"group_name"`
	GroupID   *int64 `json:"group_id"`
	// Platform 取自关联分组，用于顶部分页签过滤。
	Platform     string `json:"platform"`
	PublicNote   string `json:"public_note"`
	ReportURL    string `json:"report_url"`
	PrimaryModel string `json:"primary_model"`

	PrimaryStatus    string `json:"primary_status"`
	PrimaryLatencyMs *int   `json:"primary_latency_ms"`
	// Availability 主模型在**当前窗口**内的探测可用率（0-100），窗口内没有探测时为 null。
	// 刻意不叫 availability_7d 了：它现在跟着 range 走，再叫 7d 就是骗人。
	Availability  *float64                            `json:"availability"`
	ExtraModels   []providerHallExtraModelStatus      `json:"extra_models"`
	LastCheckedAt *string                             `json:"last_checked_at"`
	Timeline      []service.ProviderHallTimelinePoint `json:"timeline"`
	// IntervalSeconds 探测间隔，前端用它判断曲线上哪里是真的断了。
	IntervalSeconds int `json:"interval_seconds"`

	PrimaryTTFTMs *int `json:"primary_ttft_ms"`
	// InputTokens 含缓存命中的总输入；减去 CachedInputTokens 即计费口径的净输入。
	InputTokens         *int `json:"input_tokens"`
	CachedInputTokens   *int `json:"cached_input_tokens"`
	OutputTokens        *int `json:"output_tokens"`
	ExpectedInputTokens *int `json:"expected_input_tokens"`
	InputTokensDeviated bool `json:"input_tokens_deviated"`
	InputTokensExcess   int  `json:"input_tokens_excess"`
}

type providerHallExtraModelStatus struct {
	Model     string `json:"model"`
	Status    string `json:"status"`
	LatencyMs *int   `json:"latency_ms"`
}

// enabled 大厅需要 V1 探测数据与 V2 用户指标同时可用，即 hybrid 模式。
func (h *ProviderHallHandler) enabled(c *gin.Context) bool {
	if h.settingService == nil {
		return true
	}
	runtime := h.settingService.GetChannelMonitorRuntime(c.Request.Context())
	return runtime.ActiveProbesAllowed() && runtime.PassiveAggregationAllowed()
}

// List GET /api/v1/provider-hall/monitors?range=90m|24h|7d|30d&platform=openai
//
// 探测曲线（V1）和用户曲线（V2 matrix）在这里一次返回，而不是让前端并发打两个接口。
//
// 原因不是省一次请求，是正确性：两个接口各自 ParseFilter，各自取 now 再按桶对齐，
// 请求刚好跨过桶边界时会拿到相邻的两个 [start, end)。前端却只能用其中一个当横轴，
// 于是用户线整体错开一个桶，且没有任何报错。合并后全程只解析一次窗口，
// 两条曲线用的是同一个 filter，同轴是结构上保证的，不靠概率。
func (h *ProviderHallHandler) List(c *gin.Context) {
	filter, err := h.v2Service.ParseFilter(c.Query("range"), queryList(c, "platform"), nil, nil)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	// window 是曲线的横轴定义：前端按 bucket_start 在 [start, end) 中的相对位置
	// 定横坐标，两条线因此同一个 x 对应同一时刻。
	window := gin.H{
		"range":          filter.Range,
		"start":          filter.Start.UTC().Format(time.RFC3339),
		"end":            filter.End.UTC().Format(time.RFC3339),
		"bucket_seconds": int(filter.Bucket / time.Second),
	}

	if !h.enabled(c) {
		response.Success(c, gin.H{"items": []providerHallItem{}, "window": window, "passive": nil})
		return
	}
	views, err := h.monitorService.ListProviderHallView(c.Request.Context(), service.HallTimelineWindow{
		Start:  filter.Start,
		End:    filter.End,
		Bucket: filter.Bucket,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	items := make([]providerHallItem, 0, len(views))
	for _, v := range views {
		items = append(items, providerHallViewToItem(v))
	}
	response.Success(c, gin.H{
		"items": items,
		// 前端展示「数据生成时间」用。
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"window":       window,
		"passive":      h.passiveMatrix(c, filter),
	})
}

// passiveMatrix 取 V2 被动统计。
//
// 失败返回 nil 而不是报错：探测数据是这页的主体，缺了被动指标只是少几列
// 和少一条曲线，不该让整页 500。admin 传 false，脱敏由 Matrix 内部按普通用户处理。
func (h *ProviderHallHandler) passiveMatrix(
	c *gin.Context,
	filter service.ChannelMonitorV2Filter,
) *service.ChannelMonitorV2Matrix {
	matrix, err := h.v2Service.Matrix(
		c.Request.Context(), filter, service.ChannelMonitorV2GroupByPlatformGroup, false,
	)
	if err != nil {
		slog.Warn("provider_hall: passive matrix unavailable", "error", err)
		return nil
	}
	return matrix
}

func providerHallViewToItem(v *service.ProviderHallView) providerHallItem {
	extras := make([]providerHallExtraModelStatus, 0, len(v.ExtraModels))
	for _, e := range v.ExtraModels {
		extras = append(extras, providerHallExtraModelStatus{
			Model:     e.Model,
			Status:    e.Status,
			LatencyMs: e.LatencyMs,
		})
	}
	timeline := v.Timeline
	if timeline == nil {
		timeline = []service.ProviderHallTimelinePoint{}
	}

	item := providerHallItem{
		ID:                  v.ID,
		Name:                v.Name,
		Provider:            v.Provider,
		GroupName:           v.GroupName,
		GroupID:             v.GroupID,
		Platform:            v.Platform,
		PublicNote:          v.PublicNote,
		ReportURL:           v.ReportURL,
		PrimaryModel:        v.PrimaryModel,
		PrimaryStatus:       v.PrimaryStatus,
		PrimaryLatencyMs:    v.PrimaryLatencyMs,
		Availability:        v.Availability,
		ExtraModels:         extras,
		Timeline:            timeline,
		IntervalSeconds:     v.IntervalSeconds,
		PrimaryTTFTMs:       v.PrimaryTTFTMs,
		InputTokens:         v.InputTokens,
		CachedInputTokens:   v.CachedInputTokens,
		OutputTokens:        v.OutputTokens,
		ExpectedInputTokens: v.ExpectedInputTokens,
		InputTokensDeviated: v.InputTokensDeviated,
		InputTokensExcess:   v.InputTokensExcess,
	}
	if v.LastCheckedAt != nil {
		s := v.LastCheckedAt.UTC().Format(time.RFC3339)
		item.LastCheckedAt = &s
	}
	return item
}
