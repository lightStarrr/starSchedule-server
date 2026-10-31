package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/starschedule/server/internal/model"
	"github.com/starschedule/server/internal/store"
)

const providerColumns = `id, name, base_url, api_key_cipher, enabled, sort_order, timeout_seconds,
	last_check_at, last_check_ok, last_check_error, created_at, updated_at`

func scanProvider(row pgx.Row) (*model.Provider, error) {
	var p model.Provider
	err := row.Scan(&p.ID, &p.Name, &p.BaseURL, &p.APIKeyCipher, &p.Enabled, &p.SortOrder,
		&p.TimeoutSeconds, &p.LastCheckAt, &p.LastCheckOK, &p.LastCheckError, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, mapNoRows(err)
	}
	return &p, nil
}

func (s *Store) ListProviders(ctx context.Context) ([]model.Provider, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+providerColumns+` FROM ai_providers ORDER BY sort_order ASC, created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Provider{}
	for rows.Next() {
		var p model.Provider
		if err := rows.Scan(&p.ID, &p.Name, &p.BaseURL, &p.APIKeyCipher, &p.Enabled, &p.SortOrder,
			&p.TimeoutSeconds, &p.LastCheckAt, &p.LastCheckOK, &p.LastCheckError,
			&p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetProvider(ctx context.Context, id uuid.UUID) (*model.Provider, error) {
	return scanProvider(s.pool.QueryRow(ctx, `SELECT `+providerColumns+` FROM ai_providers WHERE id = $1`, id))
}

type ProviderInput struct {
	Name           string
	BaseURL        string
	APIKeyCipher   []byte
	Enabled        bool
	TimeoutSeconds int
}

func (s *Store) CreateProvider(ctx context.Context, in ProviderInput) (*model.Provider, error) {
	id := uuid.Must(uuid.NewV7())
	p, err := scanProvider(s.pool.QueryRow(ctx, `
		INSERT INTO ai_providers (id, name, base_url, api_key_cipher, enabled, timeout_seconds)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+providerColumns,
		id, in.Name, in.BaseURL, in.APIKeyCipher, in.Enabled, in.TimeoutSeconds))
	if err != nil && isUniqueViolation(err) {
		return nil, store.ErrConflict
	}
	return p, err
}

type ProviderPatch struct {
	Name           *string
	BaseURL        *string
	APIKeyCipher   []byte
	Enabled        *bool
	TimeoutSeconds *int
}

func (s *Store) UpdateProvider(ctx context.Context, id uuid.UUID, p ProviderPatch) (*model.Provider, error) {
	cur, err := s.GetProvider(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.Name != nil {
		cur.Name = *p.Name
	}
	if p.BaseURL != nil {
		cur.BaseURL = *p.BaseURL
	}
	if p.APIKeyCipher != nil {
		cur.APIKeyCipher = p.APIKeyCipher
	}
	if p.Enabled != nil {
		cur.Enabled = *p.Enabled
	}
	if p.TimeoutSeconds != nil {
		cur.TimeoutSeconds = *p.TimeoutSeconds
	}
	return scanProvider(s.pool.QueryRow(ctx, `
		UPDATE ai_providers SET name = $2, base_url = $3, api_key_cipher = $4, enabled = $5,
			timeout_seconds = $6, updated_at = now()
		WHERE id = $1 RETURNING `+providerColumns,
		id, cur.Name, cur.BaseURL, cur.APIKeyCipher, cur.Enabled, cur.TimeoutSeconds))
}

func (s *Store) DeleteProvider(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM ai_providers WHERE id = $1`, id)
	return err
}

func (s *Store) SetProviderCheck(ctx context.Context, id uuid.UUID, ok bool, errMsg string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE ai_providers SET last_check_at = now(), last_check_ok = $2,
			last_check_error = NULLIF($3, ''), updated_at = now() WHERE id = $1`, id, ok, errMsg)
	return err
}

const modelColumns = `id, provider_id, model_id, alias, display_name, context_window, source,
	enabled, fetched_at, created_at`

func scanModel(row pgx.Row) (*model.ProviderModel, error) {
	var m model.ProviderModel
	err := row.Scan(&m.ID, &m.ProviderID, &m.ModelID, &m.Alias, &m.DisplayName, &m.ContextWindow,
		&m.Source, &m.Enabled, &m.FetchedAt, &m.CreatedAt)
	if err != nil {
		return nil, mapNoRows(err)
	}
	return &m, nil
}

type FetchedModel struct {
	ModelID       string
	DisplayName   string
	ContextWindow int
}

// UpsertFetchedModels 同步上游模型列表：新增插入，已存在的只更新元信息，不覆盖后台的启用状态与别名。
func (s *Store) UpsertFetchedModels(ctx context.Context, providerID uuid.UUID, items []FetchedModel) (int, error) {
	batch := &pgx.Batch{}
	for _, it := range items {
		display := it.DisplayName
		if display == "" {
			display = it.ModelID
		}
		batch.Queue(`
			INSERT INTO ai_provider_models (id, provider_id, model_id, alias, display_name, context_window, source, enabled)
			VALUES ($1, $2, $3, $4, $5, $6, 'fetched', true)
			ON CONFLICT (provider_id, model_id) DO UPDATE SET
				display_name = EXCLUDED.display_name,
				context_window = CASE WHEN EXCLUDED.context_window > 0
					THEN EXCLUDED.context_window ELSE ai_provider_models.context_window END,
				fetched_at = now()`,
			uuid.Must(uuid.NewV7()), providerID, it.ModelID, it.ModelID, display, it.ContextWindow)
	}
	br := s.pool.SendBatch(ctx, batch)
	defer br.Close()
	n := 0
	for range items {
		if _, err := br.Exec(); err != nil {
			if isUniqueViolation(err) {
				continue // 别名冲突：保留已有别名，跳过该条
			}
			return n, err
		}
		n++
	}
	return n, nil
}

func (s *Store) ListModels(ctx context.Context, providerID uuid.UUID) ([]model.ProviderModel, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+modelColumns+` FROM ai_provider_models WHERE provider_id = $1 ORDER BY model_id`, providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ProviderModel{}
	for rows.Next() {
		var m model.ProviderModel
		if err := rows.Scan(&m.ID, &m.ProviderID, &m.ModelID, &m.Alias, &m.DisplayName, &m.ContextWindow,
			&m.Source, &m.Enabled, &m.FetchedAt, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) GetModel(ctx context.Context, id uuid.UUID) (*model.ProviderModel, error) {
	return scanModel(s.pool.QueryRow(ctx, `SELECT `+modelColumns+` FROM ai_provider_models WHERE id = $1`, id))
}

type ModelInput struct {
	ProviderID    uuid.UUID
	ModelID       string
	Alias         string
	DisplayName   string
	ContextWindow int
	Enabled       bool
}

func (s *Store) CreateModel(ctx context.Context, in ModelInput) (*model.ProviderModel, error) {
	alias := in.Alias
	if alias == "" {
		alias = in.ModelID
	}
	display := in.DisplayName
	if display == "" {
		display = in.ModelID
	}
	m, err := scanModel(s.pool.QueryRow(ctx, `
		INSERT INTO ai_provider_models (id, provider_id, model_id, alias, display_name, context_window, source, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, 'manual', $7) RETURNING `+modelColumns,
		uuid.Must(uuid.NewV7()), in.ProviderID, in.ModelID, alias, display, in.ContextWindow, in.Enabled))
	if err != nil && isUniqueViolation(err) {
		return nil, store.ErrConflict
	}
	return m, err
}

type ModelPatch struct {
	Alias         *string
	DisplayName   *string
	ContextWindow *int
	Enabled       *bool
}

func (s *Store) UpdateModel(ctx context.Context, id uuid.UUID, p ModelPatch) (*model.ProviderModel, error) {
	cur, err := s.GetModel(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.Alias != nil {
		cur.Alias = *p.Alias
	}
	if p.DisplayName != nil {
		cur.DisplayName = *p.DisplayName
	}
	if p.ContextWindow != nil {
		cur.ContextWindow = *p.ContextWindow
	}
	if p.Enabled != nil {
		cur.Enabled = *p.Enabled
	}
	m, err := scanModel(s.pool.QueryRow(ctx, `
		UPDATE ai_provider_models SET alias = $2, display_name = $3, context_window = $4, enabled = $5
		WHERE id = $1 RETURNING `+modelColumns,
		id, cur.Alias, cur.DisplayName, cur.ContextWindow, cur.Enabled))
	if err != nil && isUniqueViolation(err) {
		return nil, store.ErrConflict
	}
	return m, err
}

func (s *Store) DeleteModel(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM ai_provider_models WHERE id = $1`, id)
	return err
}

const (
	keyActiveProvider = "ai.active_provider"
	keyActiveModel    = "ai.active_model"
)

// ListEnabledCatalog 返回所有「供应商与模型都启用」的条目，按供应商排序。
func (s *Store) ListEnabledCatalog(ctx context.Context) ([]model.CatalogEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.id, p.name, m.id, m.model_id, m.alias, m.display_name, m.context_window,
		       m.source, m.fetched_at
		FROM ai_provider_models m
		JOIN ai_providers p ON p.id = m.provider_id
		WHERE m.enabled AND p.enabled
		ORDER BY p.sort_order ASC, p.created_at ASC, m.model_id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.CatalogEntry{}
	for rows.Next() {
		var e model.CatalogEntry
		if err := rows.Scan(&e.ProviderID, &e.ProviderName, &e.ModelUUID, &e.ModelID,
			&e.Alias, &e.DisplayName, &e.ContextWindow, &e.Source, &e.FetchedAt); err != nil {
			return nil, err
		}
		e.Enabled = true
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListAllModels 返回所有模型（含未启用），附带供应商名，供后台列表使用。
func (s *Store) ListAllModels(ctx context.Context) ([]model.CatalogEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.id, p.name, m.id, m.model_id, m.alias, m.display_name, m.context_window,
		       m.enabled, m.source, m.fetched_at
		FROM ai_provider_models m
		JOIN ai_providers p ON p.id = m.provider_id
		ORDER BY p.sort_order ASC, p.created_at ASC, m.model_id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.CatalogEntry{}
	for rows.Next() {
		var e model.CatalogEntry
		if err := rows.Scan(&e.ProviderID, &e.ProviderName, &e.ModelUUID, &e.ModelID,
			&e.Alias, &e.DisplayName, &e.ContextWindow, &e.Enabled, &e.Source, &e.FetchedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetActive 返回当前生效的供应商 + 模型；未配置时返回 store.ErrNotFound。
func (s *Store) GetActive(ctx context.Context) (*model.ActiveAIConfig, error) {
	raw, err := s.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	var providerID, modelID string
	_ = json.Unmarshal(raw[keyActiveProvider], &providerID)
	_ = json.Unmarshal(raw[keyActiveModel], &modelID)
	if providerID == "" || modelID == "" {
		return nil, store.ErrNotFound
	}
	pid, err := uuid.Parse(providerID)
	if err != nil {
		return nil, store.ErrNotFound
	}
	mid, err := uuid.Parse(modelID)
	if err != nil {
		return nil, store.ErrNotFound
	}
	p, err := s.GetProvider(ctx, pid)
	if err != nil {
		return nil, err
	}
	m, err := s.GetModel(ctx, mid)
	if err != nil {
		return nil, err
	}
	if !p.Enabled || !m.Enabled || m.ProviderID != p.ID {
		return nil, store.ErrNotFound
	}
	return &model.ActiveAIConfig{Provider: *p, Model: *m}, nil
}

func (s *Store) SetActive(ctx context.Context, providerID, modelID uuid.UUID) error {
	return s.SetSettings(ctx, map[string]any{
		keyActiveProvider: providerID.String(),
		keyActiveModel:    modelID.String(),
	})
}

// IncrUsage 累加设备当日用量；estimated 与 isError 分别计数。
func (s *Store) IncrUsage(ctx context.Context, date time.Time, deviceID uuid.UUID, prompt, completion int64, estimated, isError bool) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO ai_usage_daily (stat_date, device_id, request_count, error_count,
			prompt_tokens, completion_tokens, estimated_count)
		VALUES ($1::date, $2, 1, $3, $4, $5, $6)
		ON CONFLICT (stat_date, device_id) DO UPDATE SET
			request_count     = ai_usage_daily.request_count + 1,
			error_count       = ai_usage_daily.error_count + $3,
			prompt_tokens     = ai_usage_daily.prompt_tokens + $4,
			completion_tokens = ai_usage_daily.completion_tokens + $5,
			estimated_count   = ai_usage_daily.estimated_count + $6,
			updated_at        = now()`,
		dateStr(date), deviceID, boolToInt(isError), prompt, completion, boolToInt(estimated))
	return err
}

func (s *Store) UsageOn(ctx context.Context, date time.Time, deviceID uuid.UUID) (model.UsageRow, error) {
	var u model.UsageRow
	err := s.pool.QueryRow(ctx, `
		SELECT request_count, error_count, prompt_tokens, completion_tokens, estimated_count
		FROM ai_usage_daily WHERE stat_date = $1::date AND device_id = $2`, dateStr(date), deviceID).
		Scan(&u.RequestCount, &u.ErrorCount, &u.PromptTokens, &u.CompletionTokens, &u.EstimatedCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.UsageRow{}, nil
	}
	return u, err
}

type RequestLog struct {
	ID               uuid.UUID
	RequestID        string
	DeviceID         *uuid.UUID
	AdminID          *uuid.UUID
	StatDate         time.Time
	ProviderID       *uuid.UUID
	Model            string
	Stream           bool
	Source           string
	Status           string
	HTTPStatus       int
	ErrorCode        string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	Estimated        bool
	FirstTokenMS     *int
	TotalMS          *int
}

func (s *Store) InsertRequestLog(ctx context.Context, l RequestLog) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO ai_request_log (id, request_id, device_id, admin_id, stat_date, provider_id,
			model, stream, source, status, http_status, error_code, prompt_tokens, completion_tokens,
			total_tokens, estimated, first_token_ms, total_ms)
		VALUES ($1,$2,$3,$4,$5::date,$6,$7,$8,$9,$10,$11,NULLIF($12,''),$13,$14,$15,$16,$17,$18)`,
		l.ID, l.RequestID, l.DeviceID, l.AdminID, dateStr(l.StatDate), l.ProviderID, l.Model, l.Stream,
		l.Source, l.Status, l.HTTPStatus, l.ErrorCode, l.PromptTokens, l.CompletionTokens,
		l.TotalTokens, l.Estimated, l.FirstTokenMS, l.TotalMS)
	return err
}

type UsageByDay struct {
	Date         string `json:"date"`
	RequestCount int64  `json:"request_count"`
	Tokens       int64  `json:"tokens"`
	Errors       int64  `json:"errors"`
}

func (s *Store) UsageByDay(ctx context.Context, from, to time.Time) ([]UsageByDay, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT to_char(stat_date, 'YYYY-MM-DD'),
		       SUM(request_count), SUM(prompt_tokens + completion_tokens), SUM(error_count)
		FROM ai_usage_daily WHERE stat_date BETWEEN $1::date AND $2::date
		GROUP BY stat_date ORDER BY stat_date`, dateStr(from), dateStr(to))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UsageByDay{}
	for rows.Next() {
		var u UsageByDay
		if err := rows.Scan(&u.Date, &u.RequestCount, &u.Tokens, &u.Errors); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

type UsageByDevice struct {
	DeviceID     uuid.UUID `json:"device_id"`
	InstallID    string    `json:"install_id"`
	RequestCount int64     `json:"request_count"`
	Tokens       int64     `json:"tokens"`
	Errors       int64     `json:"errors"`
}

func (s *Store) UsageByDevice(ctx context.Context, from, to time.Time, limit int) ([]UsageByDevice, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		SELECT u.device_id, COALESCE(d.install_id, ''),
		       SUM(u.request_count), SUM(u.prompt_tokens + u.completion_tokens), SUM(u.error_count)
		FROM ai_usage_daily u LEFT JOIN devices d ON d.id = u.device_id
		WHERE u.stat_date BETWEEN $1::date AND $2::date
		GROUP BY u.device_id, d.install_id
		ORDER BY SUM(u.prompt_tokens + u.completion_tokens) DESC LIMIT $3`, dateStr(from), dateStr(to), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UsageByDevice{}
	for rows.Next() {
		var u UsageByDevice
		if err := rows.Scan(&u.DeviceID, &u.InstallID, &u.RequestCount, &u.Tokens, &u.Errors); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}
