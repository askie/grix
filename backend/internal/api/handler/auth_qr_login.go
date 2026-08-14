package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/i18n"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type qrCreateReq struct {
	DeviceLabel string `json:"device_label"`
}

func QRLoginCreate(c *gin.Context) {
	var req qrCreateReq
	_ = c.ShouldBindJSON(&req)

	respData, err := service.CreateQRLoginSession(c.ClientIP(), c.Request.UserAgent(), req.DeviceLabel)
	if err != nil {
		failByQRLoginError(c, err)
		return
	}
	response.OK(c, respData)
}

func QRLoginStatus(c *gin.Context) {
	sessionID := strings.TrimSpace(c.Query("session_id"))
	pollToken := strings.TrimSpace(c.Query("poll_token"))

	respData, err := service.QueryQRLoginStatus(sessionID, pollToken)
	if err != nil {
		failByQRLoginError(c, err)
		return
	}
	response.OK(c, respData)
}

type qrScanReq struct {
	RawPayload string `json:"raw_payload" binding:"required"`
}

func QRLoginScan(c *gin.Context) {
	var req qrScanReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	respData, err := service.ScanQRLoginSession(middleware.GetUserID(c), req.RawPayload)
	if err != nil {
		failByQRLoginError(c, err)
		return
	}
	response.OK(c, respData)
}

type qrConfirmReq struct {
	QRSessionID string `json:"qr_session_id" binding:"required"`
	Approve     bool   `json:"approve"`
}

func QRLoginConfirm(c *gin.Context) {
	var req qrConfirmReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	respData, err := service.ConfirmQRLoginSession(middleware.GetUserID(c), req.QRSessionID, req.Approve)
	if err != nil {
		failByQRLoginError(c, err)
		return
	}
	response.OK(c, respData)
}

type qrExchangeReq struct {
	QRSessionID string `json:"qr_session_id" binding:"required"`
	PollToken   string `json:"poll_token" binding:"required"`
	DeviceID    string `json:"device_id" binding:"required"`
	Platform    string `json:"platform" binding:"required"`
}

func QRLoginExchange(c *gin.Context) {
	var req qrExchangeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	lang := i18n.RequestLanguage(c)
	respData, err := service.ExchangeQRLoginSession(
		req.QRSessionID,
		req.PollToken,
		req.DeviceID,
		req.Platform,
		lang,
	)
	if err != nil {
		failByQRLoginError(c, err)
		return
	}
	response.OK(c, respData)
}

func failByQRLoginError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrQRLoginInvalidCode):
		response.Fail(c, http.StatusNotFound, 10004, err.Error())
	case errors.Is(err, service.ErrQRLoginRegionMismatch):
		// 10011：跨区二维码，客户端据此展示本地化的"切换区域"引导。
		response.Fail(c, http.StatusBadRequest, 10011, err.Error())
	case errors.Is(err, service.ErrQRLoginExpired):
		response.Fail(c, http.StatusBadRequest, 10003, err.Error())
	case errors.Is(err, service.ErrQRLoginNotReady):
		response.Fail(c, http.StatusBadRequest, 10003, err.Error())
	case errors.Is(err, service.ErrQRLoginAlreadyConsumed):
		response.Fail(c, http.StatusBadRequest, 10003, err.Error())
	case errors.Is(err, service.ErrQRLoginCanceled):
		response.Fail(c, http.StatusBadRequest, 10003, err.Error())
	case errors.Is(err, service.ErrQRLoginAlreadyConfirmed):
		response.Fail(c, http.StatusBadRequest, 10003, err.Error())
	case errors.Is(err, service.ErrQRLoginAlreadyScannedByPeer):
		response.Fail(c, http.StatusBadRequest, 10003, err.Error())
	case errors.Is(err, service.ErrQRLoginForbidden):
		response.Fail(c, http.StatusForbidden, 10001, err.Error())
	case errors.Is(err, service.ErrQRLoginCreateFailed):
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
	default:
		response.Fail(c, http.StatusInternalServerError, 50001, "系统错误")
	}
}
