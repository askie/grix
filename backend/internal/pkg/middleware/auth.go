package middleware

import (
	"net/http"
	"strings"

	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/askie/grix/backend/internal/security"
	"github.com/gin-gonic/gin"
)

const claimsContextKey = "jwt_claims"

func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			response.Fail(c, http.StatusUnauthorized, 10001, "missing or invalid authorization header")
			c.Abort()
			return
		}
		tokenStr := strings.TrimPrefix(auth, "Bearer ")
		claims, err := jwtpkg.ValidateAccessToken(tokenStr)
		if err != nil {
			response.Fail(c, http.StatusUnauthorized, 10001, "token invalid or expired")
			c.Abort()
			return
		}
		if security.IsAccessTokenRevoked(claims.ID) {
			response.Fail(c, http.StatusUnauthorized, 10001, "token invalid or expired")
			c.Abort()
			return
		}
		if security.IsLoginSessionRevoked(claims.UserID, claims.SessionID) {
			response.Fail(c, http.StatusUnauthorized, 10001, "token invalid or expired")
			c.Abort()
			return
		}
		if claims.IssuedAt != nil && security.IsAccessTokenInvalidByPasswordChange(claims.UserID, claims.IssuedAt.Time) {
			response.Fail(c, http.StatusUnauthorized, 10001, "token invalid or expired")
			c.Abort()
			return
		}
		if err := security.EnsureUserActive(claims.UserID); err != nil {
			if err == security.ErrUserDisabled {
				response.Fail(c, http.StatusForbidden, 10001, "用户已被禁用")
			} else {
				response.Fail(c, http.StatusUnauthorized, 10001, "token invalid or expired")
			}
			c.Abort()
			return
		}
		c.Set("user_id", claims.UserID)
		c.Set(claimsContextKey, claims)
		c.Next()
	}
}

func GetUserID(c *gin.Context) int64 {
	v, _ := c.Get("user_id")
	uid, _ := v.(int64)
	return uid
}

func GetClaims(c *gin.Context) *jwtpkg.Claims {
	v, ok := c.Get(claimsContextKey)
	if !ok {
		return nil
	}
	claims, _ := v.(*jwtpkg.Claims)
	return claims
}
