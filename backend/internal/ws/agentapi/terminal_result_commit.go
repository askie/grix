package agentapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/notification"
	"github.com/askie/grix/backend/internal/pkg/textutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/datatypes"
)

const terminalEffectsClaimLease = 30 * time.Second

func durableRecordFromTerminalLedger(
	ledger *model.AgentEventTerminalLedger,
) *durablePendingDelegateRecord {
	if ledger == nil {
		return nil
	}
	var event DelegateEventPayload
	if len(ledger.DelegateEvent) == 0 ||
		string(ledger.DelegateEvent) == "{}" ||
		json.Unmarshal(ledger.DelegateEvent, &event) != nil {
		return nil
	}
	// The indexed immutable ownership fields remain authoritative even if a
	// legacy/corrupt JSON snapshot contains conflicting values.
	event.EventID = ledger.EventID
	event.TerminalCommitToken = ledger.TerminalCommitToken
	event.OwnerID = ledger.OwnerID
	event.AgentID = ledger.AgentID
	if strings.TrimSpace(event.SessionID) == "" {
		event.SessionID = ledger.SessionID
	}
	if event.SessionType == 0 {
		event.SessionType = ledger.SessionType
	}
	if strings.TrimSpace(event.MirrorMode) == "" {
		event.MirrorMode = ledger.MirrorMode
	}
	if ledger.RecordOnly {
		event.MirrorMode = MirrorModeRecordOnly
	}
	if event.SenderID == 0 {
		event.SenderID = ledger.SenderID
	}
	if event.MsgID == 0 {
		event.MsgID = ledger.TriggerMsgID
	}
	record := &durablePendingDelegateRecord{
		Event:              event,
		Attempt:            0,
		Stage:              durablePendingDelegateStageAck,
		ReceivedAt:         ledger.ReceivedAt,
		CallTurn:           ledger.CallTurn,
		DispatchGeneration: ledger.DispatchGeneration,
	}
	if strings.TrimSpace(ledger.Status) != "" {
		record.Stage = durablePendingDelegateStageResult
		record.Terminal = &durableTerminalIntent{
			Status: ledger.Status,
			Code:   ledger.Code,
			Msg:    ledger.Msg,
		}
	}
	if ledger.StartedAt != nil {
		record.StartedAt = ledger.StartedAt.UnixMilli()
	}
	return record
}

func dispatchLedgerEntry(
	event DelegateEventPayload,
	attempt int,
	startedAt int64,
	callTurn bool,
	dispatchGeneration int64,
) (model.AgentEventTerminalLedger, error) {
	raw, err := json.Marshal(event)
	if err != nil {
		return model.AgentEventTerminalLedger{}, fmt.Errorf("marshal dispatch delegate event: %w", err)
	}
	taskEligible := event.OwnerID > 0 &&
		event.SenderID == event.OwnerID &&
		!callTurn &&
		!event.IsRecordOnly() &&
		!isNoReplyProtocolEvent(event)
	entry := model.AgentEventTerminalLedger{
		EventID:            strings.TrimSpace(event.EventID),
		TerminalCommitToken: strings.TrimSpace(event.TerminalCommitToken),
		OwnerID:            event.OwnerID,
		AgentID:            event.AgentID,
		SessionID:          strings.TrimSpace(event.SessionID),
		SessionType:        event.SessionType,
		MirrorMode:         strings.TrimSpace(event.MirrorMode),
		RecordOnly:         event.IsRecordOnly(),
		SenderID:           event.SenderID,
		TriggerMsgID:       event.MsgID,
		DelegateEvent:      datatypes.JSON(raw),
		CallTurn:           callTurn,
		DispatchGeneration: dispatchGeneration,
		EffectsState:       model.AgentTerminalEffectsPending,
		TaskEligible:       taskEligible,
	}
	_ = attempt // Attempt remains in Redis; the DB snapshot is generation-based.
	if startedAt > 0 {
		value := time.UnixMilli(startedAt).UTC()
		entry.StartedAt = &value
	}
	return entry, nil
}

func terminalChatState(
	payload EventResultPayload,
	record *durablePendingDelegateRecord,
) *model.SessionAgentState {
	if record == nil || record.Event.OwnerID <= 0 ||
		record.Event.SenderID != record.Event.OwnerID || record.CallTurn ||
		record.Event.IsRecordOnly() || isNoReplyProtocolEvent(record.Event) {
		return nil
	}
	state := model.SessionAgentStateFailed
	stopReason := firstNonEmpty(
		strings.TrimSpace(payload.Code),
		strings.TrimSpace(payload.Msg),
		protocol.AgentDeliveryCodeProcessingFailed,
	)
	switch strings.TrimSpace(payload.Status) {
	case protocol.AgentEventResultResponded:
		state = model.SessionAgentStateCompleted
		stopReason = ""
	case protocol.AgentEventResultCanceled:
		state = model.SessionAgentStateIdle
		stopReason = firstNonEmpty(
			strings.TrimSpace(payload.Code),
			strings.TrimSpace(payload.Msg),
			protocol.AgentDeliveryCodeCanceled,
		)
	}
	chatState := &model.SessionAgentState{
		SessionID:   record.Event.SessionID,
		OwnerID:     record.Event.OwnerID,
		AgentID:     record.Event.AgentID,
		State:       state,
		LastRunID:   record.Event.EventID,
		StopReason:  textutil.TruncateRunes(stopReason, model.StopReasonMaxRunes),
		CompletedAt: nowPtr(),
	}
	if record.StartedAt > 0 {
		startedAt := time.UnixMilli(record.StartedAt).UTC()
		chatState.StartedAt = &startedAt
	}
	return chatState
}

// prepareTerminalResult persists every verdict, including proxy/call turns,
// and atomically fences the optional task chat-state transition. It emits no
// externally visible effect.
func (m *Manager) prepareTerminalResult(
	payload EventResultPayload,
	record *durablePendingDelegateRecord,
) (*model.AgentEventTerminalLedger, error) {
	ledger, err := terminalLedgerEntry(payload, record, true)
	if err != nil {
		return nil, err
	}
	disposition, resolved, _, err := store.CommitAgentEventTerminalLedger(
		ledger,
		terminalChatState(payload, record),
	)
	if err != nil {
		return nil, err
	}
	return resolvePreparedTerminalLedger(disposition, resolved, &ledger)
}

// importLegacySettledTerminalResult handles a rolling-upgrade boundary. Older
// nodes ran effects and the chat-state write before writing the Redis settled
// tombstone. A settled tombstone with no ledger therefore means effects have
// already happened; importing it as pending would replay notifications.
func (m *Manager) importLegacySettledTerminalResult(
	payload EventResultPayload,
	record *durablePendingDelegateRecord,
) (*model.AgentEventTerminalLedger, error) {
	ledger, err := terminalLedgerEntry(payload, record, false)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	ledger.EffectsState = model.AgentTerminalEffectsDone
	ledger.EffectsSuppressed = true
	ledger.EffectsDoneAt = &now
	ledger.RedisCommittedAt = &now
	disposition, resolved, _, err := store.CommitAgentEventTerminalLedger(ledger, nil)
	if err != nil {
		return nil, err
	}
	return resolvePreparedTerminalLedger(disposition, resolved, &ledger)
}

func terminalLedgerEntry(
	payload EventResultPayload,
	record *durablePendingDelegateRecord,
	includeSnapshot bool,
) (model.AgentEventTerminalLedger, error) {
	if record == nil {
		return model.AgentEventTerminalLedger{}, errors.New("terminal record required")
	}
	delegateEvent := datatypes.JSON([]byte("{}"))
	if includeSnapshot {
		raw, err := json.Marshal(record.Event)
		if err != nil {
			return model.AgentEventTerminalLedger{}, fmt.Errorf("marshal terminal delegate event: %w", err)
		}
		delegateEvent = datatypes.JSON(raw)
	}
	taskEligible := record.Event.OwnerID > 0 &&
		record.Event.SenderID == record.Event.OwnerID &&
		!record.CallTurn &&
		!record.Event.IsRecordOnly() &&
		!isNoReplyProtocolEvent(record.Event)
	ledger := model.AgentEventTerminalLedger{
		EventID:            strings.TrimSpace(record.Event.EventID),
		TerminalCommitToken: strings.TrimSpace(record.Event.TerminalCommitToken),
		OwnerID:            record.Event.OwnerID,
		AgentID:            record.Event.AgentID,
		SessionID:          strings.TrimSpace(record.Event.SessionID),
		SessionType:        record.Event.SessionType,
		MirrorMode:         strings.TrimSpace(record.Event.MirrorMode),
		RecordOnly:         record.Event.IsRecordOnly(),
		SenderID:           record.Event.SenderID,
		TriggerMsgID:       record.Event.MsgID,
		DelegateEvent:      delegateEvent,
		Status:             strings.TrimSpace(payload.Status),
		Code:               strings.TrimSpace(payload.Code),
		Msg:                strings.TrimSpace(payload.Msg),
		ReceivedAt:         record.ReceivedAt,
		CallTurn:           record.CallTurn,
		DispatchGeneration: record.DispatchGeneration,
		EffectsState:       model.AgentTerminalEffectsPending,
		TaskEligible:       taskEligible,
	}
	if record.StartedAt > 0 {
		startedAt := time.UnixMilli(record.StartedAt).UTC()
		ledger.StartedAt = &startedAt
	}
	return ledger, nil
}

func resolvePreparedTerminalLedger(
	disposition store.AgentTerminalLedgerDisposition,
	resolved *model.AgentEventTerminalLedger,
	fallback *model.AgentEventTerminalLedger,
) (*model.AgentEventTerminalLedger, error) {
	switch disposition {
	case store.AgentTerminalLedgerCreated, store.AgentTerminalLedgerSame:
		if resolved != nil {
			return resolved, nil
		}
		return fallback, nil
	case store.AgentTerminalLedgerConflict:
		return nil, fmt.Errorf("conflicting terminal ledger verdict")
	case store.AgentTerminalLedgerForeign:
		return nil, fmt.Errorf("foreign terminal ledger verdict")
	default:
		return nil, fmt.Errorf("terminal ledger not persisted")
	}
}

func terminalResultFromStopResult(
	payload protocol.AgentEventStopResultPayload,
) EventResultPayload {
	result := EventResultPayload{
		EventID:             strings.TrimSpace(payload.EventID),
		TerminalCommitToken: strings.TrimSpace(payload.TerminalCommitToken),
	}
	switch strings.TrimSpace(payload.Status) {
	case "already_finished":
		result.Status = protocol.AgentEventResultResponded
	default:
		result.Status = protocol.AgentEventResultCanceled
		result.Code = firstNonEmpty(
			strings.TrimSpace(payload.Code),
			"owner_requested_stop",
		)
		result.Msg = strings.TrimSpace(payload.Msg)
	}
	return result
}

func terminalResultFromLedger(
	ledger *model.AgentEventTerminalLedger,
) EventResultPayload {
	if ledger == nil {
		return EventResultPayload{}
	}
	return EventResultPayload{
		EventID:             ledger.EventID,
		TerminalCommitToken: ledger.TerminalCommitToken,
		Status:              ledger.Status,
		Code:                ledger.Code,
		Msg:                 ledger.Msg,
	}
}

// settleEventStopTerminal gives stop-only terminal evidence the same durable
// commit/effect pipeline as event_result. The DB ledger is authoritative over
// Redis and local memory, and its dispatch generation fences chat state so a
// delayed stop for an old event cannot overwrite a newer run.
func (m *Manager) settleEventStopTerminal(
	conn *agentConn,
	stopPayload protocol.AgentEventStopResultPayload,
) (bool, *model.AgentEventTerminalLedger, *SendError) {
	if conn == nil {
		return false, nil, &SendError{Code: 5001, Msg: "agent connection unavailable"}
	}
	payload := terminalResultFromStopResult(stopPayload)
	eventID := strings.TrimSpace(payload.EventID)
	ownerID := conn.ownerID
	preloadedLedger, err := store.LoadAgentEventTerminalLedger(eventID)
	if err != nil {
		return false, nil, &SendError{Code: 5001, Msg: "load event stop ledger failed"}
	}
	if preloadedLedger != nil {
		if preloadedLedger.AgentID != conn.agentID ||
			(ownerID > 0 && preloadedLedger.OwnerID != ownerID) {
			return false, preloadedLedger, &SendError{Code: 4003, Msg: "event_id not owned by current connection"}
		}
		if ownerID <= 0 {
			ownerID = preloadedLedger.OwnerID
		}
	}
	disposition, ledger, err := store.ResolveAgentEventTerminalLedger(
		eventID,
		ownerID,
		conn.agentID,
		payload.Status,
		payload.Code,
		payload.Msg,
		payload.TerminalCommitToken,
	)
	if err != nil {
		return false, ledger, &SendError{Code: 5001, Msg: "load event stop ledger failed"}
	}
	if disposition == store.AgentTerminalLedgerForeign {
		return false, ledger, &SendError{Code: 4003, Msg: "event_id not owned by current connection"}
	}

	// A terminal row is immutable. A later stop_result may carry a less
	// specific code than an earlier event_result (or vice versa); replay the
	// frozen verdict and effects instead of inventing a conflict or overwrite.
	if ledger != nil && strings.TrimSpace(ledger.Status) != "" {
		payload = terminalResultFromLedger(ledger)
	}

	var record *durablePendingDelegateRecord
	if ledger != nil {
		record = durableRecordFromTerminalLedger(ledger)
	}
	if record == nil {
		record, _ = loadDurablePendingDelegate(context.Background(), eventID)
	}
	if record == nil {
		var fallbackErr *SendError
		record, fallbackErr = m.resolveEventResultFallback(conn, eventID)
		if fallbackErr != nil {
			return false, ledger, fallbackErr
		}
	}
	if record != nil && (record.Event.AgentID != conn.agentID ||
		(ownerID > 0 && record.Event.OwnerID > 0 && record.Event.OwnerID != ownerID)) {
		return false, ledger, &SendError{Code: 4003, Msg: "event_id not owned by current connection"}
	}
	if ownerID <= 0 && record != nil {
		ownerID = record.Event.OwnerID
	}

	if ledger == nil || strings.TrimSpace(ledger.Status) == "" {
		if record == nil {
			return false, ledger, &SendError{Code: 4003, Msg: "event_stop_result has no recoverable run"}
		}
		ledger, err = m.prepareTerminalResult(payload, record)
		if err != nil {
			return false, ledger, &SendError{Code: 5001, Msg: "prepare event stop terminal failed"}
		}
	}

	repaired, err := repairDurableTerminalIntentFromLedger(context.Background(), ledger)
	if err != nil {
		return false, ledger, &SendError{Code: 5001, Msg: "persist event stop settlement failed"}
	}
	if repaired != nil {
		record = repaired
	}
	redisCommitted, err := store.MarkAgentEventTerminalRedisCommitted(
		eventID,
		ownerID,
		conn.agentID,
		payload.Status,
		payload.Code,
		payload.Msg,
		payload.TerminalCommitToken,
	)
	if err != nil || !redisCommitted {
		return false, ledger, &SendError{Code: 5001, Msg: "persist event stop commit failed"}
	}
	done, err := m.finishTerminalEffects(conn, payload, ledger, record)
	if err != nil {
		return false, ledger, &SendError{Code: 5001, Msg: "finish event stop effects failed"}
	}
	return done, ledger, nil
}

// finishTerminalEffects drains four independent durable outbox items. A lease
// may be reclaimed after a crash, but every sink is itself idempotent:
// Gemini cards use stable client_msg_id, delivery/output carry the immutable
// run_id+timestamp, and notifications are fenced by permanent dispatcher
// receipts. No non-idempotent chat message is generated from delivery status.
func (m *Manager) finishTerminalEffects(
	conn *agentConn,
	payload EventResultPayload,
	ledger *model.AgentEventTerminalLedger,
	record *durablePendingDelegateRecord,
) (bool, error) {
	eventID := strings.TrimSpace(payload.EventID)
	m.acksMu.Lock()
	entry := m.pending[eventID]
	if entry != nil && entry.kind == pendingEventKindDelegate {
		delete(m.pending, eventID)
	}
	m.acksMu.Unlock()
	if entry != nil && entry.timer != nil {
		entry.timer.Stop()
	}
	if ledger != nil && ledger.EffectsState == model.AgentTerminalEffectsDone {
		return true, nil
	}
	if record == nil {
		record = durableRecordFromTerminalLedger(ledger)
	}
	if record == nil {
		return false, errors.New("terminal effect record required")
	}
	if err := store.EnsureAgentEventTerminalEffects(eventID); err != nil {
		return false, err
	}

	stableAt := time.Now().UnixMilli()
	if ledger != nil {
		switch {
		case ledger.TerminalAt != nil:
			stableAt = ledger.TerminalAt.UnixMilli()
		case !ledger.CreatedAt.IsZero():
			stableAt = ledger.CreatedAt.UnixMilli()
		}
	}

	effects := []struct {
		name string
		run  func() error
	}{
		{
			name: model.AgentTerminalEffectGemini,
			run: func() error {
				return m.maybeHandleGeminiEventResultRecord(conn, payload, record)
			},
		},
		{
			name: model.AgentTerminalEffectQuestionCard,
			run: func() error {
				return m.settleAgentQuestionReplyCard(record.Event, payload)
			},
		},
		{
			name: model.AgentTerminalEffectDelivery,
			run: func() error {
				if record.Event.IsRecordOnly() {
					return nil
				}
				status := deliveryStatusForEventResult(record.Event, record.ReceivedAt, payload)
				if entry != nil && entry.kind == pendingEventKindDelegate {
					status = entry.status
					applyEventResultDeliveryStatus(&status, payload)
				}
				status.UpdatedAt = stableAt
				m.emitDeliveryStatus(status)
				return nil
			},
		},
		{
			name: model.AgentTerminalEffectOutput,
			run: func() error {
				if record.Event.IsRecordOnly() {
					return nil
				}
				run := m.terminalOutputRun(record, payload, stableAt)
				m.emitOutputStatus(run)
				return nil
			},
		},
		{
			name: model.AgentTerminalEffectNotification,
			run: func() error {
				if record.Event.IsRecordOnly() || ledger == nil ||
					!ledger.TaskNotificationAllowed {
					return nil
				}
				run := terminalRunFromRecord(record, payload, stableAt)
				notifyReason := terminalNotifyReason(payload)
				switch run.State {
				case protocol.AgentOutputStateCompleted:
					return publishTaskNotification(run, notification.EventTaskCompleted, "", "", true)
				case protocol.AgentOutputStateStopped:
					if isUserInitiatedStopReason(notifyReason) {
						return nil
					}
					return publishTaskNotification(run, notification.EventTaskStoppedUnexpected, "", "", true)
				default:
					if isUserInitiatedStopReason(notifyReason) ||
						!shouldNotifyTaskFailed(run, notifyReason) {
						return nil
					}
					return publishTaskNotification(
						run,
						notification.EventTaskFailed,
						taskFailedSummary(notifyReason),
						taskFailedDetail(payload.Msg),
						true,
					)
				}
			},
		},
	}
	for _, effect := range effects {
		completed, err := m.runTerminalEffect(eventID, effect.name, effect.run)
		if err != nil {
			return false, err
		}
		if !completed {
			return false, nil
		}
	}
	return store.FinalizeAgentEventTerminalEffects(eventID)
}

func (m *Manager) runTerminalEffect(eventID, effect string, fn func() error) (bool, error) {
	claim, err := store.ClaimAgentEventTerminalEffect(
		eventID,
		effect,
		terminalEffectsClaimLease,
	)
	if err != nil {
		return false, err
	}
	if claim.Done {
		return true, nil
	}
	if !claim.Won {
		return false, nil
	}
	if err := fn(); err != nil {
		_ = store.FailAgentEventTerminalEffect(eventID, effect, claim.Token, err)
		return false, err
	}
	completed, err := store.CompleteAgentEventTerminalEffect(eventID, effect, claim.Token)
	if err != nil || !completed {
		return false, err
	}
	return true, nil
}

// terminalNotifyReason derives the machine stop-reason code that push
// decisions are keyed on. It deliberately ignores payload.Msg, unlike
// run.StopReason: every notification guard (userInitiatedStopReasons,
// suppressedFailureNotifyReasons, deferredCleanupNotifyReasons) is a code
// lookup, and free text matches none of them. Connectors report most terminal
// results with a message and no code, so folding the message in silently
// disabled those guards on this path — a user pressing stop got an "unexpected
// stop" push, and the stale-failure window from the 2026-07-10 misfire audit
// never applied. The message still reaches the user as the push detail, and
// run.StopReason keeps it verbatim for output_status and chat_states.
func terminalNotifyReason(payload EventResultPayload) string {
	if code := strings.TrimSpace(payload.Code); code != "" {
		return code
	}
	if strings.TrimSpace(payload.Status) == protocol.AgentEventResultCanceled {
		return protocol.AgentDeliveryCodeCanceled
	}
	return protocol.AgentDeliveryCodeProcessingFailed
}

func terminalRunFromRecord(
	record *durablePendingDelegateRecord,
	payload EventResultPayload,
	stableAt int64,
) *activeAgentRun {
	if record == nil {
		return nil
	}
	run := &activeAgentRun{
		EventID:       record.Event.EventID,
		SessionID:     record.Event.SessionID,
		ThreadID:      record.Event.ThreadID,
		Scope:         resolveDelegateEventScope(record.Event),
		SessionType:   record.Event.SessionType,
		SenderID:      record.Event.SenderID,
		OwnerID:       record.Event.OwnerID,
		AgentID:       record.Event.AgentID,
		TriggerMsgID:  record.Event.MsgID,
		TriggerQuoted: record.Event.QuotedMessageID,
		StartedAt:     record.StartedAt,
		RunGeneration: record.DispatchGeneration,
		CallTurn:      record.CallTurn,
		UpdatedAt:     stableAt,
		CanStop:       false,
	}
	switch strings.TrimSpace(payload.Status) {
	case protocol.AgentEventResultResponded:
		run.State = protocol.AgentOutputStateCompleted
	case protocol.AgentEventResultCanceled:
		run.State = protocol.AgentOutputStateStopped
		run.StopReason = firstNonEmpty(payload.Code, payload.Msg, protocol.AgentDeliveryCodeCanceled)
	default:
		run.State = protocol.AgentOutputStateFailed
		run.StopReason = firstNonEmpty(payload.Code, payload.Msg, protocol.AgentDeliveryCodeProcessingFailed)
	}
	return run
}

func (m *Manager) terminalOutputRun(
	record *durablePendingDelegateRecord,
	payload EventResultPayload,
	stableAt int64,
) *activeAgentRun {
	if record == nil {
		return nil
	}
	desired := terminalRunFromRecord(record, payload, stableAt)
	run, _ := m.settleRun(
		record.Event.EventID,
		desired.State,
		desired.StopReason,
	)
	if run == nil {
		run = desired
	} else {
		run.UpdatedAt = stableAt
		run.RunGeneration = record.DispatchGeneration
	}
	return run
}
