package handler

import (
	"net/http"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// --- Check upgrade ---

type agentCheckUpgradeReq struct {
	ClientType    string `form:"client_type" binding:"required"`
	ClientVersion string `form:"client_version" binding:"required"`
	Channel       string `form:"channel"`
	Platform      string `form:"platform"`
	Arch          string `form:"arch"`
}

func AgentCheckUpgrade(c *gin.Context) {
	var req agentCheckUpgradeReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	if req.Channel == "" {
		req.Channel = "stable"
	}

	agentID := middleware.GetAgentID(c)
	result, ec := service.CheckUpgrade(service.CheckUpgradeReq{
		ClientType:    req.ClientType,
		ClientVersion: req.ClientVersion,
		Channel:       req.Channel,
		AgentID:       agentID,
		Platform:      req.Platform,
		Arch:          req.Arch,
	})
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, result)
}

// --- Report upgrade ---

type agentReportUpgradeReq struct {
	ClientType  string  `json:"client_type"`
	FromVersion string  `json:"from_version" binding:"required"`
	ToVersion   string  `json:"to_version" binding:"required"`
	Status      string  `json:"status" binding:"required"`
	ErrorCode   *string `json:"error_code"`
	ErrorMsg    *string `json:"error_msg"`
	DurationMs  *int    `json:"duration_ms"`
	UpgradeLog  *string `json:"upgrade_log"`
	CrashCount  int     `json:"crash_count"`
	NpmVersion  *string `json:"npm_version"`
	NodeVersion *string `json:"node_version"`
	DiskFreeMb  *int    `json:"disk_free_mb"`
	Platform    *string `json:"platform"`
	Arch        *string `json:"arch"`
	HostName    *string `json:"host_name"`
	InstallID   *string `json:"install_id"`
}

func AgentReportUpgrade(c *gin.Context) {
	var req agentReportUpgradeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	agentID := middleware.GetAgentID(c)
	clientType := req.ClientType
	if clientType == "" {
		clientType = "grix-connector"
	}
	ec := service.ReportUpgrade(service.ReportUpgradeReq{
		AgentID:     agentID,
		ClientType:  clientType,
		FromVersion: req.FromVersion,
		ToVersion:   req.ToVersion,
		Status:      req.Status,
		ErrorCode:   req.ErrorCode,
		ErrorMsg:    req.ErrorMsg,
		UpgradeLog:  req.UpgradeLog,
		CrashCount:  req.CrashCount,
		NpmVersion:  req.NpmVersion,
		NodeVersion: req.NodeVersion,
		DiskFreeMb:  req.DiskFreeMb,
		Platform:    req.Platform,
		Arch:        req.Arch,
		DurationMs:  req.DurationMs,
		HostName:    req.HostName,
		InstallID:   req.InstallID,
	})
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, struct{}{})
}
