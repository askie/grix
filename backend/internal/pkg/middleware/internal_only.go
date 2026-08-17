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

// isPrivateIP reports whether ip falls in private/loopback ranges.
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, block := range parsedPrivateRanges {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// InternalOnly rejects requests from non-private IPs with 403.
// Use for endpoints that should only be reachable from within the cluster.
func InternalOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isPrivateIP(net.ParseIP(c.ClientIP())) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Next()
	}
}

// InternalOnlyHTTP is the net/http counterpart of InternalOnly, for servers
// that don't run gin (e.g. the WS node's /metrics endpoint).
func InternalOnlyHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if !isPrivateIP(net.ParseIP(host)) {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
