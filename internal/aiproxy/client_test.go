package aiproxy

import (
	"context"
	"errors"
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
