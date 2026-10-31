package service

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/starschedule/server/internal/store/postgres"
	redisc "github.com/starschedule/server/internal/store/redis"
)

// SettingsService 提供可在后台热改的运行时参数。
// 三级缓存：进程内 10s → Redis 30s → PostgreSQL。
type SettingsService struct {
	store    *postgres.Store
	rd       *redisc.Client
	defaults postgres.RuntimeSettings

	mu       sync.Mutex
	cached   postgres.RuntimeSettings
	cachedAt time.Time
}

func NewSettingsService(st *postgres.Store, rd *redisc.Client, defaults postgres.RuntimeSettings) *SettingsService {
	return &SettingsService{store: st, rd: rd, defaults: defaults}
}

func (s *SettingsService) Defaults() postgres.RuntimeSettings { return s.defaults }

func (s *SettingsService) Get(ctx context.Context) postgres.RuntimeSettings {
	s.mu.Lock()
	if time.Since(s.cachedAt) < 10*time.Second {
		v := s.cached
		s.mu.Unlock()
		return v
	}
	s.mu.Unlock()

	val := s.defaults
	var cached postgres.RuntimeSettings
	if ok, err := s.rd.GetJSON(ctx, redisc.KeySettings, &cached); err == nil && ok {
		val = cached
	} else if got, err := s.store.GetRuntimeSettings(ctx, s.defaults); err == nil {
		val = got
		_ = s.rd.SetJSON(ctx, redisc.KeySettings, val, 30*time.Second)
	}

	s.mu.Lock()
	s.cached, s.cachedAt = val, time.Now()
	s.mu.Unlock()
	return val
}

func (s *SettingsService) Update(ctx context.Context, values map[string]any) error {
	if err := s.store.SetSettings(ctx, values); err != nil {
		return err
	}
	s.Invalidate(ctx)
	return nil
}

// Raw 返回原始参数（管理端展示默认值 vs 已覆盖）。
func (s *SettingsService) Raw(ctx context.Context) (map[string]json.RawMessage, error) {
	return s.store.GetSettings(ctx)
}

func (s *SettingsService) Invalidate(ctx context.Context) {
	s.mu.Lock()
	s.cachedAt = time.Time{}
	s.mu.Unlock()
	_ = s.rd.Del(ctx, redisc.KeySettings)
}
