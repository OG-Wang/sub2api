package service

import (
	"strings"
	"testing"
	"time"
)

// 上游报的输入 token 分两种语义，取错会让注水检测全线误报：
//   - OpenAI / Grok / Gemini：输入字段是「含缓存的总输入」，要扣掉缓存命中
//   - Anthropic：输入字段本身已排除缓存，再扣一次会算成负数
//
// 本文件的用例取自 ricktoken 生产站的真实抓包，数值不是编的。

// grokRealUsageChunk 2026-08-13 从 ricktoken 抓到的真实收尾 chunk。
// prompt_tokens=257 含 192 个缓存命中，净输入 65——与网关用量记录一致。
const grokRealUsageChunk = `{"id":"397f6212","object":"chat.completion.chunk","model":"grok-4.5",` +
	`"choices":[],"usage":{"prompt_tokens":257,"completion_tokens":1,"total_tokens":2555,` +
	`"prompt_tokens_details":{"text_tokens":257,"audio_tokens":0,"image_tokens":0,"cached_tokens":192},` +
	`"completion_tokens_details":{"reasoning_tokens":2297}}}`

func TestStreamUsageSplitsCachedInputGrok(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"content":"56","role":"assistant"}}]}`,
		``,
		"data: " + grokRealUsageChunk,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	decoder, ok := monitorStreamDecoderFor(MonitorProviderGrok, MonitorAPIModeChatCompletions)
	if !ok {
		t.Fatal("grok 应当有流式解码器")
	}
	result, _ := parseMonitorStream(strings.NewReader(sse), decoder, time.Now(), monitorResponseMaxBytes)

	if result.InputTokens == nil || *result.InputTokens != 257 {
		t.Errorf("input tokens = %v, want 257（总输入，含缓存）", result.InputTokens)
	}
	if result.CachedInputTokens == nil || *result.CachedInputTokens != 192 {
		t.Errorf("cached input tokens = %v, want 192", result.CachedInputTokens)
	}
	// 与网关用量记录对账用的是净输入。
	if net := monitorNetInputTokens(result.InputTokens, result.CachedInputTokens); net == nil || *net != 65 {
		t.Errorf("净输入 = %v, want 65（与用量记录一致）", net)
	}
	if result.OutputTokens == nil || *result.OutputTokens != 1 {
		t.Errorf("output tokens = %v, want 1", result.OutputTokens)
	}
}

// 非流式路径必须与流式得出同一个数，否则同一个渠道换个响应形态数字就会跳变。
func TestNonStreamUsageSplitsCachedInputGrok(t *testing.T) {
	in, cached, out := extractMonitorUsage(MonitorProviderGrok, MonitorAPIModeChatCompletions, grokRealUsageChunk)
	if in == nil || *in != 257 {
		t.Errorf("input tokens = %v, want 257", in)
	}
	if cached == nil || *cached != 192 {
		t.Errorf("cached = %v, want 192", cached)
	}
	if net := monitorNetInputTokens(in, cached); net == nil || *net != 65 {
		t.Errorf("净输入 = %v, want 65", net)
	}
	if out == nil || *out != 1 {
		t.Errorf("output tokens = %v, want 1", out)
	}
}

// Anthropic 的 input_tokens 已排除缓存，绝不能再减——减了会得到负数或 0。
func TestAnthropicInputTokensNotDoubleSubtracted(t *testing.T) {
	body := `{"usage":{"input_tokens":65,"output_tokens":2,"cache_read_input_tokens":192,` +
		`"cache_creation_input_tokens":0}}`

	in, cached, _ := extractMonitorUsage(MonitorProviderAnthropic, MonitorAPIModeChatCompletions, body)
	if in == nil || *in != 65 {
		t.Errorf("input tokens = %v, want 65（Anthropic 的 input_tokens 已排除缓存）", in)
	}
	// 未配置缓存 path，净输入必须等于原值，绝不能重复扣减。
	if cached != nil {
		t.Errorf("Anthropic 不应报缓存 path，得到 %d", *cached)
	}
	if net := monitorNetInputTokens(in, cached); net == nil || *net != 65 {
		t.Errorf("净输入 = %v, want 65（不做二次扣减）", net)
	}

	sse := "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":65," +
		"\"cache_read_input_tokens\":192}}}\n\n" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"56\"}}\n\n"
	decoder, _ := monitorStreamDecoderFor(MonitorProviderAnthropic, MonitorAPIModeChatCompletions)
	result, _ := parseMonitorStream(strings.NewReader(sse), decoder, time.Now(), monitorResponseMaxBytes)
	if result.InputTokens == nil || *result.InputTokens != 65 {
		t.Errorf("流式 input tokens = %v, want 65", result.InputTokens)
	}
}

func TestGeminiSubtractsCachedContent(t *testing.T) {
	body := `{"usageMetadata":{"promptTokenCount":257,"candidatesTokenCount":4,"cachedContentTokenCount":192}}`
	in, cached, out := extractMonitorUsage(MonitorProviderGemini, MonitorAPIModeChatCompletions, body)
	if in == nil || *in != 257 {
		t.Errorf("input tokens = %v, want 257", in)
	}
	if net := monitorNetInputTokens(in, cached); net == nil || *net != 65 {
		t.Errorf("净输入 = %v, want 65", net)
	}
	if out == nil || *out != 4 {
		t.Errorf("output tokens = %v, want 4", out)
	}
}

// 上游没报缓存字段时保持原值，不能把 nil 当成 0 去减。
func TestUsageWithoutCacheFieldKeepsRawInput(t *testing.T) {
	body := `{"usage":{"prompt_tokens":65,"completion_tokens":1}}`
	in, _, _ := extractMonitorUsage(MonitorProviderOpenAI, MonitorAPIModeChatCompletions, body)
	if in == nil || *in != 65 {
		t.Errorf("input tokens = %v, want 65", in)
	}
}

func TestMonitorNetInputTokensEdgeCases(t *testing.T) {
	ptr := func(v int) *int { return &v }

	if got := monitorNetInputTokens(nil, ptr(10)); got != nil {
		t.Errorf("无输入时应返回 nil，得到 %d", *got)
	}
	if got := monitorNetInputTokens(ptr(65), nil); got == nil || *got != 65 {
		t.Errorf("无缓存字段时应原样返回 65，得到 %v", got)
	}
	// 缓存大于总输入只可能是上游字段语义与预期不符。
	// 落成 0 让异常显形，好过把负数写进库。
	if got := monitorNetInputTokens(ptr(10), ptr(99)); got == nil || *got != 0 {
		t.Errorf("缓存大于输入时应落成 0，得到 %v", got)
	}
}
