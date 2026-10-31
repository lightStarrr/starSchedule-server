package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func newManager() *Manager {
	return NewManager(
		"device-secret-0123456789abcdef0123456789",
		"admin-secret-0123456789abcdef0123456789a",
		30*24*time.Hour, 7*24*time.Hour, 23*24*time.Hour,
	)
}

func TestDeviceTokenRoundTrip(t *testing.T) {
	m := newManager()
	deviceID := uuid.Must(uuid.NewV7())
	token, exp, err := m.IssueDevice(deviceID, 3, "android")
	if err != nil {
		t.Fatal(err)
	}
	if time.Until(exp) < 29*24*time.Hour {
		t.Fatalf("有效期异常: %v", exp)
	}
	claims, err := m.ParseDevice(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != deviceID.String() || claims.Ver != 3 || claims.Plat != "android" {
		t.Fatalf("claims 不匹配: %+v", claims)
	}
	if m.NeedsRefresh(claims) {
		t.Fatal("新签发的 token 不应触发续期")
	}
}

func TestDeviceTokenRejectsWrongAudience(t *testing.T) {
	m := newManager()
	adminToken, _, err := m.IssueAdmin(uuid.Must(uuid.NewV7()), "admin", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ParseDevice(adminToken); err == nil {
		t.Fatal("管理端 token 不能当设备 token 使用")
	}
}

func TestExpiredToken(t *testing.T) {
	m := newManager()
	claims := DeviceClaims{
		Ver: 1,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   uuid.Must(uuid.NewV7()).String(),
			Audience:  jwt.ClaimStrings{audDevice},
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.deviceSecret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ParseDevice(raw); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("期望 ErrTokenExpired，实际 %v", err)
	}
}

func TestNeedsRefreshNearExpiry(t *testing.T) {
	m := newManager()
	claims := &DeviceClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
		},
	}
	if !m.NeedsRefresh(claims) {
		t.Fatal("剩余 1 天应触发续期")
	}
}
