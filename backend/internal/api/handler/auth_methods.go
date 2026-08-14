package handler

import (
	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// GetAuthMethods GET /v1/auth/methods?region=cn|global
//
// 匿名接口；返回当前区域可用的认证入口开关，供登录页 / 注册页 / 手机登录页
// 动态决定 UI 显示哪些方式。塘主在塘主面板修改 SmsSettings 后，前端拉一次
// 即可生效，无需重新发包。
func GetAuthMethods(c *gin.Context) {
	region := c.Query("region")
	response.OK(c, service.GetAuthMethods(region))
}
