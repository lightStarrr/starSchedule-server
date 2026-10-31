// Package auth 负责 JWT 签发与校验、口令哈希。
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var (
	ErrTokenInvalid = errors.New("token 无效")
	ErrTokenExpired = errors.New("token 已过期")
)

const (
	issuer      = "starschedule"
	audDevice   = "device"
	audAdmin    = "admin"
	signingAlgo = "HS256"
)

type DeviceClaims struct {
	Ver  int    `json:"ver"`
	Plat string `json:"plat"`
	jwt.RegisteredClaims
}

type AdminClaims struct {
	Role     string `json:"role"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

type Manager struct {
	deviceSecret []byte
	adminSecret  []byte
	deviceTTL    time.Duration
	adminTTL     time.Duration
	refreshAfter time.Duration
}

func NewManager(deviceSecret, adminSecret string, deviceTTL, adminTTL, refreshAfter time.Duration) *Manager {
	return &Manager{
		deviceSecret: []byte(deviceSecret),
		adminSecret:  []byte(adminSecret),
		deviceTTL:    deviceTTL,
		adminTTL:     adminTTL,
		refreshAfter: refreshAfter,
	}
}

func (m *Manager) DeviceTTL() time.Duration { return m.deviceTTL }

func (m *Manager) IssueDevice(deviceID uuid.UUID, tokenVersion int, platform string) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(m.deviceTTL)
	claims := DeviceClaims{
		Ver:  tokenVersion,
		Plat: platform,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   deviceID.String(),
			Audience:  jwt.ClaimStrings{audDevice},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-30 * time.Second)),
			ExpiresAt: jwt.NewNumericDate(exp),
			ID:        uuid.Must(uuid.NewV7()).String(),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(m.deviceSecret)
	return signed, exp, err
}

func (m *Manager) ParseDevice(raw string) (*DeviceClaims, error) {
	claims := &DeviceClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		return m.deviceSecret, nil
	}, jwt.WithValidMethods([]string{signingAlgo}), jwt.WithAudience(audDevice),
		jwt.WithIssuer(issuer), jwt.WithLeeway(60*time.Second))
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, fmt.Errorf("%w: %v", ErrTokenInvalid, err)
	}
	return claims, nil
}

// NeedsRefresh 判断 token 是否已进入续期窗口。
func (m *Manager) NeedsRefresh(claims *DeviceClaims) bool {
	if claims.ExpiresAt == nil {
		return false
	}
	return time.Until(claims.ExpiresAt.Time) < m.deviceTTL-m.refreshAfter
}

func (m *Manager) IssueAdmin(adminID uuid.UUID, username, role string) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(m.adminTTL)
	claims := AdminClaims{
		Role:     role,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   adminID.String(),
			Audience:  jwt.ClaimStrings{audAdmin},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-30 * time.Second)),
			ExpiresAt: jwt.NewNumericDate(exp),
			ID:        uuid.Must(uuid.NewV7()).String(),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(m.adminSecret)
	return signed, exp, err
}

func (m *Manager) ParseAdmin(raw string) (*AdminClaims, error) {
	claims := &AdminClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		return m.adminSecret, nil
	}, jwt.WithValidMethods([]string{signingAlgo}), jwt.WithAudience(audAdmin),
		jwt.WithIssuer(issuer), jwt.WithLeeway(60*time.Second))
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, fmt.Errorf("%w: %v", ErrTokenInvalid, err)
	}
	return claims, nil
}

func (m *Manager) AdminTTL() time.Duration { return m.adminTTL }
