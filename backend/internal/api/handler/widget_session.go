package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func WidgetSessionList(c *gin.Context) {
	siteID := parseInt64Query(c.Query("site_id"), 0)
	status := parseInt16Query(c.Query("status"), 0)
	limit := parseIntQuery(c.Query("limit"), 20)
	offset := parseIntQuery(c.Query("offset"), 0)
	resp, err := service.WidgetSessionList(service.WidgetSessionListInput{
		OwnerUserID: middleware.GetUserID(c),
		SiteID:      siteID,
		Status:      status,
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		handleWidgetSessionError(c, err)
		return
	}
	response.OK(c, resp)
}

func WidgetSessionClose(c *gin.Context) {
	updateWidgetSessionStatus(c, true)
}

func WidgetSessionBan(c *gin.Context) {
	updateWidgetSessionStatus(c, false)
}

func updateWidgetSessionStatus(c *gin.Context, closeAction bool) {
	var req struct {
		SessionID string `json:"session_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.SessionID) == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	input := service.WidgetSessionStatusUpdateInput{OwnerUserID: middleware.GetUserID(c), SessionID: req.SessionID}
	var (
		resp *service.WidgetSessionDTO
		err  error
	)
	if closeAction {
		resp, err = service.WidgetSessionClose(input)
	} else {
		resp, err = service.WidgetSessionBan(input)
	}
	if err != nil {
		handleWidgetSessionError(c, err)
		return
	}
	response.OK(c, resp)
}

func handleWidgetSessionError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrWidgetSiteInvalidInput):
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
	case errors.Is(err, service.ErrWidgetSessionNotOwned):
		response.Fail(c, http.StatusNotFound, 4004, "记录不存在")
	default:
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
	}
}
