package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/askie/grix/backend/internal/api/service/identity"
)

// CheckReachSMS 用与 SendReachSMS 完全相同的 provider 解析判断该用途的短信当前能不能发。
// 后台预览用它，避免"预览显示可用、一发就 not_configured"。
func CheckReachSMS(region, countryCode, kind string) error {
	sender, err := reachSMSSender(region, countryCode)
	if err != nil {
		return err
	}
	return normalizeReachSMSConfigError(sender.CheckMarketing(kind))
}

func reachSMSSender(region, countryCode string) (identity.MarketingSmsSender, error) {
	// 短信通道由手机号国家码决定，账号 region 只是归属地：region=global 绑 +86 手机也必须走阿里云。
	// 口径与 admin 的"测试发送"一致（identity.RegionForCountry），没有国家码时才回落到账号 region。
	if cc := strings.TrimSpace(countryCode); cc != "" {
		region = identity.RegionForCountry(cc)
	}
	provider, err := identity.Default().GetSms(identityProviderForRegion(region))
	if err != nil {
		return nil, ErrReachSMSNotConfigured
	}
	sender, ok := provider.(identity.MarketingSmsSender)
	if !ok {
		return nil, ErrReachSMSNotConfigured
	}
	return sender, nil
}

// normalizeReachSMSConfigError 把"没配"收敛成 ErrReachSMSNotConfigured，
// 保留原始文案便于定位缺的是哪一项；投递失败原样返回。
func normalizeReachSMSConfigError(err error) error {
	if errors.Is(err, identity.ErrProviderNotConfigured) || errors.Is(err, identity.ErrSmsTemplateNotConfigured) {
		return fmt.Errorf("%w: %v", ErrReachSMSNotConfigured, err)
	}
	return err
}

// SendReachSMS 按用户区域挑选短信 provider 并投递营销/通知文案。
// provider 由 identity.Registry 提供；未实现 MarketingSmsSender 或未配置模板时返回错误，由调用方记录 send log。
func SendReachSMS(ctx context.Context, req ReachSMSRequest) error {
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return fmt.Errorf("reach sms: empty text")
	}
	sender, err := reachSMSSender(req.Region, req.CountryCode)
	if err != nil {
		return err
	}
	return normalizeReachSMSConfigError(sender.SendMarketing(ctx, identity.MarketingSmsRequest{
		PhoneE164:   req.PhoneE164,
		CountryCode: req.CountryCode,
		Text:        text,
		Kind:        req.Kind,
	}))
}
