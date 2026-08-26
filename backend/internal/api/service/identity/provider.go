// Package identity 提供身份提供商抽象：把"短信+手机号"、"邮件+邮箱"、"Apple OAuth" 等
// 视为统一的 IdentityProvider。一期只接入 phone_sms_cn / phone_sms_global 两个 provider，
// 老的 email/apple/google 接口暂不迁入（避免老用户回归）。
package identity

import (
	"context"
	"errors"
)

// SmsSendScene 标识发短信的业务场景，用于选择阿里模板号或拼装 SNS 文案。
type SmsSendScene string

const (
	SmsSceneRegister SmsSendScene = "register" // 注册（实际 login-code 接口幂等：未注册则注册）
	SmsSceneLogin    SmsSendScene = "login"
	SmsSceneReset    SmsSendScene = "reset"
	SmsSceneBind     SmsSendScene = "bind"
)

// SendSmsRequest 发送短信入参。
type SendSmsRequest struct {
	PhoneE164   string // 标准化后的手机号
	CountryCode string // 如 +86 / +1
	Scene       SmsSendScene
	Code        string // 6 位明文码
	Lang        string // zh / en（影响 SNS 文案；阿里模板用模板号选择）
	ClientIP    string // 仅用于日志/审计
}

// SmsProvider 是发短信能力的最小抽象。阿里短信、AWS SNS 各自实现一份；
// 业务侧统一通过 Registry 取实例，不耦合具体 SDK。
type SmsProvider interface {
	// Name 返回 provider 唯一名，与 model.IdentityProvider* 常量一致。
	Name() string
	// Send 发送一条短信验证码。同步等待 SDK 返回（5s 超时由 provider 自己保证）。
	Send(ctx context.Context, req SendSmsRequest) error
	// HealthCheck 启动期/塘主"测试发送"按钮调用，确认 ak/sk + 模板号配置正确。
	HealthCheck(ctx context.Context) error
}

// 文案类短信的用途分类。阿里云要求营销类与通知类分别报备模板，Kind 决定选哪个模板号；
// AWS SNS 直发文案，不区分。
const (
	SmsTextKindMarketing = "marketing"
	SmsTextKindNotify    = "notify"
)

// MarketingSmsRequest 营销/通知短信入参：文案由触达模板提供，provider 负责按各自规则投递。
type MarketingSmsRequest struct {
	PhoneE164   string
	CountryCode string
	Text        string
	// Kind 取 SmsTextKind* 之一；留空按营销类处理（历史调用方行为）。
	Kind string
}

// MarketingSmsSender 营销/通知类短信能力；与验证码 Send 分开，避免验证码模板被营销文案误用。
// 阿里云需要单独报备的营销模板号（变量 content），AWS SNS 以 Promotional 类型直发文案。
type MarketingSmsSender interface {
	SendMarketing(ctx context.Context, req MarketingSmsRequest) error
	// CheckMarketing 判断该用途的文案短信当前是否可发（ak/sk、签名、模板号齐备），不真发。
	// SendMarketing 自己也走这套判定，保证"预览说能发"与"实发"不会分叉。
	CheckMarketing(kind string) error
}

// IdentityProvider 是更高层的统一身份提供商抽象。
// 一期 SMS 类 provider 同时实现 SmsProvider 与 IdentityProvider；
// 二期 email / apple / google 也走同一抽象。
type IdentityProvider interface {
	Name() string
}

var (
	// ErrProviderDisabled 表示该 provider 在塘主后台被关闭。
	ErrProviderDisabled = errors.New("身份提供商已关闭")
	// ErrProviderNotConfigured 表示 ak/sk 等关键配置缺失。
	ErrProviderNotConfigured = errors.New("身份提供商未配置")
	// ErrSmsTemplateNotConfigured 表示 ak/sk 齐全但该场景的短信模板号还没填。
	ErrSmsTemplateNotConfigured = errors.New("短信模板号未配置")
)
