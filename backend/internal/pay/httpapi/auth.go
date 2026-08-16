package httpapi

import (
	"crypto/subtle"
	"net/http"

	"github.com/gin-gonic/gin"
)

// InternalTokenHeader 是服务间共享密钥的请求头：api→pay 管理面调用必须携带。
const InternalTokenHeader = "X-Pay-Internal-Token"

// RequireInternalToken 校验管理面请求的共享密钥（常量时间比对，防时序探测）。
// pay 服务把下单/退款/查单等管理端点与第三方公网回调挂在同一 listener，
// 不能再假设“内网可达即可信”：回调路径保持公开，管理面一律过本中间件。
// token 为空时直接放行——仅本地 mock 联调（cmd/pay 在非 mock 模式下会对空密钥 fail-loud）。
func RequireInternalToken(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if token == "" {
			c.Next()
			return
		}
		got := c.GetHeader(InternalTokenHeader)
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid internal token"})
			return
		}
		c.Next()
	}
}
