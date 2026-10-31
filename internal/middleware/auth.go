package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/starschedule/server/internal/auth"
	"github.com/starschedule/server/internal/model"
	"github.com/starschedule/server/internal/respond"
	"github.com/starschedule/server/internal/service"
)

func bearerToken(c *gin.Context) string {
	h := c.GetHeader("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

// DeviceAuth 校验设备 token，并把设备写入上下文。
func DeviceAuth(svc *service.DeviceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := bearerToken(c)
		if raw == "" {
			respond.Error(c, http.StatusUnauthorized, respond.CodeUnauthenticated, "缺少访问令牌", nil)
			return
		}
		claims, err := svc.JWT().ParseDevice(raw)
		if err != nil {
			if errors.Is(err, auth.ErrTokenExpired) {
				respond.Error(c, http.StatusUnauthorized, respond.CodeTokenExpired, "登录已过期，请重新注册", nil)
				return
			}
			respond.Error(c, http.StatusUnauthorized, respond.CodeTokenInvalid, "访问令牌无效", nil)
			return
		}
		deviceID, err := uuid.Parse(claims.Subject)
		if err != nil {
			respond.Error(c, http.StatusUnauthorized, respond.CodeTokenInvalid, "访问令牌无效", nil)
			return
		}

		ctx := c.Request.Context()
		dev, err := svc.Authorize(ctx, deviceID, claims.Ver)
		if err != nil {
			switch {
			case errors.Is(err, service.ErrDeviceBanned):
				respond.Error(c, http.StatusForbidden, respond.CodeDeviceBanned, "该设备已被禁用", nil)
			case errors.Is(err, service.ErrTokenStale):
				respond.Error(c, http.StatusUnauthorized, respond.CodeTokenInvalid, "访问令牌已失效，请重新注册", nil)
			default:
				respond.Internal(c, "服务暂时不可用")
			}
			return
		}

		if token, ok := svc.RefreshIfNeeded(ctx, claims); ok {
			c.Header("X-Refresh-Token", token)
		}

		c.Set(ctxDevice, dev)
		c.Set(ctxClaims, claims)
		c.Next()
	}
}

func DeviceFrom(c *gin.Context) *model.Device {
	if v, ok := c.Get(ctxDevice); ok {
		if d, ok := v.(*model.Device); ok {
			return d
		}
	}
	return nil
}

const AdminCookieName = "admin_session"

// AdminAuth 支持 Cookie（后台 UI）与 Bearer（脚本/curl）两种方式。
func AdminAuth(jwtMgr *auth.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, _ := c.Cookie(AdminCookieName)
		if raw == "" {
			raw = bearerToken(c)
		}
		if raw == "" {
			respond.Error(c, http.StatusUnauthorized, respond.CodeUnauthenticated, "请先登录", nil)
			return
		}
		claims, err := jwtMgr.ParseAdmin(raw)
		if err != nil {
			if errors.Is(err, auth.ErrTokenExpired) {
				respond.Error(c, http.StatusUnauthorized, respond.CodeTokenExpired, "登录已过期", nil)
				return
			}
			respond.Error(c, http.StatusUnauthorized, respond.CodeTokenInvalid, "登录凭证无效", nil)
			return
		}
		adminID, err := uuid.Parse(claims.Subject)
		if err != nil {
			respond.Error(c, http.StatusUnauthorized, respond.CodeTokenInvalid, "登录凭证无效", nil)
			return
		}
		c.Set(ctxAdmin, &model.AdminUser{ID: adminID, Username: claims.Username, Role: claims.Role})
		c.Next()
	}
}

func AdminFrom(c *gin.Context) *model.AdminUser {
	if v, ok := c.Get(ctxAdmin); ok {
		if a, ok := v.(*model.AdminUser); ok {
			return a
		}
	}
	return nil
}

// CSRFGuard 对使用 Cookie 会话的状态变更请求做 Origin 校验。
func CSRFGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
			c.Next()
			return
		}
		if _, err := c.Cookie(AdminCookieName); err != nil {
			c.Next() // 走 Bearer 的脚本请求不受影响
			return
		}
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}
		host := c.Request.Host
		if !strings.Contains(origin, host) {
			respond.Error(c, http.StatusForbidden, respond.CodeUnauthenticated, "请求来源校验失败", nil)
			return
		}
		c.Next()
	}
}
