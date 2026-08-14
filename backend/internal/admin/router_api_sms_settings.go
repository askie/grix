package admin

import (
	"net/http"

	adminmiddleware "github.com/askie/grix/backend/internal/admin/middleware"
	adminservice "github.com/askie/grix/backend/internal/admin/service"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// registerSmsSettingsAPIRoutes 注册手机号短信登录注册的配置接口。
//
// 路由（全部需要 admin 鉴权）：
//   - GET  /admin/api/settings/sms       — 读配置（ak/sk 脱敏成末四位）
//   - PUT  /admin/api/settings/sms       — 写配置（ak/sk 空表示保留原值）
//   - POST /admin/api/settings/sms/test  — 给指定手机号发测试码，验证 ak/sk + 模板号配置是否正确
func registerSmsSettingsAPIRoutes(g *gin.RouterGroup) {
	g.GET("/settings/sms", apiGetSmsSettings)
	g.PUT("/settings/sms", apiUpdateSmsSettings)
	g.POST("/settings/sms/test", apiTestSendSms)
}

func apiGetSmsSettings(c *gin.Context) {
	v, err := adminservice.GetSmsSettingsView()
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	response.OK(c, v)
}

func apiUpdateSmsSettings(c *gin.Context) {
	var body adminservice.SmsSettingsPatch
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	admin := adminmiddleware.CurrentAdmin(c)
	if err := adminservice.UpdateSmsSettings(admin.ID, body, c.ClientIP(), c.Request.UserAgent()); err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func apiTestSendSms(c *gin.Context) {
	var body struct {
		PhoneE164 string `json:"phone_e164"`
		Region    string `json:"region"` // 可选；不传按手机号自动判定
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	if err := adminservice.TestSendSms(body.PhoneE164, body.Region); err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}
