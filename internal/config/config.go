package config

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config 全部来自环境变量，缺必填项直接启动失败。
type Config struct {
	Env      string `env:"APP_ENV" envDefault:"production"`
	HTTPAddr string `env:"HTTP_ADDR" envDefault:":8080"`
	AppTZ    string `env:"APP_TZ" envDefault:"Asia/Shanghai"`
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`

	DatabaseURL string `env:"DATABASE_URL,required"`
	DBMaxConns  int32  `env:"DB_MAX_CONNS" envDefault:"20"`
	DBMinConns  int32  `env:"DB_MIN_CONNS" envDefault:"2"`

	RedisAddr     string `env:"REDIS_ADDR,required"`
	RedisPassword string `env:"REDIS_PASSWORD"`
	RedisDB       int    `env:"REDIS_DB" envDefault:"0"`

	DeviceJWTSecret    string        `env:"DEVICE_JWT_SECRET,required"`
	AdminJWTSecret     string        `env:"ADMIN_JWT_SECRET,required"`
	DeviceTokenTTL     time.Duration `env:"DEVICE_TOKEN_TTL" envDefault:"720h"`
	AdminTokenTTL      time.Duration `env:"ADMIN_TOKEN_TTL" envDefault:"168h"`
	DeviceRefreshAfter time.Duration `env:"DEVICE_TOKEN_REFRESH_AFTER" envDefault:"552h"`

	// FingerprintPepper 用于对 ANDROID_ID 做不可逆加盐哈希，禁止外泄。
	FingerprintPepper string `env:"DEVICE_FINGERPRINT_PEPPER,required"`
	// EncryptionKey 为 AES-256-GCM 密钥，base64(32 字节)。
	EncryptionKey string `env:"APP_ENCRYPTION_KEY,required"`

	AdminUsername string `env:"ADMIN_USERNAME" envDefault:"admin"`
	AdminPassword string `env:"ADMIN_PASSWORD,required"`

	CORSOrigins []string `env:"CORS_ORIGINS" envSeparator:","`

	MetricsEnabled bool `env:"METRICS_ENABLED" envDefault:"true"`
	// AllowInsecureCookie 仅本地 http 调试时置 true，生产必须为 false。
	AllowInsecureCookie bool `env:"ALLOW_INSECURE_COOKIE" envDefault:"false"`

	Location *time.Location `env:"-"`
}

func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("解析环境变量失败: %w", err)
	}

	loc, err := time.LoadLocation(cfg.AppTZ)
	if err != nil {
		return nil, fmt.Errorf("APP_TZ 无效 %q: %w", cfg.AppTZ, err)
	}
	cfg.Location = loc

	if len(cfg.DeviceJWTSecret) < 32 {
		return nil, fmt.Errorf("DEVICE_JWT_SECRET 至少 32 字符")
	}
	if len(cfg.AdminJWTSecret) < 32 {
		return nil, fmt.Errorf("ADMIN_JWT_SECRET 至少 32 字符")
	}
	if _, err := cfg.DecodedEncryptionKey(); err != nil {
		return nil, err
	}
	if cfg.DeviceTokenTTL <= cfg.DeviceRefreshAfter {
		return nil, fmt.Errorf("DEVICE_TOKEN_REFRESH_AFTER 必须小于 DEVICE_TOKEN_TTL")
	}
	for i := range cfg.CORSOrigins {
		cfg.CORSOrigins[i] = strings.TrimSpace(cfg.CORSOrigins[i])
	}
	return cfg, nil
}

// DecodedEncryptionKey 返回 32 字节 AES 密钥。
func (c *Config) DecodedEncryptionKey() ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(c.EncryptionKey))
	if err != nil {
		return nil, fmt.Errorf("APP_ENCRYPTION_KEY 必须是 base64: %w", err)
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("APP_ENCRYPTION_KEY 解码后必须是 32 字节，当前 %d", len(raw))
	}
	return raw, nil
}

func (c *Config) LogLevelValue() slog.Level {
	switch strings.ToLower(c.LogLevel) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func (c *Config) IsDev() bool { return c.Env == "development" }
