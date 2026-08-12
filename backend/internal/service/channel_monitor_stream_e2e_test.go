//go:build unit

package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// runCheckForModel 的端到端验证：起一个真的 HTTP 服务端，
// 覆盖三条必须都能走通的路径。
//
// 单测里最容易被漏掉的是「上游忽略 stream:true 退回整包 JSON」——
// 那条路径一旦崩掉，所有不支持流式的中转站会被全部误判成 failed。

// challengeAnswerFromPrompt 从探测 prompt 里算出正确答案，
// 让假上游能像真上游一样回答，从而真正走通 challenge 校验。
func challengeAnswerFromPrompt(t *testing.T, prompt string) string {
	t.Helper()
	// 模板最后一行形如 "Q: 34 + 21 = ?"，取最后一组三个数字里的前两个。
	nums := monitorChallengeNumberRegex.FindAllString(prompt, -1)
	if len(nums) < 2 {
		t.Fatalf("无法从 prompt 里解析操作数: %s", prompt)
	}
	a, b := nums[len(nums)-2], nums[len(nums)-1]
	var x, y int
	if _, err := fmt.Sscanf(a, "%d", &x); err != nil {
		t.Fatalf("parse operand: %v", err)
	}
	if _, err := fmt.Sscanf(b, "%d", &y); err != nil {
		t.Fatalf("parse operand: %v", err)
	}
	if strings.Contains(prompt, fmt.Sprintf("%s - %s", a, b)) {
		return fmt.Sprint(x - y)
	}
	return fmt.Sprint(x + y)
}

func readPromptFromChatBody(t *testing.T, r *http.Request) string {
	t.Helper()
	buf := make([]byte, 8192)
	n, _ := r.Body.Read(buf)
	body := string(buf[:n])
	// 粗略取出 content 字段即可：这里只需要拿到 prompt 文本算答案。
	idx := strings.Index(body, `"content":"`)
	if idx < 0 {
		t.Fatalf("请求体里没有 content 字段: %s", body)
	}
	rest := body[idx+len(`"content":"`):]
	end := strings.Index(rest, `","`)
	if end < 0 {
		end = strings.Index(rest, `"}`)
	}
	if end < 0 {
		t.Fatalf("无法截取 content: %s", body)
	}
	return strings.ReplaceAll(rest[:end], `\n`, "\n")
}

// 流式上游：TTFT 与用量都应从流里取到，challenge 校验基于拼接文本通过。
func TestRunCheckForModelStreamingUpstream(t *testing.T) {
	swapMonitorHTTPClient(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		answer := challengeAnswerFromPrompt(t, readPromptFromChatBody(t, r))

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)

		// 先发一个只有 role 的 chunk，再隔一会儿发内容——
		// 这样 TTFT 才有可测量的值。
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		time.Sleep(30 * time.Millisecond)

		// 答案逐字符发出，验证跨 chunk 拼接。
		for _, ch := range answer {
			_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"%c\"}}]}\n\n", ch)
			if flusher != nil {
				flusher.Flush()
			}
		}
		_, _ = fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":57,\"completion_tokens\":2}}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer srv.Close()

	res := runCheckForModel(context.Background(), MonitorProviderOpenAI, srv.URL, "sk-test", "gpt-4o", nil)

	if res.Status != MonitorStatusOperational {
		t.Fatalf("status = %q (%s), want operational —— 流式下 challenge 校验必须通过",
			res.Status, res.Message)
	}
	if res.TTFTMs == nil {
		t.Fatal("流式响应必须测到 TTFT")
	}
	if *res.TTFTMs < 25 {
		t.Errorf("TTFT = %d ms，服务端刻意延迟了 30ms，说明计时点不对", *res.TTFTMs)
	}
	if res.InputTokens == nil || *res.InputTokens != 57 {
		t.Errorf("input tokens = %v, want 57", res.InputTokens)
	}
	if res.OutputTokens == nil || *res.OutputTokens != 2 {
		t.Errorf("output tokens = %v, want 2", res.OutputTokens)
	}
}

// 上游忽略 stream:true，退回整包 JSON：必须仍然判定为可用，
// 用量走非流式解析，TTFT 为空。
func TestRunCheckForModelNonStreamingUpstreamStillWorks(t *testing.T) {
	swapMonitorHTTPClient(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		answer := challengeAnswerFromPrompt(t, readPromptFromChatBody(t, r))
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w,
			`{"choices":[{"message":{"content":%q}}],"usage":{"prompt_tokens":57,"completion_tokens":2}}`,
			answer)
	}))
	defer srv.Close()

	res := runCheckForModel(context.Background(), MonitorProviderOpenAI, srv.URL, "sk-test", "gpt-4o", nil)

	if res.Status != MonitorStatusOperational {
		t.Fatalf("status = %q (%s), want operational —— 不支持流式的上游不能被误判",
			res.Status, res.Message)
	}
	if res.TTFTMs != nil {
		t.Errorf("非流式响应不应有 TTFT，实测 %d", *res.TTFTMs)
	}
	if res.InputTokens == nil || *res.InputTokens != 57 {
		t.Errorf("input tokens = %v, want 57（非流式路径的用量解析）", res.InputTokens)
	}
}

// 答案错误时必须判 failed，且错误信息里带上实际拿到的文本。
// 这条防止「流式解析把文本吃掉了，于是永远校验不过」被静默忽略。
func TestRunCheckForModelStreamingWrongAnswerFails(t *testing.T) {
	swapMonitorHTTPClient(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"9999\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	res := runCheckForModel(context.Background(), MonitorProviderOpenAI, srv.URL, "sk-test", "gpt-4o", nil)

	if res.Status != MonitorStatusFailed {
		t.Errorf("status = %q, want failed", res.Status)
	}
	if !strings.Contains(res.Message, "9999") {
		t.Errorf("错误信息应包含实际响应文本，便于排查：%s", res.Message)
	}
}

// 上游返回非 2xx 时，错误信息必须保留上游的真实回包。
func TestRunCheckForModelUpstreamErrorKeepsBody(t *testing.T) {
	swapMonitorHTTPClient(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprint(w, `{"error":{"message":"No available accounts"}}`)
	}))
	defer srv.Close()

	res := runCheckForModel(context.Background(), MonitorProviderOpenAI, srv.URL, "sk-test", "gpt-4o", nil)

	if res.Status != MonitorStatusError {
		t.Errorf("status = %q, want error", res.Status)
	}
	if !strings.Contains(res.Message, "No available accounts") {
		t.Errorf("上游错误详情丢失了：%s", res.Message)
	}
}
