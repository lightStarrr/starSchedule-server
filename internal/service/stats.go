package service

import (
	"context"
	"time"

	"github.com/starschedule/server/internal/store/postgres"
)

type StatsService struct {
	store *postgres.Store
	loc   *time.Location
}

func NewStatsService(st *postgres.Store, loc *time.Location) *StatsService {
	return &StatsService{store: st, loc: loc}
}

type Overview struct {
	Today     postgres.DayMetrics `json:"today"`
	Yesterday postgres.DayMetrics `json:"yesterday"`
	WAU       int64               `json:"wau"`
	MAU       int64               `json:"mau"`
	Date      string              `json:"date"`
}

func (s *StatsService) Overview(ctx context.Context) (Overview, error) {
	today := postgres.Today(s.loc)
	yesterday := today.AddDate(0, 0, -1)
	tz := s.loc.String()

	tm, err := s.store.DayMetricsOn(ctx, today, tz)
	if err != nil {
		return Overview{}, err
	}
	ym, err := s.store.DayMetricsOn(ctx, yesterday, tz)
	if err != nil {
		return Overview{}, err
	}
	wau, err := s.store.ActiveDevicesBetween(ctx, today.AddDate(0, 0, -6), today)
	if err != nil {
		return Overview{}, err
	}
	mau, err := s.store.ActiveDevicesBetween(ctx, today.AddDate(0, 0, -29), today)
	if err != nil {
		return Overview{}, err
	}
	return Overview{
		Today:     tm,
		Yesterday: ym,
		WAU:       wau,
		MAU:       mau,
		Date:      today.Format("2006-01-02"),
	}, nil
}

func (s *StatsService) Trend(ctx context.Context, days int) ([]DailyPoint, error) {
	if days <= 0 || days > 180 {
		days = 30
	}
	to := postgres.Today(s.loc)
	from := to.AddDate(0, 0, -(days - 1))
	rows, err := s.store.Trend(ctx, from, to, s.loc.String())
	if err != nil {
		return nil, err
	}
	out := make([]DailyPoint, 0, len(rows))
	for _, r := range rows {
		out = append(out, DailyPoint{
			Date:       r.Date.Format("2006-01-02"),
			DAU:        r.DAU,
			Launches:   r.Launches,
			NewDevices: r.NewDevices,
			AIRequests: r.AIRequests,
			AITokens:   r.AITokens,
		})
	}
	return out, nil
}

type DailyPoint struct {
	Date       string `json:"date"`
	DAU        int64  `json:"dau"`
	Launches   int64  `json:"launches"`
	NewDevices int64  `json:"new_devices"`
	AIRequests int64  `json:"ai_requests"`
	AITokens   int64  `json:"ai_tokens"`
}

type DistributionItem struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

func (s *StatsService) Distribution(ctx context.Context, dim string, days int) ([]DistributionItem, error) {
	if days <= 0 || days > 180 {
		days = 7
	}
	from := postgres.Today(s.loc).AddDate(0, 0, -(days - 1))
	rows, err := s.store.Distribution(ctx, dim, from, 10)
	if err != nil {
		return nil, err
	}
	out := make([]DistributionItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, DistributionItem{Key: r.Key, Count: r.Count})
	}
	return out, nil
}
