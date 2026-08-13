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
	// cachedInputTokens 缓存命中的输入 token path。
	//
	// 各家语义不同，这是最容易踩的坑：
	//   - OpenAI / Grok / Gemini 的输入字段是「含缓存的总输入」，要减掉这一项
	//     才是本次真正新提交的内容；
	//   - Anthropic 的 input_tokens 本身就已经排除缓存，另有独立的
	//     cache_read_input_tokens 字段，再减一次会算成负数。
	//
	// 空串表示该 provider 无需扣减。
	cachedInputTokens string
}

// monitorUsagePathTable 各 provider 非流式响应的用量字段位置。
// key 为 provider；OpenAI Responses 协议单列，见 monitorUsagePathsFor。
//
//nolint:gochecknoglobals // 只读静态表，初始化后不变更。
var monitorUsagePathTable = map[string]monitorUsagePaths{
	MonitorProviderOpenAI: {
		inputTokens:       "usage.prompt_tokens",
		outputTokens:      "usage.completion_tokens",
		cachedInputTokens: "usage.prompt_tokens_details.cached_tokens",
	},
	MonitorProviderGrok: {
		inputTokens:       "usage.prompt_tokens",
		outputTokens:      "usage.completion_tokens",
		cachedInputTokens: "usage.prompt_tokens_details.cached_tokens",
	},
	MonitorProviderAnthropic: {
		// input_tokens 已排除缓存，不能再减。
		inputTokens:  "usage.input_tokens",
		outputTokens: "usage.output_tokens",
	},
	MonitorProviderGemini: {
		inputTokens:       "usageMetadata.promptTokenCount",
		outputTokens:      "usageMetadata.candidatesTokenCount",
		cachedInputTokens: "usageMetadata.cachedContentTokenCount",
	},
}

// monitorOpenAIResponsesUsagePaths OpenAI Responses 协议用的是 input_tokens/output_tokens，
// 与 Chat Completions 的 prompt_tokens/completion_tokens 不同。
//
//nolint:gochecknoglobals // 只读静态数据。
var monitorOpenAIResponsesUsagePaths = monitorUsagePaths{
	inputTokens:       "usage.input_tokens",
	outputTokens:      "usage.output_tokens",
	cachedInputTokens: "usage.input_tokens_details.cached_tokens",
}

// monitorUsagePathsFor 按 provider + api_mode 选择用量 path。
func monitorUsagePathsFor(provider, apiMode string) (monitorUsagePaths, bool) {
	if provider == MonitorProviderOpenAI && defaultAPIMode(apiMode) == MonitorAPIModeResponses {
		return monitorOpenAIResponsesUsagePaths, true
	}
	paths, ok := monitorUsagePathTable[provider]
	return paths, ok
}

// extractMonitorUsage 从成功响应体里取用量。
//
// inputTokens 是「模型实际处理的总输入」，含缓存命中的部分。
// 刻意不在这里扣掉缓存：探测反复发送相同内容，缓存命中率会一路走高，
// 净输入随之趋近 0，既画不出有意义的曲线，也没法用于注水检测。
// 缓存量单独返回，需要与网关用量记录对账时用 input - cached。
//
// 字段缺失、非数字或为负都返回 nil —— 上游没报用量不是错误，
// 只是这次探测拿不到这个指标。
func extractMonitorUsage(provider, apiMode, rawBody string) (inputTokens, cachedInputTokens, outputTokens *int) {
	if rawBody == "" {
		return nil, nil, nil
	}
	paths, ok := monitorUsagePathsFor(provider, apiMode)
	if !ok {
		return nil, nil, nil
	}
	return extractMonitorTokenCount(rawBody, paths.inputTokens),
		extractMonitorTokenCount(rawBody, paths.cachedInputTokens),
		extractMonitorTokenCount(rawBody, paths.outputTokens)
}

// monitorNetInputTokens 净输入 = 总输入 - 缓存命中，即网关用量记录里的 input 口径。
//
// Anthropic 的 input_tokens 本身已排除缓存，其 cachedInputTokens 恒为 nil，
// 因此这里原样返回，不会重复扣减。
// 结果为负只可能是上游字段语义与预期不符，落成 0 让异常显形。
func monitorNetInputTokens(input, cached *int) *int {
	if input == nil || cached == nil {
		return input
	}
	net := *input - *cached
	if net < 0 {
		net = 0
	}
	return &net
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
