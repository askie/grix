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

// appleRelayEmailSuffix 是 Apple「隐藏我的邮箱」的中转地址后缀。
// 这类地址只在 Apple 继续转发时可达，用户一旦关掉转发就收不到验证码和找回邮件，
// 所以按「还没有可用邮箱」处理，同样引导补一个常用邮箱。
const appleRelayEmailSuffix = "@privaterelay.appleid.com"

var (
	ErrBindEmailUserNotFound = errors.New("用户不存在")
	ErrBindEmailUserDisabled = errors.New("用户已被禁用")
	ErrBindEmailAlreadyBound = errors.New("当前账号已绑定邮箱")
	ErrBindEmailRelayTarget  = errors.New("请填写常用邮箱，不能使用 Apple 隐藏邮箱")
	ErrBindEmailInvalid      = errors.New("邮箱格式不正确")
	ErrBindEmailTaken        = errors.New("该邮箱已属于另一个账号，请直接用它登录")
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

	// where 复述前置校验：并发下不会覆盖一个已经绑好的常用邮箱。
	result := store.DB.Model(&model.User{}).
		Where(
			"id = ? AND (email IS NULL OR email = ? OR LOWER(email) LIKE ?)",
			userID, "", "%"+appleRelayEmailSuffix,
		).
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
	// PostgreSQL 的 varchar 等值比较大小写敏感，uni_users_email 不折叠大小写。
	// 绑定链路统一存小写，判重也按小写比：绑定的邮箱要能被 Google/Apple 登录
	// 按邮箱认领到同一个账号，大小写差异会让认领落空、分裂出第二个账号。
	// （注册路径仍存原样，统一那条链路是另一件事。）
	email := strings.ToLower(strings.TrimSpace(emailRaw))
	if email == "" {
		return "", ErrBindEmailInvalid
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return "", ErrBindEmailInvalid
	}
	if isAppleRelayEmail(email) {
		return "", ErrBindEmailRelayTarget
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
	if !EmailNeedsBinding(user.Email) {
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

// EmailNeedsBinding 判断账号是否还缺一个可用邮箱：没有邮箱，或用的是 Apple 中转地址。
func EmailNeedsBinding(email string) bool {
	normalized := strings.ToLower(strings.TrimSpace(email))
	return normalized == "" || isAppleRelayEmail(normalized)
}

func isAppleRelayEmail(normalizedEmail string) bool {
	return strings.HasSuffix(normalizedEmail, appleRelayEmailSuffix)
}

func bindEmailTakenByOther(email string, userID int64) (bool, error) {
	var other model.User
	err := store.DB.Where("LOWER(email) = ? AND id <> ?", email, userID).First(&other).Error
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
		errors.Is(err, ErrBindEmailRelayTarget),
		errors.Is(err, ErrBindEmailInvalid),
		errors.Is(err, ErrBindEmailTaken),
		errors.Is(err, ErrBindEmailCodeInvalid),
		errors.Is(err, ErrBindEmailFailed),
		errors.Is(err, ErrEmailCodeSendTooFrequent):
		return true
	}
	return false
}
