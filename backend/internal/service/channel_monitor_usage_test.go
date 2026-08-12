package service

import "testing"

func TestExtractMonitorUsagePerProvider(t *testing.T) {
	cases := []struct {
		name       string
		provider   string
		apiMode    string
		body       string
		wantInput  int
		wantOutput int
	}{
		{
			name:       "anthropic messages",
			provider:   MonitorProviderAnthropic,
			apiMode:    MonitorAPIModeChatCompletions,
			body:       `{"usage":{"input_tokens":76,"output_tokens":20}}`,
			wantInput:  76,
			wantOutput: 20,
		},
		{
			name:       "openai chat completions",
			provider:   MonitorProviderOpenAI,
			apiMode:    MonitorAPIModeChatCompletions,
			body:       `{"usage":{"prompt_tokens":81,"completion_tokens":3}}`,
			wantInput:  81,
			wantOutput: 3,
		},
		{
			name:       "openai responses uses input/output naming",
			provider:   MonitorProviderOpenAI,
			apiMode:    MonitorAPIModeResponses,
			body:       `{"usage":{"input_tokens":90,"output_tokens":5}}`,
			wantInput:  90,
			wantOutput: 5,
		},
		{
			name:       "grok is openai compatible",
			provider:   MonitorProviderGrok,
			apiMode:    MonitorAPIModeChatCompletions,
			body:       `{"usage":{"prompt_tokens":70,"completion_tokens":2}}`,
			wantInput:  70,
			wantOutput: 2,
		},
		{
			name:       "gemini usage metadata",
			provider:   MonitorProviderGemini,
			apiMode:    MonitorAPIModeChatCompletions,
			body:       `{"usageMetadata":{"promptTokenCount":64,"candidatesTokenCount":4}}`,
			wantInput:  64,
			wantOutput: 4,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in, out := extractMonitorUsage(tc.provider, tc.apiMode, tc.body)
			if in == nil || *in != tc.wantInput {
				t.Errorf("input tokens = %v, want %d", in, tc.wantInput)
			}
			if out == nil || *out != tc.wantOutput {
				t.Errorf("output tokens = %v, want %d", out, tc.wantOutput)
			}
		})
	}
}

// 缺失用量必须返回 nil 而不是 0：0 会被当成「这次探测输入了 0 个 token」，
// 污染注水检测的基线。
func TestExtractMonitorUsageMissingReturnsNil(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"no usage object", `{"content":[{"type":"text","text":"8"}]}`},
		{"usage present but empty", `{"usage":{}}`},
		{"non-numeric value", `{"usage":{"input_tokens":"76"}}`},
		{"empty body", ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in, out := extractMonitorUsage(MonitorProviderAnthropic, MonitorAPIModeChatCompletions, tc.body)
			if in != nil {
				t.Errorf("input tokens = %d, want nil", *in)
			}
			if out != nil {
				t.Errorf("output tokens = %d, want nil", *out)
			}
		})
	}
}

func TestExtractMonitorUsageUnknownProvider(t *testing.T) {
	in, out := extractMonitorUsage("unknown", MonitorAPIModeChatCompletions, `{"usage":{"input_tokens":5}}`)
	if in != nil || out != nil {
		t.Errorf("unknown provider should yield nil usage, got in=%v out=%v", in, out)
	}
}

func TestEvaluateMonitorTokenInflation(t *testing.T) {
	ptr := func(v int) *int { return &v }

	cases := []struct {
		name     string
		expected *int
		actual   *int
		want     bool
	}{
		{"no baseline configured", nil, ptr(500), false},
		{"upstream reported no usage", ptr(76), nil, false},
		{"exact match", ptr(76), ptr(76), false},
		{"within tolerance", ptr(76), ptr(88), false},
		{"just above tolerance", ptr(76), ptr(92), true},
		{"system prompt injected", ptr(76), ptr(310), true},
		{"below baseline is not inflation", ptr(76), ptr(40), false},
		{"invalid baseline", ptr(0), ptr(999), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := evaluateMonitorTokenInflation(tc.expected, tc.actual); got != tc.want {
				t.Errorf("evaluateMonitorTokenInflation() = %v, want %v", got, tc.want)
			}
		})
	}
}

// challenge 操作数必须全是两位数，prompt 的 token 数才恒定，
// 注水检测的基线比对才成立。
func TestGenerateChallengeUsesTwoDigitOperands(t *testing.T) {
	if monitorChallengeMin < 10 || monitorChallengeMax > 99 {
		t.Fatalf("challenge operand range [%d, %d] must stay within two digits",
			monitorChallengeMin, monitorChallengeMax)
	}
	for i := 0; i < 200; i++ {
		c := generateChallenge()
		if !validateChallenge(c.Expected, c.Expected) {
			t.Fatalf("challenge %+v should validate against its own expected answer", c)
		}
		for _, n := range monitorChallengeNumberRegex.FindAllString(c.Prompt, -1) {
			// few-shot 模板里的示例数字（3/5/8/12/7）不受操作数范围约束，
			// 这里只断言不会出现三位数——那说明范围被改坏了。
			if len(n) > 2 {
				t.Fatalf("prompt contains %q, operands must stay two-digit: %s", n, c.Prompt)
			}
		}
	}
}
