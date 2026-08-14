package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type addMembersReq struct {
	SessionID   string   `json:"session_id" binding:"required"`
	MemberIDs   []string `json:"member_ids" binding:"required"`
	MemberTypes []int16  `json:"member_types"`
}

type updateInviteSettingReq struct {
	SessionID         string `json:"session_id" binding:"required"`
	AllowMemberInvite *bool  `json:"allow_member_invite" binding:"required"`
}

type updateAllMembersMutedReq struct {
	SessionID       string `json:"session_id" binding:"required"`
	AllMembersMuted *bool  `json:"all_members_muted" binding:"required"`
}

type removeMembersReq struct {
	SessionID   string   `json:"session_id" binding:"required"`
	MemberIDs   []string `json:"member_ids" binding:"required"`
	MemberTypes []int16  `json:"member_types"`
}

type leaveGroupReq struct {
	SessionID string `json:"session_id" binding:"required"`
}

type updateMemberRoleReq struct {
	SessionID  string `json:"session_id" binding:"required"`
	MemberID   string `json:"member_id" binding:"required"`
	MemberType int16  `json:"member_type"`
	Role       int16  `json:"role" binding:"required"`
}

type updateMemberSpeakingReq struct {
	SessionID            string `json:"session_id" binding:"required"`
	MemberID             string `json:"member_id" binding:"required"`
	MemberType           int16  `json:"member_type"`
	IsSpeakMuted         *bool  `json:"is_speak_muted"`
	CanSpeakWhenAllMuted *bool  `json:"can_speak_when_all_muted"`
}

type updateMemberAgentReceiveReq struct {
	SessionID                string `json:"session_id" binding:"required"`
	MemberID                 string `json:"member_id" binding:"required"`
	MemberType               int16  `json:"member_type"`
	AgentReceiveMode         int16  `json:"agent_receive_mode" binding:"required"`
	AgentReceiveBacklogCount int    `json:"agent_receive_backlog_count"`
}

type transferOwnerReq struct {
	SessionID string `json:"session_id" binding:"required"`
	MemberID  string `json:"member_id" binding:"required"`
}

type dissolveGroupReq struct {
	SessionID string `json:"session_id" binding:"required"`
}

func SessionAddMembers(c *gin.Context) {
	var req addMembersReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "session_id required")
		return
	}
	if len(req.MemberIDs) == 0 {
		response.Fail(c, http.StatusBadRequest, 10003, "member_ids required")
		return
	}

	memberIDs, err := parseMemberIDs(req.MemberIDs)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, err.Error())
		return
	}

	userID := middleware.GetUserID(c)
	data, err := service.SessionAddMembers(userID, sessionID, memberIDs, req.MemberTypes)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

func SessionUpdateInviteSetting(c *gin.Context) {
	var req updateInviteSettingReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "session_id required")
		return
	}
	if req.AllowMemberInvite == nil {
		response.Fail(c, http.StatusBadRequest, 10003, "allow_member_invite required")
		return
	}

	userID := middleware.GetUserID(c)
	data, err := service.SessionUpdateInviteSetting(userID, sessionID, *req.AllowMemberInvite)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

func SessionUpdateAllMembersMuted(c *gin.Context) {
	var req updateAllMembersMutedReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "session_id required")
		return
	}
	if req.AllMembersMuted == nil {
		response.Fail(c, http.StatusBadRequest, 10003, "all_members_muted required")
		return
	}

	userID := middleware.GetUserID(c)
	data, err := service.SessionUpdateAllMembersMuted(userID, sessionID, *req.AllMembersMuted)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

func SessionRemoveMembers(c *gin.Context) {
	var req removeMembersReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "session_id required")
		return
	}
	if len(req.MemberIDs) == 0 {
		response.Fail(c, http.StatusBadRequest, 10003, "member_ids required")
		return
	}
	memberIDs, err := parseMemberIDs(req.MemberIDs)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, err.Error())
		return
	}

	userID := middleware.GetUserID(c)
	data, err := service.SessionRemoveMembers(userID, sessionID, memberIDs, req.MemberTypes)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

func SessionLeave(c *gin.Context) {
	var req leaveGroupReq
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
	data, err := service.SessionLeave(userID, sessionID)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

func SessionUpdateMemberRole(c *gin.Context) {
	var req updateMemberRoleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "session_id required")
		return
	}
	memberID, err := strconv.ParseInt(strings.TrimSpace(req.MemberID), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, service.ErrInvalidMemberID.Error())
		return
	}

	userID := middleware.GetUserID(c)
	data, err := service.SessionUpdateMemberRole(userID, sessionID, memberID, req.MemberType, req.Role)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

func SessionUpdateMemberSpeaking(c *gin.Context) {
	var req updateMemberSpeakingReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "session_id required")
		return
	}
	memberID, err := strconv.ParseInt(strings.TrimSpace(req.MemberID), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, service.ErrInvalidMemberID.Error())
		return
	}

	userID := middleware.GetUserID(c)
	data, err := service.SessionUpdateMemberSpeaking(
		userID,
		sessionID,
		memberID,
		req.MemberType,
		req.IsSpeakMuted,
		req.CanSpeakWhenAllMuted,
	)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

func SessionUpdateMemberAgentReceive(c *gin.Context) {
	var req updateMemberAgentReceiveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "session_id required")
		return
	}
	memberID, err := strconv.ParseInt(strings.TrimSpace(req.MemberID), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, service.ErrInvalidMemberID.Error())
		return
	}

	userID := middleware.GetUserID(c)
	data, err := service.SessionUpdateMemberAgentReceiveSetting(
		userID,
		sessionID,
		memberID,
		req.MemberType,
		req.AgentReceiveMode,
		req.AgentReceiveBacklogCount,
	)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

func SessionTransferOwner(c *gin.Context) {
	var req transferOwnerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "session_id required")
		return
	}
	memberID, err := strconv.ParseInt(strings.TrimSpace(req.MemberID), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, service.ErrInvalidMemberID.Error())
		return
	}

	userID := middleware.GetUserID(c)
	data, err := service.SessionTransferOwner(userID, sessionID, memberID)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

func SessionDissolve(c *gin.Context) {
	var req dissolveGroupReq
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
	data, err := service.SessionDissolve(userID, sessionID)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}
