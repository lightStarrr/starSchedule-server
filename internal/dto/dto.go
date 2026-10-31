// Package dto 定义对外请求/响应结构。
package dto

import "time"

type RegisterRequest struct {
	InstallID   string `json:"install_id" binding:"required,min=8,max=64"`
	AndroidID   string `json:"android_id" binding:"required,min=8,max=64"`
	Platform    string `json:"platform" binding:"omitempty,oneof=android"`
	AppVersion  string `json:"app_version" binding:"required,max=32"`
	VersionCode int    `json:"version_code" binding:"required,min=1,max=2100000000"`
	OSVersion   string `json:"os_version" binding:"required,max=16"`
	SDKInt      int    `json:"sdk_int" binding:"required,min=21,max=99"`
	Arch        string `json:"arch" binding:"required,max=32"`
	Locale      string `json:"locale" binding:"omitempty,max=16"`
	Timezone    string `json:"timezone" binding:"omitempty,max=64"`
}

type RegisterResponse struct {
	DeviceID    string    `json:"device_id"`
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresIn   int       `json:"expires_in"`
	ExpiresAt   time.Time `json:"expires_at"`
	IsNew       bool      `json:"is_new"`
	ServerTime  time.Time `json:"server_time"`
}

type LaunchRequest struct {
	SessionID   string         `json:"session_id" binding:"required,min=8,max=64"`
	LaunchedAt  *time.Time     `json:"launched_at"`
	LaunchType  string         `json:"launch_type" binding:"omitempty,oneof=cold warm"`
	AppVersion  string         `json:"app_version" binding:"omitempty,max=32"`
	VersionCode int            `json:"version_code"`
	OSVersion   string         `json:"os_version" binding:"omitempty,max=16"`
	SDKInt      int            `json:"sdk_int"`
	Arch        string         `json:"arch" binding:"omitempty,max=32"`
	Extra       map[string]any `json:"extra"`
}

type AdminLoginRequest struct {
	Username string `json:"username" binding:"required,max=64"`
	Password string `json:"password" binding:"required,min=6,max=128"`
}

type ProviderRequest struct {
	Name           string `json:"name" binding:"omitempty,max=64"`
	BaseURL        string `json:"base_url" binding:"omitempty,url,max=255"`
	APIKey         string `json:"api_key" binding:"omitempty,max=512"`
	Enabled        *bool  `json:"enabled"`
	TimeoutSeconds int    `json:"timeout_seconds" binding:"omitempty,min=5,max=600"`
}

type ModelCreateRequest struct {
	ProviderID    string `json:"provider_id" binding:"required,uuid"`
	ModelID       string `json:"model_id" binding:"required,max=128"`
	Alias         string `json:"alias" binding:"omitempty,max=128"`
	DisplayName   string `json:"display_name" binding:"omitempty,max=128"`
	ContextWindow int    `json:"context_window" binding:"omitempty,min=0,max=10000000"`
	Enabled       *bool  `json:"enabled"`
}

type BulkDeleteModelsRequest struct {
	IDs []string `json:"ids" binding:"required,min=1,max=500,dive,uuid"`
}

type ModelPatchRequest struct {
	Alias         *string `json:"alias" binding:"omitempty,max=128"`
	DisplayName   *string `json:"display_name" binding:"omitempty,max=128"`
	ContextWindow *int    `json:"context_window" binding:"omitempty,min=0,max=10000000"`
	Enabled       *bool   `json:"enabled"`
}

type BanRequest struct {
	Reason string `json:"reason" binding:"omitempty,max=200"`
	Banned bool   `json:"banned"`
}

// SettingsRequest 只接受白名单键，值可为数字或 null（null 表示恢复默认）。
type SettingsRequest struct {
	Values map[string]any `json:"values" binding:"required"`
}

type DeviceSummary struct {
	ID               string     `json:"id"`
	InstallID        string     `json:"install_id"`
	Platform         string     `json:"platform"`
	AppVersion       string     `json:"app_version"`
	VersionCode      int        `json:"version_code"`
	OSVersion        string     `json:"os_version"`
	SDKInt           int        `json:"sdk_int"`
	Arch             string     `json:"arch"`
	Status           string     `json:"status"`
	BanReason        *string    `json:"ban_reason,omitempty"`
	BannedAt         *time.Time `json:"banned_at,omitempty"`
	FirstSeenAt      time.Time  `json:"first_seen_at"`
	LastSeenAt       time.Time  `json:"last_seen_at"`
	LaunchCount      int64      `json:"launch_count"`
	LastIP           *string    `json:"last_ip,omitempty"`
	SuspectReinstall bool       `json:"suspect_reinstall"`
}
