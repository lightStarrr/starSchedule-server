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
	// 缺少时间的在线上报使用接收时间；越界的客户端时间不用于覆盖设备元信息。
	metadataAt := in.LaunchedAt
	if metadataAt == nil {
		metadataAt = &now
	}
	outsideWindow := in.LaunchedAt == nil ||
		in.LaunchedAt.After(now.Add(10*time.Minute)) ||
		in.LaunchedAt.Before(now.Add(-7*24*time.Hour))
	if outsideWindow {
		if in.LaunchedAt != nil {
			metadataAt = nil
		}
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
		Metadata: postgres.DeviceMetadata{
			ReportedAt: metadataAt,
			AppVersion: strings.TrimSpace(in.AppVersion), VersionCode: in.VersionCode,
			OSVersion: strings.TrimSpace(in.OSVersion), SDKInt: in.SDKInt, Arch: strings.TrimSpace(in.Arch),
		},
	})
}
