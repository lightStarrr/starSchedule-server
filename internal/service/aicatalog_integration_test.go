package service_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/starschedule/server/internal/crypto"
	"github.com/starschedule/server/internal/migrate"
	"github.com/starschedule/server/internal/service"
	"github.com/starschedule/server/internal/store/postgres"
	redisc "github.com/starschedule/server/internal/store/redis"
)

// 集成测试：需要真实的 PostgreSQL 与 Redis。
//
//	TEST_DATABASE_URL=postgres://... TEST_REDIS_ADDR=localhost:6379 go test ./internal/service/...
//
// 未设置环境变量时跳过，保证本地 `go test ./...` 不依赖外部服务。
func TestAICatalogIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	redisAddr := os.Getenv("TEST_REDIS_ADDR")
	if dsn == "" || redisAddr == "" {
		t.Skip("未设置 TEST_DATABASE_URL / TEST_REDIS_ADDR，跳过集成测试")
	}

	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	pool, err := postgres.NewPool(ctx, dsn, 4, 1)
	if err != nil {
		t.Fatalf("连接数据库失败: %v", err)
	}
	defer pool.Close()

	if err := migrate.Run(ctx, pool, logger); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	st := postgres.New(pool)
	rd := redisc.New(redisAddr, os.Getenv("TEST_REDIS_PASSWORD"), 0)
	defer rd.Close()

	key := make([]byte, 32)
	for i := range key {
		key[i] = 7
	}
	box, err := crypto.New(key)
	if err != nil {
		t.Fatalf("初始化加密器失败: %v", err)
	}
	cat := service.NewAICatalog(st, rd, box)

	// 清掉可能残留的默认模型设置，保证从「没有默认」开始
	_ = st.DeleteSetting(ctx, "ai.active_provider")
	_ = st.DeleteSetting(ctx, "ai.active_model")
	cat.Invalidate(ctx)

	suffix := uuid.NewString()[:8]
	provider, err := cat.CreateProvider(ctx, service.ProviderPayload{
		Name:           "it-" + suffix,
		BaseURL:        "https://example.invalid/v1",
		APIKey:         "sk-test-" + suffix,
		Enabled:        true,
		TimeoutSeconds: 10,
	})
	if err != nil {
		t.Fatalf("创建供应商失败: %v", err)
	}
	t.Cleanup(func() {
		_ = st.DeleteProvider(context.Background(), provider.ID)
		_ = st.DeleteSetting(context.Background(), "ai.active_provider")
		_ = st.DeleteSetting(context.Background(), "ai.active_model")
		cat.Invalidate(context.Background())
	})

	mkModel := func(modelID string) *postgresModel {
		t.Helper()
		m, err := st.CreateModel(ctx, postgres.ModelInput{
			ProviderID:    provider.ID,
			ModelID:       modelID,
			Alias:         modelID,
			DisplayName:   modelID,
			ContextWindow: 8192,
			Enabled:       true,
		})
		if err != nil {
			t.Fatalf("创建模型 %s 失败: %v", modelID, err)
		}
		return &postgresModel{ID: m.ID, ModelID: m.ModelID, Alias: m.Alias}
	}
	first := mkModel("aaa-" + suffix)
	second := mkModel("bbb-" + suffix)
	cat.Invalidate(ctx)

	t.Run("没有显式默认时回退到第一个可用模型", func(t *testing.T) {
		snap, err := cat.Snapshot(ctx)
		if err != nil {
			t.Fatalf("Snapshot 失败: %v", err)
		}
		if snap.ModelUUID != first.ID {
			t.Fatalf("默认模型应为 %s，实际 %s", first.ModelID, snap.ModelID)
		}
	})

	t.Run("同一模型在第二家供应商下默认不启用", func(t *testing.T) {
		other, err := cat.CreateProvider(ctx, service.ProviderPayload{
			Name:           "it-other-" + suffix,
			BaseURL:        "https://example.invalid/v1",
			APIKey:         "sk-test-other-" + suffix,
			Enabled:        true,
			TimeoutSeconds: 10,
		})
		if err != nil {
			t.Fatalf("创建第二家供应商失败: %v", err)
		}
		t.Cleanup(func() { _ = st.DeleteProvider(context.Background(), other.ID) })

		dup, err := st.CreateModel(ctx, postgres.ModelInput{
			ProviderID:    other.ID,
			ModelID:       first.ModelID,
			Alias:         "dup-" + first.ModelID,
			DisplayName:   "dup",
			ContextWindow: 8192,
			Enabled:       true,
		})
		if err != nil {
			t.Fatalf("创建同名模型失败: %v", err)
		}
		if dup.Enabled {
			t.Fatal("同一模型已有启用的供应商，新行不应再启用")
		}

		entries, err := cat.ListEnabled(ctx)
		if err != nil {
			t.Fatalf("ListEnabled 失败: %v", err)
		}
		count := 0
		for _, e := range entries {
			if e.ModelID == first.ModelID {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("同一模型只应有一条启用记录，实际 %d 条", count)
		}
	})

	t.Run("按别名解析并带出思考能力", func(t *testing.T) {
		mode := "effort"
		efforts := []string{"low", "medium", "high"}
		def := "medium"
		if _, err := st.UpdateModel(ctx, first.ID, postgres.ModelPatch{
			ReasoningMode:    &mode,
			ReasoningEfforts: &efforts,
			ReasoningDefault: &def,
		}); err != nil {
			t.Fatalf("写入思考能力失败: %v", err)
		}
		cat.Invalidate(ctx)

		snap, err := cat.SnapshotByAlias(ctx, first.Alias)
		if err != nil {
			t.Fatalf("SnapshotByAlias 失败: %v", err)
		}
		if snap.ModelUUID != first.ID {
			t.Fatalf("别名解析到了错误的模型: %s", snap.ModelID)
		}
		if snap.ReasoningMode != "effort" || snap.ReasoningDefault != "medium" || len(snap.ReasoningEfforts) != 3 {
			t.Fatalf("思考能力没有带出来: mode=%q default=%q efforts=%v",
				snap.ReasoningMode, snap.ReasoningDefault, snap.ReasoningEfforts)
		}
	})

	t.Run("alias 与其他模型的上游名称冲突时仍精确定位", func(t *testing.T) {
		alias := "000-" + suffix
		if _, err := st.UpdateModel(ctx, first.ID, postgres.ModelPatch{Alias: &alias}); err != nil {
			t.Fatal(err)
		}
		first.Alias = alias
		if _, err := st.CreateModel(ctx, postgres.ModelInput{ProviderID: provider.ID, ModelID: alias, Alias: "other-" + suffix, Enabled: true}); err != nil {
			t.Fatal(err)
		}
		snap, err := cat.SnapshotByAlias(ctx, alias)
		if err != nil || snap.ModelUUID != first.ID {
			t.Fatalf("alias 不应匹配到另一模型的 model_id: %+v %v", snap, err)
		}
		if _, err := cat.SnapshotByAlias(ctx, first.ModelID); !errors.Is(err, service.ErrModelNotFound) {
			t.Fatalf("不能接受上游 model_id: %v", err)
		}
	})

	t.Run("停用供应商后模型不可用且从管理列表消失", func(t *testing.T) {
		disabled := false
		if _, err := st.UpdateProvider(ctx, provider.ID, postgres.ProviderPatch{Enabled: &disabled}); err != nil {
			t.Fatalf("停用供应商失败: %v", err)
		}
		cat.Invalidate(ctx)

		if _, err := cat.Snapshot(ctx); !errors.Is(err, service.ErrAINotConfigured) {
			t.Fatalf("期望 ErrAINotConfigured，实际 %v", err)
		}
		if _, err := cat.SnapshotByAlias(ctx, first.Alias); !errors.Is(err, service.ErrModelNotFound) {
			t.Fatalf("停用供应商的模型应不可解析，实际 %v", err)
		}
		// 管理端列表按供应商启用状态过滤
		entries, err := st.ListAllModels(ctx)
		if err != nil {
			t.Fatalf("查询模型列表失败: %v", err)
		}
		for _, e := range entries {
			if e.ProviderID == provider.ID {
				t.Fatalf("停用供应商的模型不应出现在列表中: %s", e.ModelID)
			}
		}

		enabled := true
		if _, err := st.UpdateProvider(ctx, provider.ID, postgres.ProviderPatch{Enabled: &enabled}); err != nil {
			t.Fatalf("恢复供应商失败: %v", err)
		}
		cat.Invalidate(ctx)
	})

	t.Run("绑定默认模型后优先使用默认", func(t *testing.T) {
		if err := st.SetActive(ctx, provider.ID, second.ID); err != nil {
			t.Fatalf("设置默认模型失败: %v", err)
		}
		cat.Invalidate(ctx)
		snap, err := cat.Snapshot(ctx)
		if err != nil {
			t.Fatalf("Snapshot 失败: %v", err)
		}
		if snap.ModelUUID != second.ID {
			t.Fatalf("应使用显式默认模型 %s，实际 %s", second.ModelID, snap.ModelID)
		}
	})
}

// postgresModel 只保留断言需要的字段，避免测试与模型结构强耦合。
type postgresModel struct {
	ID      uuid.UUID
	ModelID string
	Alias   string
}
