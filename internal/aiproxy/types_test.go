package aiproxy

import (
	"errors"
	"strings"
	"testing"
)

func TestBuildUpstreamCarriesReasoningEffort(t *testing.T) {
	req := &ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}}
	out := req.BuildUpstream("deepseek-chat", 4096, false, "high")
	if out.ReasoningEffort != "high" {
		t.Fatalf("思考强度应透传，实际 %q", out.ReasoningEffort)
	}
	if out.MaxTokens != 4096 {
		t.Fatalf("max_tokens 应固定为服务端上限，实际 %d", out.MaxTokens)
	}
}

func TestValidateRejectsUnknownReasoningEffort(t *testing.T) {
	req := &ChatRequest{
		Messages:        []Message{{Role: "user", Content: "hi"}},
		ReasoningEffort: "ultra",
	}
	if err := req.Validate(4096, 1024); err == nil {
		t.Fatal("非法思考强度应校验失败")
	}
	req.ReasoningEffort = "medium"
	if err := req.Validate(4096, 1024); err != nil {
		t.Fatalf("合法思考强度被拒绝: %v", err)
	}
}

// 超过输入字节上限时返回 ErrInputTooLarge，handler 依赖 errors.Is 判断 413。
func TestValidateInputTooLarge(t *testing.T) {
	req := &ChatRequest{Messages: []Message{{Role: "user", Content: strings.Repeat("x", 64)}}}
	if err := req.Validate(4096, 16); !errors.Is(err, ErrInputTooLarge) {
		t.Fatalf("期望 ErrInputTooLarge，实际 %v", err)
	}
}

// 其它校验失败不能误判成 413。
func TestValidateRejectsEmptyMessages(t *testing.T) {
	req := &ChatRequest{}
	err := req.Validate(4096, 16)
	if err == nil || errors.Is(err, ErrInputTooLarge) {
		t.Fatalf("空 messages 应返回普通错误，实际 %v", err)
	}
}
