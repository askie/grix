package middleware

import (
	"fmt"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

// TrustedProxiesEnv 允许部署侧用逗号分隔的 CIDR 列表覆盖默认可信代理网段
//（例如前置 LB / ingress 的出口不在默认私网段内时）。
const TrustedProxiesEnv = "AIBOT_TRUSTED_PROXIES"

// ApplyTrustedProxies 收敛 gin 的可信代理边界。gin v1.12 默认信任所有代理头部，
// 攻击者伪造 X-Forwarded-For 即可操纵 c.ClientIP()，绕过 IP 限流（RateLimitByIP）
// 和 /metrics 的内网限定（InternalOnly）。
// 默认基线只信任回环 + 私网网段（复用 InternalOnly 的 privateRanges）：
// 仅当直接对端（RemoteAddr）落在可信网段内时，gin 才采纳 X-Forwarded-For / X-Real-IP。
func ApplyTrustedProxies(r *gin.Engine) {
	proxies := privateRanges
	if raw := strings.TrimSpace(os.Getenv(TrustedProxiesEnv)); raw != "" {
		proxies = nil
		for _, p := range strings.Split(raw, ",") {
			if p = strings.TrimSpace(p); p != "" {
				proxies = append(proxies, p)
			}
		}
	}
	// 配置非法直接 fail-loud：可信代理边界配错还继续跑，等于安全控制静默失效。
	if err := r.SetTrustedProxies(proxies); err != nil {
		panic(fmt.Sprintf("invalid trusted proxies (%s=%q): %v", TrustedProxiesEnv, os.Getenv(TrustedProxiesEnv), err))
	}
}
