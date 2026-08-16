package service

import (
	"errors"
	"path/filepath"
	"strings"
)

// ErrMediaUploadContentType 声明的 Content-Type 不在媒体上传白名单内，
// 或与对象 key 扩展名明显不匹配。
var ErrMediaUploadContentType = errors.New("media upload content type not allowed")

// mediaUploadAllowedContentTypes 是媒体上传共用的 Content-Type 白名单
// （presign 声明校验 + 签名下发前 StatObject 复核同一口径）：
// 图片/视频/音频/常见文档。显式排除 text/html、image/svg+xml、application/xhtml+xml
// 等可在媒体域名上直接渲染执行的类型，防止存储型 XSS。
var mediaUploadAllowedContentTypes = map[string]struct{}{
	"image/jpeg": {},
	"image/png":  {},
	"image/webp": {},
	"image/gif":  {},
	"image/bmp":  {},
	"image/heic": {},
	"image/heif": {},

	"video/mp4":        {},
	"video/quicktime":  {},
	"video/webm":       {},
	"video/x-matroska": {},
	"video/x-msvideo":  {},
	"video/x-m4v":      {},
	"video/3gpp":       {},
	"video/3gpp2":      {},

	"audio/mpeg":  {},
	"audio/mp4":   {},
	"audio/x-m4a": {},
	"audio/aac":   {},
	"audio/ogg":   {},
	"audio/wav":   {},
	"audio/x-wav": {},
	"audio/flac":  {},
	"audio/webm":  {},
	"audio/amr":   {},

	"application/pdf":    {},
	"application/msword": {},
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": {},
	"application/vnd.ms-excel": {},
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         {},
	"application/vnd.ms-powerpoint":                                             {},
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": {},
	"text/plain":       {},
	"text/markdown":    {},
	"text/csv":         {},
	"application/json": {},
	"application/xml":  {},
	"text/xml":         {},

	"application/zip":              {},
	"application/x-zip-compressed": {},
	"application/x-rar-compressed": {},
	"application/vnd.rar":          {},
	"application/x-7z-compressed":  {},
	"application/x-tar":            {},
	"application/gzip":             {},
	"application/x-gzip":           {},
}

// mediaUploadBlockedExts 无论声明什么 Content-Type 都拒绝的扩展名：
// 这类文件按内容渲染即可执行脚本，不允许进入媒体桶。
var mediaUploadBlockedExts = map[string]struct{}{
	"html":  {},
	"htm":   {},
	"svg":   {},
	"xhtml": {},
	"js":    {},
	"mjs":   {},
}

// mediaUploadExtMIMEMajor 已知媒体扩展名对应的 MIME 大类。扩展名与声明的
// Content-Type 大类明显不匹配（如 .png 配 text/plain）时拒绝。
var mediaUploadExtMIMEMajor = map[string]string{
	"jpg": "image", "jpeg": "image", "png": "image", "gif": "image",
	"webp": "image", "bmp": "image", "heic": "image", "heif": "image",
	"mp4": "video", "mov": "video", "m4v": "video", "webm": "video",
	"mkv": "video", "avi": "video", "3gp": "video",
	"mp3": "audio", "m4a": "audio", "aac": "audio", "ogg": "audio",
	"wav": "audio", "flac": "audio", "amr": "audio",
}

// normalizeMediaContentType 归一化 Content-Type：小写、去参数（如 "; charset=utf-8"）。
func normalizeMediaContentType(contentType string) string {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	return ct
}

// validateMediaUploadContentType 在 presign 时校验声明的 Content-Type：
// 必须在白名单内，且与对象 key 扩展名不明显冲突。
func validateMediaUploadContentType(filename, contentType string) error {
	ct := normalizeMediaContentType(contentType)
	if ct == "" {
		return ErrMediaUploadContentType
	}
	if _, ok := mediaUploadAllowedContentTypes[ct]; !ok {
		return ErrMediaUploadContentType
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(strings.TrimSpace(filename)), "."))
	if _, blocked := mediaUploadBlockedExts[ext]; blocked {
		return ErrMediaUploadContentType
	}
	if major, known := mediaUploadExtMIMEMajor[ext]; known && !strings.HasPrefix(ct, major+"/") {
		return ErrMediaUploadContentType
	}
	return nil
}

// mediaContentTypeAllowedForSigning 判断落地后复核出的实际 Content-Type 是否允许
// 签名下发。历史对象 / 客户端 PUT 未带 Content-Type 时桶内会落成
// application/octet-stream 或空值，按通用二进制放行（浏览器不会按 HTML 渲染）。
func mediaContentTypeAllowedForSigning(contentType string) bool {
	ct := normalizeMediaContentType(contentType)
	if ct == "" || ct == "application/octet-stream" || ct == "binary/octet-stream" {
		return true
	}
	_, ok := mediaUploadAllowedContentTypes[ct]
	return ok
}
