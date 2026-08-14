package middleware

import (
	"net"
	"net/http"

	"github.com/gin-gonic/gin"
)

var privateRanges = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"127.0.0.0/8",
	"::1/128",
	"fc00::/7",
}

var parsedPrivateRanges []*net.IPNet

func init() {
	for _, cidr := range privateRanges {
		_, block, _ := net.ParseCIDR(cidr)
		if block != nil {
			parsedPrivateRanges = append(parsedPrivateRanges, block)
		}
	}
}

// InternalOnly rejects requests from non-private IPs with 403.
// Use for endpoints that should only be reachable from within the cluster.
func InternalOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := net.ParseIP(c.ClientIP())
		if ip == nil {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		for _, block := range parsedPrivateRanges {
			if block.Contains(ip) {
				c.Next()
				return
			}
		}
		c.AbortWithStatus(http.StatusForbidden)
	}
}
