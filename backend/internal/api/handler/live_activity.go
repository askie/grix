package handler

import (
	"errors"
	"net/http"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type liveActivityTokenReq struct {
	SessionID  string `json:"session_id" binding:"required"`
	ActivityID string `json:"activity_id" binding:"required"`
	Token      string `json:"token" binding:"required"`
	DeviceID   string `json:"device_id" binding:"required"`
}

// LiveActivityTokenBind 收下 iOS 端为某次 run 开出的实时活动的更新 token。
// 启动 token（push-to-start，每设备一个）走 /devices/bind，不从这里进。
func LiveActivityTokenBind(c *gin.Context) {
	var req liveActivityTokenReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	userID := middleware.GetUserID(c)
	err := service.SaveLiveActivityToken(
		c.Request.Context(),
		userID,
		req.SessionID,
		req.ActivityID,
		req.Token,
		req.DeviceID,
	)
	switch {
	case err == nil:
		response.OK(c, nil)
	case errors.Is(err, service.ErrLiveActivityInvalidRequest):
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
	case errors.Is(err, service.ErrLiveActivitySessionForbidden):
		response.Fail(c, http.StatusForbidden, 4003, "not the owner of this session")
	default:
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
	}
}
