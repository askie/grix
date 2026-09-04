package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	pkgagentapi "github.com/askie/grix/backend/internal/pkg/agentapi"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func AgentList(userID int64, categoryID *int64) ([]AgentResp, error) {
	return AgentListWithContext(context.Background(), userID, categoryID)
}

func AgentListWithContext(ctx context.Context, userID int64, categoryID *int64) ([]AgentResp, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var agents []model.Agent
	q := store.DB.WithContext(ctx).Where("owner_id = ? AND status != 3", userID)
	if categoryID != nil {
		q = q.Where("category_id = ?", *categoryID)
	}
	if err := q.Order("sort_order ASC, created_at DESC").Find(&agents).Error; err != nil {
		return nil, err
	}
	onlineByID := loadAgentOnlineMap(ctx, userID)
	agentIDs := make([]int64, len(agents))
	for i := range agents {
		agentIDs[i] = agents[i].ID
	}
	connectorAdminByID := loadAgentConnectorAdminCapableMap(ctx, userID, agentIDs)
	list := make([]AgentResp, len(agents))
	for i := range agents {
		list[i] = agentToRespWithSecretAndOnlineWithContext(ctx, &agents[i], userID, "", onlineByID[agents[i].ID])
		list[i].SupportsConnectorAdmin = connectorAdminByID[agents[i].ID]
	}
	return list, nil
}

func AgentGet(userID, agentID int64) (*AgentResp, *errcode.ErrCode) {
	var agent model.Agent
	if err := store.DB.First(&agent, agentID).Error; err != nil {
		return nil, &errcode.ErrAgentNotFound
	}
	if agent.OwnerID != userID {
		return nil, &errcode.ErrAgentForbidden
	}
	if agent.Status == 3 {
		return nil, &errcode.ErrAgentNotFound
	}
	onlineByID := loadAgentOnlineMap(context.Background(), userID)
	resp := agentToRespWithSecretAndOnline(&agent, userID, "", onlineByID[agent.ID])
	resp.SupportsConnectorAdmin = loadAgentConnectorAdminCapableMap(
		context.Background(), userID, []int64{agent.ID},
	)[agent.ID]
	return &resp, nil
}

func agentToResp(a *model.Agent, userID int64) AgentResp {
	onlineByID := loadAgentOnlineMap(context.Background(), userID)
	return agentToRespWithSecretAndOnline(a, userID, "", onlineByID[a.ID])
}

func agentToRespWithSecret(a *model.Agent, userID int64, apiKey string) AgentResp {
	onlineByID := loadAgentOnlineMap(context.Background(), userID)
	return agentToRespWithSecretAndOnline(a, userID, apiKey, onlineByID[a.ID])
}

func agentToRespWithSecretAndOnline(a *model.Agent, userID int64, apiKey string, online bool) AgentResp {
	return agentToRespWithSecretAndOnlineWithContext(context.Background(), a, userID, apiKey, online)
}

func agentToRespWithSecretAndOnlineWithContext(ctx context.Context, a *model.Agent, userID int64, apiKey string, online bool) AgentResp {
	if ctx == nil {
		ctx = context.Background()
	}
	sessionID := findAgentSessionIDWithContext(ctx, userID, a.ID)
	apiEndpoint := ""
	apiKeyHint := ""
	if a.ProviderType == model.AgentProviderAPI {
		apiEndpoint = buildAgentAPIEndpoint(a.ID)
		apiKeyHint = a.APIKeyHint
		if !online {
			online = isAgentChannelAvailableWithContext(ctx, a.ID)
		}
	} else {
		online = false
	}
	return AgentResp{
		ID:            a.ID,
		AgentName:     a.AgentName,
		Introduction:  a.Introduction,
		ModelProvider: a.ModelProvider,
		SystemPrompt:  a.SystemPrompt,
		AvatarURL:     a.AvatarURL,
		Profile: AgentProfileResp{
			AvatarURL:    a.AvatarURL,
			Introduction: a.Introduction,
		},
		OwnerID:                 a.OwnerID,
		CategoryID:              a.CategoryID,
		SortOrder:               a.SortOrder,
		ProviderType:            a.ProviderType,
		IsMain:                  a.IsMain,
		AgentClientType:         a.AgentClientType,
		LocalEndpoint:           a.LocalEndpoint,
		LocalModelName:          a.LocalModelName,
		ContextFile:             a.ContextFile,
		APIEndpoint:             apiEndpoint,
		APIKey:                  apiKey,
		APIKeyHint:              apiKeyHint,
		MediaCapability:         a.MediaCapability,
		VoiceProvider:           a.VoiceProvider,
		VoiceID:                 a.VoiceID,
		VoiceModel:              a.VoiceModel,
		VoiceEndpoint:           a.VoiceEndpoint,
		VoiceAPIKeyHint:         a.VoiceAPIKeyHint,
		VoiceMaxCallSeconds:     a.VoiceMaxCallSeconds,
		VoiceDailyCallLimit:     a.VoiceDailyCallLimit,
		VoiceMaxConcurrentCalls: a.VoiceMaxConcurrentCalls,
		VoiceAllowVisitor:       a.VoiceAllowVisitor,
		VoiceWelcomeI18n:        decodeVoiceWelcomeI18n(a.VoiceWelcomeI18n),
		Online:                  online,
		HostName:                gatewayAgentHostName(a.Config),
		Config:                  a.Config,
		Status:                  a.Status,
		SessionID:               sessionID,
		CreatedAt:               a.CreatedAt.Unix(),
		UpdatedAt:               a.UpdatedAt.Unix(),
	}
}

func decodeVoiceWelcomeI18n(raw []byte) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil || len(m) == 0 {
		return nil
	}
	return m
}

type agentOnlineStateExtra struct {
	Connected  bool  `json:"connected"`
	LeaseUntil int64 `json:"lease_until,omitempty"`
}

func loadAgentOnlineMap(ctx context.Context, userID int64) map[int64]bool {
	if userID <= 0 || store.RDB == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	items, err := store.RDB.HGetAll(ctx, fmt.Sprintf("im:agent_state:%d", userID)).Result()
	if err != nil || len(items) == 0 {
		return nil
	}

	nowMs := time.Now().UnixMilli()
	result := make(map[int64]bool, len(items))
	for field, raw := range items {
		agentID, err := strconv.ParseInt(strings.TrimSpace(field), 10, 64)
		if err != nil || agentID <= 0 || strings.TrimSpace(raw) == "" {
			continue
		}

		var payload protocol.AgentStateSyncPayload
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			continue
		}
		payload.AgentID = agentID
		if !agentStatePayloadIsOnline(payload, nowMs) {
			continue
		}
		result[agentID] = true
	}
	return result
}

func agentStatePayloadIsOnline(payload protocol.AgentStateSyncPayload, nowMs int64) bool {
	if payload.AgentID <= 0 || payload.State != protocol.AgentStateOnline {
		return false
	}

	var extra agentOnlineStateExtra
	if len(payload.Extra) == 0 {
		return false
	}
	if err := json.Unmarshal(payload.Extra, &extra); err != nil {
		return false
	}
	if !extra.Connected {
		return false
	}
	return extra.LeaseUntil > nowMs
}

func findAgentSessionID(userID, agentID int64) string {
	return findAgentSessionIDWithContext(context.Background(), userID, agentID)
}

func findAgentSessionIDWithContext(ctx context.Context, userID, agentID int64) string {
	if ctx == nil {
		ctx = context.Background()
	}
	directKey := buildDirectKey(userID, agentID, 2)

	var session model.Session
	if err := store.DB.WithContext(ctx).
		Select("session_id").
		Where("direct_key = ? AND is_deleted = false", directKey).
		Order("updated_at DESC").
		First(&session).Error; err != nil {
		return ""
	}

	return session.SessionID
}

func buildAgentAPIEndpoint(agentID int64) string {
	return pkgagentapi.BuildEndpoint(
		config.C.Server.AgentAPIDomain,
		config.C.Server.AgentAPIPath,
		config.C.Server.AgentAPIWSPath,
		config.C.Server.WSPort,
		agentID,
	)
}
