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
	promptEstimate int, maxOutput int) (StreamResult, error) {

	res := StreamResult{PromptTokens: promptEstimate}
	start := time.Now()

	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{}) // 流式响应不设写超时

	write := func(s string) error {
		if _, err := io.WriteString(w, s); err != nil {
			return err
		}
		return nil
	}
	flush := func() error { return rc.Flush() }

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
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				select {
				case lines <- lineEvent{err: err}:
				case <-ctx.Done():
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
	var completion strings.Builder
	usageSeen := false

	for !done {
		select {
		case <-ctx.Done():
			// 客户端断开：直接返回，由调用方取消上游请求。
			return res, nil

		case <-ping.C:
			if err := write(": ping\n\n"); err != nil {
				return res, nil
			}
			_ = flush()

		case <-idle.C:
			_ = write("event: error\ndata: " +
				mustJSON(map[string]any{"error": map[string]any{
					"code": "UPSTREAM_TIMEOUT", "message": "模型响应超时，请重试",
					"request_id": meta.RequestID,
				}}) + "\n\n")
			_ = flush()
			return res, fmt.Errorf("上游空闲超时")

		case ev, ok := <-lines:
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
					return res, nil
				}
				_ = flush()
				continue
			}

			if payload, ok := strings.CutPrefix(trimmed, "data:"); ok {
				payload = strings.TrimSpace(payload)

				if payload == "[DONE]" {
					if !usageSeen {
						res.Estimated = true
						res.CompletionTokens = EstimateTokens(completion.String())
						if res.CompletionTokens > maxOutput {
							res.CompletionTokens = maxOutput
						}
						inject := map[string]any{
							"choices": []any{},
							"usage": Usage{
								PromptTokens:     res.PromptTokens,
								CompletionTokens: res.CompletionTokens,
								TotalTokens:      res.PromptTokens + res.CompletionTokens,
								Estimated:        true,
							},
						}
						_ = write("data: " + mustJSON(inject) + "\n\n")
					}
					if err := write(line); err != nil {
						return res, nil
					}
					_ = flush()
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
				return res, nil
			}
		}
	}

	res.TotalMS = int(time.Since(start).Milliseconds())
	if res.CompletionTokens == 0 && completion.Len() > 0 {
		res.CompletionTokens = EstimateTokens(completion.String())
		res.Estimated = true
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
