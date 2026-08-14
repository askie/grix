package handler

import (
	"net/http"
	"strconv"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type agentAssignCategoryReq struct {
	CategoryID *int64 `json:"category_id,string"`
}

// AgentAPICategoryList handles GET /v1/agent-api/agents/categories/list
func AgentAPICategoryList(c *gin.Context) {
	ownerID := middleware.GetOwnerID(c)
	data, ec := service.AgentCategoryList(ownerID)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, gin.H{"list": data})
}

// AgentAPICategoryCreate handles POST /v1/agent-api/agents/categories/create
func AgentAPICategoryCreate(c *gin.Context) {
	ownerID := middleware.GetOwnerID(c)
	var req service.AgentCategoryReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	data, ec := service.AgentCategoryCreate(ownerID, req)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

// AgentAPICategoryUpdate handles PUT /v1/agent-api/agents/categories/:id
func AgentAPICategoryUpdate(c *gin.Context) {
	ownerID := middleware.GetOwnerID(c)
	categoryID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "无效的分类ID")
		return
	}

	var req service.AgentCategoryReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	data, ec := service.AgentCategoryUpdate(ownerID, categoryID, req)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

// AgentAPIAgentAssignCategory handles PUT /v1/agent-api/agents/:id/category
func AgentAPIAgentAssignCategory(c *gin.Context) {
	ownerID := middleware.GetOwnerID(c)
	agentID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "无效的 Agent ID")
		return
	}

	var req agentAssignCategoryReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	if req.CategoryID == nil {
		response.Fail(c, http.StatusBadRequest, 10003, "category_id required")
		return
	}

	data, ec := service.AgentAssignCategory(ownerID, agentID, *req.CategoryID)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}
