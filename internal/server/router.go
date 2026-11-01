// Package server 负责路由装配与 HTTP 服务生命周期。
package server

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/starschedule/server/internal/config"
	"github.com/starschedule/server/internal/handler"
	"github.com/starschedule/server/internal/metrics"
	"github.com/starschedule/server/internal/middleware"
	"github.com/starschedule/server/internal/webui"
)

func NewRouter(d *handler.Deps, cfg *config.Config) *gin.Engine {
	if !cfg.IsDev() {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(
		middleware.RequestID(),
		middleware.ServerTime(),
		middleware.AccessLog(d.Logger),
		middleware.Recovery(d.Logger),
		metrics.Middleware(),
	)
	if len(cfg.CORSOrigins) > 0 {
		r.Use(middleware.CORS(cfg.CORSOrigins))
	}

	r.GET("/healthz", d.Healthz)
	r.GET("/readyz", d.Readyz)
	r.GET("/version", d.VersionInfo)
	if cfg.MetricsEnabled {
		r.GET("/metrics", metrics.Handler())
	}
	webui.Register(r, "/admin")

	registerLimit := middleware.RateLimit(d.Redis, middleware.LimitRule{
		Name:   "register",
		Window: time.Hour,
		Limit: func(c *gin.Context) int {
			return d.Settings.Get(c.Request.Context()).RegisterPerIPHour
		},
		Key: middleware.ClientIPKey,
	})
	launchLimit := middleware.RateLimit(d.Redis, middleware.LimitRule{
		Name:   "launch",
		Window: time.Minute,
		Limit:  func(c *gin.Context) int { return 120 },
		Key:    middleware.DeviceKey,
	})
	readLimit := middleware.RateLimit(d.Redis, middleware.LimitRule{
		Name:   "read",
		Window: time.Minute,
		Limit:  func(c *gin.Context) int { return 120 },
		Key:    middleware.DeviceKey,
	})
	adminLoginLimit := middleware.RateLimit(d.Redis, middleware.LimitRule{
		Name:   "admin_login",
		Window: time.Minute,
		Limit:  func(c *gin.Context) int { return 5 },
		Key:    middleware.ClientIPKey,
	})

	v1 := r.Group("/api/v1")

	// 设备侧
	v1.POST("/devices/register", registerLimit, d.RegisterDevice)

	device := v1.Group("", middleware.DeviceAuth(d.Device))
	device.GET("/devices/me", readLimit, d.Me)
	device.POST("/launches", launchLimit, d.ReportLaunch)
	device.GET("/ai/models", readLimit, d.AIModels)
	device.GET("/ai/config", readLimit, d.AIConfig)
	device.GET("/ai/usage", readLimit, d.AIUsage)
	device.POST("/ai/chat/completions", d.AIChat)

	// 管理端
	admin := v1.Group("/admin")
	admin.POST("/login", adminLoginLimit, d.AdminLogin)

	authed := admin.Group("", middleware.AdminAuth(d.JWT), middleware.CSRFGuard())
	authed.POST("/logout", d.AdminLogout)
	authed.GET("/me", d.AdminMe)

	authed.GET("/devices", d.AdminListDevices)
	authed.GET("/devices/:id", d.AdminDeviceDetail)
	authed.POST("/devices/:id/ban", d.AdminSetDeviceBan)

	authed.GET("/stats/overview", d.AdminStatsOverview)
	authed.GET("/stats/trend", d.AdminStatsTrend)
	authed.GET("/stats/distribution", d.AdminStatsDistribution)

	authed.GET("/ai/providers", d.AdminListProviders)
	authed.POST("/ai/providers", d.AdminCreateProvider)
	authed.PATCH("/ai/providers/:id", d.AdminUpdateProvider)
	authed.DELETE("/ai/providers/:id", d.AdminDeleteProvider)
	authed.POST("/ai/providers/:id/test", d.AdminTestProvider)
	authed.POST("/ai/providers/:id/sync-models", d.AdminSyncModels)

	authed.GET("/ai/models", d.AdminListModels)
	authed.GET("/ai/catalog", d.AdminCatalog)
	authed.POST("/ai/models", d.AdminCreateModel)
	authed.PATCH("/ai/models/:id", d.AdminUpdateModel)
	authed.PUT("/ai/models/availability", d.AdminSetModelAvailability)
	authed.POST("/ai/models/bulk-delete", d.AdminBulkDeleteModels)
	authed.DELETE("/ai/models/:id", d.AdminDeleteModel)
	authed.GET("/ai/active", d.AdminGetActive)
	authed.POST("/ai/playground", d.AIPlayground)
	authed.GET("/ai/usage", d.AdminAIUsage)

	authed.GET("/settings", d.AdminGetSettings)
	authed.PUT("/settings", d.AdminUpdateSettings)
	authed.GET("/audit", d.AdminAuditLogs)

	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{
			"code": "NOT_FOUND", "message": "接口不存在", "request_id": c.GetString("request_id"),
		}})
	})

	return r
}
