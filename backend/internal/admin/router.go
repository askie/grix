package admin

import (
	adminmiddleware "github.com/askie/grix/backend/internal/admin/middleware"
	"github.com/gin-gonic/gin"
)

// RegisterRoutes 挂载 admin 管理后台的 JSON API 路由组。
// HTML 页面层已移除，前端统一使用 Flutter admin app 通过 /admin/api 接口交互。
func RegisterRoutes(r *gin.Engine) {
	group := r.Group("/admin")
	group.Use(adminmiddleware.SecurityHeaders())
	registerAPIRoutes(group)
}
