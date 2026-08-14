// Package call 实现语音通话信令状态机（Phase 1：真人对真人通话）。
//
// 职责：
//   - 管理通话生命周期（RINGING → ACTIVE → ENDED）
//   - 持久化 call_records（通过 Persister 接口）
//   - 调度 LiveKit Room 创建/销毁（通过 RoomProvider 接口）
//   - 超时未接判断
//
// 不负责：媒体路由、AI 托管（Phase 2）、录音（Phase 3）。
package call

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
)

func callLogInfof(format string, args ...any) {
	if logger.L != nil {
		logger.L.Infof(format, args...)
	}
}

// 哨兵错误，供调用方用 errors.Is 判断。
var (
	ErrCalleeBusy             = errors.New("callee busy")
	ErrCallerBusy             = errors.New("caller busy")
	ErrSelfCall               = errors.New("self call not allowed")
	ErrControllerShuttingDown = errors.New("controller is shutting down")
)

const defaultRingTimeout = 30 * time.Second

// defaultDirectAICallTimeout 是直拨 AI 通话的兜底超时。
// 防止 bridge 卡死导致 callEntry 永不被清理（caller busy 锁死用户）。
const defaultDirectAICallTimeout = 10 * time.Minute

// RoomProvider 抽象 LiveKit Room 操作，便于测试 mock。
type RoomProvider interface {
	// CreateRoom 创建 LiveKit 房间，返回 caller 和 callee 的 JWT token。
	CreateRoom(ctx context.Context, callID, callerID, calleeID int64) (tokenCaller, tokenCallee, roomURL string, err error)
	// CloseRoom 关闭并销毁房间。
	CloseRoom(ctx context.Context, callID int64) error
}

// Persister 抽象通话记录持久化，便于测试 mock。
type Persister interface {
	Create(ctx context.Context, r *model.CallRecord) error
	UpdateAnswered(ctx context.Context, callID int64, answeredAt time.Time) error
	// UpdateAnsweredWithAI 用于 AI 代接：写入 ai_delegated 状态和 delegation_mode。
	UpdateAnsweredWithAI(ctx context.Context, callID, agentID int64, answeredAt time.Time) error
	UpdateEnd(ctx context.Context, callID int64, state int16, endReason string, endedAt time.Time, durationSec *int) error
	UpdateHandover(ctx context.Context, callID int64, event model.CallHandoverEvent, state int16, delegationMode string) error
	// UpdateRecordingURLs 更新录音 URL（Phase 3: Egress 完成后调用）。
	UpdateRecordingURLs(ctx context.Context, callID int64, callerURL, calleeURL, aiURL, mixedURL string) error
}

// NotifyFn 向指定用户推送 WS 包的回调，由 ws.Server 注入。
type NotifyFn func(userID int64, cmd string, payload any)

// Controller 管理所有进行中的通话。
type Controller struct {
	// bgMu/bgActive/bgClosing 跟踪正在跑的定时器回调，让 Shutdown 能等它们退出。见 lifecycle.go。
	bgMu      sync.Mutex
	bgActive  int
	bgClosing bool

	mu          sync.Mutex
	calls       map[int64]*callEntry // callID → entry
	rooms       RoomProvider
	persist     Persister
	notify      NotifyFn
	bridge      BridgeManager   // Phase 2：Voice Bridge 控制（可为 nil）
	recorder    *EgressRecorder // Phase 3：Egress 录制（可为 nil）
	ringTimeout time.Duration   // 可注入，默认 30s

	// EndHook is called after a call ends with the final record state.
	// Used for call_summary generation. May be nil.
	EndHook func(ctx context.Context, rec model.CallRecord, recordingURL string)

	// CleanupHook is called synchronously when a call leaves the in-memory
	// active map. It is used by WS handlers to release cross-node Redis guards.
	CleanupHook func(ctx context.Context, rec model.CallRecord)
}

type callEntry struct {
	record      model.CallRecord
	tokenCaller string
	tokenCallee string
	roomURL     string
	timer       *time.Timer     // 响铃超时；Stop 必须走 Controller.stopCallTimer 配对减计数
	egressID    string          // Phase 3: Egress recording ID (non-empty if recording active)
	aiSpec      VoiceBridgeSpec // Phase 2: AI 托管配置（HandBack 复用）
}

// New 创建 Controller。
func New(rooms RoomProvider, persist Persister, notify NotifyFn) *Controller {
	return &Controller{
		calls:       make(map[int64]*callEntry),
		rooms:       rooms,
		persist:     persist,
		notify:      notify,
		ringTimeout: defaultRingTimeout,
	}
}

// SetRecorder sets the Egress recorder (Phase 3).
func (c *Controller) SetRecorder(r *EgressRecorder) { c.recorder = r }

// SetEndHook sets the post-call hook for call_summary generation (Phase 3).
func (c *Controller) SetEndHook(fn func(ctx context.Context, rec model.CallRecord, recordingURL string)) {
	c.EndHook = fn
}

// SetCleanupHook sets a synchronous cleanup hook for call lifecycle guards.
func (c *Controller) SetCleanupHook(fn func(ctx context.Context, rec model.CallRecord)) {
	c.CleanupHook = fn
}

// NewWithTimeout 创建 Controller，使用自定义超时（测试用）。
func NewWithTimeout(rooms RoomProvider, persist Persister, notify NotifyFn, timeout time.Duration) *Controller {
	return &Controller{
		calls:       make(map[int64]*callEntry),
		rooms:       rooms,
		persist:     persist,
		notify:      notify,
		ringTimeout: timeout,
	}
}

// Invite 处理 A 发起呼叫。返回 callID 和 A 的 room token。
func (c *Controller) Invite(ctx context.Context, callerID, calleeID int64, sessionID string) (callID int64, tokenCaller, roomURL string, err error) {
	return c.InviteWithID(ctx, snowflake.GenID(), callerID, calleeID, sessionID)
}

// InviteWithID 处理 A 发起呼叫，使用调用方提供的 callID。
// 供跨 WS 节点 guard 在创建前占用 Redis busy key。
func (c *Controller) InviteWithID(ctx context.Context, callID, callerID, calleeID int64, sessionID string) (int64, string, string, error) {
	return c.inviteWithID(ctx, callID, callerID, calleeID, sessionID, false)
}

// InviteVisitorWithID 供访客客服路径使用：被叫(owner)由 AI 自动代接、不亲自接听，
// 故跳过 callee 忙线检查，允许同一 owner 的 AI 并发接待多访客。
// 仍校验 caller(访客) 忙线：一个访客同时只能有一通。
func (c *Controller) InviteVisitorWithID(ctx context.Context, callID, callerID, calleeID int64, sessionID string) (int64, string, string, error) {
	return c.inviteWithID(ctx, callID, callerID, calleeID, sessionID, true)
}

func (c *Controller) inviteWithID(ctx context.Context, callID, callerID, calleeID int64, sessionID string, skipCalleeBusy bool) (int64, string, string, error) {
	if callerID == calleeID {
		return 0, "", "", ErrSelfCall
	}
	callLogInfof("call trace: controller invite begin call=%d caller=%d callee=%d session=%s", callID, callerID, calleeID, sessionID)

	// 正在关停就直接拒绝新邀请：Shutdown 只会等/收尾"关停开始前已经存在"的通话，
	// 关停之后再插进 c.calls 的通话既赶不上 Shutdown 的收尾扫描，也可能因为
	// bgClosing 已经是 true 而拿不到响铃定时器（trackedAfterFunc 返回 nil），
	// 状态就永远悬在 RINGING、没人管。跟这次改动里其它地方"关停后拒绝新工作、
	// 只等在途工作"的口径一致（agentapi 的 beginConn、审核 worker 的
	// spawnContentModerationTask 都是这个模式）。
	c.bgMu.Lock()
	closing := c.bgClosing
	c.bgMu.Unlock()
	if closing {
		callLogInfof("call trace: controller invite rejected shutting_down call=%d", callID)
		return 0, "", "", ErrControllerShuttingDown
	}

	// 持锁检查 busy 并立即占位，消除 isBusy 与写入之间的 TOCTOU 窗口。
	c.mu.Lock()
	if c.isBusyLocked(callerID) {
		c.mu.Unlock()
		callLogInfof("call trace: controller invite rejected caller_busy call=%d caller=%d", callID, callerID)
		return 0, "", "", ErrCallerBusy
	}
	if !skipCalleeBusy && c.isBusyLocked(calleeID) {
		c.mu.Unlock()
		callLogInfof("call trace: controller invite rejected callee_busy call=%d callee=%d", callID, calleeID)
		return 0, "", "", ErrCalleeBusy
	}
	// 写入占位 entry（State=RINGING），阻止并发 Invite 通过 busy 检查
	now := time.Now()
	placeholder := &callEntry{
		record: model.CallRecord{
			ID:             callID,
			SessionID:      sessionID,
			CallerID:       callerID,
			CalleeID:       calleeID,
			CallMode:       model.CallModeVoice,
			State:          model.CallStateRinging,
			DelegationMode: model.CallDelegationHuman,
			StartedAt:      &now,
		},
	}
	c.calls[callID] = placeholder
	c.mu.Unlock()

	// 占位后再做网络/DB 操作（失败时回滚占位）
	tokenCaller, tokenCallee, roomURL, err := c.rooms.CreateRoom(ctx, callID, callerID, calleeID)
	if err != nil {
		c.mu.Lock()
		delete(c.calls, callID)
		c.mu.Unlock()
		callLogInfof("call trace: controller invite create_room_fail call=%d err=%v", callID, err)
		return 0, "", "", fmt.Errorf("create room: %w", err)
	}
	callLogInfof("call trace: controller invite room_ready call=%d room_url=%s", callID, roomURL)

	if err := c.persist.Create(ctx, &placeholder.record); err != nil {
		_ = c.rooms.CloseRoom(ctx, callID)
		c.mu.Lock()
		delete(c.calls, callID)
		c.mu.Unlock()
		callLogInfof("call trace: controller invite persist_fail call=%d err=%v", callID, err)
		return 0, "", "", fmt.Errorf("persist call: %w", err)
	}

	timer := c.trackedAfterFunc(c.ringTimeout, func() {
		c.onRingTimeout(callID)
	})

	// 更新占位 entry 为完整 entry
	c.mu.Lock()
	placeholder.tokenCaller = tokenCaller
	placeholder.tokenCallee = tokenCallee
	placeholder.roomURL = roomURL
	placeholder.timer = timer
	c.mu.Unlock()
	callLogInfof("call trace: controller invite ringing call=%d timeout=%s", callID, c.ringTimeout)

	return callID, tokenCaller, roomURL, nil
}

// Answer 处理 B 接听。返回 B 的 room token。
func (c *Controller) Answer(ctx context.Context, callID, calleeID int64) (tokenCallee, roomURL string, err error) {
	callLogInfof("call trace: controller answer begin call=%d callee=%d", callID, calleeID)
	c.mu.Lock()
	entry, ok := c.calls[callID]
	if !ok {
		c.mu.Unlock()
		return "", "", fmt.Errorf("call not found: %d", callID)
	}
	if entry.record.CalleeID != calleeID {
		c.mu.Unlock()
		return "", "", fmt.Errorf("callee mismatch")
	}
	if entry.record.State != model.CallStateRinging {
		c.mu.Unlock()
		return "", "", fmt.Errorf("call not ringing, state=%d", entry.record.State)
	}
	c.stopCallTimer(entry.timer)
	now := time.Now()
	entry.record.State = model.CallStateActive
	entry.record.AnsweredAt = &now
	token := entry.tokenCallee
	url := entry.roomURL
	callerID := entry.record.CallerID
	c.mu.Unlock()

	if err := c.persist.UpdateAnswered(ctx, callID, now); err != nil {
		return "", "", fmt.Errorf("persist answered: %w", err)
	}
	callLogInfof("call trace: controller answer active call=%d callee=%d", callID, calleeID)

	// 通知 caller：对方已真人接听
	callIDStr := fmt.Sprintf("%d", callID)
	c.notify(callerID, "call:peer_answered", map[string]any{
		"call_id": callIDStr,
		"mode":    "human",
	})

	return token, url, nil
}
func (c *Controller) Reject(ctx context.Context, callID, calleeID int64, _ string) error {
	callLogInfof("call trace: controller reject begin call=%d callee=%d", callID, calleeID)
	c.mu.Lock()
	entry, ok := c.calls[callID]
	if !ok {
		c.mu.Unlock()
		return fmt.Errorf("call not found: %d", callID)
	}
	if entry.record.CalleeID != calleeID {
		c.mu.Unlock()
		return fmt.Errorf("callee mismatch")
	}
	c.mu.Unlock()
	return c.endCall(ctx, callID, model.CallStateRejected, "rejected")
}

// Hangup 处理任一方挂断。
func (c *Controller) Hangup(ctx context.Context, callID, userID int64) error {
	callLogInfof("call trace: controller hangup begin call=%d user=%d", callID, userID)
	c.mu.Lock()
	entry, ok := c.calls[callID]
	if !ok {
		c.mu.Unlock()
		return fmt.Errorf("call not found: %d", callID)
	}
	if entry.record.CallerID != userID && entry.record.CalleeID != userID {
		c.mu.Unlock()
		return fmt.Errorf("user %d not in call %d", userID, callID)
	}
	c.mu.Unlock()
	return c.endCall(ctx, callID, model.CallStateEnded, "hangup")
}

// endCall 统一结束通话逻辑。
func (c *Controller) endCall(ctx context.Context, callID int64, state int16, reason string) error {
	callLogInfof("call trace: controller end begin call=%d state=%d reason=%s", callID, state, reason)
	c.mu.Lock()
	entry, ok := c.calls[callID]
	if !ok {
		c.mu.Unlock()
		callLogInfof("call trace: controller end not_found call=%d", callID)
		return nil
	}
	c.stopCallTimer(entry.timer)
	now := time.Now()
	entry.record.State = state
	entry.record.EndedAt = &now
	entry.record.EndReason = reason
	var durationSec *int
	if entry.record.AnsweredAt != nil {
		secs := int(now.Sub(*entry.record.AnsweredAt).Seconds())
		durationSec = &secs
		entry.record.DurationSeconds = durationSec
	}
	callerID := entry.record.CallerID
	calleeID := entry.record.CalleeID
	egressID := entry.egressID
	finalRec := entry.record
	delete(c.calls, callID)
	c.mu.Unlock()

	if c.CleanupHook != nil {
		c.CleanupHook(ctx, finalRec)
	}

	// 关闭 LiveKit Room
	if err := c.rooms.CloseRoom(ctx, callID); err != nil {
		callLogInfof("call trace: close_room error call=%d err=%v", callID, err)
	}

	// Phase 3: Stop Egress recording if active, then persist URLs
	var recordingURL string
	if c.recorder != nil && egressID != "" {
		if url, err := c.recorder.StopRecording(ctx, egressID); err == nil && url != "" {
			recordingURL = url
			if perr := c.persist.UpdateRecordingURLs(ctx, callID, "", "", "", url); perr != nil {
				callLogInfof("call trace: update_recording_urls error call=%d err=%v", callID, perr)
			}
		}
	}

	// 持久化通话结束状态
	if err := c.persist.UpdateEnd(ctx, callID, state, reason, now, durationSec); err != nil {
		callLogInfof("call trace: persist_update_end error call=%d err=%v", callID, err)
	}

	// Phase 2: 通话结束时停止 Voice Bridge
	if c.bridge != nil {
		if err := c.bridge.StopBridge(ctx, callID); err != nil {
			callLogInfof("call trace: stop_bridge error call=%d err=%v", callID, err)
		}
	}

	// Phase 3: call_summary hook
	if c.EndHook != nil && durationSec != nil {
		go c.EndHook(context.Background(), finalRec, recordingURL)
	}

	callIDStr := fmt.Sprintf("%d", callID)
	ts := now.UnixMilli()
	c.notify(callerID, "call:state", map[string]any{"call_id": callIDStr, "state": state, "reason": reason, "ts": ts})
	c.notify(calleeID, "call:state", map[string]any{"call_id": callIDStr, "state": state, "reason": reason, "ts": ts})
	callLogInfof("call trace: controller end done call=%d caller=%d callee=%d final_state=%d reason=%s duration_sec=%v", callID, callerID, calleeID, state, reason, durationSec)
	return nil
}

// onRingTimeout 响铃超时回调。
// 仅当通话仍处于 RINGING 才结束，避免与 Answer/AnswerWithAI 的 timer.Stop 竞态：
// 若回调已被调度而 Stop 未拦住，此处状态已变，直接忽略，防止误结束已接通的通话。
func (c *Controller) onRingTimeout(callID int64) {
	callLogInfof("call trace: controller timeout fired call=%d", callID)
	c.mu.Lock()
	entry, ok := c.calls[callID]
	if !ok || entry.record.State != model.CallStateRinging {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	_ = c.endCall(context.Background(), callID, model.CallStateMissed, "timeout")
}

// isBusyLocked 检查用户是否已在通话中。调用方必须持有 c.mu。
func (c *Controller) isBusyLocked(userID int64) bool {
	for _, e := range c.calls {
		s := e.record.State
		if s == model.CallStateRinging || s == model.CallStateActive ||
			s == model.CallStateAIDelegated || s == model.CallStateHumanActive {
			if e.record.CallerID == userID || e.record.CalleeID == userID {
				return true
			}
		}
	}
	return false
}

// Shutdown 关闭所有进行中的通话，停止 timer，关闭 LiveKit Room。
// 应在进程退出前调用。
func (c *Controller) Shutdown(ctx context.Context) {
	// 先封口：不再让新触发的定时器回调开工（它们要写 DB，收尾后就写不得了）。
	c.bgMu.Lock()
	c.bgClosing = true
	c.bgMu.Unlock()

	c.mu.Lock()
	ids := make([]int64, 0, len(c.calls))
	for id := range c.calls {
		ids = append(ids, id)
	}
	c.mu.Unlock()

	for _, id := range ids {
		_ = c.endCall(ctx, id, model.CallStateError, "server_shutdown")
	}

	// endCall 只能 Stop 掉还没触发的定时器；已经触发、正在跑的回调要等它跑完，
	// 否则它会活过关停，继续读写已经关掉的库。
	c.waitCallBackground()

	// 补一轮扫描：inviteWithID 把占位写进 c.calls 到它自己看见 bgClosing 拒绝新
	// 邀请之间有一段没有锁保护的窗口（网络/DB 调用期间）。理论上极窄的竞态下，
	// 一通邀请可能在上面第一轮快照之后才插进 c.calls，从而被第一轮漏掉。这里
	// 用第二轮兜底：只要它已经进了 c.calls，就一定会被收尾，不会永远悬在 RINGING。
	c.mu.Lock()
	leftover := make([]int64, 0, len(c.calls))
	for id := range c.calls {
		leftover = append(leftover, id)
	}
	c.mu.Unlock()
	for _, id := range leftover {
		_ = c.endCall(ctx, id, model.CallStateError, "server_shutdown")
	}
	c.waitCallBackground()
}

// CleanupUserCalls 结束指定用户所有进行中的通话。
// 在用户所有 WS 设备断开时调用，防止通话因客户端消失而永远卡死。
// AI 驱动的通话（AI_DELEGATED / HUMAN_ACTIVE）不在此处结束：
// 媒体流走 LiveKit，不依赖 WS 信令连接，由通话自身的超时保护管理生命周期。
func (c *Controller) CleanupUserCalls(userID int64) {
	c.mu.Lock()
	var ids []int64
	for id, e := range c.calls {
		// AI 驱动的通话跳过：WS 断连是正常的（LiveKit 独立承载媒体），
		// 通话超时保护（defaultDirectAICallTimeout）会兜底清理。
		if e.record.State == model.CallStateAIDelegated || e.record.State == model.CallStateHumanActive {
			// AI_DELEGATED 且被清理者是 caller（发起方/访客）全设备离线 → 结束通话：
			// 发起方都走了，AI 没必要继续对着空房间、持续烧 BYOK token、悬挂忙线锁。
			// callee（AI agent 的 owner，旁听断网正常）与 HUMAN_ACTIVE（接管态）
			// 不在此结束，保持原跳过逻辑（owner 临时断网不该中断 AI 接待）。
			if e.record.State == model.CallStateAIDelegated && e.record.CallerID == userID {
				ids = append(ids, id)
			} else {
				callLogInfof("call trace: cleanup skip_ai call=%d state=%d user=%d", id, e.record.State, userID)
			}
			continue
		}
		if e.record.CallerID == userID || e.record.CalleeID == userID {
			ids = append(ids, id)
		}
	}
	c.mu.Unlock()
	if len(ids) == 0 {
		return
	}
	callLogInfof("call trace: cleanup_user_calls user=%d count=%d", userID, len(ids))
	for _, id := range ids {
		_ = c.endCall(context.Background(), id, model.CallStateEnded, "user_offline")
	}
}

// GetSessionIDByCallID returns the session_id for an active call (memory lookup).
func (c *Controller) GetSessionIDByCallID(callID int64) (string, bool) {
	c.mu.Lock()
	entry, ok := c.calls[callID]
	c.mu.Unlock()
	if !ok {
		return "", false
	}
	return entry.record.SessionID, true
}

// GetTranscriptRouteByCallID returns routing identity for transcript messages.
// ownerID/agentID are required by AgentAPISend permission checks.
func (c *Controller) GetTranscriptRouteByCallID(callID int64) (sessionID string, ownerID, agentID, callerID, calleeID int64, ok bool) {
	c.mu.Lock()
	entry, ok := c.calls[callID]
	c.mu.Unlock()
	if !ok {
		return "", 0, 0, 0, 0, false
	}

	rec := entry.record
	sessionID = rec.SessionID
	if sessionID == "" {
		return "", 0, 0, 0, 0, false
	}
	callerID = rec.CallerID
	calleeID = rec.CalleeID

	if rec.DelegatedAgentID != nil {
		agentID = *rec.DelegatedAgentID
	}

	// DirectAICall: agent == CalleeID → owner = CallerID
	// AnswerWithAI / VoiceDelegate: agent != CalleeID → owner = CalleeID
	if agentID > 0 && agentID == rec.CalleeID {
		ownerID = rec.CallerID
	} else {
		ownerID = rec.CalleeID
	}
	if ownerID <= 0 {
		ownerID = rec.CallerID
	}

	if ownerID <= 0 || agentID <= 0 {
		return sessionID, ownerID, agentID, callerID, calleeID, false
	}
	return sessionID, ownerID, agentID, callerID, calleeID, true
}

// GetActiveCallBySession 反查指定会话当前活跃的 AI 代接通话（接点B 下行注入用）。
//
// 防串线规范（见架构文档 29 §4.2.1）：
//   - 仅匹配 State=AI_DELEGATED 的活跃内存态通话（绝不查 call_records 历史表）；
//   - 同一 session 出现多通活跃 AI 通话时视为歧义，记录告警并返回 ok=false，宁可不注入也不串；
//   - 查不到返回 ok=false（普通文字会话因此天然不会被误注入）。
//
// direct 表示该通话为用户直拨自己的语音 agent（DelegatedAgentID == CalleeID），
// 注入侧据此放宽轮次与超时限制（见 call_voice_inject.go）。
func (c *Controller) GetActiveCallBySession(sessionID string) (callID int64, provider string, direct bool, ok bool) {
	if sessionID == "" {
		return 0, "", false, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	found := false
	for id, e := range c.calls {
		if e.record.SessionID == sessionID && e.record.State == model.CallStateAIDelegated {
			if found {
				callLogInfof("call trace: GetActiveCallBySession ambiguous session=%s, skip inject", sessionID)
				return 0, "", false, false
			}
			callID = id
			provider = e.aiSpec.Provider
			direct = isDirectAICallRecord(e.record)
			found = true
		}
	}
	return callID, provider, direct, found
}

// isDirectAICallRecord 判定通话记录是否为用户直拨自己语音 agent（与 callsegment.DBLookup 同款判定）。
func isDirectAICallRecord(rec model.CallRecord) bool {
	return rec.DelegatedAgentID != nil && *rec.DelegatedAgentID > 0 && *rec.DelegatedAgentID == rec.CalleeID
}

// EndCallOnBridgeExit 当 voicebridge 异常退出时结束通话。
// 若通话已正常结束则静默忽略。
// 人工接管中（HUMAN_ACTIVE）AI 桥只是辅助，桥故障不得挂断正在进行的真人通话：
// 仅记录日志，通话最终由 hangup/超时正常收尾。
func (c *Controller) EndCallOnBridgeExit(ctx context.Context, callID int64) {
	c.mu.Lock()
	entry, ok := c.calls[callID]
	var state int16
	if ok {
		state = entry.record.State
	}
	c.mu.Unlock()
	if !ok {
		return
	}
	if state == model.CallStateHumanActive {
		callLogInfof("call trace: bridge_exit during human takeover, keep call=%d alive", callID)
		return
	}
	callLogInfof("call trace: bridge_exit ending call=%d", callID)
	_ = c.endCall(ctx, callID, model.CallStateEnded, "bridge_exit")
}

// ActiveDelegatedCall 描述一通正在 AI 代接中的通话（用于 owner 上线时重推通知）。
type ActiveDelegatedCall struct {
	CallID    int64
	CallerID  int64
	SessionID string
}

// GetActiveDelegatedCalls 查询 calleeID(owner) 名下所有 AI_DELEGATED 状态的通话。
// 用于 owner WS 上线后重推 call_ai_delegated 通知。
func (c *Controller) GetActiveDelegatedCalls(calleeID int64) []ActiveDelegatedCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	var result []ActiveDelegatedCall
	for id, e := range c.calls {
		if e.record.CalleeID == calleeID && e.record.State == model.CallStateAIDelegated {
			result = append(result, ActiveDelegatedCall{
				CallID:    id,
				CallerID:  e.record.CallerID,
				SessionID: e.record.SessionID,
			})
		}
	}
	return result
}

// ListenToken 返回 owner 旁听某通通话所需的 callee room token。
// 仅当通话存在、owner 为其 callee、且处于 AI_DELEGATED/HUMAN_ACTIVE 状态时返回。
func (c *Controller) ListenToken(callID, ownerID int64) (token, roomURL string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.calls[callID]
	if !ok {
		return "", "", fmt.Errorf("call not found: %d", callID)
	}
	if entry.record.CalleeID != ownerID {
		return "", "", fmt.Errorf("not call owner")
	}
	s := entry.record.State
	if s != model.CallStateAIDelegated && s != model.CallStateHumanActive {
		return "", "", fmt.Errorf("call not listenable, state=%d", s)
	}
	return entry.tokenCallee, entry.roomURL, nil
}
