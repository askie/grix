package secretcrypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/askie/grix/backend/config"
)

// BlindIndex 为敏感明文生成确定性盲索引（HMAC-SHA256 十六进制）。
// 用途：手机号等需要"加密存储但仍要支持等值查询/唯一约束"的字段——密文随机无法查询，
// 盲索引提供稳定指纹用于唯一索引与精确查号。空明文返回空串。
//
// 密钥与加密密钥做域隔离：blind 用 sha256("phone-blind:"+secret) 派生，
// 与 Encrypt 的 "voice-apikey:" 域不同，避免两者共用同一把密钥材料。
func BlindIndex(plain string) string {
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return ""
	}
	mac := hmac.New(sha256.New, deriveBlindKey())
	mac.Write([]byte(plain))
	return hex.EncodeToString(mac.Sum(nil))
}

func deriveBlindKey() []byte {
	secret := strings.TrimSpace(config.C.Server.VoiceCryptoSecret)
	if secret == "" {
		secret = strings.TrimSpace(config.C.JWT.Secret)
	}
	sum := sha256.Sum256([]byte("phone-blind:" + secret))
	return sum[:]
}
