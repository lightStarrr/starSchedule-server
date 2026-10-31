// Package job 运行后台定时任务：日汇总校正、分区维护、数据清理。
package job

import (
	"context"
	"log/slog"
	"time"

	"github.com/starschedule/server/internal/service"
	"github.com/starschedule/server/internal/store/postgres"
)

type Runner struct {
	store    *postgres.Store
	settings *service.SettingsService
	loc      *time.Location
	logger   *slog.Logger
}

func NewRunner(st *postgres.Store, settings *service.SettingsService, loc *time.Location, logger *slog.Logger) *Runner {
	return &Runner{store: st, settings: settings, loc: loc, logger: logger}
}

func (r *Runner) Start(ctx context.Context) {
	go r.loop(ctx)
}

func (r *Runner) loop(ctx context.Context) {
	rollup := time.NewTicker(10 * time.Minute)
	hourly := time.NewTicker(time.Hour)
	defer rollup.Stop()
	defer hourly.Stop()

	r.runRollup(ctx)
	r.runPartitions(ctx)

	lastPrune := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-rollup.C:
			r.runRollup(ctx)
		case <-hourly.C:
			r.runPartitions(ctx)
			if time.Since(lastPrune) > 12*time.Hour {
				r.runPrune(ctx)
				lastPrune = time.Now()
			}
		}
	}
}

func (r *Runner) runRollup(ctx context.Context) {
	today := postgres.Today(r.loc)
	for _, d := range []time.Time{today.AddDate(0, 0, -1), today} {
		runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		err := r.store.RebuildDailyStats(runCtx, d, r.loc.String())
		cancel()
		_ = r.store.TouchJob(ctx, "daily_rollup", err)
		if err != nil {
			r.logger.Error("日汇总失败", "date", d.Format("2006-01-02"), "error", err)
			return
		}
	}
}

func (r *Runner) runPartitions(ctx context.Context) {
	runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	err := postgres.EnsurePartitions(runCtx, r.store.Pool(), r.loc, time.Now(), 4)
	_ = r.store.TouchJob(ctx, "partitions", err)
	if err != nil {
		r.logger.Error("分区维护失败", "error", err)
	}
}

func (r *Runner) runPrune(ctx context.Context) {
	s := r.settings.Get(ctx)
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	err := r.store.PruneOldEvents(runCtx, s.LaunchRetentionDays, s.AILogRetentionDays)
	_ = r.store.TouchJob(ctx, "prune", err)
	if err != nil {
		r.logger.Error("清理过期数据失败", "error", err)
		return
	}
	r.logger.Info("已清理过期数据", "launch_days", s.LaunchRetentionDays, "ai_days", s.AILogRetentionDays)
}
