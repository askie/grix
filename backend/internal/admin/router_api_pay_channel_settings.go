package admin

import (
	"net/http"

	adminmiddleware "github.com/askie/grix/backend/internal/admin/middleware"
	adminservice "github.com/askie/grix/backend/internal/admin/service"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// registerPayChannelSettingsAPIRoutes 注册支付通道商户凭证的配置接口。
//
// 路由（全部需要 admin 鉴权）：
//   - GET  /admin/api/settings/pay_channel            — 读配置（私钥/Secret 脱敏成末四位）
//   - PUT  /admin/api/settings/pay_channel            — 写配置（私钥/Secret 空表示保留原值）
//   - POST /admin/api/settings/pay_channel/test/:code — 用已保存凭证做一次自检（code=alipay/paypal）
func registerPayChannelSettingsAPIRoutes(g *gin.RouterGroup) {
	g.GET("/settings/pay_channel", apiGetPayChannelSettings)
	g.PUT("/settings/pay_channel", apiUpdatePayChannelSettings)
	g.POST("/settings/pay_channel/test/:code", apiTestPayChannel)
}

func apiGetPayChannelSettings(c *gin.Context) {
	v, err := adminservice.GetPayChannelSettingsView()
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	response.OK(c, v)
}

func apiUpdatePayChannelSettings(c *gin.Context) {
	var body adminservice.PayChannelSettingsPatch
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	admin := adminmiddleware.CurrentAdmin(c)
	if err := adminservice.UpdatePayChannelSettings(admin.ID, body, c.ClientIP(), c.Request.UserAgent()); err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func apiTestPayChannel(c *gin.Context) {
	code := c.Param("code")
	if err := adminservice.TestPayChannel(code); err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}
