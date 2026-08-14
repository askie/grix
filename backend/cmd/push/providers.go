package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	pushprov "github.com/askie/grix/backend/internal/push/provider"
)

type pushProviders struct {
	apnsSandbox    *pushprov.APNsProvider
	apnsProduction *pushprov.APNsProvider
	fcm            *pushprov.FCMProvider
	jpush          *pushprov.JPushProvider
	webpush        *pushprov.WebPushProvider
	// vendors 按设备平台索引已配置凭据的国产厂商推送通道。
	vendors map[string]pushprov.VendorSender
}

func buildPushProviders(cfg config.PushConfig) (*pushProviders, error) {
	providers := &pushProviders{vendors: map[string]pushprov.VendorSender{}}
	summary := providerStartupSummary{}

	apnsSandbox, apnsProduction, ok, err := buildAPNsProviders(cfg.APNs)
	if err != nil {
		return nil, err
	}
	if ok {
		providers.apnsSandbox = apnsSandbox
		providers.apnsProduction = apnsProduction
		summary.enabled = append(summary.enabled, "apns(sandbox)", "apns(production)")
		summary.apns = &apnsStartupSummary{
			Environments: []string{"sandbox", "production"},
			Topic:        cfg.APNs.Topic,
			KeyPath:      resolveLogPath(cfg.APNs.KeyPath),
		}
	}

	fcm, ok, err := buildFCMProvider(cfg.FCM)
	if err != nil {
		return nil, err
	}
	if ok {
		providers.fcm = fcm
		summary.enabled = append(summary.enabled, "fcm")
		summary.fcm = &fcmStartupSummary{
			CredentialsFile: resolveLogPath(cfg.FCM.CredentialsFile),
		}
	}

	jpush, ok, err := buildJPushProvider(cfg.JPush)
	if err != nil {
		return nil, err
	}
	if ok {
		providers.jpush = jpush
		summary.enabled = append(summary.enabled, "jpush")
		summary.jpush = &jpushStartupSummary{
			AppKeyHint: redactKeyHint(cfg.JPush.AppKey),
		}
	}

	webpushProvider, ok, err := buildWebPushProvider(cfg.WebPush)
	if err != nil {
		return nil, err
	}
	if ok {
		providers.webpush = webpushProvider
		summary.enabled = append(summary.enabled, "web_push")
		summary.webpush = &webPushStartupSummary{
			VAPIDPublicKeyHint: redactKeyHint(cfg.WebPush.VAPIDPublicKey),
			Subscriber:         cfg.WebPush.Subscriber,
		}
	}

	huawei, ok, err := buildHuaweiProvider(cfg.Huawei)
	if err != nil {
		return nil, err
	}
	if ok {
		providers.vendors[model.DevicePlatformAndroidHuawei] = huawei
		summary.enabled = append(summary.enabled, "huawei")
		summary.vendors = append(summary.vendors, vendorStartupSummary{
			Name:    "huawei",
			KeyHint: redactKeyHint(cfg.Huawei.ClientID),
		})
	}

	xiaomi, ok, err := buildXiaomiProvider(cfg.Xiaomi)
	if err != nil {
		return nil, err
	}
	if ok {
		providers.vendors[model.DevicePlatformAndroidXiaomi] = xiaomi
		summary.enabled = append(summary.enabled, "xiaomi")
		summary.vendors = append(summary.vendors, vendorStartupSummary{
			Name: "xiaomi",
			// 打包名而非 AppSecret：redactKeyHint 会露出首尾各 4 字符，
			// 对公开标识（如华为 ClientID）无妨，对密钥本体则是泄漏。
			KeyHint: cfg.Xiaomi.PackageName,
		})
	}

	if len(summary.enabled) == 0 {
		return nil, fmt.Errorf("no push providers configured; set APNs, FCM, JPush, WebPush, or vendor (Huawei/Xiaomi) credentials before starting cmd/push")
	}

	summary.log()
	return providers, nil
}

func buildHuaweiProvider(cfg config.HuaweiConfig) (*pushprov.HuaweiProvider, bool, error) {
	values := map[string]string{
		"app_id":        cfg.AppID,
		"client_id":     cfg.ClientID,
		"client_secret": cfg.ClientSecret,
	}
	used, missing := collectConfigUsage(values)
	if !used {
		return nil, false, nil
	}
	if len(missing) > 0 {
		return nil, false, fmt.Errorf("huawei push config incomplete, missing: %s", strings.Join(missing, ", "))
	}
	return pushprov.NewHuawei(cfg.AppID, cfg.ClientID, cfg.ClientSecret), true, nil
}

func buildXiaomiProvider(cfg config.XiaomiConfig) (*pushprov.XiaomiProvider, bool, error) {
	values := map[string]string{
		"app_secret":   cfg.AppSecret,
		"package_name": cfg.PackageName,
	}
	used, missing := collectConfigUsage(values)
	if !used {
		return nil, false, nil
	}
	if len(missing) > 0 {
		return nil, false, fmt.Errorf("xiaomi push config incomplete, missing: %s", strings.Join(missing, ", "))
	}
	return pushprov.NewXiaomi(cfg.AppSecret, cfg.PackageName), true, nil
}

func buildAPNsProviders(cfg config.APNsConfig) (*pushprov.APNsProvider, *pushprov.APNsProvider, bool, error) {
	values := map[string]string{
		"key_path": cfg.KeyPath,
		"key_id":   cfg.KeyID,
		"team_id":  cfg.TeamID,
		"topic":    cfg.Topic,
	}
	used, missing := collectConfigUsage(values)
	if !used {
		return nil, nil, false, nil
	}
	if len(missing) > 0 {
		return nil, nil, false, fmt.Errorf("apns config incomplete, missing: %s", strings.Join(missing, ", "))
	}
	if _, err := os.Stat(cfg.KeyPath); err != nil {
		return nil, nil, false, fmt.Errorf("apns key_path invalid: %w", err)
	}
	return pushprov.NewAPNs(cfg.KeyPath, cfg.KeyID, cfg.TeamID, cfg.Topic, false), pushprov.NewAPNs(cfg.KeyPath, cfg.KeyID, cfg.TeamID, cfg.Topic, true), true, nil
}

func buildFCMProvider(cfg config.FCMConfig) (*pushprov.FCMProvider, bool, error) {
	if strings.TrimSpace(cfg.CredentialsFile) == "" {
		return nil, false, nil
	}
	if _, err := os.Stat(cfg.CredentialsFile); err != nil {
		return nil, false, fmt.Errorf("fcm credentials_file invalid: %w", err)
	}
	return pushprov.NewFCM(cfg.CredentialsFile), true, nil
}

func buildJPushProvider(cfg config.JPushConfig) (*pushprov.JPushProvider, bool, error) {
	values := map[string]string{
		"app_key":       cfg.AppKey,
		"master_secret": cfg.MasterSecret,
	}
	used, missing := collectConfigUsage(values)
	if !used {
		return nil, false, nil
	}
	if len(missing) > 0 {
		return nil, false, fmt.Errorf("jpush config incomplete, missing: %s", strings.Join(missing, ", "))
	}
	return pushprov.NewJPush(cfg.AppKey, cfg.MasterSecret), true, nil
}

func buildWebPushProvider(cfg config.WebPushConfig) (*pushprov.WebPushProvider, bool, error) {
	values := map[string]string{
		"vapid_public_key":  cfg.VAPIDPublicKey,
		"vapid_private_key": cfg.VAPIDPrivateKey,
		"subscriber":        cfg.Subscriber,
	}
	used, missing := collectConfigUsage(values)
	if !used {
		return nil, false, nil
	}
	if len(missing) > 0 {
		return nil, false, fmt.Errorf("web_push config incomplete, missing: %s", strings.Join(missing, ", "))
	}
	return pushprov.NewWebPush(cfg.VAPIDPublicKey, cfg.VAPIDPrivateKey, cfg.Subscriber), true, nil
}

func collectConfigUsage(values map[string]string) (bool, []string) {
	used := false
	missing := make([]string, 0, len(values))
	for name, value := range values {
		if strings.TrimSpace(value) != "" {
			used = true
			continue
		}
		missing = append(missing, name)
	}
	return used, missing
}
