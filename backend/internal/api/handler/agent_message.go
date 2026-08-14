package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// AgentMessageHistory handles GET /v1/agent-api/messages/history
// Authenticated via AgentAPIAuth middleware (Bearer api_key).
// Uses the agent's owner_id for permission checks (agent inherits owner's access).
func AgentMessageHistory(c *gin.Context) {
	ownerID := middleware.GetOwnerID(c)
	sessionID := c.Query("session_id")
	if sessionID == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "session_id required")
		return
	}
	beforeID, _ := strconv.ParseInt(c.DefaultQuery("before_id", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "1"))
	data, err := service.AgentMessageHistory(ownerID, sessionID, beforeID, limit)
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

// AgentMessageSearch handles GET /v1/agent-api/messages/search
// Authenticated via AgentAPIAuth middleware (Bearer api_key).
// Uses the agent's owner_id for permission checks (agent inherits owner's access).
func AgentMessageSearch(c *gin.Context) {
	ownerID := middleware.GetOwnerID(c)
	sessionID := c.Query("session_id")
	if sessionID == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "session_id required")
		return
	}
	keyword := c.Query("keyword")
	if keyword == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "keyword required")
		return
	}
	beforeID, _ := strconv.ParseInt(c.DefaultQuery("before_id", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	data, err := service.MessageSearch(ownerID, sessionID, keyword, beforeID, limit)
	if err != nil {
		if errors.Is(err, service.ErrSessionGroupBanned) {
			response.Fail(c, http.StatusForbidden, 4003, err.Error())
			return
		}
		if errors.Is(err, service.ErrSessionNotFound) {
			response.Fail(c, http.StatusNotFound, 4004, err.Error())
			return
		}
		if err.Error() == "keyword required" {
			response.Fail(c, http.StatusBadRequest, 10003, "keyword required")
			return
		}
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, data)
}

// AgentOSSPresign handles POST /v1/agent-api/oss/presign
// Generates presigned upload URLs with session-based media folder structure.
func AgentOSSPresign(c *gin.Context) {
	var req struct {
		SessionID   string `json:"session_id" binding:"required"`
		Filename    string `json:"filename" binding:"required"`
		ContentType string `json:"content_type" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	ownerID := middleware.GetOwnerID(c)
	agentID := middleware.GetAgentID(c)
	data, err := service.OSSPresignForSession(c.Request.Context(), req.SessionID, ownerID, agentID, req.Filename, req.ContentType)
	if err != nil {
		if errors.Is(err, service.ErrSessionGroupBanned) {
			response.Fail(c, http.StatusForbidden, 4003, err.Error())
			return
		}
		if errors.Is(err, service.ErrSessionPermissionDenied) {
			response.Fail(c, http.StatusForbidden, 4003, "无权访问该会话")
			return
		}
		if errors.Is(err, service.ErrSessionNotFound) {
			response.Fail(c, http.StatusNotFound, 4004, err.Error())
			return
		}
		if errors.Is(err, service.ErrInvalidUploadFilename) {
			response.Fail(c, http.StatusBadRequest, 10003, "文件名非法")
			return
		}
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, data)
}

// AgentMessageDelete handles POST /v1/agent-api/messages/delete
func AgentMessageDelete(c *gin.Context) {
	var req struct {
		SessionID string `json:"session_id" binding:"required"`
		MsgID     int64  `json:"msg_id,string" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	agentID := middleware.GetAgentID(c)
	ownerID := middleware.GetOwnerID(c)
	err := service.DeleteMessage(c.Request.Context(), req.SessionID, req.MsgID, service.MessageDeleteActor{
		UserID:  ownerID,
		AgentID: agentID,
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

// AgentMessageEdit handles POST /v1/agent-api/messages/edit
func AgentMessageEdit(c *gin.Context) {
	var req struct {
		SessionID string `json:"session_id" binding:"required"`
		MsgID     int64  `json:"msg_id,string" binding:"required"`
		Content   string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	agentID := middleware.GetAgentID(c)
	ownerID := middleware.GetOwnerID(c)
	err := service.EditMessage(c.Request.Context(), req.SessionID, req.MsgID, service.MessageEditActor{
		UserID:  ownerID,
		AgentID: agentID,
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
