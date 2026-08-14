package handler

// Widget 访客排队等待队列。
// 当 agent 并发上限已满时，将访客加入队列并实时推送排队位置；
// 有空位时后端主动晋升队首访客，避免客户端轮询竞态。
//
// Redis 数据结构：
//   im:voice:queue:{agentID}          sorted set  score=enqueue_ms  member=visitorID(string)
//   im:voice:queue:data:{agentID}     hash        field=visitorID   value=JSON(queueEntry)
//   im:voice:queue:call:{callID}      string → agentID   TTL=5min+buffer（挂断取消用）
//   im:voice:queue:visitor:{visitorID} string → agentID  TTL=5min+buffer（断连清理+去重）

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/askie/grix/backend/internal/call"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/redis/go-redis/v9"
)

const voiceQueueMaxWait = 5 * time.Minute

type queueEntry struct {
	CallID          int64                `json:"call_id"`
	VisitorID       int64                `json:"visitor_id"`
	OwnerID         int64                `json:"owner_id"`
	WidgetSessionID string               `json:"widget_session_id"`
	VisitorName     string               `json:"visitor_name"`
	AgentID         int64                `json:"agent_id"`
	Spec            call.VoiceBridgeSpec `json:"spec"`
	EnqueuedAt      int64                `json:"enqueued_at"` // Unix ms
}

func vqKey(agentID int64) string {
	return fmt.Sprintf("im:voice:queue:%d", agentID)
}

func vqDataKey(agentID int64) string {
	return fmt.Sprintf("im:voice:queue:data:%d", agentID)
}

func vqCallKey(callID int64) string {
	return fmt.Sprintf("im:voice:queue:call:%d", callID)
}

func vqVisitorKey(visitorID int64) string {
	return fmt.Sprintf("im:voice:queue:visitor:%d", visitorID)
}

// enqueueVisitorCall 访客入队：加入排序集、写入数据、设置反向映射、通知访客排队位置。
func enqueueVisitorCall(
	hub HubInterface, conn ConnInterface, pkt *protocol.Packet,
	ownerID int64, widgetSessionID, visitorName string,
	agentID int64, spec call.VoiceBridgeSpec,
) {
	if store.RDB == nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("客服繁忙，请稍后再试"))
		return
	}
	visitorID := conn.GetUserID()
	ctx := context.Background()

	// 去重：同一访客已在等待队列中
	if n, err := store.RDB.Exists(ctx, vqVisitorKey(visitorID)).Result(); err == nil && n > 0 {
		logger.L.Infof("call queue: visitor=%d already_queued", visitorID)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("您已在等待队列中"))
		return
	}

	callID := snowflake.GenID()
	now := time.Now().UnixMilli()
	entry := queueEntry{
		CallID:          callID,
		VisitorID:       visitorID,
		OwnerID:         ownerID,
		WidgetSessionID: widgetSessionID,
		VisitorName:     visitorName,
		AgentID:         agentID,
		Spec:            spec,
		EnqueuedAt:      now,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("内部错误"))
		return
	}

	visitorIDStr := strconv.FormatInt(visitorID, 10)
	qKey := vqKey(agentID)

	if err := store.RDB.ZAdd(ctx, qKey, redis.Z{Score: float64(now), Member: visitorIDStr}).Err(); err != nil {
		logger.L.Warnf("call queue: zadd failed agent=%d visitor=%d err=%v", agentID, visitorID, err)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("客服繁忙，请稍后再试"))
		return
	}

	ttl := voiceQueueMaxWait + 30*time.Second
	store.RDB.HSet(ctx, vqDataKey(agentID), visitorIDStr, string(data))
	store.RDB.Set(ctx, vqCallKey(callID), strconv.FormatInt(agentID, 10), ttl)
	store.RDB.Set(ctx, vqVisitorKey(visitorID), strconv.FormatInt(agentID, 10), ttl)

	rank, _ := store.RDB.ZRank(ctx, qKey, visitorIDStr).Result()
	position := int(rank) + 1

	conn.SendPayload(protocol.CmdCallQueued, pkt.Seq, protocol.CallQueuedPayload{
		CallID:   strconv.FormatInt(callID, 10),
		Position: position,
	})
	logger.L.Infof("call queue: enqueued visitor=%d agent=%d call=%d position=%d", visitorID, agentID, callID, position)

	// 超时兜底：5 分钟后若仍未晋升则通知访客超时
	go queueExpireAfterWait(callID, visitorID, agentID, spec.DailyLimit, hub)
}

// queueExpireAfterWait 等待最大排队时间后清理并通知访客超时。
// dailyLimit 用于回退已在入队时消耗的每日配额。
func queueExpireAfterWait(callID, visitorID, agentID int64, dailyLimit int, hub HubInterface) {
	time.Sleep(voiceQueueMaxWait)
	ctx := context.Background()
	// 原子删除 callKey：返回 0 说明已被晋升或手动取消，直接退出，避免在晋升后发送 queue_expired 中断活跃通话。
	n, err := store.RDB.Del(ctx, vqCallKey(callID)).Result()
	if err != nil || n == 0 {
		return
	}
	visitorIDStr := strconv.FormatInt(visitorID, 10)
	store.RDB.ZRem(ctx, vqKey(agentID), visitorIDStr)
	store.RDB.HDel(ctx, vqDataKey(agentID), visitorIDStr)
	store.RDB.Del(ctx, vqVisitorKey(visitorID))
	releaseVoiceDailyQuota(agentID, dailyLimit)
	broadcastToUser(hub, ctx, visitorID, protocol.CmdCallQueueExpired, protocol.CallQueueExpiredPayload{})
	broadcastQueuePositions(ctx, agentID, hub)
	logger.L.Infof("call queue: expired visitor=%d agent=%d call=%d", visitorID, agentID, callID)
}

// promoteNextQueued 从队首晋升一个访客进入活跃通话（在 cleanupCallGuards 后调用）。
func promoteNextQueued(agentID int64, hub HubInterface) {
	if store.RDB == nil || hub == nil || callCtrl == nil {
		return
	}
	ctx := context.Background()
	qKey := vqKey(agentID)
	dKey := vqDataKey(agentID)

	// 原子弹出队首
	results, err := store.RDB.ZPopMin(ctx, qKey, 1).Result()
	if err != nil || len(results) == 0 {
		return
	}

	visitorIDStr, _ := results[0].Member.(string)
	visitorID, err := strconv.ParseInt(visitorIDStr, 10, 64)
	if err != nil {
		return
	}

	entryJSON, err := store.RDB.HGet(ctx, dKey, visitorIDStr).Result()
	if err != nil {
		logger.L.Warnf("call queue: promote missing_data visitor=%d agent=%d", visitorID, agentID)
		broadcastQueuePositions(ctx, agentID, hub)
		return
	}
	store.RDB.HDel(ctx, dKey, visitorIDStr)

	var entry queueEntry
	if err := json.Unmarshal([]byte(entryJSON), &entry); err != nil {
		logger.L.Warnf("call queue: promote unmarshal_err visitor=%d err=%v", visitorID, err)
		broadcastQueuePositions(ctx, agentID, hub)
		return
	}

	store.RDB.Del(ctx, vqCallKey(entry.CallID))
	store.RDB.Del(ctx, vqVisitorKey(visitorID))

	// 跳过已超时的条目
	if time.Now().UnixMilli()-entry.EnqueuedAt > voiceQueueMaxWait.Milliseconds() {
		logger.L.Infof("call queue: promote skip_expired visitor=%d agent=%d", visitorID, agentID)
		broadcastToUser(hub, ctx, visitorID, protocol.CmdCallQueueExpired, protocol.CallQueueExpiredPayload{})
		promoteNextQueued(agentID, hub)
		return
	}

	// 跳过离线访客（检查跨节点路由表）
	routeKey := fmt.Sprintf("im:ws:route:%d", visitorID)
	nodeCount, _ := store.RDB.HLen(ctx, routeKey).Result()
	if nodeCount == 0 {
		logger.L.Infof("call queue: promote skip_offline visitor=%d agent=%d", visitorID, agentID)
		promoteNextQueued(agentID, hub)
		return
	}

	// 预留并发槽（ZPOPMIN 原子弹出，此时仍有可能新通话抢占）
	newCallID := snowflake.GenID()
	if !reserveVoiceConcurrent(agentID, newCallID, entry.Spec.MaxConcurrent) {
		// 槽被新来电抢占，重新入队（保留原始入队时间维持顺序）
		logger.L.Infof("call queue: promote slot_taken visitor=%d agent=%d requeue", visitorID, agentID)
		store.RDB.ZAdd(ctx, qKey, redis.Z{Score: float64(entry.EnqueuedAt), Member: visitorIDStr})
		store.RDB.HSet(ctx, dKey, visitorIDStr, entryJSON)
		ttl := voiceQueueMaxWait + 30*time.Second
		store.RDB.Set(ctx, vqCallKey(entry.CallID), strconv.FormatInt(agentID, 10), ttl)
		store.RDB.Set(ctx, vqVisitorKey(visitorID), strconv.FormatInt(agentID, 10), ttl)
		return
	}

	busyGuard, err := reserveCallBusy(ctx, newCallID, visitorID)
	if err != nil {
		releaseVoiceConcurrent(agentID, newCallID)
		logger.L.Warnf("call queue: promote busy_guard_failed visitor=%d err=%v", visitorID, err)
		promoteNextQueued(agentID, hub)
		return
	}

	ownerCommitted := false
	defer func() {
		if !ownerCommitted {
			busyGuard.release(ctx)
			releaseVoiceConcurrent(agentID, newCallID)
		}
	}()

	_, tokenCaller, roomURL, err := callCtrl.InviteVisitorWithID(ctx, newCallID, visitorID, entry.OwnerID, entry.WidgetSessionID)
	if err != nil {
		logger.L.Warnf("call queue: promote invite_failed visitor=%d call=%d err=%v", visitorID, newCallID, err)
		return
	}
	if err := rememberCallOwner(ctx, newCallID, hub.GetNodeID()); err != nil {
		logger.L.Errorf("call queue: promote owner_guard_failed visitor=%d call=%d err=%v", visitorID, newCallID, err)
		_ = callCtrl.Hangup(ctx, newCallID, visitorID)
		return
	}
	ownerCommitted = true

	if _, _, err = callCtrl.AnswerWithAI(ctx, newCallID, entry.OwnerID, entry.Spec); err != nil {
		logger.L.Warnf("call queue: promote delegate_failed visitor=%d call=%d err=%v", visitorID, newCallID, err)
		_ = callCtrl.Hangup(ctx, newCallID, visitorID)
		// AnswerWithAI 失败时 DelegatedAgentID 可能未写入通话记录，
		// cleanupCallGuards 不会触发 releaseVoiceConcurrent/promoteNextQueued，需手动兜底。
		// SRem 幂等，与 cleanupCallGuards 路径双重释放无副作用；
		// 异步触发 promoteNextQueued 避免与 cleanupCallGuards 内部可能的同步调用竞争。
		releaseVoiceConcurrent(agentID, newCallID)
		go promoteNextQueued(agentID, hub)
		return
	}

	broadcastToUser(hub, ctx, visitorID, protocol.CmdCallInviteAck, protocol.CallInviteAckPayload{
		CallID:     strconv.FormatInt(newCallID, 10),
		RoomToken:  tokenCaller,
		RoomURL:    roomURL,
		ICEServers: callICEServers(),
	})
	notifyOwnerAiDelegated(hub, entry.OwnerID, newCallID, entry.WidgetSessionID, entry.VisitorName)
	broadcastQueuePositions(ctx, agentID, hub)
	logger.L.Infof("call queue: promoted visitor=%d agent=%d call=%d", visitorID, agentID, newCallID)
}

// broadcastQueuePositions 向队列中所有等待访客推送最新排队位置。
func broadcastQueuePositions(ctx context.Context, agentID int64, hub HubInterface) {
	if store.RDB == nil || hub == nil {
		return
	}
	members, err := store.RDB.ZRange(ctx, vqKey(agentID), 0, -1).Result()
	if err != nil {
		return
	}
	for i, memberStr := range members {
		visitorID, err := strconv.ParseInt(memberStr, 10, 64)
		if err != nil {
			continue
		}
		broadcastToUser(hub, ctx, visitorID, protocol.CmdCallQueueUpdate, protocol.CallQueueUpdatePayload{
			Position: i + 1,
		})
	}
}

// cancelQueueByCallID 根据 callID 取消排队条目（访客挂断用）。
func cancelQueueByCallID(ctx context.Context, callID int64, hub HubInterface) {
	if store.RDB == nil {
		return
	}
	agentIDStr, err := store.RDB.Get(ctx, vqCallKey(callID)).Result()
	if err != nil || agentIDStr == "" {
		return
	}
	agentID, err := strconv.ParseInt(agentIDStr, 10, 64)
	if err != nil {
		return
	}
	entries, err := store.RDB.HGetAll(ctx, vqDataKey(agentID)).Result()
	if err != nil {
		return
	}
	for visitorIDStr, entryJSON := range entries {
		var entry queueEntry
		if err := json.Unmarshal([]byte(entryJSON), &entry); err != nil {
			continue
		}
		if entry.CallID == callID {
			visitorID, _ := strconv.ParseInt(visitorIDStr, 10, 64)
			vqRemove(ctx, agentID, visitorID, callID)
			releaseVoiceDailyQuota(agentID, entry.Spec.DailyLimit)
			if hub != nil {
				broadcastQueuePositions(ctx, agentID, hub)
			}
			return
		}
	}
}

// RemoveVisitorFromQueue 访客断连清理（由 ws server 在全设备离线时调用）。
func RemoveVisitorFromQueue(visitorID int64) {
	if store.RDB == nil {
		return
	}
	ctx := context.Background()
	agentIDStr, err := store.RDB.Get(ctx, vqVisitorKey(visitorID)).Result()
	if err != nil || agentIDStr == "" {
		return
	}
	agentID, err := strconv.ParseInt(agentIDStr, 10, 64)
	if err != nil {
		return
	}
	visitorIDStr := strconv.FormatInt(visitorID, 10)
	entryJSON, err := store.RDB.HGet(ctx, vqDataKey(agentID), visitorIDStr).Result()
	if err != nil {
		store.RDB.Del(ctx, vqVisitorKey(visitorID))
		return
	}
	var entry queueEntry
	if err := json.Unmarshal([]byte(entryJSON), &entry); err != nil {
		store.RDB.Del(ctx, vqVisitorKey(visitorID))
		return
	}
	vqRemove(ctx, agentID, visitorID, entry.CallID)
	releaseVoiceDailyQuota(agentID, entry.Spec.DailyLimit)
	if resyncHub != nil {
		broadcastQueuePositions(ctx, agentID, resyncHub)
	}
	logger.L.Infof("call queue: disconnect_cleanup visitor=%d agent=%d call=%d", visitorID, agentID, entry.CallID)
}

// vqRemove 原子清理队列中的一个访客条目。
func vqRemove(ctx context.Context, agentID, visitorID, callID int64) {
	visitorIDStr := strconv.FormatInt(visitorID, 10)
	store.RDB.ZRem(ctx, vqKey(agentID), visitorIDStr)
	store.RDB.HDel(ctx, vqDataKey(agentID), visitorIDStr)
	store.RDB.Del(ctx, vqCallKey(callID))
	store.RDB.Del(ctx, vqVisitorKey(visitorID))
}
