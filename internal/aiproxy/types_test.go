package aiproxy

import (
	"encoding/json"
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

func TestValidateToolCallsAndOptions(t *testing.T) {
	parallel := true
	validReq := &ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "查课表"},
			{
				Role:             "assistant",
				ReasoningContent: "需要先查询周三课程",
				ToolCalls: []ToolCall{{
					ID:           "call_1",
					Type:         "function",
					Function:     ToolCallFunction{Name: "get_schedule", Arguments: `{"weekday":3}`},
					ExtraContent: json.RawMessage(`{"google":{"thought_signature":"sig123"}}`),
				}},
			},
			{Role: "tool", ToolCallID: "call_1", Name: "get_schedule", Content: `{"courses":["高数"]}`},
		},
		Tools: []Tool{{
			Type: "function",
			Function: ToolFunction{
				Name:        "get_schedule",
				Description: "查询课表",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"weekday":{"type":"integer"}}}`),
			},
		}},
		ToolChoice:        json.RawMessage(`{"type":"function","function":{"name":"get_schedule"}}`),
		ParallelToolCalls: &parallel,
	}
	if err := validReq.Validate(4096, 8192); err != nil {
		t.Fatalf("合法 tool_calls 请求校验失败: %v", err)
	}

	cases := []struct {
		name    string
		req     ChatRequest
		isLarge bool
	}{
		{
			name: "system带tool_calls",
			req: ChatRequest{Messages: []Message{{
				Role: "system", Content: "hi",
				ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "fn"}}},
			}}},
		},
		{
			name: "user带tool_call_id",
			req:  ChatRequest{Messages: []Message{{Role: "user", Content: "hi", ToolCallID: "c1"}}},
		},
		{
			name: "assistant同时空content和tool_calls",
			req:  ChatRequest{Messages: []Message{{Role: "assistant"}}},
		},
		{
			name: "assistant带tool_call_id",
			req:  ChatRequest{Messages: []Message{{Role: "assistant", Content: "ok", ToolCallID: "c1"}}},
		},
		{
			name: "assistant工具调用缺name",
			req: ChatRequest{Messages: []Message{{
				Role:      "assistant",
				ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Arguments: "{}"}}},
			}}},
		},
		{
			name: "tool带tool_calls",
			req: ChatRequest{Messages: []Message{{
				Role: "tool", ToolCallID: "c1", Content: "ok",
				ToolCalls: []ToolCall{{ID: "c2", Type: "function", Function: ToolCallFunction{Name: "fn"}}},
			}}},
		},
		{
			name: "tool名称重复",
			req: ChatRequest{
				Messages: []Message{{Role: "user", Content: "hi"}},
				Tools: []Tool{
					{Type: "function", Function: ToolFunction{Name: "dup"}},
					{Type: "function", Function: ToolFunction{Name: "dup"}},
				},
			},
		},
		{
			name: "parameters非对象",
			req: ChatRequest{
				Messages: []Message{{Role: "user", Content: "hi"}},
				Tools:    []Tool{{Type: "function", Function: ToolFunction{Name: "fn", Parameters: json.RawMessage(`["bad"]`)}}},
			},
		},
		{
			name: "无tools时tool_choice为auto",
			req: ChatRequest{
				Messages:   []Message{{Role: "user", Content: "hi"}},
				ToolChoice: json.RawMessage(`"auto"`),
			},
		},
		{
			name: "tool_choice指向不存在的工具",
			req: ChatRequest{
				Messages:   []Message{{Role: "user", Content: "hi"}},
				Tools:      []Tool{{Type: "function", Function: ToolFunction{Name: "fn1"}}},
				ToolChoice: json.RawMessage(`{"type":"function","function":{"name":"fn2"}}`),
			},
		},
		{
			name: "无tools时设置parallel_tool_calls",
			req: ChatRequest{
				Messages:          []Message{{Role: "user", Content: "hi"}},
				ParallelToolCalls: &parallel,
			},
		},
		{
			name: "tools计入输入字节上限",
			req: ChatRequest{
				Messages: []Message{{Role: "user", Content: "hi"}},
				Tools: []Tool{{
					Type:     "function",
					Function: ToolFunction{Name: "fn", Description: strings.Repeat("d", 100)},
				}},
			},
			isLarge: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate(4096, 64)
			if err == nil {
				t.Fatal("预期校验失败，实际通过")
			}
			if tc.isLarge && !errors.Is(err, ErrInputTooLarge) {
				t.Fatalf("期望 ErrInputTooLarge，实际 %v", err)
			}
		})
	}
}

func TestBuildUpstreamToolsAndMessageMarshal(t *testing.T) {
	parallel := false
	req := &ChatRequest{
		Messages: []Message{
			{
				Role: "assistant",
				ToolCalls: []ToolCall{{
					ID:           "call_1",
					Type:         "function",
					Function:     ToolCallFunction{Name: "get_weather", Arguments: ""},
					ExtraContent: json.RawMessage(`{"google":{"thought_signature":"abc"}}`),
				}},
			},
			{Role: "tool", ToolCallID: "call_1", Content: ""},
		},
		Tools: []Tool{{
			Type:     "function",
			Function: ToolFunction{Name: "get_weather", Parameters: json.RawMessage(`{"type":"object"}`)},
		}},
		ToolChoice:        json.RawMessage(`"auto"`),
		ParallelToolCalls: &parallel,
	}

	up := req.BuildUpstream("gpt-4o", 2048, false, "")
	raw, err := json.Marshal(up)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if decoded["tool_choice"] != "auto" {
		t.Fatalf("tool_choice 未透传: %v", decoded["tool_choice"])
	}
	if decoded["parallel_tool_calls"] != false {
		t.Fatalf("parallel_tool_calls 未透传: %v", decoded["parallel_tool_calls"])
	}
	msgs := decoded["messages"].([]any)
	asst := msgs[0].(map[string]any)
	if val, ok := asst["content"]; !ok || val != nil {
		t.Fatalf("assistant 仅含 tool_calls 时 content 应为 null，实际 exists=%v val=%v", ok, val)
	}
	toolMsg := msgs[1].(map[string]any)
	if toolMsg["content"] != "" {
		t.Fatalf("tool 空消息 content 应保持空字符串，实际 %#v", toolMsg["content"])
	}

	// tool_choice 为 null 时不下发
	req.ToolChoice = json.RawMessage("null")
	upNull := req.BuildUpstream("gpt-4o", 2048, false, "")
	if len(upNull.ToolChoice) != 0 {
		t.Fatalf("tool_choice 为 null 时不应下发，实际 %s", string(upNull.ToolChoice))
	}
}

func TestEstimatePromptTokensIncludesTools(t *testing.T) {
	base := ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}}
	withTools := ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "f", Arguments: `{"a":1}`}}}},
		},
		Tools: []Tool{{
			Type:     "function",
			Function: ToolFunction{Name: "f", Description: "describe tool", Parameters: json.RawMessage(`{"type":"object"}`)},
		}},
		ToolChoice: json.RawMessage(`"required"`),
	}
	if withTools.EstimatePromptTokens() <= base.EstimatePromptTokens() {
		t.Fatalf("含 tools 与 tool_calls 的估算应大于纯文本: %d vs %d", withTools.EstimatePromptTokens(), base.EstimatePromptTokens())
	}
}
