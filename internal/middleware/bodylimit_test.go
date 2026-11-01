package middleware_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/starschedule/server/internal/middleware"
	"github.com/starschedule/server/internal/respond"
)

// 超过 BodyLimit 时必须返回 413 PAYLOAD_TOO_LARGE，而不是 400 或 500。
func TestBodyLimitReturns413(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/echo", middleware.BodyLimit(1024), func(c *gin.Context) {
		var body struct {
			Text string `json:"text"`
		}
		if !respond.BindJSON(c, &body) {
			return
		}
		c.JSON(http.StatusOK, gin.H{"len": len(body.Text)})
	})

	big, _ := json.Marshal(map[string]string{"text": strings.Repeat("a", 4096)})
	req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewReader(big))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("期望 413，实际 %d，body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if resp.Error.Code != respond.CodePayloadTooLarge {
		t.Fatalf("期望错误码 %s，实际 %s", respond.CodePayloadTooLarge, resp.Error.Code)
	}
}

func TestBodyLimitAllowsSmallBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/echo", middleware.BodyLimit(1024), func(c *gin.Context) {
		var body struct {
			Text string `json:"text"`
		}
		if !respond.BindJSON(c, &body) {
			return
		}
		c.JSON(http.StatusOK, gin.H{"len": len(body.Text)})
	})

	small, _ := json.Marshal(map[string]string{"text": "hello"})
	req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewReader(small))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body=%s", w.Code, w.Body.String())
	}
}
