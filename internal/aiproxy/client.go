package aiproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// UpstreamError 统一映射上游失败，供 handler 转成客户端错误码。
type UpstreamError struct {
	Kind       string // timeout | unavailable | bad_request | rate_limited | server_error
	StatusCode int
	Message    string
	// RetryAfter 来自上游 429 的 Retry-After 头（秒），为 0 表示上游没给。
	RetryAfter int
}

func (e *UpstreamError) Error() string { return e.Message }

type Client struct {
	http *http.Client
}

func NewClient() *Client {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   50,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	// 不设 Client.Timeout：流式响应可能持续数分钟，超时交给响应用时与空闲看门狗。
	return &Client{http: &http.Client{Transport: transport}}
}

// OpenChat 发起上游对话请求，仅在尚未收到响应头时重试。
func (c *Client) OpenChat(ctx context.Context, baseURL, apiKey string, body upstreamRequest) (*http.Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/chat/completions"

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(300*(1<<(attempt-1))) * time.Millisecond):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		if body.Stream {
			req.Header.Set("Accept", "text/event-stream")
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		if resp.StatusCode >= 400 {
			// 4xx 不重试；5xx 也不重试，避免重复计费。
			msg := readErrorBody(resp)
			kind := "server_error"
			if resp.StatusCode == http.StatusTooManyRequests {
				kind = "rate_limited"
			} else if resp.StatusCode < 500 {
				kind = "bad_request"
			}
			retryAfter, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
			return nil, &UpstreamError{Kind: kind, StatusCode: resp.StatusCode, Message: msg, RetryAfter: retryAfter}
		}
		return resp, nil
	}

	var netErr net.Error
	if errors.As(lastErr, &netErr) && netErr.Timeout() {
		return nil, &UpstreamError{Kind: "timeout", Message: "上游连接超时"}
	}
	return nil, &UpstreamError{Kind: "unavailable", Message: fmt.Sprintf("上游不可达: %v", lastErr)}
}

func readErrorBody(resp *http.Response) string {
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var parsed struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &parsed) == nil && parsed.Error.Message != "" {
		return parsed.Error.Message
	}
	if len(body) == 0 {
		return fmt.Sprintf("上游返回 %d", resp.StatusCode)
	}
	return fmt.Sprintf("上游返回 %d: %s", resp.StatusCode, truncate(string(body), 300))
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// ReadNonStream 读取非流式响应，缺失 usage 时用估算值补齐。
func ReadNonStream(reader io.Reader, alias string, promptEstimate int) (*ChatResponse, error) {
	body, err := io.ReadAll(io.LimitReader(reader, 8<<20))
	if err != nil {
		return nil, err
	}
	var out ChatResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("解析上游响应失败: %w", err)
	}
	if out.Object == "" {
		out.Object = "chat.completion"
	}
	if out.Model == "" || out.Model != alias {
		out.Model = alias
	}
	if out.Usage.TotalTokens == 0 {
		completion := 0
		for _, ch := range out.Choices {
			completion += EstimateTokens(ch.Message.Content)
		}
		out.Usage = Usage{
			PromptTokens:     promptEstimate,
			CompletionTokens: completion,
			TotalTokens:      promptEstimate + completion,
			Estimated:        true,
		}
	}
	return &out, nil
}
