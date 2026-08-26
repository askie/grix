package agentapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/agentmsg"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// guards 集中提供 Agent 上行入口的两类硬约束：
//   1. 事件归属校验 (ensureEventOwnedBy)
//      —— 收到带 event_id 的上行（client_stream_chunk / send_msg / codex_event / pi_event 等）时,
//         必须确认该 event_id 是当前连接（agentID）持有的 pending event,
//         否则一律视为越权,直接拒绝。
//
//   2. 会话可写性校验 (ensureSessionWritableBy)
//      —— Agent 试图往某个 session_id 写消息/分片时,
//         必须确认该 (agentID, ownerID, sessionID) 组合在业务上是允许的,
//         复用 agentmsg.ResolveIdentity 的鉴权逻辑,并通过 Redis 短期缓存避免高频查询。

// agentSessionGuardCacheTTL 会话可写性正向结果的缓存时长。
// 30 秒足够覆盖一次正常对话流的所有 chunk; 失败结果不缓存以便实时反映授权变更。
const agentSessionGuardCacheTTL = 30 * time.Second

// ensureEventOwnedBy 在 pending 表中查找 event,并要求其归属于 agentID。
// 行为约定：
//   - eventID 为空 → 返回 nil（业务上允许,例如 binding card / 无事件上下文的主动推送）;
//   - 找不到 entry → 返回 4003（视为越权而非 4001,避免暴露内部状态）;
//   - entry.kind == revoke → 视为不可由 Agent 主动操作,返回 4003;
//   - entry.agentID 与传入不符 → 返回 4003。
func (m *Manager) ensureEventOwnedBy(eventID string, agentID int64) *SendError {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return nil
	}
	if m == nil {
		return &SendError{Code: 5001, Msg: "manager unavailable"}
	}
	m.acksMu.Lock()
	entry, ok := m.pending[eventID]
	m.acksMu.Unlock()
	if !ok {
		// Fallback: try to recover from Redis durable pending delegate.
		// This handles the case where ws restarted and in-memory pending map was cleared,
		// but the agent reconnected and continues sending chunks for a previously dispatched event.
		recovered := m.recoverPendingFromDurable(eventID, agentID)
		if recovered == nil {
			return &SendError{Code: 4003, Msg: "event_id not owned by current agent", NotFound: true}
		}
		entry = recovered
	}
	if entry.kind == pendingEventKindRevoke {
		return &SendError{Code: 4003, Msg: "event_id not writable"}
	}
	if entry.agentID != agentID {
		return &SendError{Code: 4003, Msg: "event_id not owned by current agent"}
	}
	return nil
}

// ensureEventOwnedByConnection extends the event ownership check to the
// concrete owner-scoped Agent connection. One Agent can have independent
// connections for its owner and shared users, so checking agent_id alone is
// insufficient: a packet from owner B must never advance or settle owner A's
// event.
func (m *Manager) ensureEventOwnedByConnection(eventID string, agentID, ownerID int64) *SendError {
	if guardErr := m.ensureEventOwnedBy(eventID, agentID); guardErr != nil {
		if !guardErr.NotFound {
			return guardErr
		}
		if run := m.LookupActiveRun(eventID); run != nil {
			if run.AgentID > 0 && run.AgentID != agentID {
				return &SendError{Code: 4003, Msg: "event_id not owned by current agent"}
			}
			if ownerID > 0 && run.OwnerID > 0 && run.OwnerID != ownerID {
				return &SendError{Code: 4003, Msg: "event_id not owned by current connection"}
			}
			return nil
		}
		return guardErr
	}

	eventID = strings.TrimSpace(eventID)
	if eventID == "" || ownerID <= 0 {
		return nil
	}
	m.acksMu.Lock()
	entry := m.pending[eventID]
	m.acksMu.Unlock()
	if entry == nil {
		entry = m.recoverPendingFromDurable(eventID, agentID)
	}
	if entry == nil {
		return &SendError{Code: 4003, Msg: "event_id not owned by current connection", NotFound: true}
	}
	if entry.event.OwnerID > 0 && entry.event.OwnerID != ownerID {
		return &SendError{Code: 4003, Msg: "event_id not owned by current connection"}
	}
	return nil
}

// authorizeInboundOutput applies one shared acceptance policy to every
// user-visible Agent output path:
//   - known events must belong to this owner-scoped connection and session;
//   - an expired/unknown event_id may fall back to the independently
//     authorized session path so late output is not discarded;
//   - a known foreign event is always rejected.
//
// The returned event id is empty only for the authorized session fallback.
type inboundOutputAuthorization struct {
	EventID        string
	AbsorbTerminal bool
}

func (m *Manager) authorizeInboundOutput(
	ctx context.Context,
	conn *agentConn,
	eventID string,
	sessionID string,
) (inboundOutputAuthorization, *SendError) {
	eventID = strings.TrimSpace(eventID)
	sessionID = strings.TrimSpace(sessionID)
	if conn == nil {
		return inboundOutputAuthorization{}, &SendError{Code: 5001, Msg: "agent connection unavailable"}
	}
	if eventID == "" {
		blocked, err := m.hasPendingStructuredInternalEventForSession(
			conn.agentID, conn.ownerID, sessionID,
		)
		if err != nil {
			return inboundOutputAuthorization{}, &SendError{
				Code: 5001,
				Msg:  "load internal event output fence failed",
			}
		}
		if blocked {
			return inboundOutputAuthorization{}, &SendError{Code: 4003, Msg: "event_id required for internal event output"}
		}
		// Some send_msg producers are proactive and intentionally have no
		// event_id. Their downstream send handler performs the full identity
		// check. Stream callers additionally require an explicit session guard.
		return inboundOutputAuthorization{}, nil
	}

	if guardErr := m.ensureEventOwnedByConnection(eventID, conn.agentID, conn.ownerID); guardErr != nil {
		if !guardErr.NotFound {
			return inboundOutputAuthorization{}, guardErr
		}

		// The immutable DB ledger is checked before the generic session
		// fallback. A committed terminal event is absorbing: late connector
		// output is acknowledged and dropped, never persisted as a new message
		// after Redis tracking expires. A pending dispatch seed reconstructs
		// the short-lived Redis/local snapshot so valid in-flight output keeps
		// its event identity across a backend restart.
		ledger, ledgerErr := store.LoadAgentEventTerminalLedger(eventID)
		if ledgerErr != nil {
			return inboundOutputAuthorization{}, &SendError{
				Code: 5001,
				Msg:  "load event output ledger failed",
			}
		}
		if ledger != nil {
			if ledger.AgentID != conn.agentID ||
				(conn.ownerID > 0 && ledger.OwnerID != conn.ownerID) {
				return inboundOutputAuthorization{}, &SendError{
					Code: 4003,
					Msg:  "event_id not owned by current connection",
				}
			}
			expectedSessionID := strings.TrimSpace(ledger.SessionID)
			if expectedSessionID != "" && expectedSessionID != sessionID {
				return inboundOutputAuthorization{}, &SendError{
					Code: 4003,
					Msg:  "session_id does not match event_id",
				}
			}
			if strings.TrimSpace(ledger.Status) != "" {
				return inboundOutputAuthorization{
					EventID:        eventID,
					AbsorbTerminal: true,
				}, nil
			}
			if m.pendingDispatchLedgerExpired(ledger) {
				deleted, deleteErr := store.DeleteAgentEventDispatchSeedIfPending(
					eventID,
					ledger.OwnerID,
					ledger.AgentID,
					ledger.DispatchGeneration,
				)
				if deleteErr != nil {
					return inboundOutputAuthorization{}, &SendError{
						Code: 5001,
						Msg:  "retire expired event output ledger failed",
					}
				}
				if deleted {
					if sessionErr := m.ensureSessionWritableBy(ctx, conn.agentID, conn.ownerID, sessionID); sessionErr != nil {
						return inboundOutputAuthorization{}, sessionErr
					}
					return inboundOutputAuthorization{}, nil
				}
				// A concurrent terminal commit may have won the CAS. Reload it
				// instead of trusting the stale pending snapshot.
				ledger, ledgerErr = store.LoadAgentEventTerminalLedger(eventID)
				if ledgerErr != nil {
					return inboundOutputAuthorization{}, &SendError{Code: 5001, Msg: "reload event output ledger failed"}
				}
				if ledger == nil {
					if sessionErr := m.ensureSessionWritableBy(ctx, conn.agentID, conn.ownerID, sessionID); sessionErr != nil {
						return inboundOutputAuthorization{}, sessionErr
					}
					return inboundOutputAuthorization{}, nil
				}
				if strings.TrimSpace(ledger.Status) != "" {
					return inboundOutputAuthorization{EventID: eventID, AbsorbTerminal: true}, nil
				}
				if m.pendingDispatchLedgerExpired(ledger) {
					if sessionErr := m.ensureSessionWritableBy(ctx, conn.agentID, conn.ownerID, sessionID); sessionErr != nil {
						return inboundOutputAuthorization{}, sessionErr
					}
					return inboundOutputAuthorization{}, nil
				}
			}
			record := durableRecordFromTerminalLedger(ledger)
			if record == nil {
				return inboundOutputAuthorization{}, &SendError{
					Code: 5001,
					Msg:  "recover event output snapshot failed",
				}
			}
			if store.RDB != nil {
				if _, _, createErr := createDurablePendingDelegate(ctx, *record); createErr != nil {
					return inboundOutputAuthorization{}, &SendError{
						Code: 5001,
						Msg:  "recover event output tracking failed",
					}
				}
				m.recoverPendingFromDurable(eventID, conn.agentID)
			}
			return inboundOutputAuthorization{EventID: eventID}, nil
		}
		if sessionErr := m.ensureSessionWritableBy(ctx, conn.agentID, conn.ownerID, sessionID); sessionErr != nil {
			return inboundOutputAuthorization{}, sessionErr
		}
		return inboundOutputAuthorization{}, nil
	}
	if guardErr := m.ensureSessionConsistentWithEvent(eventID, sessionID); guardErr != nil {
		return inboundOutputAuthorization{}, guardErr
	}
	return inboundOutputAuthorization{EventID: eventID}, nil
}

// structuredInternalOutputFenceWindow 是"存在未 ack 的 record-only 内部事件时,
// 拒绝无 event_id 主动输出"这条围栏的最长生效时间。
const structuredInternalOutputFenceWindow = 5 * time.Minute

func (m *Manager) hasPendingStructuredInternalEventForSession(
	agentID, ownerID int64,
	sessionID string,
) (bool, error) {
	if m == nil || agentID <= 0 || ownerID <= 0 {
		return false, nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return false, nil
	}
	now := time.Now()
	m.acksMu.Lock()
	for _, entry := range m.pending {
		if entry == nil || entry.kind != pendingEventKindDelegate || entry.agentID != agentID {
			continue
		}
		evt := entry.event
		if evt.OwnerID != ownerID || strings.TrimSpace(evt.SessionID) != sessionID {
			continue
		}
		if entry.trackingExpireAt > 0 && !now.Before(time.UnixMilli(entry.trackingExpireAt)) {
			continue
		}
		if m.pendingEventBeyondFenceWindow(entry, now) {
			continue
		}
		if evt.IsRecordOnly() && isNoReplyProtocolEvent(evt) {
			m.acksMu.Unlock()
			return true, nil
		}
	}
	m.acksMu.Unlock()

	ledgers, err := store.ListPendingRecordOnlyAgentEventDispatches(sessionID, ownerID, agentID)
	if err != nil {
		return false, err
	}
	for i := range ledgers {
		ledger := &ledgers[i]
		if m.pendingDispatchLedgerExpired(ledger) {
			if _, deleteErr := store.DeleteAgentEventDispatchSeedIfPending(
				ledger.EventID,
				ledger.OwnerID,
				ledger.AgentID,
				ledger.DispatchGeneration,
			); deleteErr != nil {
				return false, deleteErr
			}
			continue
		}
		if m.pendingDispatchLedgerBeyondFenceWindow(ledger) {
			// 超出围栏窗口后不再阻塞主动输出，ledger 仍留给既有过期回收逻辑处理。
			continue
		}
		record := durableRecordFromTerminalLedger(ledger)
		if record != nil && isNoReplyProtocolEvent(record.Event) {
			return true, nil
		}
	}
	return false, nil
}

func (m *Manager) pendingDispatchLedgerExpired(ledger *model.AgentEventTerminalLedger) bool {
	anchor := pendingDispatchLedgerAnchor(ledger)
	return !anchor.IsZero() && !time.Now().Before(anchor.Add(m.pendingTrackingRetention()))
}

// pendingDispatchLedgerBeyondFenceWindow 只用于判断"是否还该阻塞主动输出",
// 不参与 ledger 回收:围栏窗口远短于 pendingTrackingRetention。
func (m *Manager) pendingDispatchLedgerBeyondFenceWindow(
	ledger *model.AgentEventTerminalLedger,
) bool {
	anchor := pendingDispatchLedgerAnchor(ledger)
	if anchor.IsZero() {
		return false
	}
	return !time.Now().Before(anchor.Add(m.structuredInternalFenceWindow()))
}

func (m *Manager) pendingEventBeyondFenceWindow(entry *pendingEventAck, now time.Time) bool {
	if entry == nil {
		return false
	}
	anchor := pendingFenceAnchorFromMillis(entry.event.CreatedAt)
	if anchor.IsZero() && entry.trackingExpireAt > 0 {
		anchor = time.UnixMilli(entry.trackingExpireAt).Add(-m.pendingTrackingRetention())
	}
	if anchor.IsZero() {
		return false
	}
	return !now.Before(anchor.Add(m.structuredInternalFenceWindow()))
}

// structuredInternalFenceWindow 给"未 ack 的内部事件阻塞主动输出"设独立上限。
// pendingTrackingRetention 默认 48h,是 ledger 的保留期而不是阻塞期:直接复用会
// 让一条卡住的内部事件把整个会话的主动消息挡两天。取两者较小值,测试里的短 TTL
// 行为保持不变。
func (m *Manager) structuredInternalFenceWindow() time.Duration {
	retention := m.pendingTrackingRetention()
	if retention > 0 && retention < structuredInternalOutputFenceWindow {
		return retention
	}
	return structuredInternalOutputFenceWindow
}

func pendingDispatchLedgerAnchor(ledger *model.AgentEventTerminalLedger) time.Time {
	if ledger == nil {
		return time.Time{}
	}
	anchor := ledger.UpdatedAt
	if ledger.CreatedAt.After(anchor) {
		anchor = ledger.CreatedAt
	}
	if ledger.StartedAt != nil && ledger.StartedAt.After(anchor) {
		anchor = *ledger.StartedAt
	}
	if ledger.ReceivedAt > 0 {
		receivedAt := time.UnixMilli(ledger.ReceivedAt)
		if receivedAt.After(anchor) {
			anchor = receivedAt
		}
	}
	return anchor
}

func pendingFenceAnchorFromMillis(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// ensureSessionConsistentWithEvent 用于 chunk/send_msg 等"必带 event_id"的上行场景。
// 当带 event_id 时直接比对 pending event 上记录的 SessionID,
// 不再走 DB 查询;比对一致即认为该 (agent, owner, session) 组合可写。
//
// event_id 为空时返回 nil（由调用方决定是否再走 ensureSessionWritableBy）。
func (m *Manager) ensureSessionConsistentWithEvent(eventID, sessionID string) *SendError {
	eventID = strings.TrimSpace(eventID)
	sessionID = strings.TrimSpace(sessionID)
	if eventID == "" {
		return nil
	}
	if m == nil {
		return &SendError{Code: 5001, Msg: "manager unavailable"}
	}
	m.acksMu.Lock()
	entry, ok := m.pending[eventID]
	m.acksMu.Unlock()
	if !ok {
		// Same fallback as ensureEventOwnedBy
		recovered := m.recoverPendingFromDurable(eventID, 0)
		if recovered == nil {
			if run := m.LookupActiveRun(eventID); run != nil {
				expected := strings.TrimSpace(run.SessionID)
				if expected == "" || expected == sessionID {
					return nil
				}
				return &SendError{Code: 4003, Msg: "session_id does not match event_id"}
			}
			return &SendError{Code: 4003, Msg: "event_id not owned by current agent"}
		}
		entry = recovered
	}
	if entry.kind == pendingEventKindRevoke {
		return &SendError{Code: 4003, Msg: "event_id not writable"}
	}
	expected := strings.TrimSpace(entry.event.SessionID)
	if expected == "" {
		return nil
	}
	if expected != sessionID {
		return &SendError{Code: 4003, Msg: "session_id does not match event_id"}
	}
	return nil
}

// ensureSessionWritableBy 校验 (agentID, ownerID, sessionID) 是否被允许写入。
// 复用 agentmsg.ResolveIdentity 的核心鉴权逻辑,加 Redis 30s 缓存。
// 仅用于"不带 event_id 的主动推送类"路径；带 event_id 的路径优先走 ensureSessionConsistentWithEvent。
func (m *Manager) ensureSessionWritableBy(ctx context.Context, agentID, ownerID int64, sessionID string) *SendError {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return &SendError{Code: 4001, Msg: "session_id required"}
	}
	if agentID <= 0 || ownerID <= 0 {
		return &SendError{Code: 4003, Msg: "invalid agent_id or owner_id"}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	cacheKey := fmt.Sprintf("im:agent_sess_guard:%d:%d:%s", agentID, ownerID, sessionID)
	if store.RDB != nil {
		if exists, err := store.RDB.Exists(ctx, cacheKey).Result(); err == nil && exists > 0 {
			return nil
		}
	}

	_, err := agentmsg.ResolveIdentity(ctx, agentmsg.IdentityParams{
		Mode:      agentmsg.ModeAgentAPI,
		SessionID: sessionID,
		OwnerID:   ownerID,
		AgentID:   agentID,
	})
	if err != nil {
		if errors.Is(err, agentmsg.ErrPermissionDenied) {
			return &SendError{Code: 4003, Msg: "session not writable by agent"}
		}
		return &SendError{Code: 5001, Msg: "session writability check failed"}
	}
	if store.RDB != nil {
		store.RDB.Set(ctx, cacheKey, "1", agentSessionGuardCacheTTL)
	}
	return nil
}

// recoverPendingFromDurable attempts to restore a pending event from Redis durable storage.
// If found and (optionally) agentID matches, it reconstructs the in-memory pending entry
// and activeRun so that subsequent guards, stop requests, and output status queries work
// without another Redis round-trip.
// Returns nil if the event cannot be recovered.
func (m *Manager) recoverPendingFromDurable(eventID string, agentID int64) *pendingEventAck {
	record, ok := loadDurablePendingDelegate(context.Background(), eventID)
	if !ok {
		return nil
	}
	if agentID > 0 && record.Event.AgentID != agentID {
		return nil
	}
	if record.Stage != durablePendingDelegateStageAck &&
		record.Stage != durablePendingDelegateStageResult {
		return nil
	}

	stage := pendingEventStageAck
	deliveryState := protocol.AgentDeliveryStatusQueued
	if record.Stage == durablePendingDelegateStageResult {
		stage = pendingEventStageResult
		deliveryState = protocol.AgentDeliveryStatusReceived
	}

	// Reconstruct the in-memory pending entry
	entry := &pendingEventAck{
		kind:    pendingEventKindDelegate,
		agentID: record.Event.AgentID,
		status: protocol.AgentDeliveryStatusPayload{
			SessionID:    record.Event.SessionID,
			OwnerID:      record.Event.OwnerID,
			AgentID:      record.Event.AgentID,
			TriggerMsgID: record.Event.MsgID,
			EventID:      record.Event.EventID,
			Scope:        resolveDelegateEventScope(record.Event),
			Status:       deliveryState,
		},
		event:              record.Event,
		attempt:            record.Attempt,
		stage:              stage,
		waitResult:         !record.Event.IsRecordOnly(),
		silent:             record.Event.IsRecordOnly(),
		durableVersion:     record.Version,
		durableUpdatedAt:   record.UpdatedAt,
		dispatchGeneration: record.DispatchGeneration,
	}
	recoveredAt := record.UpdatedAt
	if recoveredAt <= 0 {
		recoveredAt = record.ReceivedAt
	}
	if recoveredAt <= 0 {
		recoveredAt = time.Now().UnixMilli()
	}
	entry.selfTouchAt = recoveredAt
	entry.trackingExpireAt = time.UnixMilli(recoveredAt).Add(m.pendingTrackingRetention()).UnixMilli()
	if record.ReceivedAt > 0 {
		entry.status.ReceivedAt = record.ReceivedAt
	}

	// Re-insert into in-memory pending map so next guard call is O(1)
	// with a result timeout timer to prevent memory leaks if the agent
	// crashes after recovery without ever sending event_result.
	m.acksMu.Lock()
	if existing, hasExisting := m.pending[eventID]; hasExisting {
		if existing.agentID != record.Event.AgentID ||
			existing.event.OwnerID != record.Event.OwnerID {
			m.acksMu.Unlock()
			return nil
		}
		existingStageRank := 1
		if existing.stage == pendingEventStageResult {
			existingStageRank = 2
		}
		recordStageRank := 1
		if stage == pendingEventStageResult {
			recordStageRank = 2
		}
		if existing.durableVersion > record.Version ||
			(existing.durableVersion == record.Version &&
				existingStageRank >= recordStageRank) {
			m.acksMu.Unlock()
			return existing
		}
		if existing.timer != nil {
			existing.timer.Stop()
		}
	}
	m.pending[eventID] = entry
	wait := m.eventAckWait
	if stage == pendingEventStageResult {
		wait = m.eventResultWait
	} else if entry.attempt >= agentAPIDeliveryMaxAttempts {
		wait = m.pendingTrackingRemaining(entry, time.Now())
	}
	if remaining := m.pendingTrackingRemaining(entry, time.Now()); wait <= 0 || wait > remaining {
		wait = remaining
	}
	m.resetPendingEventTimerLocked(entry, eventID, wait)
	m.acksMu.Unlock()

	if !record.Event.IsRecordOnly() {
		m.restoreActiveRunFromDurable(record)
	}

	logger.L.Infof(
		"recovered pending event from durable storage event=%s agent=%d stage=%s",
		eventID, record.Event.AgentID, record.Stage,
	)
	return entry
}

// restoreActiveRunFromDurable reconstructs local run metadata without emitting
// queued/streaming status. Redis remains authoritative and the reconstruction
// exists only so inbound ACK/result/stop paths retain their normal local shape.
func (m *Manager) restoreActiveRunFromDurable(record *durablePendingDelegateRecord) {
	if m == nil || record == nil {
		return
	}
	sessionID := strings.TrimSpace(record.Event.SessionID)
	ownerID := record.Event.OwnerID
	if sessionID != "" && ownerID > 0 {
		state := protocol.AgentOutputStateQueued
		canStop := true
		clientStream := false
		visibleOutput := false
		if record.Stage == durablePendingDelegateStageResult || record.ReceivedAt > 0 {
			state = protocol.AgentOutputStateStreaming
			clientStream = true
			visibleOutput = true
		}
		restoredVisibleTo := loadTriggerVisibleTo(record.Event.MsgID, sessionID)
		m.rememberOutboundVisibility(record.Event.AgentID, ownerID, sessionID, record.Event.SessionType, restoredVisibleTo)
		m.runsMu.Lock()
		if m.runs[record.Event.EventID] == nil {
			m.runs[record.Event.EventID] = &activeAgentRun{
				EventID:          record.Event.EventID,
				SessionID:        sessionID,
				ThreadID:         strings.TrimSpace(record.Event.ThreadID),
				Scope:            resolveDelegateEventScope(record.Event),
				SessionType:      record.Event.SessionType,
				SenderID:         record.Event.SenderID,
				OwnerID:          ownerID,
				AgentID:          record.Event.AgentID,
				TriggerMsgID:     record.Event.MsgID,
				TriggerQuoted:    record.Event.QuotedMessageID,
				State:            state,
				CanStop:          canStop,
				ClientStream:     clientStream,
				VisibleOutput:    visibleOutput,
				TriggerVisibleTo: restoredVisibleTo,
				StartedAt:        record.StartedAt,
				RunGeneration:    record.DispatchGeneration,
				CallTurn:         record.CallTurn,
				UpdatedAt:        time.Now().UnixMilli(),
			}
			sessionOwnerKey := activeRunSessionOwnerKey(sessionID, ownerID)
			currentID := strings.TrimSpace(m.runBySX[sessionOwnerKey])
			if currentID == "" || m.runs[currentID] == nil {
				m.runBySX[sessionOwnerKey] = record.Event.EventID
			}
		}
		m.runsMu.Unlock()
	}
}
