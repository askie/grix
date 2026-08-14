package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/datatypes"
	"gorm.io/gorm/clause"
)

const (
	serviceQueuedDelegateEventTTL     = 48 * time.Hour
	serviceQueuedDelegateEventMaxKeep = 512
)

// AgentDelegateEvent is the service-layer representation of a delegate event
// that should be delivered to an Agent API connection.
type AgentDelegateEvent struct {
	EventID     string `json:"event_id"`
	EventType   string `json:"event_type"`
	AgentID     int64  `json:"agent_id,string"`
	OwnerID     int64  `json:"owner_id,string"`
	SessionID   string `json:"session_id"`
	ThreadID    string `json:"thread_id,omitempty"`
	SessionType int16  `json:"session_type"`
	MsgID       int64  `json:"msg_id,string"`
	SenderID    int64  `json:"sender_id,string"`
	MsgType     int16  `json:"msg_type,omitempty"`
	Content     string `json:"content"`
	CreatedAt   int64  `json:"created_at"`
}

// AgentChannelBridge decouples the service layer from the Agent API runtime so
// ws/agentapi can depend on service without creating an import cycle.
// PushAgentEvent 的 ownerID 用于 agent 共享多连接物理隔离:
//   - >0: 严格按 (agentID, ownerID) 路由到对应连接
//   - 0:  兼容旧路径,回退到主连接
type AgentChannelBridge interface {
	PushAgentEvent(agentID, ownerID int64, cmd string, payload interface{}) bool
	PushDelegateEvent(event AgentDelegateEvent) bool
	IsAgentChannelAvailable(agentID int64) bool
	GetAgentClientType(agentID int64) string
}

var agentChannelBridgeState struct {
	mu     sync.RWMutex
	bridge AgentChannelBridge
}

// SetAgentChannelBridge installs the runtime bridge used by service-layer
// operations that need to reach connected Agent API channels.
func SetAgentChannelBridge(bridge AgentChannelBridge) {
	agentChannelBridgeState.mu.Lock()
	agentChannelBridgeState.bridge = bridge
	agentChannelBridgeState.mu.Unlock()
}

// pushAgentChannelEvent 把事件推给 agentID 在 ownerID 维度下的连接。
// ownerID 必须 >0：严格按 (agentID, ownerID) 路由，确保 B 在 B↔X 私聊撤回/编辑的事件
// 落到 B 的 connector；ownerID<=0 非法，路由层 fail-closed 直接拒绝（不再回退主连接）。
// 群聊等「投主实例」语义由调用方先用 resolveAgentPrimaryOwnerID 显式解析 agent.OwnerID。
func pushAgentChannelEvent(agentID, ownerID int64, cmd string, payload interface{}) bool {
	agentChannelBridgeState.mu.RLock()
	bridge := agentChannelBridgeState.bridge
	agentChannelBridgeState.mu.RUnlock()
	if bridge == nil {
		return queueAgentChannelEvent(context.Background(), agentID, ownerID, cmd, payload)
	}
	return bridge.PushAgentEvent(agentID, ownerID, cmd, payload)
}

// resolveAgentPrimaryOwnerID 解析 agent 主连接身份（agent.OwnerID）。
// 群聊场景共享只在私聊生效，agent 事件统一投主实例；路由层已将 ownerID=0 视为非法
// （fail-closed），因此调用方必须显式解析 agent 主人后按精确路由推送。
// 解析失败返回 0（后续推送会被路由层拒绝并记 Warn 日志，不会串到其他 owner 的连接）。
func resolveAgentPrimaryOwnerID(agentID int64) int64 {
	if agentID <= 0 || store.DB == nil {
		return 0
	}
	var agent model.Agent
	if err := store.DB.Select("id", "owner_id").First(&agent, agentID).Error; err != nil {
		logger.L.Warnf("resolve agent primary owner failed agent=%d err=%v", agentID, err)
		return 0
	}
	return agent.OwnerID
}

func pushDelegateAgentEvent(event AgentDelegateEvent) bool {
	agentChannelBridgeState.mu.RLock()
	bridge := agentChannelBridgeState.bridge
	agentChannelBridgeState.mu.RUnlock()
	if bridge == nil {
		return queueDelegateAgentEvent(context.Background(), event)
	}
	return bridge.PushDelegateEvent(event)
}

func isAgentChannelAvailable(agentID int64) bool {
	return isAgentChannelAvailableWithContext(context.Background(), agentID)
}

func isAgentChannelAvailableWithContext(ctx context.Context, agentID int64) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	agentChannelBridgeState.mu.RLock()
	bridge := agentChannelBridgeState.bridge
	agentChannelBridgeState.mu.RUnlock()
	if bridge == nil {
		return loadAgentRoute(ctx, agentID) != ""
	}
	return bridge.IsAgentChannelAvailable(agentID)
}

func getAgentClientType(agentID int64) string {
	agentChannelBridgeState.mu.RLock()
	bridge := agentChannelBridgeState.bridge
	agentChannelBridgeState.mu.RUnlock()
	if bridge == nil {
		return ""
	}
	return bridge.GetAgentClientType(agentID)
}

func agentRouteKey(agentID int64) string {
	return fmt.Sprintf("im:agent_api:route:%d", agentID)
}

func loadAgentRoute(ctx context.Context, agentID int64) string {
	if agentID <= 0 || store.RDB == nil {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}

	nodeID, err := store.RDB.Get(ctx, agentRouteKey(agentID)).Result()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(nodeID)
}

func queuedDelegateEventListKey(agentID int64) string {
	return fmt.Sprintf("im:agent_api:queued_events:%d", agentID)
}

func queuedDelegateEventDedupKey(eventID string) string {
	return fmt.Sprintf("im:agent_api:queued_events:dedup:%s", eventID)
}

func queueDelegateAgentEvent(ctx context.Context, event AgentDelegateEvent) bool {
	if event.AgentID <= 0 || store.RDB == nil {
		return false
	}
	eventID := strings.TrimSpace(event.EventID)
	if eventID == "" {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}

	raw, err := json.Marshal(event)
	if err != nil {
		logger.L.Warnf("marshal queued delegate event failed agent=%d event=%s err=%v", event.AgentID, eventID, err)
		return false
	}

	dedupKey := queuedDelegateEventDedupKey(eventID)
	enqueued, err := store.RDB.SetNX(ctx, dedupKey, 1, serviceQueuedDelegateEventTTL).Result()
	if err != nil {
		logger.L.Warnf("setnx queued delegate event failed agent=%d event=%s err=%v", event.AgentID, eventID, err)
		return false
	}
	if !enqueued {
		return true
	}

	listKey := queuedDelegateEventListKey(event.AgentID)
	pipe := store.RDB.TxPipeline()
	pipe.RPush(ctx, listKey, raw)
	pipe.LTrim(ctx, listKey, -serviceQueuedDelegateEventMaxKeep, -1)
	pipe.Expire(ctx, listKey, serviceQueuedDelegateEventTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		logger.L.Warnf("persist queued delegate event failed agent=%d event=%s err=%v", event.AgentID, eventID, err)
		_ = store.RDB.Del(ctx, dedupKey).Err()
		return false
	}

	logger.L.Infof(
		"queued delegate event for retry session=%s owner=%d agent=%d msg_id=%d event_id=%s",
		event.SessionID,
		event.OwnerID,
		event.AgentID,
		event.MsgID,
		eventID,
	)
	return true
}

type queuedAgentEvent struct {
	AgentID  int64           `json:"agent_id"`
	OwnerID  int64           `json:"owner_id"`
	Cmd      string          `json:"cmd"`
	EventKey string          `json:"event_key"`
	Payload  json.RawMessage `json:"payload"`
}

// queueAgentChannelEvent 把事件持久化进 agent_queued_events 表，等连接恢复时按
// (agent_id, owner_id) drain。ownerID>0 时严格按 owner 投递，避免共享场景下跨 owner 串数据；
// ownerID=0 兼容主连接旧路径（drain 时按主人路由）。
func queueAgentChannelEvent(ctx context.Context, agentID, ownerID int64, cmd string, payload interface{}) bool {
	evt, ok := buildQueuedAgentEvent(agentID, ownerID, cmd, payload)
	if !ok {
		return false
	}
	if store.DB == nil {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}

	record := model.AgentQueuedEvent{
		AgentID:  evt.AgentID,
		OwnerID:  evt.OwnerID,
		Cmd:      evt.Cmd,
		EventKey: evt.EventKey,
		Payload:  datatypes.JSON(evt.Payload),
	}
	if err := store.DB.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "event_key"}},
			DoNothing: true,
		}).
		Create(&record).Error; err != nil {
		logger.L.Warnf("persist queued agent event failed agent=%d cmd=%s key=%s err=%v", evt.AgentID, evt.Cmd, evt.EventKey, err)
		return false
	}

	logger.L.Infof("queued agent event agent=%d cmd=%s key=%s", evt.AgentID, evt.Cmd, evt.EventKey)
	return true
}

func buildQueuedAgentEvent(agentID, ownerID int64, cmd string, payload interface{}) (*queuedAgentEvent, bool) {
	if agentID <= 0 {
		return nil, false
	}

	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, false
	}

	switch strings.TrimSpace(cmd) {
	case "event_revoke":
		var keyPayload protocol.AgentRevokeEventPayload
		if err := json.Unmarshal(rawPayload, &keyPayload); err != nil {
			return nil, false
		}
		sessionID := strings.TrimSpace(keyPayload.SessionID)
		if sessionID == "" || keyPayload.MsgID <= 0 {
			return nil, false
		}
		eventKey := fmt.Sprintf("%d:%s:%s:%d", agentID, cmd, sessionID, keyPayload.MsgID)
		keyPayload.EventID = eventKey
		rawPayload, err = json.Marshal(keyPayload)
		if err != nil {
			return nil, false
		}
		return &queuedAgentEvent{
			AgentID:  agentID,
			OwnerID:  ownerID,
			Cmd:      cmd,
			EventKey: eventKey,
			Payload:  rawPayload,
		}, true
	default:
		return nil, false
	}
}
