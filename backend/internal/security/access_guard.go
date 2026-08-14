package security

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

var (
	ErrUserDisabled  = errors.New("用户已被禁用")
	ErrUserNotFound  = errors.New("用户不存在")
	ErrDBUnavailable = errors.New("数据库未初始化")
)

func EnsureUserActive(userID int64) error {
	return EnsureUserActiveWithDB(store.DB, userID)
}

func EnsureUserActiveWithDB(db *gorm.DB, userID int64) error {
	if db == nil {
		return ErrDBUnavailable
	}

	var user model.User
	if err := db.Select("id", "status").First(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrUserNotFound
		}
		return err
	}
	if user.Status != model.UserStatusActive {
		return ErrUserDisabled
	}
	return nil
}

func IsAccessTokenRevoked(jti string) bool {
	if store.RDB == nil {
		return false
	}
	tokenID := strings.TrimSpace(jti)
	if tokenID == "" {
		return false
	}
	revoked, err := store.RDB.Exists(context.Background(), revokedAccessTokenKey(tokenID)).Result()
	return err == nil && revoked > 0
}

func revokedAccessTokenKey(jti string) string {
	return fmt.Sprintf("auth:revoked:access:%s", jti)
}

func MarkUserPasswordChanged(userID int64, changedAt time.Time) error {
	if store.RDB == nil || userID <= 0 {
		return nil
	}
	ts := changedAt.UTC().Unix()
	return store.RDB.Set(context.Background(), userPasswordChangedAtKey(userID), ts, 90*24*time.Hour).Err()
}

func IsAccessTokenInvalidByPasswordChange(userID int64, issuedAt time.Time) bool {
	if store.RDB == nil || userID <= 0 || issuedAt.IsZero() {
		return false
	}
	changedAtUnix, err := store.RDB.Get(context.Background(), userPasswordChangedAtKey(userID)).Int64()
	if err != nil {
		return false
	}
	return issuedAt.UTC().Unix() < changedAtUnix
}

func userPasswordChangedAtKey(userID int64) string {
	return fmt.Sprintf("auth:user:password_changed_at:%d", userID)
}
