package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/ws/agentmsg"
	"github.com/minio/minio-go/v7"
)

type ossStorageKind string

const (
	ossStorageMedia  ossStorageKind = "media"
	ossStorageAvatar ossStorageKind = "avatar"
	ossStorageReport ossStorageKind = "report"
)

type ossRuntime struct {
	mu     sync.RWMutex
	client *minio.Client
}

var (
	mediaOSSRuntime  ossRuntime
	avatarOSSRuntime ossRuntime
	reportOSSRuntime ossRuntime
)

var (
	ErrOSSEndpointRequired  = errors.New("OSS Endpoint 未配置")
	ErrOSSAccessKeyRequired = errors.New("OSS AccessKey 未配置")
	ErrOSSSecretKeyRequired = errors.New("OSS SecretKey 未配置")
	ErrOSSBucketRequired    = errors.New("OSS Bucket 未配置")
	ErrOSSNotInitialized     = errors.New("OSS 未初始化")
	ErrMediaObjectForbidden  = errors.New("media object forbidden")
	ErrInvalidUploadFilename = errors.New("invalid upload filename")
)

// safeObjectSegment 提取安全的对象 key 文件名段，剥离任何目录穿越成分，
// 防止把对象 key 拼到其他用户/会话前缀下（路径穿越越权写入）。
func safeObjectSegment(name string) (string, bool) {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "" || base == "." || base == ".." || strings.ContainsAny(base, `/\`) {
		return "", false
	}
	return base, true
}

func InitOSS() error {
	var initErrs []string
	for _, storage := range []ossStorageKind{
		ossStorageMedia,
		ossStorageAvatar,
		ossStorageReport,
	} {
		if err := initOSSStorage(storage); err != nil {
			initErrs = append(initErrs, fmt.Sprintf("%s: %v", storage, err))
		}
	}
	if len(initErrs) > 0 {
		return errors.New(strings.Join(initErrs, "; "))
	}
	return nil
}

func initOSSStorage(storage ossStorageKind) error {
	cfg := getOSSConfig(storage)
	if err := validateOSSRuntimeConfig(cfg); err != nil {
		setOSSClient(storage, nil)
		return err
	}

	client, err := newOSSClient(cfg)
	if err != nil {
		setOSSClient(storage, nil)
		return err
	}

	setOSSClient(storage, client)
	return nil
}

type PresignResp struct {
	UploadURL      string `json:"upload_url"`
	ObjectKey      string `json:"object_key"`
	MediaAccessURL string `json:"media_access_url,omitempty"`
}

func OSSPresign(userID int64, filename, contentType string) (*PresignResp, error) {
	if err := ensureOSSReady(); err != nil {
		return nil, err
	}
	safeName, ok := safeObjectSegment(filename)
	if !ok {
		return nil, ErrInvalidUploadFilename
	}
	_ = contentType

	objectKey := buildStorageObjectKey(
		getOSSConfig(ossStorageMedia),
		fmt.Sprintf("user/%d/%d_%s", userID, time.Now().UnixMilli(), safeName),
	)
	presignedURL, err := getOSSClient(ossStorageMedia).PresignedPutObject(
		context.Background(),
		getOSSConfig(ossStorageMedia).Bucket,
		objectKey,
		10*time.Minute,
	)
	if err != nil {
		return nil, err
	}

	return &PresignResp{
		UploadURL:      presignedURL.String(),
		ObjectKey:      objectKey,
		MediaAccessURL: BuildMediaAccessURL(objectKey),
	}, nil
}

// OSSPresignForObjectKey generates a presigned upload URL for a specific object key
// on media storage. The input key should be relative (without leading slash).
func OSSPresignForObjectKey(objectKey, contentType string) (*PresignResp, error) {
	if err := ensureOSSReady(); err != nil {
		return nil, err
	}
	_ = contentType

	normalized := buildStorageObjectKey(getOSSConfig(ossStorageMedia), objectKey)
	presignedURL, err := getOSSClient(ossStorageMedia).PresignedPutObject(
		context.Background(),
		getOSSConfig(ossStorageMedia).Bucket,
		normalized,
		10*time.Minute,
	)
	if err != nil {
		return nil, err
	}

	return &PresignResp{
		UploadURL:      presignedURL.String(),
		ObjectKey:      normalized,
		MediaAccessURL: BuildMediaAccessURL(normalized),
	}, nil
}

// OSSPresignForExactObjectKey generates a presigned upload URL for a specific
// media object key without applying the shared storage_dir prefix.
func OSSPresignForExactObjectKey(objectKey, contentType string) (*PresignResp, error) {
	if err := ensureOSSReady(); err != nil {
		return nil, err
	}
	_ = contentType

	normalized := strings.TrimLeft(strings.TrimSpace(objectKey), "/")
	presignedURL, err := getOSSClient(ossStorageMedia).PresignedPutObject(
		context.Background(),
		getOSSConfig(ossStorageMedia).Bucket,
		normalized,
		10*time.Minute,
	)
	if err != nil {
		return nil, err
	}

	return &PresignResp{
		UploadURL:      presignedURL.String(),
		ObjectKey:      normalized,
		MediaAccessURL: BuildMediaAccessURL(normalized),
	}, nil
}

func DeleteUserMediaObjects(userID int64, objectKeys []string) error {
	if err := ensureOSSReady(); err != nil {
		return err
	}
	if len(objectKeys) == 0 {
		return nil
	}

	client := getOSSClient(ossStorageMedia)
	bucket := getOSSConfig(ossStorageMedia).Bucket
	ctx := context.Background()
	for _, objectKey := range objectKeys {
		normalized := strings.TrimSpace(objectKey)
		if normalized == "" {
			continue
		}
		if !isUserMediaObjectKey(userID, normalized) {
			return ErrMediaObjectForbidden
		}
		if err := client.RemoveObject(
			ctx,
			bucket,
			normalized,
			minio.RemoveObjectOptions{},
		); err != nil {
			return err
		}
	}
	return nil
}

// OSSPresignForSession generates a presigned upload URL for Agent API media,
// using the path structure: media/{session_id}/{timestamp}_{filename}.
// 权限与"能否在该会话发消息"对齐：调用方必须是该会话的 agent 成员或持有 owner 委派，
// 否则拒绝，避免向他人会话媒体目录越权写入。
func OSSPresignForSession(ctx context.Context, sessionID string, ownerID, agentID int64, filename, contentType string) (*PresignResp, error) {
	if err := ensureSessionAccessible(ctx, sessionID); err != nil {
		return nil, err
	}
	if _, err := agentmsg.ResolveIdentity(ctx, agentmsg.IdentityParams{
		Mode:      agentmsg.ModeAgentAPI,
		SessionID: sessionID,
		OwnerID:   ownerID,
		AgentID:   agentID,
	}); err != nil {
		return nil, ErrSessionPermissionDenied
	}
	if err := ensureOSSReady(); err != nil {
		return nil, err
	}
	safeName, ok := safeObjectSegment(filename)
	if !ok {
		return nil, ErrInvalidUploadFilename
	}
	_ = contentType

	objectKey := buildStorageObjectKey(
		getOSSConfig(ossStorageMedia),
		fmt.Sprintf("media/%s/%d_%s", sessionID, time.Now().UnixMilli(), safeName),
	)
	presignedURL, err := getOSSClient(ossStorageMedia).PresignedPutObject(
		context.Background(),
		getOSSConfig(ossStorageMedia).Bucket,
		objectKey,
		10*time.Minute,
	)
	if err != nil {
		return nil, err
	}

	return &PresignResp{
		UploadURL:      presignedURL.String(),
		ObjectKey:      objectKey,
		MediaAccessURL: BuildMediaAccessURL(objectKey),
	}, nil
}

// BuildMediaAccessURL constructs the access URL for media uploads.
func BuildMediaAccessURL(objectKey string) string {
	return buildOSSAccessURL(ossStorageMedia, objectKey)
}

func buildAvatarAccessURL(objectKey string) string {
	return buildOSSAccessURL(ossStorageAvatar, objectKey)
}

func buildOSSAccessURL(storage ossStorageKind, objectKey string) string {
	cfg := getOSSConfig(storage)
	publicURL := strings.TrimRight(cfg.PublicURL, "/")
	if publicURL != "" {
		return fmt.Sprintf("%s/%s", publicURL, strings.TrimLeft(objectKey, "/"))
	}

	scheme := "http"
	if cfg.UseSSL {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/%s/%s", scheme, cfg.Endpoint, cfg.Bucket, strings.TrimLeft(objectKey, "/"))
}

func validateOSSRuntimeConfig(cfg config.OSSConfig) error {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return ErrOSSEndpointRequired
	}
	if strings.TrimSpace(cfg.AccessKey) == "" {
		return ErrOSSAccessKeyRequired
	}
	if strings.TrimSpace(cfg.SecretKey) == "" {
		return ErrOSSSecretKeyRequired
	}
	if strings.TrimSpace(cfg.Bucket) == "" {
		return ErrOSSBucketRequired
	}
	return nil
}

func ensureOSSReady() error {
	return ensureOSSStorageReady(ossStorageMedia)
}

func ensureAvatarOSSReady() error {
	return ensureOSSStorageReady(ossStorageAvatar)
}

func ensureReportOSSReady() error {
	return ensureOSSStorageReady(ossStorageReport)
}

func ensureOSSStorageReady(storage ossStorageKind) error {
	if err := validateOSSRuntimeConfig(getOSSConfig(storage)); err != nil {
		return err
	}
	if getOSSClient(storage) == nil {
		return ErrOSSNotInitialized
	}
	return nil
}

func getOSSConfig(storage ossStorageKind) config.OSSConfig {
	switch storage {
	case ossStorageAvatar:
		return config.C.OSS.Avatar
	case ossStorageReport:
		return config.C.OSS.Report
	default:
		return config.C.OSS.Media
	}
}

func getOSSClient(storage ossStorageKind) *minio.Client {
	runtime := getOSSRuntime(storage)
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	return runtime.client
}

func setOSSClient(storage ossStorageKind, client *minio.Client) {
	runtime := getOSSRuntime(storage)
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.client = client
}

func getOSSRuntime(storage ossStorageKind) *ossRuntime {
	switch storage {
	case ossStorageAvatar:
		return &avatarOSSRuntime
	case ossStorageReport:
		return &reportOSSRuntime
	default:
		return &mediaOSSRuntime
	}
}

func buildStorageObjectKey(cfg config.OSSConfig, objectKey string) string {
	normalizedKey := strings.TrimLeft(strings.TrimSpace(objectKey), "/")
	if dir := strings.Trim(cfg.StorageDir, "/"); dir != "" {
		return fmt.Sprintf("%s/%s", dir, normalizedKey)
	}
	return normalizedKey
}

func buildUserMediaObjectPrefix(userID int64) string {
	return buildStorageObjectKey(
		getOSSConfig(ossStorageMedia),
		fmt.Sprintf("user/%d/", userID),
	)
}

func isUserMediaObjectKey(userID int64, objectKey string) bool {
	normalized := strings.TrimLeft(strings.TrimSpace(objectKey), "/")
	return strings.HasPrefix(normalized, buildUserMediaObjectPrefix(userID))
}
