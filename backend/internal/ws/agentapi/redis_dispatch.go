package agentapi

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/askie/grix/backend/internal/gateway/provisioning"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

const redisCmdKickAgent = "kick_agent"

func HandleRedisDispatch(cmd string, payload json.RawMessage) bool {
	// Try forwarded local_action commands first.
	if HandleForwardedLocalActionDispatch(cmd, payload) {
		return true
	}

	if HandleForwardedLifecycleDispatch(cmd, payload) {
		return true
	}

	if HandleForwardedMcpFrameDispatch(cmd, payload) {
		return true
	}

	if cmd == redisCmdKickAgent {
		handleKickAgentDispatch(payload)
		return true
	}

	if cmd == redisCmdBroadcastConnectorUpgradePush {
		// 走 Manager 的后台工作组：这个广播会回头遍历在线 agent、下发 local_action
		// 并落 pending（写 DB/Redis），裸 goroutine 会活过关停。
		if mgr := GetGlobalManager(); mgr != nil {
			mgr.goBackground(handleBroadcastConnectorUpgradePush)
		} else {
			logger.L.Warnf("drop connector upgrade push broadcast: manager unavailable")
		}
		return true
	}

	if cmd == provisioning.RedisCmdConfigureGatewayProvider {
		var cfg provisioning.GatewayProviderConfig
		if err := json.Unmarshal(payload, &cfg); err != nil {
			logger.L.Warnf("decode configure_gateway_provider broadcast failed: %v", err)
			return true
		}
		if mgr := GetGlobalManager(); mgr != nil {
			mgr.goBackground(func() { handleBroadcastConfigureGatewayProvider(cfg) })
		} else {
			logger.L.Warnf("drop configure_gateway_provider broadcast: manager unavailable")
		}
		return true
	}

	if cmd == provisioning.RedisCmdApplyRelayState {
		var cfg provisioning.RelayStateApplyConfig
		if err := json.Unmarshal(payload, &cfg); err != nil {
			logger.L.Warnf("decode apply_relay_state broadcast failed: %v", err)
			return true
		}
		if mgr := GetGlobalManager(); mgr != nil {
			mgr.goBackground(func() { handleBroadcastApplyRelayState(cfg) })
		} else {
			logger.L.Warnf("drop apply_relay_state broadcast: manager unavailable")
		}
		return true
	}

	if cmd == protocol.RedisCmdAgentShareSync {
		var p protocol.AgentShareSyncPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			logger.L.Warnf("decode agent share sync failed: %v", err)
			return true
		}
		globalMu.RLock()
		manager := globalManager
		globalMu.RUnlock()
		if manager != nil && p.AgentID > 0 {
			// 先踢掉已失去授权的被共享者连接（撤销即时生效兜底），再下发最新名单。
			manager.enforceAuthorizedShareConns(p.AgentID)
			manager.pushShareSetToPrimary(p.AgentID)
		}
		return true
	}

	if cmd == protocol.RedisCmdSkillLibraryChanged {
		var p protocol.SkillLibraryChangedPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			logger.L.Warnf("decode skill library changed failed: %v", err)
			return true
		}
		globalMu.RLock()
		manager := globalManager
		globalMu.RUnlock()
		if manager != nil && p.OwnerID > 0 {
			manager.pushSkillSyncToOwner(p.OwnerID, p.Name, p.Version)
		}
		return true
	}

	if cmd == protocol.RedisCmdAgentProfileSync {
		var p protocol.AgentProfileSyncPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			logger.L.Warnf("decode agent profile sync failed: %v", err)
			return true
		}
		globalMu.RLock()
		manager := globalManager
		globalMu.RUnlock()
		if manager != nil && p.AgentID > 0 {
			manager.pushProfileToPrimary(p.AgentID)
		}
		return true
	}

	if cmd == redisCmdForwardDelegateRetry {
		var envelope durableRetryEnvelope
		if err := json.Unmarshal(payload, &envelope); err != nil {
			logger.L.Warnf("decode forwarded agent api retry failed: %v", err)
			return true
		}
		globalMu.RLock()
		manager := globalManager
		globalMu.RUnlock()
		if manager != nil {
			// A missing local connection is not requeued here. The durable retry
			// claim remains available for the reconnect drain.
			manager.HandleForwardedDelegateRetry(envelope)
		}
		return true
	}

	if cmd != redisCmdForwardDelegateEvent {
		return false
	}

	var evt DelegateEventPayload
	if err := json.Unmarshal(payload, &evt); err != nil {
		logger.L.Warnf("decode forwarded agent api event failed: %v", err)
		return true
	}

	globalMu.RLock()
	manager := globalManager
	globalMu.RUnlock()
	if manager == nil {
		if evt.Command {
			logger.L.Infof("drop forwarded command delegate event: manager unavailable agent=%d event=%s", evt.AgentID, evt.EventID)
			return true
		}
		if enqueueDelegateEvent(nil, evt) {
			return true
		}
		logger.L.Warnf("drop forwarded agent api event: manager unavailable agent=%d", evt.AgentID)
		return true
	}

	if ok := manager.HandleForwardedDelegateEvent(evt); !ok {
		logger.L.Warnf("drop forwarded agent api event: local channel unavailable agent=%d event=%s", evt.AgentID, evt.EventID)
	}
	return true
}

// flexInt64 兼容 JSON 数字与字符串两种形式的 int64。
// publishKickAgent 历史上把 agent_id 编码为字符串，而这里此前按 int64 解码必然失败，
// 导致跨节点 kick_agent 从未生效；用宽容解码修复并保持对两种编码兼容。
type flexInt64 int64

func (f *flexInt64) UnmarshalJSON(data []byte) error {
	trimmed := strings.Trim(string(data), `"`)
	if trimmed == "" || trimmed == "null" {
		*f = 0
		return nil
	}
	v, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return err
	}
	*f = flexInt64(v)
	return nil
}

func handleKickAgentDispatch(payload json.RawMessage) {
	var msg struct {
		AgentID flexInt64 `json:"agent_id"`
		OwnerID flexInt64 `json:"owner_id"`
		Reason  string    `json:"reason"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		logger.L.Warnf("decode kick_agent payload failed: %v", err)
		return
	}

	globalMu.RLock()
	manager := globalManager
	globalMu.RUnlock()
	if manager == nil {
		return
	}

	// owner_id > 0 时只踢该 owner 的单条连接（管控 API 按连接踢线）；否则踢全部。
	if msg.OwnerID > 0 {
		manager.KickAgentOwner(int64(msg.AgentID), int64(msg.OwnerID), msg.Reason)
	} else {
		manager.KickAgent(int64(msg.AgentID), msg.Reason)
	}
	logger.L.Infof("kicked agent %d owner %d via redis dispatch: %s", msg.AgentID, msg.OwnerID, msg.Reason)
}
