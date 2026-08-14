package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/agentreceive"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// Lua: atomic HSET for delegate start (check existence → set agent_id + started_at)
var delegateStartLua = `
local key = KEYS[1]
if redis.call('EXISTS', key) == 1 then
    redis.call('HSET', key, 'agent_id', ARGV[1], 'max_consecutive_replies', ARGV[3], 'updated_at', ARGV[2])
    return 'UPDATED'
end
redis.call('HSET', key, 'agent_id', ARGV[1], 'started_at', ARGV[2], 'max_consecutive_replies', ARGV[3])
return 'CREATED'
`

// Lua: atomic DEL for delegate stop (check existence → delete → return agent_id)
var delegateStopLua = `
local key = KEYS[1]
if redis.call('EXISTS', key) == 0 then
    return redis.error_reply('NOT_DELEGATED')
end
local aid = redis.call('HGET', key, 'agent_id')
redis.call('DEL', key)
return aid
`

func HandleDelegateStart(hub HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.DelegateStartPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("delegate_start payload error: %v", err)
		return
	}

	userID := conn.GetUserID()
	ctx := context.Background()

	// Validate agent ownership
	var agent model.Agent
	if err := store.DB.First(&agent, payload.AgentID).Error; err != nil || agent.OwnerID != userID || agent.Status == 3 || agent.ProviderType == model.AgentProviderVoice {
		logger.L.Infof(
			"[DelegateTrace] delegate_start rejected session=%s user=%d agent=%d reason=agent_invalid_or_not_owned",
			payload.SessionID,
			userID,
			payload.AgentID,
		)
		conn.SendPayload(protocol.CmdDelegateAck, pkt.Seq, protocol.DelegateAckPayload{
			SessionID: payload.SessionID, AgentID: payload.AgentID, Active: false,
		})
		return
	}

	// Validate session membership: user must be a member
	var member model.SessionMember
	if err := store.DB.Where("session_id = ? AND member_id = ? AND member_type = 1", payload.SessionID, userID).First(&member).Error; err != nil {
		logger.L.Infof(
			"[DelegateTrace] delegate_start rejected session=%s user=%d agent=%d reason=user_not_human_member",
			payload.SessionID,
			userID,
			payload.AgentID,
		)
		conn.SendPayload(protocol.CmdDelegateAck, pkt.Seq, protocol.DelegateAckPayload{
			SessionID: payload.SessionID, AgentID: payload.AgentID, Active: false,
		})
		return
	}
	if agent.ProviderType == model.AgentProviderAPI && !wsagentapi.IsAgentChannelAvailable(payload.AgentID) {
		logger.L.Infof(
			"[DelegateTrace] delegate_start rejected session=%s user=%d agent=%d reason=agent_api_channel_unavailable",
			payload.SessionID,
			userID,
			payload.AgentID,
		)
		conn.SendPayload(protocol.CmdAgentDeliveryStatus, pkt.Seq, protocol.AgentDeliveryStatusPayload{
			SessionID: payload.SessionID,
			OwnerID:   userID,
			AgentID:   payload.AgentID,
			Scope:     protocol.AgentDeliveryScopeDelegate,
			Status:    protocol.AgentDeliveryStatusFailed,
			Code:      protocol.AgentDeliveryCodeChannelUnavailable,
			Msg:       "delegated agent channel unavailable",
			UpdatedAt: time.Now().UnixMilli(),
		})
		conn.SendPayload(protocol.CmdDelegateAck, pkt.Seq, protocol.DelegateAckPayload{
			SessionID: payload.SessionID, AgentID: payload.AgentID, Active: false,
		})
		return
	}

	maxConsecutive := payload.MaxConsecutiveReplies
	if maxConsecutive <= 0 {
		maxConsecutive = delegateMaxRepliesFromAgentConfig(agent.Config)
	}
	maxConsecutive = normalizeDelegateMaxConsecutiveReplies(maxConsecutive)

	// Atomic Redis set
	delegateKey := fmt.Sprintf("im:delegate:%s:%d", payload.SessionID, userID)
	result, err := store.RDB.Eval(ctx, delegateStartLua, []string{delegateKey},
		payload.AgentID, time.Now().Unix(), maxConsecutive).Result()
	if err != nil {
		logger.L.Warnf("delegate_start lua error: %v", err)
		conn.SendPayload(protocol.CmdDelegateAck, pkt.Seq, protocol.DelegateAckPayload{
			SessionID: payload.SessionID, AgentID: payload.AgentID, Active: false,
		})
		return
	}
	delegateAction := "start"
	if fmt.Sprint(result) == "UPDATED" {
		delegateAction = "update"
	} else {
		// New delegation start should reset consecutive counter.
		store.RDB.Del(ctx, delegateStreakKey(payload.SessionID, userID))
	}
	logger.L.Infof(
		"[DelegateTrace] delegate_start accepted session=%s user=%d agent=%d action=%s max_consecutive=%d",
		payload.SessionID,
		userID,
		payload.AgentID,
		delegateAction,
		maxConsecutive,
	)

	// Audit log
	store.DB.Create(&model.DelegationLog{
		SessionID: payload.SessionID,
		UserID:    userID,
		AgentID:   payload.AgentID,
		Action:    delegateAction,
	})

	// Broadcast delegate_ack to all user devices
	ack := protocol.DelegateAckPayload{
		SessionID:             payload.SessionID,
		AgentID:               payload.AgentID,
		Active:                true,
		MaxConsecutiveReplies: maxConsecutive,
	}
	broadcastToUser(hub, ctx, userID, protocol.CmdDelegateAck, ack)
}

func HandleDelegateStop(hub HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.DelegateStopPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("delegate_stop payload error: %v", err)
		return
	}

	userID := conn.GetUserID()
	ctx := context.Background()
	publishDelegateStopSignal(payload.SessionID, userID)

	delegateKey := fmt.Sprintf("im:delegate:%s:%d", payload.SessionID, userID)
	result, err := store.RDB.Eval(ctx, delegateStopLua, []string{delegateKey}).Result()
	if err != nil {
		logger.L.Warnf("delegate_stop lua error: %v", err)
		logger.L.Infof(
			"[DelegateTrace] delegate_stop rejected session=%s user=%d reason=not_delegated_or_redis_error",
			payload.SessionID,
			userID,
		)
		conn.SendPayload(protocol.CmdDelegateAck, pkt.Seq, protocol.DelegateAckPayload{
			SessionID: payload.SessionID, Active: false,
		})
		return
	}

	agentID := int64(0)
	if s, ok := result.(string); ok {
		fmt.Sscanf(s, "%d", &agentID)
	}
	store.RDB.Del(ctx, delegateStreakKey(payload.SessionID, userID))
	_ = agentreceive.ClearBuffer(ctx, payload.SessionID, 1, userID)

	// Audit log
	store.DB.Create(&model.DelegationLog{
		SessionID: payload.SessionID,
		UserID:    userID,
		AgentID:   agentID,
		Action:    "stop",
	})
	logger.L.Infof(
		"[DelegateTrace] delegate_stop accepted session=%s user=%d agent=%d",
		payload.SessionID,
		userID,
		agentID,
	)

	ack := protocol.DelegateAckPayload{
		SessionID: payload.SessionID,
		AgentID:   agentID,
		Active:    false,
	}
	broadcastToUser(hub, ctx, userID, protocol.CmdDelegateAck, ack)
}

func publishDelegateStopSignal(sessionID string, ownerID int64) {
	if sessionID == "" || ownerID <= 0 || store.JS == nil {
		return
	}

	data, _ := json.Marshal(map[string]any{
		"cmd":        "delegate_stop",
		"session_id": sessionID,
		"owner_id":   ownerID,
		"user_id":    ownerID,
	})
	if _, err := store.JS.Publish(fmt.Sprintf("ai.request.%s", sessionID), data); err != nil {
		logger.L.Warnf("delegate_stop publish cancel signal failed session=%s owner=%d: %v", sessionID, ownerID, err)
	}
}

func HandleDelegateList(hub HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	userID := conn.GetUserID()
	ctx := context.Background()

	pattern := fmt.Sprintf("im:delegate:*:%d", userID)
	var keys []string
	iter := store.RDB.Scan(ctx, 0, pattern, 100).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		logger.L.Warnf("delegate_list scan error: %v", err)
		return
	}

	delegates := make([]protocol.DelegateItem, 0, len(keys))

	// We explicitly only want to match "im:delegate:{session_id}:{user_id}".
	// Since we use the pattern "im:delegate:*:<userID>", this can incorrectly match
	// tracking keys like "im:delegate:streak:<session_id>:<userID>" or "im:delegate:rate:auto:<session_id>:<userID>".
	// We should split by ":" and only parse keys that have exactly 4 parts:
	// "im" (0), "delegate" (1), "{session_id}" (2), "{user_id}" (3).
	// If it has more parts, it's a tracking key, so skip it.
	for _, key := range keys {
		parts := strings.Split(key, ":")
		if len(parts) != 4 || parts[0] != "im" || parts[1] != "delegate" {
			continue
		}

		actualSessionID := parts[2]
		uidStr := parts[3]

		// Ensure the uid in the key actually matches what we expect
		if uidStr != fmt.Sprintf("%d", userID) {
			continue
		}

		fields, err := store.RDB.HMGet(ctx, key, "agent_id", "max_consecutive_replies").Result()
		if err == nil && len(fields) >= 1 {
			agentID, ok := parseIntFromAny(fields[0])
			if ok && agentID > 0 {
				maxConsecutive := defaultDelegateMaxConsecutiveReplies
				if len(fields) >= 2 {
					maxConsecutive = delegateMaxRepliesFromRedis(fields[1])
				}
				delegates = append(delegates, protocol.DelegateItem{
					SessionID:             actualSessionID,
					AgentID:               int64(agentID),
					MaxConsecutiveReplies: maxConsecutive,
				})
			}
		}
	}
	logger.L.Debugf("[DelegateTrace] delegate_list user=%d count=%d", userID, len(delegates))

	respPayload := protocol.DelegateListRespPayload{
		Delegates: delegates,
	}

	conn.SendPayload(protocol.CmdDelegateListResp, pkt.Seq, respPayload)
}
