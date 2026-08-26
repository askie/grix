package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aliyun/alibaba-cloud-sdk-go/services/dysmsapi"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
)

// AliyunSmsConfig 阿里短信 provider 配置。明文，由上层从加密的 system_settings 解密后传入。
type AliyunSmsConfig struct {
	RegionID             string
	AccessKeyID          string
	AccessKeySecret      string
	SignName             string
	TemplateCodeRegister string
	TemplateCodeLogin    string
	TemplateCodeReset    string
	// TemplateCodeMarketing 营销模板号，模板变量固定为 ${content}。
	TemplateCodeMarketing string
	// TemplateCodeNotify 通知模板号，模板变量同样固定为 ${content}。
	TemplateCodeNotify string
}

// PhoneSmsCN 阿里云国内短信 provider；client singleton（性能补丁第 21 条）。
type PhoneSmsCN struct {
	cfg    AliyunSmsConfig
	client *dysmsapi.Client
}

// NewPhoneSmsCN 用配置实例化 provider；ak/sk 缺失时仍可构造但 Send 时返 ErrProviderNotConfigured。
func NewPhoneSmsCN(cfg AliyunSmsConfig) (*PhoneSmsCN, error) {
	p := &PhoneSmsCN{cfg: cfg}
	if cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" {
		return p, nil // 允许"未配置"实例存在，Send 时再拒
	}
	region := strings.TrimSpace(cfg.RegionID)
	if region == "" {
		region = "cn-hangzhou"
	}
	cli, err := dysmsapi.NewClientWithAccessKey(region, cfg.AccessKeyID, cfg.AccessKeySecret)
	if err != nil {
		return nil, err
	}
	p.client = cli
	return p, nil
}

func (p *PhoneSmsCN) Name() string { return model.IdentityProviderPhoneSmsCN }

func (p *PhoneSmsCN) Send(ctx context.Context, req SendSmsRequest) error {
	if p.client == nil || p.cfg.SignName == "" {
		return ErrProviderNotConfigured
	}
	tmpl, err := p.templateForScene(req.Scene)
	if err != nil {
		return err
	}

	r := dysmsapi.CreateSendSmsRequest()
	r.Scheme = "https"
	r.SignName = p.cfg.SignName
	r.TemplateCode = tmpl
	// 阿里短信国内号码不带 +86 前缀；剥去
	r.PhoneNumbers = strings.TrimPrefix(req.PhoneE164, "+86")
	param, _ := json.Marshal(map[string]string{"code": req.Code})
	r.TemplateParam = string(param)

	resp, err := p.client.SendSms(r)
	if err != nil {
		logger.L.Errorf("aliyun SendSms failed phone=%s scene=%s err=%v", PhoneMask(req.PhoneE164), req.Scene, err)
		return errors.New("短信发送失败")
	}
	if resp == nil || strings.ToUpper(resp.Code) != "OK" {
		bizCode := ""
		bizMsg := ""
		if resp != nil {
			bizCode = resp.Code
			bizMsg = resp.Message
		}
		logger.L.Errorf("aliyun SendSms biz fail phone=%s scene=%s code=%s msg=%s", PhoneMask(req.PhoneE164), req.Scene, bizCode, bizMsg)
		return fmt.Errorf("短信发送失败：%s", bizMsg)
	}
	return nil
}

func (p *PhoneSmsCN) CheckMarketing(kind string) error {
	if p.client == nil || p.cfg.SignName == "" {
		return ErrProviderNotConfigured
	}
	_, err := p.templateForTextKind(kind)
	return err
}

func (p *PhoneSmsCN) SendMarketing(ctx context.Context, req MarketingSmsRequest) error {
	if err := p.CheckMarketing(req.Kind); err != nil {
		return err
	}
	tmpl, err := p.templateForTextKind(req.Kind)
	if err != nil {
		return err
	}
	r := dysmsapi.CreateSendSmsRequest()
	r.Scheme = "https"
	r.SignName = p.cfg.SignName
	r.TemplateCode = tmpl
	r.PhoneNumbers = strings.TrimPrefix(req.PhoneE164, "+86")
	param, _ := json.Marshal(map[string]string{"content": req.Text})
	r.TemplateParam = string(param)

	resp, err := p.client.SendSms(r)
	if err != nil {
		logger.L.Errorf("aliyun SendSms marketing failed phone=%s err=%v", PhoneMask(req.PhoneE164), err)
		return errors.New("短信发送失败")
	}
	if resp == nil || strings.ToUpper(resp.Code) != "OK" {
		bizCode, bizMsg := "", ""
		if resp != nil {
			bizCode, bizMsg = resp.Code, resp.Message
		}
		logger.L.Errorf("aliyun SendSms marketing biz fail phone=%s code=%s msg=%s", PhoneMask(req.PhoneE164), bizCode, bizMsg)
		return fmt.Errorf("短信发送失败：%s", bizMsg)
	}
	return nil
}

// templateForTextKind 按用途挑模板号。通知类绝不回落到营销模板：阿里云对两类模板的
// 报备口径不同，混用会被判违规。
func (p *PhoneSmsCN) templateForTextKind(kind string) (string, error) {
	if kind == SmsTextKindNotify {
		if p.cfg.TemplateCodeNotify == "" {
			return "", fmt.Errorf("阿里通知短信模板号未配置: %w", ErrSmsTemplateNotConfigured)
		}
		return p.cfg.TemplateCodeNotify, nil
	}
	if p.cfg.TemplateCodeMarketing == "" {
		return "", fmt.Errorf("阿里营销短信模板号未配置: %w", ErrSmsTemplateNotConfigured)
	}
	return p.cfg.TemplateCodeMarketing, nil
}

func (p *PhoneSmsCN) HealthCheck(_ context.Context) error {
	if p.cfg.AccessKeyID == "" || p.cfg.AccessKeySecret == "" || p.cfg.SignName == "" {
		return ErrProviderNotConfigured
	}
	if p.cfg.TemplateCodeRegister == "" && p.cfg.TemplateCodeLogin == "" {
		return errors.New("阿里短信模板号未配置")
	}
	return nil
}

func (p *PhoneSmsCN) templateForScene(scene SmsSendScene) (string, error) {
	switch scene {
	case SmsSceneRegister:
		if p.cfg.TemplateCodeRegister == "" {
			return "", errors.New("注册短信模板号未配置")
		}
		return p.cfg.TemplateCodeRegister, nil
	case SmsSceneLogin, SmsSceneBind:
		// login-code 接口幂等：未注册即注册，已注册即登录。
		// 模板号若分别配则用各自；缺哪个用 register 兜底（运营常先报登录模板）。
		if p.cfg.TemplateCodeLogin != "" {
			return p.cfg.TemplateCodeLogin, nil
		}
		return p.cfg.TemplateCodeRegister, nil
	case SmsSceneReset:
		if p.cfg.TemplateCodeReset != "" {
			return p.cfg.TemplateCodeReset, nil
		}
		return p.cfg.TemplateCodeLogin, nil
	default:
		return "", errors.New("不支持的短信场景")
	}
}
