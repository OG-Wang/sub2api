package service

import "testing"

// TestMonitorExpectedInputTokensOpenAIChatIsExact 钉住 OpenAI Chat Completions
// 的真值：prompt 实测 50 token + cookbook 公式的 7 token 框架开销 = 57。
//
// 这个数一旦变了，要么是 challenge 模板被改了，要么是开销常数被动过，
// 两种情况都必须重新校准，不能默默放过。
func TestMonitorExpectedInputTokensOpenAIChatIsExact(t *testing.T) {
	challenge := generateChallenge()
	ref := monitorExpectedInputTokens(
		MonitorProviderOpenAI, MonitorAPIModeChatCompletions, "gpt-4o", challenge.Prompt,
	)

	if !ref.Exact {
		t.Errorf("OpenAI chat_completions 应当是精确参考值，实测 Exact=false")
	}
	if ref.PromptTokens != 50 {
		t.Errorf("prompt token 数 = %d, want 50（模板变了就要重新校准）", ref.PromptTokens)
	}
	if ref.FramingOverhead != monitorOpenAIChatFramingOverhead {
		t.Errorf("framing overhead = %d, want %d", ref.FramingOverhead, monitorOpenAIChatFramingOverhead)
	}
	if ref.Expected != 57 {
		t.Errorf("expected input tokens = %d, want 57", ref.Expected)
	}
}

// 非 OpenAI 上游本地没有对应 tokenizer，参考值只能当量级用，
// 必须标记为不精确，否则会按零容差误报。
func TestMonitorExpectedInputTokensNonOpenAIIsApproximate(t *testing.T) {
	challenge := generateChallenge()
	cases := []struct {
		provider string
		apiMode  string
		model    string
	}{
		{MonitorProviderAnthropic, MonitorAPIModeChatCompletions, "claude-sonnet-4"},
		{MonitorProviderGemini, MonitorAPIModeChatCompletions, "gemini-2.5-pro"},
		{MonitorProviderGrok, MonitorAPIModeChatCompletions, "grok-4.5"},
		// Responses 协议的计数公式没有公开定义，同样按不精确处理。
		{MonitorProviderOpenAI, MonitorAPIModeResponses, "gpt-4o"},
	}
	for _, tc := range cases {
		t.Run(tc.provider+"/"+tc.apiMode, func(t *testing.T) {
			ref := monitorExpectedInputTokens(tc.provider, tc.apiMode, tc.model, challenge.Prompt)
			if ref.Exact {
				t.Errorf("%s 本地无对应 tokenizer / 开销未知，Exact 必须为 false", tc.provider)
			}
			if ref.PromptTokens <= 0 {
				t.Errorf("即使不精确也应给出量级参考，实测 PromptTokens=%d", ref.PromptTokens)
			}
		})
	}
}

func TestEvaluateMonitorInputTokens(t *testing.T) {
	ptr := func(v int) *int { return &v }
	exactRef := monitorTokenReference{PromptTokens: 50, FramingOverhead: 7, Expected: 57, Exact: true}
	fuzzyRef := monitorTokenReference{PromptTokens: 50, Expected: 50, Exact: false}

	cases := []struct {
		name         string
		ref          monitorTokenReference
		actual       *int
		override     *int
		wantVerdict  bool
		wantInflated bool
		wantDeviated bool
	}{
		{"上游没报用量则不下结论", exactRef, nil, nil, false, false, false},
		{"精确匹配", exactRef, ptr(57), nil, true, false, false},
		{"精确参考下 2 token 内的偏差不算注水", exactRef, ptr(59), nil, true, false, false},
		{"精确参考下超出容差即判定", exactRef, ptr(60), nil, true, true, true},
		{"注入 system prompt", exactRef, ptr(420), nil, true, true, true},
		// 少报不是注水（不进告警），但确实和真值对不上，大厅要标出来。
		{"低于参考值不是注水但算偏离", exactRef, ptr(40), nil, true, false, true},
		{"少报但在容差内不算偏离", exactRef, ptr(55), nil, true, false, false},
		// 本地只有 OpenAI 系 tokenizer，对其他 provider 算出来的数只是量级参考。
		// 拿它判定会把每个这类渠道都误标成注水，所以一律不下结论。
		{"参考值不精确时不下结论", fuzzyRef, ptr(74), nil, false, false, false},
		{"参考值不精确时即使偏差很大也不下结论", fuzzyRef, ptr(500), nil, false, false, false},
		{"手填基线覆盖本地计算", fuzzyRef, ptr(300), ptr(295), true, false, false},
		{"手填基线下的注水", fuzzyRef, ptr(300), ptr(80), true, true, true},
		// 手填基线走宽容差，少报 20 还在容差里，不该报偏离。
		{"手填基线下的小幅少报不算偏离", fuzzyRef, ptr(280), ptr(300), true, false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := evaluateMonitorInputTokens(tc.ref, tc.actual, tc.override)
			if (v != nil) != tc.wantVerdict {
				t.Fatalf("verdict presence = %v, want %v", v != nil, tc.wantVerdict)
			}
			if v == nil {
				return
			}
			if v.Inflated != tc.wantInflated {
				t.Errorf("Inflated = %v, want %v (actual=%d expected=%d excess=%d)",
					v.Inflated, tc.wantInflated, v.Actual, v.Reference.Expected, v.ExcessTokens)
			}
			if v.Deviated != tc.wantDeviated {
				t.Errorf("Deviated = %v, want %v (actual=%d expected=%d excess=%d)",
					v.Deviated, tc.wantDeviated, v.Actual, v.Reference.Expected, v.ExcessTokens)
			}
		})
	}
}

// 注水不能改变可用性状态：渠道本身是通的，把它降级会让可用率曲线失真。
func TestAppendMonitorInputTokenWarningKeepsStatus(t *testing.T) {
	res := &CheckResult{
		Status:              MonitorStatusOperational,
		Message:             "",
		InputTokenWarning:   "input tokens inflated: upstream reported 420, expected ~57 (+363)",
		InputTokensInflated: true,
	}
	appendMonitorInputTokenWarning(res)

	if res.Status != MonitorStatusOperational {
		t.Errorf("status = %q, 注水不应改变可用性判定", res.Status)
	}
	if res.Message == "" {
		t.Error("告警必须并进 message 才能随历史记录落库")
	}

	// 已有 message 时应追加而不是覆盖，否则会吞掉降级原因。
	withMsg := &CheckResult{
		Status:            MonitorStatusDegraded,
		Message:           "slow response: 4200ms",
		InputTokenWarning: "input tokens inflated: upstream reported 420, expected ~57 (+363)",
	}
	appendMonitorInputTokenWarning(withMsg)
	if !contains(withMsg.Message, "slow response") || !contains(withMsg.Message, "inflated") {
		t.Errorf("message = %q, 应同时保留降级原因和注水告警", withMsg.Message)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
