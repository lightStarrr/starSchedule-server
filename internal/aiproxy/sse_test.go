package aiproxy

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRelayInjectsUsageWhenMissing(t *testing.T) {
	upstream := strings.NewReader(
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"你好\"}}]}\n\n" +
			"data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: [DONE]\n\n")

	rec := httptest.NewRecorder()
	res, err := Relay(context.Background(), rec, upstream,
		Meta{ID: "req-1", Model: "star", Alias: "star", RequestID: "req-1"}, 11, 4096)
	if err != nil {
		t.Fatalf("Relay 返回错误: %v", err)
	}
	body := rec.Body.String()

	if !strings.Contains(body, "event: meta") {
		t.Fatal("缺少 meta 帧")
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatal("缺少 [DONE]")
	}
	if !strings.Contains(body, `"usage"`) {
		t.Fatal("缺失 usage 时应注入估算帧")
	}
	if !res.Estimated {
		t.Fatal("应标记为估算")
	}
	if res.CompletionTokens <= 0 {
		t.Fatalf("补算的输出 token 不合理: %d", res.CompletionTokens)
	}
	// 注入帧必须出现在 [DONE] 之前，否则客户端会提前结束。
	if strings.Index(body, `"usage"`) > strings.Index(body, "data: [DONE]") {
		t.Fatal("usage 帧必须在 [DONE] 之前")
	}
}

func TestRelayUsesUpstreamUsage(t *testing.T) {
	upstream := strings.NewReader(
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n" +
			"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":34,\"total_tokens\":46}}\n\n" +
			"data: [DONE]\n\n")

	rec := httptest.NewRecorder()
	res, err := Relay(context.Background(), rec, upstream,
		Meta{ID: "r", Model: "m", Alias: "m", RequestID: "r"}, 11, 4096)
	if err != nil {
		t.Fatalf("Relay 返回错误: %v", err)
	}
	if res.Estimated {
		t.Fatal("上游已返回 usage，不应标记估算")
	}
	if res.PromptTokens != 12 || res.CompletionTokens != 34 {
		t.Fatalf("usage 解析错误: %+v", res)
	}
}

func TestRelayClientCancelReturnsQuietly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// 上游永远阻塞，模拟慢响应；客户端已断开时必须立即返回且不报错。
	pr, pw := io.Pipe()
	defer pw.Close()

	rec := httptest.NewRecorder()
	if _, err := Relay(ctx, rec, pr, Meta{}, 1, 10); err != nil {
		t.Fatalf("客户端断开不应返回错误: %v", err)
	}
}
