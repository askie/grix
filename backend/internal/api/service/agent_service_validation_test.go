package service

import (
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/askie/grix/backend/config"
)

func TestValidateLocalEndpoint(t *testing.T) {
	originalConfig := config.C
	t.Cleanup(func() {
		config.C = originalConfig
	})

	allowed := []string{
		"http://8.8.8.8:11434",
		"https://203.0.113.10",
	}
	rejected := []string{
		"http://localhost:11434",
		"http://127.0.0.1:11434",
		"http://[::1]:11434",
		"http://10.0.0.5:11434",
		"http://172.16.1.2",
		"http://192.168.1.10",
		"http://169.254.169.254/latest/meta-data",
		"http://[fd00::1]:11434",
		"http://[fc00::1]:11434",
	}

	// SaaS 默认（开关关闭）：loopback / 链路本地 / 私网一律拒绝，公网 IP 放行。
	config.C.Security.AllowPrivateLocalEndpoint = false
	for _, endpoint := range allowed {
		if ec := validateLocalEndpoint(endpoint); ec != nil {
			t.Fatalf("strict mode: endpoint %q should be allowed, got %+v", endpoint, ec)
		}
	}
	for _, endpoint := range rejected {
		if ec := validateLocalEndpoint(endpoint); ec == nil {
			t.Fatalf("strict mode: endpoint %q should be rejected", endpoint)
		}
	}

	// 自托管（开关开启）：保持原有行为，仅允许 loopback / 私网，公网反而拒绝。
	config.C.Security.AllowPrivateLocalEndpoint = true
	selfHostedAllowed := []string{
		"http://localhost:11434",
		"http://127.0.0.1:11434",
		"http://[::1]:11434",
		"http://10.0.0.5:11434",
		"http://172.16.1.2",
		"http://192.168.1.10",
		"http://[fd00::1]:11434",
		"http://[fc00::1]:11434",
	}
	for _, endpoint := range selfHostedAllowed {
		if ec := validateLocalEndpoint(endpoint); ec != nil {
			t.Fatalf("self-hosted mode: endpoint %q should be allowed, got %+v", endpoint, ec)
		}
	}
	for _, endpoint := range allowed {
		if ec := validateLocalEndpoint(endpoint); ec == nil {
			t.Fatalf("self-hosted mode: endpoint %q should be rejected", endpoint)
		}
	}
	if ec := validateLocalEndpoint("http://example.com:11434"); ec == nil {
		t.Fatal("self-hosted mode: non-IP host should be rejected")
	}
}

func TestIsPrivateIP(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"10.1.2.3", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.0.1", true},
		{"fd00::1", true},
		{"fc00::1", true}, // ULA 是 fc00::/7，不是只有 fd 前缀
		{"fdff:ffff::1", true},
		{"8.8.8.8", false},
		{"169.254.0.1", false}, // 链路本地不属于私网段
		{"fe80::1", false},
		{"127.0.0.1", false}, // loopback 由调用方单独处理
	}
	for _, tc := range cases {
		if got := isPrivateIP(net.ParseIP(tc.ip)); got != tc.want {
			t.Fatalf("isPrivateIP(%q) = %v, want %v", tc.ip, got, tc.want)
		}
	}
}

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
