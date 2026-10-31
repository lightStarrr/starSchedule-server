// Package middleware 提供请求 ID、日志、鉴权、限流等横切能力。
package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/starschedule/server/internal/respond"
)

const (
	ctxDevice = "ctx.device"
	ctxAdmin  = "ctx.admin"
	ctxClaims = "ctx.device.claims"
)

// RequestID 透传或生成请求 ID，并回写响应头。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := strings.TrimSpace(c.GetHeader("X-Request-Id"))
		if rid == "" {
			rid = uuid.Must(uuid.NewV7()).String()
		}
		c.Set(respond.RequestIDKey, rid)
		c.Header("X-Request-Id", rid)
		c.Next()
	}
}

// ServerTime 让 Android 客户端可以校正本地时钟。
func ServerTime() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Server-Time", time.Now().UTC().Format(time.RFC3339))
		c.Next()
	}
}

// AccessLog 输出结构化访问日志（SSE 请求跳过，避免长时间占用后重复记录）。
func AccessLog(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		logger.LogAttrs(c.Request.Context(), slog.LevelInfo, "http_request",
			slog.String("method", c.Request.Method),
			slog.String("path", path),
			slog.Int("status", c.Writer.Status()),
			slog.Int64("latency_ms", time.Since(start).Milliseconds()),
			slog.String("request_id", c.GetString(respond.RequestIDKey)),
			slog.String("ip", c.ClientIP()),
			slog.String("ua", c.Request.UserAgent()),
		)
	}
}

func Recovery(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("panic", "error", r, "stack", string(debug.Stack()),
					"path", c.Request.URL.Path, "request_id", c.GetString(respond.RequestIDKey))
				respond.Internal(c, "服务内部错误")
			}
		}()
		c.Next()
	}
}

// CORS 仅在开发态放行 Vite 调试源。
func CORS(origins []string) gin.HandlerFunc {
	allowed := map[string]bool{}
	for _, o := range origins {
		allowed[strings.TrimRight(o, "/")] = true
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && allowed[origin] {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Headers", "Authorization,Content-Type,X-Request-Id,X-Client-Version,X-Client-Version-Code,X-Requested-With")
			c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
			c.Header("Access-Control-Expose-Headers", "X-Request-Id,X-Server-Time,X-Refresh-Token")
			c.Header("Vary", "Origin")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
