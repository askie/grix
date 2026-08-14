package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func SessionList(c *gin.Context) {
	userID := middleware.GetUserID(c)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	data, err := service.SessionList(userID, limit, offset)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, data)
}

func SessionSync(c *gin.Context) {
	userID := middleware.GetUserID(c)
	since, _ := strconv.ParseInt(c.DefaultQuery("since", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	data, err := service.SessionSync(userID, since, limit)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, data)
}

func SessionConversations(c *gin.Context) {
	userID := middleware.GetUserID(c)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
	cursor := c.Query("cursor")
	data, err := service.SessionConversations(userID, limit, cursor)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, data)
}

func SessionConversationThreads(c *gin.Context) {
	userID := middleware.GetUserID(c)
	groupKey := c.Query("group_key")
	if groupKey == "" {
		peerTypeStr := c.Query("peer_type")
		peerIDStr := c.Query("peer_id")
		if peerTypeStr != "" && peerIDStr != "" {
			pt, ptErr := strconv.Atoi(peerTypeStr)
			pi, piErr := strconv.ParseInt(peerIDStr, 10, 64)
			if ptErr == nil && piErr == nil && pt > 0 && pi > 0 {
				groupKey = fmt.Sprintf("private:%d:%d", pt, pi)
			}
		}
	}
	if groupKey == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "group_key or peer_type+peer_id required")
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	cursor := c.Query("cursor")
	data, err := service.SessionConversationThreads(userID, groupKey, limit, cursor)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

func SessionDetail(c *gin.Context) {
	userID := middleware.GetUserID(c)
	sessionID := c.Query("session_id")
	if sessionID == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "session_id required")
		return
	}

	data, err := service.SessionDetail(userID, sessionID)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}
