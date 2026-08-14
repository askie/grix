package webhook

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"strings"

	"github.com/askie/grix/backend/config"
)

func generateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "whk_" + hex.EncodeToString(buf), nil
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(h[:])
}

func tokenPrefix(token string) string {
	token = strings.TrimSpace(token)
	if len(token) <= 12 {
		return token
	}
	return token[:12]
}

func encryptToken(token string) (string, error) {
	key := deriveWebhookCryptoKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, nonce, []byte(token), nil)
	out := append(nonce, sealed...)
	return base64.RawStdEncoding.EncodeToString(out), nil
}

func decryptToken(cipherText string) (string, error) {
	buf, err := base64.RawStdEncoding.DecodeString(strings.TrimSpace(cipherText))
	if err != nil {
		return "", err
	}
	key := deriveWebhookCryptoKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(buf) <= gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce := buf[:gcm.NonceSize()]
	payload := buf[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, payload, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func deriveWebhookCryptoKey() []byte {
	secret := strings.TrimSpace(config.C.Server.WebhookTokenSecret)
	if secret == "" {
		secret = strings.TrimSpace(config.C.JWT.Secret)
	}
	sum := sha256.Sum256([]byte("webhook-token:" + secret))
	return sum[:]
}
