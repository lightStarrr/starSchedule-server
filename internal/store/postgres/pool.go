// Package postgres 提供 PostgreSQL 连接池与各领域 SQL。
package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool 建立连接池，带指数退避重试（容器启动顺序不完全可靠）。
func NewPool(ctx context.Context, dsn string, maxConns, minConns int32) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("解析 DATABASE_URL: %w", err)
	}
	cfg.MaxConns = maxConns
	cfg.MinConns = minConns
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 30 * time.Minute
	cfg.HealthCheckPeriod = time.Minute

	var lastErr error
	backoff := 500 * time.Millisecond
	for attempt := 1; attempt <= 10; attempt++ {
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		if err == nil {
			pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err = pool.Ping(pingCtx)
			cancel()
			if err == nil {
				return pool, nil
			}
			pool.Close()
		}
		lastErr = err
		slog.Warn("数据库连接失败，准备重试", "attempt", attempt, "error", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 10*time.Second {
			backoff *= 2
		}
	}
	return nil, fmt.Errorf("连接数据库失败: %w", lastErr)
}

// EnsurePartitions 为当月及后续 months 个月创建分区，并清理过期 DEFAULT 压力。
func EnsurePartitions(ctx context.Context, pool *pgxpool.Pool, loc *time.Location, base time.Time, months int) error {
	tables := []struct {
		parent string
		prefix string
		column string
	}{
		{"launch_events", "launch_events", "stat_date"},
		{"ai_request_log", "ai_request_log", "stat_date"},
	}
	for _, t := range tables {
		start := time.Date(base.In(loc).Year(), base.In(loc).Month(), 1, 0, 0, 0, 0, loc)
		for i := 0; i < months; i++ {
			from := start.AddDate(0, i, 0)
			to := from.AddDate(0, 1, 0)
			name := fmt.Sprintf("%s_%s", t.prefix, from.Format("2006_01"))
			stmt := fmt.Sprintf(
				`CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM ('%s') TO ('%s')`,
				name, t.parent, from.Format("2006-01-02"), to.Format("2006-01-02"),
			)
			if _, err := pool.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("创建分区 %s: %w", name, err)
			}
		}
	}
	return nil
}

// Ping 健康检查。
func Ping(ctx context.Context, pool *pgxpool.Pool) error {
	return pool.Ping(ctx)
}
