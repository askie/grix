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

// registerModerationAPIRoutes 注册内容审查相关 JSON 接口。
func registerModerationAPIRoutes(g *gin.RouterGroup) {
	g.GET("/moderation", apiListModerationEvents)
	g.GET("/moderation/settings", apiGetModerationSettings)
	g.PUT("/moderation/settings", apiUpdateModerationSettings)
	g.POST("/moderation/unmute", apiModerationUnmute)
}

func apiListModerationEvents(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	result, err := adminservice.ListContentModerationEvents(adminservice.ContentModerationEventListParams{
		Query:     c.Query("q"),
		MutedOnly: c.Query("muted") == "1",
		Page:      page,
		PageSize:  pageSize,
	})
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}

	response.OK(c, gin.H{
		"items":     result.Items,
		"total":     result.Total,
		"page":      result.Page,
		"page_size": result.PageSize,
	})
}

func apiGetModerationSettings(c *gin.Context) {
	settings, err := adminservice.GetContentModerationSettings()
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	response.OK(c, gin.H{"settings": settings})
}

func apiUpdateModerationSettings(c *gin.Context) {
	var settings systemsetting.ContentModerationSettings
	if err := c.ShouldBindJSON(&settings); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	admin := adminmiddleware.CurrentAdmin(c)
	if err := adminservice.UpdateContentModerationSettings(admin.ID, settings, c.ClientIP(), c.Request.UserAgent()); err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func apiModerationUnmute(c *gin.Context) {
	var body struct {
		SessionID string `json:"session_id"`
		MemberID  string `json:"member_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	memberID, err := strconv.ParseInt(strings.TrimSpace(body.MemberID), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "用户ID格式错误")
		return
	}
	admin := adminmiddleware.CurrentAdmin(c)
	if err := adminservice.UnmuteModeratedSessionMember(admin.ID, body.SessionID, memberID, c.ClientIP(), c.Request.UserAgent()); err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}
