// Package clientip 从 HTTP 请求解析客户端真实 IP，用于封禁等安全判定。
// 线上流量经过负载均衡/反向代理，RemoteAddr 是代理地址，
// 真实来源要按 X-Forwarded-For → X-Real-IP → RemoteAddr 的顺序取。
// 适用于非 gin 的原生 http.Request（如 WS 握手）；gin 侧请继续用 c.ClientIP()。
package clientip

import (
	"net"
	"net/http"
	"strings"
)

// FromRequest 返回客户端真实 IP 字符串；解析不出时返回空串。
//
// X-Forwarded-For 取最右侧的合法公网 IP：该头由每一跳代理向右追加自己观测到的
// 上一跳地址，客户端只能在自己一侧（最左）伪造/塞入任意内容，但无法覆盖可信入口
// 自己追加在最右的那一跳，因此安全判定必须从右往左取，不能取最左（可被伪造绕过封禁）。
// 全为内网时回退最右侧合法 IP，兼容内网直连/开发环境。
func FromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	if ip := fromForwardedFor(r.Header.Get("X-Forwarded-For")); ip != "" {
		return ip
	}
	if ip := parseIP(r.Header.Get("X-Real-IP")); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return parseIP(r.RemoteAddr)
	}
	return parseIP(host)
}

func fromForwardedFor(header string) string {
	if strings.TrimSpace(header) == "" {
		return ""
	}
	parts := strings.Split(header, ",")
	rightmostValid := ""
	for i := len(parts) - 1; i >= 0; i-- {
		ip := parseIP(parts[i])
		if ip == "" {
			continue
		}
		if rightmostValid == "" {
			rightmostValid = ip
		}
		if !isPrivateIP(ip) {
			return ip
		}
	}
	return rightmostValid
}

func parseIP(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	// 兼容偶发的 "ip:port" 形式
	if host, _, err := net.SplitHostPort(trimmed); err == nil {
		trimmed = host
	}
	parsed := net.ParseIP(trimmed)
	if parsed == nil {
		return ""
	}
	return parsed.String()
}

func isPrivateIP(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return true
	}
	return parsed.IsPrivate() || parsed.IsLoopback() || parsed.IsLinkLocalUnicast() || parsed.IsUnspecified()
}
