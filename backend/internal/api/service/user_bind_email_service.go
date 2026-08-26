package service

// 已登录用户绑定邮箱：手机号注册的账号 users.email 为 NULL，
// 需要用户自行填写邮箱并通过验证码验证后写入，作为找回账号的凭据。
// 与 BindUserPhone 一致：一个邮箱只能归属一个账号，已绑定的账号不再允许改绑。

import (
	"errors"
	"net/mail"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

const bindEmailScene = "bind_email"

var (
	ErrBindEmailUserNotFound = errors.New("用户不存在")
	ErrBindEmailUserDisabled = errors.New("用户已被禁用")
	ErrBindEmailAlreadyBound = errors.New("当前账号已绑定邮箱")
	ErrBindEmailInvalid      = errors.New("邮箱格式不正确")
	ErrBindEmailTaken        = errors.New("该邮箱已被使用，请更换邮箱")
	ErrBindEmailCodeInvalid  = errors.New("邮箱验证码错误或已过期")
	ErrBindEmailFailed       = errors.New("绑定失败，请稍后再试")
)

// SendBindEmailCode 给待绑定的邮箱发验证码（需鉴权）。
func SendBindEmailCode(userID int64, clientIP, emailRaw, lang string) error {
	email, err := prepareBindEmail(userID, emailRaw)
	if err != nil {
		return err
	}
	return sendEmailCodeWithCooldown(clientIP, email, bindEmailScene, lang)
}

// BindUserEmail 校验验证码后把邮箱写入当前账号。
func BindUserEmail(userID int64, emailRaw, code string) error {
	email, err := prepareBindEmail(userID, emailRaw)
	if err != nil {
		return err
	}
	normalizedCode := strings.TrimSpace(code)
	if normalizedCode == "" {
		return ErrBindEmailCodeInvalid
	}
	if !VerifyEmailCode(email, bindEmailScene, normalizedCode) {
		return ErrBindEmailCodeInvalid
	}

	// where 带上「当前邮箱为空」：并发下不会覆盖已绑定的邮箱。
	result := store.DB.Model(&model.User{}).
		Where("id = ? AND (email IS NULL OR email = ?)", userID, "").
		Update("email", email)
	if result.Error != nil {
		// 发码到落库之间该邮箱可能被别人占用，唯一索引会拦下。
		if taken, checkErr := bindEmailTakenByOther(email, userID); checkErr == nil && taken {
			return ErrBindEmailTaken
		}
		logger.L.Errorf("绑定邮箱写库失败 user=%d: %v", userID, result.Error)
		return ErrBindEmailFailed
	}
	if result.RowsAffected == 0 {
		return ErrBindEmailAlreadyBound
	}
	logger.L.Infof("用户绑定邮箱成功 user=%d", userID)
	return nil
}

// prepareBindEmail 规范化邮箱并校验绑定前置条件：账号可用、尚未绑定、邮箱未被占用。
func prepareBindEmail(userID int64, emailRaw string) (string, error) {
	if userID <= 0 {
		return "", ErrBindEmailUserNotFound
	}
	if store.DB == nil {
		return "", errors.New("数据库未初始化")
	}
	// 与注册路径保持一致：只去空白，不做大小写归一。
	// PostgreSQL 的 varchar 等值比较大小写敏感，uni_users_email 不会把
	// Foo@x.com 和 foo@x.com 判为同一个，这里有意接受该口径：
	// 统一小写要连注册、登录、重置一起改，不夹带在绑定链路里做。
	email := strings.TrimSpace(emailRaw)
	if email == "" {
		return "", ErrBindEmailInvalid
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return "", ErrBindEmailInvalid
	}

	var user model.User
	if err := store.DB.First(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrBindEmailUserNotFound
		}
		return "", err
	}
	if user.Status != model.UserStatusActive {
		return "", ErrBindEmailUserDisabled
	}
	if strings.TrimSpace(user.Email) != "" {
		return "", ErrBindEmailAlreadyBound
	}

	taken, err := bindEmailTakenByOther(email, userID)
	if err != nil {
		return "", err
	}
	if taken {
		return "", ErrBindEmailTaken
	}
	return email, nil
}

func bindEmailTakenByOther(email string, userID int64) (bool, error) {
	var other model.User
	err := store.DB.Where("email = ? AND id <> ?", email, userID).First(&other).Error
	if err == nil {
		return true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return false, err
}

// IsBindEmailBusinessError 供 handler 区分「用户可纠正的错误」与服务端故障。
func IsBindEmailBusinessError(err error) bool {
	switch {
	case errors.Is(err, ErrBindEmailUserNotFound),
		errors.Is(err, ErrBindEmailUserDisabled),
		errors.Is(err, ErrBindEmailAlreadyBound),
		errors.Is(err, ErrBindEmailInvalid),
		errors.Is(err, ErrBindEmailTaken),
		errors.Is(err, ErrBindEmailCodeInvalid),
		errors.Is(err, ErrBindEmailFailed),
		errors.Is(err, ErrEmailCodeSendTooFrequent):
		return true
	}
	return false
}
