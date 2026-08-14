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

type agentCreateSessionReq struct {
	PeerID   int64 `json:"peer_id,string" binding:"required"`
	PeerType int16 `json:"peer_type" binding:"required"`
}

type agentCreateGroupReq struct {
	Name        string   `json:"name" binding:"required"`
	MemberIDs   []string `json:"member_ids"`
	MemberTypes []int16  `json:"member_types"`
}

// AgentSessionCreate handles POST /v1/agent-api/sessions/create
func AgentSessionCreate(c *gin.Context) {
	ownerID := middleware.GetOwnerID(c)
	var req agentCreateSessionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	data, err := service.SessionCreate(ownerID, req.PeerID, req.PeerType)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

// AgentSessionCreateGroup handles POST /v1/agent-api/sessions/create_group
func AgentSessionCreateGroup(c *gin.Context) {
	agentID := middleware.GetAgentID(c)
	ownerID := middleware.GetOwnerID(c)
	var req agentCreateGroupReq
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

	data, err := service.SessionCreateGroupByAgent(ownerID, agentID, groupName, memberIDs, req.MemberTypes)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

// AgentSessionOpenLatest handles POST /v1/agent-api/sessions/open_latest
func AgentSessionOpenLatest(c *gin.Context) {
	ownerID := middleware.GetOwnerID(c)
	var req agentCreateSessionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	data, err := service.SessionOpenLatest(ownerID, req.PeerID, req.PeerType)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

// AgentSessionLeave handles POST /v1/agent-api/sessions/leave
func AgentSessionLeave(c *gin.Context) {
	agentID := middleware.GetAgentID(c)
	ownerID := middleware.GetOwnerID(c)
	var req struct {
		SessionID string `json:"session_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	data, err := service.SessionLeaveByAgent(agentID, ownerID, req.SessionID)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

// AgentSessionSearch handles GET /v1/agent-api/sessions/search
func AgentSessionSearch(c *gin.Context) {
	ownerID := middleware.GetOwnerID(c)
	id := strings.TrimSpace(c.Query("id"))
	keyword := strings.TrimSpace(c.Query("keyword"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	sessionTypeRaw, _ := strconv.Atoi(c.Query("session_type"))
	var sessionType int16
	if sessionTypeRaw == 1 || sessionTypeRaw == 2 {
		sessionType = int16(sessionTypeRaw)
	}

	var (
		data *service.SessionSearchResp
		err  error
	)
	switch {
	case id != "":
		data, err = service.SessionSearchByID(ownerID, id, limit, offset, sessionType)
	case keyword != "":
		data, err = service.SessionSearch(ownerID, keyword, limit, offset, sessionType)
	default:
		data, err = service.SessionListAll(ownerID, limit, offset, sessionType)
	}
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

// AgentSessionAddMembers handles POST /v1/agent-api/sessions/members/add
// The agent acts as the owner, inheriting the owner's permissions.
func AgentSessionAddMembers(c *gin.Context) {
	ownerID := middleware.GetOwnerID(c)
	var req struct {
		SessionID   string   `json:"session_id" binding:"required"`
		MemberIDs   []string `json:"member_ids" binding:"required"`
		MemberTypes []int16  `json:"member_types"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	memberIDs, err := parseMemberIDs(req.MemberIDs)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "member_ids 格式错误")
		return
	}
	data, err := service.SessionAddMembers(ownerID, req.SessionID, memberIDs, req.MemberTypes)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

// AgentSessionRemoveMembers handles POST /v1/agent-api/sessions/members/remove
func AgentSessionRemoveMembers(c *gin.Context) {
	ownerID := middleware.GetOwnerID(c)
	var req struct {
		SessionID   string   `json:"session_id" binding:"required"`
		MemberIDs   []string `json:"member_ids" binding:"required"`
		MemberTypes []int16  `json:"member_types"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	memberIDs, err := parseMemberIDs(req.MemberIDs)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "member_ids 格式错误")
		return
	}
	data, err := service.SessionRemoveMembers(ownerID, req.SessionID, memberIDs, req.MemberTypes)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

// AgentSessionUpdateMemberRole handles POST /v1/agent-api/sessions/members/role
func AgentSessionUpdateMemberRole(c *gin.Context) {
	ownerID := middleware.GetOwnerID(c)
	var req struct {
		SessionID  string `json:"session_id" binding:"required"`
		MemberID   string `json:"member_id" binding:"required"`
		MemberType int16  `json:"member_type"`
		Role       int16  `json:"role" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	memberID, err := strconv.ParseInt(strings.TrimSpace(req.MemberID), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "member_id 格式错误")
		return
	}
	data, err := service.SessionUpdateMemberRole(ownerID, req.SessionID, memberID, req.MemberType, req.Role)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

// AgentSessionUpdateAllMembersMuted handles POST /v1/agent-api/sessions/speaking/all_muted
func AgentSessionUpdateAllMembersMuted(c *gin.Context) {
	ownerID := middleware.GetOwnerID(c)
	var req struct {
		SessionID       string `json:"session_id" binding:"required"`
		AllMembersMuted *bool  `json:"all_members_muted" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	if req.AllMembersMuted == nil {
		response.Fail(c, http.StatusBadRequest, 10003, "all_members_muted required")
		return
	}
	data, err := service.SessionUpdateAllMembersMuted(ownerID, req.SessionID, *req.AllMembersMuted)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

// AgentSessionUpdateMemberSpeaking handles POST /v1/agent-api/sessions/members/speaking
func AgentSessionUpdateMemberSpeaking(c *gin.Context) {
	ownerID := middleware.GetOwnerID(c)
	var req struct {
		SessionID            string `json:"session_id" binding:"required"`
		MemberID             string `json:"member_id" binding:"required"`
		MemberType           int16  `json:"member_type"`
		IsSpeakMuted         *bool  `json:"is_speak_muted"`
		CanSpeakWhenAllMuted *bool  `json:"can_speak_when_all_muted"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	memberID, err := strconv.ParseInt(strings.TrimSpace(req.MemberID), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "member_id 格式错误")
		return
	}
	data, err := service.SessionUpdateMemberSpeaking(
		ownerID,
		req.SessionID,
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

// AgentSessionDissolve handles POST /v1/agent-api/sessions/dissolve
func AgentSessionDissolve(c *gin.Context) {
	ownerID := middleware.GetOwnerID(c)
	var req struct {
		SessionID string `json:"session_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	data, err := service.SessionDissolve(ownerID, req.SessionID)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}

// AgentSessionGroupDetail handles GET /v1/agent-api/sessions/group/detail
func AgentSessionGroupDetail(c *gin.Context) {
	agentID := middleware.GetAgentID(c)
	ownerID := middleware.GetOwnerID(c)
	sessionID := strings.TrimSpace(c.Query("session_id"))
	if sessionID == "" {
		response.Fail(c, http.StatusBadRequest, 10003, "session_id required")
		return
	}
	data, err := service.SessionGroupDetail(agentID, ownerID, sessionID)
	if err != nil {
		handleSessionServiceError(c, err)
		return
	}
	response.OK(c, data)
}
