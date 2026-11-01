package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/starschedule/server/internal/aiproxy"
	"github.com/starschedule/server/internal/auth"
	"github.com/starschedule/server/internal/config"
	"github.com/starschedule/server/internal/crypto"
	"github.com/starschedule/server/internal/handler"
	"github.com/starschedule/server/internal/job"
	"github.com/starschedule/server/internal/metrics"
	"github.com/starschedule/server/internal/migrate"
	"github.com/starschedule/server/internal/server"
	"github.com/starschedule/server/internal/service"
	"github.com/starschedule/server/internal/store/postgres"
	redisc "github.com/starschedule/server/internal/store/redis"
	"github.com/starschedule/server/internal/version"
)

func main() {
	migrateOnly := flag.Bool("migrate-only", false, "只执行数据库迁移后退出")
	showVersion := flag.Bool("version", false, "打印版本信息")
	flag.Parse()

	if *showVersion {
		fmt.Printf("starSchedule-server %s (commit %s, built %s)\n", version.Version, version.Commit, version.BuildTime)
		return
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "配置错误: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevelValue()}))
	slog.SetDefault(logger)
	logger.Info("启动服务", "version", version.Version, "env", cfg.Env)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL, cfg.DBMaxConns, cfg.DBMinConns)
	if err != nil {
		logger.Error("连接数据库失败", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := migrate.Run(ctx, pool, logger); err != nil {
		logger.Error("数据库迁移失败", "error", err)
		os.Exit(1)
	}
	if *migrateOnly {
		logger.Info("迁移完成，按 -migrate-only 退出")
		return
	}

	rd := redisc.New(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	defer rd.Close()
	if err := rd.Ping(ctx); err != nil {
		logger.Warn("Redis 暂不可用，相关能力将降级", "error", err)
	}

	key, err := cfg.DecodedEncryptionKey()
	if err != nil {
		logger.Error("加密密钥无效", "error", err)
		os.Exit(1)
	}
	box, err := crypto.New(key)
	if err != nil {
		logger.Error("初始化加密器失败", "error", err)
		os.Exit(1)
	}

	st := postgres.New(pool)
	jwtMgr := auth.NewManager(cfg.DeviceJWTSecret, cfg.AdminJWTSecret,
		cfg.DeviceTokenTTL, cfg.AdminTokenTTL, cfg.DeviceRefreshAfter)

	defaults := postgres.RuntimeSettings{
		DailyTokenQuota:     200000,
		RPMPerDevice:        20,
		InflightPerDevice:   2,
		GlobalConcurrency:   50,
		MaxOutputTokens:     4096,
		RegisterPerIPHour:   60,
		LaunchRetentionDays: 90,
		AILogRetentionDays:  30,
		ReasoningEffort:     "medium",
	}
	settings := service.NewSettingsService(st, rd, defaults)
	catalog := service.NewAICatalog(st, rd, box)

	if err := postgres.EnsurePartitions(ctx, pool, cfg.Location, time.Now(), 4); err != nil {
		logger.Warn("初始化分区失败", "error", err)
	}

	adminSvc := service.NewAdminService(st, jwtMgr)
	if created, err := adminSvc.SeedAdmin(ctx, cfg.AdminUsername, cfg.AdminPassword); err != nil {
		logger.Error("初始化管理员失败", "error", err)
	} else if created {
		logger.Info("已创建初始管理员", "username", cfg.AdminUsername)
		if cfg.AdminPassword == "changeme" {
			logger.Warn("管理员密码为默认值，请立即在环境中修改 ADMIN_PASSWORD 并重置账号")
		}
	}

	metrics.Register()

	deps := &handler.Deps{
		Store:    st,
		Redis:    rd,
		JWT:      jwtMgr,
		Device:   service.NewDeviceService(st, rd, jwtMgr, cfg.FingerprintPepper, cfg.Location, settings),
		Launch:   service.NewLaunchService(st, cfg.Location),
		Stats:    service.NewStatsService(st, cfg.Location),
		Admin:    adminSvc,
		Catalog:  catalog,
		Settings: settings,
		AI:       aiproxy.NewClient(),
		Logger:   logger,
		Loc:      cfg.Location,
		Version:  version.Version,

		AllowInsecureCookie: cfg.AllowInsecureCookie,
	}

	runner := job.NewRunner(st, settings, cfg.Location, logger)
	runner.Start(ctx)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           server.NewRouter(deps, cfg),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		IdleTimeout:       120 * time.Second,
		// 不设 WriteTimeout：AI 流式响应可能持续数分钟，超时由上游看门狗控制。
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("HTTP 服务已启动", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		logger.Error("HTTP 服务异常退出", "error", err)
		os.Exit(1)
	case <-ctx.Done():
		logger.Info("收到退出信号，开始优雅关停")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("优雅关停失败", "error", err)
	}
	logger.Info("已退出")
}
