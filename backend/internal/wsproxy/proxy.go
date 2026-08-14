package wsproxy

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/gin-gonic/gin"
)

func RegisterRoutes(r *gin.Engine) bool {
	if r == nil {
		return false
	}

	target, err := targetURL(config.C.Server.WSHost, config.C.Server.WSPort)
	if err != nil {
		logger.L.Warnf("ws proxy disabled: %v", err)
		return false
	}

	paths := []string{
		"/ws",
		"/v1/widget/ws",
		agentAPIWSPath(config.C.Server.AgentAPIPath, config.C.Server.AgentAPIWSPath),
	}
	registered := registerRoutesWithTarget(r, target, paths...)
	proxy := newReverseProxy(target)
	r.Any("/v1/webhook/incoming/*token", gin.WrapH(proxy))
	return registered
}

func registerRoutesWithTarget(r *gin.Engine, target *url.URL, paths ...string) bool {
	if r == nil || target == nil {
		return false
	}

	proxy := newReverseProxy(target)
	registered := false
	for _, routePath := range paths {
		normalizedPath := normalizePath(routePath)
		if normalizedPath == "" {
			continue
		}
		r.Any(normalizedPath, gin.WrapH(proxy))
		registered = true
	}

	return registered
}

func targetURL(wsHost string, wsPort int) (*url.URL, error) {
	if wsPort <= 0 {
		return nil, fmt.Errorf("invalid ws port: %d", wsPort)
	}
	host := strings.TrimSpace(wsHost)
	if host == "" {
		host = "127.0.0.1"
	}
	return url.Parse(fmt.Sprintf("http://%s:%d", host, wsPort))
}

func agentAPIWSPath(agentAPIPath, agentAPIWSPath string) string {
	basePath := normalizePath(agentAPIPath)
	if basePath == "" {
		basePath = "/v1/agent-api"
	}

	wsPath := strings.TrimSpace(agentAPIWSPath)
	if wsPath == "" {
		wsPath = "/ws"
	}

	return normalizePath(strings.TrimRight(basePath, "/") + "/" + strings.TrimLeft(wsPath, "/"))
}

func normalizePath(rawPath string) string {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(trimmed, "/") {
		return "/" + trimmed
	}
	return trimmed
}

func newReverseProxy(target *url.URL) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = -1 // 立即 flush，对 WebSocket 代理至关重要
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		logger.L.Warnf("ws proxy error: path=%s err=%v", r.URL.Path, err)
		http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
	}
	return proxy
}
