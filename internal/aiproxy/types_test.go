package aiproxy

import (
	"strings"
	"testing"
)

// 客户端不传 temperature 时必须显式下发默认值，不能靠上游的默认（通常是 1.0）。
func TestBuildUpstreamDefaultsTemperature(t *testing.T) {
	req := &ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}}
	out := req.BuildUpstream("deepseek-chat", 4096, false)
	if out.Temperature == nil {
		t.Fatal("temperature 应被填充为默认值，实际为 nil")
	}
	if *out.Temperature != DefaultTemperature {
		t.Fatalf("期望默认温度 %v，实际 %v", DefaultTemperature, *out.Temperature)
	}
}

func TestBuildUpstreamKeepsExplicitTemperature(t *testing.T) {
	temperature := 0.2
	req := &ChatRequest{
		Messages:    []Message{{Role: "user", Content: "hi"}},
		Temperature: &temperature,
	}
	out := req.BuildUpstream("deepseek-chat", 4096, false)
	if out.Temperature == nil || *out.Temperature != 0.2 {
		t.Fatalf("客户端传入的温度应原样保留，实际 %v", out.Temperature)
	}
}

// 超过输入字节上限时返回固定文案，handler 依赖它判断 413。
func TestValidateInputTooLarge(t *testing.T) {
	req := &ChatRequest{Messages: []Message{{Role: "user", Content: strings.Repeat("x", 64)}}}
	if msg := req.Validate(4096, 16); msg != MsgInputTooLarge {
		t.Fatalf("期望 %q，实际 %q", MsgInputTooLarge, msg)
	}
}
