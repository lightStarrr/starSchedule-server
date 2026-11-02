package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/starschedule/server/internal/model"
	"github.com/starschedule/server/internal/service"
	"github.com/starschedule/server/internal/store/postgres"
)

func TestQuotaContractIntegration(t *testing.T) {
	d := integrationDeps(t)
	ctx := context.Background()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["model"] != "quota-model" {
			t.Errorf("应转发上游 model_id: %v %v", body, err)
		}
		_, _ = w.Write([]byte(`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`))
	}))
	t.Cleanup(upstream.Close)
	p, err := d.Catalog.CreateProvider(ctx, service.ProviderPayload{Name: "quota", BaseURL: upstream.URL, APIKey: "test", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Store.CreateModel(ctx, postgres.ModelInput{ProviderID: p.ID, ModelID: "quota-model", Alias: "android-alias", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	deviceID := uuid.New()
	router.Use(func(c *gin.Context) { c.Set("ctx.device", &model.Device{ID: deviceID}) })
	router.GET("/config", d.AIConfig)
	router.GET("/usage", d.AIUsage)
	router.POST("/chat", d.AIChat)
	d.Admin = service.NewAdminService(d.Store, nil)
	router.PUT("/settings", d.AdminUpdateSettings)
	for _, unlimited := range []bool{true, false} {
		raw, _ := json.Marshal(map[string]any{"values": map[string]any{"ai.quota_unlimited": unlimited, "ai.daily_token_quota": 100}})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("设置开关失败: %d %s", rec.Code, rec.Body.String())
		}
		for _, path := range []string{"/config", "/usage"} {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != 200 {
				t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			limits, usage := body, body
			if path == "/config" {
				limits = body["limits"].(map[string]any)
				usage = body["usage"].(map[string]any)
			}
			if limits["quota_unlimited"] != unlimited {
				t.Fatalf("开关不一致: %v", body)
			}
			if unlimited && usage["remaining_tokens"] != nil {
				t.Fatalf("不限量应返回 null: %v", body)
			}
			if !unlimited && usage["remaining_tokens"] != float64(100) {
				t.Fatalf("限制时应返回数值: %v", body)
			}
		}
	}
	if err := d.Settings.Update(ctx, map[string]any{"ai.quota_unlimited": true, "ai.daily_token_quota": 1}); err != nil {
		t.Fatal(err)
	}
	send := func(model string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(`{"model":"`+model+`","messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(rec, req)
		return rec
	}
	if rec := send("quota-model"); rec.Code != 400 {
		t.Fatalf("Android 不应接受上游 ID: %d %s", rec.Code, rec.Body.String())
	}
	if rec := send("android-alias"); rec.Code != 200 {
		t.Fatalf("不限量不应受数字额度限制: %d %s", rec.Code, rec.Body.String())
	}
	if err := d.Settings.Update(ctx, map[string]any{"ai.quota_unlimited": false}); err != nil {
		t.Fatal(err)
	}
	if rec := send("android-alias"); rec.Code != 429 {
		t.Fatalf("关闭不限量后应继续限制: %d %s", rec.Code, rec.Body.String())
	}
}

func TestInvalidQuotaSettings(t *testing.T) {
	d := &Deps{}
	router := gin.New()
	router.PUT("/settings", d.AdminUpdateSettings)
	for _, body := range []string{`{"values":{"ai.quota_unlimited":1}}`, `{"values":{"ai.quota_unlimited":"true"}}`, `{"values":{"ai.daily_token_quota":0}}`} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(rec, req)
		if rec.Code != 400 {
			t.Fatalf("应拒绝错误配置: %s: %d", body, rec.Code)
		}
	}
}
