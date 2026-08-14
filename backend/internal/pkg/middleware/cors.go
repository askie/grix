package middleware

import (
	"net/http"
	"strings"

	"github.com/askie/grix/backend/config"
	"github.com/gin-gonic/gin"
)

// CORS 按 server.allowed_web_origins 白名单回显 Origin，拒绝不在白名单内的跨域请求。
// 空 Origin（非浏览器客户端）直接放行，不设 ACAO 头。
// /v1/widget/ 公共路由对所有 Origin 放行 CORS，由 widget service 层按站点配置校验权限。
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		widgetPublic := isWidgetPublicPath(c.Request.URL.Path)
		// widget 公共接口的响应禁止被浏览器/CDN 缓存，避免某次跨域头缺失等临时异常
		// 被缓存后反复回放，导致部分客户长期跨域报错（即使服务端已修复）。
		if widgetPublic {
			c.Header("Cache-Control", "no-store")
		}
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if origin != "" {
			if widgetPublic || isAllowedCORSOrigin(origin, config.C.Server.AllowedWebOrigins) {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Vary", "Origin")
				c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-App-Locale, Accept-Language")
				c.Header("Access-Control-Max-Age", "86400")
			}
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func isAllowedCORSOrigin(origin, allowedOrigins string) bool {
	for _, item := range strings.Split(allowedOrigins, ",") {
		allowed := strings.TrimRight(strings.TrimSpace(item), "/")
		if allowed != "" && strings.EqualFold(allowed, strings.TrimRight(origin, "/")) {
			return true
		}
	}
	return false
}

// isWidgetPublicPath 判断是否为 widget 公共路由（嵌入第三方站点使用，需对所有 Origin 放行 CORS）。
// 实际的 Origin 权限校验由 widget service 层按 WidgetSite.AllowedOrigins 执行。
func isWidgetPublicPath(path string) bool {
	return strings.HasPrefix(path, "/v1/widget/visitor/") ||
		path == "/v1/widget/config" ||
		strings.HasPrefix(path, "/v1/widget/ws")
}
