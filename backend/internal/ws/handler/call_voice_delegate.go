package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	apiservice "github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// voiceDelegateKey 是会话级语音托管的 redis key（与文字 im:delegate 解耦）。
func voiceDelegateKey(sessionID string, userID int64) string {
	return fmt.Sprintf("im:voice_delegate:%s:%d", sessionID, userID)
}

// HandleCallVoiceDelegateStart 为会话绑定语音托管 agent（type=4，owner 本人）。
func HandleCallVoiceDelegateStart(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.CallVoiceDelegateStartPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("invalid payload"))
		return
	}
	agentID, err := strconv.ParseInt(payload.AgentID, 10, 64)
	if err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("invalid agent_id"))
		return
	}
	userID := conn.GetUserID()
	ctx := context.Background()

	// 校验：agent 本人所有、type=4、可用
	var agent model.Agent
	if err := store.DB.Select("id", "owner_id", "status", "provider_type").
		First(&agent, agentID).Error; err != nil ||
		agent.OwnerID != userID || agent.Status != 1 || agent.ProviderType != model.AgentProviderVoice {
		conn.SendPayload(protocol.CmdCallVoiceDelegateAck, pkt.Seq, protocol.CallVoiceDelegateAckPayload{
			SessionID: payload.SessionID, AgentID: payload.AgentID, Active: false,
		})
		return
	}
	// 校验：用户是该会话的人类成员
	var member model.SessionMember
	if err := store.DB.Where("session_id = ? AND member_id = ? AND member_type = 1", payload.SessionID, userID).
		First(&member).Error; err != nil {
		conn.SendPayload(protocol.CmdCallVoiceDelegateAck, pkt.Seq, protocol.CallVoiceDelegateAckPayload{
			SessionID: payload.SessionID, AgentID: payload.AgentID, Active: false,
		})
		return
	}
	if err := store.RDB.HSet(ctx, voiceDelegateKey(payload.SessionID, userID), "agent_id", agentID).Err(); err != nil {
		logger.L.Warnf("voice_delegate_start hset error session=%s user=%d: %v", payload.SessionID, userID, err)
		conn.SendPayload(protocol.CmdCallVoiceDelegateAck, pkt.Seq, protocol.CallVoiceDelegateAckPayload{
			SessionID: payload.SessionID, AgentID: payload.AgentID, Active: false,
		})
		return
	}
	conn.SendPayload(protocol.CmdCallVoiceDelegateAck, pkt.Seq, protocol.CallVoiceDelegateAckPayload{
		SessionID: payload.SessionID, AgentID: payload.AgentID, Active: true,
	})
}

// HandleCallVoiceDelegateStop 解绑会话语音托管。
func HandleCallVoiceDelegateStop(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.CallVoiceDelegateStopPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("invalid payload"))
		return
	}
	userID := conn.GetUserID()
	store.RDB.Del(context.Background(), voiceDelegateKey(payload.SessionID, userID))
	conn.SendPayload(protocol.CmdCallVoiceDelegateAck, pkt.Seq, protocol.CallVoiceDelegateAckPayload{
		SessionID: payload.SessionID, Active: false,
	})
}

// resolveCalleeVoiceAgent 解析 callee 在该会话的语音托管 agent：
// 会话级 im:voice_delegate 优先，其次用户级 VoiceAutoDelegateAgentID。可在测试中替换。
var resolveCalleeVoiceAgent = func(calleeID int64, sessionID string) (int64, bool) {
	if store.RDB != nil && sessionID != "" {
		if v, err := store.RDB.HGet(context.Background(), voiceDelegateKey(sessionID, calleeID), "agent_id").Int64(); err == nil && v > 0 {
			logger.L.Debugf("call trace: resolve_voice_agent redis callee=%d session=%s agent=%d", calleeID, sessionID, v)
			return v, true
		}
	}
	agentID, ok := apiservice.LoadUserVoiceAutoDelegateAgentID(calleeID)
	if ok {
		logger.L.Debugf("call trace: resolve_voice_agent fallback callee=%d agent=%d", calleeID, agentID)
	}
	return agentID, ok
}
