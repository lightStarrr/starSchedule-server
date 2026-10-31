package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/starschedule/server/internal/model"
	"github.com/starschedule/server/internal/store/postgres"
)

type LaunchService struct {
	store *postgres.Store
	loc   *time.Location
}

func NewLaunchService(st *postgres.Store, loc *time.Location) *LaunchService {
	return &LaunchService{store: st, loc: loc}
}

type LaunchInput struct {
	SessionID   string
	LaunchedAt  *time.Time
	LaunchType  string
	AppVersion  string
	VersionCode int
	OSVersion   string
	SDKInt      int
	Arch        string
	Extra       map[string]any
}

// Report 记录一次启动。客户端时钟不可信：偏差超窗口时一律用服务端时间。
func (s *LaunchService) Report(ctx context.Context, dev *model.Device, in LaunchInput) (bool, error) {
	now := time.Now().UTC()
	outsideWindow := in.LaunchedAt == nil ||
		in.LaunchedAt.After(now.Add(10*time.Minute)) ||
		in.LaunchedAt.Before(now.Add(-7*24*time.Hour))
	if outsideWindow {
		t := now
		in.LaunchedAt = &t
	}

	appVersion := strings.TrimSpace(in.AppVersion)
	if appVersion == "" {
		appVersion = dev.AppVersion
	}
	osVersion := strings.TrimSpace(in.OSVersion)
	if osVersion == "" {
		osVersion = dev.OSVersion
	}
	arch := strings.TrimSpace(in.Arch)
	if arch == "" {
		arch = dev.Arch
	}
	launchType := strings.ToLower(strings.TrimSpace(in.LaunchType))
	if launchType != "cold" && launchType != "warm" {
		launchType = "cold"
	}

	local := in.LaunchedAt.In(s.loc)
	statDate := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, s.loc)

	return s.store.InsertLaunch(ctx, postgres.LaunchInput{
		ID:         uuid.Must(uuid.NewV7()),
		DeviceID:   dev.ID,
		StatDate:   statDate,
		OccurredAt: *in.LaunchedAt,
		LaunchType: launchType,
		Platform:   dev.Platform,
		AppVersion: appVersion,
		OSVersion:  osVersion,
		Arch:       arch,
		SessionID:  strings.TrimSpace(in.SessionID),
		Extra:      in.Extra,
	})
}
