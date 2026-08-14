package middleware

import (
	"net/http"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// RequirePermission 校验当前管理员是否拥有指定权限 key。
// 权限列表已在 RequireAPIAuth 阶段加载至 context，此处直接读取，无额外 DB 查询。
func RequirePermission(key string) gin.HandlerFunc {
	return func(c *gin.Context) {
		admin := CurrentAdmin(c)
		if admin == nil {
			response.Fail(c, http.StatusUnauthorized, 10001, "admin auth required")
			c.Abort()
			return
		}
		if admin.Role == model.AdminRoleSuperAdmin {
			c.Next()
			return
		}
		perms := GetPermissions(c)
		for _, k := range perms {
			if k == key {
				c.Next()
				return
			}
		}
		response.Fail(c, http.StatusForbidden, 10005, "权限不足")
		c.Abort()
	}
}

// GetPermissions 从 context 获取当前管理员的权限列表（RequireAPIAuth 已预加载）。
func GetPermissions(c *gin.Context) []string {
	value, ok := c.Get(adminPermissionsContextKey)
	if !ok {
		return nil
	}
	perms, _ := value.([]string)
	return perms
}
