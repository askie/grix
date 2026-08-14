package service

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/askie/grix/backend/config"
)

const avatarObjectDir = "avatars"

func buildUserAvatarObjectKey(userID int64) string {
	return buildUserAvatarObjectKeyForConfig(config.C.OSS.Avatar, userID)
}

func buildAgentAvatarObjectKey(agentID int64) string {
	return buildAgentAvatarObjectKeyForConfig(config.C.OSS.Avatar, agentID)
}

func buildUserAvatarObjectKeyForConfig(cfg config.OSSConfig, userID int64) string {
	return buildAvatarObjectKeyForConfig(cfg, fmt.Sprintf("%d.jpg", userID))
}

func buildAgentAvatarObjectKeyForConfig(cfg config.OSSConfig, agentID int64) string {
	return buildAvatarObjectKeyForConfig(cfg, fmt.Sprintf("agent_%d.jpg", agentID))
}

// buildAgentAvatarVersionedObjectKey builds an object key with a version suffix
// so each upload produces a distinct URL, defeating CDN and client-side caching.
func buildAgentAvatarVersionedObjectKey(agentID, versionTs int64) string {
	return buildAgentAvatarVersionedObjectKeyForConfig(config.C.OSS.Avatar, agentID, versionTs)
}

func buildAgentAvatarVersionedObjectKeyForConfig(cfg config.OSSConfig, agentID, versionTs int64) string {
	return buildAvatarObjectKeyForConfig(cfg, fmt.Sprintf("agent_%d_v%d.jpg", agentID, versionTs))
}

func buildAvatarObjectKeyForConfig(cfg config.OSSConfig, filename string) string {
	return buildStorageObjectKey(
		cfg,
		fmt.Sprintf("%s/%s", avatarObjectDir, strings.TrimSpace(filename)),
	)
}

func isUserAvatarObjectKey(userID int64, objectKey string) bool {
	return isUserAvatarObjectKeyForConfig(config.C.OSS.Avatar, userID, objectKey)
}

func isUserAvatarObjectKeyForConfig(cfg config.OSSConfig, userID int64, objectKey string) bool {
	normalizedKey := normalizeObjectKey(objectKey)
	if normalizedKey == "" {
		return false
	}
	return normalizedKey == buildUserAvatarObjectKeyForConfig(cfg, userID) ||
		strings.HasPrefix(normalizedKey, legacyUserAvatarObjectPrefixForConfig(cfg, userID))
}

func isAgentAvatarObjectKey(userID, agentID int64, objectKey string) bool {
	return isAgentAvatarObjectKeyForConfig(config.C.OSS.Avatar, userID, agentID, objectKey)
}

func isAgentAvatarObjectKeyForConfig(
	cfg config.OSSConfig,
	userID, agentID int64,
	objectKey string,
) bool {
	normalizedKey := normalizeObjectKey(objectKey)
	if normalizedKey == "" {
		return false
	}
	if normalizedKey == buildAgentAvatarObjectKeyForConfig(cfg, agentID) {
		return true
	}
	// Versioned keys: avatars/agent_{id}_v{ts}.jpg
	versionedPrefix := buildAvatarObjectKeyForConfig(cfg, fmt.Sprintf("agent_%d_v", agentID))
	if strings.HasPrefix(normalizedKey, versionedPrefix) {
		return true
	}
	return strings.HasPrefix(normalizedKey, legacyAgentAvatarObjectPrefixForConfig(cfg, userID, agentID))
}

func legacyUserAvatarObjectPrefix(userID int64) string {
	return legacyUserAvatarObjectPrefixForConfig(config.C.OSS.Avatar, userID)
}

func legacyAgentAvatarObjectPrefix(userID, agentID int64) string {
	return legacyAgentAvatarObjectPrefixForConfig(config.C.OSS.Avatar, userID, agentID)
}

func legacyUserAvatarObjectPrefixForConfig(cfg config.OSSConfig, userID int64) string {
	return buildLegacyAvatarObjectPrefixForConfig(cfg, fmt.Sprintf("user/%d/avatar", userID))
}

func legacyAgentAvatarObjectPrefixForConfig(cfg config.OSSConfig, userID, agentID int64) string {
	return buildLegacyAvatarObjectPrefixForConfig(cfg, fmt.Sprintf("agent/%d/%d/avatar", userID, agentID))
}

func buildLegacyAvatarObjectPrefixForConfig(cfg config.OSSConfig, path string) string {
	prefix := fmt.Sprintf("%s/", strings.Trim(path, "/"))
	if dir := strings.Trim(cfg.StorageDir, "/"); dir != "" {
		return fmt.Sprintf("%s/%s", dir, prefix)
	}
	return prefix
}

func resolveAvatarObjectKey(avatarURL string) string {
	return resolveObjectKeyFromURLWithConfig(avatarURL, config.C.OSS.Avatar)
}

func resolveObjectKeyFromURLWithConfig(avatarURL string, cfg config.OSSConfig) string {
	normalizedURL := strings.TrimSpace(avatarURL)
	if normalizedURL == "" {
		return ""
	}

	publicURL := strings.TrimRight(strings.TrimSpace(cfg.PublicURL), "/")
	if publicURL != "" {
		publicPrefix := publicURL + "/"
		if strings.HasPrefix(normalizedURL, publicPrefix) {
			return normalizeObjectKey(strings.TrimPrefix(normalizedURL, publicPrefix))
		}
	}

	if !strings.Contains(normalizedURL, "://") {
		return normalizeObjectKey(normalizedURL)
	}

	parsedURL, err := url.Parse(normalizedURL)
	if err != nil {
		return ""
	}

	path := normalizeObjectKey(parsedURL.Path)
	if path == "" {
		return ""
	}

	bucket := strings.Trim(cfg.Bucket, "/")
	if bucket != "" {
		bucketPrefix := bucket + "/"
		if strings.HasPrefix(path, bucketPrefix) {
			return strings.TrimPrefix(path, bucketPrefix)
		}
	}

	return path
}

func normalizeObjectKey(objectKey string) string {
	return strings.Trim(strings.TrimSpace(objectKey), "/")
}
