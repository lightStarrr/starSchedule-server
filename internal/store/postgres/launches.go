package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type LaunchInput struct {
	Metadata   DeviceMetadata
	ID         uuid.UUID
	DeviceID   uuid.UUID
	StatDate   time.Time
	OccurredAt time.Time
	LaunchType string
	Platform   string
	AppVersion string
	OSVersion  string
	Arch       string
	SessionID  string
	Extra      map[string]any
}

type DeviceMetadata struct {
	ReportedAt  *time.Time
	AppVersion  string
	VersionCode int
	OSVersion   string
	SDKInt      int
	Arch        string
}

// InsertLaunch 在同一事务内写事件、日聚合、设备冗余计数。
// 返回 false 表示 session_id 重复（客户端重试），不计入统计。
func (s *Store) InsertLaunch(ctx context.Context, in LaunchInput) (bool, error) {
	extra, err := json.Marshal(in.Extra)
	if err != nil {
		return false, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var one int
	err = tx.QueryRow(ctx, `
		INSERT INTO launch_events (id, device_id, stat_date, occurred_at, launch_type,
			platform, app_version, os_version, arch, session_id, extra)
		VALUES ($1, $2, $3::date, $4, $5, $6, $7, $8, $9, $10, $11::jsonb)
		ON CONFLICT (device_id, session_id, stat_date) DO NOTHING
		RETURNING 1`,
		in.ID, in.DeviceID, dateStr(in.StatDate), in.OccurredAt, in.LaunchType,
		in.Platform, in.AppVersion, in.OSVersion, in.Arch, in.SessionID, string(extra)).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if _, err = tx.Exec(ctx, `
		INSERT INTO daily_active_device (stat_date, device_id, platform, app_version, launch_count,
			first_launch_at, last_launch_at)
		VALUES ($1::date, $2, $3, $4, 1, $5, $5)
		ON CONFLICT (stat_date, device_id) DO UPDATE SET
			launch_count   = daily_active_device.launch_count + 1,
			last_launch_at = GREATEST(daily_active_device.last_launch_at, EXCLUDED.last_launch_at),
			app_version    = CASE WHEN EXCLUDED.last_launch_at >= daily_active_device.last_launch_at THEN EXCLUDED.app_version ELSE daily_active_device.app_version END`,
		dateStr(in.StatDate), in.DeviceID, in.Platform, in.AppVersion, in.OccurredAt); err != nil {
		return false, err
	}
	meta := in.Metadata
	if meta.ReportedAt != nil && (meta.AppVersion != "" || meta.VersionCode > 0 || meta.OSVersion != "" || meta.SDKInt > 0 || meta.Arch != "") {
		if _, err := tx.Exec(ctx, `UPDATE devices SET
			app_version=COALESCE(NULLIF($3,''),app_version), version_code=CASE WHEN $4>0 THEN $4 ELSE version_code END,
			os_version=COALESCE(NULLIF($5,''),os_version), sdk_int=CASE WHEN $6>0 THEN $6 ELSE sdk_int END,
			arch=COALESCE(NULLIF($7,''),arch), metadata_reported_at=$2
			WHERE id=$1 AND (metadata_reported_at IS NULL OR metadata_reported_at<=$2)`,
			in.DeviceID, meta.ReportedAt, meta.AppVersion, meta.VersionCode, meta.OSVersion, meta.SDKInt, meta.Arch); err != nil {
			return false, err
		}
	}

	if _, err = tx.Exec(ctx, `
		UPDATE devices SET launch_count = launch_count + 1, last_seen_at = GREATEST(last_seen_at, $2),
			updated_at = now() WHERE id = $1`, in.DeviceID, in.OccurredAt); err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// RebuildDailyStats 重算指定日期的汇总行（T+1 校正）。
func (s *Store) RebuildDailyStats(ctx context.Context, date time.Time, tz string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO daily_stats (stat_date, dau, launches, new_devices, ai_requests, ai_tokens, updated_at)
		SELECT $1::date,
		       COALESCE((SELECT count(*) FROM daily_active_device WHERE stat_date = $1::date), 0),
		       COALESCE((SELECT sum(launch_count) FROM daily_active_device WHERE stat_date = $1::date), 0),
		       COALESCE((SELECT count(*) FROM devices WHERE (first_seen_at AT TIME ZONE $2::text)::date = $1::date), 0),
		       COALESCE((SELECT sum(request_count) FROM ai_usage_daily WHERE stat_date = $1::date), 0),
		       COALESCE((SELECT sum(prompt_tokens + completion_tokens) FROM ai_usage_daily WHERE stat_date = $1::date), 0),
		       now()
		ON CONFLICT (stat_date) DO UPDATE SET
			dau = EXCLUDED.dau, launches = EXCLUDED.launches, new_devices = EXCLUDED.new_devices,
			ai_requests = EXCLUDED.ai_requests, ai_tokens = EXCLUDED.ai_tokens, updated_at = now()`,
		dateStr(date), tz)
	return err
}

// PruneOldEvents 清理过期明细并归档旧分区。
func (s *Store) PruneOldEvents(ctx context.Context, launchKeepDays, aiKeepDays int) error {
	if launchKeepDays > 0 {
		if _, err := s.pool.Exec(ctx,
			`DELETE FROM launch_events WHERE stat_date < CURRENT_DATE - $1::int`, launchKeepDays); err != nil {
			return err
		}
	}
	if aiKeepDays > 0 {
		if _, err := s.pool.Exec(ctx,
			`DELETE FROM ai_request_log WHERE stat_date < CURRENT_DATE - $1::int`, aiKeepDays); err != nil {
			return err
		}
	}
	return nil
}
