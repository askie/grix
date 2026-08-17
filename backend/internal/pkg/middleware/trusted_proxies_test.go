package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// serveWithRemote 用指定 RemoteAddr / X-Forwarded-For 发起一次请求，返回响应记录器。
func serveWithRemote(r *gin.Engine, remoteAddr, xff string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.RemoteAddr = remoteAddr
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func newClientIPEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	ApplyTrustedProxies(r)
	r.GET("/probe", func(c *gin.Context) { c.String(http.StatusOK, c.ClientIP()) })
	return r
}

// 伪造 XFF 的安全语义：不可信对端（公网直连）伪造 X-Forwarded-For 不得改变 ClientIP 取值。
func TestApplyTrustedProxies_ForgedXFFIgnored(t *testing.T) {
	r := newClientIPEngine(t)
	w := serveWithRemote(r, "203.0.113.10:12345", "1.2.3.4")
	if got := w.Body.String(); got != "203.0.113.10" {
		t.Fatalf("ClientIP = %q, want remote addr %q (forged XFF must be ignored)", got, "203.0.113.10")
	}
}

// 可信对端（私网 LB/ingress）传来的 XFF 仍应被采纳，保证正常部署下 ClientIP 取到真实客户端。
func TestApplyTrustedProxies_TrustedProxyHonorsXFF(t *testing.T) {
	r := newClientIPEngine(t)
	w := serveWithRemote(r, "10.0.0.1:12345", "198.51.100.7")
	if got := w.Body.String(); got != "198.51.100.7" {
		t.Fatalf("ClientIP = %q, want XFF value %q from trusted proxy", got, "198.51.100.7")
	}
}

// InternalOnly（/metrics 内网限定）：公网对端伪造私网 XFF 不得拿到 200。
func TestInternalOnly_ForgedXFFCannotBypass(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	ApplyTrustedProxies(r)
	r.GET("/probe", InternalOnly(), func(c *gin.Context) { c.Status(http.StatusOK) })

	w := serveWithRemote(r, "203.0.113.10:12345", "10.1.2.3")
	if w.Code != http.StatusForbidden {
		t.Fatalf("forged private XFF from untrusted remote: status = %d, want 403", w.Code)
	}

	// 真实私网对端（无 XFF）正常放行。
	w = serveWithRemote(r, "192.168.1.10:8080", "")
	if w.Code != http.StatusOK {
		t.Fatalf("private remote: status = %d, want 200", w.Code)
	}
}

// 限流取值：RateLimitByIP 以 ClientIP 为 key，伪造 XFF 不得污染限流键。
// 不可信对端连发两次、每次伪造不同 XFF，两次取到的限流键必须相同（即按真实对端限流）。
func TestRateLimitKey_NotForgedByXFF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	ApplyTrustedProxies(r)
	var keys []string
	keyFn := func(c *gin.Context) string { return c.ClientIP() } // 与 RateLimitByIP 相同的取值逻辑
	r.GET("/probe", func(c *gin.Context) {
		keys = append(keys, keyFn(c))
		c.Status(http.StatusOK)
	})

	for _, xff := range []string{"1.1.1.1", "2.2.2.2"} {
		req := httptest.NewRequest(http.MethodGet, "/probe", nil)
		req.RemoteAddr = "203.0.113.10:12345"
		req.Header.Set("X-Forwarded-For", xff)
		r.ServeHTTP(httptest.NewRecorder(), req)
	}
	if len(keys) != 2 || keys[0] != "203.0.113.10" || keys[1] != "203.0.113.10" {
		t.Fatalf("rate limit keys = %v, want both %q (forged XFF must not change the key)", keys, "203.0.113.10")
	}
}

// 环境变量覆盖：AIBOT_TRUSTED_PROXIES 显式指定后，默认私网段不再可信。
func TestApplyTrustedProxies_EnvOverride(t *testing.T) {
	t.Setenv(TrustedProxiesEnv, "192.0.2.0/24")
	r := newClientIPEngine(t)

	// 默认基线里的 10.0.0.1 已被覆盖，不再可信，XFF 被忽略。
	w := serveWithRemote(r, "10.0.0.1:12345", "198.51.100.7")
	if got := w.Body.String(); got != "10.0.0.1" {
		t.Fatalf("after override, ClientIP = %q, want remote addr %q", got, "10.0.0.1")
	}

	// 覆盖网段内的对端可信，XFF 被采纳。
	w = serveWithRemote(r, "192.0.2.5:12345", "198.51.100.7")
	if got := w.Body.String(); got != "198.51.100.7" {
		t.Fatalf("after override, ClientIP = %q, want XFF value %q", got, "198.51.100.7")
	}
}

// 非法 CIDR 必须 fail-loud（panic），不得带着错误的可信边界继续跑。
func TestApplyTrustedProxies_InvalidEnvPanics(t *testing.T) {
	t.Setenv(TrustedProxiesEnv, "not-a-cidr")
	gin.SetMode(gin.TestMode)
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on invalid AIBOT_TRUSTED_PROXIES")
		}
	}()
	ApplyTrustedProxies(gin.New())
}
