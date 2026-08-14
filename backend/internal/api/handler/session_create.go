package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type createSessionReq struct {
	PeerID   int64 `json:"peer_id,string" binding:"required"`
	PeerType int16 `json:"peer_type" binding:"required"`
}

type createGroupReq struct {
	Name        string   `json:"name" binding:"required"`
	MemberIDs   []string `json:"member_ids"`
	MemberTypes []int16  `json:"member_types"`
}

type joinGroupByQRReq struct {
	Code string `json:"code" binding:"required"`
}

func SessionCreate(c *gin.Context) {
	var req createSessionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	userID := middleware.GetUserID(c)
	data, err := service.SessionCreate(userID, req.PeerID, req.PeerType)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

func SessionOpenLatest(c *gin.Context) {
	var req createSessionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	userID := middleware.GetUserID(c)
	data, err := service.SessionOpenLatest(userID, req.PeerID, req.PeerType)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

func SessionCreateGroup(c *gin.Context) {
	var req createGroupReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	groupName := strings.TrimSpace(req.Name)
	if groupName == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "name required")
		return
	}

	memberIDs, err := parseMemberIDs(req.MemberIDs)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, err.Error())
		return
	}
	userID := middleware.GetUserID(c)
	data, err := service.SessionCreateGroup(userID, groupName, memberIDs, req.MemberTypes)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

type convertToGroupReq struct {
	SessionID string `json:"session_id" binding:"required"`
	Name      string `json:"name"`
}

func SessionConvertToGroup(c *gin.Context) {
	var req convertToGroupReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	userID := middleware.GetUserID(c)
	data, err := service.SessionConvertToGroup(userID, req.SessionID, req.Name)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

func SessionGroupQRCodeGet(c *gin.Context) {
	sessionID := strings.TrimSpace(c.Query("session_id"))
	if sessionID == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "session_id required")
		return
	}

	userID := middleware.GetUserID(c)
	data, err := service.GetOrCreateGroupQRCode(userID, sessionID)
	if err != nil {
		if errors.Is(err, service.ErrSessionNotFound) {
			response.Fail(c, http.StatusNotFound, 4004, err.Error())
			return
		}
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

func SessionGroupQRCodeResolve(c *gin.Context) {
	code := strings.TrimSpace(c.Param("code"))
	if code == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "invalid qr code")
		return
	}

	userID := middleware.GetUserID(c)
	data, err := service.ResolveGroupQRCode(userID, code)
	if err != nil {
		if service.IsGroupQRCodeNotFound(err) {
			response.Fail(c, http.StatusNotFound, 10004, "二维码无效")
			return
		}
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

func SessionJoinGroupByQRCode(c *gin.Context) {
	var req joinGroupByQRReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	code := strings.TrimSpace(req.Code)
	if code == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "code required")
		return
	}

	userID := middleware.GetUserID(c)
	data, err := service.JoinGroupByQRCode(userID, code)
	if err != nil {
		if service.IsGroupQRCodeNotFound(err) {
			response.Fail(c, http.StatusNotFound, 10004, "二维码无效")
			return
		}
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}
