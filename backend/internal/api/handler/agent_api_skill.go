package handler

import (
	"strconv"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// 自定义技能多机器同步的 agent-api（api_key 鉴权）侧。
// connector 以 agent 身份拉取该 owner 的技能包同步到本机 grix/skills，并上载/删除本地技能。
// owner 由 AgentAPIAuth 从 api_key 解出，与用户 JWT 侧共用同一份技能库与 service。

type agentSkillUpsertReq struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// AgentSkillList 处理 GET /v1/agent-api/skills —— connector 拉取技能清单摘要（含 version/digest）。
func AgentSkillList(c *gin.Context) {
	ownerID := middleware.GetOwnerID(c)
	data, ec := service.ListUserSkills(ownerID)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, gin.H{"items": data})
}

// AgentSkillGetContent 处理 GET /v1/agent-api/skills/:id/content —— 拉取单个技能全文。
func AgentSkillGetContent(c *gin.Context) {
	ownerID := middleware.GetOwnerID(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.ErrBadRequest.HTTPStatus, errcode.ErrBadRequest.BizCode, errcode.ErrBadRequest.Msg)
		return
	}
	skill, ec := service.GetUserSkillContent(ownerID, id)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, skill)
}

// AgentSkillUpload 处理 POST /v1/agent-api/skills/upload —— connector 上载本地技能（按名 upsert）。
func AgentSkillUpload(c *gin.Context) {
	ownerID := middleware.GetOwnerID(c)
	var req agentSkillUpsertReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errcode.ErrBadRequest.HTTPStatus, errcode.ErrBadRequest.BizCode, errcode.ErrBadRequest.Msg)
		return
	}
	skill, ec := service.UpsertUserSkillByName(ownerID, req.Name, req.Content)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, skill)
}

// AgentSkillDelete 处理 POST /v1/agent-api/skills/delete —— connector 删除某技能（按名，幂等）。
func AgentSkillDelete(c *gin.Context) {
	ownerID := middleware.GetOwnerID(c)
	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errcode.ErrBadRequest.HTTPStatus, errcode.ErrBadRequest.BizCode, errcode.ErrBadRequest.Msg)
		return
	}
	if ec := service.DeleteUserSkillByName(ownerID, req.Name); ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, gin.H{"deleted": true})
}
