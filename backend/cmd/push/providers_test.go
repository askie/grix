package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/pkg/logger"
)

func TestBuildPushProvidersRejectsEmptyConfig(t *testing.T) {
	logger.Init()

	_, err := buildPushProviders(config.PushConfig{})
	if err == nil {
		t.Fatal("expected error for empty push config")
	}
	if !strings.Contains(err.Error(), "no push providers configured") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildPushProvidersRejectsIncompleteAPNsConfig(t *testing.T) {
	logger.Init()

	_, err := buildPushProviders(config.PushConfig{
		APNs: config.APNsConfig{
			KeyPath: "/tmp/apns-key.p8",
			KeyID:   "kid",
		},
	})
	if err == nil {
		t.Fatal("expected error for incomplete apns config")
	}
	if !strings.Contains(err.Error(), "apns config incomplete") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildPushProvidersRejectsIncompleteJPushConfig(t *testing.T) {
	logger.Init()

	_, err := buildPushProviders(config.PushConfig{
		JPush: config.JPushConfig{
			AppKey: "app-key-only",
		},
	})
	if err == nil {
		t.Fatal("expected error for incomplete jpush config")
	}
	if !strings.Contains(err.Error(), "jpush config incomplete") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildPushProvidersRejectsIncompleteWebPushConfig(t *testing.T) {
	logger.Init()

	_, err := buildPushProviders(config.PushConfig{
		WebPush: config.WebPushConfig{
			VAPIDPublicKey: "public-key-only",
		},
	})
	if err == nil {
		t.Fatal("expected error for incomplete web_push config")
	}
	if !strings.Contains(err.Error(), "web_push config incomplete") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildPushProvidersRejectsMissingFCMCredentialsFile(t *testing.T) {
	logger.Init()

	_, err := buildPushProviders(config.PushConfig{
		FCM: config.FCMConfig{
			CredentialsFile: filepath.Join(t.TempDir(), "missing-firebase.json"),
		},
	})
	if err == nil {
		t.Fatal("expected error for missing fcm credentials file")
	}
	if !strings.Contains(err.Error(), "fcm credentials_file invalid") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildPushProvidersAcceptsCompleteConfig(t *testing.T) {
	logger.Init()

	apnsKeyPath := filepath.Join(t.TempDir(), "AuthKey_TEST.p8")
	if err := os.WriteFile(apnsKeyPath, []byte("not-a-real-key-but-file-exists"), 0o600); err != nil {
		t.Fatalf("write apns key file: %v", err)
	}

	fcmPath := filepath.Join(t.TempDir(), "firebase.json")
	if err := os.WriteFile(fcmPath, []byte(`{"project_id":"demo-project"}`), 0o600); err != nil {
		t.Fatalf("write fcm credentials file: %v", err)
	}

	providers, err := buildPushProviders(config.PushConfig{
		APNs: config.APNsConfig{
			KeyPath: apnsKeyPath,
			KeyID:   "kid",
			TeamID:  "team",
			Topic:   "com.example.frontend",
		},
		FCM: config.FCMConfig{
			CredentialsFile: fcmPath,
		},
		JPush: config.JPushConfig{
			AppKey:       "app-key",
			MasterSecret: "master-secret",
		},
		WebPush: config.WebPushConfig{
			VAPIDPublicKey:  "vapid-public",
			VAPIDPrivateKey: "vapid-private",
			Subscriber:      "mailto:push@example.com",
		},
	})
	if err != nil {
		t.Fatalf("buildPushProviders returned error: %v", err)
	}
	if providers == nil || providers.apnsSandbox == nil || providers.apnsProduction == nil || providers.fcm == nil || providers.jpush == nil || providers.webpush == nil {
		t.Fatalf("expected all providers to be initialized, got %#v", providers)
	}
}
