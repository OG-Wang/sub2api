package service

import (
	"strings"
	"testing"
	"time"
)

// parseMonitorStream 的解析必须做到三件事：
//   1. 把散在多个 chunk 里的增量文本拼回完整答案（challenge 校验依赖它）
//   2. TTFT 取首个「有内容」的增量，而不是首个事件
//   3. 用量从流里正确取出
//
// 这三条任一错了，要么可用率全线误报 failed，要么 TTFT 偏小到没有意义。

func TestParseMonitorStreamOpenAIChat(t *testing.T) {
	// 答案 42 被拆成两个 chunk，第一个 chunk 只有 role 没有内容——
	// 这正是拿首个事件计时会翻车的形态。
	sse := strings.Join([]string{
		`data: {"choices":[{"delta":{"role":"assistant"}}]}`,
		``,
		`data: {"choices":[{"delta":{"content":"4"}}]}`,
		``,
		`data: {"choices":[{"delta":{"content":"2"}}]}`,
		``,
		`data: {"choices":[],"usage":{"prompt_tokens":57,"completion_tokens":1}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	decoder, ok := monitorStreamDecoderFor(MonitorProviderOpenAI, MonitorAPIModeChatCompletions)
	if !ok {
		t.Fatal("openai chat 应当有流式解码器")
	}
	result, raw := parseMonitorStream(strings.NewReader(sse), decoder, time.Now(), monitorResponseMaxBytes)

	if result.Text != "42" {
		t.Errorf("拼接文本 = %q, want %q", result.Text, "42")
	}
	if !validateChallenge(result.Text, "42") {
		t.Error("拼接后的文本必须能通过 challenge 校验，否则可用率会全线误报")
	}
	if result.TTFTMs == nil {
		t.Error("TTFT 不应为空")
	}
	if result.InputTokens == nil || *result.InputTokens != 57 {
		t.Errorf("input tokens = %v, want 57", result.InputTokens)
	}
	if result.OutputTokens == nil || *result.OutputTokens != 1 {
		t.Errorf("output tokens = %v, want 1", result.OutputTokens)
	}
	if len(raw) == 0 {
		t.Error("原始字节必须保留，错误路径要靠它报告上游回包")
	}
}

func TestParseMonitorStreamAnthropic(t *testing.T) {
	// Anthropic 把用量拆在两个事件里：输入在 message_start，输出在 message_delta。
	sse := strings.Join([]string{
		`data: {"type":"message_start","message":{"usage":{"input_tokens":50,"output_tokens":0}}}`,
		``,
		`data: {"type":"content_block_start","content_block":{"type":"text","text":""}}`,
		``,
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"4"}}`,
		``,
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"2"}}`,
		``,
		`data: {"type":"message_delta","usage":{"output_tokens":2}}`,
		``,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	decoder, ok := monitorStreamDecoderFor(MonitorProviderAnthropic, MonitorAPIModeChatCompletions)
	if !ok {
		t.Fatal("anthropic 应当有流式解码器")
	}
	result, _ := parseMonitorStream(strings.NewReader(sse), decoder, time.Now(), monitorResponseMaxBytes)

	if result.Text != "42" {
		t.Errorf("拼接文本 = %q, want %q", result.Text, "42")
	}
	if result.InputTokens == nil || *result.InputTokens != 50 {
		t.Errorf("input tokens = %v, want 50", result.InputTokens)
	}
	if result.OutputTokens == nil || *result.OutputTokens != 2 {
		t.Errorf("output tokens = %v, want 2", result.OutputTokens)
	}
}

func TestParseMonitorStreamOpenAIResponses(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1"}}`,
		``,
		`data: {"type":"response.output_text.delta","delta":"4"}`,
		``,
		`data: {"type":"response.output_text.delta","delta":"2"}`,
		``,
		`data: {"type":"response.completed","response":{"usage":{"input_tokens":61,"output_tokens":2}}}`,
		``,
	}, "\n")

	decoder, ok := monitorStreamDecoderFor(MonitorProviderOpenAI, MonitorAPIModeResponses)
	if !ok {
		t.Fatal("openai responses 应当有流式解码器")
	}
	result, _ := parseMonitorStream(strings.NewReader(sse), decoder, time.Now(), monitorResponseMaxBytes)

	if result.Text != "42" {
		t.Errorf("拼接文本 = %q, want %q", result.Text, "42")
	}
	if result.InputTokens == nil || *result.InputTokens != 61 {
		t.Errorf("input tokens = %v, want 61", result.InputTokens)
	}
}

// 首个 chunk 只带 role 时，TTFT 必须算到第一个真正有文本的 chunk。
// 用首个事件计时会把 TTFT 报成接近 0，指标直接失去意义。
func TestParseMonitorStreamTTFTSkipsEmptyDeltas(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"choices":[{"delta":{"role":"assistant"}}]}`,
		``,
		`data: {"choices":[{"delta":{"content":""}}]}`,
		``,
		`data: {"choices":[{"delta":{"content":"8"}}]}`,
		``,
	}, "\n")

	decoder, _ := monitorStreamDecoderFor(MonitorProviderOpenAI, MonitorAPIModeChatCompletions)
	// start 往前拨 200ms，模拟「请求已经发出去一段时间了」。
	start := time.Now().Add(-200 * time.Millisecond)
	result, _ := parseMonitorStream(strings.NewReader(sse), decoder, start, monitorResponseMaxBytes)

	if result.TTFTMs == nil {
		t.Fatal("TTFT 不应为空")
	}
	if *result.TTFTMs < 200 {
		t.Errorf("TTFT = %d ms，应当 >= 200ms（说明是从请求发出时刻算起，且跳过了空增量）", *result.TTFTMs)
	}
}

// 流里一个文本增量都没有时，TTFT 必须为空而不是 0。
// 0 会被读成「首 token 瞬间返回」，比没有数据更糟。
func TestParseMonitorStreamNoTextYieldsNilTTFT(t *testing.T) {
	sse := "data: {\"type\":\"ping\"}\n\ndata: [DONE]\n\n"
	decoder, _ := monitorStreamDecoderFor(MonitorProviderAnthropic, MonitorAPIModeChatCompletions)
	result, _ := parseMonitorStream(strings.NewReader(sse), decoder, time.Now(), monitorResponseMaxBytes)

	if result.TTFTMs != nil {
		t.Errorf("TTFT = %d, want nil（没有任何文本增量时不能填 0）", *result.TTFTMs)
	}
	if result.Text != "" {
		t.Errorf("Text = %q, want empty", result.Text)
	}
}

func TestSSEDataPayload(t *testing.T) {
	cases := []struct {
		line    string
		want    string
		wantOK  bool
		comment string
	}{
		{`data: {"a":1}`, `{"a":1}`, true, "标准数据行"},
		{`data:{"a":1}`, `{"a":1}`, true, "冒号后无空格"},
		{`data: [DONE]`, "", false, "结束标记不是 JSON"},
		{`event: message`, "", false, "非 data 字段"},
		{`id: 42`, "", false, "非 data 字段"},
		{``, "", false, "空行是事件分隔符"},
		{`data: `, "", false, "空载荷"},
		{`: keep-alive`, "", false, "注释行"},
	}
	for _, tc := range cases {
		t.Run(tc.comment, func(t *testing.T) {
			got, ok := sseDataPayload(tc.line)
			if ok != tc.wantOK || got != tc.want {
				t.Errorf("sseDataPayload(%q) = (%q, %v), want (%q, %v)", tc.line, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestIsSSEResponse(t *testing.T) {
	cases := []struct {
		contentType string
		want        bool
	}{
		{"text/event-stream", true},
		{"text/event-stream; charset=utf-8", true},
		{"Text/Event-Stream", true},
		{"application/json", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isSSEResponse(tc.contentType); got != tc.want {
			t.Errorf("isSSEResponse(%q) = %v, want %v", tc.contentType, got, tc.want)
		}
	}
}

// Gemini 的流式要换 :streamGenerateContent 端点，本次没做。
// 它应当继续走整包 JSON 路径——这里钉住这个事实，避免有人误以为已支持。
func TestGeminiKeepsNonStreamingBody(t *testing.T) {
	adapter, _, ok := providerAdapterFor(MonitorProviderGemini, MonitorAPIModeChatCompletions)
	if !ok {
		t.Fatal("gemini adapter 应当存在")
	}
	body, err := adapter.buildBody("gemini-2.5-pro", "Q: 12 + 13 = ?")
	if err != nil {
		t.Fatalf("buildBody failed: %v", err)
	}
	if strings.Contains(string(body), `"stream"`) {
		t.Errorf("gemini 请求体不应带 stream 字段（端点仍是 :generateContent）：%s", body)
	}
}

// OpenAI Chat 流式默认不返回 usage，必须显式带 stream_options。
// 漏了这个字段，注水检测直接失效且不会有任何报错。
func TestOpenAIChatBodyRequestsStreamUsage(t *testing.T) {
	adapter, _, ok := providerAdapterFor(MonitorProviderOpenAI, MonitorAPIModeChatCompletions)
	if !ok {
		t.Fatal("openai adapter 应当存在")
	}
	body, err := adapter.buildBody("gpt-4o", "Q: 12 + 13 = ?")
	if err != nil {
		t.Fatalf("buildBody failed: %v", err)
	}
	s := string(body)
	if !strings.Contains(s, `"stream":true`) {
		t.Errorf("请求体应开启流式：%s", s)
	}
	if !strings.Contains(s, `"include_usage":true`) {
		t.Errorf("请求体必须带 stream_options.include_usage，否则流式下拿不到 usage：%s", s)
	}
}
