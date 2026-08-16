package service

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"sync"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/minio/minio-go/v7"
)

// mediaURLInTextPattern matches an absolute http(s) URL embedded in free-form
// message text. It stops at whitespace and the markdown delimiters that wrap a
// link destination (`<`, `>`, `)`, `]`) and at quotes, so the bare URL inside
// `![image](<url>)` is captured without its surrounding syntax.
var mediaURLInTextPattern = regexp.MustCompile(`https?://[^\s<>)\]"']+`)

// mediaURLSignFunc signs a single URL. It is injectable so that the inline copy
// in a message's content and the structured copy in its extra can share one
// memo and therefore resolve to byte-identical signed URLs.
type mediaURLSignFunc func(string) string

// MediaPresignedGetTTL controls how long a signed media GET URL stays valid.
// Media objects live in a private bucket, so every outbound message carries a
// freshly-signed, time-limited URL instead of a bare public link.
const MediaPresignedGetTTL = 7 * 24 * time.Hour

// BuildMediaPresignedGetURL returns a time-limited signed GET URL for a media
// object key. A non-positive ttl falls back to MediaPresignedGetTTL.
func BuildMediaPresignedGetURL(objectKey string, ttl time.Duration) (string, error) {
	if err := ensureOSSReady(); err != nil {
		return "", err
	}
	if ttl <= 0 {
		ttl = MediaPresignedGetTTL
	}
	presigned, err := getOSSClient(ossStorageMedia).PresignedGetObject(
		context.Background(),
		getOSSConfig(ossStorageMedia).Bucket,
		objectKey,
		ttl,
		nil,
	)
	if err != nil {
		return "", err
	}
	return presigned.String(), nil
}

// SignMediaURL converts a stored media URL into a time-limited signed GET URL.
// It is best-effort: a URL that does not point at the media bucket, or a
// signing failure, returns the input unchanged so message delivery never
// blocks. Re-signing an already-signed URL is safe — the object key is derived
// from the path and any existing query is ignored.
func SignMediaURL(mediaURL string) string {
	objectKey := resolveMediaObjectKey(mediaURL)
	if objectKey == "" {
		return mediaURL
	}
	// 落地后复核：presign 不绑定实际上传的 Content-Type/大小，签名前 StatObject
	// 校验白名单与大小上限，不合格则不签名（私有桶裸 URL 不可访问，等于拒绝渲染）。
	if !mediaObjectAllowedForSigning(objectKey) {
		warnMediaSigning("refuse to sign media object key=%s: content type not allowed or size exceeds limit", objectKey)
		return mediaURL
	}
	signed, err := BuildMediaPresignedGetURL(objectKey, MediaPresignedGetTTL)
	if err != nil || signed == "" {
		return mediaURL
	}
	return signed
}

// defaultMediaMaxUploadBytes 是 security.media_max_upload_bytes 未配置时的默认上限。
const defaultMediaMaxUploadBytes = 100 * 1024 * 1024

// mediaSignCheckTTL 签名前复核结果的缓存时长，避免每条消息渲染都打一次 StatObject。
const mediaSignCheckTTL = 5 * time.Minute

type mediaSignCheckResult struct {
	allowed bool
	expires time.Time
}

var mediaSignCheckCache sync.Map // objectKey -> mediaSignCheckResult

// mediaStatObject 抽象出 StatObject 以便单测注入。
var mediaStatObject = func(ctx context.Context, bucket, objectKey string) (minio.ObjectInfo, error) {
	return getOSSClient(ossStorageMedia).StatObject(ctx, bucket, objectKey, minio.StatObjectOptions{})
}

func warnMediaSigning(format string, args ...interface{}) {
	if logger.L != nil {
		logger.L.Warnf(format, args...)
	}
}

// mediaObjectAllowedForSigning 复核媒体对象的实际 Content-Type 与大小，
// 结果按 objectKey 短 TTL 缓存（消息列表渲染会反复签名同一对象）。
func mediaObjectAllowedForSigning(objectKey string) bool {
	if cached, ok := mediaSignCheckCache.Load(objectKey); ok {
		if entry, ok := cached.(mediaSignCheckResult); ok && time.Now().Before(entry.expires) {
			return entry.allowed
		}
	}
	allowed := checkMediaObjectForSigning(objectKey)
	mediaSignCheckCache.Store(objectKey, mediaSignCheckResult{
		allowed: allowed,
		expires: time.Now().Add(mediaSignCheckTTL),
	})
	return allowed
}

func checkMediaObjectForSigning(objectKey string) bool {
	if getOSSClient(ossStorageMedia) == nil {
		// OSS 未初始化时签名路径本身会失败回落，这里不额外拦截。
		return true
	}
	cfg := getOSSConfig(ossStorageMedia)
	info, err := mediaStatObject(context.Background(), cfg.Bucket, objectKey)
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			warnMediaSigning("media object missing key=%s", objectKey)
			return false
		}
		// 存储暂时性故障不阻断既有消息渲染，仅记录日志。
		warnMediaSigning("stat media object failed key=%s: %v", objectKey, err)
		return true
	}
	if !mediaContentTypeAllowedForSigning(info.ContentType) {
		return false
	}
	maxBytes := config.C.Security.MediaMaxUploadBytes
	if maxBytes <= 0 {
		maxBytes = defaultMediaMaxUploadBytes
	}
	return info.Size <= maxBytes
}

// SignMessageMedia signs every media URL carried by an outbound message, in both
// the free-form content and the structured extra blob. The two halves share one
// memo, so the same source URL becomes the exact same signed URL in both places.
// That byte-identity lets the client de-duplicate the inline image (embedded in
// content as `![image](<url>)`) against the structured attachment grid instead of
// rendering both — and guarantees the inline copy is itself accessible (private
// bucket) for forwarding, agent feeding and copy-paste.
func SignMessageMedia(content string, extra json.RawMessage) (string, json.RawMessage) {
	return signMessageMedia(content, extra, SignMediaURL)
}

// signMessageMedia signs content and extra with a shared, memoized signer built
// on top of baseSign. The memo guarantees a source URL maps to one signed value
// across both halves. baseSign is injectable for tests.
func signMessageMedia(content string, extra json.RawMessage, baseSign mediaURLSignFunc) (string, json.RawMessage) {
	memo := make(map[string]string)
	sign := func(u string) string {
		if u == "" {
			return u
		}
		if cached, ok := memo[u]; ok {
			return cached
		}
		signed := baseSign(u)
		memo[u] = signed
		return signed
	}
	return signMediaURLsInContent(content, sign), signMediaURLsInExtra(extra, sign)
}

// signMediaURLsInContent rewrites every media-bucket URL embedded in free-form
// message text into a signed GET URL. URLs that do not point at the media bucket
// are returned unchanged by the signer, so unrelated links stay intact.
func signMediaURLsInContent(content string, sign mediaURLSignFunc) string {
	if content == "" {
		return content
	}
	return mediaURLInTextPattern.ReplaceAllStringFunc(content, sign)
}

// SignMediaURLsInExtra rewrites media URLs inside a message extra blob into
// signed GET URLs. Kept for callers that only carry an extra blob.
func SignMediaURLsInExtra(extra json.RawMessage) json.RawMessage {
	return signMediaURLsInExtra(extra, SignMediaURL)
}

// signMediaURLsInExtra signs the top-level media_url and every
// attachments[].media_url using the provided signer. Best-effort: on
// parse/marshal failure the original bytes are returned untouched.
func signMediaURLsInExtra(extra json.RawMessage, sign mediaURLSignFunc) json.RawMessage {
	if len(bytes.TrimSpace(extra)) == 0 {
		return extra
	}

	var envelope map[string]any
	if err := json.Unmarshal(extra, &envelope); err != nil {
		return extra
	}

	changed := false
	if top, ok := envelope["media_url"].(string); ok && top != "" {
		if signed := sign(top); signed != top {
			envelope["media_url"] = signed
			changed = true
		}
	}
	if list, ok := envelope["attachments"].([]any); ok {
		for _, item := range list {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if u, ok := obj["media_url"].(string); ok && u != "" {
				if signed := sign(u); signed != u {
					obj["media_url"] = signed
					changed = true
				}
			}
		}
	}

	if !changed {
		return extra
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return extra
	}
	return encoded
}
