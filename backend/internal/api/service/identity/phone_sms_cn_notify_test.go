package identity

import (
	"errors"
	"testing"
)

// 通知类文案绝不能回落到营销模板：阿里云对两类模板的报备口径不同，混用会被判违规。
func TestPhoneSmsCN_TemplateForTextKind(t *testing.T) {
	p := &PhoneSmsCN{cfg: AliyunSmsConfig{TemplateCodeMarketing: "SMS_MKT"}}

	if _, err := p.templateForTextKind(SmsTextKindNotify); !errors.Is(err, ErrSmsTemplateNotConfigured) {
		t.Fatalf("notify without template should report not-configured, got %v", err)
	}

	got, err := p.templateForTextKind(SmsTextKindMarketing)
	if err != nil || got != "SMS_MKT" {
		t.Fatalf("marketing kind should use marketing template, got %q err=%v", got, err)
	}

	p.cfg.TemplateCodeNotify = "SMS_NOTIFY"
	got, err = p.templateForTextKind(SmsTextKindNotify)
	if err != nil || got != "SMS_NOTIFY" {
		t.Fatalf("notify kind should use notify template, got %q err=%v", got, err)
	}

	// 留空沿用历史行为：按营销类走。
	got, err = p.templateForTextKind("")
	if err != nil || got != "SMS_MKT" {
		t.Fatalf("empty kind should fall back to marketing, got %q err=%v", got, err)
	}
}
