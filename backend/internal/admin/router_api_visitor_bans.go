package admin

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	adminmiddleware "github.com/askie/grix/backend/internal/admin/middleware"
	adminservice "github.com/askie/grix/backend/internal/admin/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func registerVisitorBanAPIRoutes(g *gin.RouterGroup) {
	g.GET("/visitor-bans", apiListVisitorBans)
	g.POST("/visitor-bans/:session_id/unban", apiUnbanWidgetVisitor)
}

func apiListVisitorBans(c *gin.Context) {
	status, ok := parseVisitorBanStatus(c.DefaultQuery("status", "banned"))
	if !ok {
		response.Fail(c, http.StatusBadRequest, 10002, "无效状态")
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	result, err := adminservice.ListVisitorBans(adminservice.VisitorBanListParams{
		Query:    c.Query("q"),
		Status:   status,
		Page:     page,
		PageSize: pageSize,
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

func apiUnbanWidgetVisitor(c *gin.Context) {
	admin := adminmiddleware.CurrentAdmin(c)
	if err := adminservice.UnbanWidgetVisitor(admin.ID, c.Param("session_id"), c.ClientIP(), c.Request.UserAgent()); err != nil {
		if errors.Is(err, adminservice.ErrVisitorBanNotFound) {
			response.Fail(c, http.StatusNotFound, 4004, err.Error())
			return
		}
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func parseVisitorBanStatus(raw string) (int16, bool) {
	switch strings.TrimSpace(raw) {
	case "", "all":
		return 0, true
	case "active":
		return model.WidgetSessionStatusActive, true
	case "closed":
		return model.WidgetSessionStatusClosed, true
	case "banned":
		return model.WidgetSessionStatusBanned, true
	default:
		return 0, false
	}
}
