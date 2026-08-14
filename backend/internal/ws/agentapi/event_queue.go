package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const queuedAgentEventDrainBatch = 128

const queuedAgentEventMaxDispatchAttempts = 5

type queuedAgentEvent struct {
	AgentID  int64           `json:"agent_id"`
	OwnerID  int64           `json:"owner_id"`
	Cmd      string          `json:"cmd"`
	EventKey string          `json:"event_key"`
	Payload  json.RawMessage `json:"payload"`
}

// buildQueuedAgentEvent 构造离线队列事件。ownerID>0 时严格按 owner 入队，drain 时只投递给该 owner 的连接；
// ownerID=0 兼容主连接旧路径。
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
	case protocol.CmdEventEdit:
		var keyPayload protocol.EditEventPayload
		if err := json.Unmarshal(rawPayload, &keyPayload); err != nil {
			return nil, false
		}
		sessionID := strings.TrimSpace(keyPayload.SessionID)
		if sessionID == "" || keyPayload.MsgID <= 0 || strings.TrimSpace(keyPayload.Content) == "" {
			return nil, false
		}
		eventKey := fmt.Sprintf("%d:%s:%s:%d", agentID, cmd, sessionID, keyPayload.MsgID)
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

func enqueueQueuedAgentEvent(ctx context.Context, evt queuedAgentEvent) bool {
	if evt.AgentID <= 0 || strings.TrimSpace(evt.EventKey) == "" {
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

func (m *Manager) dispatchQueuedAgentEvent(conn *agentConn, evt model.AgentQueuedEvent) bool {
	if conn == nil || evt.AgentID <= 0 || evt.AgentID != conn.agentID {
		return false
	}
	if !m.ensureAgentConnectionAuthoritative(conn) {
		return false
	}
	if evt.Cmd == protocol.CmdEventEdit {
		outboundCmd, outbound := conn.resolveAgentEventOutbound(evt.Cmd, json.RawMessage(evt.Payload))
		if !conn.sendPayload(outboundCmd, 0, outbound) {
			return false
		}
		deleteQueuedAgentEventRecord(context.Background(), evt.ID, evt.EventKey)
		return true
	}
	// 先注册 pending 再发送，避免 ack 竞态
	m.registerQueuedAgentEventAck(evt)
	outboundCmd, outbound := conn.resolveAgentEventOutbound(evt.Cmd, json.RawMessage(evt.Payload))
	if !conn.sendPayload(outboundCmd, 0, outbound) {
		m.rollbackPendingEventAck(evt.EventKey)
		return false
	}
	return true
}

func deleteQueuedAgentEventRecord(ctx context.Context, recordID int64, eventKey string) {
	if recordID <= 0 || store.DB == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := store.DB.WithContext(ctx).Delete(&model.AgentQueuedEvent{}, recordID).Error; err != nil {
		logger.L.Warnf("delete queued agent event failed id=%d key=%s err=%v", recordID, eventKey, err)
	}
}

func (m *Manager) drainQueuedAgentEvents(conn *agentConn, maxCount int) {
	if conn == nil || conn.agentID <= 0 {
		return
	}
	if store.DB == nil {
		return
	}
	if maxCount <= 0 {
		maxCount = queuedAgentEventDrainBatch
	}

	ctx := context.Background()
	redeliverBefore := time.Now().Add(-m.eventAckWait)
	// 按 (agent_id, owner_id) 过滤：当前连接只 drain 自己 owner 的事件。
	// 老数据/主连接路径的 owner_id=0 视为「无 owner 归属」，由主连接 drain（主人对自己的事件天然有权见）。
	// 主连接：drain 自己 owner + owner_id=0 的老事件；
	// 共享连接：仅 drain 自己 owner 的事件，绝不碰其他 owner（含 0）的。
	query := store.DB.WithContext(ctx).
		Where("agent_id = ?", conn.agentID).
		Where("dispatched_at IS NULL OR dispatched_at <= ?", redeliverBefore).
		Where("dispatch_attempts < ?", queuedAgentEventMaxDispatchAttempts)
	if conn.isPrimary {
		query = query.Where("owner_id = ? OR owner_id = 0", conn.ownerID)
	} else {
		query = query.Where("owner_id = ?", conn.ownerID)
	}
	var events []model.AgentQueuedEvent
	if err := query.
		Order("created_at ASC, id ASC").
		Limit(maxCount).
		Find(&events).Error; err != nil {
		logger.L.Warnf("load queued agent events failed agent=%d owner=%d err=%v", conn.agentID, conn.ownerID, err)
		return
	}

	for _, evt := range events {
		if !m.dispatchQueuedAgentEvent(conn, evt) {
			break
		}
		now := time.Now().UTC()
		if err := store.DB.WithContext(ctx).
			Model(&model.AgentQueuedEvent{}).
			Where("id = ?", evt.ID).
			Updates(map[string]interface{}{
				"dispatch_attempts": gorm.Expr("dispatch_attempts + 1"),
				"dispatched_at":     now,
				"updated_at":        now,
			}).Error; err != nil {
			logger.L.Warnf("mark queued agent event dispatched failed agent=%d id=%d key=%s err=%v", conn.agentID, evt.ID, evt.EventKey, err)
			break
		}
	}
}
