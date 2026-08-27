package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	pkgagentapi "github.com/askie/grix/backend/internal/pkg/agentapi"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/pkg/locale"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/secretcrypto"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// marshalVoiceWelcomeI18n 把 owner 填写的语音开场白多语言 map 按 key 归一化后编码为 jsonb。
// nil/空 map 编码为 "{}"（不打招呼）。
func marshalVoiceWelcomeI18n(m map[string]string) datatypes.JSON {
	normalized := make(map[string]string, len(m))
	for k, v := range m {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		// 只保留能真命中集合的 key，未知语言 key 直接丢弃，
		// 避免多个未知 key 都归一到 Default 后互相覆盖（Go map 遍历无序）。
		loc, ok := locale.Match(k)
		if !ok {
			continue
		}
		normalized[loc] = v
	}
	raw, _ := json.Marshal(normalized)
	return datatypes.JSON(raw)
}

func AgentCreate(userID int64, req AgentCreateReq) (*AgentResp, *errcode.ErrCode) {
	agentName, ec := normalizeAgentName(req.AgentName)
	if ec != nil {
		return nil, ec
	}
	introduction, ec := normalizeAgentIntroduction(req.Introduction)
	if ec != nil {
		return nil, ec
	}
	if ec := validateAgentAvatarURLValue(req.AvatarURL); ec != nil {
		return nil, ec
	}
	req.AgentName = agentName
	req.Introduction = introduction
	req.AvatarURL = strings.TrimSpace(req.AvatarURL)

	if req.ProviderType == 0 {
		req.ProviderType = model.AgentProviderRemote
	}
	if !isValidProviderType(req.ProviderType) {
		return nil, &errcode.ErrAgentInvalidType
	}
	if req.IsMain && req.ProviderType != model.AgentProviderAPI {
		return nil, &errcode.ErrCode{
			HTTPStatus: 400,
			BizCode:    10003,
			Msg:        "is_main 仅支持 provider_type=3",
		}
	}
	if req.ProviderType == model.AgentProviderLocal {
		if e := validateLocalEndpoint(req.LocalEndpoint); e != nil {
			return nil, e
		}
	}
	agentClientType, ec := normalizeAgentClientTypeForProvider(req.ProviderType, req.AgentClientType)
	if ec != nil {
		return nil, ec
	}
	if req.ProviderType == model.AgentProviderAPI {
		req.ModelProvider = ""
		req.LocalEndpoint = ""
		req.LocalModelName = ""
		req.ContextFile = ""
	}

	// 语音大模型 BYOK：必填校验 + API key 加密
	mediaCapability := ""
	voiceCipher := ""
	voiceHint := ""
	if req.ProviderType == model.AgentProviderVoice {
		if ec := normalizeAndValidateVoiceCreate(&req); ec != nil {
			return nil, ec
		}
		cipher, err := secretcrypto.Encrypt(req.VoiceAPIKey)
		if err != nil {
			return nil, internalAgentErr("加密语音 API 密钥失败", err)
		}
		voiceCipher = cipher
		voiceHint = secretcrypto.Hint(req.VoiceAPIKey)
		mediaCapability = model.AgentMediaCapabilityVoice
	}

	var count int64
	store.DB.Model(&model.Agent{}).Where("owner_id = ? AND status != 3", userID).Count(&count)
	if count >= maxAgentsPerUser {
		return nil, &errcode.ErrAgentLimitExceed
	}

	if ec := CheckOwnerCategory(userID, req.CategoryID); ec != nil {
		return nil, ec
	}

	var existing model.Agent
	if err := store.DB.Where("owner_id = ? AND agent_name = ? AND status != 3", userID, req.AgentName).First(&existing).Error; err == nil {
		return nil, &errcode.ErrAgentNameExists
	}

	now := time.Now()
	agentID := snowflake.GenID()
	apiKeyPlain := ""
	apiKeyHash := ""
	apiKeyHint := ""
	if req.ProviderType == model.AgentProviderAPI {
		plain, hash, hint, err := pkgagentapi.GenerateAPIKey(agentID)
		if err != nil {
			return nil, internalAgentErr("生成 Agent API 密钥失败", err)
		}
		apiKeyPlain = plain
		apiKeyHash = hash
		apiKeyHint = hint
	}

	agent := model.Agent{
		ID:                      agentID,
		AgentName:               req.AgentName,
		Introduction:            req.Introduction,
		ModelProvider:           req.ModelProvider,
		SystemPrompt:            req.SystemPrompt,
		AvatarURL:               req.AvatarURL,
		OwnerID:                 userID,
		CategoryID:              req.CategoryID,
		ProviderType:            req.ProviderType,
		AgentClientType:         agentClientType,
		LocalEndpoint:           req.LocalEndpoint,
		LocalModelName:          req.LocalModelName,
		ContextFile:             req.ContextFile,
		IsMain:                  req.IsMain && req.ProviderType == model.AgentProviderAPI,
		APIKeyHash:              apiKeyHash,
		APIKeyHint:              apiKeyHint,
		MediaCapability:         mediaCapability,
		VoiceProvider:           req.VoiceProvider,
		VoiceID:                 req.VoiceID,
		VoiceModel:              req.VoiceModel,
		VoiceEndpoint:           req.VoiceEndpoint,
		VoiceAPIKeyCipher:       voiceCipher,
		VoiceAPIKeyHint:         voiceHint,
		VoiceMaxCallSeconds:     maxInt(req.VoiceMaxCallSeconds, 0),
		VoiceDailyCallLimit:     maxInt(req.VoiceDailyCallLimit, 0),
		VoiceMaxConcurrentCalls: clampVoiceMaxConcurrentCalls(req.VoiceMaxConcurrentCalls),
		VoiceAllowVisitor:       req.ProviderType == model.AgentProviderVoice && req.VoiceAllowVisitor,
		VoiceWelcomeI18n:        marshalVoiceWelcomeI18n(req.VoiceWelcomeI18n),
		Config:                  datatypes.JSON([]byte("{}")),
		Status:                  1,
		CreatedAt:               now,
		UpdatedAt:               now,
	}
	if err := store.DB.Create(&agent).Error; err != nil {
		return nil, internalAgentErr("创建 Agent 失败", err)
	}

	if _, err := SessionCreate(userID, agent.ID, 2); err != nil {
		return nil, internalAgentErr("创建 Agent 会话失败", err)
	}
	if req.ProviderType == model.AgentProviderAPI && req.IsMain {
		if _, ec := AgentScopeReplace(userID, agent.ID, agentscope.AllowedScopes()); ec != nil {
			return nil, ec
		}
	}

	resp := agentToRespWithSecret(&agent, userID, apiKeyPlain)
	return &resp, nil
}

// AgentCreateAPIForOwner creates an Agent API type agent on behalf of owner.
// It always forces provider_type=3 to avoid mixed provider creation from Agent API path.
func AgentCreateAPIForOwner(
	ownerID int64,
	agentName, avatarURL, introduction, systemPrompt, agentClientType string,
	isMain bool,
) (*AgentResp, *errcode.ErrCode) {
	return AgentCreate(ownerID, AgentCreateReq{
		AgentName:       agentName,
		Introduction:    introduction,
		SystemPrompt:    systemPrompt,
		AvatarURL:       avatarURL,
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: agentClientType,
		IsMain:          isMain,
	})
}

// ResolveAgentAPIOwner resolves the owner user id from the authenticated actor agent id.
// This acts as the source of truth for Agent API write operations.
func ResolveAgentAPIOwner(actorAgentID int64) (int64, *errcode.ErrCode) {
	if actorAgentID <= 0 {
		return 0, &errcode.ErrCode{
			HTTPStatus: 401,
			BizCode:    10001,
			Msg:        "invalid agent context",
		}
	}

	var actor model.Agent
	if err := store.DB.
		Select("id", "owner_id", "provider_type", "status").
		First(&actor, actorAgentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, &errcode.ErrCode{
				HTTPStatus: 401,
				BizCode:    10001,
				Msg:        "invalid agent context",
			}
		}
		return 0, internalAgentErr("查询 Agent 归属失败", err)
	}

	if actor.ProviderType != model.AgentProviderAPI {
		return 0, &errcode.ErrCode{
			HTTPStatus: 403,
			BizCode:    10002,
			Msg:        "agent is not an API provider",
		}
	}
	if actor.Status != model.AgentStatusActive {
		return 0, &errcode.ErrCode{
			HTTPStatus: 403,
			BizCode:    10002,
			Msg:        "agent is not active",
		}
	}
	if actor.OwnerID <= 0 {
		return 0, &errcode.ErrCode{
			HTTPStatus: 403,
			BizCode:    10002,
			Msg:        "agent owner is invalid",
		}
	}

	return actor.OwnerID, nil
}

func AgentUpdate(userID, agentID int64, req AgentUpdateReq) (*AgentResp, *errcode.ErrCode) {
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
	if ec := validateAgentAvatarURLInputProvided(req.AvatarURL); ec != nil {
		return nil, ec
	}
	if req.ProviderType != nil && !isValidProviderType(*req.ProviderType) {
		return nil, &errcode.ErrAgentInvalidType
	}

	updates := make(map[string]any)
	targetProviderType := agent.ProviderType
	if req.ProviderType != nil {
		targetProviderType = *req.ProviderType
	}
	if req.AgentClientType != nil {
		normalizedClientType, ec := normalizeAgentClientTypeForProvider(targetProviderType, *req.AgentClientType)
		if ec != nil {
			return nil, ec
		}
		updates["agent_client_type"] = normalizedClientType
	}

	apiKeyPlain := ""
	if req.AgentName != nil {
		normalizedName, ec := normalizeAgentName(*req.AgentName)
		if ec != nil {
			return nil, ec
		}
		var dup model.Agent
		if err := store.DB.Where("owner_id = ? AND agent_name = ? AND status != 3 AND id != ?", userID, normalizedName, agentID).First(&dup).Error; err == nil {
			return nil, &errcode.ErrAgentNameExists
		}
		updates["agent_name"] = normalizedName
	}
	if req.Introduction != nil {
		normalizedIntroduction, ec := normalizeAgentIntroduction(*req.Introduction)
		if ec != nil {
			return nil, ec
		}
		updates["introduction"] = normalizedIntroduction
	}
	if req.CategoryID != nil {
		if ec := CheckOwnerCategory(userID, *req.CategoryID); ec != nil {
			return nil, ec
		}
		updates["category_id"] = *req.CategoryID
	}
	if req.ModelProvider != nil {
		updates["model_provider"] = *req.ModelProvider
	}
	if req.SystemPrompt != nil {
		updates["system_prompt"] = *req.SystemPrompt
	}
	if req.AvatarURL != nil {
		updates["avatar_url"] = strings.TrimSpace(*req.AvatarURL)
	}
	if req.ProviderType != nil {
		updates["provider_type"] = *req.ProviderType
		if *req.ProviderType == model.AgentProviderAPI {
			if agent.ProviderType != model.AgentProviderAPI && req.AgentClientType == nil {
				updates["agent_client_type"] = ""
			}
			updates["model_provider"] = ""
			updates["local_endpoint"] = ""
			updates["local_model_name"] = ""
			updates["context_file"] = ""
			if agent.ProviderType != model.AgentProviderAPI || agent.APIKeyHash == "" {
				plain, keyHash, keyHint, err := pkgagentapi.GenerateAPIKey(agent.ID)
				if err != nil {
					return nil, internalAgentErr("生成 Agent API 密钥失败", err)
				}
				apiKeyPlain = plain
				updates["api_key_hash"] = keyHash
				updates["api_key_hint"] = keyHint
			}
		} else if agent.ProviderType == model.AgentProviderAPI {
			updates["agent_client_type"] = ""
			updates["api_key_hash"] = ""
			updates["api_key_hint"] = ""
		}
	}
	if req.LocalEndpoint != nil {
		if e := validateLocalEndpoint(*req.LocalEndpoint); e != nil {
			return nil, e
		}
		updates["local_endpoint"] = *req.LocalEndpoint
	}
	if req.LocalModelName != nil {
		updates["local_model_name"] = *req.LocalModelName
	}
	if req.SortOrder != nil {
		updates["sort_order"] = *req.SortOrder
	}

	// 语音大模型 BYOK 字段更新
	if req.VoiceProvider != nil {
		updates["voice_provider"] = strings.TrimSpace(*req.VoiceProvider)
	}
	if req.VoiceID != nil {
		updates["voice_id"] = strings.TrimSpace(*req.VoiceID)
	}
	if req.VoiceModel != nil {
		updates["voice_model"] = strings.TrimSpace(*req.VoiceModel)
	}
	if req.VoiceEndpoint != nil {
		endpoint := strings.TrimSpace(*req.VoiceEndpoint)
		if ec := validateVoiceEndpoint(endpoint); ec != nil {
			return nil, ec
		}
		updates["voice_endpoint"] = endpoint
	}
	if req.VoiceAPIKey != nil {
		// 留空表示保持原值；非空则重新加密
		if k := strings.TrimSpace(*req.VoiceAPIKey); k != "" {
			cipher, err := secretcrypto.Encrypt(k)
			if err != nil {
				return nil, internalAgentErr("加密语音 API 密钥失败", err)
			}
			updates["voice_api_key_cipher"] = cipher
			updates["voice_api_key_hint"] = secretcrypto.Hint(k)
		}
	}
	if req.VoiceMaxCallSeconds != nil {
		updates["voice_max_call_seconds"] = maxInt(*req.VoiceMaxCallSeconds, 0)
	}
	if req.VoiceDailyCallLimit != nil {
		updates["voice_daily_call_limit"] = maxInt(*req.VoiceDailyCallLimit, 0)
	}
	if req.VoiceMaxConcurrentCalls != nil {
		updates["voice_max_concurrent_calls"] = clampVoiceMaxConcurrentCalls(*req.VoiceMaxConcurrentCalls)
	}
	if req.VoiceAllowVisitor != nil {
		updates["voice_allow_visitor"] = *req.VoiceAllowVisitor
	}
	if req.VoiceWelcomeI18n != nil {
		updates["voice_welcome_i18n"] = marshalVoiceWelcomeI18n(*req.VoiceWelcomeI18n)
	}
	if req.ProviderType != nil {
		if *req.ProviderType == model.AgentProviderVoice {
			updates["media_capability"] = model.AgentMediaCapabilityVoice
		} else if agent.ProviderType == model.AgentProviderVoice {
			// 离开语音类型：清空语音配置
			updates["media_capability"] = model.AgentMediaCapabilityText
			updates["voice_provider"] = ""
			updates["voice_id"] = ""
			updates["voice_model"] = ""
			updates["voice_endpoint"] = ""
			updates["voice_api_key_cipher"] = ""
			updates["voice_api_key_hint"] = ""
			updates["voice_welcome_i18n"] = datatypes.JSON([]byte("{}"))
		}
	}
	// 最终为语音类型时强制必填齐全（结合已有值与本次更新，无兜底）
	if targetProviderType == model.AgentProviderVoice {
		finalProvider := pickUpdateStr(updates, "voice_provider", agent.VoiceProvider)
		if finalProvider == "" ||
			pickUpdateStr(updates, "voice_model", agent.VoiceModel) == "" ||
			pickUpdateStr(updates, "voice_api_key_cipher", agent.VoiceAPIKeyCipher) == "" {
			return nil, &errcode.ErrCode{
				HTTPStatus: 400,
				BizCode:    10003,
				Msg:        "语音大模型需填写 voice_provider / voice_model / voice_api_key",
			}
		}
		if !isSupportedVoiceProvider(finalProvider) {
			return nil, &errcode.ErrCode{
				HTTPStatus: 400,
				BizCode:    10003,
				Msg:        "暂不支持的语音 provider（当前支持 openai_realtime / doubao_realtime）",
			}
		}
	}

	// 保存更新前的 name/introduction 快照，供文件型 agent（openclaw/hermes）的自更新指令使用。
	oldNameSnapshot := agent.AgentName
	oldIntroSnapshot := agent.Introduction
	oldAgentSnapshot := agent

	if len(updates) > 0 {
		updates["updated_at"] = time.Now()
		store.DB.Model(&agent).Updates(updates)
	}

	if updates["agent_name"] != nil || updates["introduction"] != nil || updates["system_prompt"] != nil {
		notifyAgentProfileChanged(agentID)
	}
	if updates["agent_name"] != nil || updates["introduction"] != nil {
		// 对身份/记忆存于本地文件的 agent（openclaw/hermes），额外投递一条带外指令消息，
		// 让 agent 用本地工具自更新文件。消息不入库，prompt 显式禁止回复。
		newName := pickUpdateStr(updates, "agent_name", oldNameSnapshot)
		newIntro := pickUpdateStr(updates, "introduction", oldIntroSnapshot)
		notifyFileBasedAgentProfileChange(&oldAgentSnapshot, oldNameSnapshot, newName, oldIntroSnapshot, newIntro)
	}

	store.DB.First(&agent, agentID)
	resp := agentToRespWithSecret(&agent, userID, apiKeyPlain)
	return &resp, nil
}

// pickUpdateStr 取 updates[key] 的字符串值，不存在则返回 fallback。
func pickUpdateStr(updates map[string]any, key, fallback string) string {
	if v, ok := updates[key]; ok {
		if s, ok2 := v.(string); ok2 {
			return s
		}
	}
	return fallback
}

func AgentUpdateContext(userID, agentID int64, text string) *errcode.ErrCode {
	if len(text) > maxContextFileBytes {
		return &errcode.ErrAgentCtxTooLarge
	}

	var agent model.Agent
	if err := store.DB.First(&agent, agentID).Error; err != nil {
		return &errcode.ErrAgentNotFound
	}
	if agent.OwnerID != userID {
		return &errcode.ErrAgentForbidden
	}
	if agent.Status == 3 {
		return &errcode.ErrAgentNotFound
	}

	store.DB.Model(&agent).Updates(map[string]any{
		"context_file": text,
		"updated_at":   time.Now(),
	})
	return nil
}

func AgentDelete(userID, agentID int64) *errcode.ErrCode {
	var agent model.Agent
	if err := store.DB.First(&agent, agentID).Error; err != nil {
		return &errcode.ErrAgentNotFound
	}
	if agent.OwnerID != userID {
		return &errcode.ErrAgentForbidden
	}
	if agent.Status == 3 {
		return &errcode.ErrAgentNotFound
	}

	ctx := context.Background()
	pattern := "im:delegate:*"
	iter := store.RDB.Scan(ctx, 0, pattern, 100).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		aid, _ := store.RDB.HGet(ctx, key, "agent_id").Int64()
		if aid == agentID {
			store.RDB.Del(ctx, key)
		}
	}

	store.DB.Model(&agent).Updates(map[string]any{
		"status":     3,
		"updated_at": time.Now(),
	})

	// 同步把该 agent 的有效共享全部撤销，避免孤儿 agent_shares 记录长期残留，
	// 并通过 notifyAgentShareChanged 让被共享者的连接被踢（虽然下面 publishAgentKickAllNodes
	// 也会全员踢，这里多一道保险，且让 agent_shares 状态与 agent 状态一致）。
	if err := store.DB.Model(&model.AgentShare{}).
		Where("agent_id = ? AND status = ?", agentID, model.AgentShareStatusActive).
		Updates(map[string]any{
			"status":     model.AgentShareStatusRevoked,
			"updated_at": time.Now(),
		}).Error; err != nil {
		logger.L.Warnf("revoke agent_shares on agent delete failed agent=%d err=%v", agentID, err)
	} else {
		notifyAgentShareChanged(agentID)
	}

	sessionID := findAgentSessionID(userID, agentID)
	if sessionID != "" {
		store.DB.Model(&model.Session{}).Where("session_id = ?", sessionID).
			Update("is_deleted", true)
	}

	// 在线连接主动踢线：kicked 包带 reason="agent_deleted"，connector 识别后自清理。
	// 必须放在 status=3 落库之后，否则被踢连接立刻重连仍能通过认证。
	// 按 owner 路由向所有节点广播（原 publishKickAgent 只发主路由节点，共享连接散在
	// 其他节点会漏踢）。
	publishAgentKickAllNodes(agentID, "agent_deleted")

	return nil
}

func AgentRotateAPIKey(userID, agentID int64) (*AgentResp, *errcode.ErrCode) {
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
	if agent.ProviderType != model.AgentProviderAPI {
		return nil, &errcode.ErrAgentInvalidType
	}

	apiKey, keyHash, keyHint, err := pkgagentapi.GenerateAPIKey(agent.ID)
	if err != nil {
		return nil, internalAgentErr("生成 Agent API 密钥失败", err)
	}

	if err := store.DB.Model(&agent).Updates(map[string]any{
		"api_key_hash": keyHash,
		"api_key_hint": keyHint,
		"updated_at":   time.Now(),
	}).Error; err != nil {
		return nil, internalAgentErr("更新 Agent API 密钥失败", err)
	}

	store.DB.First(&agent, agentID)
	resp := agentToRespWithSecret(&agent, userID, apiKey)
	return &resp, nil
}

// notifyAgentProfileChanged 把资料变更通知发到 agent 主连接所在 ws 节点。
// agent 不在线时静默跳过——主连接重连时 auth_ack 会带上最新资料。
func notifyAgentProfileChanged(agentID int64) {
	if store.RDB == nil || agentID <= 0 {
		return
	}
	ctx := context.Background()
	node, err := store.RDB.Get(ctx, fmt.Sprintf("im:agent_api:route:%d", agentID)).Result()
	if err != nil || strings.TrimSpace(node) == "" {
		return
	}
	payload, _ := json.Marshal(protocol.AgentProfileSyncPayload{AgentID: agentID})
	envelope, _ := json.Marshal(map[string]interface{}{
		"cmd":     protocol.RedisCmdAgentProfileSync,
		"payload": json.RawMessage(payload),
	})
	if err := store.RDB.Publish(ctx, fmt.Sprintf("chan:%s", node), envelope).Err(); err != nil {
		logger.L.Warnf("publish agent profile sync failed agent=%d node=%s err=%v", agentID, node, err)
	}
}
