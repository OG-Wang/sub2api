package service

import (
	"github.com/tidwall/gjson"
)

// 探测响应里的 token 用量提取。
//
// 单独成文件的原因：这是本站为「供应商大厅」加的扩展，
// providerAdapter 本身是上游结构，改动越少 rebase 越省事。
// 这里只按 (provider, api_mode) 查一张只读 path 表，不碰 callProvider 签名。

// monitorUsagePaths 描述一个 provider 响应里 token 用量的 gjson path。
type monitorUsagePaths struct {
	inputTokens  string
	outputTokens string
}

// monitorUsagePathTable 各 provider 非流式响应的用量字段位置。
// key 为 provider；OpenAI Responses 协议单列，见 monitorUsagePathsFor。
//
//nolint:gochecknoglobals // 只读静态表，初始化后不变更。
var monitorUsagePathTable = map[string]monitorUsagePaths{
	MonitorProviderOpenAI: {
		inputTokens:  "usage.prompt_tokens",
		outputTokens: "usage.completion_tokens",
	},
	MonitorProviderGrok: {
		inputTokens:  "usage.prompt_tokens",
		outputTokens: "usage.completion_tokens",
	},
	MonitorProviderAnthropic: {
		inputTokens:  "usage.input_tokens",
		outputTokens: "usage.output_tokens",
	},
	MonitorProviderGemini: {
		inputTokens:  "usageMetadata.promptTokenCount",
		outputTokens: "usageMetadata.candidatesTokenCount",
	},
}

// monitorOpenAIResponsesUsagePaths OpenAI Responses 协议用的是 input_tokens/output_tokens，
// 与 Chat Completions 的 prompt_tokens/completion_tokens 不同。
//
//nolint:gochecknoglobals // 只读静态数据。
var monitorOpenAIResponsesUsagePaths = monitorUsagePaths{
	inputTokens:  "usage.input_tokens",
	outputTokens: "usage.output_tokens",
}

// monitorUsagePathsFor 按 provider + api_mode 选择用量 path。
func monitorUsagePathsFor(provider, apiMode string) (monitorUsagePaths, bool) {
	if provider == MonitorProviderOpenAI && defaultAPIMode(apiMode) == MonitorAPIModeResponses {
		return monitorOpenAIResponsesUsagePaths, true
	}
	paths, ok := monitorUsagePathTable[provider]
	return paths, ok
}

// extractMonitorUsage 从成功响应体里取输入/输出 token 数。
// 字段缺失、非数字或为负都返回 nil —— 上游没报用量不是错误，
// 只是这次探测拿不到这个指标。
func extractMonitorUsage(provider, apiMode, rawBody string) (inputTokens, outputTokens *int) {
	if rawBody == "" {
		return nil, nil
	}
	paths, ok := monitorUsagePathsFor(provider, apiMode)
	if !ok {
		return nil, nil
	}
	return extractMonitorTokenCount(rawBody, paths.inputTokens),
		extractMonitorTokenCount(rawBody, paths.outputTokens)
}

// extractMonitorTokenCount 读单个 token 计数。
// 用 Exists()+Type 判断而不是直接 Int()：gjson 对缺失字段返回 0，
// 会把「上游没报用量」误记成「用量为 0」。
func extractMonitorTokenCount(rawBody, path string) *int {
	if path == "" {
		return nil
	}
	result := gjson.Get(rawBody, path)
	if !result.Exists() || result.Type != gjson.Number {
		return nil
	}
	value := int(result.Int())
	if value < 0 {
		return nil
	}
	return &value
}
