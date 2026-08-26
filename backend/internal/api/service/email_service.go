package service

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/services/dm"
	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/featuregate"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

const emailVerifyCodePrefix = "auth:email_code:"

// 验证码失败计数：同一邮箱+场景累计失败达上限即作废验证码，阻断对 6 位验证码的暴力枚举。
const (
	emailVerifyAttemptPrefix = "auth:email_code_fail:"
	emailVerifyMaxFailures   = 5
	emailVerifyCodeTTL       = 5 * time.Minute
)

const (
	publicEmailCodeSceneRegister = "register"
	publicEmailCodeSceneReset    = "reset"
)

var sendEmailCodeDispatcher = sendEmailCodeInternal

// SendEmailCode 发送阿里云邮件验证码
func SendEmailCode(clientIP, email, scene, captchaID, captchaValue, lang string) error {
	normalizedScene, err := validatePublicEmailCodeScene(scene)
	if err != nil {
		logger.L.Warnf("邮件验证码 scene 校验失败 ip=%s email=%s scene=%s: %v", clientIP, email, scene, err)
		return err
	}

	if publicEmailCodeSceneRequiresCaptcha(normalizedScene) && !VerifyCaptcha(captchaID, captchaValue) {
		return errors.New("图形验证码错误或已过期")
	}

	return sendEmailCodeWithCooldown(clientIP, email, normalizedScene, lang)
}

// sendEmailCodeWithCooldown 按「IP + 邮箱」冷却发送验证码，供公开与已鉴权的发码入口共用。
// 调用方需先完成 scene 校验；发送失败回滚冷却，成功后提交。
func sendEmailCodeWithCooldown(clientIP, email, scene, lang string) error {
	reservation, err := reserveEmailCodeSendCooldown(clientIP, email)
	if err != nil {
		logger.L.Warnf("邮件验证码发送冷却拒绝 ip=%s email=%s scene=%s: %v", clientIP, email, scene, err)
		return err
	}

	logger.L.Infof("开始发送邮件验证码 ip=%s email=%s scene=%s", clientIP, email, scene)

	if err := sendEmailCodeDispatcher(email, scene, lang); err != nil {
		if rollbackErr := reservation.Rollback(); rollbackErr != nil {
			logger.L.Errorf("回滚邮件验证码发送冷却失败 ip=%s email=%s: %v", clientIP, email, rollbackErr)
		}
		logger.L.Errorf("发送邮件验证码失败 ip=%s email=%s scene=%s: %v", clientIP, email, scene, err)
		return err
	}
	if err := reservation.Commit(); err != nil {
		logger.L.Errorf("提交邮件验证码发送冷却失败 ip=%s email=%s: %v", clientIP, email, err)
	}
	logger.L.Infof("邮件验证码发送成功 ip=%s email=%s scene=%s", clientIP, email, scene)
	return nil
}

func sendEmailCodeWithoutCaptcha(email, scene, lang string) error {
	normalizedScene, err := validateEmailCodeScene(scene)
	if err != nil {
		return err
	}

	return sendEmailCodeDispatcher(email, normalizedScene, lang)
}

func validatePublicEmailCodeScene(scene string) (string, error) {
	normalizedScene := normalizeEmailCodeScene(scene)
	switch normalizedScene {
	case publicEmailCodeSceneRegister, publicEmailCodeSceneReset:
		return validateEmailCodeScene(normalizedScene)
	default:
		return "", errors.New("参数错误")
	}
}

func validateEmailCodeScene(scene string) (string, error) {
	normalizedScene := normalizeEmailCodeScene(scene)
	switch normalizedScene {
	case publicEmailCodeSceneRegister:
		enabled, err := featuregate.IsPublicFeatureEnabled("auth_register")
		if err != nil {
			return "", err
		}
		if !enabled {
			return "", errors.New("系统已关闭注册")
		}
	case publicEmailCodeSceneReset, changePasswordEmailCodeScene, bindEmailScene:
		// always allowed
	default:
		return "", errors.New("参数错误")
	}
	return normalizedScene, nil
}

func publicEmailCodeSceneRequiresCaptcha(scene string) bool {
	return normalizeEmailCodeScene(scene) != publicEmailCodeSceneRegister
}

func normalizeEmailCodeScene(scene string) string {
	return strings.ToLower(strings.TrimSpace(scene))
}

func sendEmailCodeInternal(email, scene, lang string) error {
	code, err := generateEmailCode()
	if err != nil {
		logger.L.Errorf("生成邮件验证码失败 email=%s scene=%s: %v", email, scene, err)
		return errors.New("生成验证码失败")
	}

	if err := validateAliEmailConfig(); err != nil {
		logger.L.Errorf("邮件配置缺失 email=%s scene=%s: %v", email, scene, err)
		return err
	}

	client, err := dm.NewClientWithAccessKey(config.C.AliEmail.RegionID, config.C.AliEmail.AccessKeyID, config.C.AliEmail.AccessKeySecret)
	if err != nil {
		logger.L.Errorf("初始化邮件客户端失败 email=%s scene=%s: %v", email, scene, err)
		return errors.New("初始化邮件客户端失败")
	}

	request := dm.CreateSingleSendMailRequest()
	request.Scheme = "https"
	request.AccountName = config.C.AliEmail.FromAddress
	request.AddressType = "1"
	request.ReplyToAddress = "false"
	request.ToAddress = email
	request.Subject, request.HtmlBody = verificationEmailContent(code, lang)

	_, err = client.SingleSendMail(request)
	if err != nil {
		logger.L.Errorf("阿里云邮件发送失败 email=%s scene=%s from=%s: %v", email, scene, config.C.AliEmail.FromAddress, err)
		return errors.New("邮件发送失败")
	}

	return storeEmailCode(email, scene, code)
}

func verificationEmailContent(code, lang string) (subject, body string) {
	if normalizeEmailLanguage(lang) == "en" {
		return "Your verification code", fmt.Sprintf("Hello, your verification code is: <b>%s</b>. It is valid for 5 minutes. If this was not you, please ignore this email.", code)
	}
	return "您的验证码", fmt.Sprintf("您好，您的验证码是：<b>%s</b>，有效期为5分钟。如果非本人操作，请忽略此邮件。", code)
}

func validateAliEmailConfig() error {
	if strings.TrimSpace(config.C.AliEmail.AccessKeyID) == "" {
		return errors.New("邮件服务未配置 access key id")
	}
	if strings.TrimSpace(config.C.AliEmail.AccessKeySecret) == "" {
		return errors.New("邮件服务未配置 access key secret")
	}
	if strings.TrimSpace(config.C.AliEmail.FromAddress) == "" {
		return errors.New("邮件服务未配置 from address")
	}
	return nil
}

func normalizeEmailLanguage(raw string) string {
	lang := strings.ToLower(strings.TrimSpace(raw))
	if strings.Contains(lang, "en") {
		return "en"
	}
	return "zh"
}

// emailCodeKey 生成验证码/失败计数的 Redis key。
// 邮箱统一去空白并转小写，与发码冷却 key 和各处 LOWER(email) 查找同一口径：
// 发码与验码可能来自不同入口，同一邮箱的不同写法不该算成两把 key。
func emailCodeKey(prefix, scene, email string) string {
	return fmt.Sprintf("%s%s:%s", prefix, scene, strings.ToLower(strings.TrimSpace(email)))
}

func storeEmailCode(email, scene, code string) error {
	key := emailCodeKey(emailVerifyCodePrefix, scene, email)
	if store.RDB == nil {
		logger.L.Warn("Redis 未初始化，跳过存储验证码")
		return nil // 如果 Redis未准备好，这里假设不阻断（真实环境应当阻断或使用内存Fallback）
	}
	return store.RDB.Set(emailCodeSendContext(), key, code, 5*time.Minute).Err()
}

// VerifyEmailCode 验证邮箱验证码：成功即作废；失败累计达上限即作废验证码，
// 迫使攻击者重新发码（受发送侧冷却与 IP 限流约束），阻断暴力枚举。
func VerifyEmailCode(email, scene, expectedCode string) bool {
	if store.RDB == nil {
		return false
	}
	ctx := emailCodeSendContext()
	key := emailCodeKey(emailVerifyCodePrefix, scene, email)
	val, err := store.RDB.Get(ctx, key).Result()
	if err != nil {
		return false
	}
	attemptKey := emailCodeKey(emailVerifyAttemptPrefix, scene, email)
	if subtle.ConstantTimeCompare([]byte(val), []byte(expectedCode)) == 1 {
		store.RDB.Del(ctx, key, attemptKey) // 验证通过即作废验证码与失败计数
		return true
	}
	if attempts, aerr := store.RDB.Incr(ctx, attemptKey).Result(); aerr == nil {
		if attempts == 1 {
			store.RDB.Expire(ctx, attemptKey, emailVerifyCodeTTL)
		}
		if attempts >= emailVerifyMaxFailures {
			store.RDB.Del(ctx, key, attemptKey) // 失败封顶：作废当前验证码
		}
	}
	return false
}

func generateEmailCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}
