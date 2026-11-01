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
	ID         uuid.UUID      `json:"id"`
	DeviceID   uuid.UUID      `json:"device_id"`
	StatDate   time.Time      `json:"stat_date"`
	OccurredAt time.Time      `json:"occurred_at"`
	ReceivedAt time.Time      `json:"received_at"`
	LaunchType string         `json:"launch_type"`
	Platform   string         `json:"platform"`
	AppVersion string         `json:"app_version"`
	OSVersion  string         `json:"os_version"`
	Arch       string         `json:"arch"`
	SessionID  string         `json:"session_id"`
	Extra      map[string]any `json:"extra,omitempty"`
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
	ID               uuid.UUID
	ProviderID       uuid.UUID
	ModelID          string
	Alias            string
	DisplayName      string
	ContextWindow    int
	Source           string
	Enabled          bool
	ReasoningMode    string
	ReasoningEfforts []string
	ReasoningDefault string
	FetchedAt        time.Time
	CreatedAt        time.Time
}

// ActiveAIConfig 是当前生效的供应商 + 模型组合。
type ActiveAIConfig struct {
	Provider Provider
	Model    ProviderModel
}

// CatalogEntry 是「可调用模型」的扁平视图，供后台选择器使用。
type CatalogEntry struct {
	ProviderID       uuid.UUID `json:"provider_id"`
	ProviderName     string    `json:"provider_name"`
	ModelUUID        uuid.UUID `json:"id"`
	ModelID          string    `json:"model_id"`
	Alias            string    `json:"alias"`
	DisplayName      string    `json:"display_name"`
	ContextWindow    int       `json:"context_window"`
	Enabled          bool      `json:"enabled"`
	Source           string    `json:"source"`
	ReasoningMode    string    `json:"reasoning_mode"`
	ReasoningEfforts []string  `json:"reasoning_efforts"`
	ReasoningDefault string    `json:"reasoning_default"`
	FetchedAt        time.Time `json:"fetched_at"`
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
	ID            uuid.UUID      `json:"id"`
	AdminID       *uuid.UUID     `json:"admin_id,omitempty"`
	AdminUsername string         `json:"admin_username"`
	Action        string         `json:"action"`
	TargetType    string         `json:"target_type"`
	TargetID      string         `json:"target_id"`
	Detail        map[string]any `json:"detail,omitempty"`
	IP            string         `json:"ip"`
	CreatedAt     time.Time      `json:"created_at"`
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
