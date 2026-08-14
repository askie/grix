package webhook

import (
	"testing"

	"github.com/askie/grix/backend/config"
)

func TestEncryptDecryptToken(t *testing.T) {
	config.C.Server.WebhookTokenSecret = "webhook-secret-a"
	config.C.JWT.Secret = "jwt-secret-a"
	plain := "whk_test_token_123"
	cipherText, err := encryptToken(plain)
	if err != nil {
		t.Fatalf("encrypt error: %v", err)
	}
	out, err := decryptToken(cipherText)
	if err != nil {
		t.Fatalf("decrypt error: %v", err)
	}
	if out != plain {
		t.Fatalf("decrypt mismatch got=%q want=%q", out, plain)
	}
}

func TestEncryptFallbackToJWTSecret(t *testing.T) {
	config.C.Server.WebhookTokenSecret = ""
	config.C.JWT.Secret = "jwt-secret-fallback"
	plain := "whk_test_token_456"
	cipherText, err := encryptToken(plain)
	if err != nil {
		t.Fatalf("encrypt error: %v", err)
	}
	out, err := decryptToken(cipherText)
	if err != nil {
		t.Fatalf("decrypt error: %v", err)
	}
	if out != plain {
		t.Fatalf("decrypt mismatch got=%q want=%q", out, plain)
	}
}
