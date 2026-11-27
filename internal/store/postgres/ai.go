package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
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
	enabled, reasoning_mode, reasoning_efforts, reasoning_default, fetched_at, created_at`

func scanModel(row pgx.Row) (*model.ProviderModel, error) {
	var m model.ProviderModel
	err := row.Scan(&m.ID, &m.ProviderID, &m.ModelID, &m.Alias, &m.DisplayName, &m.ContextWindow,
		&m.Source, &m.Enabled, &m.ReasoningMode, &m.ReasoningEfforts, &m.ReasoningDefault, &m.FetchedAt, &m.CreatedAt)
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
	tx, err := s.beginModelWrite(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	for _, it := range items {
		display := it.DisplayName
		if display == "" {
			display = it.ModelID
		}
		alias, err := modelAlias(ctx, tx, it.ModelID, it.ModelID)
		if err != nil {
			return 0, err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO ai_provider_models (id, provider_id, model_id, alias, display_name, context_window, source, enabled)
			VALUES ($1, $2, $3, $4, $5, $6, 'fetched',
			        NOT EXISTS (SELECT 1 FROM ai_provider_models x WHERE x.model_id = $3 AND x.enabled))
			ON CONFLICT (provider_id, model_id) DO UPDATE SET
				display_name = EXCLUDED.display_name,
				context_window = CASE WHEN EXCLUDED.context_window > 0
					THEN EXCLUDED.context_window ELSE ai_provider_models.context_window END,
				fetched_at = now()`,
			uuid.Must(uuid.NewV7()), providerID, it.ModelID, alias, display, it.ContextWindow)
		if err != nil {
			return 0, modelWriteError(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(items), nil
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
			&m.Source, &m.Enabled, &m.ReasoningMode, &m.ReasoningEfforts, &m.ReasoningDefault, &m.FetchedAt, &m.CreatedAt); err != nil {
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
	tx, err := s.beginModelWrite(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	alias, err := modelAlias(ctx, tx, in.ModelID, in.Alias)
	if err != nil {
		return nil, err
	}
	display := in.DisplayName
	if display == "" {
		display = in.ModelID
	}
	m, err := scanModel(tx.QueryRow(ctx, `
		INSERT INTO ai_provider_models (id, provider_id, model_id, alias, display_name, context_window, source, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, 'manual',
		        $7 AND NOT EXISTS (SELECT 1 FROM ai_provider_models x WHERE x.model_id = $3 AND x.enabled))
		RETURNING `+modelColumns,
		uuid.Must(uuid.NewV7()), in.ProviderID, in.ModelID, alias, display, in.ContextWindow, in.Enabled))
	if err != nil && isUniqueViolation(err) {
		return nil, store.ErrConflict
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return m, nil
}

type ModelPatch struct {
	Alias            *string
	DisplayName      *string
	ContextWindow    *int
	Enabled          *bool
	ReasoningMode    *string
	ReasoningEfforts *[]string
	ReasoningDefault *string
}

func (s *Store) UpdateModel(ctx context.Context, id uuid.UUID, p ModelPatch) (*model.ProviderModel, error) {
	tx, err := s.beginModelWrite(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	cur, err := scanModel(tx.QueryRow(ctx, `SELECT `+modelColumns+` FROM ai_provider_models WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return nil, err
	}
	if p.Alias != nil {
		cur.Alias = strings.TrimSpace(*p.Alias)
		if cur.Alias == "" {
			return nil, store.ErrConflict
		}
		var conflict bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ai_provider_models WHERE alias=$1 AND model_id<>$2)`, cur.Alias, cur.ModelID).Scan(&conflict); err != nil {
			return nil, err
		}
		if conflict {
			return nil, store.ErrConflict
		}
		if _, err := tx.Exec(ctx, `UPDATE ai_provider_models SET alias=$1 WHERE model_id=$2`, cur.Alias, cur.ModelID); err != nil {
			return nil, modelWriteError(err)
		}
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
	if p.ReasoningMode != nil {
		cur.ReasoningMode = *p.ReasoningMode
	}
	if p.ReasoningEfforts != nil {
		// nil 切片会被 pgx 编码为 SQL NULL；无可选档位应保存为空数组。
		cur.ReasoningEfforts = append([]string{}, (*p.ReasoningEfforts)...)
	}
	if p.ReasoningDefault != nil {
		cur.ReasoningDefault = *p.ReasoningDefault
	}
	m, err := scanModel(tx.QueryRow(ctx, `
		UPDATE ai_provider_models SET alias = $2, display_name = $3, context_window = $4, enabled = $5,
			reasoning_mode = $6, reasoning_efforts = $7, reasoning_default = $8
		WHERE id = $1 RETURNING `+modelColumns,
		id, cur.Alias, cur.DisplayName, cur.ContextWindow, cur.Enabled,
		cur.ReasoningMode, cur.ReasoningEfforts, cur.ReasoningDefault))
	if err != nil && isUniqueViolation(err) {
		return nil, store.ErrConflict
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return m, nil
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
		       m.source, m.reasoning_mode, m.reasoning_efforts, m.reasoning_default, m.fetched_at
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
			&e.Alias, &e.DisplayName, &e.ContextWindow, &e.Source,
			&e.ReasoningMode, &e.ReasoningEfforts, &e.ReasoningDefault, &e.FetchedAt); err != nil {
			return nil, err
		}
		e.Enabled = true
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListModelsByModelID 返回同一上游模型在各供应商下的全部记录。
func (s *Store) ListModelsByModelID(ctx context.Context, modelID string) ([]model.ProviderModel, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+modelColumns+` FROM ai_provider_models
		WHERE model_id = $1 ORDER BY created_at ASC`, modelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ProviderModel{}
	for rows.Next() {
		m, err := scanModel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// ListAllModels 返回所有模型（含未启用），附带供应商名，供后台列表使用。
func (s *Store) ListAllModels(ctx context.Context) ([]model.CatalogEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.id, p.name, m.id, m.model_id, m.alias, m.display_name, m.context_window,
		       m.enabled, m.source, m.reasoning_mode, m.reasoning_efforts, m.reasoning_default, m.fetched_at
		FROM ai_provider_models m
		JOIN ai_providers p ON p.id = m.provider_id
		WHERE p.enabled
		ORDER BY p.sort_order ASC, p.created_at ASC, m.model_id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.CatalogEntry{}
	for rows.Next() {
		var e model.CatalogEntry
		if err := rows.Scan(&e.ProviderID, &e.ProviderName, &e.ModelUUID, &e.ModelID,
			&e.Alias, &e.DisplayName, &e.ContextWindow, &e.Enabled, &e.Source,
			&e.ReasoningMode, &e.ReasoningEfforts, &e.ReasoningDefault, &e.FetchedAt); err != nil {
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
	tx, err := s.beginModelWrite(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var valid bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ai_provider_models m JOIN ai_providers p ON p.id=m.provider_id WHERE m.id=$1 AND p.id=$2 AND m.enabled AND p.enabled)`, modelID, providerID).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return store.ErrConflict
	}
	for _, setting := range []struct{ key, value string }{{keyActiveModel, modelID.String()}, {keyActiveProvider, providerID.String()}} {
		if _, err := tx.Exec(ctx, `INSERT INTO app_settings (key,value) VALUES ($1,to_jsonb($2::text)) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,updated_at=now()`, setting.key, setting.value); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
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
	CachedTokens     int
	TotalTokens      int
	Estimated        bool
	FirstTokenMS     *int
	TotalMS          *int
}

func (s *Store) InsertRequestLog(ctx context.Context, l RequestLog) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO ai_request_log (id, request_id, device_id, admin_id, stat_date, provider_id,
			model, stream, source, status, http_status, error_code, prompt_tokens, completion_tokens,
			cached_tokens, total_tokens, estimated, first_token_ms, total_ms)
		VALUES ($1,$2,$3,$4,$5::date,$6,$7,$8,$9,$10,$11,NULLIF($12,''),$13,$14,$15,$16,$17,$18,$19)`,
		l.ID, l.RequestID, l.DeviceID, l.AdminID, dateStr(l.StatDate), l.ProviderID, l.Model, l.Stream,
		l.Source, l.Status, l.HTTPStatus, l.ErrorCode, l.PromptTokens, l.CompletionTokens,
		l.CachedTokens, l.TotalTokens, l.Estimated, l.FirstTokenMS, l.TotalMS)
	return err
}

type AIRequestFilter struct {
	From     *time.Time
	To       *time.Time
	DeviceID *uuid.UUID
	Model    string
	Stream   *bool
	Status   string
	Source   string
	Query    string
	Limit    int
	Cursor   string
}

type AIRequestLogItem struct {
	ID               uuid.UUID  `json:"id"`
	RequestID        string     `json:"request_id"`
	CreatedAt        time.Time  `json:"created_at"`
	Source           string     `json:"source"`
	DeviceID         *uuid.UUID `json:"device_id,omitempty"`
	InstallID        string     `json:"install_id,omitempty"`
	AdminID          *uuid.UUID `json:"admin_id,omitempty"`
	AdminUsername    string     `json:"admin_username,omitempty"`
	ProviderName     string     `json:"provider_name,omitempty"`
	Model            string     `json:"model"`
	Stream           bool       `json:"stream"`
	Status           string     `json:"status"`
	HTTPStatus       int        `json:"http_status"`
	ErrorCode        string     `json:"error_code,omitempty"`
	PromptTokens     int        `json:"prompt_tokens"`
	CompletionTokens int        `json:"completion_tokens"`
	CachedTokens     int        `json:"cached_tokens"`
	TotalTokens      int        `json:"total_tokens"`
	Estimated        bool       `json:"estimated"`
	FirstTokenMS     *int       `json:"first_token_ms,omitempty"`
	TotalMS          *int       `json:"total_ms,omitempty"`
	TPS              *float64   `json:"tps,omitempty"`
}

// ComputeStreamTPS 计算流式请求扣除首 Token 后的纯生成速度（tokens/s，保留 1 位小数）。
// 缺少首 Token 耗时或生成阶段耗时 <= 0 时返回 nil，不与首字等待混算。
func ComputeStreamTPS(stream bool, completionTokens int, firstTokenMS, totalMS *int) *float64 {
	if !stream || completionTokens <= 0 || firstTokenMS == nil || totalMS == nil || *firstTokenMS < 0 {
		return nil
	}
	decodeMS := *totalMS - *firstTokenMS
	if decodeMS <= 0 {
		return nil
	}
	tps := math.Round(float64(completionTokens)/(float64(decodeMS)/1000.0)*10) / 10
	return &tps
}

type aiRequestCursor struct {
	CreatedAt time.Time `json:"t"`
	ID        uuid.UUID `json:"i"`
}

func encodeAIRequestCursor(c aiRequestCursor) string {
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeAIRequestCursor(v string) (aiRequestCursor, error) {
	var c aiRequestCursor
	raw, err := base64.RawURLEncoding.DecodeString(v)
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(raw, &c)
	return c, err
}

func buildAIRequestWhere(f AIRequestFilter, includeCursor bool) (string, []any, error) {
	where := []string{"1 = 1"}
	args := []any{}
	if f.From != nil {
		args = append(args, dateStr(*f.From))
		where = append(where, fmt.Sprintf("l.stat_date >= $%d::date", len(args)))
	}
	if f.To != nil {
		args = append(args, dateStr(*f.To))
		where = append(where, fmt.Sprintf("l.stat_date <= $%d::date", len(args)))
	}
	if f.DeviceID != nil {
		args = append(args, *f.DeviceID)
		where = append(where, fmt.Sprintf("l.device_id = $%d", len(args)))
	}
	if m := strings.TrimSpace(f.Model); m != "" {
		args = append(args, m)
		where = append(where, fmt.Sprintf("l.model = $%d", len(args)))
	}
	if f.Stream != nil {
		args = append(args, *f.Stream)
		where = append(where, fmt.Sprintf("l.stream = $%d", len(args)))
	}
	if s := strings.TrimSpace(f.Status); s != "" {
		args = append(args, s)
		where = append(where, fmt.Sprintf("l.status = $%d", len(args)))
	}
	if src := strings.TrimSpace(f.Source); src != "" {
		args = append(args, src)
		where = append(where, fmt.Sprintf("l.source = $%d", len(args)))
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		args = append(args, "%"+q+"%")
		n := len(args)
		where = append(where, fmt.Sprintf(
			"(COALESCE(d.install_id, '') ILIKE $%d OR COALESCE(l.device_id::text, '') ILIKE $%d OR COALESCE(a.username, '') ILIKE $%d OR l.request_id ILIKE $%d)",
			n, n, n, n))
	}
	if includeCursor && f.Cursor != "" {
		cur, err := decodeAIRequestCursor(f.Cursor)
		if err != nil {
			return "", nil, fmt.Errorf("游标无效: %w", err)
		}
		args = append(args, cur.CreatedAt, cur.ID)
		where = append(where, fmt.Sprintf("(l.created_at, l.id) < ($%d, $%d)", len(args)-1, len(args)))
	}
	return strings.Join(where, " AND "), args, nil
}

// ListAIRequests 按时间倒序游标分页查询 AI 请求明细。
func (s *Store) ListAIRequests(ctx context.Context, f AIRequestFilter) ([]AIRequestLogItem, string, bool, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 20
	}
	whereSQL, args, err := buildAIRequestWhere(f, true)
	if err != nil {
		return nil, "", false, err
	}
	args = append(args, f.Limit+1)
	query := fmt.Sprintf(`
		SELECT l.id, l.request_id, l.created_at, l.source,
		       l.device_id, COALESCE(d.install_id, ''),
		       l.admin_id, COALESCE(a.username, ''),
		       COALESCE(p.name, ''), l.model, l.stream, l.status, l.http_status,
		       COALESCE(l.error_code, ''), l.prompt_tokens, l.completion_tokens,
		       l.cached_tokens, l.total_tokens, l.estimated, l.first_token_ms, l.total_ms
		FROM ai_request_log l
		LEFT JOIN devices d ON d.id = l.device_id
		LEFT JOIN admin_users a ON a.id = l.admin_id
		LEFT JOIN ai_providers p ON p.id = l.provider_id
		WHERE %s
		ORDER BY l.created_at DESC, l.id DESC
		LIMIT $%d`, whereSQL, len(args))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", false, err
	}
	defer rows.Close()

	out := []AIRequestLogItem{}
	for rows.Next() {
		var item AIRequestLogItem
		if err := rows.Scan(
			&item.ID, &item.RequestID, &item.CreatedAt, &item.Source,
			&item.DeviceID, &item.InstallID,
			&item.AdminID, &item.AdminUsername,
			&item.ProviderName, &item.Model, &item.Stream, &item.Status, &item.HTTPStatus,
			&item.ErrorCode, &item.PromptTokens, &item.CompletionTokens,
			&item.CachedTokens, &item.TotalTokens, &item.Estimated, &item.FirstTokenMS, &item.TotalMS,
		); err != nil {
			return nil, "", false, err
		}
		item.TPS = ComputeStreamTPS(item.Stream, item.CompletionTokens, item.FirstTokenMS, item.TotalMS)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, "", false, err
	}

	hasMore := len(out) > f.Limit
	if hasMore {
		out = out[:f.Limit]
	}
	var next string
	if hasMore && len(out) > 0 {
		last := out[len(out)-1]
		next = encodeAIRequestCursor(aiRequestCursor{CreatedAt: last.CreatedAt, ID: last.ID})
	}
	return out, next, hasMore, nil
}

func (s *Store) CountAIRequests(ctx context.Context, f AIRequestFilter) (int64, error) {
	whereSQL, args, err := buildAIRequestWhere(f, false)
	if err != nil {
		return 0, err
	}
	var n int64
	err = s.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM ai_request_log l
		LEFT JOIN devices d ON d.id = l.device_id
		LEFT JOIN admin_users a ON a.id = l.admin_id
		WHERE `+whereSQL, args...).Scan(&n)
	return n, err
}

// ListAIRequestModels 返回时间范围内请求日志里出现过的模型列表（含已删除/停用模型）。
func (s *Store) ListAIRequestModels(ctx context.Context, from, to *time.Time) ([]string, error) {
	where := []string{"1 = 1"}
	args := []any{}
	if from != nil {
		args = append(args, dateStr(*from))
		where = append(where, fmt.Sprintf("stat_date >= $%d::date", len(args)))
	}
	if to != nil {
		args = append(args, dateStr(*to))
		where = append(where, fmt.Sprintf("stat_date <= $%d::date", len(args)))
	}
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT model FROM ai_request_log
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY model ASC LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		if m != "" {
			out = append(out, m)
		}
	}
	return out, rows.Err()
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
