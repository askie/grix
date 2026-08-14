package admin

import (
	"net/http"
	"strconv"

	adminmiddleware "github.com/askie/grix/backend/internal/admin/middleware"
	apiservice "github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// registerAgentSecurityAPIRoutes 注册 agent WS 连接安全管控接口（阶段0）。
// 与用户侧 /v1/agents/:id/* 同一套服务实现，admin 免属主校验，
// 且 agent_id=0 可管理全局 IP 规则。
func registerAgentSecurityAPIRoutes(g *gin.RouterGroup) {
	sec := g.Group("/agent-security/:agent_id")
	{
		sec.GET("/connections", apiAdminAgentConnections)
		sec.GET("/connection-logs", apiAdminAgentConnectionLogs)
		sec.POST("/connections/kick", apiAdminAgentConnectionKick)
		sec.GET("/ip-rules", apiAdminAgentIPRuleList)
		sec.POST("/ip-rules", apiAdminAgentIPRuleCreate)
		sec.DELETE("/ip-rules/:rule_id", apiAdminAgentIPRuleDelete)
	}
}

func adminAgentSecurityAuditMeta(c *gin.Context) apiservice.AuditMeta {
	meta := apiservice.AuditMeta{
		ClientIP:  c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	}
	if admin := adminmiddleware.CurrentAdmin(c); admin != nil {
		meta.ActorID = admin.ID
	}
	return meta
}

// parseAdminAgentID 解析 agent_id；allowZero 时 0 表示全局（仅 IP 规则接口）。
func parseAdminAgentID(c *gin.Context, allowZero bool) (int64, bool) {
	agentID, err := strconv.ParseInt(c.Param("agent_id"), 10, 64)
	if err != nil || agentID < 0 || (agentID == 0 && !allowZero) {
		response.Fail(c, http.StatusBadRequest, 10003, "无效的 Agent ID")
		return 0, false
	}
	return agentID, true
}

func apiAdminAgentConnections(c *gin.Context) {
	agentID, ok := parseAdminAgentID(c, false)
	if !ok {
		return
	}
	response.OK(c, gin.H{"items": apiservice.AgentOnlineConnectionsForAdmin(agentID)})
}

func apiAdminAgentConnectionLogs(c *gin.Context) {
	agentID, ok := parseAdminAgentID(c, false)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	onlyGeoChanged := c.Query("geo_changed") == "1"
	data, ec := apiservice.AgentConnectionLogListForAdmin(agentID, page, pageSize, onlyGeoChanged)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

func apiAdminAgentConnectionKick(c *gin.Context) {
	agentID, ok := parseAdminAgentID(c, false)
	if !ok {
		return
	}
	var req apiservice.AgentConnectionKickReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	data, ec := apiservice.AgentConnectionKickForAdmin(agentID, req, adminAgentSecurityAuditMeta(c))
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

func apiAdminAgentIPRuleList(c *gin.Context) {
	agentID, ok := parseAdminAgentID(c, true)
	if !ok {
		return
	}
	data, ec := apiservice.AgentIPRuleListForAdmin(agentID)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, gin.H{"items": data})
}

func apiAdminAgentIPRuleCreate(c *gin.Context) {
	agentID, ok := parseAdminAgentID(c, true)
	if !ok {
		return
	}
	var req apiservice.AgentIPRuleCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	data, ec := apiservice.AgentIPRuleCreateForAdmin(agentID, req, adminAgentSecurityAuditMeta(c))
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

func apiAdminAgentIPRuleDelete(c *gin.Context) {
	agentID, ok := parseAdminAgentID(c, true)
	if !ok {
		return
	}
	ruleID, err := strconv.ParseInt(c.Param("rule_id"), 10, 64)
	if err != nil || ruleID <= 0 {
		response.Fail(c, http.StatusBadRequest, 10003, "无效的规则 ID")
		return
	}
	if ec := apiservice.AgentIPRuleDeleteForAdmin(agentID, ruleID, adminAgentSecurityAuditMeta(c)); ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, nil)
}
