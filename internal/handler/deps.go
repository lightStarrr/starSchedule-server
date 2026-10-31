// Package handler 组装 HTTP 处理器。
package handler

import (
	"log/slog"
	"time"

	"github.com/starschedule/server/internal/aiproxy"
	"github.com/starschedule/server/internal/auth"
	"github.com/starschedule/server/internal/service"
	"github.com/starschedule/server/internal/store/postgres"
	redisc "github.com/starschedule/server/internal/store/redis"
)

type Deps struct {
	Store    *postgres.Store
	Redis    *redisc.Client
	JWT      *auth.Manager
	Device   *service.DeviceService
	Launch   *service.LaunchService
	Stats    *service.StatsService
	Admin    *service.AdminService
	Catalog  *service.AICatalog
	Settings *service.SettingsService
	AI       *aiproxy.Client
	Logger   *slog.Logger
	Loc      *time.Location
	Version  string

	// AllowInsecureCookie 仅本地 http 调试时开启，生产必须为 false。
	AllowInsecureCookie bool
}
