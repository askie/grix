package admin

import (
	"net/http"
	"strconv"
	"strings"

	adminmiddleware "github.com/askie/grix/backend/internal/admin/middleware"
	adminservice "github.com/askie/grix/backend/internal/admin/service"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/askie/grix/backend/internal/systemsetting"
	"github.com/gin-gonic/gin"
)

// registerSettingsAPIRoutes 注册系统设置相关 JSON 接口。
func registerSettingsAPIRoutes(g *gin.RouterGroup) {
	g.GET("/settings", apiGetSettings)
	g.PUT("/settings/auth", apiUpdateAuthSettings)
	g.PUT("/settings/group", apiUpdateGroupSettings)
	g.GET("/settings/voice-models", apiGetVoiceModels)
	g.PUT("/settings/voice-models", apiUpdateVoiceModels)
}

func apiGetSettings(c *gin.Context) {
	auth, err := adminservice.GetAuthSettings()
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	group, err := adminservice.GetGroupSettings()
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	response.OK(c, gin.H{
		"auth": gin.H{
			"auto_add_customer_user_id": strconv.FormatInt(auth.AutoAddCustomerUserID, 10),
		},
		"group": gin.H{
			"member_invite_threshold": group.MemberInviteThreshold,
		},
	})
}

func apiUpdateAuthSettings(c *gin.Context) {
	var body struct {
		AutoAddCustomerUserID string `json:"auto_add_customer_user_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}

	var customerID int64
	if v := strings.TrimSpace(body.AutoAddCustomerUserID); v != "" {
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			response.Fail(c, http.StatusBadRequest, 10002, "系统客户账户ID必须为非负整数")
			return
		}
		customerID = parsed
	}

	settings := systemsetting.AuthSettings{
		AutoAddCustomerUserID: customerID,
	}

	admin := adminmiddleware.CurrentAdmin(c)
	if err := adminservice.UpdateAuthSettings(admin.ID, settings, c.ClientIP(), c.Request.UserAgent()); err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func apiUpdateGroupSettings(c *gin.Context) {
	var body struct {
		MemberInviteThreshold int `json:"member_invite_threshold"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	admin := adminmiddleware.CurrentAdmin(c)
	if err := adminservice.UpdateGroupSettings(admin.ID, systemsetting.GroupSettings{
		MemberInviteThreshold: body.MemberInviteThreshold,
	}, c.ClientIP(), c.Request.UserAgent()); err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func apiGetVoiceModels(c *gin.Context) {
	settings, err := adminservice.GetVoiceModelsSettings()
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	response.OK(c, gin.H{
		"options":             settings.Options,
		"supported_providers": systemsetting.SupportedVoiceProviders(),
	})
}

func apiUpdateVoiceModels(c *gin.Context) {
	var body struct {
		Options []systemsetting.VoiceModelOption `json:"options"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	admin := adminmiddleware.CurrentAdmin(c)
	if err := adminservice.UpdateVoiceModelsSettings(admin.ID, systemsetting.VoiceModelsSettings{Options: body.Options}, c.ClientIP(), c.Request.UserAgent()); err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func apiChangeOwnPassword(c *gin.Context) {
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	admin := adminmiddleware.CurrentAdmin(c)
	if err := adminservice.ChangeOwnPassword(admin.ID, body.CurrentPassword, body.NewPassword, c.ClientIP(), c.Request.UserAgent()); err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, err.Error())
		return
	}
	// 改密后后端会撤销所有会话，前端需要重新登录。
	response.OK(c, gin.H{"ok": true, "relogin": true})
}
