package handler

import (
	"net/http"

	"github.com/askie/grix/backend/internal/notification"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// GetNotificationPrefs returns the caller's per-event notification preferences,
// seeding defaults on first read.
func GetNotificationPrefs(c *gin.Context) {
	userID := middleware.GetUserID(c)
	prefs, err := notification.GetPrefs(userID)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, prefs)
}

type updateNotificationPrefsReq struct {
	Prefs []notification.PrefView `json:"prefs" binding:"required"`
}

// UpdateNotificationPrefs upserts the caller's preferences. approval_requested
// stays force-enabled regardless of the request.
func UpdateNotificationPrefs(c *gin.Context) {
	var req updateNotificationPrefsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	userID := middleware.GetUserID(c)
	if err := notification.UpdatePrefs(userID, req.Prefs); err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}
