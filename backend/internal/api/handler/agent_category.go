package handler

import (
	"net/http"
	"strconv"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func AgentCategoryList(c *gin.Context) {
	userID := middleware.GetUserID(c)
	data, ec := service.AgentCategoryList(userID)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, gin.H{"list": data})
}

func AgentCategoryCreate(c *gin.Context) {
	var req service.AgentCategoryReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	userID := middleware.GetUserID(c)
	data, ec := service.AgentCategoryCreate(userID, req)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

func AgentCategoryUpdate(c *gin.Context) {
	userID := middleware.GetUserID(c)
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
	
	data, ec := service.AgentCategoryUpdate(userID, categoryID, req)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

func AgentCategoryDelete(c *gin.Context) {
	userID := middleware.GetUserID(c)
	categoryID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "无效的分类ID")
		return
	}

	ec := service.AgentCategoryDelete(userID, categoryID)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, nil)
}
