package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const dummyAdminPasswordHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZRGdjGj/n3.eG1H0B/XQsI3a3hOyC"

var (
	ErrInvalidCredentials  = errors.New("管理员账号或密码错误")
	ErrAdminDisabled       = errors.New("管理员账号已被禁用")
	ErrAdminSessionInvalid = errors.New("管理员会话无效")
)

const sessionTTL = 12 * time.Hour

func Login(username, password, clientIP, userAgent string) (string, *model.AdminUser, error) {
	ctx := context.Background()
	var admin model.AdminUser
	trimmedUsername := strings.TrimSpace(username)
	passwordBytes := []byte(password)
	if err := store.DB.Where("username = ?", trimmedUsername).First(&admin).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil, errors.New("登录服务暂时不可用，请稍后再试")
		}

		usernameGuard := security.NewAdminLoginGuardByUsername(trimmedUsername)
		locked, remaining, guardErr := usernameGuard.IsLocked(ctx)
		if guardErr != nil {
			return "", nil, errors.New("登录服务暂时不可用，请稍后再试")
		}
		if locked {
			return "", nil, errors.New(security.LoginLockedMessage(remaining))
		}

		_ = bcrypt.CompareHashAndPassword([]byte(dummyAdminPasswordHash), passwordBytes)
		_, lockedNow, lockDuration, guardErr := usernameGuard.RecordFailure(ctx)
		if guardErr != nil {
			return "", nil, errors.New("登录服务暂时不可用，请稍后再试")
		}
		if lockedNow {
			return "", nil, errors.New(security.LoginLockedMessage(lockDuration))
		}
		return "", nil, ErrInvalidCredentials
	}
	if admin.Status != model.AdminStatusActive {
		_ = bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), passwordBytes)
		return "", nil, ErrInvalidCredentials
	}

	adminGuard := security.NewAdminLoginGuardByAdminID(admin.ID)
	locked, remaining, err := adminGuard.IsLocked(ctx)
	if err != nil {
		return "", nil, errors.New("登录服务暂时不可用，请稍后再试")
	}
	if locked {
		return "", nil, errors.New(security.LoginLockedMessage(remaining))
	}

	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), passwordBytes); err != nil {
		_, lockedNow, lockDuration, guardErr := adminGuard.RecordFailure(ctx)
		if guardErr != nil {
			return "", nil, errors.New("登录服务暂时不可用，请稍后再试")
		}
		if lockedNow {
			return "", nil, errors.New(security.LoginLockedMessage(lockDuration))
		}
		return "", nil, ErrInvalidCredentials
	}
	if err := adminGuard.RecordSuccess(ctx); err != nil {
		return "", nil, errors.New("登录服务暂时不可用，请稍后再试")
	}

	sessionID, err := generateSessionToken()
	if err != nil {
		return "", nil, err
	}
	now := time.Now().UTC()
	session := model.AdminSession{
		SessionID:  hashToken(sessionID),
		AdminID:    admin.ID,
		ExpiresAt:  now.Add(sessionTTL),
		ClientIP:   clientIP,
		UserAgent:  truncate(userAgent, 255),
		LastSeenAt: now,
	}

	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&session).Error; err != nil {
			return err
		}
		return tx.Model(&model.AdminUser{}).
			Where("id = ?", admin.ID).
			Updates(map[string]any{
				"last_login_at": now,
				"updated_at":    now,
			}).Error
	}); err != nil {
		return "", nil, err
	}

	admin.LastLoginAt = &now
	return sessionID, &admin, nil
}

func LoadAdminBySession(sessionID string) (*model.AdminSession, *model.AdminUser, error) {
	token := strings.TrimSpace(sessionID)
	if token == "" {
		return nil, nil, ErrAdminSessionInvalid
	}

	var session model.AdminSession
	if err := store.DB.Where("session_id = ?", hashToken(token)).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrAdminSessionInvalid
		}
		return nil, nil, err
	}
	if session.RevokedAt != nil || !session.ExpiresAt.After(time.Now().UTC()) {
		return nil, nil, ErrAdminSessionInvalid
	}

	var admin model.AdminUser
	if err := store.DB.First(&admin, session.AdminID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrAdminSessionInvalid
		}
		return nil, nil, err
	}
	if admin.Status != model.AdminStatusActive {
		return nil, nil, ErrAdminDisabled
	}

	_ = store.DB.Model(&model.AdminSession{}).
		Where("session_id = ?", session.SessionID).
		Updates(map[string]any{
			"last_seen_at": time.Now().UTC(),
			"updated_at":   time.Now().UTC(),
		}).Error

	return &session, &admin, nil
}

func Logout(sessionID string) error {
	now := time.Now().UTC()
	return store.DB.Model(&model.AdminSession{}).
		Where("session_id = ? AND revoked_at IS NULL", hashToken(strings.TrimSpace(sessionID))).
		Updates(map[string]any{
			"revoked_at": now,
			"updated_at": now,
		}).Error
}

func truncate(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}

func generateSessionToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}
