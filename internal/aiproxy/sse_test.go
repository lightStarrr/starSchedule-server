package aiproxy

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
	if !strings.HasSuffix(body, "data: [DONE]\n\n") {
		t.Fatal("[DONE] 必须是以空行结束的完整 SSE 事件")
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

func TestRelayClientCancelReturnsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// 客户端已断开时立即返回取消原因，供调用方记录中断状态。
	pr, pw := io.Pipe()
	defer pw.Close()

	rec := httptest.NewRecorder()
	if _, err := Relay(ctx, rec, pr, Meta{}, 1, 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("应返回客户端取消，实际: %v", err)
	}
}

func TestRelaySettlesPartialOutput(t *testing.T) {
	for _, limit := range []int{2, 4096} {
		res, err := Relay(context.Background(), httptest.NewRecorder(),
			strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"你好世界\"}}]}\n\n"),
			Meta{}, 11, limit)
		if err == nil {
			t.Fatal("缺少 DONE 应返回中断错误")
		}
		if res.PromptTokens != 11 || res.CompletionTokens != min(4, limit) || !res.Estimated {
			t.Fatalf("中断后应估算已生成内容且不超过输出上限: %+v", res)
		}
	}
}

func TestRelayPreservesReportedZeroUsage(t *testing.T) {
	res, err := Relay(context.Background(), httptest.NewRecorder(), strings.NewReader(
		"data: {\"choices\":[{\"delta\":{\"content\":\"cached\"}}]}\n\n"+
			"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":0,\"completion_tokens\":0,\"total_tokens\":0}}\n\n"+
			"data: [DONE]\n\n"), Meta{}, 11, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if res.Estimated || res.PromptTokens != 0 || res.CompletionTokens != 0 {
		t.Fatalf("应保留上游明确报告的零用量: %+v", res)
	}
}

type cancelAfterContentWriter struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
}

func (w cancelAfterContentWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseRecorder.Write(p)
	if strings.Contains(string(p), `"content"`) {
		w.cancel()
	}
	return n, err
}

func (w cancelAfterContentWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func TestRelaySettlesOutputOnClientCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := cancelAfterContentWriter{httptest.NewRecorder(), cancel}
	res, err := Relay(ctx, w, strings.NewReader(
		"data: {\"choices\":[{\"delta\":{\"content\":\"你好\"}}]}\n\n"), Meta{}, 11, 4096)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("应记录客户端取消，实际: %v", err)
	}
	if res.CompletionTokens != 2 || !res.Estimated {
		t.Fatalf("取消后仍需结算已生成的回答: %+v", res)
	}
}

func TestRelayDeadlineSettlesPartialOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	pr, pw := io.Pipe()
	defer pr.Close()
	defer pw.Close()
	upstream := io.MultiReader(strings.NewReader(
		"data: {\"choices\":[{\"delta\":{\"content\":\"你好\"}}]}\n\n"), pr)
	rec := httptest.NewRecorder()
	res, err := Relay(ctx, rec, upstream, Meta{}, 11, 4096)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("应报告整体超时，实际 %v", err)
	}
	if res.CompletionTokens != 2 || !res.Estimated || res.TotalMS <= 0 {
		t.Fatalf("整体超时也应记录已生成内容和耗时: %+v", res)
	}
	if !strings.Contains(rec.Body.String(), "UPSTREAM_TIMEOUT") {
		t.Fatal("应向客户端发送超时事件")
	}
}
