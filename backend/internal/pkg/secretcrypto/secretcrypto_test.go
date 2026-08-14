package secretcrypto

import (
	"encoding/base64"
	"testing"

	"github.com/askie/grix/backend/config"
)

func init() {
	config.C.Server.VoiceCryptoSecret = "unit-test-voice-secret"
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	plain := "sk-proj-abcdef1234567890"
	cipherText, err := Encrypt(plain)
	if err != nil {
		t.Fatalf("encrypt error: %v", err)
	}
	if cipherText == plain {
		t.Fatal("ciphertext must not equal plaintext")
	}
	got, err := Decrypt(cipherText)
	if err != nil {
		t.Fatalf("decrypt error: %v", err)
	}
	if got != plain {
		t.Fatalf("round-trip mismatch: got %q want %q", got, plain)
	}
}

func TestEncryptRandomNonce(t *testing.T) {
	plain := "same-secret-value"
	c1, _ := Encrypt(plain)
	c2, _ := Encrypt(plain)
	if c1 == c2 {
		t.Fatal("two encryptions of same plaintext must differ (random nonce)")
	}
}

func TestDecryptTampered(t *testing.T) {
	cipherText, _ := Encrypt("hello-world-key")
	buf, err := base64.RawStdEncoding.DecodeString(cipherText)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	buf[len(buf)-1] ^= 0xFF // 翻转密文最后一字节，破坏 GCM tag
	tampered := base64.RawStdEncoding.EncodeToString(buf)
	if _, err := Decrypt(tampered); err == nil {
		t.Fatal("tampered ciphertext must fail to decrypt")
	}
}

func TestEmptyString(t *testing.T) {
	c, err := Encrypt("")
	if err != nil || c != "" {
		t.Fatalf("empty encrypt: got %q err %v", c, err)
	}
	p, err := Decrypt("")
	if err != nil || p != "" {
		t.Fatalf("empty decrypt: got %q err %v", p, err)
	}
}

func TestHint(t *testing.T) {
	if got := Hint("sk-proj-abcd1234"); got != "1234" {
		t.Fatalf("hint long: got %q want %q", got, "1234")
	}
	if got := Hint("abc"); got != "abc" {
		t.Fatalf("hint short: got %q want %q", got, "abc")
	}
}
