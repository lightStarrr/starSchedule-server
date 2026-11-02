package aiproxy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	pingInterval = 15 * time.Second
	idleTimeout  = 90 * time.Second
)

type Meta struct {
	ID        string `json:"id"`
	Model     string `json:"model"`
	Alias     string `json:"alias"`
	RequestID string `json:"request_id"`
}

type StreamResult struct {
	PromptTokens     int
	CompletionTokens int
	Estimated        bool
	FirstTokenMS     int
	TotalMS          int
	FinishReason     string
	ContentBytes     int
}

type lineEvent struct {
	line string
	err  error
}

// Relay 把上游 SSE 逐帧转发给客户端：
//   - 帧边界（空行）才 Flush，避免半帧
//   - 每 15s 发注释帧保活（Android 侧据此判活）
//   - 90s 无任何上游字节视为空闲超时
//   - 上游缺 usage 时用估算值补一帧，再转发 [DONE]
func Relay(ctx context.Context, w http.ResponseWriter, upstream io.Reader, meta Meta,
	promptEstimate int, maxOutput int) (res StreamResult, relayErr error) {

	res.PromptTokens = promptEstimate
	start := time.Now()
	var completion strings.Builder
	usageSeen := false
	finalizeUsage := func() {
		if !usageSeen {
			res.Estimated = true
			res.CompletionTokens = min(EstimateTokens(completion.String()), maxOutput)
		}
	}
	// 正常结束、客户端取消和上游断流都必须结算已接收到的内容。
	defer func() {
		finalizeUsage()
		res.TotalMS = int(time.Since(start).Milliseconds())
	}()
	readCtx, cancelRead := context.WithCancel(ctx)
	defer cancelRead()

	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{}) // 流式响应不设写超时

	write := func(s string) error {
		if _, err := io.WriteString(w, s); err != nil {
			return err
		}
		return nil
	}
	flush := func() error { return rc.Flush() }
	writeTimeout := func() {
		_ = write("event: error\ndata: " +
			mustJSON(map[string]any{"error": map[string]any{
				"code": "UPSTREAM_TIMEOUT", "message": "模型响应超时，请重试",
				"request_id": meta.RequestID,
			}}) + "\n\n")
		_ = flush()
	}

	metaJSON, _ := json.Marshal(meta)
	if err := write("event: meta\ndata: " + string(metaJSON) + "\n\n"); err != nil {
		return res, err
	}
	if err := flush(); err != nil {
		return res, err
	}

	// 读上游放在独立 goroutine，主循环才能同时处理保活帧与空闲超时。
	lines := make(chan lineEvent, 64)
	go func() {
		defer close(lines)
		reader := bufio.NewReaderSize(upstream, 32*1024)
		for {
			line, err := reader.ReadString('\n')
			if line != "" {
				select {
				case lines <- lineEvent{line: line}:
				case <-readCtx.Done():
					return
				}
			}
			if err != nil {
				select {
				case lines <- lineEvent{err: err}:
				case <-readCtx.Done():
				}
				return
			}
		}
	}()

	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	idle := time.NewTimer(idleTimeout)
	defer idle.Stop()

	done := false

	for !done {
		select {
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				writeTimeout()
			}
			return res, ctx.Err()

		case <-ping.C:
			if err := write(": ping\n\n"); err != nil {
				return res, err
			}
			if err := flush(); err != nil {
				return res, err
			}

		case <-idle.C:
			writeTimeout()
			return res, fmt.Errorf("上游空闲超时: %w", context.DeadlineExceeded)

		case ev, ok := <-lines:
			if ctx.Err() != nil {
				if ctx.Err() == context.DeadlineExceeded {
					writeTimeout()
				}
				return res, ctx.Err()
			}
			if !ok {
				return res, fmt.Errorf("上游连接中断")
			}
			if ev.err != nil {
				if ev.err == io.EOF {
					return res, fmt.Errorf("上游未正常结束")
				}
				return res, ev.err
			}

			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(idleTimeout)

			line := ev.line
			trimmed := strings.TrimRight(line, "\r\n")

			if trimmed == "" {
				if err := write("\n"); err != nil {
					return res, err
				}
				if err := flush(); err != nil {
					return res, err
				}
				continue
			}

			if payload, ok := strings.CutPrefix(trimmed, "data:"); ok {
				payload = strings.TrimSpace(payload)

				if payload == "[DONE]" {
					if !usageSeen {
						finalizeUsage()
						inject := map[string]any{
							"choices": []any{},
							"usage": Usage{
								PromptTokens:     res.PromptTokens,
								CompletionTokens: res.CompletionTokens,
								TotalTokens:      res.PromptTokens + res.CompletionTokens,
								Estimated:        true,
							},
						}
						if err := write("data: " + mustJSON(inject) + "\n\n"); err != nil {
							return res, err
						}
					}
					// SSE 必须以空行结束事件，不能只转发 [DONE] 所在的一行。
					if err := write("data: [DONE]\n\n"); err != nil {
						return res, err
					}
					if err := flush(); err != nil {
						return res, err
					}
					done = true
					break
				}

				var chunk streamChunk
				if err := json.Unmarshal([]byte(payload), &chunk); err == nil {
					if chunk.Usage != nil {
						usageSeen = true
						res.PromptTokens = chunk.Usage.PromptTokens
						res.CompletionTokens = chunk.Usage.CompletionTokens
						res.Estimated = false
					}
					for _, ch := range chunk.Choices {
						if ch.Delta.Content != "" {
							if res.FirstTokenMS == 0 {
								res.FirstTokenMS = int(time.Since(start).Milliseconds())
							}
							completion.WriteString(ch.Delta.Content)
							res.ContentBytes += len(ch.Delta.Content)
						}
						if ch.FinishReason != nil && *ch.FinishReason != "" {
							res.FinishReason = *ch.FinishReason
						}
					}
				}
			}

			if err := write(line); err != nil {
				return res, err
			}
		}
	}

	return res, nil
}

func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(raw)
}
