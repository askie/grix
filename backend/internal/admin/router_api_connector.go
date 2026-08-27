package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	adminmiddleware "github.com/askie/grix/backend/internal/admin/middleware"
	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/gin-gonic/gin"
)

const (
	// 一次救援批量的上限：太大会让轮询等待和单节点下发压力都不可控。
	connectorRollbackPushMaxAgents = 100
	// 送达回收的等待上限与轮询间隔。派发是跨节点异步的，必须有界。
	connectorRollbackPushWait         = 3 * time.Second
	connectorRollbackPushPollInterval = 150 * time.Millisecond
)

func registerConnectorAPIRoutes(g *gin.RouterGroup) {
	g.GET("/connector/releases", apiListConnectorReleases)
	g.POST("/connector/releases", apiCreateConnectorRelease)
	g.POST("/connector/releases/:id/publish", apiPublishConnectorRelease)
	g.POST("/connector/releases/:id/pause", apiPauseConnectorRelease)
	g.POST("/connector/releases/:id/resume", apiResumeConnectorRelease)
	g.POST("/connector/releases/:id/revoke", apiRevokeConnectorRelease)
	g.PUT("/connector/releases/:id/min-version", apiUpdateConnectorReleaseMinVersion)
	g.POST("/connector/releases/push-upgrade", apiPushConnectorUpgrade)
	// 定向下发 connector_rollback：救那些卡在事务链上、自动升级永远走不通的机器。
	g.POST("/connector/releases/rollback-push", adminmiddleware.RequirePermission("app"), apiPushConnectorRollback)
	g.POST("/connector/upgrade-notify", apiNotifyConnectorUpgrade)
	g.GET("/connector/releases/:id/rules", apiListConnectorRolloutRules)
	g.POST("/connector/rollout-rules", apiCreateConnectorRolloutRule)
	g.POST("/connector/rollout-rules/:id/toggle", apiToggleConnectorRolloutRule)
	g.DELETE("/connector/rollout-rules/:id", apiDeleteConnectorRolloutRule)
	g.GET("/connector/reports", apiListConnectorUpgradeReports)
	g.GET("/connector/reports/problem-users", apiListConnectorProblemUsers)
	// 发送类动作额外要求 app 权限（与其它触达入口一致），只读列表沿用 connector。
	g.POST("/connector/reports/notify/preview", adminmiddleware.RequirePermission("app"), apiPreviewConnectorNotify)
	g.POST("/connector/reports/notify", adminmiddleware.RequirePermission("app"), apiNotifyConnectorProblemUsers)
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
		ClientType string  `json:"client_type"`
		Version    string  `json:"version"`
		Channel    string  `json:"channel"`
		Changelog  string  `json:"changelog"`
		MinVersion *string `json:"min_version"`
		NpmPackage string  `json:"npm_package"`
		NpmTag     string  `json:"npm_tag"`
		Force      bool    `json:"force"`
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

func apiPublishConnectorRelease(c *gin.Context) {
	connectorReleaseAction(c, service.PublishConnectorRelease)
}
func apiPauseConnectorRelease(c *gin.Context) {
	connectorReleaseAction(c, service.PauseConnectorRelease)
}
func apiResumeConnectorRelease(c *gin.Context) {
	connectorReleaseAction(c, service.ResumeConnectorRelease)
}
func apiRevokeConnectorRelease(c *gin.Context) {
	connectorReleaseAction(c, service.RevokeConnectorRelease)
}

// 调整已发布版本的 min_version 门槛：min_version 为 null 表示清空。
func apiUpdateConnectorReleaseMinVersion(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "无效ID")
		return
	}
	var body struct {
		MinVersion *string `json:"min_version"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	updated, ec := service.UpdateConnectorReleaseMinVersion(id, body.MinVersion)
	if ec != nil {
		response.Fail(c, http.StatusBadRequest, 10006, ec.Msg)
		return
	}
	response.OK(c, gin.H{"release": updated})
}

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

// apiPushConnectorRollback 给指定 agent 定向下发 connector_rollback，让客户端直接
// npm install 目标版本并重启——不经 staged 事务与 guardian。用于抢救自动升级链路
// 已经坏掉、只能靠远端指令救回来的存量机器。
//
// 可靠性上做了四件事：目标版本必须已发布；只推给声明了该 local_action 的在线连接
// （由 SendLocalActionForOwner 把关）；逐 agent 回收真实送达结果；送达的打冷却，
// 避免重复推让同一台机器反复重装重启。
func apiPushConnectorRollback(c *gin.Context) {
	var body struct {
		AgentIDs      []string `json:"agent_ids"`
		TargetVersion string   `json:"target_version"`
		ClientType    string   `json:"client_type"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	targetVersion := strings.TrimSpace(body.TargetVersion)
	if targetVersion == "" {
		response.Fail(c, http.StatusBadRequest, 10002, "缺少 target_version")
		return
	}
	if !service.ConnectorReleaseIsPublished(body.ClientType, targetVersion) {
		response.Fail(c, http.StatusBadRequest, 10002, "目标版本不是已发布版本，拒绝下发")
		return
	}

	seen := make(map[int64]bool, len(body.AgentIDs))
	agentIDs := make([]int64, 0, len(body.AgentIDs))
	for _, raw := range body.AgentIDs {
		id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil || id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		agentIDs = append(agentIDs, id)
	}
	if len(agentIDs) == 0 {
		response.Fail(c, http.StatusBadRequest, 10002, "agent_ids 为空或全部非法")
		return
	}
	if len(agentIDs) > connectorRollbackPushMaxAgents {
		response.Fail(c, http.StatusBadRequest, 10002, "单次最多下发 "+strconv.Itoa(connectorRollbackPushMaxAgents)+" 个 agent")
		return
	}

	ctx := c.Request.Context()
	cooling := wsagentapi.ConnectorRollbackInCooldown(ctx, agentIDs)
	targets := make([]int64, 0, len(agentIDs))
	skipped := make([]string, 0)
	for _, id := range agentIDs {
		if cooling[id] {
			skipped = append(skipped, strconv.FormatInt(id, 10))
			continue
		}
		targets = append(targets, id)
	}
	if len(targets) == 0 {
		response.OK(c, gin.H{
			"target_version": targetVersion,
			"requested":      len(agentIDs),
			"dispatched":     []string{},
			"missed":         []string{},
			"skipped":        skipped,
		})
		return
	}

	pushID := strconv.FormatInt(snowflake.GenID(), 10)
	if err := wsagentapi.PublishConnectorRollbackPush(pushID, targetVersion, targets); err != nil {
		response.Fail(c, http.StatusServiceUnavailable, 10004, "Redis 不可用，无法下发 rollback")
		return
	}

	// 派发在各 ws 节点上异步完成，这里轮询送达集合。收齐即返回，收不齐也有上限，
	// 不能让 admin 请求吊在这儿。没收到的算 missed，可以等 agent 上线后重推。
	dispatched := pollConnectorRollbackDispatched(ctx, pushID, len(targets))
	wsagentapi.MarkConnectorRollbackCooldown(ctx, dispatched)

	got := make(map[int64]bool, len(dispatched))
	dispatchedOut := make([]string, 0, len(dispatched))
	for _, id := range dispatched {
		got[id] = true
		dispatchedOut = append(dispatchedOut, strconv.FormatInt(id, 10))
	}
	missed := make([]string, 0)
	for _, id := range targets {
		if !got[id] {
			missed = append(missed, strconv.FormatInt(id, 10))
		}
	}

	response.OK(c, gin.H{
		"push_id":        pushID,
		"target_version": targetVersion,
		"requested":      len(agentIDs),
		"dispatched":     dispatchedOut,
		"missed":         missed,
		"skipped":        skipped,
	})
}

// pollConnectorRollbackDispatched 在上限内等待各 ws 节点回写送达集合，收齐即返回。
func pollConnectorRollbackDispatched(ctx context.Context, pushID string, want int) []int64 {
	deadline := time.Now().Add(connectorRollbackPushWait)
	var last []int64
	for {
		got, err := wsagentapi.ConnectorRollbackDispatched(ctx, pushID)
		if err == nil {
			last = got
			if len(got) >= want {
				return got
			}
		}
		if time.Now().After(deadline) {
			return last
		}
		select {
		case <-ctx.Done():
			return last
		case <-time.After(connectorRollbackPushPollInterval):
		}
	}
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
	var body struct {
		Status int16 `json:"status"`
	}
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

// apiNotifyConnectorUpgrade 给仍在跑旧版本 connector 的用户发升级提醒（邮件/站内/短信按直达 reach 顺序尝试）。
// dry_run=true 只返回命中用户，供发送前确认。
func apiNotifyConnectorUpgrade(c *gin.Context) {
	var body service.ConnectorUpgradeNotifyReq
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	result, err := service.NotifyConnectorUpgrade(c.Request.Context(), body)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10006, err.Error())
		return
	}
	response.OK(c, result)
}

// apiListConnectorProblemUsers 返回指定版本上仍未自愈的问题机器所属用户。
// statuses 逗号分隔，缺省 failed,rolled_back；include_unsupported=1 时把
// WINDOWS_UPGRADE_UNSUPPORTED 也算进来。
func apiListConnectorProblemUsers(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	var statuses []string
	for _, s := range strings.Split(c.Query("statuses"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			statuses = append(statuses, s)
		}
	}
	includeUnsupported := c.Query("include_unsupported") == "1" || c.Query("include_unsupported") == "true"

	result, ec := service.ListConnectorProblemUsers(service.ListConnectorProblemUsersReq{
		Version:            c.Query("version"),
		ClientType:         c.Query("client_type"),
		Statuses:           statuses,
		IncludeUnsupported: includeUnsupported,
		Page:               page,
		PageSize:           pageSize,
	})
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	// 回显 clamp 之后真正生效的分页，而不是原样回请求参数。
	response.OK(c, gin.H{"users": result.Users, "total": result.Total, "page": result.Page, "page_size": result.PageSize})
}

// apiPreviewConnectorNotify 渲染发送前预览：邮件主题/正文 + 短信文案。
func apiPreviewConnectorNotify(c *gin.Context) {
	var body struct {
		Title        string `json:"title"`
		Body         string `json:"body"`
		SampleUserID int64  `json:"sample_user_id,string"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	preview, ec := service.PreviewConnectorNotify(body.Title, body.Body, body.SampleUserID)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, gin.H{"preview": preview})
}

// apiNotifyConnectorProblemUsers 手动向勾选的用户逐个发送升级失败告知。
func apiNotifyConnectorProblemUsers(c *gin.Context) {
	var body struct {
		Version string   `json:"version"`
		UserIDs []string `json:"user_ids"`
		Channel string   `json:"channel"`
		Title   string   `json:"title"`
		Body    string   `json:"body"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}
	userIDs := make([]int64, 0, len(body.UserIDs))
	for _, raw := range body.UserIDs {
		id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil || id <= 0 {
			response.Fail(c, http.StatusBadRequest, 10002, "无效用户 ID")
			return
		}
		userIDs = append(userIDs, id)
	}

	var createdBy int64
	if admin := adminmiddleware.CurrentAdmin(c); admin != nil {
		createdBy = admin.ID
	}
	results, ec := service.NotifyConnectorProblemUsers(c.Request.Context(), service.NotifyConnectorProblemUsersReq{
		Version:   body.Version,
		UserIDs:   userIDs,
		Channel:   body.Channel,
		Title:     body.Title,
		Body:      body.Body,
		CreatedBy: createdBy,
	})
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, gin.H{"results": results})
}
