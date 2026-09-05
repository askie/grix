package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type bindReq struct {
	Platform    string `json:"platform" binding:"required"`
	PushEnv     string `json:"push_env" binding:"required"`
	DeviceToken string `json:"device_token" binding:"required"`
	DeviceID    string `json:"device_id" binding:"required"`
	// ChannelTrail 记录客户端推送通道降级链上失败过的通道及原因（如
	// "android_huawei:timeout"），非空时说明最终通道不是设备本该走的首选通道。
	ChannelTrail string `json:"channel_trail"`
	// LiveActivityToken 是 ActivityKit 的 push-to-start token。iOS 端拿到后随下一次
	// 设备注册捎带上来，省一次独立请求；其它平台不带这个字段。
	LiveActivityToken string `json:"live_activity_token"`
}

func DeviceBind(c *gin.Context) {
	var req bindReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	userID := middleware.GetUserID(c)
	err := service.DeviceBind(userID, req.Platform, req.PushEnv, req.DeviceToken, req.DeviceID, req.ChannelTrail)
	if err != nil {
		if service.IsInvalidDeviceBinding(err) {
			response.Fail(c, http.StatusBadRequest, 10003, err.Error())
			return
		}
		if service.IsPushChannelDisabled(err) {
			// 10012：该推送通道被塘主关闭。客户端据此把这条通道排除后
			// 沿降级链继续取下一条通道的 token 重新注册（如 android_fcm -> android_jpush）。
			response.FailWithData(c, http.StatusConflict, 10012, "push channel disabled", gin.H{
				"disabled_platform": strings.TrimSpace(req.Platform),
			})
			return
		}
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	// 绑定成功之后再写：push-to-start token 挂在设备行上，行得先存在。
	// 写失败不让整次注册失败——普通推送已经绑好了，实时活动下次注册会补上。
	if err := service.SaveDeviceLiveActivityStartToken(userID, req.DeviceID, req.LiveActivityToken); err != nil {
		logger.L.Warnf("save live activity start token failed user=%d device=%s err=%v", userID, req.DeviceID, err)
	}
	response.OK(c, nil)
}

func DeviceSessionList(c *gin.Context) {
	userID := middleware.GetUserID(c)
	currentSessionID := ""
	if claims := middleware.GetClaims(c); claims != nil {
		currentSessionID = claims.SessionID
	}

	items, err := service.ListLoginDeviceSessions(userID, currentSessionID)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, gin.H{"items": items})
}

func DeviceSessionRemove(c *gin.Context) {
	userID := middleware.GetUserID(c)
	sessionID := c.Param("session_id")
	if err := service.RevokeLoginDeviceSession(userID, sessionID); err != nil {
		switch {
		case errors.Is(err, service.ErrLoginDeviceSessionIDRequired):
			response.Fail(c, http.StatusBadRequest, 10003, err.Error())
		case errors.Is(err, service.ErrLoginDeviceSessionNotFound):
			response.Fail(c, http.StatusNotFound, 10004, "设备不存在")
		default:
			response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		}
		return
	}
	response.OK(c, nil)
}
