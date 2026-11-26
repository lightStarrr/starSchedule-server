// Package aiproxy 实现 OpenAI 兼容协议的上游转发、SSE 透传与用量计量。
package aiproxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
)

type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolCall struct {
	ID           string           `json:"id"`
	Type         string           `json:"type"`
	Function     ToolCallFunction `json:"function"`
	ExtraContent json.RawMessage  `json:"extra_content,omitempty"`
}

type Message struct {
	Role             string     `json:"role"`
	Content          string     `json:"content"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	Name             string     `json:"name,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
}

// MarshalJSON 在 assistant 仅返回 tool_calls 时将 content 序列化为 null，
// 以满足 OpenAI 及部分上游网关对工具调用消息的规范要求。
func (m Message) MarshalJSON() ([]byte, error) {
	type rawMessage struct {
		Role             string     `json:"role"`
		Content          *string    `json:"content"`
		ReasoningContent string     `json:"reasoning_content,omitempty"`
		Name             string     `json:"name,omitempty"`
		ToolCallID       string     `json:"tool_call_id,omitempty"`
		ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	}
	var contentPtr *string
	if !(m.Role == "assistant" && m.Content == "" && len(m.ToolCalls) > 0) {
		c := m.Content
		contentPtr = &c
	}
	return json.Marshal(rawMessage{
		Role:             m.Role,
		Content:          contentPtr,
		ReasoningContent: m.ReasoningContent,
		Name:             m.Name,
		ToolCallID:       m.ToolCallID,
		ToolCalls:        m.ToolCalls,
	})
}

// ChatRequest 是客户端可见的请求体，只保留白名单字段。
// 温度与 max_tokens 不再对外开放：输出上限由服务端配置决定。
type ChatRequest struct {
	Messages          []Message       `json:"messages"`
	Tools             []Tool          `json:"tools,omitempty"`
	ToolChoice        json.RawMessage `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool           `json:"parallel_tool_calls,omitempty"`
	TopP              *float64        `json:"top_p,omitempty"`
	Stop              []string        `json:"stop,omitempty"`
	Stream            bool            `json:"stream,omitempty"`
	Model             string          `json:"model,omitempty"`
	ReasoningEffort   string          `json:"reasoning_effort,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// upstreamRequest 是实际发给供应商的载荷，字段收敛，避免客户端越权。
type upstreamRequest struct {
	Model             string          `json:"model"`
	Messages          []Message       `json:"messages"`
	Tools             []Tool          `json:"tools,omitempty"`
	ToolChoice        json.RawMessage `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool           `json:"parallel_tool_calls,omitempty"`
	TopP              *float64        `json:"top_p,omitempty"`
	MaxTokens         int             `json:"max_tokens"`
	Stop              []string        `json:"stop,omitempty"`
	Stream            bool            `json:"stream"`
	StreamOptions     *streamOptions  `json:"stream_options,omitempty"`
	ReasoningEffort   string          `json:"reasoning_effort,omitempty"`
}

type Usage struct {
	PromptTokens     int  `json:"prompt_tokens"`
	CompletionTokens int  `json:"completion_tokens"`
	TotalTokens      int  `json:"total_tokens"`
	Estimated        bool `json:"estimated"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type ChatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// streamChunk 只解析我们关心的字段，其余原样透传。
type streamChunk struct {
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role             string `json:"role"`
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// ReasoningEfforts 是 OpenAI 兼容的思考强度档位。
var ReasoningEfforts = []string{"minimal", "low", "medium", "high"}

// ErrInputTooLarge 表示请求输入内容超过字节上限。
// handler 用 errors.Is 判断并返回 413，避免靠文案相等来分支。
var ErrInputTooLarge = errors.New("messages 内容过长，请精简上下文")

// Validate 做服务端侧校验，返回的错误信息可直接展示给用户。
func (r *ChatRequest) Validate(maxOutput int, maxInputBytes int) error {
	msgBytes, err := validateMessages(r.Messages)
	if err != nil {
		return err
	}
	toolBytes, toolNames, err := validateTools(r.Tools)
	if err != nil {
		return err
	}
	choiceBytes, err := validateToolChoice(r.ToolChoice, len(r.Tools) > 0, toolNames)
	if err != nil {
		return err
	}
	if r.ParallelToolCalls != nil && len(r.Tools) == 0 {
		return errors.New("未提供 tools 时不能设置 parallel_tool_calls")
	}
	if msgBytes+toolBytes+choiceBytes > maxInputBytes {
		return ErrInputTooLarge
	}
	if r.TopP != nil && (*r.TopP < 0 || *r.TopP > 1) {
		return errors.New("top_p 必须在 0–1 之间")
	}
	if len(r.Stop) > 4 {
		return errors.New("stop 最多 4 条")
	}
	if r.ReasoningEffort != "" && !IsValidReasoningEffort(r.ReasoningEffort) {
		return errors.New("reasoning_effort 只能是 minimal / low / medium / high")
	}
	_ = maxOutput
	return nil
}

func validateMessages(msgs []Message) (int, error) {
	if len(msgs) == 0 {
		return 0, errors.New("messages 不能为空")
	}
	if len(msgs) > 100 {
		return 0, errors.New("messages 最多 100 条")
	}
	total := 0
	for _, m := range msgs {
		switch m.Role {
		case "system", "user":
			if strings.TrimSpace(m.Content) == "" {
				return 0, errors.New("messages.content 不能为空")
			}
			if len(m.ToolCalls) > 0 || m.ToolCallID != "" {
				return 0, errors.New("system/user 消息不能包含 tool_calls 或 tool_call_id")
			}
		case "assistant":
			if m.ToolCallID != "" {
				return 0, errors.New("assistant 消息不能包含 tool_call_id")
			}
			if strings.TrimSpace(m.Content) == "" && len(m.ToolCalls) == 0 {
				return 0, errors.New("assistant 消息的 content 与 tool_calls 不能同时为空")
			}
			for _, tc := range m.ToolCalls {
				if strings.TrimSpace(tc.ID) == "" {
					return 0, errors.New("tool_calls.id 不能为空")
				}
				if tc.Type != "function" {
					return 0, errors.New("tool_calls.type 只能是 function")
				}
				if strings.TrimSpace(tc.Function.Name) == "" {
					return 0, errors.New("tool_calls.function.name 不能为空")
				}
				total += len(tc.ID) + len(tc.Type) + len(tc.Function.Name) + len(tc.Function.Arguments) + len(tc.ExtraContent)
			}
		case "tool":
			if strings.TrimSpace(m.ToolCallID) == "" {
				return 0, errors.New("tool 消息必须提供 tool_call_id")
			}
			if len(m.ToolCalls) > 0 {
				return 0, errors.New("tool 消息不能包含 tool_calls")
			}
		default:
			return 0, errors.New("messages.role 只能是 system/user/assistant/tool")
		}
		total += len(m.Content) + len(m.ReasoningContent) + len(m.ToolCallID) + len(m.Name)
	}
	return total, nil
}

func validateTools(tools []Tool) (int, map[string]struct{}, error) {
	if len(tools) > 128 {
		return 0, nil, errors.New("tools 最多 128 个")
	}
	names := make(map[string]struct{}, len(tools))
	total := 0
	for _, t := range tools {
		if t.Type != "function" {
			return 0, nil, errors.New("tools.type 只能是 function")
		}
		name := strings.TrimSpace(t.Function.Name)
		if name == "" || len(name) > 128 {
			return 0, nil, errors.New("tools.function.name 不能为空且不超过 128 字符")
		}
		if _, exists := names[name]; exists {
			return 0, nil, errors.New("tools.function.name 不能重复")
		}
		names[name] = struct{}{}
		if params := bytes.TrimSpace(t.Function.Parameters); len(params) > 0 && !bytes.Equal(params, []byte("null")) {
			if params[0] != '{' || !json.Valid(params) {
				return 0, nil, errors.New("tools.function.parameters 必须是合法的 JSON 对象")
			}
		}
		total += len(t.Type) + len(t.Function.Name) + len(t.Function.Description) + len(t.Function.Parameters)
	}
	return total, names, nil
}

func validateToolChoice(raw json.RawMessage, hasTools bool, toolNames map[string]struct{}) (int, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return 0, nil
	}
	var mode string
	if json.Unmarshal(trimmed, &mode) == nil {
		switch mode {
		case "none":
			return len(trimmed), nil
		case "auto", "required":
			if !hasTools {
				return 0, errors.New("未提供 tools 时 tool_choice 只能为 none")
			}
			return len(trimmed), nil
		default:
			return 0, errors.New("tool_choice 字符串只能是 none / auto / required")
		}
	}
	if trimmed[0] == '{' {
		var obj struct {
			Type     string `json:"type"`
			Function *struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		if json.Unmarshal(trimmed, &obj) == nil && obj.Type == "function" && obj.Function != nil {
			name := strings.TrimSpace(obj.Function.Name)
			if name == "" {
				return 0, errors.New("tool_choice.function.name 不能为空")
			}
			if !hasTools {
				return 0, errors.New("未提供 tools 时不能指定 tool_choice 函数")
			}
			if _, ok := toolNames[name]; !ok {
				return 0, errors.New("tool_choice 指定的函数不在 tools 中")
			}
			return len(trimmed), nil
		}
	}
	return 0, errors.New("tool_choice 格式无效")
}

// IsValidReasoningEffort 判断档位是否属于 OpenAI 兼容集合。
func IsValidReasoningEffort(effort string) bool {
	for _, v := range ReasoningEfforts {
		if v == effort {
			return true
		}
	}
	return false
}

// BuildUpstream 组装上游请求体，max_tokens 始终由服务端配置决定。
func (r *ChatRequest) BuildUpstream(model string, maxOutput int, stream bool, effort string) upstreamRequest {
	out := upstreamRequest{
		Model:           model,
		Messages:        r.Messages,
		TopP:            r.TopP,
		MaxTokens:       maxOutput,
		Stream:          stream,
		ReasoningEffort: effort,
	}
	if len(r.Tools) > 0 {
		out.Tools = r.Tools
	}
	if tc := bytes.TrimSpace(r.ToolChoice); len(tc) > 0 && !bytes.Equal(tc, []byte("null")) {
		out.ToolChoice = tc
	}
	if r.ParallelToolCalls != nil {
		out.ParallelToolCalls = r.ParallelToolCalls
	}
	if len(r.Stop) > 0 {
		out.Stop = r.Stop[:min(len(r.Stop), 4)]
	}
	if stream {
		out.StreamOptions = &streamOptions{IncludeUsage: true}
	}
	return out
}

// BuildReasoningProbe 构造一条最小请求，用于探测上游是否接受 reasoning_effort：
// 每个档位独立确认；200 表示上游接受参数，4xx 需结合基线请求判断。
func BuildReasoningProbe(model, effort string) upstreamRequest {
	return upstreamRequest{
		Model:           model,
		Messages:        []Message{{Role: "user", Content: "ping"}},
		MaxTokens:       16,
		Stream:          false,
		ReasoningEffort: effort,
	}
}

// EstimateTokens 估算 token 数：CJK 按字符计，ASCII 按 4 字符 1 token，另加结构开销。
// 上游未返回 usage 时兜底，避免账目完全失真。
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	cjk, other := 0, 0
	for _, r := range text {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
			unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) {
			cjk++
			continue
		}
		other++
	}
	est := cjk + other/4
	if est < 1 {
		est = 1
	}
	return est
}

func EstimateMessagesTokens(msgs []Message) int {
	total := 0
	for _, m := range msgs {
		total += EstimateTokens(m.Content) +
			EstimateTokens(m.ReasoningContent) +
			EstimateTokens(m.ToolCallID) +
			EstimateTokens(m.Name) + 4 // 角色与分隔符开销
		for _, tc := range m.ToolCalls {
			total += EstimateTokens(tc.ID) +
				EstimateTokens(tc.Function.Name) +
				EstimateTokens(tc.Function.Arguments) + 4
		}
	}
	return total
}

// EstimatePromptTokens 估算整次请求的输入 token（含 messages、tools 定义与 tool_choice）。
func (r *ChatRequest) EstimatePromptTokens() int {
	total := EstimateMessagesTokens(r.Messages)
	for _, t := range r.Tools {
		total += EstimateTokens(t.Function.Name) +
			EstimateTokens(t.Function.Description) +
			EstimateTokens(string(t.Function.Parameters)) + 8
	}
	if tc := bytes.TrimSpace(r.ToolChoice); len(tc) > 0 && !bytes.Equal(tc, []byte("null")) {
		total += EstimateTokens(string(tc))
	}
	return total
}
