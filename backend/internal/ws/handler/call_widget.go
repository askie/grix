package handler

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// resolveWidgetSessionLocale 取该 widget 会话访客初始化时归一化并落库的 locale，
// 供本次访客发起的语音通话选取同一种语言（system prompt 语言 + 开场白）。
// 查不到时返回空串，由 resolveAgentVoiceSpec 归一化兜底 en_US。
var resolveWidgetSessionLocale = func(widgetSessionID string) string {
	if store.DB == nil || widgetSessionID == "" {
		return ""
	}
	var loc string
	store.DB.Model(&model.WidgetSession{}).
		Select("locale").
		Where("session_id = ?", widgetSessionID).
		Limit(1).
		Scan(&loc)
	return loc
}

// reserveVisitorCallQuota 访客呼叫频率限制：每访客每分钟最多 maxVisitorCallsPerMin 次。
// 防陌生访客刷 owner 的 BYOK 通话花费。可在测试中替换。
const maxVisitorCallsPerMin = 3

var reserveVisitorCallQuota = func(visitorID int64) bool {
	if store.RDB == nil {
		return true
	}
	ctx := context.Background()
	key := fmt.Sprintf("im:voice:visitor:%d:%s", visitorID, time.Now().Format("200601021504"))
	n, err := store.RDB.Incr(ctx, key).Result()
	if err != nil {
		return true
	}
	if n == 1 {
		store.RDB.Expire(ctx, key, 2*time.Minute)
	}
	return n <= maxVisitorCallsPerMin
}

// voiceConcurrentKey agent 级活跃通话集合 key（集合成员为 callID）。
func voiceConcurrentKey(agentID int64) string {
	return fmt.Sprintf("im:voice:concurrent:%d", agentID)
}

// reserveVoiceConcurrent 在 agent 并发上限内预留一通：把 callID 加入活跃集合。
// limit<=0 表示不限。超限则回滚并返回 false。使用集合而非计数器，
// 释放时按 callID SREM 幂等，避免计数漂移/为负。可在测试中替换。
var reserveVoiceConcurrent = func(agentID, callID int64, limit int) bool {
	if limit <= 0 || store.RDB == nil {
		return true
	}
	ctx := context.Background()
	key := voiceConcurrentKey(agentID)
	if err := store.RDB.SAdd(ctx, key, callID).Err(); err != nil {
		logger.L.Warnf("call trace: concurrent redis_err agent=%d call=%d err=%v", agentID, callID, err)
		return true // redis 抖动放行，避免误杀
	}
	store.RDB.Expire(ctx, key, 12*time.Hour) // 安全兜底，防崩溃残留
	n, err := store.RDB.SCard(ctx, key).Result()
	if err != nil {
		return true
	}
	if n > int64(limit) {
		store.RDB.SRem(ctx, key, callID)
		return false
	}
	return true
}

// releaseVoiceConcurrent 通话结束时从活跃集合移除（幂等，非成员无副作用）。
func releaseVoiceConcurrent(agentID, callID int64) {
	if store.RDB == nil || agentID <= 0 || callID <= 0 {
		return
	}
	if err := store.RDB.SRem(context.Background(), voiceConcurrentKey(agentID), callID).Err(); err != nil {
		logger.L.Warnf("call trace: concurrent release_err agent=%d call=%d err=%v", agentID, callID, err)
	}
}

// HandleWidgetCallInvite 处理 widget 访客发起的语音通话（客服）。
// 严格限定：被叫固定为该 widget 会话的 owner、会话固定为 widget 会话；
// 跳过好友校验（访客非好友）；要求 owner 已配置语音托管且该 agent voice_allow_visitor=true；
// 命中即自动 AI 代接，并通知 owner 可随时接管。ownerID/widgetSessionID 由 widget WS 层可信传入。
func HandleWidgetCallInvite(hub HubInterface, conn ConnInterface, pkt *protocol.Packet, ownerID int64, widgetSessionID, visitorName string) {
	visitorID := conn.GetUserID()
	logger.L.Infof("call trace: widget invite begin owner=%d visitor=%d session=%s visitor_name=%s seq=%d", ownerID, visitorID, widgetSessionID, visitorName, pkt.Seq)

	if callCtrl == nil {
		logger.L.Warnf("call trace: widget invite rejected (callCtrl=nil) owner=%d visitor=%d", ownerID, visitorID)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("call service unavailable"))
		return
	}
	if !reserveVisitorCallQuota(visitorID) {
		logger.L.Warnf("call trace: widget invite rate_limited visitor=%d owner=%d", visitorID, ownerID)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("呼叫过于频繁，请稍后再试"))
		return
	}

	agentID, ok := resolveCalleeVoiceAgent(ownerID, widgetSessionID)
	if !ok {
		logger.L.Warnf("call trace: widget invite no_voice_agent owner=%d visitor=%d session=%s", ownerID, visitorID, widgetSessionID)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("客服暂不可用"))
		return
	}
	spec, err := resolveAgentVoiceSpec(agentID, resolveWidgetSessionLocale(widgetSessionID))
	if err != nil || !spec.AllowVisitor {
		logger.L.Warnf("call trace: widget invite spec_failed owner=%d visitor=%d agent=%d allow=%v err=%v", ownerID, visitorID, agentID, spec.AllowVisitor, err)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("客服暂不可用"))
		return
	}
	if !reserveVoiceDailyQuota(agentID, spec.DailyLimit) {
		logger.L.Warnf("call trace: widget invite daily_quota owner=%d visitor=%d agent=%d limit=%d", ownerID, visitorID, agentID, spec.DailyLimit)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("客服今日繁忙，请稍后再试"))
		return
	}

	ctx := context.Background()
	callID := snowflake.GenID()

	// 并发上限：放开 AI 并发接待多访客后，限制单 agent 瞬时并发，约束 BYOK 成本/Provider 连接数。
	// 超限时进入等待队列，由空闲后端主动晋升，避免客户端轮询竞态。
	concurrentReserved := false
	if !reserveVoiceConcurrent(agentID, callID, spec.MaxConcurrent) {
		logger.L.Infof("call trace: widget invite queued owner=%d visitor=%d agent=%d limit=%d", ownerID, visitorID, agentID, spec.MaxConcurrent)
		enqueueVisitorCall(hub, conn, pkt, ownerID, widgetSessionID, visitorName, agentID, spec)
		return
	}
	concurrentReserved = true

	// 访客客服只锁访客（一访客一通）；不再锁 owner——owner 不被 AI 代接占用，
	// 才能让 AI 同时接待多个访客。owner 是否亲自参与由"参与锁"（call:listen 时获取）控制。
	busyGuard, err := reserveCallBusy(ctx, callID, visitorID)
	if err != nil {
		logger.L.Warnf("call trace: widget invite busy_guard_failed owner=%d visitor=%d call=%d err=%v", ownerID, visitorID, callID, err)
		releaseVoiceConcurrent(agentID, callID)
		releaseVoiceDailyQuota(agentID, spec.DailyLimit)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("客服暂不可用"))
		return
	}
	ownerCommitted := false
	defer func() {
		if !ownerCommitted {
			busyGuard.release(ctx)
			if concurrentReserved {
				releaseVoiceConcurrent(agentID, callID)
			}
			releaseVoiceDailyQuota(agentID, spec.DailyLimit)
		}
	}()

	_, tokenCaller, roomURL, err := callCtrl.InviteVisitorWithID(ctx, callID, visitorID, ownerID, widgetSessionID)
	if err != nil {
		logger.L.Warnf("call trace: widget invite invite_failed owner=%d visitor=%d call=%d err=%v", ownerID, visitorID, callID, err)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("客服暂不可用"))
		return
	}
	if err := rememberCallOwner(ctx, callID, hub.GetNodeID()); err != nil {
		logger.L.Errorf("call trace: widget invite owner_guard_failed owner=%d visitor=%d call=%d node=%s err=%v", ownerID, visitorID, callID, hub.GetNodeID(), err)
		_ = callCtrl.Hangup(ctx, callID, visitorID)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("客服暂不可用"))
		return
	}
	ownerCommitted = true

	_, _, err = callCtrl.AnswerWithAI(ctx, callID, ownerID, spec)
	if err != nil {
		logger.L.Warnf("call trace: widget invite auto_delegate_failed owner=%d agent=%d call=%d err=%v", ownerID, agentID, callID, err)
		_ = callCtrl.Hangup(ctx, callID, visitorID)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("客服暂不可用"))
		// AnswerWithAI 失败时内部会回滚 DelegatedAgentID=nil，cleanupCallGuards 不会释放并发槽；
		// ownerCommitted=true 导致 defer 也不释放，需手动兜底。SRem 幂等，与 cleanupCallGuards 双重调用无副作用。
		releaseVoiceConcurrent(agentID, callID)
		releaseVoiceDailyQuota(agentID, spec.DailyLimit)
		go promoteNextQueued(agentID, hub)
		return
	}

	// 访客拿到 caller token 入房
	conn.SendPayload(protocol.CmdCallInviteAck, pkt.Seq, protocol.CallInviteAckPayload{
		CallID:     strconv.FormatInt(callID, 10),
		RoomToken:  tokenCaller,
		RoomURL:    roomURL,
		ICEServers: callICEServers(),
	})
	logger.L.Infof("call trace: widget invite ack_sent owner=%d visitor=%d call=%d room_url=%s", ownerID, visitorID, callID, roomURL)
	// owner 收到代接通知（不预发 room token，等 call:listen 时按需签发）
	notifyOwnerAiDelegated(hub, ownerID, callID, widgetSessionID, visitorName)
	// 若 owner 离线，WS 通知无法送达，入队离线推送。
	// ForcePush=true 确保 push worker 不跳过"在线"的 iOS 设备（app 可能在后台但 WS 还活着）。
	pushContent := "[语音通话]"
	if visitorName != "" {
		pushContent = fmt.Sprintf("[语音通话] 来自%s", visitorName)
	}
	enqueueOfflinePushTask(ownerID, protocol.CmdPushMsg, protocol.PushMsgPayload{
		MsgID:         callID,
		SessionID:     widgetSessionID,
		SenderID:      visitorID,
		SenderType:    1,
		Content:       pushContent,
		MsgType:       1,
		ForcePush:     true,
		TimeSensitive: true,
	})
	logger.L.Infof("call trace: widget invite done owner=%d agent=%d call=%d visitor=%d", ownerID, agentID, callID, visitorID)
}
