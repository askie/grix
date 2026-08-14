// Package tailnet 提供 Tailscale 网络相关的工具函数。
package tailnet

import (
	"net"
	"strings"
)

// tailnetCIDRs 是 Tailscale 使用的 IP 段。
// 100.64.0.0/10 是 CGNAT 段（IPv4），fd7a:115c:a1e0::/48 是 Tailscale IPv6 段。
var tailnetCIDRs = func() []*net.IPNet {
	cidrs := []string{"100.64.0.0/10", "fd7a:115c:a1e0::/48"}
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err == nil {
			nets = append(nets, ipNet)
		}
	}
	return nets
}()

// IsTailnetIP 判断给定 IP 字符串是否属于 Tailscale 网络段。
func IsTailnetIP(ipStr string) bool {
	ipStr = strings.TrimSpace(ipStr)
	if ipStr == "" {
		return false
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, cidr := range tailnetCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// SameTailnet 判断两个 IP 是否都在 Tailscale 网络中（即可以直连）。
func SameTailnet(ipA, ipB string) bool {
	return IsTailnetIP(ipA) && IsTailnetIP(ipB)
}
