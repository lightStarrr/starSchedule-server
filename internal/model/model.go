// Package model 定义跨层共享的领域实体。
package model

import (
	"time"

	"github.com/google/uuid"
)

const (
	DeviceStatusActive = "active"
	DeviceStatusBanned = "banned"

	PlatformAndroid = "android"

	ModelSourceFetched = "fetched"
	ModelSourceManual  = "manual"

	RequestSourceDevice     = "device"
	RequestSourcePlayground = "playground"
)

type Device struct {
	ID               uuid.UUID
	InstallID        string
	AndroidIDHash    string
	Platform         string
	AppVersion       string
	VersionCode      int
	OSVersion        string
	SDKInt           int
	Arch             string
	Status           string
	BanReason        *string
	BannedAt         *time.Time
	TokenVersion     int
	FirstSeenAt      time.Time
	LastSeenAt       time.Time
	LaunchCount      int64
	FirstIP          *string
	LastIP           *string
	SuspectReinstall bool
	Extra            map[string]any
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (d *Device) Banned() bool { return d.Status == DeviceStatusBanned }

type LaunchEvent struct {
	ID         uuid.UUID
	DeviceID   uuid.UUID
	StatDate   time.Time
	OccurredAt time.Time
	ReceivedAt time.Time
	LaunchType string
	Platform   string
	AppVersion string
	OSVersion  string
	Arch       string
	SessionID  string
	Extra      map[string]any
}

type Provider struct {
	ID             uuid.UUID
	Name           string
	BaseURL        string
	APIKeyCipher   []byte
	Enabled        bool
	SortOrder      int
	TimeoutSeconds int
	LastCheckAt    *time.Time
	LastCheckOK    *bool
	LastCheckError *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ProviderModel struct {
	ID            uuid.UUID
	ProviderID    uuid.UUID
	ModelID       string
	Alias         string
	DisplayName   string
	ContextWindow int
	Source        string
	Enabled       bool
	FetchedAt     time.Time
	CreatedAt     time.Time
}

// ActiveAIConfig 是当前生效的供应商 + 模型组合。
type ActiveAIConfig struct {
	Provider Provider
	Model    ProviderModel
}

type UsageRow struct {
	RequestCount     int64
	ErrorCount       int64
	PromptTokens     int64
	CompletionTokens int64
	EstimatedCount   int64
}

type AdminUser struct {
	ID           uuid.UUID
	Username     string
	PasswordHash string
	Role         string
	CreatedAt    time.Time
	LastLoginAt  *time.Time
}

type AuditLog struct {
	ID            uuid.UUID
	AdminID       *uuid.UUID
	AdminUsername string
	Action        string
	TargetType    string
	TargetID      string
	Detail        map[string]any
	IP            string
	CreatedAt     time.Time
}

type DailyPoint struct {
	Date       time.Time
	DAU        int64
	Launches   int64
	NewDevices int64
	AIRequests int64
	AITokens   int64
}

type DistributionItem struct {
	Key   string
	Count int64
}
