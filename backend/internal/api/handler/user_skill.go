package handler

import (
	"strconv"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// 自定义技能多机器同步（docs/architecture/38）：技能库 CRUD + 上载 + connector 拉取。
// 全部按当前登录用户（owner）隔离；系统内置技能（owner_id=0）只读。

type skillUpsertReq struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// SkillList 处理 GET /v1/skills —— 列出本 owner + 系统内置技能摘要（不含正文）。
func SkillList(c *gin.Context) {
	ownerID := middleware.GetUserID(c)
	data, ec := service.ListUserSkills(ownerID)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, gin.H{"items": data})
}

// SkillCreate 处理 POST /v1/skills —— 新建技能，同名报错。
func SkillCreate(c *gin.Context) {
	ownerID := middleware.GetUserID(c)
	var req skillUpsertReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errcode.ErrBadRequest.HTTPStatus, errcode.ErrBadRequest.BizCode, errcode.ErrBadRequest.Msg)
		return
	}
	skill, ec := service.CreateUserSkill(ownerID, req.Name, req.Content)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, skill)
}

// SkillUpdate 处理 PUT /v1/skills/:id —— 更新本 owner 的技能。
func SkillUpdate(c *gin.Context) {
	ownerID := middleware.GetUserID(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.ErrBadRequest.HTTPStatus, errcode.ErrBadRequest.BizCode, errcode.ErrBadRequest.Msg)
		return
	}
	var req skillUpsertReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errcode.ErrBadRequest.HTTPStatus, errcode.ErrBadRequest.BizCode, errcode.ErrBadRequest.Msg)
		return
	}
	skill, ec := service.UpdateUserSkill(ownerID, id, req.Name, req.Content)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, skill)
}

// SkillDelete 处理 DELETE /v1/skills/:id —— 删除本 owner 的技能。
func SkillDelete(c *gin.Context) {
	ownerID := middleware.GetUserID(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.ErrBadRequest.HTTPStatus, errcode.ErrBadRequest.BizCode, errcode.ErrBadRequest.Msg)
		return
	}
	if ec := service.DeleteUserSkill(ownerID, id); ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, gin.H{"deleted": true})
}

// SkillGetContent 处理 GET /v1/skills/:id/content —— connector 拉取技能全文。
func SkillGetContent(c *gin.Context) {
	ownerID := middleware.GetUserID(c)
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

// SkillUpload 处理 POST /v1/skills/upload —— connector 从本地上载技能（按名 upsert）。
func SkillUpload(c *gin.Context) {
	ownerID := middleware.GetUserID(c)
	var req skillUpsertReq
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
