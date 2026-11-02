package postgres

import (
	"context"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestLaunchMetadataIntegration(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	dev, _, err := s.UpsertDevice(ctx, RegisterDeviceInput{InstallID: uuid.NewString(), AndroidIDHash: "hash", Platform: "android", AppVersion: "1", VersionCode: 1, OSVersion: "13", SDKInt: 33, Arch: "old"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(time.Minute)
	send := func(session string, at time.Time, meta DeviceMetadata) bool {
		t.Helper()
		meta.ReportedAt = &at
		ok, err := s.InsertLaunch(ctx, LaunchInput{ID: uuid.New(), DeviceID: dev.ID, StatDate: now, OccurredAt: at, SessionID: session, Platform: "android", LaunchType: "cold", AppVersion: meta.AppVersion, Metadata: meta})
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	current := DeviceMetadata{AppVersion: "2", VersionCode: 2, OSVersion: "14", SDKInt: 34, Arch: "arm64"}
	if !send("session-new", now, current) {
		t.Fatal("首次上报应计数")
	}
	if send("session-new", now, DeviceMetadata{AppVersion: "bad", VersionCode: 999}) {
		t.Fatal("重复 session 不计数")
	}
	send("session-old", now.Add(-time.Second), DeviceMetadata{AppVersion: "1", VersionCode: 1, OSVersion: "13", SDKInt: 33, Arch: "old"})
	send("session-partial", now.Add(time.Second), DeviceMetadata{OSVersion: "15"})
	got, err := s.GetDeviceByID(ctx, dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AppVersion != "2" || got.VersionCode != 2 || got.OSVersion != "15" || got.SDKInt != 34 || got.Arch != "arm64" || got.LaunchCount != 3 {
		t.Fatalf("旧上报与缺省字段不应覆盖新信息，重复不计数: %+v", got)
	}
	// 按事件时间而非版本号排序，允许真实的新近降级。
	send("session-downgrade", now.Add(2*time.Second), DeviceMetadata{AppVersion: "1", VersionCode: 1})
	got, err = s.GetDeviceByID(ctx, dev.ID)
	if err != nil || got.VersionCode != 1 || got.AppVersion != "1" {
		t.Fatalf("应保留真实的新近降级: %+v %v", got, err)
	}
	var version string
	if err := s.pool.QueryRow(ctx, `SELECT app_version FROM daily_active_device WHERE device_id=$1`, dev.ID).Scan(&version); err != nil || version != "1" {
		t.Fatalf("日聚合也需按事件时间更新: %q %v", version, err)
	}
}
