package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/starschedule/server/internal/dto"
	"github.com/starschedule/server/internal/middleware"
	"github.com/starschedule/server/internal/respond"
	"github.com/starschedule/server/internal/service"
)

func serverNow() time.Time { return time.Now().UTC() }

// RegisterDevice POST /api/v1/devices/register
func (d *Deps) RegisterDevice(c *gin.Context) {
	var req dto.RegisterRequest
	if !respond.BindJSON(c, &req) {
		return
	}

	// 同一 install_id 强制限流，防止脚本刷注册。
	if allowed, retry, err := d.Redis.AllowFixedWindow(c.Request.Context(),
		"rl:register:install:"+req.InstallID, 5, time.Minute); err == nil && !allowed {
		c.Header("Retry-After", strconv.Itoa(retry))
		respond.Error(c, http.StatusTooManyRequests, respond.CodeRateLimited,
			"注册过于频繁，请稍后再试", map[string]any{"retry_after": retry})
		return
	}

	res, err := d.Device.Register(c.Request.Context(), service.RegisterInput{
		InstallID:   req.InstallID,
		AndroidID:   req.AndroidID,
		Platform:    req.Platform,
		AppVersion:  req.AppVersion,
		VersionCode: req.VersionCode,
		OSVersion:   req.OSVersion,
		SDKInt:      req.SDKInt,
		Arch:        req.Arch,
		Locale:      req.Locale,
		Timezone:    req.Timezone,
		IP:          c.ClientIP(),
	})
	if err != nil {
		if errors.Is(err, service.ErrDeviceBanned) {
			respond.Error(c, http.StatusForbidden, respond.CodeDeviceBanned, "该设备已被禁用", nil)
			return
		}
		d.Logger.Error("注册设备失败", "error", err, "request_id", respond.RequestID(c))
		respond.Internal(c, "注册失败，请稍后重试")
		return
	}

	c.JSON(http.StatusOK, dto.RegisterResponse{
		DeviceID:    res.Device.ID.String(),
		AccessToken: res.Token,
		TokenType:   "Bearer",
		ExpiresIn:   int(time.Until(res.ExpiresAt).Seconds()),
		ExpiresAt:   res.ExpiresAt.UTC(),
		IsNew:       res.IsNew,
		ServerTime:  serverNow(),
	})
}

// Me GET /api/v1/devices/me
func (d *Deps) Me(c *gin.Context) {
	dev := middleware.DeviceFrom(c)
	if dev == nil {
		respond.Unauthenticated(c, "缺少设备上下文")
		return
	}
	today := d.Device.Today()
	usage, err := d.Store.UsageOn(c.Request.Context(), today, dev.ID)
	if err != nil {
		d.Logger.Warn("读取用量失败", "error", err)
	}
	settings := d.Settings.Get(c.Request.Context())

	c.JSON(http.StatusOK, gin.H{
		"device_id":         dev.ID.String(),
		"status":            dev.Status,
		"platform":          dev.Platform,
		"app_version":       dev.AppVersion,
		"first_seen_at":     dev.FirstSeenAt.UTC(),
		"last_seen_at":      dev.LastSeenAt.UTC(),
		"launch_count":      dev.LaunchCount,
		"suspect_reinstall": dev.SuspectReinstall,
		"ai": gin.H{
			"used_tokens": usage.PromptTokens + usage.CompletionTokens,
			"daily_quota": settings.DailyTokenQuota,
			"reset_at":    d.resetAt().Format(time.RFC3339),
		},
		"limits": gin.H{
			"rpm":      settings.RPMPerDevice,
			"inflight": settings.InflightPerDevice,
		},
		"server_time": serverNow(),
	})
}

func (d *Deps) resetAt() time.Time {
	now := time.Now().In(d.Loc)
	return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, d.Loc)
}
