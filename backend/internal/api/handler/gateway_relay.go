// Package handler 的这个文件是"Grix中转"模型设置的 C端 HTTP 入口：
// 可用模型清单（含单价）、兜底模型与模型映射表的读写、我名下Agent的接入状态。
package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
)

// GatewayListModels handles GET /v1/gateway/models —— 后端当前支持的模型 + 单价。
func GatewayListModels(c *gin.Context) {
	data, ec := service.GatewayListModels()
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

// GatewayGetRelaySettings handles GET /v1/gateway/relay-settings.
func GatewayGetRelaySettings(c *gin.Context) {
	userID := middleware.GetUserID(c)
	data, ec := service.GatewayGetRelaySettings(userID)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

// GatewayPutRelaySettings handles PUT /v1/gateway/relay-settings.
// 保存即生效：网关每次请求都按这份设置解析模型，不需要给 connector 下发任何东西。
func GatewayPutRelaySettings(c *gin.Context) {
	var req service.GatewayPutRelaySettingsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 400, "参数错误")
		return
	}
	userID := middleware.GetUserID(c)
	data, ec := service.GatewayPutRelaySettings(userID, req)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

// GatewayListAgents handles GET /v1/gateway/agents —— 我名下托管Agent的中转接入状态。
func GatewayListAgents(c *gin.Context) {
	userID := middleware.GetUserID(c)
	data, ec := service.GatewayListAgents(userID)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

// GatewaySetAgentRelay handles POST /v1/gateway/agents/:agent_id/relay ——
// 设置某个托管Agent的中转开关（desired 期望态，migration 111）。
// body: {"enabled": bool, "model": "...", "expected_revision": 3}，后两个字段可选。
func GatewaySetAgentRelay(c *gin.Context) {
	agentID, err := strconv.ParseInt(c.Param("agent_id"), 10, 64)
	if err != nil || agentID <= 0 {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	var req struct {
		Enabled          bool   `json:"enabled"`
		Model            string `json:"model"`
		ExpectedRevision *int64 `json:"expected_revision"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	userID := middleware.GetUserID(c)
	data, ec := service.GatewaySetAgentRelay(c.Request.Context(), userID, agentID, req.Enabled, req.Model, req.ExpectedRevision)
	if ec != nil {
		// 409 冲突时 data 带回最新 state，前端刷新后按新 revision 重试（设计 §2.3 并发控制）。
		if data != nil {
			response.FailWithData(c, ec.HTTPStatus, ec.BizCode, ec.Msg, data)
			return
		}
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}
