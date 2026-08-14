package service

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeAgentName(t *testing.T) {
	t.Run("trims spaces", func(t *testing.T) {
		got, ec := normalizeAgentName("  demo-agent  ")
		if ec != nil {
			t.Fatalf("unexpected error: %+v", ec)
		}
		if got != "demo-agent" {
			t.Fatalf("expected trimmed name demo-agent, got %q", got)
		}
	})

	t.Run("rejects empty after trim", func(t *testing.T) {
		_, ec := normalizeAgentName("   ")
		if ec == nil {
			t.Fatal("expected error for empty name")
		}
		if ec.BizCode != 10003 {
			t.Fatalf("expected biz code 10003, got %d", ec.BizCode)
		}
	})

	t.Run("rejects too long name", func(t *testing.T) {
		tooLong := strings.Repeat("a", maxAgentNameRunes+1)
		_, ec := normalizeAgentName(tooLong)
		if ec == nil {
			t.Fatal("expected error for too long name")
		}
		if ec.BizCode != 10003 {
			t.Fatalf("expected biz code 10003, got %d", ec.BizCode)
		}
	})

	t.Run("rejects control characters", func(t *testing.T) {
		_, ec := normalizeAgentName("demo\nagent")
		if ec == nil {
			t.Fatal("expected error for control char")
		}
		if ec.BizCode != 10003 {
			t.Fatalf("expected biz code 10003, got %d", ec.BizCode)
		}
	})
}

func TestInternalAgentErrHidesCause(t *testing.T) {
	ec := internalAgentErr("创建 Agent 失败", errors.New("dial tcp 1.2.3.4:5432: i/o timeout"))
	if ec == nil {
		t.Fatal("expected non-nil error code")
	}
	if ec.Msg != "创建 Agent 失败" {
		t.Fatalf("unexpected message: %s", ec.Msg)
	}
	if strings.Contains(ec.Msg, "timeout") {
		t.Fatalf("message should not expose internal cause: %s", ec.Msg)
	}
}

func TestNormalizeAgentIntroduction(t *testing.T) {
	t.Run("trims and normalizes line breaks", func(t *testing.T) {
		got, ec := normalizeAgentIntroduction("  hello\r\nworld  ")
		if ec != nil {
			t.Fatalf("unexpected error: %+v", ec)
		}
		if got != "hello\nworld" {
			t.Fatalf("expected normalized introduction, got %q", got)
		}
	})

	t.Run("rejects too long introduction", func(t *testing.T) {
		_, ec := normalizeAgentIntroduction(strings.Repeat("a", maxAgentIntroductionRunes+1))
		if ec == nil {
			t.Fatal("expected error for too long introduction")
		}
		if ec.BizCode != 10003 {
			t.Fatalf("expected biz code 10003, got %d", ec.BizCode)
		}
	})

	t.Run("rejects invalid control chars", func(t *testing.T) {
		_, ec := normalizeAgentIntroduction("hello\x00world")
		if ec == nil {
			t.Fatal("expected error for invalid control char")
		}
		if ec.BizCode != 10003 {
			t.Fatalf("expected biz code 10003, got %d", ec.BizCode)
		}
	})
}
