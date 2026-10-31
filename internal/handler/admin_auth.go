package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/starschedule/server/internal/dto"
	"github.com/starschedule/server/internal/middleware"
	"github.com/starschedule/server/internal/respond"
	"github.com/starschedule/server/internal/service"
)

type adminProfile struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// AdminLogin POST /api/v1/admin/login
func (d *Deps) AdminLogin(c *gin.Context) {
	var req dto.AdminLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.InvalidArgument(c, "参数校验失败："+err.Error())
		return
	}
	res, err := d.Admin.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			respond.Error(c, http.StatusUnauthorized, respond.CodeUnauthenticated, "用户名或密码错误", nil)
			return
		}
		d.Logger.Error("管理员登录失败", "error", err)
		respond.Internal(c, "登录失败，请稍后重试")
		return
	}

	ttl := int(time.Until(res.ExpiresAt).Seconds())
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(middleware.AdminCookieName, res.Token, ttl, "/", "", !d.AllowInsecureCookie, true)

	d.Admin.Audit(c.Request.Context(), res.Admin, "admin.login", "admin", res.Admin.ID.String(),
		map[string]any{"username": res.Admin.Username}, c.ClientIP())

	c.JSON(http.StatusOK, gin.H{
		"token":      res.Token,
		"expires_at": res.ExpiresAt.UTC(),
		"admin": adminProfile{
			ID:       res.Admin.ID.String(),
			Username: res.Admin.Username,
			Role:     res.Admin.Role,
		},
	})
}

// AdminLogout POST /api/v1/admin/logout
func (d *Deps) AdminLogout(c *gin.Context) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(middleware.AdminCookieName, "", -1, "/", "", !d.AllowInsecureCookie, true)
	c.Status(http.StatusNoContent)
}

// AdminMe GET /api/v1/admin/me
func (d *Deps) AdminMe(c *gin.Context) {
	admin := adminFromAny(c)
	if admin == nil {
		respond.Unauthenticated(c, "请先登录")
		return
	}
	c.JSON(http.StatusOK, adminProfile{ID: admin.ID.String(), Username: admin.Username, Role: admin.Role})
}
