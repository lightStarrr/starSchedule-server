package postgres

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/starschedule/server/internal/migrate"
)

// 每个用例在独立 schema 中运行，既不覆盖服务配置，也不与其他测试共享数据。
func integrationStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("未设置 TEST_DATABASE_URL，跳过数据库集成测试")
	}
	ctx := context.Background()
	root, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(root.Close)
	schema := "it_" + uuid.NewString()[:8]
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := root.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = root.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE") })
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := migrate.Run(ctx, pool, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	return New(pool)
}

func TestSettingsIntegration(t *testing.T) {
	st := integrationStore(t)
	ctx := context.Background()
	if err := st.SetSettings(ctx, map[string]any{"a": 10, "b": 20}); err != nil {
		t.Fatal(err)
	}
	// 删除执行后，后面的值序列化失败；整笔事务必须回滚。
	err := st.SetSettings(ctx, map[string]any{"a": nil, "b": make(chan int)})
	if err == nil {
		t.Fatal("期望序列化失败")
	}
	raw, err := st.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw["a"]) != "10" || string(raw["b"]) != "20" {
		t.Fatalf("失败请求不应改动任何配置: %v", raw)
	}
	if err := st.SetSettings(ctx, map[string]any{"a": nil, "b": 30}); err != nil {
		t.Fatal(err)
	}
	raw, err = st.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["a"]; ok || string(raw["b"]) != "30" {
		t.Fatalf("删除和更新应一起生效: %v", raw)
	}
}

func TestReasoningEffortsIntegration(t *testing.T) {
	st := integrationStore(t)
	ctx := context.Background()
	p, err := st.CreateProvider(ctx, ProviderInput{Name: "provider", BaseURL: "https://example.invalid", Enabled: true, APIKeyCipher: []byte("test"), TimeoutSeconds: 120})
	if err != nil {
		t.Fatal(err)
	}
	m, err := st.CreateModel(ctx, ModelInput{ProviderID: p.ID, ModelID: "model", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"fixed", "none"} {
		var efforts []string
		updated, err := st.UpdateModel(ctx, m.ID, ModelPatch{ReasoningMode: &mode, ReasoningEfforts: &efforts})
		if err != nil {
			t.Fatalf("保存 %s 失败: %v", mode, err)
		}
		if updated.ReasoningMode != mode || len(updated.ReasoningEfforts) != 0 {
			t.Fatalf("能力应保存为空数组: %+v", updated)
		}
		var raw json.RawMessage
		if err := st.pool.QueryRow(ctx, "SELECT to_json(reasoning_efforts) FROM ai_provider_models WHERE id=$1", m.ID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if string(raw) != "[]" {
			t.Fatalf("数据库应保存空数组，实际 %s", raw)
		}
	}
}

func TestUsageErrorCountIntegration(t *testing.T) {
	st := integrationStore(t)
	ctx := context.Background()
	date, deviceID := time.Now().UTC(), uuid.New()
	if err := st.IncrUsage(ctx, date, deviceID, 0, 0, false, true); err != nil {
		t.Fatal(err)
	}
	if err := st.IncrUsage(ctx, date, deviceID, 10, 20, false, false); err != nil {
		t.Fatal(err)
	}
	usage, err := st.UsageOn(ctx, date, deviceID)
	if err != nil {
		t.Fatal(err)
	}
	if usage.RequestCount != 2 || usage.ErrorCount != 1 || usage.PromptTokens+usage.CompletionTokens != 30 {
		t.Fatalf("成功与失败请求应分别累计: %+v", usage)
	}
}
