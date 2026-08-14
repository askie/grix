// 手机号无密码短信登录注册业务层。
// 路由层 -> handler/auth_phone.go -> 本文件。
// 关键设计点：
//   - login-code 接口幂等：未注册自动注册，已注册直接登录（业内 OTP 主链路通用做法）。
//   - 区域路由按手机号 country_code 强制判定，不信客户端 region，防"跨区刷量"。
//   - 老 email 用户不回填 user_identities，避免老路径回归；他们通过 BindUserPhone 主动绑。
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/askie/grix/backend/internal/api/service/identity"
	"github.com/askie/grix/backend/internal/featuregate"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/secretcrypto"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"gorm.io/gorm"
)

var phoneSmsStore = identity.SmsCodeStore{}

// phoneStorageFields 把已标准化的 E.164 手机号拆成加密存储三件套：
// 密文（可解密取回真实号）、末 4 位明文（搜索/展示）、盲索引（唯一约束与精确查号）。
func phoneStorageFields(phone string) (cipher, last4, blind string, err error) {
	cipher, err = secretcrypto.Encrypt(phone)
	if err != nil {
		return "", "", "", err
	}
	return cipher, secretcrypto.Hint(phone), secretcrypto.BlindIndex(phone), nil
}

const (
	phoneRegisterUsernameMaxLen = 100
	phoneRegisterNicknameMaxLen = 50
)

// SendPhoneSmsCode 发送短信验证码入口。
//   - clientIP 仅用于限流 + 审计；
//   - captchaID/captchaValue 在"60s 内第 2 次起强 captcha"时必传（补丁清单第 4 条）。
func SendPhoneSmsCode(clientIP, phoneRaw string, scene identity.SmsSendScene, captchaID, captchaValue, lang string) error {
	phone, err := identity.SanitizePhoneE164(phoneRaw)
	if err != nil {
		return err
	}
	country, err := identity.ParseCountryCode(phone)
	if err != nil {
		return err
	}
	region := identity.RegionForCountry(country)

	smsCfg, err := systemsetting.GetSmsSettings()
	if err != nil {
		return errors.New("短信配置读取失败")
	}
	if err := ensureSmsRegionAllowed(smsCfg, region, country, scene); err != nil {
		return err
	}

	ctx := context.Background()
	// 第二次起强 captcha
	if phoneSmsStore.IsCaptchaRequired(ctx, phone) {
		if !VerifyCaptcha(captchaID, captchaValue) {
			return identity.ErrSmsCaptchaRequired
		}
	}

	// 60s + IP + 日额度
	if err := phoneSmsStore.ReserveCooldown(ctx, phone, clientIP); err != nil {
		return err
	}

	code, err := phoneSmsStore.Generate6Digit()
	if err != nil {
		phoneSmsStore.RollbackCooldown(ctx, phone)
		return errors.New("生成验证码失败")
	}
	if err := phoneSmsStore.StoreCode(ctx, string(scene), phone, code); err != nil {
		phoneSmsStore.RollbackCooldown(ctx, phone)
		return err
	}

	_ = smsCfg // 配置仅用于开关/白名单；具体 provider 实例按 region 直接取
	providerName := identityProviderForRegion(region)
	provider, err := identity.Default().GetSms(providerName)
	if err != nil {
		phoneSmsStore.RollbackCooldown(ctx, phone)
		logger.L.Errorf("sms provider not configured region=%s name=%s", region, providerName)
		return errors.New("短信服务暂不可用")
	}

	sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := provider.Send(sendCtx, identity.SendSmsRequest{
		PhoneE164:   phone,
		CountryCode: country,
		Scene:       scene,
		Code:        code,
		Lang:        lang,
		ClientIP:    clientIP,
	}); err != nil {
		phoneSmsStore.RollbackCooldown(ctx, phone)
		logger.L.Errorf("phone sms send fail phone=%s scene=%s err=%v", identity.PhoneMask(phone), scene, err)
		WriteAuditLog(WriteAuditLogReq{
			EventType: "phone_sms_send_fail",
			ClientIP:  clientIP,
			Detail:    map[string]any{"phone_masked": identity.PhoneMask(phone), "scene": string(scene), "region": region, "err": err.Error()},
		})
		return err
	}

	phoneSmsStore.MarkCaptchaRequired(ctx, phone)
	WriteAuditLog(WriteAuditLogReq{
		EventType: "phone_sms_send",
		ClientIP:  clientIP,
		Detail:    map[string]any{"phone_masked": identity.PhoneMask(phone), "scene": string(scene), "region": region},
	})
	logger.L.Infof("phone sms sent phone=%s scene=%s region=%s", identity.PhoneMask(phone), scene, region)
	return nil
}

// PhoneLoginWithCode login-code 接口幂等：未注册自动注册，已注册直接登录。
func PhoneLoginWithCode(phoneRaw, code, deviceID, platform, language, clientIP string) (*LoginResp, error) {
	phone, err := identity.SanitizePhoneE164(phoneRaw)
	if err != nil {
		return nil, err
	}
	country, err := identity.ParseCountryCode(phone)
	if err != nil {
		return nil, err
	}
	region := identity.RegionForCountry(country)

	// 校验码（统一返同一错误，避免泄漏手机号是否已注册）
	ctx := context.Background()
	if !phoneSmsStore.VerifyCode(ctx, string(identity.SmsSceneLogin), phone, code) {
		return nil, errors.New("验证码错误或已过期")
	}

	smsCfg, err := systemsetting.GetSmsSettings()
	if err != nil {
		return nil, errors.New("短信配置读取失败")
	}

	// 查 user_identities 看号是否已被绑（external_id 存盲索引，按号指纹精确匹配）
	providerName := identityProviderForRegion(region)
	phoneBlind := secretcrypto.BlindIndex(phone)
	var identityRow model.UserIdentity
	err = store.DB.Where("provider = ? AND external_id = ?", providerName, phoneBlind).First(&identityRow).Error
	if err == nil {
		// 命中 → 登录
		var user model.User
		if err := store.DB.First(&user, identityRow.UserID).Error; err != nil {
			return nil, errors.New("用户不存在")
		}
		if user.Status != model.UserStatusActive {
			if user.Status == model.UserStatusBanned {
				return nil, security.ErrUserDisabled
			}
			return nil, errors.New("用户不存在")
		}
		WriteAuditLog(WriteAuditLogReq{
			EventType: "phone_login_code",
			UserID:    &user.ID,
			ClientIP:  clientIP,
			Detail:    map[string]any{"phone_masked": identity.PhoneMask(phone), "region": region},
		})
		return doIssueToken(user, deviceID, platform, language)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrAuthServiceUnavailable
	}

	// 未命中 → 注册（要求该 region 注册开关已开）
	if err := ensureSmsRegionAllowed(smsCfg, region, country, identity.SmsSceneRegister); err != nil {
		return nil, err
	}
	registerEnabled, err := featuregate.IsPublicFeatureEnabled("auth_register")
	if err != nil {
		return nil, err
	}
	if !registerEnabled {
		return nil, errors.New("系统已关闭注册")
	}
	authSettings, err := systemsetting.GetAuthSettings()
	if err != nil {
		return nil, err
	}

	resp, user, err := createUserWithPhone(phone, country, region, deviceID, platform, language, authSettings)
	if err != nil {
		return nil, err
	}
	notifyConfiguredCustomerFriendAdded(user.ID, authSettings.AutoAddCustomerUserID)
	scheduleRegisterWelcomeCompensation(user.ID, authSettings.AutoAddCustomerUserID)
	WriteAuditLog(WriteAuditLogReq{
		EventType: "phone_register",
		UserID:    &user.ID,
		ClientIP:  clientIP,
		Detail:    map[string]any{"phone_masked": identity.PhoneMask(phone), "region": region},
	})
	return resp, nil
}

// BindUserPhone 老 email 用户登录后绑定手机号；该号码不可被任何其他 user 绑过。
func BindUserPhone(userID int64, phoneRaw, code, clientIP string) error {
	if userID <= 0 {
		return errors.New("用户不存在")
	}
	phone, err := identity.SanitizePhoneE164(phoneRaw)
	if err != nil {
		return err
	}
	country, err := identity.ParseCountryCode(phone)
	if err != nil {
		return err
	}
	region := identity.RegionForCountry(country)

	ctx := context.Background()
	if !phoneSmsStore.VerifyCode(ctx, string(identity.SmsSceneBind), phone, code) {
		return errors.New("验证码错误或已过期")
	}

	providerName := identityProviderForRegion(region)
	phoneBlind := secretcrypto.BlindIndex(phone)

	// 该号已被其它 user 绑 → 拒绝（号码 takeover 防御，补丁清单第 10 条）
	var existing model.UserIdentity
	if err := store.DB.Where("provider = ? AND external_id = ?", providerName, phoneBlind).First(&existing).Error; err == nil {
		if existing.UserID == userID {
			return nil // 幂等：当前用户重复绑同号
		}
		return errors.New("该手机号已被使用，请联系客服")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("绑定失败，请稍后再试")
	}

	cipher, last4, blind, err := phoneStorageFields(phone)
	if err != nil {
		return errors.New("绑定失败，请稍后再试")
	}
	now := time.Now().UTC()
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		row := model.UserIdentity{
			ID:          snowflake.GenID(),
			UserID:      userID,
			Provider:    providerName,
			ExternalID:  blind,
			CountryCode: country,
			PrimaryFlag: false,
			VerifiedAt:  &now,
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		// users 写加密三件套；旧明文列 phone_e164 不再写
		return tx.Model(&model.User{}).Where("id = ?", userID).Updates(map[string]any{
			"phone_cipher":  cipher,
			"phone_last4":   last4,
			"phone_blind":   blind,
			"phone_country": country,
		}).Error
	}); err != nil {
		return errors.New("绑定失败，请稍后再试")
	}

	WriteAuditLog(WriteAuditLogReq{
		EventType: "phone_bind",
		UserID:    &userID,
		ClientIP:  clientIP,
		Detail:    map[string]any{"phone_masked": identity.PhoneMask(phone), "region": region},
	})
	return nil
}

func createUserWithPhone(phone, country, region, deviceID, platform, language string, authSettings systemsetting.AuthSettings) (*LoginResp, *model.User, error) {
	cipher, last4, blind, err := phoneStorageFields(phone)
	if err != nil {
		return nil, nil, errors.New("注册失败，请稍后重试")
	}
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		username, nickname, err := buildRegisterIdentityByPhone(phone)
		if err != nil {
			return nil, nil, err
		}
		user := model.User{
			ID:           snowflake.GenID(),
			Username:     username,
			Nickname:     nickname,
			Region:       region,
			PhoneCipher:  cipher,
			PhoneLast4:   last4,
			PhoneBlind:   blind,
			PhoneCountry: country,
			AuthProvider: "phone_sms",
		}
		var resp *LoginResp
		var staleDevices []model.Device
		if err := store.DB.Transaction(func(tx *gorm.DB) error {
			// Omit Email：让该列存 NULL，避免多个空串撞 uni_users_email
			if err := tx.Omit("Email").Create(&user).Error; err != nil {
				return err
			}
			id := model.UserIdentity{
				ID:          snowflake.GenID(),
				UserID:      user.ID,
				Provider:    identityProviderForRegion(region),
				ExternalID:  blind,
				CountryCode: country,
				PrimaryFlag: true,
				VerifiedAt:  &now,
			}
			if err := tx.Create(&id).Error; err != nil {
				return err
			}
			if err := createDefaultUserSettingsTx(tx, user.ID, language); err != nil {
				return err
			}
			if err := addConfiguredCustomerFriendTx(tx, user.ID, authSettings.AutoAddCustomerUserID); err != nil {
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
			if isDuplicatedUsernameErr(err) {
				continue
			}
			if isDuplicatedPhoneErr(err) {
				return nil, nil, errors.New("该手机号已被使用，请联系客服")
			}
			return nil, nil, errors.New("注册失败，请稍后重试")
		}
		cleanStaleDeviceCacheAfterCommit(user.ID, deviceID, staleDevices)
		return resp, &user, nil
	}
	return nil, nil, errors.New("注册失败: 用户名生成冲突，请重试")
}

func buildRegisterIdentityByPhone(phone string) (string, string, error) {
	tail := phone
	if len(phone) > 4 {
		tail = phone[len(phone)-4:]
	}
	usernameBase := "u_" + tail
	username, err := findAvailableUserFieldValue("username", usernameBase, phoneRegisterUsernameMaxLen)
	if err != nil {
		return "", "", err
	}
	nicknameBase := "用户" + tail
	nickname, err := findAvailableUserFieldValue("nickname", nicknameBase, phoneRegisterNicknameMaxLen)
	if err != nil {
		return "", "", err
	}
	return username, nickname, nil
}

func isDuplicatedPhoneErr(err error) bool {
	return isDuplicatedConstraintErr(err, "uq_users_phone_blind", "uq_user_identities_provider_extid", "user_identities")
}

func identityProviderForRegion(region string) string {
	if region == "cn" {
		return model.IdentityProviderPhoneSmsCN
	}
	return model.IdentityProviderPhoneSmsGlobal
}

func ensureSmsRegionAllowed(cfg systemsetting.SmsSettings, region, country string, scene identity.SmsSendScene) error {
	switch scene {
	case identity.SmsSceneRegister:
		if region == "cn" && !cfg.PhoneRegisterEnabledCN {
			return errors.New("系统已关闭注册")
		}
		if region != "cn" && !cfg.PhoneRegisterEnabledGlobal {
			return errors.New("系统已关闭注册")
		}
	case identity.SmsSceneLogin, identity.SmsSceneBind, identity.SmsSceneReset:
		if region == "cn" && !cfg.PhoneLoginEnabledCN {
			return errors.New("手机号登录已关闭")
		}
		if region != "cn" && !cfg.PhoneLoginEnabledGlobal {
			return errors.New("手机号登录已关闭")
		}
	}
	allowed := cfg.AllowedCountryCodesCN
	if region != "cn" {
		allowed = cfg.AllowedCountryCodesGlobal
	}
	if !countryAllowed(allowed, country) {
		return fmt.Errorf("不支持的区号 %s", country)
	}
	return nil
}

func countryAllowed(list []string, country string) bool {
	for _, v := range list {
		if v == "*" {
			return true
		}
		if v == country {
			return true
		}
	}
	return false
}
