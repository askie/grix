// Package secretcrypto 提供可复用的对称加密工具，用于在数据库中安全存储
// 用户自带的敏感凭证（如语音大模型 BYOK 的 API key）。
//
// 加密算法：AES-256-GCM（随机 nonce，base64 输出）。
// 密钥来源：一把固定统一的服务端密钥，由 sha256("voice-apikey:" + secret) 派生，
// secret 取 config.C.Server.VoiceCryptoSecret，为空时回退 config.C.JWT.Secret。
package secretcrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"strings"

	"github.com/askie/grix/backend/config"
)

// ErrCipherTooShort 表示密文长度不足，无法解密。
var ErrCipherTooShort = errors.New("secretcrypto: ciphertext too short")

// Encrypt 用固定统一密钥加密明文，返回 base64 密文。空明文返回空串。
func Encrypt(plain string) (string, error) {
	return encryptWithKey(plain, deriveKey())
}

// Decrypt 解密 base64 密文，返回明文。空密文返回空串。
func Decrypt(cipherText string) (string, error) {
	return decryptWithKey(cipherText, deriveKey())
}

// EncryptPay 用支付通道专属密钥加密明文（域与语音 BYOK 隔离），用于塘主后台
// 录入的支付宝/PayPal 等商户凭证落库。密钥取 config.C.Server.PayCryptoSecret，
// 为空时回退 JWT secret（与 Encrypt 同样的兜底策略，但派生域不同）。
func EncryptPay(plain string) (string, error) {
	return encryptWithKey(plain, derivePayKey())
}

// DecryptPay 解密支付通道专属密钥加密的密文。
func DecryptPay(cipherText string) (string, error) {
	return decryptWithKey(cipherText, derivePayKey())
}

// Hint 返回明文末 4 位，用于在 API 中展示而不泄露完整密钥。
func Hint(plain string) string {
	plain = strings.TrimSpace(plain)
	if len(plain) <= 4 {
		return plain
	}
	return plain[len(plain)-4:]
}

func encryptWithKey(plain string, key []byte) (string, error) {
	if plain == "" {
		return "", nil
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, nonce, []byte(plain), nil)
	out := append(nonce, sealed...)
	return base64.RawStdEncoding.EncodeToString(out), nil
}

func decryptWithKey(cipherText string, key []byte) (string, error) {
	cipherText = strings.TrimSpace(cipherText)
	if cipherText == "" {
		return "", nil
	}
	buf, err := base64.RawStdEncoding.DecodeString(cipherText)
	if err != nil {
		return "", err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	if len(buf) <= gcm.NonceSize() {
		return "", ErrCipherTooShort
	}
	nonce := buf[:gcm.NonceSize()]
	payload := buf[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, payload, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func deriveKey() []byte {
	secret := strings.TrimSpace(config.C.Server.VoiceCryptoSecret)
	if secret == "" {
		secret = strings.TrimSpace(config.C.JWT.Secret)
	}
	sum := sha256.Sum256([]byte("voice-apikey:" + secret))
	return sum[:]
}

func derivePayKey() []byte {
	secret := strings.TrimSpace(config.C.Server.PayCryptoSecret)
	if secret == "" {
		secret = strings.TrimSpace(config.C.JWT.Secret)
	}
	sum := sha256.Sum256([]byte("pay-credential:" + secret))
	return sum[:]
}
