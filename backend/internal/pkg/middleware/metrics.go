package middleware

import (
	"strconv"
	"time"

	"github.com/askie/grix/backend/internal/metrics"
	"github.com/gin-gonic/gin"
)

// Metrics 记录每个请求的计数与耗时，供 Prometheus 抓取（API QPS 与 p99 延迟来源）。
// 路由维度用 c.FullPath() 的路由模板（而非真实 URL），避免高基数爆炸。
func Metrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		method := c.Request.Method
		metrics.HTTPRequestDuration.WithLabelValues("api", method, route).Observe(time.Since(start).Seconds())
		metrics.HTTPRequestsTotal.WithLabelValues("api", method, route, strconv.Itoa(c.Writer.Status())).Inc()
	}
}
