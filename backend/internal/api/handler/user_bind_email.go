package handler

import (
	"net/http"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/i18n"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type sendBindEmailCodeReq struct {
	Email string `json:"email" binding:"required,email"`
}

// SendBindEmailCode POST /v1/users/bind-email/code（需鉴权）
func SendBindEmailCode(c *gin.Context) {
	var req sendBindEmailCodeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	userID := middleware.GetUserID(c)
	lang := i18n.RequestLanguage(c)
	if err := service.SendBindEmailCode(userID, c.ClientIP(), req.Email, lang); err != nil {
		failBindEmail(c, err)
		return
	}
	response.OK(c, nil)
}

type bindEmailReq struct {
	Email string `json:"email" binding:"required,email"`
	Code  string `json:"code" binding:"required"`
}

// BindEmail POST /v1/users/bind-email（需鉴权）
func BindEmail(c *gin.Context) {
	var req bindEmailReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	userID := middleware.GetUserID(c)
	if err := service.BindUserEmail(userID, req.Email, req.Code); err != nil {
		failBindEmail(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func failBindEmail(c *gin.Context, err error) {
	if service.IsBindEmailBusinessError(err) {
		response.Fail(c, http.StatusBadRequest, 10005, err.Error())
		return
	}
	response.Fail(c, http.StatusInternalServerError, 10001, "绑定失败，请稍后再试")
}
