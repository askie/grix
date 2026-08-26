package identity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
)

// AwsSnsConfig AWS SNS provider 配置。
type AwsSnsConfig struct {
	Region          string
	AccessKeyID     string
	AccessKeySecret string
	SenderID        string // 可选；部分国家需在 AWS 控制台预注册
}

// PhoneSmsGlobal AWS SNS Publish 走全球短信。
// 重要：必须设置 MessageAttributes AWS.SNS.SMS.SMSType=Transactional，
// 否则 AWS 视为 Promotional → 优先级低 / 部分国家夜间静默 → OTP 体验差（补丁清单第 16 条）。
type PhoneSmsGlobal struct {
	cfg  AwsSnsConfig
	http *http.Client
}

func NewPhoneSmsGlobal(cfg AwsSnsConfig) *PhoneSmsGlobal {
	return &PhoneSmsGlobal{
		cfg:  cfg,
		http: &http.Client{Timeout: 5 * time.Second},
	}
}

func (p *PhoneSmsGlobal) Name() string { return model.IdentityProviderPhoneSmsGlobal }

func (p *PhoneSmsGlobal) Send(ctx context.Context, req SendSmsRequest) error {
	// SMSType=Transactional：OTP 必须，确保优先级与发送窗口
	return p.publish(ctx, req.PhoneE164, buildSnsMessage(req), "Transactional", string(req.Scene))
}

func (p *PhoneSmsGlobal) CheckMarketing(string) error {
	// SNS 直发文案，没有模板概念；ak/sk 齐备即可发。
	if p.cfg.AccessKeyID == "" || p.cfg.AccessKeySecret == "" {
		return ErrProviderNotConfigured
	}
	return nil
}

func (p *PhoneSmsGlobal) SendMarketing(ctx context.Context, req MarketingSmsRequest) error {
	// 通知类走 Transactional：与阿里云侧"通知不混营销模板"同一个道理，
	// Promotional 在部分国家会被降优先级甚至夜间静默。
	if err := p.CheckMarketing(req.Kind); err != nil {
		return err
	}
	smsType, scene := "Promotional", "marketing"
	if req.Kind == SmsTextKindNotify {
		smsType, scene = "Transactional", "notify"
	}
	return p.publish(ctx, req.PhoneE164, req.Text, smsType, scene)
}

func (p *PhoneSmsGlobal) publish(ctx context.Context, phone, message, smsType, scene string) error {
	if p.cfg.AccessKeyID == "" || p.cfg.AccessKeySecret == "" {
		return ErrProviderNotConfigured
	}
	region := strings.TrimSpace(p.cfg.Region)
	if region == "" {
		region = "ap-southeast-1"
	}

	form := url.Values{}
	form.Set("Action", "Publish")
	form.Set("Version", "2010-03-31")
	form.Set("Message", message)
	form.Set("PhoneNumber", phone)
	form.Set("MessageAttributes.entry.1.Name", "AWS.SNS.SMS.SMSType")
	form.Set("MessageAttributes.entry.1.Value.DataType", "String")
	form.Set("MessageAttributes.entry.1.Value.StringValue", smsType)
	if id := strings.TrimSpace(p.cfg.SenderID); id != "" {
		form.Set("MessageAttributes.entry.2.Name", "AWS.SNS.SMS.SenderID")
		form.Set("MessageAttributes.entry.2.Value.DataType", "String")
		form.Set("MessageAttributes.entry.2.Value.StringValue", id)
	}

	body := form.Encode()
	endpoint := fmt.Sprintf("https://sns.%s.amazonaws.com/", region)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return errors.New("短信发送失败")
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	signAWSV4Form(httpReq, []byte(body), p.cfg.AccessKeyID, p.cfg.AccessKeySecret, region, "sns", time.Now())

	resp, err := p.http.Do(httpReq)
	if err != nil {
		logger.L.Errorf("aws sns publish err phone=%s scene=%s err=%v", PhoneMask(phone), scene, err)
		return errors.New("短信发送失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		logger.L.Errorf("aws sns publish non-2xx phone=%s scene=%s status=%d body=%s",
			PhoneMask(phone), scene, resp.StatusCode, truncateForLog(string(bodyBytes), 256))
		return errors.New("短信发送失败")
	}
	return nil
}

func (p *PhoneSmsGlobal) HealthCheck(_ context.Context) error {
	if p.cfg.AccessKeyID == "" || p.cfg.AccessKeySecret == "" {
		return ErrProviderNotConfigured
	}
	return nil
}

// buildSnsMessage 按语言出 zh/en 文案（补丁清单第 7 条）。
func buildSnsMessage(req SendSmsRequest) string {
	lang := strings.ToLower(strings.TrimSpace(req.Lang))
	if strings.HasPrefix(lang, "zh") {
		return fmt.Sprintf("您的验证码：%s，5 分钟内有效。", req.Code)
	}
	return fmt.Sprintf("Your verification code: %s (valid for 5 minutes).", req.Code)
}

func truncateForLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
