package agentapi

import (
	"context"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

const (
	pendingEventStageAck = iota + 1
	pendingEventStageResult
)

const (
	pendingEventKindDelegate = "delegate"
	pendingEventKindRevoke   = "revoke"
)

type pendingEventAck struct {
	kind            string
	agentID         int64
	status          protocol.AgentDeliveryStatusPayload
	event           DelegateEventPayload
	attempt         int
	stage           int
	waitResult      bool
	silent          bool
	ackDeleteRecord int64
	timer           *trackedTimer
	// selfTouchAt 只被明确引用该 event_id 的活动刷新（composing、
	// stream/send、Codex/Pi event）；ping/pong、其它会话或共享 owner
	// 的流量不影响它。该字段仅供非终态观测和诊断。
	selfTouchAt      int64
	trackingExpireAt int64
	// durableVersion is the explicit monotonic Redis CAS generation observed
	// by this tracker. UpdatedAt is only an observation timestamp: clocks are
	// neither cross-node monotonic nor guaranteed to advance within 1ms.
	durableVersion      int64
	durableUpdatedAt    int64
	dispatchGeneration  int64
	dispatchSeedCreated bool
}

type pendingEventRegistration int

const (
	pendingEventRegistrationFailed pendingEventRegistration = iota
	pendingEventRegistrationCreated
	pendingEventRegistrationExisting
)

func (m *Manager) registerPendingEventAck(
	evt DelegateEventPayload,
	attempt int,
) pendingEventRegistration {
	return m.registerPendingEventAckWithMetadata(
		evt,
		attempt,
		time.Now().UTC(),
		senderInVoiceCall(evt.SenderID),
	)
}

func (m *Manager) registerPendingEventAckWithMetadata(
	evt DelegateEventPayload,
	attempt int,
	startedAt time.Time,
	callTurn bool,
) pendingEventRegistration {
	if strings.TrimSpace(evt.EventID) == "" {
		return pendingEventRegistrationFailed
	}
	if attempt <= 0 {
		attempt = 1
	}
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	now := startedAt
	durableUpdatedAt := now.UnixMilli()
	durableVersion := int64(0)
	durableStartedAt := now.UnixMilli()
	dispatchGeneration, err := store.NextAgentRunGeneration(evt.SessionID, evt.OwnerID)
	if err != nil {
		logger.L.Warnf(
			"allocate agent run generation failed event=%s err=%v",
			evt.EventID,
			err,
		)
		return pendingEventRegistrationFailed
	}
	seed, err := dispatchLedgerEntry(
		evt,
		attempt,
		durableStartedAt,
		callTurn,
		dispatchGeneration,
	)
	if err != nil {
		logger.L.Warnf("build dispatch ledger seed failed event=%s err=%v", evt.EventID, err)
		return pendingEventRegistrationFailed
	}
	seedDisposition, seeded, seedCreated, err := store.SeedAgentEventDispatchLedger(seed)
	if err != nil {
		logger.L.Warnf("seed dispatch ledger failed event=%s err=%v", evt.EventID, err)
		return pendingEventRegistrationFailed
	}
	if seeded != nil {
		dispatchGeneration = seeded.DispatchGeneration
	}
	if seedDisposition == store.AgentTerminalLedgerForeign ||
		(seedDisposition == store.AgentTerminalLedgerSame &&
			seeded != nil && strings.TrimSpace(seeded.Status) != "") {
		return pendingEventRegistrationExisting
	}
	if store.RDB != nil {
		if !seedCreated {
			if durable, ok := loadDurablePendingDelegate(context.Background(), evt.EventID); ok {
				m.recoverPendingFromDurable(evt.EventID, evt.AgentID)
				_ = durable
				return pendingEventRegistrationExisting
			}
			if seeded != nil {
				if restored := durableRecordFromTerminalLedger(seeded); restored != nil {
					evt = restored.Event
					durableStartedAt = restored.StartedAt
					callTurn = restored.CallTurn
					dispatchGeneration = restored.DispatchGeneration
				}
			}
		}
		stored, created, err := createDurablePendingDelegate(
			context.Background(),
			durablePendingDelegateRecord{
				Event:              evt,
				Attempt:            attempt,
				Stage:              durablePendingDelegateStageAck,
				StartedAt:          durableStartedAt,
				CallTurn:           callTurn,
				DispatchGeneration: dispatchGeneration,
				UpdatedAt:          durableUpdatedAt,
			},
		)
		if err != nil || stored == nil {
			if err != nil {
				logger.L.Warnf(
					"create durable pending delegate failed event=%s err=%v",
					evt.EventID,
					err,
				)
			}
			if seedCreated {
				_, _ = store.DeleteAgentEventDispatchSeedIfPending(
					evt.EventID, evt.OwnerID, evt.AgentID, dispatchGeneration,
				)
			}
			return pendingEventRegistrationFailed
		}
		if !created {
			if seedCreated && stored != nil &&
				stored.Stage == durablePendingDelegateStageSettled &&
				stored.Terminal != nil {
				_, importErr := m.importLegacySettledTerminalResult(
					EventResultPayload{
						EventID: stored.Event.EventID,
						Status:  stored.Terminal.Status,
						Code:    stored.Terminal.Code,
						Msg:     stored.Terminal.Msg,
					},
					stored,
				)
				if importErr != nil {
					logger.L.Warnf(
						"import pre-ledger settled dispatch failed event=%s err=%v",
						evt.EventID,
						importErr,
					)
					return pendingEventRegistrationFailed
				}
			}
			m.recoverPendingFromDurable(evt.EventID, evt.AgentID)
			return pendingEventRegistrationExisting
		}
		durableVersion = stored.Version
		durableUpdatedAt = stored.UpdatedAt
	}
	status := protocol.AgentDeliveryStatusPayload{
		SessionID:    evt.SessionID,
		OwnerID:      evt.OwnerID,
		AgentID:      evt.AgentID,
		TriggerMsgID: evt.MsgID,
		EventID:      evt.EventID,
		Scope:        resolveDelegateEventScope(evt),
		Status:       protocol.AgentDeliveryStatusQueued,
		UpdatedAt:    now.UnixMilli(),
	}

	entry := &pendingEventAck{
		kind:                pendingEventKindDelegate,
		agentID:             evt.AgentID,
		status:              status,
		event:               evt,
		attempt:             attempt,
		stage:               pendingEventStageAck,
		waitResult:          !evt.IsRecordOnly(),
		silent:              evt.IsRecordOnly(),
		selfTouchAt:         status.UpdatedAt,
		trackingExpireAt:    now.Add(m.pendingTrackingRetention()).UnixMilli(),
		durableVersion:      durableVersion,
		durableUpdatedAt:    durableUpdatedAt,
		dispatchGeneration:  dispatchGeneration,
		dispatchSeedCreated: seedCreated,
	}
	initialWait := m.eventAckWait
	if retention := m.pendingTrackingRetention(); initialWait <= 0 || initialWait > retention {
		initialWait = retention
	}
	entry.timer = m.newPendingEventTimer(evt.EventID, initialWait)

	m.acksMu.Lock()
	if existing, ok := m.pending[evt.EventID]; ok {
		if existing.timer != nil {
			existing.timer.Stop()
		}
	}
	m.pending[evt.EventID] = entry
	m.acksMu.Unlock()

	if !entry.silent {
		m.emitDeliveryStatus(status)
	}
	return pendingEventRegistrationCreated
}

func (m *Manager) registerQueuedAgentEventAck(evt model.AgentQueuedEvent) {
	if strings.TrimSpace(evt.EventKey) == "" || evt.ID <= 0 {
		return
	}

	entry := &pendingEventAck{
		kind:            pendingEventKindRevoke,
		agentID:         evt.AgentID,
		stage:           pendingEventStageAck,
		waitResult:      false,
		ackDeleteRecord: evt.ID,
	}
	entry.timer = m.newPendingEventTimer(evt.EventKey, m.eventAckWait)

	m.acksMu.Lock()
	if existing, ok := m.pending[evt.EventKey]; ok {
		if existing.timer != nil {
			existing.timer.Stop()
		}
	}
	m.pending[evt.EventKey] = entry
	m.acksMu.Unlock()
}

func (m *Manager) resolvePendingEventAck(eventID string, receivedAt int64) {
	logger.L.Infof("agent api resolvePendingEventAck called event=%s", eventID)
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return
	}

	m.acksMu.Lock()
	entry, ok := m.pending[eventID]
	if !ok {
		logger.L.Warnf("agent api resolvePendingEventAck not found in pending event=%s pending_count=%d", eventID, len(m.pending))
		m.acksMu.Unlock()
		return
	}
	if entry.stage != pendingEventStageAck {
		m.acksMu.Unlock()
		return
	}
	if !entry.waitResult {
		delete(m.pending, eventID)
		m.acksMu.Unlock()
		if entry.timer != nil {
			entry.timer.Stop()
		}
		if entry.kind == pendingEventKindDelegate {
			deleteDurablePendingDelegate(context.Background(), eventID, entry.agentID)
		}
		if entry.kind == pendingEventKindRevoke && entry.ackDeleteRecord > 0 && store.DB != nil {
			if err := store.DB.WithContext(context.Background()).Delete(&model.AgentQueuedEvent{}, entry.ackDeleteRecord).Error; err != nil {
				logger.L.Warnf("delete acked queued agent event failed id=%d key=%s err=%v", entry.ackDeleteRecord, eventID, err)
			}
		}
		return
	}
	ackObservedAt := time.Now()
	if receivedAt <= 0 || receivedAt > ackObservedAt.UnixMilli() {
		receivedAt = ackObservedAt.UnixMilli()
	}
	if store.RDB != nil {
		evt := entry.event
		m.acksMu.Unlock()
		advanced, durableRecord, err := advanceDurablePendingDelegateAck(
			context.Background(),
			eventID,
			evt.AgentID,
			evt.OwnerID,
			receivedAt,
		)
		if err != nil {
			logger.L.Warnf("advance durable event ack failed event=%s err=%v", eventID, err)
			return
		}
		if !advanced {
			// Another node may already have advanced the durable stage or
			// terminal verdict. Mirror it locally so this stale tracker does not
			// wake every eventAckWait for the rest of the retention window.
			m.syncPendingEventFromDurable(eventID, entry, durableRecord)
			return
		}
		m.acksMu.Lock()
		if current := m.pending[eventID]; current != entry || entry.stage != pendingEventStageAck {
			m.acksMu.Unlock()
			return
		}
		if durableRecord != nil {
			entry.durableVersion = durableRecord.Version
			entry.durableUpdatedAt = durableRecord.UpdatedAt
			entry.attempt = durableRecord.Attempt
		}
	}
	status := entry.status
	status.Status = protocol.AgentDeliveryStatusReceived
	status.Code = ""
	status.Msg = ""
	status.ReceivedAt = receivedAt
	// Ordering uses the server observation clock. Connector clocks may be
	// arbitrarily slow/fast and must not move received behind queued (or ahead
	// of a later terminal result).
	status.UpdatedAt = ackObservedAt.UnixMilli()
	entry.status = status
	entry.stage = pendingEventStageResult
	entry.selfTouchAt = ackObservedAt.UnixMilli()
	entry.trackingExpireAt = ackObservedAt.Add(m.pendingTrackingRetention()).UnixMilli()
	m.resetPendingEventTimerLocked(entry, eventID, m.eventResultWait)
	m.acksMu.Unlock()

	if store.RDB == nil {
		persistDurablePendingDelegate(context.Background(), durablePendingDelegateRecord{
			Event:      entry.event,
			Attempt:    entry.attempt,
			Stage:      durablePendingDelegateStageResult,
			ReceivedAt: receivedAt,
		})
	}
	if !entry.silent {
		m.emitDeliveryStatus(status)
		m.MarkRunReceived(eventID)
	}
}

func (m *Manager) timeoutPendingEvent(eventID string) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return
	}

	m.acksMu.Lock()
	entry, ok := m.pending[eventID]
	if !ok {
		logger.L.Warnf("agent api resolvePendingEventAck not found in pending event=%s pending_count=%d", eventID, len(m.pending))
		m.acksMu.Unlock()
		return
	}
	now := time.Now()
	if entry.kind == pendingEventKindDelegate && entry.trackingExpireAt <= 0 {
		entry.trackingExpireAt = now.Add(m.pendingTrackingRetention()).UnixMilli()
	}
	if entry.kind == pendingEventKindDelegate && entry.trackingExpireAt <= now.UnixMilli() {
		// Check Redis outside the local lock before retiring. Another node may
		// have advanced ACK/result/attempt and refreshed the durable version
		// after this timer was armed.
		expected := pendingDurableExpectationFromEntry(entry)
		m.acksMu.Unlock()
		record, durable := loadDurablePendingDelegate(context.Background(), eventID)
		if durable && !pendingMatchesDurableVersion(expected, record) {
			m.syncPendingEventFromDurable(eventID, entry, record)
			return
		}
		m.acksMu.Lock()
		if current := m.pending[eventID]; current != entry ||
			!pendingEntryMatchesDurableExpectation(current, expected) {
			m.acksMu.Unlock()
			return
		}
		delete(m.pending, eventID)
		entry.timer = nil
		m.acksMu.Unlock()
		m.retireExpiredEventTracking(eventID, expected)
		logger.L.Warnf(
			"agent api retired expired tracking metadata without terminal state event=%s agent=%d owner=%d",
			eventID, expected.agentID, expected.ownerID,
		)
		return
	}
	if wait := m.pendingDisconnectWait(entry); entry.kind != pendingEventKindRevoke && m.lookupConnForDelegate(entry.event) == nil && wait > 0 {
		m.resetPendingEventTimerLocked(entry, eventID, wait)
		m.acksMu.Unlock()
		return
	}
	if entry.stage == pendingEventStageResult {
		// Silence is not proof that connector-side execution failed. Preserve
		// pending/durable/run state so late chunks, send_msg, or event_result
		// remain admissible. The timer is observation-only until the bounded
		// tracking retention expires.
		m.resetPendingEventTimerLocked(entry, eventID, m.pendingTrackingRemaining(entry, now))
		lastActivity := entry.selfTouchAt
		m.acksMu.Unlock()
		logger.L.Warnf(
			"agent api event result overdue; preserving non-terminal state event=%s agent=%d owner=%d last_activity=%d",
			eventID, entry.agentID, entry.event.OwnerID, lastActivity,
		)
		return
	}
	if entry.kind == pendingEventKindRevoke {
		delete(m.pending, eventID)
		m.acksMu.Unlock()
		if entry.ackDeleteRecord > 0 {
			deleteQueuedAgentEventRecord(context.Background(), entry.ackDeleteRecord, eventID)
		}
		return
	}
	if store.RDB != nil {
		evt := entry.event
		nextAttempt := entry.attempt + 1
		m.acksMu.Unlock()
		outcome := m.redeliverDelegateEventOutcome(evt, nextAttempt)
		if outcome.Record != nil {
			m.syncPendingEventFromDurable(eventID, entry, outcome.Record)
		} else {
			m.acksMu.Lock()
			if current := m.pending[eventID]; current == entry {
				m.resetPendingEventTimerLocked(entry, eventID, m.eventAckWait)
			}
			m.acksMu.Unlock()
		}
		if outcome.Routed {
			logger.L.Warnf(
				"agent api event ack timeout, durable retry routed event=%s agent=%d owner=%d",
				eventID, evt.AgentID, evt.OwnerID,
			)
		}
		return
	}
	nextAttempt := entry.attempt + 1
	if nextAttempt <= agentAPIDeliveryMaxAttempts {
		// Claim the retry and re-arm retention before dropping the lock. The
		// retry path only resends the wire packet; it must never replace this
		// entry or recreate a queued run after a concurrent ACK/result.
		entry.attempt = nextAttempt
		m.resetPendingEventTimerLocked(entry, eventID, m.eventAckWait)
		evt := entry.event
		agentID := entry.agentID
		m.acksMu.Unlock()

		if m.redeliverDelegateEvent(evt, nextAttempt) {
			logger.L.Warnf(
				"agent api event ack timeout, retry dispatched event=%s agent=%d attempt=%d/%d",
				eventID,
				agentID,
				nextAttempt,
				agentAPIDeliveryMaxAttempts,
			)
			return
		}
		logger.L.Warnf(
			"agent api event ack timeout, retry deferred event=%s agent=%d attempt=%d/%d session=%s owner=%d",
			eventID,
			agentID,
			nextAttempt,
			agentAPIDeliveryMaxAttempts,
			evt.SessionID,
			evt.OwnerID,
		)
		return
	}
	// Exhausted delivery attempts mean only that an ack was not observed.
	// Keep the authorization and durable context so a delayed connector can
	// still acknowledge or publish output. No execution terminal is inferred.
	if current := m.pending[eventID]; current == entry {
		m.resetPendingEventTimerLocked(current, eventID, m.pendingTrackingRemaining(current, time.Now()))
	}
	agentID := entry.agentID
	ownerID := entry.event.OwnerID
	attempt := entry.attempt
	m.acksMu.Unlock()
	logger.L.Warnf(
		"agent api event ack overdue after retries; preserving non-terminal state event=%s agent=%d owner=%d attempt=%d/%d",
		eventID, agentID, ownerID, attempt, agentAPIDeliveryMaxAttempts,
	)
}

type pendingDurableExpectation struct {
	agentID            int64
	ownerID            int64
	version            int64
	stage              string
	attempt            int
	dispatchGeneration int64
}

func pendingDurableExpectationFromEntry(
	entry *pendingEventAck,
) pendingDurableExpectation {
	expected := pendingDurableExpectation{}
	if entry == nil {
		return expected
	}
	expected.agentID = entry.agentID
	expected.ownerID = entry.event.OwnerID
	expected.version = entry.durableVersion
	expected.stage = durablePendingStageForLocal(entry)
	expected.attempt = entry.attempt
	expected.dispatchGeneration = entry.dispatchGeneration
	return expected
}

func pendingEntryMatchesDurableExpectation(
	entry *pendingEventAck,
	expected pendingDurableExpectation,
) bool {
	if entry == nil {
		return false
	}
	return entry.agentID == expected.agentID &&
		entry.event.OwnerID == expected.ownerID &&
		entry.durableVersion == expected.version &&
		durablePendingStageForLocal(entry) == expected.stage &&
		entry.attempt == expected.attempt &&
		entry.dispatchGeneration == expected.dispatchGeneration
}

func pendingMatchesDurableVersion(
	expected pendingDurableExpectation,
	record *durablePendingDelegateRecord,
) bool {
	if record == nil {
		return false
	}
	return expected.agentID == record.Event.AgentID &&
		expected.ownerID == record.Event.OwnerID &&
		expected.version == record.Version &&
		expected.stage == record.Stage &&
		expected.attempt == record.Attempt &&
		expected.dispatchGeneration == record.DispatchGeneration
}

// syncPendingEventFromDurable mirrors the latest Redis stage into one existing
// local tracker. It never emits status: the Redis winner owns those effects.
// The returned bool reports whether the tracker remains locally active.
func (m *Manager) syncPendingEventFromDurable(
	eventID string,
	expected *pendingEventAck,
	record *durablePendingDelegateRecord,
) bool {
	if m == nil || expected == nil || record == nil {
		return false
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" || record.Event.EventID != eventID {
		return false
	}

	m.acksMu.Lock()
	current := m.pending[eventID]
	if current != expected {
		m.acksMu.Unlock()
		return false
	}
	if record.Event.AgentID != current.agentID ||
		(record.Event.OwnerID > 0 && record.Event.OwnerID != current.event.OwnerID) {
		m.acksMu.Unlock()
		return false
	}
	if record.Version < current.durableVersion {
		m.acksMu.Unlock()
		return true
	}
	switch record.Stage {
	case durablePendingDelegateStageAck:
		current.stage = pendingEventStageAck
		current.status.Status = protocol.AgentDeliveryStatusQueued
	case durablePendingDelegateStageResult:
		current.stage = pendingEventStageResult
		current.status.Status = protocol.AgentDeliveryStatusReceived
		current.status.ReceivedAt = record.ReceivedAt
	case durablePendingDelegateStageIntent, durablePendingDelegateStageSettled:
		delete(m.pending, eventID)
		timer := current.timer
		m.acksMu.Unlock()
		if timer != nil {
			timer.Stop()
		}
		_ = m.removeRun(eventID)
		return false
	default:
		m.acksMu.Unlock()
		return false
	}

	current.attempt = record.Attempt
	current.durableVersion = record.Version
	current.durableUpdatedAt = record.UpdatedAt
	current.dispatchGeneration = record.DispatchGeneration
	recoveredAt := record.UpdatedAt
	if recoveredAt <= 0 {
		recoveredAt = time.Now().UnixMilli()
	}
	current.selfTouchAt = recoveredAt
	current.trackingExpireAt = time.UnixMilli(recoveredAt).
		Add(m.pendingTrackingRetention()).
		UnixMilli()

	now := time.Now()
	wait := m.eventAckWait
	if current.stage == pendingEventStageResult {
		wait = m.eventResultWait
	} else if current.attempt >= agentAPIDeliveryMaxAttempts {
		wait = m.pendingTrackingRemaining(current, now)
	}
	if remaining := m.pendingTrackingRemaining(current, now); wait <= 0 || wait > remaining {
		wait = remaining
	}
	m.resetPendingEventTimerLocked(current, eventID, wait)
	m.acksMu.Unlock()
	return true
}

func (m *Manager) TouchPendingEventResult(eventID string) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return
	}

	m.acksMu.Lock()
	defer m.acksMu.Unlock()

	entry, ok := m.pending[eventID]
	if !ok || entry.stage != pendingEventStageResult {
		return
	}
	entry.selfTouchAt = time.Now().UnixMilli()
	m.resetPendingEventTimerLocked(entry, eventID, m.eventResultWait)
}

// cleanupEventStopResidue 在收到终结性 event_stop_result（stopped/already_finished）
// 时清掉该事件残留的 pending 计时器与 Redis durable 记录，并返回清理前读到的
// durable 记录（无则 nil）。agentID 为发起清理的连接归属 agent：只清归属一致的
// 记录，防止异常/恶意连接借 stop_result 清掉他人事件的追踪状态。
//
// 正常停止流程里客户端会先发 event_result(canceled)，resolvePendingEventResult
// 已完成同样的清理，这里是幂等空操作；对永远等不到 event_result 的幽灵事件
// （如客户端重启时静默丢弃的排队事件），这里是唯一收口点。
func (m *Manager) cleanupEventStopResidue(eventID string, agentID int64) *durablePendingDelegateRecord {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return nil
	}

	m.acksMu.Lock()
	entry, ok := m.pending[eventID]
	if ok && agentID > 0 && entry.agentID != agentID {
		entry, ok = nil, false
	} else if ok {
		delete(m.pending, eventID)
	}
	m.acksMu.Unlock()
	if ok && entry != nil && entry.timer != nil {
		entry.timer.Stop()
	}

	record, found := loadDurablePendingDelegate(context.Background(), eventID)
	if found && agentID > 0 && record.Event.AgentID != agentID {
		return nil
	}
	if found || (ok && entry != nil) {
		resolvedAgentID := agentID
		if found {
			resolvedAgentID = record.Event.AgentID
		}
		deleteDurablePendingDelegate(context.Background(), eventID, resolvedAgentID)
	}
	if !found {
		return nil
	}
	return record
}

func (m *Manager) resolvePendingEventResult(payload EventResultPayload) {
	eventID := strings.TrimSpace(payload.EventID)
	if eventID == "" {
		return
	}
	// A terminal result closes any unfinished stream accounting even when the
	// connector could not deliver the final chunk (for example after an
	// explicit NACK). Normal finish chunks already release this key; duplicate
	// deletion is harmless.
	defer m.streamChunkTrackers.release(eventID)

	m.acksMu.Lock()
	entry, ok := m.pending[eventID]
	if !ok {
		logger.L.Warnf("agent api resolvePendingEventAck not found in pending event=%s pending_count=%d", eventID, len(m.pending))
		m.acksMu.Unlock()
		if m.resolvePendingEventResultFromDurable(payload) {
			return
		}
		m.resolvePendingEventResultFromActiveRun(payload)
		return
	}
	if entry.kind == pendingEventKindRevoke {
		delete(m.pending, eventID)
		m.acksMu.Unlock()
		if entry.timer != nil {
			entry.timer.Stop()
		}
		return
	}
	if entry.stage != pendingEventStageResult && entry.stage != pendingEventStageAck {
		m.acksMu.Unlock()
		return
	}
	delete(m.pending, eventID)
	m.acksMu.Unlock()

	if entry.timer != nil {
		entry.timer.Stop()
	}
	deleteDurablePendingDelegate(context.Background(), eventID, entry.agentID)

	status := entry.status
	switch strings.TrimSpace(payload.Status) {
	case protocol.AgentEventResultResponded:
		status.Status = protocol.AgentDeliveryStatusResponded
		status.Code = ""
		status.Msg = ""
	case protocol.AgentEventResultCanceled:
		status.Status = protocol.AgentDeliveryStatusCanceled
		status.Code = firstNonEmpty(strings.TrimSpace(payload.Code), protocol.AgentDeliveryCodeCanceled)
		status.Msg = firstNonEmpty(strings.TrimSpace(payload.Msg), "agent api event canceled")
	default:
		status.Status = protocol.AgentDeliveryStatusFailed
		status.Code = firstNonEmpty(strings.TrimSpace(payload.Code), protocol.AgentDeliveryCodeProcessingFailed)
		status.Msg = firstNonEmpty(strings.TrimSpace(payload.Msg), "agent api event processing failed")
	}
	status.UpdatedAt = time.Now().UnixMilli()
	m.emitDeliveryStatus(status)
	switch status.Status {
	case protocol.AgentDeliveryStatusResponded:
		m.MarkRunCompleted(eventID)
	case protocol.AgentDeliveryStatusCanceled:
		m.MarkRunStopped(eventID, firstNonEmpty(status.Code, status.Msg))
	case protocol.AgentDeliveryStatusFailed:
		// payload.Msg, not status.Msg: the latter has already been filled with
		// a placeholder and would push "agent api event processing failed" as
		// the reason. The connector's own message is what explains the failure.
		m.MarkRunFailedNotify(
			eventID,
			firstNonEmpty(status.Code, status.Msg),
			status.Code,
			payload.Msg,
		)
	}
}

func deliveryStatusForEventResult(
	event DelegateEventPayload,
	receivedAt int64,
	payload EventResultPayload,
) protocol.AgentDeliveryStatusPayload {
	status := protocol.AgentDeliveryStatusPayload{
		SessionID:    event.SessionID,
		OwnerID:      event.OwnerID,
		AgentID:      event.AgentID,
		TriggerMsgID: event.MsgID,
		EventID:      event.EventID,
		Scope:        resolveDelegateEventScope(event),
		ReceivedAt:   receivedAt,
	}
	applyEventResultDeliveryStatus(&status, payload)
	status.UpdatedAt = time.Now().UnixMilli()
	return status
}

func applyEventResultDeliveryStatus(
	status *protocol.AgentDeliveryStatusPayload,
	payload EventResultPayload,
) {
	if status == nil {
		return
	}
	switch strings.TrimSpace(payload.Status) {
	case protocol.AgentEventResultResponded:
		status.Status = protocol.AgentDeliveryStatusResponded
		status.Code = ""
		status.Msg = ""
	case protocol.AgentEventResultCanceled:
		status.Status = protocol.AgentDeliveryStatusCanceled
		status.Code = firstNonEmpty(strings.TrimSpace(payload.Code), protocol.AgentDeliveryCodeCanceled)
		status.Msg = firstNonEmpty(strings.TrimSpace(payload.Msg), "agent api event canceled")
	default:
		status.Status = protocol.AgentDeliveryStatusFailed
		status.Code = firstNonEmpty(strings.TrimSpace(payload.Code), protocol.AgentDeliveryCodeProcessingFailed)
		status.Msg = firstNonEmpty(strings.TrimSpace(payload.Msg), "agent api event processing failed")
	}
}

// resolvePendingEventResultFromActiveRun settles the in-memory run when its
// pending/durable context has already expired or was lost during recovery.
// Ownership is validated by handleEventResult before this fallback is called.
func (m *Manager) resolvePendingEventResultFromActiveRun(payload EventResultPayload) bool {
	run := m.LookupActiveRun(payload.EventID)
	if run == nil {
		return false
	}
	status := protocol.AgentDeliveryStatusPayload{
		SessionID:    run.SessionID,
		OwnerID:      run.OwnerID,
		AgentID:      run.AgentID,
		TriggerMsgID: run.TriggerMsgID,
		EventID:      run.EventID,
		Scope:        run.Scope,
	}
	switch strings.TrimSpace(payload.Status) {
	case protocol.AgentEventResultResponded:
		status.Status = protocol.AgentDeliveryStatusResponded
	case protocol.AgentEventResultCanceled:
		status.Status = protocol.AgentDeliveryStatusCanceled
		status.Code = firstNonEmpty(strings.TrimSpace(payload.Code), protocol.AgentDeliveryCodeCanceled)
		status.Msg = firstNonEmpty(strings.TrimSpace(payload.Msg), "agent api event canceled")
	default:
		status.Status = protocol.AgentDeliveryStatusFailed
		status.Code = firstNonEmpty(strings.TrimSpace(payload.Code), protocol.AgentDeliveryCodeProcessingFailed)
		status.Msg = firstNonEmpty(strings.TrimSpace(payload.Msg), "agent api event processing failed")
	}
	status.UpdatedAt = time.Now().UnixMilli()
	m.emitDeliveryStatus(status)
	switch status.Status {
	case protocol.AgentDeliveryStatusResponded:
		m.MarkRunCompleted(run.EventID)
	case protocol.AgentDeliveryStatusCanceled:
		m.MarkRunStopped(run.EventID, firstNonEmpty(status.Code, status.Msg))
	default:
		m.MarkRunFailedNotify(
			run.EventID,
			firstNonEmpty(status.Code, status.Msg),
			status.Code,
			payload.Msg,
		)
	}
	return true
}

// observeStaleResultEventsForNewEvent records potentially stale work before a
// new dispatch, but deliberately does not stop or finalize it. Backend-side
// inactivity is an observation, not reliable evidence that connector-side
// execution failed. The connector remains authoritative through
// event_result/event_stop_result.
func (m *Manager) observeStaleResultEventsForNewEvent(evt DelegateEventPayload) {
	sessionID := strings.TrimSpace(evt.SessionID)
	if sessionID == "" || evt.OwnerID <= 0 {
		return
	}
	idle := m.staleRunReapWait
	if idle <= 0 {
		return
	}
	now := time.Now().UnixMilli()
	newEventID := strings.TrimSpace(evt.EventID)

	var staleEventIDs []string
	m.acksMu.Lock()
	for eventID, entry := range m.pending {
		if eventID == newEventID || entry == nil {
			continue
		}
		if entry.kind != pendingEventKindDelegate || entry.stage != pendingEventStageResult {
			continue
		}
		if strings.TrimSpace(entry.event.SessionID) != sessionID || entry.event.OwnerID != evt.OwnerID {
			continue
		}
		last := entry.selfTouchAt
		if last <= 0 {
			last = entry.status.ReceivedAt
		}
		if last <= 0 {
			last = entry.status.UpdatedAt
		}
		// 三个时间戳都缺失时无法判断年龄，宁可不收——正常注册路径总会写
		// selfTouchAt/UpdatedAt，全零只可能来自异常构造，误杀比漏收危害大。
		if last <= 0 || now-last < idle.Milliseconds() {
			continue
		}
		staleEventIDs = append(staleEventIDs, eventID)
	}
	m.acksMu.Unlock()

	for _, staleEventID := range staleEventIDs {
		logger.L.Warnf(
			"agent api observed stale event before new dispatch; preserving non-terminal state session=%s owner=%d stale_event=%s new_event=%s",
			sessionID,
			evt.OwnerID,
			staleEventID,
			newEventID,
		)
	}
}

func (m *Manager) preservePendingForAgentDisconnect(agentID int64) {
	if agentID <= 0 {
		return
	}

	m.acksMu.Lock()
	defer m.acksMu.Unlock()

	for eventID, entry := range m.pending {
		if entry.agentID != agentID || entry.kind == pendingEventKindRevoke {
			continue
		}
		wait := m.pendingDisconnectWait(entry)
		if wait <= 0 {
			continue
		}
		m.resetPendingEventTimerLocked(entry, eventID, wait)
	}
}

func (m *Manager) pendingDisconnectWait(entry *pendingEventAck) time.Duration {
	if entry == nil {
		return 0
	}
	wait := m.disconnectRecoveryWait
	switch entry.stage {
	case pendingEventStageAck:
		if wait < m.eventAckWait {
			wait = m.eventAckWait
		}
	case pendingEventStageResult:
		if wait < m.eventResultWait {
			wait = m.eventResultWait
		}
	}
	return wait
}

func (m *Manager) pendingTrackingRetention() time.Duration {
	if m != nil && m.pendingTrackingTTL > 0 {
		return m.pendingTrackingTTL
	}
	return durablePendingDelegateTTL
}

func (m *Manager) pendingTrackingRemaining(entry *pendingEventAck, now time.Time) time.Duration {
	if entry == nil {
		return 0
	}
	if entry.trackingExpireAt <= 0 {
		entry.trackingExpireAt = now.Add(m.pendingTrackingRetention()).UnixMilli()
	}
	remaining := time.UnixMilli(entry.trackingExpireAt).Sub(now)
	if remaining <= 0 {
		return time.Millisecond
	}
	return remaining
}

// retireExpiredEventTracking bounds backend-owned metadata without inventing
// an execution result. It deliberately emits no delivery/output status and no
// event_stop. Late stream/send output remains admissible through the
// independently authorized session fallback.
func (m *Manager) retireExpiredEventTracking(
	eventID string,
	expected pendingDurableExpectation,
) {
	if expected.agentID <= 0 || expected.ownerID <= 0 {
		return
	}
	deleted := deleteDurablePendingDelegateIfUnchanged(
		context.Background(),
		eventID,
		expected.agentID,
		expected.ownerID,
		expected.version,
		expected.stage,
		expected.attempt,
		expected.dispatchGeneration,
	)
	if !deleted {
		// A newer node won between the pre-expiry read and CAS. Rehydrate that
		// version instead of erasing its durable state or local run.
		if record, ok := loadDurablePendingDelegate(context.Background(), eventID); ok {
			m.recoverPendingFromDurable(record.Event.EventID, record.Event.AgentID)
			return
		}
	}
	// The DB dispatch seed is the long-lived recovery source for a missing
	// Redis snapshot. Retiring only Redis would let the next late output
	// immediately recreate the expired tracker and trust its event_id again.
	// This CAS deletes only the same still-pending generation; a concurrent
	// terminal commit or replacement generation is preserved.
	if deleted {
		seedDeleted, err := store.DeleteAgentEventDispatchSeedIfPending(
			eventID,
			expected.ownerID,
			expected.agentID,
			expected.dispatchGeneration,
		)
		if err != nil {
			logger.L.Warnf(
				"retire expired dispatch ledger seed failed event=%s generation=%d err=%v",
				eventID, expected.dispatchGeneration, err,
			)
		} else if !seedDeleted {
			logger.L.Infof(
				"retire expired dispatch ledger seed CAS lost event=%s generation=%d",
				eventID, expected.dispatchGeneration,
			)
		}
	}
	_ = m.removeRun(eventID)
	m.streamChunkTrackers.release(eventID)

	m.runsMu.Lock()
	delete(m.stopFenceUntil, eventID)
	m.runsMu.Unlock()
}

func durablePendingStageForLocal(entry *pendingEventAck) string {
	if entry != nil && entry.stage == pendingEventStageResult {
		return durablePendingDelegateStageResult
	}
	return durablePendingDelegateStageAck
}

func (m *Manager) resolvePendingEventResultFromDurable(payload EventResultPayload) bool {
	record, ok := loadDurablePendingDelegate(context.Background(), payload.EventID)
	if !ok {
		return false
	}
	if record.Stage != durablePendingDelegateStageAck &&
		record.Stage != durablePendingDelegateStageResult {
		return false
	}
	deleteDurablePendingDelegate(context.Background(), payload.EventID, record.Event.AgentID)

	status := protocol.AgentDeliveryStatusPayload{
		SessionID:    record.Event.SessionID,
		OwnerID:      record.Event.OwnerID,
		AgentID:      record.Event.AgentID,
		TriggerMsgID: record.Event.MsgID,
		EventID:      record.Event.EventID,
		Scope:        resolveDelegateEventScope(record.Event),
		ReceivedAt:   record.ReceivedAt,
	}
	switch strings.TrimSpace(payload.Status) {
	case protocol.AgentEventResultResponded:
		status.Status = protocol.AgentDeliveryStatusResponded
	case protocol.AgentEventResultCanceled:
		status.Status = protocol.AgentDeliveryStatusCanceled
		status.Code = firstNonEmpty(strings.TrimSpace(payload.Code), protocol.AgentDeliveryCodeCanceled)
		status.Msg = firstNonEmpty(strings.TrimSpace(payload.Msg), "agent api event canceled")
	default:
		status.Status = protocol.AgentDeliveryStatusFailed
		status.Code = firstNonEmpty(strings.TrimSpace(payload.Code), protocol.AgentDeliveryCodeProcessingFailed)
		status.Msg = firstNonEmpty(strings.TrimSpace(payload.Msg), "agent api event processing failed")
	}
	status.UpdatedAt = time.Now().UnixMilli()

	m.registerActiveRun(record.Event)
	if record.Stage == durablePendingDelegateStageResult {
		m.MarkRunReceived(record.Event.EventID)
	}
	m.emitDeliveryStatus(status)
	switch status.Status {
	case protocol.AgentDeliveryStatusResponded:
		m.MarkRunCompleted(record.Event.EventID)
	case protocol.AgentDeliveryStatusCanceled:
		m.MarkRunStopped(record.Event.EventID, firstNonEmpty(status.Code, status.Msg))
	default:
		m.MarkRunFailedNotify(
			record.Event.EventID,
			firstNonEmpty(status.Code, status.Msg),
			status.Code,
			payload.Msg,
		)
	}
	return true
}

func (m *Manager) newPendingEventTimer(eventID string, wait time.Duration) *trackedTimer {
	return m.afterFunc(wait, func() {
		m.timeoutPendingEvent(eventID)
	})
}

func (m *Manager) resetPendingEventTimerLocked(entry *pendingEventAck, eventID string, wait time.Duration) {
	if entry == nil {
		return
	}
	if entry.timer != nil {
		entry.timer.Stop()
	}
	if entry.kind == pendingEventKindDelegate {
		remaining := m.pendingTrackingRemaining(entry, time.Now())
		if wait <= 0 || wait > remaining {
			wait = remaining
		}
	}
	entry.timer = m.newPendingEventTimer(eventID, wait)
}

// rollbackPendingEventAck rolls back this registration when the channel write
// fails. The caller queues the same event for reconnect delivery, so this is
// not a terminal failure and must not emit failed/idle state in between.
func (m *Manager) rollbackPendingEventAck(eventID string) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return
	}
	m.acksMu.Lock()
	entry, ok := m.pending[eventID]
	if ok {
		expected := pendingDurableExpectationFromEntry(entry)
		m.acksMu.Unlock()
		deleted := store.RDB == nil || deleteDurablePendingDelegateIfUnchanged(
			context.Background(),
			eventID,
			expected.agentID,
			expected.ownerID,
			expected.version,
			expected.stage,
			expected.attempt,
			expected.dispatchGeneration,
		)
		if !deleted {
			if latest, found := loadDurablePendingDelegate(context.Background(), eventID); found {
				m.syncPendingEventFromDurable(eventID, entry, latest)
			}
			return
		}
		m.acksMu.Lock()
		if current := m.pending[eventID]; current != entry ||
			!pendingEntryMatchesDurableExpectation(current, expected) {
			m.acksMu.Unlock()
			if latest, found := loadDurablePendingDelegate(context.Background(), eventID); found {
				m.syncPendingEventFromDurable(eventID, entry, latest)
			}
			return
		}
		delete(m.pending, eventID)
	}
	m.acksMu.Unlock()
	if !ok || entry == nil || entry.kind != pendingEventKindDelegate {
		return
	}
	if entry.timer != nil {
		entry.timer.Stop()
	}
	if entry.dispatchSeedCreated {
		deleted, err := store.DeleteAgentEventDispatchSeedIfPending(
			eventID,
			entry.event.OwnerID,
			entry.agentID,
			entry.dispatchGeneration,
		)
		if err != nil {
			logger.L.Warnf("rollback dispatch ledger seed failed event=%s err=%v", eventID, err)
		} else if !deleted {
			logger.L.Infof("rollback dispatch seed CAS lost event=%s generation=%d", eventID, entry.dispatchGeneration)
		}
	}
	_ = m.removeRun(eventID)
}
