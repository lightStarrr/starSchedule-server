package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	Register(r, "/admin")
	return r
}

func TestAdminRootRedirects(t *testing.T) {
	r := newRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusMovedPermanently {
		t.Fatalf("期望 301，实际 %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/admin/" {
		t.Fatalf("跳转地址异常: %q", loc)
	}
}

func TestAdminServesIndex(t *testing.T) {
	r := newRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", w.Code)
	}
	if body := w.Body.String(); strings.TrimSpace(body) == "" {
		t.Fatal("响应体为空")
	}
}

// 前端路由必须回落到 index，否则直接刷新 /admin/devices 会 404。
func TestSPAFallback(t *testing.T) {
	r := newRouter()
	for _, path := range []string{"/admin/devices", "/admin/ai/providers", "/admin/settings"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s 期望 200，实际 %d", path, w.Code)
		}
	}
}
