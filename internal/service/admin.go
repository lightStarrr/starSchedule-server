package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/starschedule/server/internal/auth"
	"github.com/starschedule/server/internal/model"
	"github.com/starschedule/server/internal/store/postgres"
)

type AdminService struct {
	store *postgres.Store
	jwt   *auth.Manager
}

func NewAdminService(st *postgres.Store, jwt *auth.Manager) *AdminService {
	return &AdminService{store: st, jwt: jwt}
}

type LoginResult struct {
	Token     string
	ExpiresAt time.Time
	Admin     *model.AdminUser
}

func (s *AdminService) Login(ctx context.Context, username, password string) (*LoginResult, error) {
	admin, err := s.store.GetAdminByUsername(ctx, username)
	if err != nil {
		// 统一错误，避免暴露用户名是否存在。
		return nil, ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}
	token, exp, err := s.jwt.IssueAdmin(admin.ID, admin.Username, admin.Role)
	if err != nil {
		return nil, err
	}
	_ = s.store.TouchAdminLogin(ctx, admin.ID)
	return &LoginResult{Token: token, ExpiresAt: exp, Admin: admin}, nil
}

// SeedAdmin 确保存在初始管理员（已存在则跳过，不覆盖密码）。
func (s *AdminService) SeedAdmin(ctx context.Context, username, password string) (bool, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return false, err
	}
	return s.store.SeedAdmin(ctx, username, string(hash))
}

func (s *AdminService) Audit(ctx context.Context, admin *model.AdminUser, action, targetType, targetID string,
	detail map[string]any, ip string) {
	if admin == nil {
		return
	}
	var adminID *uuid.UUID
	if admin.ID != uuid.Nil {
		id := admin.ID
		adminID = &id
	}
	_ = s.store.InsertAudit(ctx, model.AuditLog{
		ID:            uuid.Must(uuid.NewV7()),
		AdminID:       adminID,
		AdminUsername: admin.Username,
		Action:        action,
		TargetType:    targetType,
		TargetID:      targetID,
		Detail:        detail,
		IP:            ip,
	})
}
