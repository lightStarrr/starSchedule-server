// Package aiproxy 实现 OpenAI 兼容协议的上游转发、SSE 透传与用量计量。
package aiproxy

import (
	"errors"
	"strings"
	"unicode"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest 是客户端可见的请求体，只保留白名单字段。
// 温度与 max_tokens 不再对外开放：输出上限由服务端配置决定。
type ChatRequest struct {
	Messages        []Message `json:"messages"`
	TopP            *float64  `json:"top_p,omitempty"`
	Stop            []string  `json:"stop,omitempty"`
	Stream          bool      `json:"stream,omitempty"`
	Model           string    `json:"model,omitempty"`
	ReasoningEffort string    `json:"reasoning_effort,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// upstreamRequest 是实际发给供应商的载荷，字段收敛，避免客户端越权。
type upstreamRequest struct {
	Model           string         `json:"model"`
	Messages        []Message      `json:"messages"`
	TopP            *float64       `json:"top_p,omitempty"`
	MaxTokens       int            `json:"max_tokens"`
	Stop            []string       `json:"stop,omitempty"`
	Stream          bool           `json:"stream"`
	StreamOptions   *streamOptions `json:"stream_options,omitempty"`
	ReasoningEffort string         `json:"reasoning_effort,omitempty"`
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
			Role    string `json:"role"`
			Content string `json:"content"`
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

// ErrInputTooLarge 表示 messages 超过字节上限。
// handler 用 errors.Is 判断并返回 413，避免靠文案相等来分支。
var ErrInputTooLarge = errors.New("messages 内容过长，请精简上下文")

// Validate 做服务端侧校验，返回的错误信息可直接展示给用户。
func (r *ChatRequest) Validate(maxOutput int, maxInputBytes int) error {
	if len(r.Messages) == 0 {
		return errors.New("messages 不能为空")
	}
	if len(r.Messages) > 100 {
		return errors.New("messages 最多 100 条")
	}
	total := 0
	for _, m := range r.Messages {
		switch m.Role {
		case "system", "user", "assistant":
		default:
			return errors.New("messages.role 只能是 system/user/assistant")
		}
		if strings.TrimSpace(m.Content) == "" {
			return errors.New("messages.content 不能为空")
		}
		total += len(m.Content)
	}
	if total > maxInputBytes {
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
	if len(r.Stop) > 0 {
		out.Stop = r.Stop[:min(len(r.Stop), 4)]
	}
	if stream {
		out.StreamOptions = &streamOptions{IncludeUsage: true}
	}
	return out
}

// BuildReasoningProbe 构造一条最小请求，用于探测上游是否接受 reasoning_effort：
// 200 说明接受，400 说明不支持，其余错误无法判断。
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
		total += EstimateTokens(m.Content) + 4 // 角色与分隔符开销
	}
	return total
}
