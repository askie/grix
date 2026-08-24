package service

import (
	"github.com/askie/grix/backend/internal/api/service/identity"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/systemsetting"
)

// InitSmsBootstrap 在 cmd/api 启动时调用一次：
//  1. 给 systemsetting.SaveSmsSettings 注入 reload 钩子；
//  2. 立刻读一次配置，把 SMS provider 注册到 identity.Registry，
//     之后 SendPhoneSmsCode 调用时直接通过 Registry 拿 client（client singleton）。
func InitSmsBootstrap() {
	systemsetting.RegisterSmsReloadHook(reloadSmsProviders)
	cfg, err := systemsetting.GetSmsSettings()
	if err != nil {
		logger.L.Warnf("init sms providers: read settings failed: %v", err)
		return
	}
	reloadSmsProviders(cfg)
}

func reloadSmsProviders(cfg systemsetting.SmsSettings) {
	reg := identity.Default()

	cn, err := identity.NewPhoneSmsCN(identity.AliyunSmsConfig{
		RegionID:              cfg.Aliyun.RegionID,
		AccessKeyID:           cfg.Aliyun.AccessKeyID,
		AccessKeySecret:       cfg.Aliyun.AccessKeySecret,
		SignName:              cfg.Aliyun.SignName,
		TemplateCodeRegister:  cfg.Aliyun.TemplateCodeRegister,
		TemplateCodeLogin:     cfg.Aliyun.TemplateCodeLogin,
		TemplateCodeReset:     cfg.Aliyun.TemplateCodeReset,
		TemplateCodeMarketing: cfg.Aliyun.TemplateCodeMarketing,
	})
	if err != nil {
		logger.L.Warnf("build aliyun sms provider failed: %v", err)
	} else {
		reg.SetSms(cn)
	}

	global := identity.NewPhoneSmsGlobal(identity.AwsSnsConfig{
		Region:          cfg.AwsSns.Region,
		AccessKeyID:     cfg.AwsSns.AccessKeyID,
		AccessKeySecret: cfg.AwsSns.AccessKeySecret,
		SenderID:        cfg.AwsSns.SenderID,
	})
	reg.SetSms(global)
}
