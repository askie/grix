package handler

import (
	"net/http"
	"strings"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type renameSessionReq struct {
	SessionID string `json:"session_id" binding:"required"`
	Title     string `json:"title"`
}

type setGroupNicknameReq struct {
	SessionID string `json:"session_id" binding:"required"`
	Nickname  string `json:"nickname"`
}

type setSessionPinReq struct {
	SessionID string `json:"session_id" binding:"required"`
	IsPinned  *bool  `json:"is_pinned" binding:"required"`
}

type setSessionMuteReq struct {
	SessionID string `json:"session_id" binding:"required"`
	IsMuted   *bool  `json:"is_muted" binding:"required"`
}

func SessionRename(c *gin.Context) {
	var req renameSessionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "session_id required")
		return
	}

	userID := middleware.GetUserID(c)
	data, err := service.SessionRename(userID, sessionID, req.Title)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

func SessionSetGroupNickname(c *gin.Context) {
	var req setGroupNicknameReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "session_id required")
		return
	}

	userID := middleware.GetUserID(c)
	data, err := service.SessionSetGroupNickname(userID, sessionID, req.Nickname)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

func SessionSetPinned(c *gin.Context) {
	var req setSessionPinReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "session_id required")
		return
	}

	userID := middleware.GetUserID(c)
	data, err := service.SessionSetPinned(userID, sessionID, *req.IsPinned)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

func SessionSetMuted(c *gin.Context) {
	var req setSessionMuteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "session_id required")
		return
	}

	userID := middleware.GetUserID(c)
	data, err := service.SessionSetMuted(userID, sessionID, *req.IsMuted)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}
