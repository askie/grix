package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/askie/grix/backend/config"
	"github.com/gin-gonic/gin"
)

func withGatewayAllowedOrigins(t *testing.T, origins string) {
	t.Helper()
	original := config.C.Server.AllowedWebOrigins
	config.C.Server.AllowedWebOrigins = origins
	t.Cleanup(func() { config.C.Server.AllowedWebOrigins = original })
}

func newGatewayTestContext(host, forwardedHost, forwardedProto string) *gin.Context {
	req := httptest.NewRequest("POST", "http://example.internal/v1/gateway/agents/1/provider", nil)
	req.Host = host
	if forwardedHost != "" {
		req.Header.Set("X-Forwarded-Host", forwardedHost)
	}
	if forwardedProto != "" {
		req.Header.Set("X-Forwarded-Proto", forwardedProto)
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	return c
}

func TestResolveGatewayBaseURLs_TrustsWhitelistedForwardedHost(t *testing.T) {
	withGatewayAllowedOrigins(t, "https://grix.dhf.pub,https://gb.grix.im")

	c := newGatewayTestContext("internal-service:8080", "grix.dhf.pub", "https")
	anthropic, openai := resolveGatewayBaseURLs(c)

	if anthropic != "https://grix.dhf.pub/anthropic/v1" {
		t.Fatalf("expected whitelisted forwarded host to be used, got %q", anthropic)
	}
	if openai != "https://grix.dhf.pub/openai/v1" {
		t.Fatalf("expected whitelisted forwarded host to be used, got %q", openai)
	}
}

func TestResolveGatewayBaseURLs_IgnoresUnknownForwardedHost(t *testing.T) {
	withGatewayAllowedOrigins(t, "https://grix.dhf.pub,https://gb.grix.im")

	// X-Forwarded-Host 不在白名单里（比如被伪造成 evil.com，或入口没设该头导致读到内部服务名），
	// 必须回退到 c.Request.Host，绝不能拿去拼下发地址。
	c := newGatewayTestContext("grix.dhf.pub", "evil.com", "https")
	anthropic, openai := resolveGatewayBaseURLs(c)

	if anthropic != "https://grix.dhf.pub/anthropic/v1" {
		t.Fatalf("expected fallback to request host, got %q", anthropic)
	}
	if openai != "https://grix.dhf.pub/openai/v1" {
		t.Fatalf("expected fallback to request host, got %q", openai)
	}
}

func TestResolveGatewayBaseURLs_WhitelistedForwardedHostWithPort(t *testing.T) {
	withGatewayAllowedOrigins(t, "https://grix.dhf.pub")

	c := newGatewayTestContext("internal-service:8080", "grix.dhf.pub:443", "https")
	anthropic, _ := resolveGatewayBaseURLs(c)

	if anthropic != "https://grix.dhf.pub:443/anthropic/v1" {
		t.Fatalf("expected whitelisted forwarded host (with port) to be used, got %q", anthropic)
	}
}

func withGatewayPort(t *testing.T, port int) {
	t.Helper()
	original := config.C.Gateway.Port
	config.C.Gateway.Port = port
	t.Cleanup(func() { config.C.Gateway.Port = original })
}

// 本地开发 api(27180) 和 gateway(27184) 是两个独立端口的进程，没有反代把它们合并到同一个
// Host 之下。没有可信 X-Forwarded-Host 时，兜底地址必须换成网关自己的端口，不能原样把
// 调用方（api 自己）的端口回给客户端，否则下发地址打到网关没注册的路由上就是裸404。
func TestResolveGatewayBaseURLs_LocalDevHostGetsGatewayPort(t *testing.T) {
	withGatewayAllowedOrigins(t, "https://grix.dhf.pub,https://gb.grix.im")
	withGatewayPort(t, 27184)

	c := newGatewayTestContext("127.0.0.1:27180", "", "")
	anthropic, openai := resolveGatewayBaseURLs(c)

	if anthropic != "http://127.0.0.1:27184/anthropic/v1" {
		t.Fatalf("expected local dev host to be rewritten to the gateway's own port, got %q", anthropic)
	}
	if openai != "http://127.0.0.1:27184/openai/v1" {
		t.Fatalf("expected local dev host to be rewritten to the gateway's own port, got %q", openai)
	}
}

// Host 不带端口（生产反代场景）时不应该被动到——localDevGatewayHost 只处理显式带端口的情况。
func TestResolveGatewayBaseURLs_HostWithoutPortUnchanged(t *testing.T) {
	withGatewayAllowedOrigins(t, "https://grix.dhf.pub")
	withGatewayPort(t, 27184)

	c := newGatewayTestContext("grix.dhf.pub", "", "")
	anthropic, _ := resolveGatewayBaseURLs(c)

	if anthropic != "http://grix.dhf.pub/anthropic/v1" {
		t.Fatalf("expected host without port to stay unchanged, got %q", anthropic)
	}
}
