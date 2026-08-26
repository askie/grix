package service

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/api/service/identity"
	"github.com/askie/grix/backend/internal/model"
)

// fakeReachSMSProvider 同时实现 SmsProvider 与 MarketingSmsSender，只记录自己被选中。
type fakeReachSMSProvider struct {
	name string
}

func (p *fakeReachSMSProvider) Name() string { return p.name }

func (p *fakeReachSMSProvider) Send(context.Context, identity.SendSmsRequest) error { return nil }

func (p *fakeReachSMSProvider) HealthCheck(context.Context) error { return nil }

func (p *fakeReachSMSProvider) SendMarketing(context.Context, identity.MarketingSmsRequest) error {
	return nil
}

func (p *fakeReachSMSProvider) CheckMarketing(string) error { return nil }

func registerFakeReachSMSProviders(t *testing.T) {
	t.Helper()
	reg := identity.Default()
	reg.SetSms(&fakeReachSMSProvider{name: model.IdentityProviderPhoneSmsCN})
	reg.SetSms(&fakeReachSMSProvider{name: model.IdentityProviderPhoneSmsGlobal})
	t.Cleanup(func() {
		reg.Remove(model.IdentityProviderPhoneSmsCN)
		reg.Remove(model.IdentityProviderPhoneSmsGlobal)
	})
}

// 短信通道必须按手机号国家码选，不能按账号 region：
// region=global 的账号绑 +86 手机时走 AWS SNS 会直接发不出去。
func TestReachSMSSenderPicksProviderByCountryCode(t *testing.T) {
	registerFakeReachSMSProviders(t)

	cases := []struct {
		name        string
		region      string
		countryCode string
		want        string
	}{
		{"global 账号 +86 手机走阿里云", "global", "+86", model.IdentityProviderPhoneSmsCN},
		{"cn 账号 +86 手机走阿里云", "cn", "+86", model.IdentityProviderPhoneSmsCN},
		{"空 region +86 手机走阿里云", "", "+86", model.IdentityProviderPhoneSmsCN},
		{"cn 账号 +1 手机走 SNS", "cn", "+1", model.IdentityProviderPhoneSmsGlobal},
		{"没有国家码回落到账号 region", "cn", "", model.IdentityProviderPhoneSmsCN},
		{"没有国家码且账号是 global 走 SNS", "global", "", model.IdentityProviderPhoneSmsGlobal},
		{"国家码带空格也认", "global", " +86 ", model.IdentityProviderPhoneSmsCN},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sender, err := reachSMSSender(tc.region, tc.countryCode)
			if err != nil {
				t.Fatalf("reachSMSSender(%q, %q) err=%v", tc.region, tc.countryCode, err)
			}
			got, ok := sender.(*fakeReachSMSProvider)
			if !ok {
				t.Fatalf("unexpected sender type %T", sender)
			}
			if got.name != tc.want {
				t.Fatalf("region=%q country=%q picked %q, want %q", tc.region, tc.countryCode, got.name, tc.want)
			}
		})
	}
}
