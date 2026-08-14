package handler

import (
	"net/http"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// --- Client: Appcast XML feed for Sparkle/WinSparkle ---

func AppcastXML(c *gin.Context) {
	platform := c.Query("platform")
	if platform == "" {
		c.String(http.StatusBadRequest, "missing platform parameter")
		return
	}
	channel := c.DefaultQuery("channel", "stable")
	xml, ec := service.GenerateAppcast(platform, channel)
	if ec != nil {
		c.String(ec.HTTPStatus, ec.Msg)
		return
	}
	c.Data(http.StatusOK, "application/xml; charset=utf-8", []byte(xml))
}

// --- Client: Check update ---

type checkAppUpdateReq struct {
	Platform    string `form:"platform" binding:"required"`
	Version     string `form:"version" binding:"required"`
	BuildNumber int    `form:"build_number" binding:"required"`
	Channel     string `form:"channel"`
	OsVersion   string `form:"os_version"`
}

func CheckAppUpdate(c *gin.Context) {
	var req checkAppUpdateReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	userID := middleware.GetUserID(c)
	result, ec := service.CheckAppUpdate(service.CheckAppUpdateReq{
		Platform:    req.Platform,
		Version:     req.Version,
		BuildNumber: req.BuildNumber,
		Channel:     req.Channel,
		UserID:      userID,
		OsVersion:   req.OsVersion,
	})
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, result)
}

// --- Client: Report download ---

func ReportAppDownload(c *gin.Context) {
	var req struct {
		BuildNumber int    `json:"build_number" binding:"required"`
		FromBuild   *int   `json:"from_build"`
		Platform    string `json:"platform" binding:"required"`
		ErrorMsg    string `json:"error_msg"`
		DurationMs  int    `json:"duration_ms"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	userID := middleware.GetUserID(c)
	ec := service.ReportAppDownload(service.ReportAppDownloadReq{
		UserID:      userID,
		BuildNumber: req.BuildNumber,
		Platform:    req.Platform,
		FromBuild:   req.FromBuild,
		ErrorMsg:    req.ErrorMsg,
		DurationMs:  req.DurationMs,
	})
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, struct{}{})
}
