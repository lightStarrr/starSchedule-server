package postgres

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/starschedule/server/internal/migrate"
)

func TestUnlimitedMigrationIntegration(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	// 还原到本轮迁移前的配置与 schema，验证旧的 0 明确迁移成布尔开关。
	if _, err := s.pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version='000004_model_alias_and_quota';
 DROP INDEX ai_models_one_enabled_provider; DROP INDEX ai_models_enabled_alias;
 ALTER TABLE devices DROP COLUMN metadata_reported_at;
 DELETE FROM app_settings WHERE key='ai.quota_unlimited';
 UPDATE app_settings SET value='0'::jsonb WHERE key='ai.daily_token_quota'`); err != nil {
		t.Fatal(err)
	}
	if err := migrate.Run(ctx, s.pool, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	settings, err := s.GetRuntimeSettings(ctx, RuntimeSettings{})
	if err != nil {
		t.Fatal(err)
	}
	if !settings.QuotaUnlimited || settings.DailyTokenQuota != 200000 {
		t.Fatalf("旧的不限量未转换: %+v", settings)
	}
	if err := s.SetSettings(ctx, map[string]any{"ai.quota_unlimited": false}); err != nil {
		t.Fatal(err)
	}
	settings, err = s.GetRuntimeSettings(ctx, RuntimeSettings{})
	if err != nil || settings.QuotaUnlimited {
		t.Fatalf("关闭开关应恢复限制: %+v %v", settings, err)
	}
}
