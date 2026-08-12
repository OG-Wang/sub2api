package service

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/tiktoken-go/tokenizer"
)

// TestChallengePromptTokenCountIsConstant 实测：固定两位数操作数后，
// challenge prompt 在 OpenAI 系 tokenizer 下的 token 数是否真的恒定。
//
// 这是注水检测能否成立的前提。若同一 tokenizer 下不同操作数产生不同
// token 数，基线比对就必须留容差；若恒定，则可以零容差告警。
func TestChallengePromptTokenCountIsConstant(t *testing.T) {
	encodings := []struct {
		name string
		enc  tokenizer.Encoding
	}{
		{"cl100k_base", tokenizer.Cl100kBase},
		{"o200k_base", tokenizer.O200kBase},
	}

	for _, e := range encodings {
		t.Run(e.name, func(t *testing.T) {
			codec, err := tokenizer.Get(e.enc)
			if err != nil {
				t.Skipf("codec %s unavailable: %v", e.name, err)
			}

			counts := map[int][]string{}
			for a := monitorChallengeMin; a <= monitorChallengeMax; a++ {
				for b := monitorChallengeMin; b <= monitorChallengeMax; b++ {
					for _, op := range []string{"+", "-"} {
						// 减法分支实际渲染的是 hi - lo，这里两个方向都测，
						// 覆盖面只会更大。
						prompt := fmt.Sprintf(monitorChallengePromptTemplate, a, op, b)
						ids, _, err := codec.Encode(prompt)
						if err != nil {
							t.Fatalf("encode failed: %v", err)
						}
						n := len(ids)
						if len(counts[n]) < 3 {
							counts[n] = append(counts[n], fmt.Sprintf("%d %s %d", a, op, b))
						}
					}
				}
			}

			if len(counts) == 1 {
				for n := range counts {
					t.Logf("%s: 全部 %d 种组合的 prompt 都是 %d 个 token（恒定）",
						e.name, 2*(monitorChallengeMax-monitorChallengeMin+1)*(monitorChallengeMax-monitorChallengeMin+1), n)
				}
				return
			}

			// 不恒定：把分布打出来，用于确定容差该留多大。
			minN, maxN := -1, -1
			for n := range counts {
				if minN == -1 || n < minN {
					minN = n
				}
				if n > maxN {
					maxN = n
				}
			}
			t.Errorf("%s: prompt token 数不恒定，出现 %d 种取值 [%d, %d]，样例：%v",
				e.name, len(counts), minN, maxN, counts)
		})
	}
}

// TestChallengeOperandRangeMustBeTwoDigit 记录「为什么操作数范围固定为 10-49」。
//
// 注意：对 OpenAI 系 tokenizer（cl100k / o200k）而言这个改动是**不必要**的——
// 它们把 1-3 位数字都编成单个 token，所以旧的 1-50 范围同样恒定。
//
// 真正需要防的是**逐位切分**数字的 tokenizer（部分 SentencePiece 实现，
// 如 Gemini 系）：那里 "7" 是 1 个 token 而 "42" 是 2 个，
// 操作数位数不定就会让 prompt 的 token 数随机波动，基线比对失效。
// 本地没有这类 tokenizer 可测，所以用一个逐位切分的模型来体现差异。
func TestChallengeOperandRangeMustBeTwoDigit(t *testing.T) {
	// digitSplitTokenCount 模拟逐位切分：数字部分按字符计，其余部分恒定，
	// 因此只需数操作数的总位数即可反映 token 数的波动。
	digitSplitTokenCount := func(a, b int) int {
		return len(strconv.Itoa(a)) + len(strconv.Itoa(b))
	}

	distinct := func(lo, hi int) map[int]struct{} {
		seen := map[int]struct{}{}
		for a := lo; a <= hi; a++ {
			for b := lo; b <= hi; b++ {
				seen[digitSplitTokenCount(a, b)] = struct{}{}
			}
		}
		return seen
	}

	if legacy := distinct(1, 50); len(legacy) < 2 {
		t.Errorf("逐位切分下旧范围 1-50 应当产生多种 token 数，实测 %v", legacy)
	} else {
		t.Logf("逐位切分下旧范围 1-50 产生 %d 种 token 数，无法用于基线比对", len(legacy))
	}

	if current := distinct(monitorChallengeMin, monitorChallengeMax); len(current) != 1 {
		t.Errorf("当前范围 %d-%d 在逐位切分下也必须恒定，实测 %v",
			monitorChallengeMin, monitorChallengeMax, current)
	}

	// 顺带钉住范围本身：一旦有人把边界改出两位数区间，这里立刻失败。
	if monitorChallengeMin < 10 || monitorChallengeMax > 99 {
		t.Fatalf("操作数范围 [%d, %d] 必须全部是两位数", monitorChallengeMin, monitorChallengeMax)
	}
}
