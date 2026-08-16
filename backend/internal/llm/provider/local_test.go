package provider

import (
	"net/netip"
	"testing"
)

func TestLocalDisallowedIP(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true},
		{"::1", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		{"169.254.169.254", true}, // 云元数据
		{"100.64.0.1", true},      // CGNAT / Tailscale
		{"100.127.255.254", true},
		{"0.0.0.0", true},
		{"224.0.0.1", true},
		{"fe80::1", true},
		{"fc00::1", true}, // ULA 全段
		{"fd00::1", true},
		{"8.8.8.8", false},
		{"100.63.255.255", false}, // CGNAT 段之外
		{"100.128.0.1", false},
		{"1.1.1.1", false},
		{"2606:4700:4700::1111", false},
	}
	for _, tc := range cases {
		t.Run(tc.ip, func(t *testing.T) {
			got := localDisallowedIP(netip.MustParseAddr(tc.ip))
			if got != tc.want {
				t.Fatalf("localDisallowedIP(%s) = %v, want %v", tc.ip, got, tc.want)
			}
		})
	}
}

func TestLocalHTTPClientRejectsRedirect(t *testing.T) {
	if err := localHTTPClient.CheckRedirect(nil, nil); err == nil {
		t.Fatal("local client must not follow redirects")
	}
}
