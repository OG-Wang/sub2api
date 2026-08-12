package service

import (
	"net/url"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// 供应商大厅（Provider Hall）字段的归一与校验。
// 单独成文件：这几个字段是本站自定义扩展，集中在一处便于跟上游 rebase。

const (
	monitorPublicNoteMaxLen = 200
	monitorReportURLMaxLen  = 500
	// 探测 prompt 长度恒定（few-shot 算术模板，几十个 token），
	// 上限只用于挡住明显误填，不是业务约束。
	monitorExpectedInputTokensMax = 100000
)

var (
	ErrChannelMonitorInvalidReportURL = infraerrors.BadRequest(
		"CHANNEL_MONITOR_INVALID_REPORT_URL", "report_url must be a valid http(s) URL",
	)
	ErrChannelMonitorInvalidExpectedInputTokens = infraerrors.BadRequest(
		"CHANNEL_MONITOR_INVALID_EXPECTED_INPUT_TOKENS", "expected_input_tokens must be > 0",
	)
	ErrChannelMonitorInvalidGroupID = infraerrors.BadRequest(
		"CHANNEL_MONITOR_INVALID_GROUP_ID", "group_id must be > 0",
	)
)

// normalizeMonitorPublicNote 去空白并截断到列长上限。
func normalizeMonitorPublicNote(raw string) string {
	note := strings.TrimSpace(raw)
	if len(note) > monitorPublicNoteMaxLen {
		note = note[:monitorPublicNoteMaxLen]
	}
	return note
}

// validateMonitorReportURL 校验可选的外部报告链接。
// 空字符串合法（表示不展示链接）。仅放行 http/https：该 URL 会被渲染成
// 用户可点击的外链，允许 javascript:/data: 等 scheme 会形成 XSS 面。
func validateMonitorReportURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if len(trimmed) > monitorReportURLMaxLen {
		return "", ErrChannelMonitorInvalidReportURL
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", ErrChannelMonitorInvalidReportURL
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", ErrChannelMonitorInvalidReportURL
	}
	if parsed.Host == "" {
		return "", ErrChannelMonitorInvalidReportURL
	}
	return trimmed, nil
}

// validateMonitorGroupID 校验关联分组 ID。nil 表示未关联，合法。
func validateMonitorGroupID(groupID *int64) error {
	if groupID != nil && *groupID <= 0 {
		return ErrChannelMonitorInvalidGroupID
	}
	return nil
}

// validateMonitorExpectedInputTokens 校验注水检测基线。nil 表示走自动学习，合法。
func validateMonitorExpectedInputTokens(tokens *int) error {
	if tokens == nil {
		return nil
	}
	if *tokens <= 0 || *tokens > monitorExpectedInputTokensMax {
		return ErrChannelMonitorInvalidExpectedInputTokens
	}
	return nil
}

// validateMonitorPublicProfile 统一校验创建/更新后的大厅字段终值。
func validateMonitorPublicProfile(m *ChannelMonitor) error {
	if m == nil {
		return nil
	}
	if err := validateMonitorGroupID(m.GroupID); err != nil {
		return err
	}
	if err := validateMonitorExpectedInputTokens(m.ExpectedInputTokens); err != nil {
		return err
	}
	reportURL, err := validateMonitorReportURL(m.ReportURL)
	if err != nil {
		return err
	}
	m.ReportURL = reportURL
	m.PublicNote = normalizeMonitorPublicNote(m.PublicNote)
	return nil
}

// cloneIntPointer 复制一个可空 int，避免副本与源共享同一指针。
// （cloneInt64Pointer 已在 channel_monitor_service.go 中定义。）
func cloneIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

// applyMonitorPublicProfileUpdate 把更新参数中的大厅字段落到 existing 上。
// GroupID / ExpectedInputTokens 用显式 Clear 标志表达「置空」。
func applyMonitorPublicProfileUpdate(existing *ChannelMonitor, p ChannelMonitorUpdateParams) {
	if existing == nil {
		return
	}
	if p.ClearGroupID {
		existing.GroupID = nil
	} else if p.GroupID != nil {
		id := *p.GroupID
		existing.GroupID = &id
	}
	if p.PublicVisible != nil {
		existing.PublicVisible = *p.PublicVisible
	}
	if p.PublicNote != nil {
		existing.PublicNote = *p.PublicNote
	}
	if p.ReportURL != nil {
		existing.ReportURL = *p.ReportURL
	}
	if p.ClearExpectedInputTokens {
		existing.ExpectedInputTokens = nil
	} else if p.ExpectedInputTokens != nil {
		tokens := *p.ExpectedInputTokens
		existing.ExpectedInputTokens = &tokens
	}
}
