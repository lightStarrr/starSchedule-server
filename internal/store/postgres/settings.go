package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/starschedule/server/internal/model"
)

// GetSettings 读取全部运行时参数（原始 JSON）。
func (s *Store) GetSettings(ctx context.Context) (map[string]json.RawMessage, error) {
	rows, err := s.pool.Query(ctx, `SELECT key, value FROM app_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]json.RawMessage{}
	for rows.Next() {
		var k string
		var v []byte
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = append(json.RawMessage(nil), v...)
	}
	return out, rows.Err()
}

// SetSettings 写入运行时参数（合并语义由调用方决定）。
func (s *Store) SetSettings(ctx context.Context, values map[string]any) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	for k, v := range values {
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("序列化参数 %s: %w", k, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO app_settings (key, value, updated_at) VALUES ($1, $2::jsonb, now())
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
			k, string(raw)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) DeleteSetting(ctx context.Context, key string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM app_settings WHERE key = $1`, key)
	return err
}

// rawToInt 兼容 123 与 "123" 两种写法。
func rawToInt(raw json.RawMessage) (int64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, true
	}
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		if v, err := strconv.ParseInt(str, 10, 64); err == nil {
			return v, true
		}
	}
	return 0, false
}

// RuntimeSettings 是解析后的运行时参数。
type RuntimeSettings struct {
	DailyTokenQuota     int64
	RPMPerDevice        int
	InflightPerDevice   int
	GlobalConcurrency   int
	MaxOutputTokens     int
	RegisterPerIPHour   int
	LaunchRetentionDays int
	AILogRetentionDays  int
}

func (s *Store) GetRuntimeSettings(ctx context.Context, defaults RuntimeSettings) (RuntimeSettings, error) {
	raw, err := s.GetSettings(ctx)
	if err != nil {
		return defaults, err
	}
	out := defaults
	setInt := func(key string, dst *int64) {
		if v, ok := rawToInt(raw[key]); ok && v >= 0 {
			*dst = v
		}
	}
	setInt("ai.daily_token_quota", &out.DailyTokenQuota)
	toI := func(n int64) int { return int(n) }

	if v, ok := rawToInt(raw["ai.max_output_tokens"]); ok && v >= 0 {
		out.MaxOutputTokens = toI(v)
	}

	if v, ok := rawToInt(raw["ai.rpm_per_device"]); ok && v >= 0 {
		out.RPMPerDevice = toI(v)
	}
	if v, ok := rawToInt(raw["ai.inflight_per_device"]); ok && v >= 0 {
		out.InflightPerDevice = toI(v)
	}
	if v, ok := rawToInt(raw["ai.global_concurrency"]); ok && v >= 0 {
		out.GlobalConcurrency = toI(v)
	}
	if v, ok := rawToInt(raw["device.register_per_ip_hour"]); ok && v >= 0 {
		out.RegisterPerIPHour = toI(v)
	}
	if v, ok := rawToInt(raw["retention.launch_event_days"]); ok && v >= 0 {
		out.LaunchRetentionDays = toI(v)
	}
	if v, ok := rawToInt(raw["retention.ai_log_days"]); ok && v >= 0 {
		out.AILogRetentionDays = toI(v)
	}
	return out, nil
}

// ---- 管理端 ----

func (s *Store) GetAdminByUsername(ctx context.Context, username string) (*model.AdminUser, error) {
	var a model.AdminUser
	err := s.pool.QueryRow(ctx, `
		SELECT id, username, password_hash, role, created_at, last_login_at
		FROM admin_users WHERE username = $1`, username).
		Scan(&a.ID, &a.Username, &a.PasswordHash, &a.Role, &a.CreatedAt, &a.LastLoginAt)
	if err != nil {
		return nil, mapNoRows(err)
	}
	return &a, nil
}

// SeedAdmin 仅当用户不存在时创建，绝不覆盖已改过的密码。
func (s *Store) SeedAdmin(ctx context.Context, username, passwordHash string) (bool, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		INSERT INTO admin_users (id, username, password_hash, role)
		VALUES (gen_random_uuid(), $1, $2, 'admin')
		ON CONFLICT (username) DO NOTHING
		RETURNING 1`, username, passwordHash).Scan(&n)
	if err != nil {
		if isNoRows(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *Store) TouchAdminLogin(ctx context.Context, id any) error {
	_, err := s.pool.Exec(ctx, `UPDATE admin_users SET last_login_at = now() WHERE id = $1`, id)
	return err
}

func (s *Store) InsertAudit(ctx context.Context, a model.AuditLog) error {
	detail, err := json.Marshal(a.Detail)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO admin_audit_logs (id, admin_id, admin_username, action, target_type, target_id, detail, ip)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8)`,
		a.ID, a.AdminID, a.AdminUsername, a.Action, a.TargetType, a.TargetID, string(detail), a.IP)
	return err
}

func (s *Store) ListAudit(ctx context.Context, limit int) ([]model.AuditLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, admin_id, admin_username, action, target_type, target_id, detail, ip, created_at
		FROM admin_audit_logs ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.AuditLog{}
	for rows.Next() {
		var a model.AuditLog
		var detail []byte
		if err := rows.Scan(&a.ID, &a.AdminID, &a.AdminUsername, &a.Action, &a.TargetType,
			&a.TargetID, &detail, &a.IP, &a.CreatedAt); err != nil {
			return nil, err
		}
		if len(detail) > 0 {
			_ = json.Unmarshal(detail, &a.Detail)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) TouchJob(ctx context.Context, name string, err error) error {
	if err != nil {
		_, e := s.pool.Exec(ctx, `
			INSERT INTO job_state (name, last_run_at, last_error) VALUES ($1, now(), $2)
			ON CONFLICT (name) DO UPDATE SET last_run_at = now(), last_error = EXCLUDED.last_error`,
			name, err.Error())
		return e
	}
	_, e := s.pool.Exec(ctx, `
		INSERT INTO job_state (name, last_run_at, last_success_at, last_error)
		VALUES ($1, now(), now(), NULL)
		ON CONFLICT (name) DO UPDATE SET last_run_at = now(), last_success_at = now(), last_error = NULL`,
		name)
	return e
}
