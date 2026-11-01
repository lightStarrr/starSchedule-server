package aiproxy

import "testing"

func TestEstimateTokens(t *testing.T) {
	cases := []struct {
		name string
		in   string
		min  int
		max  int
	}{
		{"空字符串", "", 0, 0},
		{"纯中文", "你好世界", 4, 4},
		{"纯英文", "hello world", 2, 3},
		{"中英混合", "帮我排课 schedule", 5, 8},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := EstimateTokens(c.in)
			if got < c.min || got > c.max {
				t.Fatalf("EstimateTokens(%q) = %d, 期望区间 [%d,%d]", c.in, got, c.min, c.max)
			}
		})
	}
}

func TestEstimateMessagesTokens(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "你是助教"},
		{Role: "user", Content: "帮我看下周三的课"},
	}
	got := EstimateMessagesTokens(msgs)
	if got < 10 {
		t.Fatalf("估算值偏小: %d", got)
	}
}

func TestBuildUpstreamUsesServerMaxTokens(t *testing.T) {
	req := ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Stop:     []string{"a", "b", "c", "d", "e"},
	}
	up := req.BuildUpstream("deepseek-chat", 4096, true, "medium")
	if up.MaxTokens != 4096 {
		t.Fatalf("max_tokens 未被截断: %d", up.MaxTokens)
	}
	if len(up.Stop) != 4 {
		t.Fatalf("stop 未被截断: %d", len(up.Stop))
	}
	if up.StreamOptions == nil || !up.StreamOptions.IncludeUsage {
		t.Fatal("流式请求必须带 include_usage")
	}
}

func TestValidate(t *testing.T) {
	bad := ChatRequest{Messages: []Message{{Role: "user", Content: "   "}}}
	if err := bad.Validate(4096, 1024); err == nil {
		t.Fatal("空消息内容应该校验失败")
	}
	ok := ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}}
	if err := ok.Validate(4096, 1024); err != nil {
		t.Fatalf("合法请求被拒绝: %v", err)
	}
	role := ChatRequest{Messages: []Message{{Role: "tool", Content: "x"}}}
	if err := role.Validate(4096, 1024); err == nil {
		t.Fatal("非法 role 应该校验失败")
	}
}
