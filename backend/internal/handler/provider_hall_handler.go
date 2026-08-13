package handler

import (
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
}

// NewProviderHallHandler 创建 handler。
func NewProviderHallHandler(
	monitorService *service.ChannelMonitorService,
	settingService *service.SettingService,
) *ProviderHallHandler {
	return &ProviderHallHandler{
		monitorService: monitorService,
		settingService: settingService,
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

	PrimaryStatus    string                             `json:"primary_status"`
	PrimaryLatencyMs *int                               `json:"primary_latency_ms"`
	Availability7d   float64                            `json:"availability_7d"`
	ExtraModels      []providerHallExtraModelStatus     `json:"extra_models"`
	LastCheckedAt    *string                            `json:"last_checked_at"`
	Timeline         []service.UserMonitorTimelinePoint `json:"timeline"`

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

// List GET /api/v1/provider-hall/monitors
func (h *ProviderHallHandler) List(c *gin.Context) {
	if !h.enabled(c) {
		response.Success(c, gin.H{"items": []providerHallItem{}})
		return
	}
	views, err := h.monitorService.ListProviderHallView(c.Request.Context())
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
	})
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
		timeline = []service.UserMonitorTimelinePoint{}
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
		Availability7d:      v.Availability7d,
		ExtraModels:         extras,
		Timeline:            timeline,
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
