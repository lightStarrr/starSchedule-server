package middleware

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCSRFGuard(t *testing.T) {
	for _, tc := range []struct {
		name, origin, host, forwardedProto string
		tls, cookie                        bool
		allowed                            []string
		want                               int
	}{
		{name: "同源", origin: "https://admin.example.com", host: "admin.example.com", tls: true, cookie: true, want: 204},
		{name: "默认端口", origin: "https://admin.example.com:443", host: "admin.example.com", tls: true, cookie: true, want: 204},
		{name: "反向代理", origin: "https://admin.example.com", host: "admin.example.com", forwardedProto: "https", cookie: true, want: 204},
		{name: "不同域名", origin: "https://other.example.com", host: "admin.example.com", tls: true, cookie: true, want: 403},
		{name: "不同端口", origin: "https://admin.example.com:444", host: "admin.example.com", tls: true, cookie: true, want: 403},
		{name: "不同协议", origin: "http://admin.example.com", host: "admin.example.com", tls: true, cookie: true, want: 403},
		{name: "配置允许源", origin: "http://localhost:5173", host: "localhost:8080", cookie: true, allowed: []string{"http://localhost:5173"}, want: 204},
		{name: "无Cookie的脚本", origin: "http://localhost:5173", host: "localhost:8080", want: 204},
		{name: "无Origin的脚本", host: "localhost:8080", cookie: true, want: 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.POST("/settings", CSRFGuard(tc.allowed...), func(c *gin.Context) { c.Status(204) })
			req := httptest.NewRequest(http.MethodPost, "http://"+tc.host+"/settings", nil)
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("X-Forwarded-Proto", tc.forwardedProto)
			if tc.tls {
				req.TLS = &tls.ConnectionState{}
			}
			if tc.cookie {
				req.AddCookie(&http.Cookie{Name: AdminCookieName, Value: "session"})
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("状态码 = %d，期望 %d", rec.Code, tc.want)
			}
		})
	}
}
