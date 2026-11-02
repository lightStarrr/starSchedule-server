package handler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/starschedule/server/internal/aiproxy"
	"github.com/starschedule/server/internal/crypto"
	"github.com/starschedule/server/internal/migrate"
	"github.com/starschedule/server/internal/service"
	"github.com/starschedule/server/internal/store/postgres"
	redisc "github.com/starschedule/server/internal/store/redis"
)

func integrationDeps(t *testing.T) *Deps {
	t.Helper()
	dsn, addr := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_REDIS_ADDR")
	if dsn == "" || addr == "" {
		t.Skip("未设置 TEST_DATABASE_URL / TEST_REDIS_ADDR，跳过集成测试")
	}
	ctx := context.Background()
	root, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(root.Close)
	schema := "it_handler_" + uuid.NewString()[:8]
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
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := migrate.Run(ctx, pool, logger); err != nil {
		t.Fatal(err)
	}
	st := postgres.New(pool)
	rd := redisc.New(addr, os.Getenv("TEST_REDIS_PASSWORD"), 0)
	t.Cleanup(func() { _ = rd.Close() })
	if err := rd.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	settings := service.NewSettingsService(st, rd, postgres.RuntimeSettings{})
	if err := settings.Update(ctx, map[string]any{
		"ai.daily_token_quota": 10000, "ai.max_output_tokens": 100,
		"ai.inflight_per_device": 0, "ai.rpm_per_device": 0,
	}); err != nil {
		t.Fatal(err)
	}
	box, err := crypto.New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	catalog := service.NewAICatalog(st, rd, box)
	catalog.Invalidate(ctx)
	t.Cleanup(func() { catalog.Invalidate(ctx); settings.Invalidate(ctx) })
	return &Deps{
		Store: st, Redis: rd, Settings: settings, Catalog: catalog,
		Device: service.NewDeviceService(st, rd, nil, "test", time.UTC, settings),
		AI:     aiproxy.NewClient(), Logger: logger, Loc: time.UTC,
	}
}

func TestAIChatIntegration(t *testing.T) {
	d := integrationDeps(t)
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			w.WriteHeader(http.StatusServiceUnavailable)
		case 2:
			_, _ = io.WriteString(w, "invalid JSON")
		case 3:
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"你好\"}}]}\n\n")
		case 4:
			_, _ = io.WriteString(w, `{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`)
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}
	}))
	t.Cleanup(upstream.Close)
	p, err := d.Catalog.CreateProvider(context.Background(), service.ProviderPayload{
		Name: "test", BaseURL: upstream.URL, APIKey: "test", Enabled: true, TimeoutSeconds: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := d.Store.CreateModel(context.Background(), postgres.ModelInput{ProviderID: p.ID, ModelID: "test", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	deviceID := uuid.New()
	date := d.Device.Today()
	quotaKey := redisc.QuotaKey(deviceID.String(), date.Format("2006-01-02"))
	t.Cleanup(func() { _ = d.Redis.Del(context.Background(), quotaKey) })
	caller := chatCaller{DeviceID: &deviceID, Source: "device", RateKey: deviceID.String(), ApplyQuota: true, AllowModelOverride: true}
	router := gin.New()
	router.POST("/chat", func(c *gin.Context) {
		d.handleChat(c, caller, &aiproxy.ChatRequest{
			Model: m.Alias, Messages: []aiproxy.Message{{Role: "user", Content: "hello"}}, Stream: c.Query("stream") == "true",
		})
	})
	for _, tc := range []struct {
		name   string
		stream bool
		status int
	}{
		{"上游失败", false, 502}, {"解析失败", false, 502},
		{"流式中断", true, 200}, {"正常完成", false, 200}, {"响应体超时", false, 504},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			started := time.Now()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/chat?stream=%t", tc.stream), nil))
			if rec.Code != tc.status {
				t.Fatalf("状态码 %d，期望 %d: %s", rec.Code, tc.status, rec.Body.String())
			}
			if tc.name == "响应体超时" {
				if time.Since(started) > 5*time.Second || !strings.Contains(rec.Body.String(), "UPSTREAM_TIMEOUT") {
					t.Fatalf("供应商超时应覆盖响应体读取: %s", rec.Body.String())
				}
			}
		})
	}
	usage, err := d.Store.UsageOn(context.Background(), date, deviceID)
	if err != nil {
		t.Fatal(err)
	}
	if usage.RequestCount != 5 || usage.ErrorCount != 4 || usage.CompletionTokens != 22 || usage.EstimatedCount != 1 {
		t.Fatalf("每次请求应且仅应结算一次: %+v", usage)
	}
	used, err := d.Redis.GetQuotaUsed(context.Background(), quotaKey)
	if err != nil {
		t.Fatal(err)
	}
	if used != usage.PromptTokens+usage.CompletionTokens {
		t.Fatalf("Redis 额度 %d 与数据库结算 %d 不一致", used, usage.PromptTokens+usage.CompletionTokens)
	}
}

func TestAdminSettingsValidationIntegration(t *testing.T) {
	d := integrationDeps(t)
	ctx := context.Background()
	if err := d.Settings.Update(ctx, map[string]any{"ai.rpm_per_device": 17}); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.PUT("/settings", d.AdminUpdateSettings)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(`{"values":{"ai.rpm_per_device":null,"ai.max_output_tokens":-1}}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("应拒绝非法配置，实际 %d", rec.Code)
	}
	raw, err := d.Store.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw["ai.rpm_per_device"]) != "17" {
		t.Fatal("校验失败不应提前删除已有配置")
	}
}
