package middleware

import (
	"net/http"
	"strings"

	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

const (
	widgetClaimsContextKey    = "widget_jwt_claims"
	widgetSiteIDContextKey    = "widget_site_id"
	widgetVisitorIDContextKey = "widget_visitor_id"
	widgetOwnerIDContextKey   = "widget_owner_user_id"
	widgetSessionIDContextKey = "widget_session_id"
)

func WidgetAuth(requiredScopes ...string) gin.HandlerFunc {
	normalizedRequiredScopes := normalizeRequiredScopes(requiredScopes)
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			response.Fail(c, http.StatusUnauthorized, 10001, "missing or invalid authorization header")
			c.Abort()
			return
		}

		tokenStr := strings.TrimPrefix(auth, "Bearer ")
		claims, err := jwtpkg.ValidateWidgetAccessToken(tokenStr)
		if err != nil {
			response.Fail(c, http.StatusUnauthorized, 10001, "token invalid or expired")
			c.Abort()
			return
		}

		for _, scope := range normalizedRequiredScopes {
			if jwtpkg.WidgetScopeAllowed(claims, scope) {
				continue
			}
			response.Fail(c, http.StatusForbidden, 4003, "permission denied")
			c.Abort()
			return
		}

		c.Set(widgetClaimsContextKey, claims)
		c.Set(widgetSiteIDContextKey, claims.WidgetSiteID)
		c.Set(widgetVisitorIDContextKey, claims.WidgetVisitorID)
		c.Set(widgetOwnerIDContextKey, claims.WidgetOwnerUserID)
		c.Set(widgetSessionIDContextKey, claims.SessionID)
		c.Next()
	}
}

func GetWidgetClaims(c *gin.Context) *jwtpkg.Claims {
	if c == nil {
		return nil
	}
	v, ok := c.Get(widgetClaimsContextKey)
	if !ok {
		return nil
	}
	claims, _ := v.(*jwtpkg.Claims)
	return claims
}

func GetWidgetSiteID(c *gin.Context) int64 {
	return getInt64ContextValue(c, widgetSiteIDContextKey)
}

func GetWidgetVisitorID(c *gin.Context) int64 {
	return getInt64ContextValue(c, widgetVisitorIDContextKey)
}

func GetWidgetOwnerUserID(c *gin.Context) int64 {
	return getInt64ContextValue(c, widgetOwnerIDContextKey)
}

func GetWidgetSessionID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	v, ok := c.Get(widgetSessionIDContextKey)
	if !ok {
		return ""
	}
	sessionID, _ := v.(string)
	return strings.TrimSpace(sessionID)
}

func normalizeRequiredScopes(required []string) []string {
	if len(required) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(required))
	for _, item := range required {
		scope := strings.TrimSpace(item)
		if scope == "" {
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		result = append(result, scope)
	}
	return result
}

func getInt64ContextValue(c *gin.Context, key string) int64 {
	if c == nil {
		return 0
	}
	v, ok := c.Get(key)
	if !ok {
		return 0
	}
	val, _ := v.(int64)
	return val
}
