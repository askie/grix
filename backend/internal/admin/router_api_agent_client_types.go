package admin

import (
	"net/http"

	adminmiddleware "github.com/askie/grix/backend/internal/admin/middleware"
	adminservice "github.com/askie/grix/backend/internal/admin/service"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// registerAgentClientTypesSettingsAPIRoutes 注册「支持的智能体类型」系统设置接口。
//
//   - GET  /admin/api/settings/agent-client-types — 全集 + 每项是否启用
//   - PUT  /admin/api/settings/agent-client-types — 保存启用列表（至少 1 个合法类型）
func registerAgentClientTypesSettingsAPIRoutes(g *gin.RouterGroup) {
	g.GET("/settings/agent-client-types", apiGetAgentClientTypesSettings)
	g.PUT("/settings/agent-client-types", apiUpdateAgentClientTypesSettings)
}

func apiGetAgentClientTypesSettings(c *gin.Context) {
	view, err := adminservice.GetAgentClientTypesAdminView()
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	response.OK(c, view)
}

func apiUpdateAgentClientTypesSettings(c *gin.Context) {
	var body struct {
		Enabled []string `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	admin := adminmiddleware.CurrentAdmin(c)
	if err := adminservice.UpdateAgentClientTypesSettings(admin.ID, body.Enabled, c.ClientIP(), c.Request.UserAgent()); err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}
