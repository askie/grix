package agentapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	defaultPath   = "/v1/agent-api"
	defaultWSPath = "/ws"
)

func BuildEndpoint(domain, path, wsPath string, wsPort int, agentID int64) string {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		domain = fmt.Sprintf("ws://127.0.0.1:%d", wsPort)
	}
	domain = strings.TrimRight(domain, "/")

	path = normalizePath(path, defaultPath)
	wsPath = normalizePath(wsPath, defaultWSPath)
	return fmt.Sprintf("%s%s%s?agent_id=%d", domain, path, wsPath, agentID)
}

func GenerateAPIKey(agentID int64) (plain, hash, hint string, err error) {
	buf := make([]byte, 24)
	if _, err = rand.Read(buf); err != nil {
		return "", "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	plain = fmt.Sprintf("ak_%d_%s", agentID, token)
	hash = HashAPIKey(plain)
	hint = APIKeyHint(plain)
	return plain, hash, hint, nil
}

func HashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return hex.EncodeToString(sum[:])
}

func APIKeyHint(key string) string {
	k := strings.TrimSpace(key)
	if len(k) <= 8 {
		return k
	}
	return k[len(k)-8:]
}

func normalizePath(path string, fallback string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		path = fallback
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return strings.TrimRight(path, "/")
}
