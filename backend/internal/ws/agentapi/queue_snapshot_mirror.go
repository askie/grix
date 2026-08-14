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

// 队列快照镜像：事件队列的唯一权威在 agent 连接器侧（谁在跑、谁在排队），
// 连接器每次队列变化都会上报 queue_snapshot。服务端把最近一次快照按
// owner+session 存成 Redis 只读镜像（跨节点可读），停止按钮与工具栏
// "当前任务"解析优先从镜像取 running 事件，避免服务端自记的
// "最新注册 run 指针"（runBySX）与真实队列脱节导致停错目标。

const queueSnapshotMirrorTTL = 48 * time.Hour

// queueIdleTTL marks "this session's event queue was authoritatively drained".
// While set, stale composing ticks from the connector are ignored so the
// frontend composing indicator cannot outlive an empty task queue.
const queueIdleTTL = 48 * time.Hour

type queueSnapshotMirror struct {
	AgentID   int64    `json:"agent_id"`
	Running   []string `json:"running"`
	Queued    []string `json:"queued"`
	UpdatedAt int64    `json:"updated_at"`
}

func queueIdleKey(ownerID int64, sessionID string) string {
	return fmt.Sprintf("im:agent_api:queue_idle:%d:%s", ownerID, strings.TrimSpace(sessionID))
}

// IsSessionQueueIdle reports whether the latest authoritative queue_snapshot
// for this owner+session was empty. Agents that never emit queue_snapshot
// never set this flag, so their composing path is unchanged.
func IsSessionQueueIdle(ctx context.Context, ownerID int64, sessionID string) bool {
	if store.RDB == nil || ownerID <= 0 {
		return false
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	n, err := store.RDB.Exists(ctx, queueIdleKey(ownerID, sessionID)).Result()
	return err == nil && n > 0
}

func markSessionQueueIdle(ctx context.Context, ownerID int64, sessionID string) {
	if store.RDB == nil || ownerID <= 0 {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := store.RDB.Set(ctx, queueIdleKey(ownerID, sessionID), "1", queueIdleTTL).Err(); err != nil {
		logger.L.Warnf("queue idle mark failed owner=%d session=%s err=%v", ownerID, sessionID, err)
	}
}

func clearSessionQueueIdle(ctx context.Context, ownerID int64, sessionID string) {
	if store.RDB == nil || ownerID <= 0 {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := store.RDB.Del(ctx, queueIdleKey(ownerID, sessionID)).Err(); err != nil {
		logger.L.Warnf("queue idle clear failed owner=%d session=%s err=%v", ownerID, sessionID, err)
	}
}

// queueSnapshotInbound 只解析镜像所需字段；其余字段原样透传给前端，不在此关心。
// 同时兼容 "running"/"queued" 结构与 "items"/"events"/"queue" 通用事件列表结构，
// 避免 connector 使用通用列表时服务端镜像错误为空，导致停止目标解析与前端队列状态脱节。
type queueSnapshotInbound struct {
	SessionID string   `json:"session_id"`
	Running   []string `json:"running"`
	Queued    []struct {
		EventID string `json:"event_id"`
	} `json:"queued"`
	Items  []queueSnapshotInboundItem `json:"items"`
	Events []queueSnapshotInboundItem `json:"events"`
	Queue  []queueSnapshotInboundItem `json:"queue"`
}

type queueSnapshotInboundItem struct {
	EventID string `json:"event_id"`
	State   string `json:"state"`
}

func queueSnapshotMirrorKey(ownerID int64, sessionID string) string {
	return fmt.Sprintf("im:agent_api:queue_snapshot:%d:%s", ownerID, strings.TrimSpace(sessionID))
}

// storeQueueSnapshotMirror 收到 agent 上报的 queue_snapshot 时更新镜像。
// running 与 queued 均为空时删除镜像（会话空闲），解析退回旧逻辑，并返回
// 空闲会话 ID 供调用方立即清 composing。
func storeQueueSnapshotMirror(ctx context.Context, ownerID int64, agentID int64, payload json.RawMessage) (string, bool) {
	if store.RDB == nil || ownerID <= 0 {
		return "", false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var snap queueSnapshotInbound
	if err := json.Unmarshal(payload, &snap); err != nil {
		logger.L.Warnf("queue_snapshot mirror decode failed owner=%d err=%v", ownerID, err)
		return "", false
	}
	sessionID := strings.TrimSpace(snap.SessionID)
	if sessionID == "" {
		return "", false
	}

	// 合并 "running"/"queued" 与通用 "items"/"events"/"queue" 列表，
	// 保证使用通用事件列表的 connector 也能正确落镜像。
	running := make([]string, 0, len(snap.Running))
	for _, id := range snap.Running {
		if id = strings.TrimSpace(id); id != "" {
			running = append(running, id)
		}
	}
	queued := make([]string, 0, len(snap.Queued))
	for _, item := range snap.Queued {
		if id := strings.TrimSpace(item.EventID); id != "" {
			queued = append(queued, id)
		}
	}
	for _, item := range append(append(snap.Items, snap.Events...), snap.Queue...) {
		id := strings.TrimSpace(item.EventID)
		if id == "" {
			continue
		}
		switch strings.TrimSpace(item.State) {
		case "running":
			running = append(running, id)
		case "queued":
			queued = append(queued, id)
		}
	}

	logger.L.Debugf(
		"[queue-debug] storeQueueSnapshotMirror owner=%d session=%s agent=%d running=%d queued=%d payload_bytes=%d",
		ownerID, sessionID, agentID, len(running), len(queued), len(payload),
	)

	key := queueSnapshotMirrorKey(ownerID, sessionID)
	if len(running) == 0 && len(queued) == 0 {
		if err := store.RDB.Del(ctx, key).Err(); err != nil {
			logger.L.Warnf("queue_snapshot mirror delete failed owner=%d session=%s err=%v", ownerID, sessionID, err)
		}
		markSessionQueueIdle(ctx, ownerID, sessionID)
		return sessionID, true
	}
	clearSessionQueueIdle(ctx, ownerID, sessionID)
	mirror := queueSnapshotMirror{
		AgentID:   agentID,
		Running:   running,
		Queued:    queued,
		UpdatedAt: time.Now().UnixMilli(),
	}
	raw, err := json.Marshal(mirror)
	if err != nil {
		logger.L.Warnf("queue_snapshot mirror encode failed owner=%d session=%s err=%v", ownerID, sessionID, err)
		return "", false
	}
	if err := store.RDB.Set(ctx, key, raw, queueSnapshotMirrorTTL).Err(); err != nil {
		logger.L.Warnf("queue_snapshot mirror store failed owner=%d session=%s err=%v", ownerID, sessionID, err)
	}
	return "", false
}

// loadQueueSnapshotMirror 读镜像；不存在或读取失败返回 nil。
func loadQueueSnapshotMirror(ctx context.Context, ownerID int64, sessionID string) *queueSnapshotMirror {
	if store.RDB == nil || ownerID <= 0 {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	raw, err := store.RDB.Get(ctx, queueSnapshotMirrorKey(ownerID, sessionID)).Result()
	if err == redis.Nil {
		return nil
	}
	if err != nil {
		logger.L.Warnf("queue_snapshot mirror load failed owner=%d session=%s err=%v", ownerID, sessionID, err)
		return nil
	}
	var mirror queueSnapshotMirror
	if err := json.Unmarshal([]byte(raw), &mirror); err != nil {
		logger.L.Warnf("queue_snapshot mirror decode failed owner=%d session=%s err=%v", ownerID, sessionID, err)
		return nil
	}
	return &mirror
}

// resolveRunFromQueueMirror 按队列快照镜像解析"正在运行"的 run。
// 依序尝试镜像 running 列表：优先本节点内存 run（状态最准），
// 其次 durable 只读快照（跨节点场景）。解析不到时返回 nil，
// 由调用方走旧逻辑兜底——覆盖镜像里的自驱虚拟项（selfdrive_*，
// 不对应真实 run）与镜像过期尾巴。
func (m *Manager) resolveRunFromQueueMirror(ownerID int64, sessionID string) *ActiveRunSnapshot {
	mirror := loadQueueSnapshotMirror(context.Background(), ownerID, sessionID)
	if mirror == nil {
		return nil
	}
	for _, eventID := range mirror.Running {
		eventID = strings.TrimSpace(eventID)
		if eventID == "" {
			continue
		}
		m.runsMu.Lock()
		run := m.runs[eventID]
		if run != nil && run.OwnerID == ownerID && run.SessionID == sessionID {
			cp := *run
			m.runsMu.Unlock()
			return snapshotActiveRun(&cp)
		}
		m.runsMu.Unlock()
		if snap := m.lookupDurableRunByEvent(eventID, ownerID, sessionID); snap != nil {
			return snap
		}
	}
	return nil
}
