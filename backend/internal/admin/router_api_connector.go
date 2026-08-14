package admin

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/pkg/response"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/gin-gonic/gin"
)

func registerConnectorAPIRoutes(g *gin.RouterGroup) {
	g.GET("/connector/releases", apiListConnectorReleases)
	g.POST("/connector/releases", apiCreateConnectorRelease)
	g.POST("/connector/releases/:id/publish", apiPublishConnectorRelease)
	g.POST("/connector/releases/:id/pause", apiPauseConnectorRelease)
	g.POST("/connector/releases/:id/resume", apiResumeConnectorRelease)
	g.POST("/connector/releases/:id/revoke", apiRevokeConnectorRelease)
	g.POST("/connector/releases/push-upgrade", apiPushConnectorUpgrade)
	g.GET("/connector/releases/:id/rules", apiListConnectorRolloutRules)
	g.POST("/connector/rollout-rules", apiCreateConnectorRolloutRule)
	g.POST("/connector/rollout-rules/:id/toggle", apiToggleConnectorRolloutRule)
	g.DELETE("/connector/rollout-rules/:id", apiDeleteConnectorRolloutRule)
	g.GET("/connector/reports", apiListConnectorUpgradeReports)
	g.GET("/connector/stats", apiConnectorUpgradeStats)
}

func apiListConnectorReleases(c *gin.Context) {
	releases, ec := service.ListConnectorReleases(c.Query("client_type"))
	if ec != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, ec.Msg)
		return
	}
	response.OK(c, gin.H{"releases": releases})
}

func apiCreateConnectorRelease(c *gin.Context) {
	var body struct {
		ClientType string `json:"client_type"`
		Version    string `json:"version"`
		Channel    string `json:"channel"`
		Changelog  string `json:"changelog"`
		MinVersion *string `json:"min_version"`
		NpmPackage string `json:"npm_package"`
		NpmTag     string `json:"npm_tag"`
		Force      bool   `json:"force"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	if body.ClientType == "" {
		body.ClientType = "grix-connector"
	}
	if body.Channel == "" {
		body.Channel = "stable"
	}
	if body.NpmPackage == "" {
		body.NpmPackage = "grix-connector"
	}
	if body.NpmTag == "" {
		body.NpmTag = "latest"
	}
	created, ec := service.CreateConnectorRelease(service.CreateConnectorReleaseReq{
		ClientType: body.ClientType, Version: body.Version, Channel: body.Channel, Changelog: body.Changelog,
		MinVersion: body.MinVersion, NpmPackage: body.NpmPackage, NpmTag: body.NpmTag, Force: body.Force,
	})
	if ec != nil {
		response.Fail(c, http.StatusBadRequest, 10006, ec.Msg)
		return
	}
	response.OK(c, gin.H{"release": created})
}

func apiPublishConnectorRelease(c *gin.Context)  { connectorReleaseAction(c, service.PublishConnectorRelease) }
func apiPauseConnectorRelease(c *gin.Context)    { connectorReleaseAction(c, service.PauseConnectorRelease) }
func apiResumeConnectorRelease(c *gin.Context)   { connectorReleaseAction(c, service.ResumeConnectorRelease) }
func apiRevokeConnectorRelease(c *gin.Context)   { connectorReleaseAction(c, service.RevokeConnectorRelease) }

func connectorReleaseAction(c *gin.Context, fn func(int64) (*service.ConnectorReleaseResp, *errcode.ErrCode)) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "无效ID")
		return
	}
	_, ec := fn(id)
	if ec != nil {
		response.Fail(c, http.StatusBadRequest, 10006, ec.Msg)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func apiPushConnectorUpgrade(c *gin.Context) {
	// 走 Redis 广播让所有 ws 节点都收到一份，各节点对本地在线 agent 派 local_action。
	// API 服务进程内通常没有 ws 连接，老路径 ForEachOnlineAgent 只命中本进程 → 多节点部署下覆盖不全。
	// nodes 为收到广播的 ws 节点数（单实例 Redis 下成立，见 PublishConnectorUpgradePush 注释）。
	nodes, err := wsagentapi.PublishConnectorUpgradePush()
	if err != nil {
		response.Fail(c, http.StatusServiceUnavailable, 10004, "Redis 不可用，无法广播升级 push")
		return
	}
	response.OK(c, gin.H{"broadcasted": true, "nodes": nodes})
}

func apiListConnectorRolloutRules(c *gin.Context) {
	releaseID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	rules, ec := service.ListRolloutRules(releaseID)
	if ec != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, ec.Msg)
		return
	}
	response.OK(c, gin.H{"rules": rules})
}

func apiCreateConnectorRolloutRule(c *gin.Context) {
	var body struct {
		ReleaseID int64           `json:"release_id,string"`
		RuleType  string          `json:"rule_type"`
		RuleValue json.RawMessage `json:"rule_value"`
		Priority  int             `json:"priority"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	_, ec := service.CreateRolloutRule(service.CreateRolloutRuleReq{
		ReleaseID: body.ReleaseID, RuleType: body.RuleType, RuleValue: body.RuleValue, Priority: body.Priority,
	})
	if ec != nil {
		response.Fail(c, http.StatusBadRequest, 10006, ec.Msg)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func apiToggleConnectorRolloutRule(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var body struct{ Status int16 `json:"status"` }
	_ = c.ShouldBindJSON(&body)
	_, ec := service.UpdateRolloutRuleStatus(id, body.Status)
	if ec != nil {
		response.Fail(c, http.StatusBadRequest, 10006, ec.Msg)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func apiDeleteConnectorRolloutRule(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if ec := service.DeleteRolloutRule(id); ec != nil {
		response.Fail(c, http.StatusBadRequest, 10006, ec.Msg)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func apiListConnectorUpgradeReports(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	result, ec := service.ListUpgradeReports(service.ListUpgradeReportsReq{
		ClientType: c.Query("client_type"), ToVersion: c.Query("to_version"), Status: c.Query("status"), Page: page, PageSize: pageSize,
	})
	if ec != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, ec.Msg)
		return
	}
	response.OK(c, gin.H{"reports": result.Reports, "total": result.Total, "page": page, "page_size": pageSize})
}

func apiConnectorUpgradeStats(c *gin.Context) {
	version := c.Query("version")
	if version == "" {
		response.OK(c, gin.H{"stats": nil, "detail": nil})
		return
	}
	clientType := c.Query("client_type")
	stats, _ := service.GetUpgradeStats(version, clientType)
	detail, _ := service.GetUpgradeStatsDetail(version, clientType)
	response.OK(c, gin.H{"stats": stats, "detail": detail})
}
