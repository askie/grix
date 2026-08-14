package handler

import (
	"net/http"
	"strconv"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// agent WS 连接安全管控（阶段0）：在线连接 / 连接日志 / 踢线 / IP 黑白名单。

func agentSecurityAuditMeta(c *gin.Context) service.AuditMeta {
	return service.AuditMeta{
		ActorID:   middleware.GetUserID(c),
		ClientIP:  c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	}
}

func parseAgentIDParam(c *gin.Context) (int64, bool) {
	agentID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || agentID <= 0 {
		response.Fail(c, http.StatusBadRequest, 10003, "无效的 Agent ID")
		return 0, false
	}
	return agentID, true
}

// AgentConnectionsOnline GET /v1/agents/:id/connections
func AgentConnectionsOnline(c *gin.Context) {
	agentID, ok := parseAgentIDParam(c)
	if !ok {
		return
	}
	data, ec := service.AgentOnlineConnections(middleware.GetUserID(c), agentID)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, gin.H{"items": data})
}

// AgentConnectionLogs GET /v1/agents/:id/connection-logs?page=&page_size=&geo_changed=1
func AgentConnectionLogs(c *gin.Context) {
	agentID, ok := parseAgentIDParam(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	onlyGeoChanged := c.Query("geo_changed") == "1"
	data, ec := service.AgentConnectionLogList(middleware.GetUserID(c), agentID, page, pageSize, onlyGeoChanged)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

// AgentConnectionKick POST /v1/agents/:id/connections/kick
func AgentConnectionKick(c *gin.Context) {
	agentID, ok := parseAgentIDParam(c)
	if !ok {
		return
	}
	var req service.AgentConnectionKickReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	data, ec := service.AgentConnectionKick(middleware.GetUserID(c), agentID, req, agentSecurityAuditMeta(c))
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

// AgentIPRuleList GET /v1/agents/:id/ip-rules
func AgentIPRuleList(c *gin.Context) {
	agentID, ok := parseAgentIDParam(c)
	if !ok {
		return
	}
	data, ec := service.AgentIPRuleListSvc(middleware.GetUserID(c), agentID)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, gin.H{"items": data})
}

// AgentIPRuleCreate POST /v1/agents/:id/ip-rules
func AgentIPRuleCreate(c *gin.Context) {
	agentID, ok := parseAgentIDParam(c)
	if !ok {
		return
	}
	var req service.AgentIPRuleCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	data, ec := service.AgentIPRuleCreate(middleware.GetUserID(c), agentID, req, agentSecurityAuditMeta(c))
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

// AgentIPRuleDelete DELETE /v1/agents/:id/ip-rules/:rule_id
func AgentIPRuleDelete(c *gin.Context) {
	agentID, ok := parseAgentIDParam(c)
	if !ok {
		return
	}
	ruleID, err := strconv.ParseInt(c.Param("rule_id"), 10, 64)
	if err != nil || ruleID <= 0 {
		response.Fail(c, http.StatusBadRequest, 10003, "无效的规则 ID")
		return
	}
	if ec := service.AgentIPRuleDelete(middleware.GetUserID(c), agentID, ruleID, agentSecurityAuditMeta(c)); ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, nil)
}
