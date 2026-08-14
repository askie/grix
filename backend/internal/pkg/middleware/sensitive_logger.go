package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// SensitivePathLogger 返回一个 gin.Logger，对含敏感凭证的路径前缀跳过访问日志，
// 防止长期凭证（如 webhook token）被写入日志文件。
func SensitivePathLogger(skipPrefixes ...string) gin.HandlerFunc {
	logger := gin.Logger()
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		for _, prefix := range skipPrefixes {
			if strings.HasPrefix(path, prefix) {
				c.Next()
				return
			}
		}
		logger(c)
	}
}
