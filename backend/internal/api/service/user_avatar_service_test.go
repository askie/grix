package service

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/askie/grix/backend/config"
)

func TestNormalizeAvatarImage_ResizeAndEncodeJPEG(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1200, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 1200; x++ {
			src.SetRGBA(x, y, color.RGBA{
				R: uint8((x * 255) / 1200),
				G: uint8((y * 255) / 600),
				B: 100,
				A: 255,
			})
		}
	}

	var input bytes.Buffer
	if err := png.Encode(&input, src); err != nil {
		t.Fatalf("encode source png failed: %v", err)
	}

	output, err := normalizeAvatarImage(input.Bytes())
	if err != nil {
		t.Fatalf("normalize avatar image failed: %v", err)
	}
	if len(output) == 0 {
		t.Fatal("expected non-empty output bytes")
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(output))
	if err != nil {
		t.Fatalf("decode output config failed: %v", err)
	}
	if format != "jpeg" {
		t.Fatalf("expected jpeg output, got %s", format)
	}
	if cfg.Width != userAvatarTargetSizePx || cfg.Height != userAvatarTargetSizePx {
		t.Fatalf(
			"expected output size %dx%d, got %dx%d",
			userAvatarTargetSizePx,
			userAvatarTargetSizePx,
			cfg.Width,
			cfg.Height,
		)
	}

	if _, err := jpeg.Decode(bytes.NewReader(output)); err != nil {
		t.Fatalf("expected valid jpeg bytes, decode failed: %v", err)
	}
}

func TestNormalizeAvatarImage_InvalidBytes(t *testing.T) {
	_, err := normalizeAvatarImage([]byte("not-an-image"))
	if !errors.Is(err, ErrAvatarImageInvalid) {
		t.Fatalf("expected ErrAvatarImageInvalid, got %v", err)
	}
}

func TestResolveAvatarObjectKey_WithPublicURL(t *testing.T) {
	originalConfig := config.C
	t.Cleanup(func() {
		config.C = originalConfig
	})
	config.C.OSS.Avatar.PublicURL = "https://cdn.example.com/avatar"
	config.C.OSS.Avatar.Bucket = "aibot-avatar"

	objectKey := resolveAvatarObjectKey("https://cdn.example.com/avatar/avatars/8.jpg")
	if objectKey != "avatars/8.jpg" {
		t.Fatalf("expected object key avatars/8.jpg, got %q", objectKey)
	}
}

func TestResolveAvatarObjectKey_WithEndpointFallback(t *testing.T) {
	originalConfig := config.C
	t.Cleanup(func() {
		config.C = originalConfig
	})
	config.C.OSS.Avatar.PublicURL = ""
	config.C.OSS.Avatar.Endpoint = "oss.example.com"
	config.C.OSS.Avatar.Bucket = "aibot-avatar"
	config.C.OSS.Avatar.UseSSL = true

	objectKey := resolveAvatarObjectKey("https://oss.example.com/aibot-avatar/avatars/8.jpg")
	if objectKey != "avatars/8.jpg" {
		t.Fatalf("expected object key avatars/8.jpg, got %q", objectKey)
	}
}

func TestResolveAvatarObjectKey_UsesURLPathForLegacyHosts(t *testing.T) {
	originalConfig := config.C
	t.Cleanup(func() {
		config.C = originalConfig
	})
	config.C.OSS.Avatar.Bucket = "aibot-avatar"

	objectKey := resolveAvatarObjectKey("https://legacy-cdn.example.com/aibot-avatar/user/8/avatar/9.jpg")
	if objectKey != "user/8/avatar/9.jpg" {
		t.Fatalf("expected object key user/8/avatar/9.jpg, got %q", objectKey)
	}
}

func TestIsUserAvatarObjectKey_WithStorageDir(t *testing.T) {
	originalConfig := config.C
	t.Cleanup(func() {
		config.C = originalConfig
	})
	config.C.OSS.Avatar.StorageDir = "prod"

	if !isUserAvatarObjectKey(8, "prod/avatars/8.jpg") {
		t.Fatal("expected object key to match user avatar prefix")
	}
	if !isUserAvatarObjectKey(8, "prod/user/8/avatar/9.jpg") {
		t.Fatal("expected legacy object key to match user avatar prefix")
	}
	if isUserAvatarObjectKey(8, "prod/avatars/9.jpg") {
		t.Fatal("expected object key to reject another user's avatar path")
	}
}

func TestIsUserAvatarObjectKey_WithoutStorageDir(t *testing.T) {
	originalConfig := config.C
	t.Cleanup(func() {
		config.C = originalConfig
	})
	config.C.OSS.Avatar.StorageDir = ""

	if !isUserAvatarObjectKey(8, "avatars/8.jpg") {
		t.Fatal("expected object key to match user avatar prefix")
	}
	if !isUserAvatarObjectKey(8, "user/8/avatar/9.jpg") {
		t.Fatal("expected legacy object key to match user avatar prefix")
	}
	if isUserAvatarObjectKey(8, "avatars/9.jpg") {
		t.Fatal("expected object key to reject non-avatar path")
	}
}

func TestBuildUserAvatarObjectKey_UsesStableAvatarPath(t *testing.T) {
	originalConfig := config.C
	t.Cleanup(func() {
		config.C = originalConfig
	})
	config.C.OSS.Avatar.StorageDir = "prod"

	if got := buildUserAvatarObjectKey(88); got != "prod/avatars/88.jpg" {
		t.Fatalf("expected prod/avatars/88.jpg, got %q", got)
	}
}
