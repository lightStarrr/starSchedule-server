package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/starschedule/server/internal/model"
)

type DayMetrics struct {
	DAU        int64 `json:"dau"`
	Launches   int64 `json:"launches"`
	NewDevices int64 `json:"new_devices"`
	AIRequests int64 `json:"ai_requests"`
	AITokens   int64 `json:"ai_tokens"`
}

// ActiveDevicesBetween 统计区间内活跃设备数（跨日去重，WAU/MAU 用）。
func (s *Store) ActiveDevicesBetween(ctx context.Context, from, to time.Time) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `
		SELECT count(DISTINCT device_id) FROM daily_active_device
		WHERE stat_date BETWEEN $1::date AND $2::date`, dateStr(from), dateStr(to)).Scan(&n)
	return n, err
}

func (s *Store) DayMetricsOn(ctx context.Context, date time.Time, tz string) (DayMetrics, error) {
	var m DayMetrics
	row := s.pool.QueryRow(ctx, `
		SELECT
			COALESCE((SELECT count(*) FROM daily_active_device WHERE stat_date = $1::date), 0),
			COALESCE((SELECT sum(launch_count) FROM daily_active_device WHERE stat_date = $1::date), 0),
			COALESCE((SELECT count(*) FROM devices WHERE (first_seen_at AT TIME ZONE $2::text)::date = $1::date), 0),
			COALESCE((SELECT sum(request_count) FROM ai_usage_daily WHERE stat_date = $1::date), 0),
			COALESCE((SELECT sum(prompt_tokens + completion_tokens) FROM ai_usage_daily WHERE stat_date = $1::date), 0)`,
		dateStr(date), tz)
	err := row.Scan(&m.DAU, &m.Launches, &m.NewDevices, &m.AIRequests, &m.AITokens)
	return m, err
}

// Trend 返回区间内逐日指标（含补齐空缺日）。
func (s *Store) Trend(ctx context.Context, from, to time.Time, tz string) ([]model.DailyPoint, error) {
	rows, err := s.pool.Query(ctx, `
		WITH days AS (
			SELECT generate_series($1::date, $2::date, interval '1 day')::date AS d
		), active AS (
			SELECT stat_date AS d, count(*) AS dau, sum(launch_count) AS launches
			FROM daily_active_device WHERE stat_date BETWEEN $1::date AND $2::date GROUP BY 1
		), fresh AS (
			SELECT (first_seen_at AT TIME ZONE $3::text)::date AS d, count(*) AS n
			FROM devices
			WHERE (first_seen_at AT TIME ZONE $3::text)::date BETWEEN $1::date AND $2::date
			GROUP BY 1
		), ai AS (
			SELECT stat_date AS d, sum(request_count) AS req,
			       sum(prompt_tokens + completion_tokens) AS tok
			FROM ai_usage_daily WHERE stat_date BETWEEN $1::date AND $2::date GROUP BY 1
		)
		SELECT days.d,
		       COALESCE(active.dau, 0), COALESCE(active.launches, 0), COALESCE(fresh.n, 0),
		       COALESCE(ai.req, 0), COALESCE(ai.tok, 0)
		FROM days
		LEFT JOIN active ON active.d = days.d
		LEFT JOIN fresh  ON fresh.d  = days.d
		LEFT JOIN ai     ON ai.d     = days.d
		ORDER BY days.d`, dateStr(from), dateStr(to), tz)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.DailyPoint{}
	for rows.Next() {
		var p model.DailyPoint
		if err := rows.Scan(&p.Date, &p.DAU, &p.Launches, &p.NewDevices, &p.AIRequests, &p.AITokens); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Distribution 按维度统计活跃设备数，仅接受白名单维度。
func (s *Store) Distribution(ctx context.Context, dim string, from time.Time, limit int) ([]model.DistributionItem, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	var query string
	switch dim {
	case "platform":
		query = `SELECT platform AS k, count(DISTINCT device_id) AS c
			FROM daily_active_device WHERE stat_date >= $1::date GROUP BY 1 ORDER BY c DESC LIMIT $2`
	case "app_version":
		query = `SELECT COALESCE(NULLIF(app_version, ''), '未知') AS k, count(DISTINCT device_id) AS c
			FROM daily_active_device WHERE stat_date >= $1::date GROUP BY 1 ORDER BY c DESC LIMIT $2`
	case "os_version":
		query = `SELECT COALESCE(NULLIF(os_version, ''), '未知') AS k, count(DISTINCT device_id) AS c
			FROM launch_events WHERE stat_date >= $1::date GROUP BY 1 ORDER BY c DESC LIMIT $2`
	default:
		return nil, fmt.Errorf("不支持的维度 %q", dim)
	}
	rows, err := s.pool.Query(ctx, query, dateStr(from), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.DistributionItem{}
	for rows.Next() {
		var it model.DistributionItem
		if err := rows.Scan(&it.Key, &it.Count); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// Today 返回应用时区下的「今天」，避免全链路时区混乱。
func Today(loc *time.Location) time.Time {
	now := time.Now().In(loc)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
}
