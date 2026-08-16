package service

import (
	"context"
	"errors"
	"testing"

	"github.com/askie/grix/backend/config"
	"github.com/minio/minio-go/v7"
)

func TestValidateMediaUploadContentType(t *testing.T) {
	allowed := []struct{ filename, contentType string }{
		{"photo.jpg", "image/jpeg"},
		{"photo.png", "image/png"},
		{"clip.mp4", "video/mp4"},
		{"voice.m4a", "audio/mp4"},
		{"doc.pdf", "application/pdf"},
		{"notes.md", "text/markdown"},
		{"data.csv", "text/csv"},
		{"archive.zip", "application/zip"},
		{"noext", "image/png"},                      // 无扩展名时只看 Content-Type
		{"photo.JPG", "image/jpeg"},                 // 扩展名大小写不敏感
		{"photo.jpg", "image/jpeg; charset=binary"}, // 参数应被剥离
	}
	for _, tc := range allowed {
		if err := validateMediaUploadContentType(tc.filename, tc.contentType); err != nil {
			t.Fatalf("validateMediaUploadContentType(%q, %q) should pass, got %v", tc.filename, tc.contentType, err)
		}
	}

	rejected := []struct{ filename, contentType string }{
		{"page.html", "text/html"},              // text/html 不在白名单
		{"icon.svg", "image/svg+xml"},           // svg 不在白名单
		{"page.html", "image/png"},              // 危险扩展名伪装图片
		{"icon.svg", "image/png"},               // 同上
		{"script.js", "text/plain"},             // js 扩展名直接拒绝
		{"photo.png", "text/plain"},             // 扩展名大类与 Content-Type 明显不匹配
		{"clip.mp4", "image/png"},               // 同上
		{"doc.pdf", ""},                         // 空 Content-Type
		{"doc.pdf", "application/x-msdownload"}, // exe 等任意二进制
	}
	for _, tc := range rejected {
		if err := validateMediaUploadContentType(tc.filename, tc.contentType); !errors.Is(err, ErrMediaUploadContentType) {
			t.Fatalf("validateMediaUploadContentType(%q, %q) should be rejected, got %v", tc.filename, tc.contentType, err)
		}
	}
}

func TestMediaContentTypeAllowedForSigning(t *testing.T) {
	allowed := []string{
		"image/png",
		"video/mp4",
		"application/pdf",
		"application/octet-stream", // 历史对象/客户端未带 Content-Type 落成的通用二进制
		"binary/octet-stream",
		"",
	}
	for _, ct := range allowed {
		if !mediaContentTypeAllowedForSigning(ct) {
			t.Fatalf("mediaContentTypeAllowedForSigning(%q) should be true", ct)
		}
	}

	rejected := []string{
		"text/html",
		"image/svg+xml",
		"application/xhtml+xml",
		"text/javascript",
		"application/x-msdownload",
	}
	for _, ct := range rejected {
		if mediaContentTypeAllowedForSigning(ct) {
			t.Fatalf("mediaContentTypeAllowedForSigning(%q) should be false", ct)
		}
	}
}

func TestCheckMediaObjectForSigning(t *testing.T) {
	originalConfig := config.C
	originalStat := mediaStatObject
	t.Cleanup(func() {
		config.C = originalConfig
		mediaStatObject = originalStat
		setOSSClient(ossStorageMedia, nil)
	})

	// StatObject 需要非 nil client 才会被调用；测试里注入的 mediaStatObject 不会真正用到它。
	setOSSClient(ossStorageMedia, &minio.Client{})
	config.C.Security.MediaMaxUploadBytes = 1024

	stub := func(info minio.ObjectInfo, err error) {
		mediaStatObject = func(context.Context, string, string) (minio.ObjectInfo, error) {
			return info, err
		}
	}

	stub(minio.ObjectInfo{ContentType: "image/png", Size: 100}, nil)
	if !checkMediaObjectForSigning("user/1/a.png") {
		t.Fatal("whitelisted image within size limit should pass")
	}

	stub(minio.ObjectInfo{ContentType: "text/html", Size: 100}, nil)
	if checkMediaObjectForSigning("user/1/a.html") {
		t.Fatal("text/html object should be rejected")
	}

	stub(minio.ObjectInfo{ContentType: "image/png", Size: 2048}, nil)
	if checkMediaObjectForSigning("user/1/big.png") {
		t.Fatal("object exceeding size limit should be rejected")
	}

	stub(minio.ObjectInfo{}, minio.ErrorResponse{Code: "NoSuchKey"})
	if checkMediaObjectForSigning("user/1/gone.png") {
		t.Fatal("missing object should be rejected")
	}

	stub(minio.ObjectInfo{}, errors.New("connection refused"))
	if !checkMediaObjectForSigning("user/1/a.png") {
		t.Fatal("transient storage error should fail open to keep message rendering working")
	}
}
