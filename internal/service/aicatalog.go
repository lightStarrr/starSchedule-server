package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/starschedule/server/internal/crypto"
	"github.com/starschedule/server/internal/model"
	"github.com/starschedule/server/internal/store"
	"github.com/starschedule/server/internal/store/postgres"
	redisc "github.com/starschedule/server/internal/store/redis"
)

// CatalogSnapshot 是转发链路需要的完整配置快照，整体缓存以支撑高频读取。
// APIKeyCipher 仍是密文，落 Redis 也安全。
type CatalogSnapshot struct {
	ProviderID       uuid.UUID `json:"provider_id"`
	ProviderName     string    `json:"provider_name"`
	BaseURL          string    `json:"base_url"`
	APIKeyCipher     []byte    `json:"api_key_cipher"`
	TimeoutSeconds   int       `json:"timeout_seconds"`
	ModelUUID        uuid.UUID `json:"model_uuid"`
	ModelID          string    `json:"model_id"`
	Alias            string    `json:"alias"`
	DisplayName      string    `json:"display_name"`
	ContextWindow    int       `json:"context_window"`
	ReasoningMode    string    `json:"reasoning_mode"`
	ReasoningEfforts []string  `json:"reasoning_efforts"`
	ReasoningDefault string    `json:"reasoning_default"`
}

// AICatalog 管理供应商与模型，并把「当前生效配置」缓存在 Redis。
type AICatalog struct {
	store  *postgres.Store
	rd     *redisc.Client
	cipher *crypto.Cipher

	mu       sync.Mutex
	cached   *CatalogSnapshot
	cachedAt time.Time
}

func NewAICatalog(st *postgres.Store, rd *redisc.Client, c *crypto.Cipher) *AICatalog {
	return &AICatalog{store: st, rd: rd, cipher: c}
}

func (c *AICatalog) Cipher() *crypto.Cipher { return c.cipher }

// Snapshot 返回「默认模型」配置：优先用后台指定的默认，其次回退到第一个已启用的模型。
// 完全没配置时返回 ErrAINotConfigured。
func (c *AICatalog) Snapshot(ctx context.Context) (*CatalogSnapshot, error) {
	c.mu.Lock()
	if c.cached != nil && time.Since(c.cachedAt) < 10*time.Second {
		snap := *c.cached
		c.mu.Unlock()
		return &snap, nil
	}
	c.mu.Unlock()

	var cached CatalogSnapshot
	if ok, err := c.rd.GetJSON(ctx, redisc.KeyAIActive, &cached); err == nil && ok {
		c.storeInProcess(&cached)
		return &cached, nil
	}

	active, err := c.store.GetActive(ctx)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	var snap *CatalogSnapshot
	if err == nil {
		snap = &CatalogSnapshot{
			ProviderID:       active.Provider.ID,
			ProviderName:     active.Provider.Name,
			BaseURL:          active.Provider.BaseURL,
			APIKeyCipher:     active.Provider.APIKeyCipher,
			TimeoutSeconds:   active.Provider.TimeoutSeconds,
			ModelUUID:        active.Model.ID,
			ModelID:          active.Model.ModelID,
			Alias:            active.Model.Alias,
			DisplayName:      active.Model.DisplayName,
			ContextWindow:    active.Model.ContextWindow,
			ReasoningMode:    active.Model.ReasoningMode,
			ReasoningEfforts: active.Model.ReasoningEfforts,
			ReasoningDefault: active.Model.ReasoningDefault,
		}
	} else {
		// 没有显式默认时，取第一个可用模型，避免用户必须先设置默认才能用
		entries, lerr := c.store.ListEnabledCatalog(ctx)
		if lerr != nil {
			return nil, lerr
		}
		if len(entries) == 0 {
			return nil, ErrAINotConfigured
		}
		snap, err = c.snapshotForEntry(ctx, entries[0])
		if err != nil {
			return nil, err
		}
	}

	_ = c.rd.SetJSON(ctx, redisc.KeyAIActive, snap, 60*time.Second)
	c.storeInProcess(snap)
	return snap, nil
}

// snapshotForEntry 把目录项补全成可用的转发配置。
func (c *AICatalog) snapshotForEntry(ctx context.Context, e model.CatalogEntry) (*CatalogSnapshot, error) {
	p, err := c.store.GetProvider(ctx, e.ProviderID)
	if err != nil {
		return nil, err
	}
	if !p.Enabled {
		return nil, ErrModelNotFound
	}
	return &CatalogSnapshot{
		ProviderID:       p.ID,
		ProviderName:     p.Name,
		BaseURL:          p.BaseURL,
		APIKeyCipher:     p.APIKeyCipher,
		TimeoutSeconds:   p.TimeoutSeconds,
		ModelUUID:        e.ModelUUID,
		ModelID:          e.ModelID,
		Alias:            e.Alias,
		DisplayName:      e.DisplayName,
		ContextWindow:    e.ContextWindow,
		ReasoningMode:    e.ReasoningMode,
		ReasoningEfforts: e.ReasoningEfforts,
		ReasoningDefault: e.ReasoningDefault,
	}, nil
}

func (c *AICatalog) storeInProcess(snap *CatalogSnapshot) {
	c.mu.Lock()
	cp := *snap
	c.cached = &cp
	c.cachedAt = time.Now()
	c.mu.Unlock()
}

// APIKey 解密上游密钥，仅在发起请求时调用。
func (c *AICatalog) APIKey(snap *CatalogSnapshot) (string, error) {
	return c.cipher.Decrypt(snap.APIKeyCipher)
}

func (c *AICatalog) Invalidate(ctx context.Context) {
	c.mu.Lock()
	c.cached = nil
	c.cachedAt = time.Time{}
	c.mu.Unlock()
	_ = c.rd.Del(ctx, redisc.KeyAIActive, redisc.KeyAICatalog)
}

// ListEnabled 列出所有可调用的模型（供应商与模型都启用），供后台选择器使用。
func (c *AICatalog) ListEnabled(ctx context.Context) ([]model.CatalogEntry, error) {
	return c.store.ListEnabledCatalog(ctx)
}

// SnapshotByAlias 严格按对外 alias 查找启用模型，避免与上游 model_id 的命名空间冲突。
func (c *AICatalog) SnapshotByAlias(ctx context.Context, key string) (*CatalogSnapshot, error) {
	entries, err := c.store.ListEnabledCatalog(ctx)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Alias != key {
			continue
		}
		// 统一走 snapshotForEntry，避免新增字段时漏填（之前思考能力就漏在这里）
		return c.snapshotForEntry(ctx, e)
	}
	return nil, ErrModelNotFound
}

// ---- 后台维护 ----

type ProviderPayload struct {
	Name           string
	BaseURL        string
	APIKey         string
	Enabled        bool
	TimeoutSeconds int
}

func (c *AICatalog) CreateProvider(ctx context.Context, in ProviderPayload) (*model.Provider, error) {
	cipherText, err := c.cipher.Encrypt(in.APIKey)
	if err != nil {
		return nil, err
	}
	timeout := in.TimeoutSeconds
	if timeout <= 0 {
		timeout = 120
	}
	p, err := c.store.CreateProvider(ctx, postgres.ProviderInput{
		Name:           strings.TrimSpace(in.Name),
		BaseURL:        strings.TrimRight(strings.TrimSpace(in.BaseURL), "/"),
		APIKeyCipher:   cipherText,
		Enabled:        in.Enabled,
		TimeoutSeconds: timeout,
	})
	if err != nil {
		return nil, err
	}
	c.Invalidate(ctx)
	return p, nil
}

func (c *AICatalog) UpdateProvider(ctx context.Context, id uuid.UUID, in ProviderPayload, replaceKey bool) (*model.Provider, error) {
	patch := postgres.ProviderPatch{
		Enabled:        &in.Enabled,
		TimeoutSeconds: &in.TimeoutSeconds,
	}
	if in.Name != "" {
		patch.Name = &in.Name
	}
	if in.BaseURL != "" {
		base := strings.TrimRight(in.BaseURL, "/")
		patch.BaseURL = &base
	}
	if replaceKey && in.APIKey != "" {
		cipherText, err := c.cipher.Encrypt(in.APIKey)
		if err != nil {
			return nil, err
		}
		patch.APIKeyCipher = cipherText
	}
	p, err := c.store.UpdateProvider(ctx, id, patch)
	if err != nil {
		return nil, err
	}
	c.Invalidate(ctx)
	return p, nil
}

// TestProvider 调上游 /models 探活，结果落库供后台展示。
func (c *AICatalog) TestProvider(ctx context.Context, id uuid.UUID) (bool, string) {
	p, err := c.store.GetProvider(ctx, id)
	if err != nil {
		return false, err.Error()
	}
	key, err := c.cipher.Decrypt(p.APIKeyCipher)
	if err != nil {
		_ = c.store.SetProviderCheck(ctx, id, false, err.Error())
		return false, err.Error()
	}
	_, err = c.fetchModels(ctx, p.BaseURL, key, 10*time.Second)
	if err != nil {
		_ = c.store.SetProviderCheck(ctx, id, false, err.Error())
		return false, err.Error()
	}
	_ = c.store.SetProviderCheck(ctx, id, true, "")
	return true, ""
}

// SyncResult 同步结果：写入/更新数量，以及本次上游返回的模型 ID 集合。
type SyncResult struct {
	Synced      int
	UpstreamIDs map[string]bool
}

// SyncModels 拉取上游模型列表并 upsert。
func (c *AICatalog) SyncModels(ctx context.Context, id uuid.UUID) (SyncResult, error) {
	p, err := c.store.GetProvider(ctx, id)
	if err != nil {
		return SyncResult{}, err
	}
	key, err := c.cipher.Decrypt(p.APIKeyCipher)
	if err != nil {
		return SyncResult{}, err
	}
	models, err := c.fetchModels(ctx, p.BaseURL, key, 20*time.Second)
	if err != nil {
		return SyncResult{}, err
	}
	n, err := c.store.UpsertFetchedModels(ctx, id, models)
	if err != nil {
		return SyncResult{}, err
	}
	_ = c.store.SetProviderCheck(ctx, id, true, "")

	upstream := make(map[string]bool, len(models))
	for _, m := range models {
		upstream[m.ModelID] = true
	}
	c.Invalidate(ctx)
	return SyncResult{Synced: n, UpstreamIDs: upstream}, nil
}

type upstreamModel struct {
	ID            string `json:"id"`
	DisplayName   string `json:"display_name"`
	ContextLength int    `json:"context_length"`
	ContextWindow int    `json:"context_window"`
}

func (c *AICatalog) fetchModels(ctx context.Context, baseURL, apiKey string, timeout time.Duration) ([]postgres.FetchedModel, error) {
	url := strings.TrimRight(baseURL, "/") + "/models"
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求上游失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("上游返回 %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	// 兼容 {"data":[...]} 与 [...] 两种形状。
	var wrapper struct {
		Data []upstreamModel `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil || wrapper.Data == nil {
		var arr []upstreamModel
		if err2 := json.Unmarshal(body, &arr); err2 != nil {
			return nil, fmt.Errorf("解析模型列表失败: %w", err)
		}
		wrapper.Data = arr
	}

	out := make([]postgres.FetchedModel, 0, len(wrapper.Data))
	for _, m := range wrapper.Data {
		if strings.TrimSpace(m.ID) == "" {
			continue
		}
		cw := m.ContextLength
		if cw == 0 {
			cw = m.ContextWindow
		}
		out = append(out, postgres.FetchedModel{
			ModelID:       m.ID,
			DisplayName:   m.DisplayName,
			ContextWindow: cw,
		})
	}
	return out, nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
