package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/askie/grix/backend/config"
)

const (
	qrLoginScheme = "grix"
	qrLoginHost   = "auth"
	qrLoginPath   = "/qr-login"
)

// normalizeQRLoginRegion 归一化区域标识：与注册逻辑一致，除 cn 外一律 global。
func normalizeQRLoginRegion(raw string) string {
	if strings.EqualFold(strings.TrimSpace(raw), "cn") {
		return "cn"
	}
	return "global"
}

// qrLoginDeployRegion 当前部署区域（cn / global），来自 server.region 配置。
func qrLoginDeployRegion() string {
	return normalizeQRLoginRegion(config.C.Server.Region)
}

func generateQRLoginToken(byteLen int) (string, error) {
	if byteLen <= 0 {
		return "", errors.New("invalid token length")
	}
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashQRLoginToken(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}

func buildQRLoginText(sessionID, qrToken, region string) string {
	v := url.Values{}
	v.Set("sid", strings.TrimSpace(sessionID))
	v.Set("qt", strings.TrimSpace(qrToken))
	v.Set("rg", normalizeQRLoginRegion(region))
	return fmt.Sprintf("%s://%s%s?%s", qrLoginScheme, qrLoginHost, qrLoginPath, v.Encode())
}

// parseQRLoginText 解析二维码文本，返回 sessionID、qrToken 与二维码携带的区域标识。
// region 为空表示旧版二维码未携带区域信息。
func parseQRLoginText(raw string) (sessionID, qrToken, region string, err error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", "", ErrQRLoginInvalidCode
	}

	uri, parseErr := url.Parse(trimmed)
	if parseErr != nil {
		return "", "", "", ErrQRLoginInvalidCode
	}
	if !strings.EqualFold(uri.Scheme, qrLoginScheme) ||
		!strings.EqualFold(uri.Host, qrLoginHost) ||
		uri.Path != qrLoginPath {
		return "", "", "", ErrQRLoginInvalidCode
	}

	sessionID = strings.TrimSpace(uri.Query().Get("sid"))
	qrToken = strings.TrimSpace(uri.Query().Get("qt"))
	region = strings.TrimSpace(uri.Query().Get("rg"))
	if sessionID == "" || qrToken == "" {
		return "", "", "", ErrQRLoginInvalidCode
	}
	return sessionID, qrToken, region, nil
}
