package service

import (
	"bufio"
	"bytes"
	"io"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// 探测请求的流式（SSE）解析。
//
// 为什么要流式：非流式响应只能测到「整个请求耗时」，测不到首 token 到达时间（TTFT），
// 而 TTFT 才是用户实际感知的响应速度。
//
// 设计上刻意不把请求写死成流式，而是按响应的 Content-Type 分流：
//   - text/event-stream → 按 SSE 解析，取 TTFT / 增量文本 / 用量
//   - 其他              → 退回原来的整包 JSON 解析，TTFT 为空
//
// 这样三种情况都不会崩：上游忽略了 stream:true、用户用 body override 关掉了流式、
// 或者 provider 压根不支持流式。

// monitorStreamResult 一次流式响应的解析结果。
type monitorStreamResult struct {
	// TTFTMs 首个「有内容的」增量到达的耗时。
	// 用首个非空文本增量而不是首个事件：很多上游第一个 chunk 只带 role，
	// 拿它计时会把 TTFT 报得偏小。
	TTFTMs *int
	// Text 拼接后的完整回答文本，用于 challenge 校验。
	Text string
	// InputTokens / OutputTokens 上游在流中报告的用量。
	InputTokens  *int
	OutputTokens *int
}

// monitorStreamDecoder 描述某个 provider 的 SSE 事件如何解读。
// 三个函数都对「单个 data 事件的 JSON」求值，取不到就返回零值。
type monitorStreamDecoder struct {
	// textDelta 取该事件携带的增量文本。
	textDelta func(event gjson.Result) string
	// inputTokens / outputTokens 取该事件携带的用量（可能出现在不同事件里）。
	inputTokens  func(event gjson.Result) *int
	outputTokens func(event gjson.Result) *int
}

// monitorStreamDecoderFor 按 provider + api_mode 选择解码器。
// 第二个返回值为 false 表示该 provider 未接入流式解析。
func monitorStreamDecoderFor(provider, apiMode string) (monitorStreamDecoder, bool) {
	if provider == MonitorProviderOpenAI && defaultAPIMode(apiMode) == MonitorAPIModeResponses {
		return openAIResponsesStreamDecoder, true
	}
	decoder, ok := monitorStreamDecoders[provider]
	return decoder, ok
}

//nolint:gochecknoglobals // 只读静态表，初始化后不变更。
var monitorStreamDecoders = map[string]monitorStreamDecoder{
	MonitorProviderOpenAI:    openAIChatStreamDecoder,
	MonitorProviderGrok:      openAIChatStreamDecoder,
	MonitorProviderAnthropic: anthropicStreamDecoder,
	MonitorProviderGemini:    geminiStreamDecoder,
}

//nolint:gochecknoglobals // 只读静态数据。
var openAIChatStreamDecoder = monitorStreamDecoder{
	textDelta: func(e gjson.Result) string { return e.Get("choices.0.delta.content").String() },
	// OpenAI Chat 的用量只在最后一个 chunk 里，且必须请求时带
	// stream_options.include_usage=true，否则根本不返回（见 buildBody）。
	inputTokens:  func(e gjson.Result) *int { return streamTokenCount(e, "usage.prompt_tokens") },
	outputTokens: func(e gjson.Result) *int { return streamTokenCount(e, "usage.completion_tokens") },
}

//nolint:gochecknoglobals // 只读静态数据。
var openAIResponsesStreamDecoder = monitorStreamDecoder{
	textDelta: func(e gjson.Result) string {
		if e.Get("type").String() == "response.output_text.delta" {
			return e.Get("delta").String()
		}
		return ""
	},
	inputTokens:  func(e gjson.Result) *int { return streamTokenCount(e, "response.usage.input_tokens") },
	outputTokens: func(e gjson.Result) *int { return streamTokenCount(e, "response.usage.output_tokens") },
}

//nolint:gochecknoglobals // 只读静态数据。
var anthropicStreamDecoder = monitorStreamDecoder{
	textDelta: func(e gjson.Result) string {
		if e.Get("type").String() == "content_block_delta" {
			return e.Get("delta.text").String()
		}
		return ""
	},
	// Anthropic 把用量拆在两个事件里：输入在 message_start，输出在 message_delta。
	inputTokens:  func(e gjson.Result) *int { return streamTokenCount(e, "message.usage.input_tokens") },
	outputTokens: func(e gjson.Result) *int { return streamTokenCount(e, "usage.output_tokens") },
}

//nolint:gochecknoglobals // 只读静态数据。
var geminiStreamDecoder = monitorStreamDecoder{
	textDelta:   func(e gjson.Result) string { return e.Get("candidates.0.content.parts.0.text").String() },
	inputTokens: func(e gjson.Result) *int { return streamTokenCount(e, "usageMetadata.promptTokenCount") },
	// Gemini 每个 chunk 都带 usageMetadata，取最后一个即可（解析时后值覆盖前值）。
	outputTokens: func(e gjson.Result) *int { return streamTokenCount(e, "usageMetadata.candidatesTokenCount") },
}

// streamTokenCount 读取事件里的 token 计数。
// 与非流式一致：缺失/非数字返回 nil，不退化成 0。
func streamTokenCount(event gjson.Result, path string) *int {
	r := event.Get(path)
	if !r.Exists() || r.Type != gjson.Number {
		return nil
	}
	v := int(r.Int())
	if v < 0 {
		return nil
	}
	return &v
}

// sseDoneMarker OpenAI 系用 [DONE] 标记流结束，它不是 JSON。
const sseDoneMarker = "[DONE]"

// parseMonitorStream 逐行解析 SSE，返回 TTFT、拼接文本与用量。
//
// start 是请求发出的时间点，TTFT 相对它计算。
// body 读取受 maxBytes 限制，防止上游发无限流打爆内存。
//
// 同时返回原始字节：非 2xx 或解析不出内容时，调用方仍要拿它报告上游的真实回包。
func parseMonitorStream(
	body io.Reader,
	decoder monitorStreamDecoder,
	start time.Time,
	maxBytes int64,
) (*monitorStreamResult, []byte) {
	var raw bytes.Buffer
	limited := io.TeeReader(io.LimitReader(body, maxBytes), &raw)

	result := &monitorStreamResult{}
	var text strings.Builder

	scanner := bufio.NewScanner(limited)
	// SSE 单行可能较长（尤其带 usage 的收尾 chunk），放宽行缓冲上限。
	scanner.Buffer(make([]byte, 0, 8*1024), int(maxBytes))

	for scanner.Scan() {
		payload, ok := sseDataPayload(scanner.Text())
		if !ok {
			continue
		}
		event := gjson.Parse(payload)
		if !event.Exists() {
			continue
		}

		if delta := decoder.textDelta(event); delta != "" {
			if result.TTFTMs == nil {
				ms := int(time.Since(start) / time.Millisecond)
				result.TTFTMs = &ms
			}
			text.WriteString(delta)
		}
		if v := decoder.inputTokens(event); v != nil {
			result.InputTokens = v
		}
		if v := decoder.outputTokens(event); v != nil {
			result.OutputTokens = v
		}
	}

	result.Text = text.String()
	return result, raw.Bytes()
}

// sseDataPayload 从一行里取出 data: 后面的载荷。
// 返回 false 表示这行不是数据行（空行、event:/id: 等字段、或 [DONE]）。
func sseDataPayload(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || !strings.HasPrefix(trimmed, "data:") {
		return "", false
	}
	payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	if payload == "" || payload == sseDoneMarker {
		return "", false
	}
	return payload, true
}

// isSSEResponse 判断响应是否为 SSE。
// 只认 Content-Type，不猜内容：上游返回 JSON 错误体时也带 data: 开头的可能性极低，
// 但按 Content-Type 判断更不容易误伤。
func isSSEResponse(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "text/event-stream")
}
