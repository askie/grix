package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/i18n"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type agentCreateReq struct {
	AgentName      string `json:"agent_name" binding:"required"`
	Introduction   string `json:"introduction"`
	ModelProvider  string `json:"model_provider"`
	SystemPrompt   string `json:"system_prompt"`
	AvatarURL      string `json:"avatar_url"`
	CategoryID     int64  `json:"category_id,string"`
	ProviderType   int16  `json:"provider_type"`
	IsMain         bool   `json:"is_main"`
	LocalEndpoint  string `json:"local_endpoint"`
	LocalModelName string `json:"local_model_name"`
	ContextFile    string `json:"context_file"`
	// 语音大模型 BYOK（provider_type=4）
	VoiceProvider       string            `json:"voice_provider"`
	VoiceID             string            `json:"voice_id"`
	VoiceModel          string            `json:"voice_model"`
	VoiceEndpoint       string            `json:"voice_endpoint"`
	VoiceAPIKey         string            `json:"voice_api_key"`
	VoiceMaxCallSeconds int               `json:"voice_max_call_seconds"`
	VoiceDailyCallLimit int               `json:"voice_daily_call_limit"`
	VoiceAllowVisitor   bool              `json:"voice_allow_visitor"`
	VoiceWelcomeI18n    map[string]string `json:"voice_welcome_i18n"`
}

func (req agentCreateReq) toServiceReq() service.AgentCreateReq {
	return service.AgentCreateReq{
		AgentName:           req.AgentName,
		Introduction:        req.Introduction,
		ModelProvider:       req.ModelProvider,
		SystemPrompt:        req.SystemPrompt,
		AvatarURL:           req.AvatarURL,
		CategoryID:          req.CategoryID,
		ProviderType:        req.ProviderType,
		IsMain:              req.IsMain,
		LocalEndpoint:       req.LocalEndpoint,
		LocalModelName:      req.LocalModelName,
		ContextFile:         req.ContextFile,
		VoiceProvider:       req.VoiceProvider,
		VoiceID:             req.VoiceID,
		VoiceModel:          req.VoiceModel,
		VoiceEndpoint:       req.VoiceEndpoint,
		VoiceAPIKey:         req.VoiceAPIKey,
		VoiceMaxCallSeconds: req.VoiceMaxCallSeconds,
		VoiceDailyCallLimit: req.VoiceDailyCallLimit,
		VoiceAllowVisitor:   req.VoiceAllowVisitor,
		VoiceWelcomeI18n:    req.VoiceWelcomeI18n,
	}
}

type agentUpdateReq struct {
	AgentName      *string `json:"agent_name"`
	Introduction   *string `json:"introduction"`
	ModelProvider  *string `json:"model_provider"`
	SystemPrompt   *string `json:"system_prompt"`
	AvatarURL      *string `json:"avatar_url"`
	CategoryID     *int64  `json:"category_id,string"`
	ProviderType   *int16  `json:"provider_type"`
	LocalEndpoint  *string `json:"local_endpoint"`
	LocalModelName *string `json:"local_model_name"`
	SortOrder      *int    `json:"sort_order"`
	// 语音大模型 BYOK（provider_type=4）
	VoiceProvider       *string            `json:"voice_provider"`
	VoiceID             *string            `json:"voice_id"`
	VoiceModel          *string            `json:"voice_model"`
	VoiceEndpoint       *string            `json:"voice_endpoint"`
	VoiceAPIKey         *string            `json:"voice_api_key"`
	VoiceMaxCallSeconds *int               `json:"voice_max_call_seconds"`
	VoiceDailyCallLimit *int               `json:"voice_daily_call_limit"`
	VoiceAllowVisitor   *bool              `json:"voice_allow_visitor"`
	VoiceWelcomeI18n    *map[string]string `json:"voice_welcome_i18n"`
}

func (req agentUpdateReq) toServiceReq() service.AgentUpdateReq {
	return service.AgentUpdateReq{
		AgentName:           req.AgentName,
		Introduction:        req.Introduction,
		ModelProvider:       req.ModelProvider,
		SystemPrompt:        req.SystemPrompt,
		AvatarURL:           req.AvatarURL,
		CategoryID:          req.CategoryID,
		ProviderType:        req.ProviderType,
		LocalEndpoint:       req.LocalEndpoint,
		LocalModelName:      req.LocalModelName,
		SortOrder:           req.SortOrder,
		VoiceProvider:       req.VoiceProvider,
		VoiceID:             req.VoiceID,
		VoiceModel:          req.VoiceModel,
		VoiceEndpoint:       req.VoiceEndpoint,
		VoiceAPIKey:         req.VoiceAPIKey,
		VoiceMaxCallSeconds: req.VoiceMaxCallSeconds,
		VoiceDailyCallLimit: req.VoiceDailyCallLimit,
		VoiceAllowVisitor:   req.VoiceAllowVisitor,
		VoiceWelcomeI18n:    req.VoiceWelcomeI18n,
	}
}

func AgentCreate(c *gin.Context) {
	var req agentCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	userID := middleware.GetUserID(c)
	data, ec := service.AgentCreate(userID, req.toServiceReq())
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

type agentAPIAgentCreateReq struct {
	AgentName       string `json:"agent_name" binding:"required"`
	Introduction    string `json:"introduction"`
	SystemPrompt    string `json:"system_prompt"`
	AvatarURL       string `json:"avatar_url"`
	AgentClientType string `json:"agent_client_type"`
	IsMain          bool   `json:"is_main"`
}

// AgentAPIAgentCreate handles POST /v1/agent-api/agents/create.
// Authenticated via AgentAPIAuth and scoped by AgentAPIScope(agent.api.create).
// It always creates provider_type=3 agent for the current owner.
func AgentAPIAgentCreate(c *gin.Context) {
	actorAgentID := middleware.GetAgentID(c)

	resolvedOwnerID, ownerEC := service.ResolveAgentAPIOwner(actorAgentID)
	if ownerEC != nil {
		response.Fail(c, ownerEC.HTTPStatus, ownerEC.BizCode, ownerEC.Msg)
		return
	}
	ownerID := resolvedOwnerID
	ownerIDRef := ownerID

	var req agentAPIAgentCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		service.WriteAuditLog(service.WriteAuditLogReq{
			EventType: "agent_api_agent_create_failed",
			UserID:    &ownerIDRef,
			ClientIP:  c.ClientIP(),
			UserAgent: c.GetHeader("User-Agent"),
			Detail: map[string]any{
				"actor_agent_id": actorAgentID,
				"reason":         "invalid_payload",
			},
		})
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}

	data, ec := service.AgentCreateAPIForOwner(
		ownerID,
		req.AgentName,
		req.AvatarURL,
		req.Introduction,
		req.SystemPrompt,
		req.AgentClientType,
		req.IsMain,
	)
	if ec != nil {
		service.WriteAuditLog(service.WriteAuditLogReq{
			EventType: "agent_api_agent_create_failed",
			UserID:    &ownerIDRef,
			ClientIP:  c.ClientIP(),
			UserAgent: c.GetHeader("User-Agent"),
			Detail: map[string]any{
				"actor_agent_id":    actorAgentID,
				"agent_name":        req.AgentName,
				"agent_client_type": req.AgentClientType,
				"biz_code":          ec.BizCode,
				"http_status":       ec.HTTPStatus,
			},
		})
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	service.WriteAuditLog(service.WriteAuditLogReq{
		EventType: "agent_api_agent_create",
		UserID:    &ownerIDRef,
		ClientIP:  c.ClientIP(),
		UserAgent: c.GetHeader("User-Agent"),
		Detail: map[string]any{
			"actor_agent_id":    actorAgentID,
			"new_agent_id":      data.ID,
			"agent_name":        data.AgentName,
			"agent_client_type": data.AgentClientType,
		},
	})
	response.OK(c, data)
}

// AgentAPIAgentRotateAPIKey handles POST /v1/agent-api/agents/:id/api/key/rotate.
// Scoped by AgentAPIScope(agent.api.create) — same as agent creation.
func AgentAPIAgentRotateAPIKey(c *gin.Context) {
	actorAgentID := middleware.GetAgentID(c)
	targetAgentID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "无效的 Agent ID")
		return
	}

	ownerID, ownerEC := service.ResolveAgentAPIOwner(actorAgentID)
	if ownerEC != nil {
		response.Fail(c, ownerEC.HTTPStatus, ownerEC.BizCode, ownerEC.Msg)
		return
	}

	data, ec := service.AgentRotateAPIKey(ownerID, targetAgentID)
	if ec != nil {
		service.WriteAuditLog(service.WriteAuditLogReq{
			EventType: "agent_api_key_rotate_failed",
			UserID:    &ownerID,
			ClientIP:  c.ClientIP(),
			UserAgent: c.GetHeader("User-Agent"),
			Detail: map[string]any{
				"actor_agent_id":  actorAgentID,
				"target_agent_id": targetAgentID,
				"biz_code":        ec.BizCode,
			},
		})
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}

	service.WriteAuditLog(service.WriteAuditLogReq{
		EventType: "agent_api_key_rotate",
		UserID:    &ownerID,
		ClientIP:  c.ClientIP(),
		UserAgent: c.GetHeader("User-Agent"),
		Detail: map[string]any{
			"actor_agent_id":  actorAgentID,
			"target_agent_id": targetAgentID,
		},
	})
	response.OK(c, data)
}

func AgentList(c *gin.Context) {
	userID := middleware.GetUserID(c)
	var categoryID *int64
	if catStr := c.Query("category_id"); catStr != "" {
		if id, err := strconv.ParseInt(catStr, 10, 64); err == nil {
			categoryID = &id
		}
	}
	data, err := service.AgentList(userID, categoryID)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, gin.H{"list": data})
}

func AgentGet(c *gin.Context) {
	userID := middleware.GetUserID(c)
	agentID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "无效的 Agent ID")
		return
	}
	data, ec := service.AgentGet(userID, agentID)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

func AgentAPIInstallGuideList(c *gin.Context) {
	response.OK(c, service.AgentAPIInstallGuideCatalog(i18n.RequestAppLanguage(c)))
}

func AgentVoiceModelList(c *gin.Context) {
	catalog, err := service.AgentVoiceModelCatalog()
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		return
	}
	response.OK(c, catalog)
}

func AgentUpdate(c *gin.Context) {
	userID := middleware.GetUserID(c)
	agentID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "无效的 Agent ID")
		return
	}
	var req agentUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	data, ec := service.AgentUpdate(userID, agentID, req.toServiceReq())
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

func AgentUploadAvatar(c *gin.Context) {
	userID := middleware.GetUserID(c)
	agentID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "无效的 Agent ID")
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "头像文件不能为空")
		return
	}

	data, ec, err := service.UploadAgentAvatar(userID, agentID, fileHeader)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	if err != nil {
		switch {
		case errors.Is(err, service.ErrAvatarFileRequired):
			response.Fail(c, http.StatusBadRequest, 10003, "头像文件不能为空")
		case errors.Is(err, service.ErrAvatarFileTooLarge):
			response.Fail(c, http.StatusBadRequest, 10003, "头像文件过大（最大10MB）")
		case errors.Is(err, service.ErrAvatarImageInvalid):
			response.Fail(c, http.StatusBadRequest, 10003, "头像文件格式无效")
		default:
			response.Fail(c, http.StatusInternalServerError, 50001, err.Error())
		}
		return
	}

	response.OK(c, data)
}

type updateContextReq struct {
	ContextFile string `json:"context_file" binding:"required"`
}

func AgentUpdateContext(c *gin.Context) {
	userID := middleware.GetUserID(c)
	agentID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "无效的 Agent ID")
		return
	}
	var req updateContextReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	ec := service.AgentUpdateContext(userID, agentID, req.ContextFile)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, nil)
}

func AgentDelete(c *gin.Context) {
	userID := middleware.GetUserID(c)
	agentID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "无效的 Agent ID")
		return
	}
	ec := service.AgentDelete(userID, agentID)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, nil)
}

func AgentRotateAPIKey(c *gin.Context) {
	userID := middleware.GetUserID(c)
	agentID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "无效的 Agent ID")
		return
	}
	data, ec := service.AgentRotateAPIKey(userID, agentID)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

type agentBatchSortReq struct {
	Items []service.AgentSortItem `json:"items" binding:"required"`
}

func AgentBatchSort(c *gin.Context) {
	userID := middleware.GetUserID(c)
	var req agentBatchSortReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	ec := service.AgentBatchSort(userID, req.Items)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, nil)
}
