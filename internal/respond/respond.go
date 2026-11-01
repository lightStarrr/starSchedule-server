// Package respond 统一 HTTP 响应与错误码。
package respond

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// 稳定错误码：客户端按 code 判断，不解析 message。
const (
	CodeInvalidArgument    = "INVALID_ARGUMENT"
	CodeModelNotAvailable  = "MODEL_NOT_AVAILABLE"
	CodeUnauthenticated    = "UNAUTHENTICATED"
	CodeTokenExpired       = "TOKEN_EXPIRED"
	CodeTokenInvalid       = "TOKEN_INVALID"
	CodeDeviceBanned       = "DEVICE_BANNED"
	CodeNotFound           = "NOT_FOUND"
	CodePayloadTooLarge    = "PAYLOAD_TOO_LARGE"
	CodeRateLimited        = "RATE_LIMITED"
	CodeAIQuotaExceeded    = "AI_QUOTA_EXCEEDED"
	CodeAINotConfigured    = "AI_NOT_CONFIGURED"
	CodeQuotaUnavailable   = "QUOTA_SERVICE_UNAVAILABLE"
	CodeUpstreamError      = "UPSTREAM_ERROR"
	CodeUpstreamTimeout    = "UPSTREAM_TIMEOUT"
	CodeServiceUnavailable = "SERVICE_UNAVAILABLE"
	CodeInternal           = "INTERNAL"
)

const RequestIDKey = "request_id"

type errorDetail struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	RequestID string         `json:"request_id"`
	Details   map[string]any `json:"details,omitempty"`
}

type errorEnvelope struct {
	Error errorDetail `json:"error"`
}

// RequestID 从上下文取请求 ID（中间件写入）。
func RequestID(c *gin.Context) string {
	if v, ok := c.Get(RequestIDKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// Error 输出统一错误体。
func Error(c *gin.Context, status int, code, message string, details map[string]any) {
	c.AbortWithStatusJSON(status, errorEnvelope{Error: errorDetail{
		Code:      code,
		Message:   message,
		RequestID: RequestID(c),
		Details:   details,
	}})
}

func InvalidArgument(c *gin.Context, message string) {
	Error(c, http.StatusBadRequest, CodeInvalidArgument, message, nil)
}

func Unauthenticated(c *gin.Context, message string) {
	Error(c, http.StatusUnauthorized, CodeUnauthenticated, message, nil)
}

// BindJSON 解析请求体：超过中间件设置的上限返回 413，其余解析/校验失败返回 400。
// 返回 false 时调用方应直接 return。
func BindJSON(c *gin.Context, obj any) bool {
	if err := c.ShouldBindJSON(obj); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			Error(c, http.StatusRequestEntityTooLarge, CodePayloadTooLarge,
				fmt.Sprintf("请求体超过 %d 字节上限", tooLarge.Limit), nil)
			return false
		}
		InvalidArgument(c, "参数校验失败："+err.Error())
		return false
	}
	return true
}

func Internal(c *gin.Context, message string) {
	Error(c, http.StatusInternalServerError, CodeInternal, message, nil)
}

func NotFound(c *gin.Context, message string) {
	Error(c, http.StatusNotFound, CodeNotFound, message, nil)
}

// CursorPage 列表统一分页结构。
type CursorPage[T any] struct {
	Items         []T    `json:"items"`
	NextCursor    string `json:"next_cursor"`
	HasMore       bool   `json:"has_more"`
	TotalEstimate int64  `json:"total_estimate,omitempty"`
}
