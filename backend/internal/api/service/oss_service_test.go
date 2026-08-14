package service

import (
	"errors"
	"testing"

	"github.com/askie/grix/backend/config"
)

func TestValidateOSSRuntimeConfig_RequiresBucket(t *testing.T) {
	err := validateOSSRuntimeConfig(config.OSSConfig{
		Endpoint:  "oss.example.com",
		AccessKey: "ak",
		SecretKey: "sk",
	})
	if !errors.Is(err, ErrOSSBucketRequired) {
		t.Fatalf("expected ErrOSSBucketRequired, got %v", err)
	}
}

func TestValidateOSSRuntimeConfig_AllowsCompleteConfig(t *testing.T) {
	err := validateOSSRuntimeConfig(config.OSSConfig{
		Endpoint:  "oss.example.com",
		AccessKey: "ak",
		SecretKey: "sk",
		Bucket:    "aibot",
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestBuildMediaAccessURL_UsesMediaConfig(t *testing.T) {
	originalConfig := config.C
	t.Cleanup(func() {
		config.C = originalConfig
	})

	config.C.OSS.Media.PublicURL = "https://cdn.example.com/media"

	got := BuildMediaAccessURL("chat/1.png")
	if got != "https://cdn.example.com/media/chat/1.png" {
		t.Fatalf("expected media access URL, got %q", got)
	}
}

func TestBuildAvatarAccessURL_UsesAvatarConfig(t *testing.T) {
	originalConfig := config.C
	t.Cleanup(func() {
		config.C = originalConfig
	})

	config.C.OSS.Avatar.Endpoint = "oss.example.com"
	config.C.OSS.Avatar.Bucket = "aibot-avatar"
	config.C.OSS.Avatar.UseSSL = true

	got := buildAvatarAccessURL("avatars/88.jpg")
	if got != "https://oss.example.com/aibot-avatar/avatars/88.jpg" {
		t.Fatalf("expected avatar access URL, got %q", got)
	}
}

func TestBuildStorageObjectKey_UsesStorageDir(t *testing.T) {
	got := buildStorageObjectKey(config.OSSConfig{
		StorageDir: "prod/report",
	}, "report-assets/1/a.png")
	if got != "prod/report/report-assets/1/a.png" {
		t.Fatalf("expected storage dir applied, got %q", got)
	}
}

func TestIsUserMediaObjectKey_MatchesOwnedPrefix(t *testing.T) {
	originalConfig := config.C
	t.Cleanup(func() {
		config.C = originalConfig
	})

	config.C.OSS.Media.StorageDir = "aibot/media"

	if !isUserMediaObjectKey(42, "aibot/media/user/42/1_demo.png") {
		t.Fatalf("expected owned media object key to pass")
	}
	if isUserMediaObjectKey(42, "aibot/media/user/99/1_demo.png") {
		t.Fatalf("expected foreign media object key to fail")
	}
}
