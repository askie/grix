package handler

import (
	"net/http"
	"strconv"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// agent 自定义斜杠命令：主人在命令面板里自加的命令，随工具栏快照与内置命令一起下发。

// AgentSlashCommandList GET /v1/agents/:id/slash-commands
func AgentSlashCommandList(c *gin.Context) {
	agentID, ok := parseAgentIDParam(c)
	if !ok {
		return
	}
	items, ec := service.AgentSlashCommandList(middleware.GetUserID(c), agentID)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, gin.H{"items": items})
}

// AgentSlashCommandCreate POST /v1/agents/:id/slash-commands
func AgentSlashCommandCreate(c *gin.Context) {
	agentID, ok := parseAgentIDParam(c)
	if !ok {
		return
	}
	var req service.AgentSlashCommandCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	data, ec := service.AgentSlashCommandCreate(middleware.GetUserID(c), agentID, req)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

// AgentSlashCommandDelete DELETE /v1/agents/:id/slash-commands/:cmd_id
func AgentSlashCommandDelete(c *gin.Context) {
	agentID, ok := parseAgentIDParam(c)
	if !ok {
		return
	}
	commandID, err := strconv.ParseInt(c.Param("cmd_id"), 10, 64)
	if err != nil || commandID <= 0 {
		response.Fail(c, http.StatusBadRequest, 10003, "无效的命令 ID")
		return
	}
	if ec := service.AgentSlashCommandDelete(middleware.GetUserID(c), agentID, commandID); ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, nil)
}
