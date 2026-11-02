package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/starschedule/server/internal/aiproxy"
	"github.com/starschedule/server/internal/model"
)

func TestDetectReasoningEfforts(t *testing.T) {
	for _, tc := range []struct {
		name, mode, baseline string
		accepted             []string
		failureStatus        int
	}{
		{name: "仅支持部分档位", mode: "effort", accepted: []string{"medium", "high"}},
		{name: "支持全部档位", mode: "effort", accepted: []string{"minimal", "low", "medium", "high"}},
		{name: "不支持强度参数", mode: "none", baseline: `{"choices":[]}`},
		{name: "无参数仍输出思考", mode: "fixed", baseline: `{"choices":[{"message":{"reasoning_content":"thinking"}}]}`},
		{name: "无参数包含思考用量", mode: "fixed", baseline: `{"choices":[],"usage":{"completion_tokens_details":{"reasoning_tokens":3}}}`},
		{name: "基线也不可用", mode: "unknown"},
		{name: "无效基线响应", mode: "unknown", baseline: `invalid`},
		{name: "基线缺少聊天结果", mode: "unknown", baseline: `{}`},
		{name: "限流不等于不支持", mode: "unknown", failureStatus: 429},
		{name: "权限错误不等于不支持", mode: "unknown", failureStatus: 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var seen atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Effort string `json:"reasoning_effort"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					w.WriteHeader(500)
					return
				}
				seen.Add(1)
				if tc.failureStatus != 0 {
					w.WriteHeader(tc.failureStatus)
					return
				}
				if req.Effort == "" && tc.baseline != "" {
					_, _ = io.WriteString(w, tc.baseline)
					return
				}
				if slices.Contains(tc.accepted, req.Effort) {
					_, _ = io.WriteString(w, `{"choices":[]}`)
					return
				}
				w.WriteHeader(400)
			}))
			defer server.Close()
			d := &Deps{AI: aiproxy.NewClient(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			mode, efforts := d.detectReasoning(context.Background(), &model.ProviderModel{ModelID: "reasoner-test"}, server.URL, "test")
			if mode != tc.mode || !slices.Equal(efforts, tc.accepted) {
				t.Fatalf("mode=%s efforts=%v，期望 %s %v", mode, efforts, tc.mode, tc.accepted)
			}
			if tc.failureStatus == 0 && seen.Load() < 4 {
				t.Fatalf("必须逐档检测，实际请求数: %d", seen.Load())
			}
		})
	}
}

func TestDetectReasoningInvalidSuccess(t *testing.T) {
	for _, body := range []string{`invalid`, `{}`, `{"error":"upstream failed"}`, `{"choices":null}`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }))
			defer server.Close()
			d := &Deps{AI: aiproxy.NewClient(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			mode, efforts := d.detectReasoning(context.Background(), &model.ProviderModel{ModelID: "test"}, server.URL, "test")
			if mode != "unknown" || len(efforts) != 0 {
				t.Fatalf("无效成功响应不能发布能力: %s %v", mode, efforts)
			}
		})
	}
}
