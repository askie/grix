package handler

import (
	"net/http"
	"strconv"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/i18n"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type agentScopeReplaceReq struct {
	Scopes *[]string `json:"scopes"`
}

func AgentScopeGet(c *gin.Context) {
	userID := middleware.GetUserID(c)
	agentID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "无效的 Agent ID")
		return
	}

	data, ec := service.AgentScopeGet(userID, agentID, i18n.RequestAppLanguage(c))
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

func AgentScopeReplace(c *gin.Context) {
	userID := middleware.GetUserID(c)
	agentID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "无效的 Agent ID")
		return
	}

	var req agentScopeReplaceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	if req.Scopes == nil {
		response.Fail(c, http.StatusBadRequest, 10003, "scopes required")
		return
	}

	data, ec := service.AgentScopeReplace(userID, agentID, *req.Scopes, i18n.RequestAppLanguage(c))
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}
