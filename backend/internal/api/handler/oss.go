package handler

import (
	"errors"
	"net/http"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type presignReq struct {
	Filename    string `json:"filename" binding:"required"`
	ContentType string `json:"content_type" binding:"required"`
}

type deleteObjectsReq struct {
	ObjectKeys []string `json:"object_keys" binding:"required"`
}

func OSSPresign(c *gin.Context) {
	var req presignReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	userID := middleware.GetUserID(c)
	data, err := service.OSSPresign(userID, req.Filename, req.ContentType)
	if err != nil {
		if errors.Is(err, service.ErrInvalidUploadFilename) {
			response.Fail(c, http.StatusBadRequest, 10003, "文件名非法")
			return
		}
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, data)
}

func OSSDeleteObjects(c *gin.Context) {
	var req deleteObjectsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	userID := middleware.GetUserID(c)
	if err := service.DeleteUserMediaObjects(userID, req.ObjectKeys); err != nil {
		if errors.Is(err, service.ErrMediaObjectForbidden) {
			response.Fail(c, http.StatusForbidden, 4003, "无权删除该对象")
			return
		}
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, gin.H{})
}
