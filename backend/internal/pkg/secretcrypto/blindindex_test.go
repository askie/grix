package secretcrypto

import "testing"

func TestBlindIndex(t *testing.T) {
	// 空串返回空
	if got := BlindIndex(""); got != "" {
		t.Fatalf("empty should map to empty, got %q", got)
	}
	if got := BlindIndex("   "); got != "" {
		t.Fatalf("blank should map to empty, got %q", got)
	}

	// 确定性：同输入同输出，且为 64 位十六进制（HMAC-SHA256）
	a := BlindIndex("+8613800138000")
	b := BlindIndex("+8613800138000")
	if a != b {
		t.Fatalf("not deterministic: %q vs %q", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("expected 64 hex chars, got %d (%q)", len(a), a)
	}

	// 不同号码不同指纹；且指纹里不含原文
	if BlindIndex("+8613800138001") == a {
		t.Fatalf("different phones collided")
	}
	if a == "+8613800138000" {
		t.Fatalf("blind index must not equal plaintext")
	}
}
