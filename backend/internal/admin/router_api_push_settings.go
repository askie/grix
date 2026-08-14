package admin

import (
	"net/http"

	adminmiddleware "github.com/askie/grix/backend/internal/admin/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/askie/grix/backend/internal/systemsetting"
	"github.com/gin-gonic/gin"
)

// registerPushSettingsAPIRoutes 注册离线推送通道开关接口。
//
//   - GET /admin/api/settings/push — 读各通道开关（iOS / 安卓FCM / 网页WebPush / 极光JPush / 国产厂商通道）
//   - PUT /admin/api/settings/push — 写各通道开关
//
// push 服务带 1 分钟缓存读取同一份配置，变更后最多 1 分钟生效，无需重启。
func registerPushSettingsAPIRoutes(g *gin.RouterGroup) {
	g.GET("/settings/push", apiGetPushChannelSettings)
	g.PUT("/settings/push", apiUpdatePushChannelSettings)
}

func apiGetPushChannelSettings(c *gin.Context) {
	s, err := systemsetting.GetPushChannelSettings()
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	response.OK(c, s)
}

func apiUpdatePushChannelSettings(c *gin.Context) {
	// 以当前已存设置打底再反序列化覆盖：请求体未携带的通道保持原状。
	// 不能用零值打底（旧表单会把新增通道静默关掉），也不能用默认值打底
	// （会把管理员先前手动关掉的通道静默重新打开）。绕过缓存直读，避免拿到
	// 别的副本一分钟前的旧值而覆盖掉这期间的变更。
	s, err := systemsetting.GetPushChannelSettingsFresh()
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	if err := c.ShouldBindJSON(&s); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	admin := adminmiddleware.CurrentAdmin(c)
	if err := systemsetting.SavePushChannelSettings(s, &admin.ID); err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}
