package main

import (
	"path/filepath"
	"strings"

	"github.com/askie/grix/backend/internal/pkg/logger"
)

type providerStartupSummary struct {
	enabled []string
	apns    *apnsStartupSummary
	fcm     *fcmStartupSummary
	jpush   *jpushStartupSummary
	webpush *webPushStartupSummary
	vendors []vendorStartupSummary
}

// vendorStartupSummary 记录已启用的国产厂商推送通道及其凭据指纹。
type vendorStartupSummary struct {
	Name    string
	KeyHint string
}

type apnsStartupSummary struct {
	Environments []string
	Topic        string
	KeyPath      string
}

type fcmStartupSummary struct {
	CredentialsFile string
}

type jpushStartupSummary struct {
	AppKeyHint string
}

type webPushStartupSummary struct {
	VAPIDPublicKeyHint string
	Subscriber         string
}

func (s providerStartupSummary) log() {
	fields := []any{
		"enabled", strings.Join(s.enabled, ", "),
	}
	if s.apns != nil {
		fields = append(fields,
			"apns_envs", strings.Join(s.apns.Environments, ","),
			"apns_topic", s.apns.Topic,
			"apns_key_path", s.apns.KeyPath,
		)
	}
	if s.fcm != nil {
		fields = append(fields,
			"fcm_credentials_file", s.fcm.CredentialsFile,
		)
	}
	if s.jpush != nil {
		fields = append(fields,
			"jpush_app_key_hint", s.jpush.AppKeyHint,
		)
	}
	for _, vendor := range s.vendors {
		fields = append(fields,
			vendor.Name+"_key_hint", vendor.KeyHint,
		)
	}
	if s.webpush != nil {
		fields = append(fields,
			"web_push_vapid_public_key_hint", s.webpush.VAPIDPublicKeyHint,
			"web_push_subscriber", s.webpush.Subscriber,
		)
	}
	logger.L.Infow("push startup summary", fields...)
}

func resolveLogPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	absolutePath, err := filepath.Abs(trimmed)
	if err != nil {
		return filepath.Clean(trimmed)
	}
	return absolutePath
}

func redactKeyHint(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= 6 {
		return trimmed
	}
	if len(trimmed) <= 12 {
		return trimmed[:3] + "..." + trimmed[len(trimmed)-3:]
	}
	return trimmed[:4] + "..." + trimmed[len(trimmed)-4:]
}
