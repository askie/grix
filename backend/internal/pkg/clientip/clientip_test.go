package clientip

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func newReq(remoteAddr string, headers map[string]string) *http.Request {
	r, _ := http.NewRequest(http.MethodGet, "/v1/agent-api", nil)
	r.RemoteAddr = remoteAddr
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

func TestFromRequestRemoteAddrOnly(t *testing.T) {
	assert.Equal(t, "203.0.113.7", FromRequest(newReq("203.0.113.7:52341", nil)))
	assert.Equal(t, "127.0.0.1", FromRequest(newReq("127.0.0.1:8080", nil)))
}

func TestFromRequestForwardedFor(t *testing.T) {
	// LB 场景：XFF 带真实客户端 + 中间代理追加的内网跳
	r := newReq("10.0.0.2:443", map[string]string{
		"X-Forwarded-For": "203.0.113.7, 10.0.0.1",
	})
	assert.Equal(t, "203.0.113.7", FromRequest(r))

	// 最右是内网跳（多层内网代理），跳过内网取最右侧公网 IP
	r = newReq("10.0.0.2:443", map[string]string{
		"X-Forwarded-For": "192.168.1.5, 203.0.113.9, 10.0.0.1",
	})
	assert.Equal(t, "203.0.113.9", FromRequest(r))

	// 全内网：回退最右侧合法 IP（开发环境）
	r = newReq("10.0.0.2:443", map[string]string{
		"X-Forwarded-For": "192.168.1.5, 10.0.0.1",
	})
	assert.Equal(t, "10.0.0.1", FromRequest(r))
}

// TestFromRequestForwardedForResistsClientForgery 验证安全判定不会被客户端伪造的
// XFF 绕过：客户端只能在自己一侧（最左）塞入任意内容，无法覆盖可信入口自己追加在
// 最右的那一跳，因此必须取最右侧公网 IP，取最左会被下面这种伪造直接绕过封禁。
func TestFromRequestForwardedForResistsClientForgery(t *testing.T) {
	r := newReq("10.0.0.2:443", map[string]string{
		// 攻击者自己拼接了一个伪造的公网 IP 放在最左，
		// 真实来源 IP 由可信入口追加在最右——必须取到最右这个才是真的。
		"X-Forwarded-For": "1.2.3.4, 203.0.113.55",
	})
	assert.Equal(t, "203.0.113.55", FromRequest(r))
}

func TestFromRequestRealIPFallback(t *testing.T) {
	r := newReq("10.0.0.2:443", map[string]string{"X-Real-IP": "198.51.100.3"})
	assert.Equal(t, "198.51.100.3", FromRequest(r))
}

func TestFromRequestGarbage(t *testing.T) {
	r := newReq("10.0.0.2:443", map[string]string{"X-Forwarded-For": "not-an-ip, ???"})
	assert.Equal(t, "10.0.0.2", FromRequest(r))
	assert.Equal(t, "", FromRequest(newReq("garbage", nil)))
}

func TestFromRequestIPv6(t *testing.T) {
	r := newReq("[2001:db8::1]:443", nil)
	assert.Equal(t, "2001:db8::1", FromRequest(r))
	r = newReq("10.0.0.2:443", map[string]string{"X-Forwarded-For": "2001:db8::2"})
	assert.Equal(t, "2001:db8::2", FromRequest(r))
}
