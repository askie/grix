package service

import (
	"errors"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const changePasswordEmailCodeScene = "change_password"

var (
	ErrChangePasswordUserNotFound    = errors.New("用户不存在")
	ErrChangePasswordUserDisabled    = errors.New("用户已被禁用")
	ErrChangePasswordUserEmailAbsent = errors.New("用户邮箱不存在")
	ErrChangePasswordCodeRequired    = errors.New("邮箱验证码不能为空")
	ErrChangePasswordCodeInvalid     = errors.New("邮箱验证码错误或已过期")
	ErrChangePasswordNewPasswordMiss = errors.New("新密码不能为空")
	ErrChangePasswordHashFailed      = errors.New("密码加密失败")
)

func SendChangePasswordEmailCode(userID int64, lang string) error {
	user, err := loadChangePasswordUser(userID)
	if err != nil {
		return err
	}

	return sendEmailCodeWithoutCaptcha(user.Email, changePasswordEmailCodeScene, lang)
}

func ChangeOwnPassword(userID int64, newPassword, emailCode, accessJTI string, accessExpiresAt time.Time) error {
	user, err := loadChangePasswordUser(userID)
	if err != nil {
		return err
	}

	normalizedEmailCode := strings.TrimSpace(emailCode)
	if normalizedEmailCode == "" {
		return ErrChangePasswordCodeRequired
	}
	if strings.TrimSpace(newPassword) == "" {
		return ErrChangePasswordNewPasswordMiss
	}
	if err := ValidateUserPassword(newPassword); err != nil {
		return err
	}
	if !VerifyEmailCode(user.Email, changePasswordEmailCodeScene, normalizedEmailCode) {
		return ErrChangePasswordCodeInvalid
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return ErrChangePasswordHashFailed
	}

	if err := store.DB.Model(&model.User{}).
		Where("id = ?", user.ID).
		Update("password_hash", string(hash)).
		Error; err != nil {
		return err
	}

	changedAt := time.Now().UTC()
	if err := security.MarkUserPasswordChanged(user.ID, changedAt); err != nil {
		return err
	}

	if err := LogoutWithToken(user.ID, "", "", accessJTI, accessExpiresAt); err != nil {
		return err
	}
	return nil
}

func loadChangePasswordUser(userID int64) (*model.User, error) {
	if userID <= 0 {
		return nil, ErrChangePasswordUserNotFound
	}
	if store.DB == nil {
		return nil, errors.New("数据库未初始化")
	}

	var user model.User
	if err := store.DB.First(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrChangePasswordUserNotFound
		}
		return nil, err
	}
	if user.Status != model.UserStatusActive {
		return nil, ErrChangePasswordUserDisabled
	}
	if strings.TrimSpace(user.Email) == "" {
		return nil, ErrChangePasswordUserEmailAbsent
	}

	return &user, nil
}
