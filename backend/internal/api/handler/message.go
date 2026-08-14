package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	historysync "github.com/askie/grix/backend/internal/agentsync/orchestrator"
	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

var syncBoundSessionHistory = historysync.SyncBoundSessionHistory
var nativeHistorySyncWait = 5 * time.Second

type nativeHistorySyncResult struct {
	imported int
	err      error
}

func MessageHistory(c *gin.Context) {
	userID := middleware.GetUserID(c)
	sessionID := c.Query("session_id")
	if sessionID == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "session_id required")
		return
	}
	beforeID, _ := strconv.ParseInt(c.DefaultQuery("before_id", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	data, err := service.MessageHistory(userID, sessionID, beforeID, limit)
	if err == nil && beforeID == 0 {
		owned, ownerErr := service.SessionOwnedBy(userID, sessionID)
		if ownerErr != nil {
			logger.L.Warnf("message_history: native history owner lookup failed user=%d session=%s err=%v", userID, sessionID, ownerErr)
		} else if owned {
			imported, syncErr, finished := waitForNativeHistorySync(userID, sessionID)
			if syncErr != nil {
				logger.L.Warnf("message_history: native history sync failed user=%d session=%s err=%v", userID, sessionID, syncErr)
			}
			if finished && (syncErr == nil || imported > 0) {
				data, err = service.MessageHistory(userID, sessionID, beforeID, limit)
			}
		}
	}
	if err != nil {
		if errors.Is(err, service.ErrSessionGroupBanned) {
			response.Fail(c, http.StatusForbidden, 4003, err.Error())
			return
		}
		if errors.Is(err, service.ErrSessionNotFound) {
			response.Fail(c, http.StatusNotFound, 4004, err.Error())
			return
		}
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, data)
}

func waitForNativeHistorySync(userID int64, sessionID string) (int, error, bool) {
	resultCh := make(chan nativeHistorySyncResult, 1)
	syncHistory := syncBoundSessionHistory
	go func() {
		imported, err := syncHistory(context.Background(), userID, sessionID)
		resultCh <- nativeHistorySyncResult{imported: imported, err: err}
	}()

	timer := time.NewTimer(nativeHistorySyncWait)
	defer timer.Stop()
	select {
	case result := <-resultCh:
		return result.imported, result.err, true
	case <-timer.C:
		return 0, nil, false
	}
}

// MessageDelete handles POST /v1/messages/delete
func MessageDelete(c *gin.Context) {
	var req struct {
		SessionID string `json:"session_id" binding:"required"`
		MsgID     int64  `json:"msg_id,string" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	userID := middleware.GetUserID(c)
	err := service.DeleteMessage(c.Request.Context(), req.SessionID, req.MsgID, service.MessageDeleteActor{
		UserID: userID,
	})
	if err != nil {
		if errors.Is(err, service.ErrSessionGroupBanned) {
			response.Fail(c, http.StatusForbidden, 4003, err.Error())
			return
		}
		if errors.Is(err, service.ErrSessionNotFound) {
			response.Fail(c, http.StatusNotFound, 4004, err.Error())
			return
		}
		if err.Error() == "20008" || err.Error() == "20008: 无权删除该消息" {
			response.Fail(c, http.StatusForbidden, 20008, "无权删除该消息")
		} else {
			response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		}
		return
	}
	response.OK(c, gin.H{})
}

// MessageEdit handles POST /v1/messages/edit
func MessageEdit(c *gin.Context) {
	var req struct {
		SessionID string `json:"session_id" binding:"required"`
		MsgID     int64  `json:"msg_id,string" binding:"required"`
		Content   string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	userID := middleware.GetUserID(c)
	err := service.EditMessage(c.Request.Context(), req.SessionID, req.MsgID, service.MessageEditActor{
		UserID: userID,
	}, req.Content)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrSessionGroupBanned):
			response.Fail(c, http.StatusForbidden, 4003, err.Error())
		case errors.Is(err, service.ErrSessionNotFound), errors.Is(err, service.ErrMessageNotFound):
			response.Fail(c, http.StatusNotFound, 4004, "消息不存在")
		case errors.Is(err, service.ErrMessageContentEmpty):
			response.Fail(c, http.StatusBadRequest, 10003, "消息内容不能为空")
		case errors.Is(err, service.ErrMessageEditDenied):
			response.Fail(c, http.StatusForbidden, 20008, "无权编辑该消息")
		default:
			response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		}
		return
	}
	response.OK(c, gin.H{})
}
