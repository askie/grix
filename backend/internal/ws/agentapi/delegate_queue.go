package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/redis/go-redis/v9"
)

const (
	queuedDelegateEventTTL        = 48 * time.Hour
	queuedDelegateEventMaxKeep    = 512
	queuedDelegateEventDrainBatch = 128
)

// queuedDelegateEventListKey 按 agentID 分键（单一队列）。
// agent 共享的 owner 隔离在 drain 时按 conn.ownerID 过滤（见 drainQueuedDelegateEvents），
// 而非分键——这样既隔离，又不破坏「离线即入队」的既有契约。
func queuedDelegateEventListKey(agentID int64) string {
	return fmt.Sprintf("im:agent_api:queued_events:%d", agentID)
}

func queuedDelegateEventDedupKey(eventID string) string {
	return fmt.Sprintf("im:agent_api:queued_events:dedup:%s", eventID)
}

func enqueueDelegateEvent(ctx context.Context, evt DelegateEventPayload) bool {
	if evt.AgentID <= 0 {
		return false
	}
	if evt.Command {
		return false
	}
	if store.RDB == nil {
		return false
	}
	eventID := strings.TrimSpace(evt.EventID)
	if eventID == "" {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}

	raw, err := json.Marshal(evt)
	if err != nil {
		logger.L.Warnf("marshal queued delegate event failed agent=%d event=%s err=%v", evt.AgentID, eventID, err)
		return false
	}

	dedupKey := queuedDelegateEventDedupKey(eventID)
	enqueued, err := store.RDB.SetNX(ctx, dedupKey, 1, queuedDelegateEventTTL).Result()
	if err != nil {
		logger.L.Warnf("setnx queued delegate event failed agent=%d event=%s err=%v", evt.AgentID, eventID, err)
		return false
	}
	if !enqueued {
		return true
	}

	listKey := queuedDelegateEventListKey(evt.AgentID)
	pipe := store.RDB.TxPipeline()
	pipe.RPush(ctx, listKey, raw)
	pipe.LTrim(ctx, listKey, -queuedDelegateEventMaxKeep, -1)
	pipe.Expire(ctx, listKey, queuedDelegateEventTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		logger.L.Warnf("persist queued delegate event failed agent=%d event=%s err=%v", evt.AgentID, eventID, err)
		_ = store.RDB.Del(ctx, dedupKey).Err()
		return false
	}

	logger.L.Infof(
		"queued delegate event for retry session=%s owner=%d agent=%d msg_id=%d event_id=%s",
		evt.SessionID,
		evt.OwnerID,
		evt.AgentID,
		evt.MsgID,
		eventID,
	)
	return true
}

func clearQueuedDelegateEventDedup(ctx context.Context, eventID string) {
	if store.RDB == nil {
		return
	}
	normalizedEventID := strings.TrimSpace(eventID)
	if normalizedEventID == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	_ = store.RDB.Del(ctx, queuedDelegateEventDedupKey(normalizedEventID)).Err()
}

func (m *Manager) drainQueuedDelegateEvents(conn *agentConn, maxCount int) {
	if conn == nil || conn.agentID <= 0 {
		return
	}
	if store.RDB == nil {
		return
	}
	if maxCount <= 0 {
		maxCount = queuedDelegateEventDrainBatch
	}

	ctx := context.Background()
	listKey := queuedDelegateEventListKey(conn.agentID)
	// held: 非本连接 owner 的事件 / 投递失败的事件，drain 结束后原样放回队列。
	var held []string
	for i := 0; i < maxCount; i++ {
		raw, err := store.RDB.LPop(ctx, listKey).Result()
		if err == redis.Nil {
			break
		}
		if err != nil {
			logger.L.Warnf("pop queued delegate event failed agent=%d err=%v", conn.agentID, err)
			break
		}

		var evt DelegateEventPayload
		if err := json.Unmarshal([]byte(raw), &evt); err != nil {
			logger.L.Warnf("decode queued delegate event failed agent=%d err=%v raw=%s", conn.agentID, err, raw)
			continue
		}
		if evt.AgentID != conn.agentID {
			logger.L.Warnf("skip queued delegate event with mismatched agent queue=%d payload=%d event=%s", conn.agentID, evt.AgentID, evt.EventID)
			clearQueuedDelegateEventDedup(ctx, evt.EventID)
			continue
		}
		// agent 共享：只补发「本连接 owner」的积压；其它 owner 的留回队列给对应连接 drain。
		// owner=0 的遗留事件（共享功能上线前入队）只允许主连接 drain——共享连接
		// drain 走它会把主人的事件泄漏到被共享者的 connector（跨 owner 串线）。
		deliverable := evt.OwnerID == conn.ownerID || (evt.OwnerID == 0 && conn.isPrimary)
		if !deliverable {
			held = append(held, raw)
			continue
		}
		// A durable record means this list item is a legacy ACK-retry residue,
		// not a fresh offline event. Durable replay runs immediately before this
		// drain and is the sole attempt/state authority; dispatching normally
		// here would recreate queued state and duplicate the wire packet.
		if _, durable := loadDurablePendingDelegate(ctx, evt.EventID); durable {
			clearQueuedDelegateEventDedup(ctx, evt.EventID)
			continue
		}

		if !m.dispatchDelegateEvent(conn, evt) {
			held = append(held, raw)
			break
		}
		clearQueuedDelegateEventDedup(ctx, evt.EventID)
	}
	// 放回未投递的事件到队列头，保持相对顺序。
	if len(held) > 0 {
		for i := len(held) - 1; i >= 0; i-- {
			if err := store.RDB.LPush(ctx, listKey, held[i]).Err(); err != nil {
				logger.L.Warnf("requeue delegate event failed agent=%d err=%v", conn.agentID, err)
			}
		}
		_ = store.RDB.Expire(ctx, listKey, queuedDelegateEventTTL).Err()
	}
}
