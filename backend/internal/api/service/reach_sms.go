package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/askie/grix/backend/internal/api/service/identity"
)

// SendReachSMS 按用户区域挑选短信 provider 并投递营销/通知文案。
// provider 由 identity.Registry 提供；未实现 MarketingSmsSender 或未配置模板时返回错误，由调用方记录 send log。
func SendReachSMS(ctx context.Context, req ReachSMSRequest) error {
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return fmt.Errorf("reach sms: empty text")
	}
	region := req.Region
	if region == "" && req.CountryCode == "+86" {
		region = "cn"
	}
	provider, err := identity.Default().GetSms(identityProviderForRegion(region))
	if err != nil {
		return ErrReachSMSNotConfigured
	}
	sender, ok := provider.(identity.MarketingSmsSender)
	if !ok {
		return ErrReachSMSNotConfigured
	}
	return sender.SendMarketing(ctx, identity.MarketingSmsRequest{
		PhoneE164:   req.PhoneE164,
		CountryCode: req.CountryCode,
		Text:        text,
	})
}
