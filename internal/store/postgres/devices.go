package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/starschedule/server/internal/model"
	"github.com/starschedule/server/internal/store"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Pool() *pgxpool.Pool { return s.pool }

const deviceColumns = `id, install_id, android_id_hash, platform, app_version, version_code,
	os_version, sdk_int, arch, status, ban_reason, banned_at, token_version,
	first_seen_at, last_seen_at, launch_count, first_ip, last_ip, suspect_reinstall,
	extra, created_at, updated_at`

func scanDevice(row pgx.Row) (*model.Device, error) {
	var d model.Device
	var extra []byte
	err := row.Scan(
		&d.ID, &d.InstallID, &d.AndroidIDHash, &d.Platform, &d.AppVersion, &d.VersionCode,
		&d.OSVersion, &d.SDKInt, &d.Arch, &d.Status, &d.BanReason, &d.BannedAt, &d.TokenVersion,
		&d.FirstSeenAt, &d.LastSeenAt, &d.LaunchCount, &d.FirstIP, &d.LastIP, &d.SuspectReinstall,
		&extra, &d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	if len(extra) > 0 {
		_ = json.Unmarshal(extra, &d.Extra)
	}
	return &d, nil
}

type RegisterDeviceInput struct {
	InstallID     string
	AndroidIDHash string
	Platform      string
	AppVersion    string
	VersionCode   int
	OSVersion     string
	SDKInt        int
	Arch          string
	IP            string
	Extra         map[string]any
}

// UpsertDevice 以 install_id 幂等注册；同 android_id 的新 install 标记为疑似重装。
func (s *Store) UpsertDevice(ctx context.Context, in RegisterDeviceInput) (*model.Device, bool, error) {
	extra, err := json.Marshal(in.Extra)
	if err != nil {
		return nil, false, err
	}
	now := time.Now().UTC()
	row := s.pool.QueryRow(ctx, `
		INSERT INTO devices (id, install_id, android_id_hash, platform, app_version, version_code,
			os_version, sdk_int, arch, status, first_seen_at, last_seen_at, first_ip, last_ip,
			suspect_reinstall, extra)
		SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, 'active', $10, $10, $11, $11,
		       EXISTS (SELECT 1 FROM devices d2 WHERE d2.android_id_hash = $3 AND d2.install_id <> $2),
		       $12::jsonb
		ON CONFLICT (install_id) DO UPDATE SET
			android_id_hash = EXCLUDED.android_id_hash,
			platform        = EXCLUDED.platform,
			app_version     = EXCLUDED.app_version,
			version_code    = EXCLUDED.version_code,
			os_version      = EXCLUDED.os_version,
			sdk_int         = EXCLUDED.sdk_int,
			arch            = EXCLUDED.arch,
			last_seen_at    = GREATEST(devices.last_seen_at, EXCLUDED.last_seen_at),
			last_ip         = EXCLUDED.last_ip,
			extra           = EXCLUDED.extra,
			updated_at      = now()
		RETURNING `+deviceColumns+`, (xmax = 0) AS inserted`,
		uuid.Must(uuid.NewV7()), in.InstallID, in.AndroidIDHash, in.Platform, in.AppVersion,
		in.VersionCode, in.OSVersion, in.SDKInt, in.Arch, now, in.IP, string(extra))

	var d model.Device
	var jsonExtra []byte
	var inserted bool
	if err := row.Scan(
		&d.ID, &d.InstallID, &d.AndroidIDHash, &d.Platform, &d.AppVersion, &d.VersionCode,
		&d.OSVersion, &d.SDKInt, &d.Arch, &d.Status, &d.BanReason, &d.BannedAt, &d.TokenVersion,
		&d.FirstSeenAt, &d.LastSeenAt, &d.LaunchCount, &d.FirstIP, &d.LastIP, &d.SuspectReinstall,
		&jsonExtra, &d.CreatedAt, &d.UpdatedAt, &inserted,
	); err != nil {
		return nil, false, err
	}
	if len(jsonExtra) > 0 {
		_ = json.Unmarshal(jsonExtra, &d.Extra)
	}
	return &d, inserted, nil
}

func (s *Store) GetDeviceByID(ctx context.Context, id uuid.UUID) (*model.Device, error) {
	return scanDevice(s.pool.QueryRow(ctx, `SELECT `+deviceColumns+` FROM devices WHERE id = $1`, id))
}

func (s *Store) GetDeviceByInstallID(ctx context.Context, installID string) (*model.Device, error) {
	return scanDevice(s.pool.QueryRow(ctx, `SELECT `+deviceColumns+` FROM devices WHERE install_id = $1`, installID))
}

func (s *Store) TouchDevice(ctx context.Context, id uuid.UUID, ip string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE devices SET last_seen_at = now(), last_ip = $2, updated_at = now() WHERE id = $1`, id, ip)
	return err
}

func (s *Store) SetDeviceBanned(ctx context.Context, id uuid.UUID, banned bool, reason string) (*model.Device, error) {
	if banned {
		return scanDevice(s.pool.QueryRow(ctx, `
			UPDATE devices SET status = 'banned', ban_reason = NULLIF($2, ''), banned_at = now(),
				token_version = token_version + 1, updated_at = now()
			WHERE id = $1 RETURNING `+deviceColumns, id, reason))
	}
	return scanDevice(s.pool.QueryRow(ctx, `
		UPDATE devices SET status = 'active', ban_reason = NULL, banned_at = NULL, updated_at = now()
		WHERE id = $1 RETURNING `+deviceColumns, id))
}

type DeviceFilter struct {
	Query  string
	Status string
	Limit  int
	Cursor string
}

type deviceCursor struct {
	LastSeen time.Time `json:"t"`
	ID       uuid.UUID `json:"i"`
}

func encodeCursor(c deviceCursor) string {
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(v string) (deviceCursor, error) {
	var c deviceCursor
	raw, err := base64.RawURLEncoding.DecodeString(v)
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(raw, &c)
	return c, err
}

// ListDevices keyset 分页，默认按 last_seen_at 倒序。
func (s *Store) ListDevices(ctx context.Context, f DeviceFilter) ([]model.Device, string, bool, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 20
	}
	args := []any{}
	where := []string{"1 = 1"}
	if f.Status != "" {
		args = append(args, f.Status)
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		args = append(args, "%"+q+"%")
		where = append(where, fmt.Sprintf("(install_id ILIKE $%d OR id::text = $%d OR android_id_hash = $%d)",
			len(args), len(args), len(args)))
	}
	if f.Cursor != "" {
		cur, err := decodeCursor(f.Cursor)
		if err != nil {
			return nil, "", false, fmt.Errorf("游标无效: %w", err)
		}
		args = append(args, cur.LastSeen, cur.ID)
		where = append(where, fmt.Sprintf("(last_seen_at, id) < ($%d, $%d)", len(args)-1, len(args)))
	}
	args = append(args, f.Limit+1)
	query := fmt.Sprintf(`SELECT %s FROM devices WHERE %s
		ORDER BY last_seen_at DESC, id DESC LIMIT $%d`,
		deviceColumns, strings.Join(where, " AND "), len(args))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", false, err
	}
	defer rows.Close()

	var out []model.Device
	for rows.Next() {
		var d model.Device
		var extra []byte
		if err := rows.Scan(
			&d.ID, &d.InstallID, &d.AndroidIDHash, &d.Platform, &d.AppVersion, &d.VersionCode,
			&d.OSVersion, &d.SDKInt, &d.Arch, &d.Status, &d.BanReason, &d.BannedAt, &d.TokenVersion,
			&d.FirstSeenAt, &d.LastSeenAt, &d.LaunchCount, &d.FirstIP, &d.LastIP, &d.SuspectReinstall,
			&extra, &d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, "", false, err
		}
		if len(extra) > 0 {
			_ = json.Unmarshal(extra, &d.Extra)
		}
		out = append(out, d)
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
		next = encodeCursor(deviceCursor{LastSeen: last.LastSeenAt, ID: last.ID})
	}
	return out, next, hasMore, nil
}

func (s *Store) CountDevices(ctx context.Context, f DeviceFilter) (int64, error) {
	args := []any{}
	where := []string{"1 = 1"}
	if f.Status != "" {
		args = append(args, f.Status)
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		args = append(args, "%"+q+"%")
		where = append(where, fmt.Sprintf("(install_id ILIKE $%d OR id::text = $%d)", len(args), len(args)))
	}
	var n int64
	err := s.pool.QueryRow(ctx,
		"SELECT count(*) FROM devices WHERE "+strings.Join(where, " AND "), args...).Scan(&n)
	return n, err
}

// DeviceDailyActivity 返回某设备近 N 天活跃（用于详情页柱状图）。
type DailyActivity struct {
	StatDate    string `json:"stat_date"`
	LaunchCount int    `json:"launch_count"`
}

func (s *Store) DeviceDailyActivity(ctx context.Context, deviceID uuid.UUID, days int) ([]DailyActivity, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT to_char(stat_date, 'YYYY-MM-DD'), launch_count
		FROM daily_active_device
		WHERE device_id = $1 AND stat_date >= (CURRENT_DATE - $2::int)
		ORDER BY stat_date`, deviceID, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DailyActivity{}
	for rows.Next() {
		var a DailyActivity
		if err := rows.Scan(&a.StatDate, &a.LaunchCount); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) RecentLaunches(ctx context.Context, deviceID uuid.UUID, limit int) ([]model.LaunchEvent, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, device_id, stat_date, occurred_at, received_at, launch_type, platform,
		       app_version, os_version, arch, session_id
		FROM launch_events WHERE device_id = $1 ORDER BY occurred_at DESC LIMIT $2`, deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.LaunchEvent{}
	for rows.Next() {
		var e model.LaunchEvent
		if err := rows.Scan(&e.ID, &e.DeviceID, &e.StatDate, &e.OccurredAt, &e.ReceivedAt,
			&e.LaunchType, &e.Platform, &e.AppVersion, &e.OSVersion, &e.Arch, &e.SessionID); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
