package service

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/askie/grix/backend/config"
)

func buildSessionMediaObjectPrefix(sessionID string) string {
	return buildStorageObjectKey(
		getOSSConfig(ossStorageMedia),
		fmt.Sprintf("media/%s/", sessionID),
	)
}

func isSessionMediaObjectKey(sessionID, objectKey string) bool {
	normalizedKey := normalizeObjectKey(objectKey)
	if normalizedKey == "" {
		return false
	}
	return strings.HasPrefix(normalizedKey, buildSessionMediaObjectPrefix(sessionID))
}

// resolveMediaObjectKey maps a URL to an object key in our media bucket, but
// ONLY when the URL actually points at our own media storage. External links
// embedded in a message (e.g. a user-supplied http://host/clip.mp4 or a tailnet
// http://100.64.0.1:8099/x.mp4) must NOT be re-hosted onto the media bucket:
// callers treat an empty result as "leave the URL untouched", so signing/
// cleanup never rewrites a foreign URL into a COS presigned link. This enforces
// the contract SignMediaURL documents but the generic resolver does not.
func resolveMediaObjectKey(mediaURL string) string {
	cfg := config.C.OSS.Media
	objectKey := resolveObjectKeyFromURLWithConfig(mediaURL, cfg)
	if objectKey == "" {
		return ""
	}
	if !mediaURLBelongsToMediaStorage(mediaURL, objectKey, cfg) {
		return ""
	}
	return objectKey
}

// mediaURLBelongsToMediaStorage reports whether mediaURL references our own
// media object storage rather than an arbitrary external host. A URL qualifies
// when any of these hold: it is a bare (relative) object key under the
// configured storage_dir; it starts with the configured public/CDN base URL;
// or its host is exactly our bucket's virtual-hosted subdomain, or the bare
// endpoint with our bucket named in the path (path-style). Everything else,
// including a third-party bucket that merely shares our region's endpoint
// suffix, is foreign.
func mediaURLBelongsToMediaStorage(mediaURL, objectKey string, cfg config.OSSConfig) bool {
	raw := strings.TrimSpace(mediaURL)
	if raw == "" {
		return false
	}

	// A relative value is already a bare object key, not an external link — the
	// storage_dir check only applies here, since a schemeless value can't carry
	// a foreign host.
	if !strings.Contains(raw, "://") {
		if dir := strings.Trim(strings.TrimSpace(cfg.StorageDir), "/"); dir != "" {
			return objectKey == dir || strings.HasPrefix(objectKey, dir+"/")
		}
		return true
	}

	// Configured public/CDN base URL for media access.
	if publicURL := strings.TrimRight(strings.TrimSpace(cfg.PublicURL), "/"); publicURL != "" {
		if strings.HasPrefix(raw, publicURL+"/") {
			return true
		}
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	endpoint := mediaEndpointHost(cfg.Endpoint)
	bucket := strings.ToLower(strings.Trim(strings.TrimSpace(cfg.Bucket), "/"))
	if host == "" || endpoint == "" || bucket == "" {
		return false
	}
	// Virtual-hosted access: host must be exactly our bucket's subdomain, not
	// just any bucket sharing the same regional endpoint (e.g. a third-party
	// bucket in the same COS/S3 region would otherwise also match).
	if host == bucket+"."+endpoint {
		return true
	}
	// Path-style access: host is the bare endpoint AND the path names our bucket.
	if host == endpoint {
		p := strings.Trim(parsed.EscapedPath(), "/")
		return p == bucket || strings.HasPrefix(p, bucket+"/")
	}
	return false
}

// mediaEndpointHost extracts the bare hostname from a configured OSS endpoint,
// which may be given as "host", "host:port", or a full "scheme://host" URL.
func mediaEndpointHost(endpoint string) string {
	trimmed := strings.ToLower(strings.TrimSpace(endpoint))
	if trimmed == "" {
		return ""
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "//" + trimmed
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}
