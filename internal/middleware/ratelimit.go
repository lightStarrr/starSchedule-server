package middleware

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/starschedule/server/internal/respond"
	redisc "github.com/starschedule/server/internal/store/redis"
)

type LimitRule struct {
	Name   string
	Window time.Duration
	Limit  func(c *gin.Context) int
	Key    func(c *gin.Context) string
}

// RateLimit 固定窗口限流：Redis 异常时 fail-open，避免限流组件拖垮主链路。
func RateLimit(rd *redisc.Client, rule LimitRule) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := rule.Limit(c)
		if limit <= 0 {
			c.Next()
			return
		}
		key := "rl:" + rule.Name + ":" + rule.Key(c)
		allowed, retryAfter, err := rd.AllowFixedWindow(c.Request.Context(), key, limit, rule.Window)
		if err != nil {
			c.Next()
			return
		}
		c.Header("X-RateLimit-Limit", strconv.Itoa(limit))
		if !allowed {
			c.Header("X-RateLimit-Remaining", "0")
			c.Header("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Duration(retryAfter)*time.Second).Unix(), 10))
			c.Header("Retry-After", strconv.Itoa(retryAfter))
			respond.Error(c, http.StatusTooManyRequests, respond.CodeRateLimited,
				fmt.Sprintf("请求过于频繁，请 %d 秒后重试", retryAfter),
				map[string]any{"retry_after": retryAfter})
			return
		}
		c.Next()
	}
}

// ClientIPKey 以 IP 为主键（注册接口用，因 install_id 在 body 中）。
func ClientIPKey(c *gin.Context) string { return c.ClientIP() }

func DeviceKey(c *gin.Context) string {
	if d := DeviceFrom(c); d != nil {
		return d.ID.String()
	}
	return c.ClientIP()
}
