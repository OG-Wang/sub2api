package service

import (
	"fmt"
)

// 探测请求的「应计输入 token」真值计算。
//
// 出发点：探测请求的字节内容完全由我们构造，每次只有两个两位数操作数和
// 一个运算符不同，因此输入长度恒定（见 channel_monitor_challenge_tokens_test.go
// 的实测：3200 种组合在 cl100k/o200k 下全部恰好 50 token）。
// 既然真值可算，就不该让管理员去猜一个数填进配置里。
//
// 真值 = prompt 文本 token 数（本地 tokenizer 精确计算）
//      + 协议框架开销（每种 provider/api_mode 一个常数）
//
// 精度是分级的，见 monitorTokenReference.Exact。

// monitorTokenReference 一次探测的输入 token 参考值。
type monitorTokenReference struct {
	// PromptTokens prompt 文本本身的 token 数。
	PromptTokens int
	// FramingOverhead 协议封装带来的固定开销（role 标记、消息边界、回复预热等）。
	FramingOverhead int
	// Expected = PromptTokens + FramingOverhead，即上游「应该」报出的数。
	Expected int
	// Exact 为 true 表示本地 tokenizer 与该上游一致、且框架开销有公开定义，
	// Expected 可以按接近零容差比对。
	// 为 false 表示 tokenizer 不同或开销未知（Anthropic/Gemini/Grok），
	// Expected 只能当量级参考，判定要放宽（见 monitorMaxPlausibleFramingOverhead）。
	Exact bool
}

// monitorOpenAIChatFramingOverhead OpenAI Chat Completions 的固定开销。
//
// 取自 OpenAI cookbook 的 num_tokens_from_messages 公式：
//   - 每条 message 3 个 token
//   - message 的每个字段值都参与编码，role="user" 是 1 个 token
//   - 整个请求末尾 3 个 token 用于预热 assistant 回复
//
// 探测请求只有一条 user message，所以 3 + 1 + 3 = 7。
const monitorOpenAIChatFramingOverhead = 7

// monitorMaxPlausibleFramingOverhead 未知协议下框架开销的合理上限。
//
// 各家封装方式不同，但都只是 role 标记、消息边界一类的元数据，
// 量级在几个到二十几个 token。超过这个数就不是封装开销，
// 而是有人往请求里塞了内容。
//
// 注意这个阈值只用于 Exact=false 的 provider；OpenAI 侧用精确值比对。
const monitorMaxPlausibleFramingOverhead = 25

// monitorInputTokenExactTolerance Exact=true 时允许的偏差。
//
// 留 2 个 token 而不是零：不同 API 版本对 role 字段、消息边界的计法
// 可能有 1-2 个 token 的出入，零容差会误报。注入一段 system prompt
// 至少是上百 token 的量级，2 个 token 的容差不会漏掉它。
const monitorInputTokenExactTolerance = 2

// monitorExpectedInputTokens 计算某次探测应计的输入 token 数。
//
// prompt 传入实际发送的 challenge 文本，保证参考值与真实请求一一对应，
// 不会因为模板改了而悄悄失配。
func monitorExpectedInputTokens(provider, apiMode, model, prompt string) monitorTokenReference {
	promptTokens, exactTokenizer := monitorLocalPromptTokens(provider, model, prompt)
	overhead, knownOverhead := monitorFramingOverhead(provider, apiMode)
	return monitorTokenReference{
		PromptTokens:    promptTokens,
		FramingOverhead: overhead,
		Expected:        promptTokens + overhead,
		Exact:           exactTokenizer && knownOverhead,
	}
}

// monitorLocalPromptTokens 本地计算 prompt 的 token 数。
//
// 第二个返回值表示本地 tokenizer 是否与该上游真实使用的一致：
// 只有 OpenAI 系模型成立。Grok 走 OpenAI 兼容协议但用的是 xAI 自己的
// tokenizer，Anthropic / Gemini 的 tokenizer 均未公开，
// 这些情况下返回的数只是量级参考。
func monitorLocalPromptTokens(provider, model, prompt string) (tokens int, exact bool) {
	codec, err := openAIInputTokensCodecForModel(model)
	if err != nil {
		return 0, false
	}
	count, err := codec.Count(prompt)
	if err != nil {
		return 0, false
	}
	return count, provider == MonitorProviderOpenAI
}

// monitorFramingOverhead 返回该协议的固定封装开销。
// 第二个返回值为 false 表示开销未知，此时按 0 计，判定改用宽阈值。
func monitorFramingOverhead(provider, apiMode string) (overhead int, known bool) {
	if provider == MonitorProviderOpenAI && defaultAPIMode(apiMode) == MonitorAPIModeChatCompletions {
		return monitorOpenAIChatFramingOverhead, true
	}
	// OpenAI Responses 的封装方式（instructions + input）没有公开的计数公式；
	// Anthropic / Gemini / Grok 同理。都按未知处理。
	return 0, false
}

// monitorInputTokenVerdict 一次输入 token 比对的结论。
type monitorInputTokenVerdict struct {
	// Reference 本次比对使用的参考值。
	Reference monitorTokenReference
	// Actual 上游报告的输入 token 数。
	Actual int
	// Inflated 上游报数明显超出参考值，说明请求里被塞了额外内容。
	Inflated bool
	// ExcessTokens 超出参考值的 token 数，Inflated 为 true 时才有意义。
	ExcessTokens int
}

// evaluateMonitorInputTokens 比对上游报告的输入 token 与本地算出的真值。
//
// override 非空时（管理员在配置页手填了「期望输入 Token」）以它为准，
// 用于本地计算不适用的特殊上游。
//
// actual 为 nil（上游没报 usage）时返回 nil —— 没有依据就不下结论。
//
// 只判「超出」方向：注入内容只会让输入变多。低于参考值可能是上游做了
// 缓存或压缩，不是欺诈信号。
//
// 能力边界：只能抓「注入 system prompt / 虚报输入用量」。
// 上游虚报 output token、私调倍率、降智换小模型都抓不到。
func evaluateMonitorInputTokens(ref monitorTokenReference, actual, override *int) *monitorInputTokenVerdict {
	if actual == nil {
		return nil
	}
	effective := ref
	if override != nil && *override > 0 {
		// 手填基线一律按「近似」处理：管理员填的是他从实测值里观察出来的
		// 大致数字，不可能比本地计算更精确。按精确值零容差比对只会误报。
		effective = monitorTokenReference{
			PromptTokens: *override,
			Expected:     *override,
			Exact:        false,
		}
	}
	if effective.Expected <= 0 {
		return nil
	}

	tolerance := monitorMaxPlausibleFramingOverhead
	if effective.Exact {
		tolerance = monitorInputTokenExactTolerance
	}

	excess := *actual - effective.Expected
	return &monitorInputTokenVerdict{
		Reference:    effective,
		Actual:       *actual,
		Inflated:     excess > tolerance,
		ExcessTokens: excess,
	}
}

// monitorInputTokenWarning 生成写进检测记录的告警文案。
// 未注水时返回空串。
func monitorInputTokenWarning(v *monitorInputTokenVerdict) string {
	if v == nil || !v.Inflated {
		return ""
	}
	return fmt.Sprintf("input tokens inflated: upstream reported %d, expected ~%d (+%d)",
		v.Actual, v.Reference.Expected, v.ExcessTokens)
}
