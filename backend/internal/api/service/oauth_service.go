package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/featuregate"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"google.golang.org/api/idtoken"
	"gorm.io/gorm"
)

type googleTokenProfile struct {
	Email         string
	Name          string
	Subject       string
	EmailVerified bool
}

var (
	errGoogleLoginNotConfigured = errors.New("Google 登录服务暂未配置")
	errGoogleLoginVerifyFailed  = errors.New("Google 登录验证失败")
	errGoogleEmailUnavailable   = errors.New("无法从 Google 获取邮箱")
	errGoogleEmailUnverified    = errors.New("Google 邮箱未验证")
)

var validateGoogleIDToken = defaultValidateGoogleIDToken

// LoginWithGoogle 验证 Google ID Token，若绑定则登录，否则自动注册并绑定
func LoginWithGoogle(idToken, deviceID, platform, language string) (*LoginResp, error) {
	googleEnabled, err := featuregate.IsPublicFeatureEnabled("auth_google_login")
	if err != nil {
		return nil, err
	}
	if !googleEnabled {
		return nil, errors.New("系统已关闭 Google 登录")
	}

	profile, err := validateGoogleIDToken(
		context.Background(),
		idToken,
		configuredGoogleAllowedClientIDs(),
	)
	if err != nil {
		return nil, err
	}
	if !profile.EmailVerified {
		return nil, errGoogleEmailUnverified
	}

	email := profile.Email
	name := profile.Name
	subject := profile.Subject

	var oauthAccount model.OAuthAccount
	err = store.DB.Where("provider = ? AND provider_uid = ?", "google", subject).First(&oauthAccount).Error
	if err == nil {
		// 已绑定过，直接走登录
		return loginWithOAuthAccount(&oauthAccount, deviceID, platform, language)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("Google 登录服务暂时不可用，请稍后再试")
	}

	// 尚未绑定，检查邮箱是否已存在
	var user model.User
	err = findUserByEmailFold(email, &user)
	if err == nil {
		if err := security.EnsureUserActive(user.ID); err != nil {
			if errors.Is(err, security.ErrUserDisabled) {
				return nil, err
			}
			if errors.Is(err, security.ErrUserNotFound) {
				return nil, errors.New("对应用户不存在")
			}
			return nil, ErrAuthServiceUnavailable
		}
		// 邮箱存在，自动绑定此 Google 账号
		newAccount := model.OAuthAccount{
			ID:          snowflake.GenID(),
			UserID:      user.ID,
			Provider:    "google",
			ProviderUID: subject,
		}
		if err := store.DB.Create(&newAccount).Error; err != nil {
			if isDuplicatedOAuthProviderUIDErr(err) {
				return loginWithGoogleProviderUID(subject, deviceID, platform, language)
			}
			return nil, errors.New("绑定 Google 账号失败")
		}
		return doIssueToken(user, deviceID, platform, language)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("Google 登录服务暂时不可用，请稍后再试")
	}

	// 邮箱也不存在，自动创建新用户并绑定
	registerEnabled, err := featuregate.IsPublicFeatureEnabled("auth_register")
	if err != nil {
		return nil, err
	}
	if !registerEnabled {
		return nil, errors.New("系统已关闭注册")
	}
	settings, err := systemsetting.GetAuthSettings()
	if err != nil {
		return nil, err
	}
	idStr := strconv.FormatInt(snowflake.GenID(), 10)
	importName := fmt.Sprintf("u_%s", idStr[:8])
	user = model.User{
		ID:           snowflake.GenID(),
		Email:        email,
		Username:     importName,
		PasswordHash: "", // 第三方登录没有本地密码
		AuthProvider: "google",
		Nickname:     name,
		Status:       model.UserStatusActive,
	}

	newAccount := model.OAuthAccount{
		ID:          snowflake.GenID(),
		UserID:      user.ID,
		Provider:    "google",
		ProviderUID: subject,
	}

	var resp *LoginResp
	var staleDevices []model.Device
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		if err := createDefaultUserSettingsTx(tx, user.ID, language); err != nil {
			return err
		}
		if err := tx.Create(&newAccount).Error; err != nil {
			return err
		}
		if err := addConfiguredCustomerFriendTx(tx, user.ID, settings.AutoAddCustomerUserID); err != nil {
			return err
		}

		issued, stale, err := issueTokenWithDB(tx, user, deviceID, platform)
		if err != nil {
			return err
		}
		resp = issued
		staleDevices = stale
		return nil
	}); err != nil {
		if isDuplicatedOAuthProviderUIDErr(err) {
			return loginWithGoogleProviderUID(subject, deviceID, platform, language)
		}
		return nil, normalizeGoogleAutoRegisterErr(err)
	}

	cleanStaleDeviceCacheAfterCommit(user.ID, deviceID, staleDevices)
	notifyConfiguredCustomerFriendAdded(user.ID, settings.AutoAddCustomerUserID)
	scheduleRegisterWelcomeCompensation(user.ID, settings.AutoAddCustomerUserID)
	return resp, nil
}

func loginWithOAuthAccount(account *model.OAuthAccount, deviceID, platform, language string) (*LoginResp, error) {
	if account == nil {
		return nil, errors.New("对应用户不存在")
	}

	var user model.User
	if err := store.DB.First(&user, account.UserID).Error; err != nil {
		return nil, errors.New("对应用户不存在")
	}
	if err := security.EnsureUserActive(user.ID); err != nil {
		if errors.Is(err, security.ErrUserDisabled) {
			return nil, err
		}
		if errors.Is(err, security.ErrUserNotFound) {
			return nil, errors.New("对应用户不存在")
		}
		return nil, ErrAuthServiceUnavailable
	}
	return doIssueToken(user, deviceID, platform, language)
}

func loginWithGoogleProviderUID(providerUID, deviceID, platform, language string) (*LoginResp, error) {
	var account model.OAuthAccount
	if err := store.DB.Where("provider = ? AND provider_uid = ?", "google", providerUID).First(&account).Error; err != nil {
		return nil, errors.New("绑定 Google 账号失败")
	}
	return loginWithOAuthAccount(&account, deviceID, platform, language)
}

func isDuplicatedOAuthProviderUIDErr(err error) bool {
	return isDuplicatedConstraintErr(
		err,
		"idx_oauth_accounts_provider_uid_unique",
		"oauth_accounts.provider",
		"oauth_accounts.provider_uid",
	)
}

func normalizeGoogleAutoRegisterErr(err error) error {
	if err == nil {
		return nil
	}
	if isDuplicatedEmailErr(err) {
		return errors.New("该邮箱已被注册")
	}
	if errors.Is(err, ErrConfiguredCustomerInvalid) ||
		errors.Is(err, ErrConfiguredCustomerNotFound) ||
		errors.Is(err, ErrConfiguredCustomerDisabled) {
		return err
	}
	switch err.Error() {
	default:
		return errors.New("Google 登录服务暂时不可用，请稍后再试")
	}
}

func defaultValidateGoogleIDToken(
	ctx context.Context,
	idToken string,
	allowedAudiences []string,
) (googleTokenProfile, error) {
	if strings.TrimSpace(idToken) == "" {
		return googleTokenProfile{}, errGoogleLoginVerifyFailed
	}
	if len(allowedAudiences) == 0 {
		return googleTokenProfile{}, errGoogleLoginNotConfigured
	}

	var lastErr error
	for _, audience := range allowedAudiences {
		payload, err := idtoken.Validate(ctx, idToken, audience)
		if err != nil {
			lastErr = err
			continue
		}

		profile, err := googleTokenProfileFromPayload(payload)
		if err != nil {
			return googleTokenProfile{}, err
		}
		return profile, nil
	}

	if lastErr != nil {
		return googleTokenProfile{}, errGoogleLoginVerifyFailed
	}
	return googleTokenProfile{}, errGoogleLoginVerifyFailed
}

func configuredGoogleAllowedClientIDs() []string {
	raw := strings.TrimSpace(config.C.OAuth.GoogleAllowedClientIDs)
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		normalized := strings.TrimSpace(part)
		if normalized == "" {
			continue
		}
		result = append(result, normalized)
	}
	return result
}

func googleTokenProfileFromPayload(payload *idtoken.Payload) (googleTokenProfile, error) {
	if payload == nil {
		return googleTokenProfile{}, errGoogleLoginVerifyFailed
	}

	subject := strings.TrimSpace(payload.Subject)
	if subject == "" {
		return googleTokenProfile{}, errGoogleLoginVerifyFailed
	}

	email, ok := claimString(payload.Claims["email"])
	if !ok || email == "" {
		return googleTokenProfile{}, errGoogleEmailUnavailable
	}

	name, _ := claimString(payload.Claims["name"])
	if name == "" {
		name = email
	}

	return googleTokenProfile{
		Email:         email,
		Name:          name,
		Subject:       subject,
		EmailVerified: claimBool(payload.Claims["email_verified"]),
	}, nil
}

func claimString(value any) (string, bool) {
	raw, ok := value.(string)
	if !ok {
		return "", false
	}
	normalized := strings.TrimSpace(raw)
	if normalized == "" {
		return "", false
	}
	return normalized, true
}

func claimBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

// findUserByEmailFold 按邮箱忽略大小写查用户，用于第三方登录认领已有账号：
// 用户在别处绑的邮箱可能是 Foo@x.com，精确匹配会漏掉它，进而给同一个人再建一个账号。
// 历史脏数据可能让同一邮箱存在多个大小写变体的账号，这里按主键取最老的一个并留痕告警。
func findUserByEmailFold(email string, out *model.User) error {
	normalized := strings.ToLower(strings.TrimSpace(email))
	var matched []model.User
	if err := store.DB.Where("LOWER(email) = ?", normalized).
		Order("id").Limit(2).Find(&matched).Error; err != nil {
		return err
	}
	if len(matched) == 0 {
		return gorm.ErrRecordNotFound
	}
	if len(matched) > 1 {
		logger.L.Warnf(
			"同一邮箱存在多个大小写变体账号，按最老账号认领 email=%s user=%d",
			normalized, matched[0].ID,
		)
	}
	*out = matched[0]
	return nil
}
