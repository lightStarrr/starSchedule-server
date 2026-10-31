package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/starschedule/server/internal/auth"
	"github.com/starschedule/server/internal/model"
	"github.com/starschedule/server/internal/store"
	"github.com/starschedule/server/internal/store/postgres"
	redisc "github.com/starschedule/server/internal/store/redis"
)

type DeviceService struct {
	store    *postgres.Store
	rd       *redisc.Client
	jwt      *auth.Manager
	pepper   string
	location *time.Location
	settings *SettingsService
}

func NewDeviceService(st *postgres.Store, rd *redisc.Client, jwtMgr *auth.Manager,
	pepper string, loc *time.Location, settings *SettingsService) *DeviceService {
	return &DeviceService{store: st, rd: rd, jwt: jwtMgr, pepper: pepper, location: loc, settings: settings}
}

type RegisterInput struct {
	InstallID   string
	AndroidID   string
	Platform    string
	AppVersion  string
	VersionCode int
	OSVersion   string
	SDKInt      int
	Arch        string
	Locale      string
	Timezone    string
	IP          string
}

type RegisterResult struct {
	Device    *model.Device
	Token     string
	ExpiresAt time.Time
	IsNew     bool
}

// HashAndroidID 对 ANDROID_ID 加盐哈希，库内不留明文。
func (s *DeviceService) HashAndroidID(androidID string) string {
	sum := sha256.Sum256([]byte(s.pepper + ":" + strings.ToLower(strings.TrimSpace(androidID))))
	return hex.EncodeToString(sum[:])
}

func (s *DeviceService) Register(ctx context.Context, in RegisterInput) (*RegisterResult, error) {
	platform := strings.ToLower(strings.TrimSpace(in.Platform))
	if platform == "" {
		platform = model.PlatformAndroid
	}
	extra := map[string]any{}
	if in.Locale != "" {
		extra["locale"] = in.Locale
	}
	if in.Timezone != "" {
		extra["timezone"] = in.Timezone
	}

	dev, isNew, err := s.store.UpsertDevice(ctx, postgres.RegisterDeviceInput{
		InstallID:     strings.TrimSpace(in.InstallID),
		AndroidIDHash: s.HashAndroidID(in.AndroidID),
		Platform:      platform,
		AppVersion:    in.AppVersion,
		VersionCode:   in.VersionCode,
		OSVersion:     in.OSVersion,
		SDKInt:        in.SDKInt,
		Arch:          in.Arch,
		IP:            in.IP,
		Extra:         extra,
	})
	if err != nil {
		return nil, err
	}
	if dev.Banned() {
		return nil, ErrDeviceBanned
	}

	token, exp, err := s.jwt.IssueDevice(dev.ID, dev.TokenVersion, dev.Platform)
	if err != nil {
		return nil, err
	}
	return &RegisterResult{Device: dev, Token: token, ExpiresAt: exp, IsNew: isNew}, nil
}

// Authorize 供鉴权中间件使用：三级校验封禁（Redis → DB），并校验 token 版本。
func (s *DeviceService) Authorize(ctx context.Context, deviceID uuid.UUID, tokenVersion int) (*model.Device, error) {
	idStr := deviceID.String()
	if banned, hit := s.rd.IsBanned(ctx, idStr); hit && banned {
		return nil, ErrDeviceBanned
	}

	dev, err := s.store.GetDeviceByID(ctx, deviceID)
	if err != nil {
		if err == store.ErrNotFound {
			return nil, ErrTokenStale
		}
		return nil, err
	}
	if dev.Banned() {
		s.rd.SetBanned(ctx, idStr)
		return nil, ErrDeviceBanned
	}
	if dev.TokenVersion != tokenVersion {
		return nil, ErrTokenStale
	}
	return dev, nil
}

// RefreshIfNeeded 在 token 进入续期窗口时重读设备状态并签发新 token。
func (s *DeviceService) RefreshIfNeeded(ctx context.Context, claims *auth.DeviceClaims) (string, bool) {
	if !s.jwt.NeedsRefresh(claims) {
		return "", false
	}
	deviceID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return "", false
	}
	dev, err := s.store.GetDeviceByID(ctx, deviceID)
	if err != nil || dev.Banned() || dev.TokenVersion != claims.Ver {
		return "", false
	}
	token, _, err := s.jwt.IssueDevice(dev.ID, dev.TokenVersion, dev.Platform)
	if err != nil {
		return "", false
	}
	return token, true
}

func (s *DeviceService) Today() time.Time {
	now := time.Now().In(s.location)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.location)
}

func (s *DeviceService) Settings() *SettingsService { return s.settings }

func (s *DeviceService) JWT() *auth.Manager { return s.jwt }
