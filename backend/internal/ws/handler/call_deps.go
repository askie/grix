package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	apiservice "github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/call"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/locale"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/secretcrypto"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/datatypes"
)

// callCtrl 由 ws.Server 在启动时注入。
var callCtrl *call.Controller

// iceServers 由 ws.Server 在启动时注入，存放 TURN/STUN URL 列表和共享密钥。
var iceServers []protocol.ICEServer

// SetCallController 由 ws.Server 调用，注入 Call Controller。
func SetCallController(c *call.Controller) {
	if c != nil {
		c.SetCleanupHook(cleanupCallGuards)
	}
	logger.L.Infof("call trace: call_controller_injected has_hook=%v", c != nil)
	callCtrl = c
}

// SetICEServers 由 ws.Server 调用，注入 TURN/STUN ICE 服务器列表。
// Credential 字段作为 HMAC 共享密钥（对应 coturn 的 static-auth-secret），不在响应中透传。
func SetICEServers(servers []protocol.ICEServer) {
	iceServers = servers
	logger.L.Infof("call trace: ice_servers_injected count=%d", len(servers))
}

// callICEServers 生成带临时凭据的 ICE 服务器列表。
// 使用 TURN REST API（HMAC-SHA1）生成 24 小时有效的临时凭据，
// credential 中的共享密钥不会被下发给客户端。
func callICEServers() []protocol.ICEServer {
	if len(iceServers) == 0 {
		return nil
	}
	result := make([]protocol.ICEServer, 0, len(iceServers))
	for _, s := range iceServers {
		if s.Credential == "" {
			// 无共享密钥的 ICE 服务器（如 STUN）直接透传
			result = append(result, s)
			continue
		}
		// 用 HMAC-SHA1 生成临时凭据
		expiry := time.Now().Add(24 * time.Hour).Unix()
		username := fmt.Sprintf("%d:grix", expiry)
		mac := hmac.New(sha1.New, []byte(s.Credential))
		mac.Write([]byte(username))
		credential := base64.StdEncoding.EncodeToString(mac.Sum(nil))
		result = append(result, protocol.ICEServer{
			URLs:       s.URLs,
			Username:   username,
			Credential: credential,
		})
	}
	return result
}

// validateCallPermission 验证通话权限：好友关系 + 未被拉黑。
// 可在测试中替换为 mock。
var validateCallPermission = func(callerID, calleeID int64) error {
	if store.DB == nil {
		return nil // 测试环境跳过
	}
	// 检查 callee 是否拉黑了 caller
	if err := apiservice.EnsureUserNotBlocked(calleeID, callerID); err != nil {
		if errors.Is(err, apiservice.ErrUserBlockedByPeer) {
			return errors.New("you have been blocked by this user")
		}
		return err
	}
	// 检查好友关系
	var count int64
	if err := store.DB.Model(&model.Friend{}).
		Where("user_id = ? AND friend_id = ?", callerID, calleeID).
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return errors.New("can only call friends")
	}
	return nil
}

// enqueueVoIPPushTask 向 push worker 发布 VoIP 来电推送任务。
// 可在测试中替换为 mock。
var enqueueVoIPPushTask = func(calleeID, callID, callerID int64, callerName string) {
	if store.JS == nil {
		logger.L.Warnf("voip push skipped: jetstream not initialized callee=%d", calleeID)
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"call_id":     fmt.Sprintf("%d", callID),
		"caller_id":   fmt.Sprintf("%d", callerID),
		"caller_name": "", // 由 push worker 从 DB 查询
		"call_mode":   1,  // 1=voice
	})
	data, _ := json.Marshal(map[string]any{
		"user_id": calleeID,
		"cmd":     "call:invite",
		"payload": json.RawMessage(payload),
	})
	if _, err := store.JS.Publish(fmt.Sprintf("im.push.offline.%d", calleeID), data); err != nil {
		logger.L.Warnf("voip push enqueue error callee=%d err=%v", calleeID, err)
	}
}

// resolveCallSession 解析 caller↔agent callee 的真实会话 ID（命中最近线程，否则新建）。
// calleeID 始终是 agent，peerType=2。通话记录与 Phase 3 内容回灌均落入此会话。
// 可在测试中替换为 mock。
var resolveCallSession = func(callerID, calleeID int64) (string, error) {
	if store.DB == nil {
		return fmt.Sprintf("%d", calleeID), nil // 测试环境
	}
	resp, err := apiservice.SessionOpenLatest(callerID, calleeID, 2)
	if err != nil {
		return "", err
	}
	return resp.SessionID, nil
}

// resolveAgentVoiceSpec 从 agents 表解析语音托管所需的完整配置（BYOK）。
// 校验 agent 为 provider_type=4 且 status=1；解密用户 API key；provider/model/api_key
// 任一缺失即报错（无全局兜底）。rawLocale 为调用方能拿到的语言来源（如 widget 访客
// locale），归一化后写入 spec.Language，并据此从 agent 的多语言开场白配置选文案。
// 拿不到语言来源时传空串，归一化兜底 en_US。可在测试中替换为 mock。
var resolveAgentVoiceSpec = func(agentID int64, rawLocale string) (call.VoiceBridgeSpec, error) {
	resolvedLocale := locale.Normalize(rawLocale)
	if store.DB == nil {
		// 测试环境返回最小可用 spec
		return call.VoiceBridgeSpec{
			AgentID: agentID, Provider: "openai_realtime", Model: "test-model", APIKey: "test-key",
			Language: resolvedLocale,
		}, nil
	}
	var ag struct {
		ProviderType            int16
		VoiceProvider           string
		VoiceModel              string
		VoiceEndpoint           string
		VoiceID                 string
		SystemPrompt            string
		VoiceAPIKeyCipher       string
		VoiceMaxCallSeconds     int
		VoiceDailyCallLimit     int
		VoiceAllowVisitor       bool
		VoiceMaxConcurrentCalls int
		VoiceWelcomeI18n        datatypes.JSON
	}
	if err := store.DB.Model(&model.Agent{}).
		Select("provider_type, voice_provider, voice_model, voice_endpoint, voice_id, system_prompt, voice_api_key_cipher, voice_max_call_seconds, voice_daily_call_limit, voice_allow_visitor, voice_max_concurrent_calls, voice_welcome_i18n").
		Where("id = ? AND status = 1", agentID).
		Scan(&ag).Error; err != nil {
		logger.L.Warnf("call trace: resolve_voice_spec query_fail agent=%d err=%v", agentID, err)
		return call.VoiceBridgeSpec{}, fmt.Errorf("query agent: %w", err)
	}
	if ag.ProviderType != model.AgentProviderVoice {
		logger.L.Warnf("call trace: resolve_voice_spec not_voice agent=%d type=%d", agentID, ag.ProviderType)
		return call.VoiceBridgeSpec{}, fmt.Errorf("agent %d is not a voice model (provider_type=%d)", agentID, ag.ProviderType)
	}
	if ag.VoiceProvider == "" || ag.VoiceModel == "" || ag.VoiceAPIKeyCipher == "" {
		logger.L.Warnf("call trace: resolve_voice_spec incomplete agent=%d provider=%q model=%q has_key=%v", agentID, ag.VoiceProvider, ag.VoiceModel, ag.VoiceAPIKeyCipher != "")
		return call.VoiceBridgeSpec{}, fmt.Errorf("agent %d voice config incomplete (provider/model/api_key required)", agentID)
	}
	apiKey, err := secretcrypto.Decrypt(ag.VoiceAPIKeyCipher)
	if err != nil {
		logger.L.Errorf("call trace: resolve_voice_spec decrypt_fail agent=%d err=%v", agentID, err)
		return call.VoiceBridgeSpec{}, fmt.Errorf("decrypt voice api key for agent %d failed", agentID)
	}
	if apiKey == "" {
		logger.L.Warnf("call trace: resolve_voice_spec key_empty agent=%d", agentID)
		return call.VoiceBridgeSpec{}, fmt.Errorf("agent %d voice api key empty after decrypt", agentID)
	}
	var welcomeMap map[string]string
	if len(ag.VoiceWelcomeI18n) > 0 {
		_ = json.Unmarshal(ag.VoiceWelcomeI18n, &welcomeMap)
	}
	return call.VoiceBridgeSpec{
		AgentID:        agentID,
		Provider:       ag.VoiceProvider,
		Model:          ag.VoiceModel,
		Endpoint:       ag.VoiceEndpoint,
		Voice:          ag.VoiceID,
		SystemPrompt:   ag.SystemPrompt,
		APIKey:         apiKey,
		Language:       resolvedLocale,
		Opening:        locale.Pick(welcomeMap, resolvedLocale),
		MaxCallSeconds: ag.VoiceMaxCallSeconds,
		DailyLimit:     ag.VoiceDailyCallLimit,
		AllowVisitor:   ag.VoiceAllowVisitor,
		MaxConcurrent:  ag.VoiceMaxConcurrentCalls,
	}, nil
}

// reserveVoiceDailyQuota 在每日上限内预留一次配额：未超限则计数+1并返回 true。
// limit<=0 表示不限。可在测试中替换。
var reserveVoiceDailyQuota = func(agentID int64, limit int) bool {
	if limit <= 0 || store.RDB == nil {
		return true
	}
	ctx := context.Background()
	key := fmt.Sprintf("im:voice:daily:%d:%s", agentID, time.Now().Format("20060102"))
	n, err := store.RDB.Incr(ctx, key).Result()
	if err != nil {
		logger.L.Warnf("call trace: daily_quota redis_err agent=%d err=%v", agentID, err)
		return true // redis 抖动时放行，避免误杀
	}
	if n == 1 {
		store.RDB.Expire(ctx, key, 48*time.Hour)
	}
	if n > int64(limit) {
		store.RDB.Decr(ctx, key) // 回退本次未消费的计数
		logger.L.Warnf("call trace: daily_quota exceeded agent=%d limit=%d current=%d", agentID, limit, n)
		return false
	}
	return true
}

// releaseVoiceDailyQuota 退还一次配额（DECR）；用于排队条目被取消/超时/断连时回退。
// limit<=0 时与 reserveVoiceDailyQuota 行为对称地跳过。可在测试中替换。
var releaseVoiceDailyQuota = func(agentID int64, limit int) {
	if limit <= 0 || store.RDB == nil {
		return
	}
	ctx := context.Background()
	key := fmt.Sprintf("im:voice:daily:%d:%s", agentID, time.Now().Format("20060102"))
	store.RDB.Decr(ctx, key)
}

// ensureVoiceAgentOwner 校验 agent 存在、为 type=4 语音大模型、且归属于 userID。
// 用于自测拨打，防他人借测试消耗用户 key。可在测试中替换为 mock。
var ensureVoiceAgentOwner = func(userID, agentID int64) error {
	if store.DB == nil {
		return nil // 测试环境
	}
	var ag struct {
		OwnerID      int64
		ProviderType int16
	}
	if err := store.DB.Model(&model.Agent{}).
		Select("owner_id, provider_type").
		Where("id = ? AND status = 1", agentID).
		Scan(&ag).Error; err != nil {
		return fmt.Errorf("query agent: %w", err)
	}
	if ag.OwnerID == 0 {
		return fmt.Errorf("agent %d not found or disabled", agentID)
	}
	if ag.OwnerID != userID {
		return fmt.Errorf("only the owner can test this agent")
	}
	if ag.ProviderType != model.AgentProviderVoice {
		return fmt.Errorf("agent %d is not a voice model", agentID)
	}
	return nil
}

// lookupCallerName 查询用户昵称用于来电显示。
// 优先取 nickname，fallback 到 username。
var lookupCallerName = func(userID int64) string {
	if store.DB == nil {
		return "" // 测试环境
	}
	var u struct {
		Nickname string
		Username string
	}
	if err := store.DB.Model(&model.User{}).
		Select("nickname, username").
		Where("id = ?", userID).
		Scan(&u).Error; err != nil {
		return ""
	}
	if u.Nickname != "" {
		return u.Nickname
	}
	return u.Username
}
