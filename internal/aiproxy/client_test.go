package aiproxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOpenChatDeadlineCoversResponseBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer upstream.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	client := NewClient()
	defer client.http.CloseIdleConnections()
	resp, err := client.OpenChat(ctx, upstream.URL, "test", upstreamRequest{Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, err = ReadNonStream(resp.Body, "test", 1)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("收到响应头后，读取响应体仍应受截止时间约束: %v", err)
	}
}

func TestOpenChatAndReadNonStreamToolCalls(t *testing.T) {
	var seenBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &seenBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"up-1",
			"object":"chat.completion",
			"created":1761896599,
			"model":"upstream-m",
			"choices":[{
				"index":0,
				"message":{
					"role":"assistant",
					"content":null,
					"reasoning_content":"先查课表",
					"tool_calls":[{
						"id":"call_1",
						"type":"function",
						"function":{"name":"get_schedule","arguments":"{\"weekday\":3}"},
						"extra_content":{"google":{"thought_signature":"sig"}}
					}]
				},
				"finish_reason":"tool_calls"
			}]
		}`)
	}))
	defer upstream.Close()

	req := ChatRequest{
		Messages: []Message{{Role: "user", Content: "周三什么课"}},
		Tools: []Tool{{
			Type:     "function",
			Function: ToolFunction{Name: "get_schedule", Parameters: json.RawMessage(`{"type":"object"}`)},
		}},
		ToolChoice: json.RawMessage(`"auto"`),
	}
	client := NewClient()
	defer client.http.CloseIdleConnections()

	resp, err := client.OpenChat(context.Background(), upstream.URL, "test", req.BuildUpstream("upstream-m", 1024, false, ""))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if seenBody["tool_choice"] != "auto" || len(seenBody["tools"].([]any)) != 1 {
		t.Fatalf("上游未收到完整的 tools 请求体: %+v", seenBody)
	}

	out, err := ReadNonStream(resp.Body, "alias-m", req.EstimatePromptTokens())
	if err != nil {
		t.Fatal(err)
	}
	if out.Model != "alias-m" || len(out.Choices) != 1 {
		t.Fatalf("响应元信息异常: %+v", out)
	}
	ch := out.Choices[0]
	if ch.FinishReason != "tool_calls" || len(ch.Message.ToolCalls) != 1 {
		t.Fatalf("tool_calls 未保留: %+v", ch)
	}
	if ch.Message.ToolCalls[0].Function.Name != "get_schedule" || string(ch.Message.ToolCalls[0].ExtraContent) == "" {
		t.Fatalf("tool_call 详情异常: %+v", ch.Message.ToolCalls[0])
	}
	if !out.Usage.Estimated || out.Usage.CompletionTokens <= 0 {
		t.Fatalf("缺失 usage 时应按 tool_calls 和 reasoning_content 补算输出 token: %+v", out.Usage)
	}
}
