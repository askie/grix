package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/i18n"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func SendChangePasswordEmailCode(c *gin.Context) {
	userID := middleware.GetUserID(c)
	lang := i18n.RequestLanguage(c)
	if err := service.SendChangePasswordEmailCode(userID, lang); err != nil {
		status := http.StatusBadRequest
		if !isUserPasswordBusinessError(err) {
			status = http.StatusInternalServerError
		}
		response.Fail(c, status, 10001, userPasswordErrorMessage(err))
		return
	}
	response.OK(c, nil)
}

type changeOwnPasswordReq struct {
	EmailCode   string `json:"email_code" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

func ChangeOwnPassword(c *gin.Context) {
	var req changeOwnPasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	userID := middleware.GetUserID(c)
	accessJTI := ""
	accessExp := time.Time{}
	claims := middleware.GetClaims(c)
	if claims != nil {
		accessJTI = claims.ID
		if claims.ExpiresAt != nil {
			accessExp = claims.ExpiresAt.Time
		}
	}

	if err := service.ChangeOwnPassword(userID, req.NewPassword, req.EmailCode, accessJTI, accessExp); err != nil {
		status := http.StatusBadRequest
		if !isUserPasswordBusinessError(err) {
			status = http.StatusInternalServerError
		}
		response.Fail(c, status, 10001, userPasswordErrorMessage(err))
		return
	}
	response.OK(c, nil)
}

func userPasswordErrorMessage(err error) string {
	if isUserPasswordBusinessError(err) {
		return err.Error()
	}
	return "修改密码失败，请稍后重试"
}

func isUserPasswordBusinessError(err error) bool {
	return errors.Is(err, service.ErrChangePasswordUserNotFound) ||
		errors.Is(err, service.ErrChangePasswordUserDisabled) ||
		errors.Is(err, service.ErrChangePasswordUserEmailAbsent) ||
		errors.Is(err, service.ErrChangePasswordCodeRequired) ||
		errors.Is(err, service.ErrChangePasswordCodeInvalid) ||
		errors.Is(err, service.ErrChangePasswordNewPasswordMiss) ||
		errors.Is(err, service.ErrChangePasswordHashFailed) ||
		errors.Is(err, service.ErrUserPasswordPolicy)
}
