package middleware

import (
	"net/http"
	"strings"

	adminservice "github.com/askie/grix/backend/internal/admin/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

const adminContextKey = "admin_user"
const adminSessionTokenContextKey = "admin_session_token"
const adminPermissionsContextKey = "admin_permissions"

// RequireAPIAuth 校验 Bearer Token 认证，并一次性加载角色权限存入 context。
func RequireAPIAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionToken := resolveAdminSessionToken(c)
		if sessionToken == "" {
			response.Fail(c, http.StatusUnauthorized, 10001, "admin auth required")
			c.Abort()
			return
		}

		_, admin, err := adminservice.LoadAdminBySession(sessionToken)
		if err != nil {
			response.Fail(c, http.StatusUnauthorized, 10001, "admin auth required")
			c.Abort()
			return
		}

		// 一次性加载权限列表，后续 RequirePermission / AdminPermissions 直接从 context 取。
		perms := adminservice.LoadAdminPermissions(admin)

		c.Set(adminContextKey, admin)
		c.Set(adminSessionTokenContextKey, sessionToken)
		c.Set(adminPermissionsContextKey, perms)
		c.Next()
	}
}

func resolveAdminSessionToken(c *gin.Context) string {
	authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return strings.TrimSpace(authHeader[7:])
	}
	return ""
}

// CurrentAdmin 从上下文获取当前管理员。
func CurrentAdmin(c *gin.Context) *model.AdminUser {
	value, ok := c.Get(adminContextKey)
	if !ok {
		return nil
	}
	admin, _ := value.(*model.AdminUser)
	return admin
}

// CurrentSessionToken 从上下文获取当前会话令牌。
func CurrentSessionToken(c *gin.Context) string {
	value, ok := c.Get(adminSessionTokenContextKey)
	if !ok {
		return ""
	}
	token, _ := value.(string)
	return token
}
