package security

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

func IsAllowedWebOrigin(r *http.Request, allowedOrigins string) bool {
	if r == nil {
		return false
	}

	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		// 无 Origin 头：非浏览器客户端（原生 App、服务端）不发 Origin，
		// 真正的鉴权由 JWT token 保障，此处放行不降低安全性。
		// 注意：浏览器发起的跨域请求必然携带 Origin，因此此分支不会被浏览器触发。
		return true
	}

	originURL, err := url.Parse(origin)
	if err != nil || originURL.Scheme == "" || originURL.Host == "" {
		return false
	}

	if sameHost(originURL.Host, r.Host) {
		return true
	}
	if sameLoopbackHost(originURL.Host, r.Host) {
		return true
	}

	for _, item := range strings.Split(allowedOrigins, ",") {
		allowed := strings.TrimRight(strings.TrimSpace(item), "/")
		if allowed == "" {
			continue
		}
		if strings.EqualFold(allowed, strings.TrimRight(origin, "/")) {
			return true
		}
	}

	return false
}

func sameHost(originHost, requestHost string) bool {
	return strings.EqualFold(strings.TrimSpace(originHost), strings.TrimSpace(requestHost))
}

func sameLoopbackHost(originHost, requestHost string) bool {
	originName := normalizeHost(originHost)
	requestName := normalizeHost(requestHost)
	if originName == "" || requestName == "" {
		return false
	}
	return isLoopbackHost(originName) && isLoopbackHost(requestName)
}

func normalizeHost(hostport string) string {
	trimmed := strings.TrimSpace(hostport)
	if trimmed == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(trimmed); err == nil {
		return strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]")
	}
	return strings.Trim(strings.ToLower(trimmed), "[]")
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
