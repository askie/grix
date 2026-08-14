package agentapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	codexadapter "github.com/askie/grix/backend/internal/agentadapter/codex"
	agenttoolbar "github.com/askie/grix/backend/internal/agenttoolbar"
	tooli18n "github.com/askie/grix/backend/internal/agenttoolbar/i18n"
	toolstore "github.com/askie/grix/backend/internal/agenttoolbar/store"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

const terminalCommitCapability = "terminal_commit_v1"

func (m *Manager) handleEventAck(conn *agentConn, pkt *protocol.Packet) {
	logger.L.Infof("agent api event_ack raw received agent=%d seq=%d bytes=%d", conn.agentID, pkt.Seq, len(pkt.Payload))
	var payload EventAckPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.sendPayload("error", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "invalid event_ack payload",
		})
		return
	}
	if strings.TrimSpace(payload.EventID) == "" {
		conn.sendPayload("error", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "event_id required",
		})
		return
	}
	eventID := strings.TrimSpace(payload.EventID)
	m.acksMu.Lock()
	entry := m.pending[eventID]
	m.acksMu.Unlock()
	if entry == nil {
		logger.L.Warnf(
			"agent api late event_ack has no tracking record; accepting without state transition event=%s agent=%d owner=%d",
			eventID, conn.agentID, conn.ownerID,
		)
		return
	}
	if entry.agentID != conn.agentID ||
		(conn.ownerID > 0 && entry.event.OwnerID > 0 && entry.event.OwnerID != conn.ownerID) {
		conn.recordViolation()
		conn.sendPayload("error", pkt.Seq, SendNackPayload{
			Code: 4003,
			Msg:  "event_id not owned by current connection",
		})
		return
	}
	m.resolvePendingEventAck(payload.EventID, payload.ReceivedAt)
}

func (m *Manager) handleEventResult(conn *agentConn, pkt *protocol.Packet) {
	var payload EventResultPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.sendPayload("error", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "invalid event_result payload",
		})
		return
	}
	if strings.TrimSpace(payload.EventID) == "" {
		conn.sendPayload("error", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "event_id required",
		})
		return
	}
	if strings.TrimSpace(payload.TerminalCommitToken) != "" &&
		!hasDeclaredName(conn.capabilities, terminalCommitCapability) {
		conn.recordViolation()
		conn.sendPayload("error", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "terminal_commit_token requires terminal_commit_v1",
		})
		return
	}
	status := strings.TrimSpace(payload.Status)
	switch status {
	case protocol.AgentEventResultResponded, protocol.AgentEventResultFailed, protocol.AgentEventResultCanceled:
		// canceled 对全部连接器（含 hermes）放行：hermes 契约
		// （protocol.hermesEventResultStatuses）自插件 1.7.x 起已包含 canceled，
		// 此前对 hermes 的 4001 硬拒会让 canceled 终态无法结算 run，
		// 导致工具栏停止按钮残留。
	default:
		conn.sendPayload("error", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "unsupported event_result status",
		})
		return
	}

	if store.RDB != nil {
		m.handleDurableEventResult(conn, pkt, payload)
		return
	}
	m.handleLocalEventResult(conn, pkt, payload)
}

func eventResultVerdict(payload EventResultPayload) string {
	return strings.Join([]string{
		strings.TrimSpace(payload.Status),
		strings.TrimSpace(payload.Code),
		strings.TrimSpace(payload.Msg),
	}, "\x00")
}

func (m *Manager) sendEventResultAck(
	conn *agentConn,
	pkt *protocol.Packet,
	payload EventResultPayload,
) {
	if conn == nil || pkt == nil {
		return
	}
	terminalCommitToken := strings.TrimSpace(payload.TerminalCommitToken)
	if terminalCommitToken == "" &&
		!hasDeclaredName(conn.capabilities, "event_result_ack") {
		return
	}
	ack := map[string]any{
		"event_id":    strings.TrimSpace(payload.EventID),
		"status":      strings.TrimSpace(payload.Status),
		"received_at": time.Now().UnixMilli(),
	}
	if terminalCommitToken != "" {
		ack["terminal_committed"] = true
		ack["terminal_commit_token"] = terminalCommitToken
	}
	conn.sendPayload(protocol.CmdSendAck, pkt.Seq, ack)
}

func (m *Manager) sendEventResultError(
	conn *agentConn,
	pkt *protocol.Packet,
	code int,
	msg string,
) {
	if conn == nil || pkt == nil {
		return
	}
	conn.sendPayload("error", pkt.Seq, SendNackPayload{Code: code, Msg: msg})
}

func (m *Manager) handleDurableEventResult(
	conn *agentConn,
	pkt *protocol.Packet,
	payload EventResultPayload,
) {
	eventID := strings.TrimSpace(payload.EventID)
	ledgerDisposition, ledger, ledgerErr := store.ResolveAgentEventTerminalLedger(
		eventID,
		conn.ownerID,
		conn.agentID,
		payload.Status,
		payload.Code,
		payload.Msg,
		payload.TerminalCommitToken,
	)
	if ledgerErr != nil {
		m.sendEventResultError(conn, pkt, 5001, "load event_result ledger failed")
		return
	}
	switch ledgerDisposition {
	case store.AgentTerminalLedgerForeign:
		conn.recordViolation()
		m.sendEventResultError(conn, pkt, 4003, "event_id not owned by current connection")
		return
	case store.AgentTerminalLedgerConflict:
		conn.recordViolation()
		m.sendEventResultError(conn, pkt, 4001, "conflicting event_result verdict")
		return
	case store.AgentTerminalLedgerSame:
		// The long-lived DB row is authoritative. Repair a missing or even
		// opposite Redis verdict before ACKing; stale Redis coordination must
		// never turn a committed same-verdict replay into a 4001.
		repaired, repairErr := repairDurableTerminalIntentFromLedger(
			context.Background(),
			ledger,
		)
		if repairErr != nil {
			m.sendEventResultError(conn, pkt, 5001, "repair event_result settlement failed")
			return
		}
		redisCommitted, markErr := store.MarkAgentEventTerminalRedisCommitted(
			eventID,
			conn.ownerID,
			conn.agentID,
			payload.Status,
			payload.Code,
			payload.Msg,
			payload.TerminalCommitToken,
		)
		if markErr != nil || !redisCommitted {
			m.sendEventResultError(conn, pkt, 5001, "persist event_result commit failed")
			return
		}
		done, finishErr := m.finishTerminalEffects(conn, payload, ledger, repaired)
		if finishErr != nil {
			m.sendEventResultError(conn, pkt, 5001, "finish event_result effects failed")
			return
		}
		if done {
			m.sendEventResultAck(conn, pkt, payload)
		}
		return
	}

	claim, err := claimDurableTerminalIntent(
		context.Background(),
		eventID,
		conn.agentID,
		conn.ownerID,
		payload,
		nil,
	)
	if err != nil {
		m.sendEventResultError(conn, pkt, 5001, "persist event_result intent failed")
		return
	}
	if claim.Disposition == terminalIntentMissing {
		var fallback *durablePendingDelegateRecord
		var fallbackErr *SendError
		if (ledgerDisposition == store.AgentTerminalLedgerSame ||
			ledgerDisposition == store.AgentTerminalLedgerPending) && ledger != nil {
			fallback = durableRecordFromTerminalLedger(ledger)
		} else {
			fallback, fallbackErr = m.resolveEventResultFallback(conn, eventID)
		}
		if fallbackErr != nil {
			conn.recordViolation()
			m.sendEventResultError(conn, pkt, fallbackErr.Code, fallbackErr.Msg)
			return
		}
		if fallback == nil {
			// Without durable/local/DB ownership there is no recoverable commit.
			// Do not ACK and cause the connector to discard its terminal outbox.
			m.sendEventResultError(conn, pkt, 4003, "event_result has no recoverable run")
			return
		}
		claim, err = claimDurableTerminalIntent(
			context.Background(),
			eventID,
			conn.agentID,
			conn.ownerID,
			payload,
			fallback,
		)
		if err != nil {
			m.sendEventResultError(conn, pkt, 5001, "persist event_result intent failed")
			return
		}
	}

	switch claim.Disposition {
	case terminalIntentPending:
		// The winner has a durable lease but has not committed settlement yet.
		// Returning without ACK keeps the connector outbox retryable.
		return
	case terminalIntentSettled:
		// Redis may have committed before the process could persist the DB
		// marker/effects. Continue through the same prepare/finalize path.
	case terminalIntentConflict:
		conn.recordViolation()
		m.sendEventResultError(conn, pkt, 4001, "conflicting event_result verdict")
		return
	case terminalIntentUnauthorized:
		conn.recordViolation()
		m.sendEventResultError(conn, pkt, 4003, "event_id not owned by current connection")
		return
	case terminalIntentClaimed:
		// Continue below.
	default:
		m.sendEventResultError(conn, pkt, 4003, "event_result has no recoverable run")
		return
	}

	if claim.Record == nil || claim.Record.Terminal == nil {
		m.sendEventResultError(conn, pkt, 5001, "invalid event_result intent")
		return
	}
	if store.DB == nil && claim.Disposition == terminalIntentSettled {
		// Standalone tests/development have no long-lived effect ledger. Redis
		// settlement still makes duplicate ACK safe, but effects ran only on
		// the original claimed winner.
		m.sendEventResultAck(conn, pkt, payload)
		return
	}
	if ledgerDisposition == store.AgentTerminalLedgerMissing &&
		claim.Disposition == terminalIntentSettled {
		if _, err := m.importLegacySettledTerminalResult(payload, claim.Record); err != nil {
			logger.L.Warnf("import legacy settled event_result failed event=%s err=%v", eventID, err)
			m.sendEventResultError(conn, pkt, 5001, "import event_result settlement failed")
			return
		}
		m.sendEventResultAck(conn, pkt, payload)
		return
	}
	preparedLedger, err := m.prepareTerminalResult(payload, claim.Record)
	if err != nil {
		logger.L.Warnf("prepare claimed event_result failed event=%s err=%v", eventID, err)
		m.sendEventResultError(conn, pkt, 5001, "prepare event_result failed")
		return
	}
	if claim.Disposition == terminalIntentClaimed {
		settled, settleErr := settleDurableTerminalIntent(
			context.Background(),
			claim.Record,
			claim.Token,
		)
		if settleErr != nil || !settled {
			logger.L.Warnf(
				"commit durable event_result failed event=%s settled=%t err=%v",
				eventID, settled, settleErr,
			)
			m.sendEventResultError(conn, pkt, 5001, "commit event_result failed")
			return
		}
	}
	redisCommitted, err := store.MarkAgentEventTerminalRedisCommitted(
		eventID,
		conn.ownerID,
		conn.agentID,
		payload.Status,
		payload.Code,
		payload.Msg,
		payload.TerminalCommitToken,
	)
	if err != nil || !redisCommitted {
		m.sendEventResultError(conn, pkt, 5001, "persist event_result commit failed")
		return
	}
	done, err := m.finishTerminalEffects(conn, payload, preparedLedger, claim.Record)
	if err != nil {
		m.sendEventResultError(conn, pkt, 5001, "finish event_result effects failed")
		return
	}
	if !done {
		return
	}
	m.sendEventResultAck(conn, pkt, payload)
}

func (m *Manager) resolveEventResultFallback(
	conn *agentConn,
	eventID string,
) (*durablePendingDelegateRecord, *SendError) {
	m.acksMu.Lock()
	entry := m.pending[eventID]
	if entry != nil {
		if entry.agentID != conn.agentID ||
			(entry.event.OwnerID > 0 && entry.event.OwnerID != conn.ownerID) {
			m.acksMu.Unlock()
			return nil, &SendError{Code: 4003, Msg: "event_id not owned by current connection"}
		}
		event := entry.event
		attempt := entry.attempt
		m.acksMu.Unlock()
		record := &durablePendingDelegateRecord{
			Event:              event,
			Attempt:            attempt,
			Stage:              durablePendingDelegateStageResult,
			DispatchGeneration: entry.dispatchGeneration,
		}
		if run := m.LookupActiveRun(eventID); run != nil {
			record.StartedAt = run.StartedAt
			record.CallTurn = run.CallTurn
		}
		return record, nil
	}
	m.acksMu.Unlock()

	if run := m.LookupActiveRun(eventID); run != nil {
		if run.AgentID != conn.agentID || (run.OwnerID > 0 && run.OwnerID != conn.ownerID) {
			return nil, &SendError{Code: 4003, Msg: "event_id not owned by current connection"}
		}
		return &durablePendingDelegateRecord{
			Event: DelegateEventPayload{
				EventID:         run.EventID,
				AgentID:         run.AgentID,
				OwnerID:         run.OwnerID,
				SessionID:       run.SessionID,
				ThreadID:        run.ThreadID,
				SessionType:     run.SessionType,
				SenderID:        run.SenderID,
				MsgID:           run.TriggerMsgID,
				QuotedMessageID: run.TriggerQuoted,
			},
			Attempt:            0,
			Stage:              durablePendingDelegateStageResult,
			StartedAt:          run.StartedAt,
			CallTurn:           run.CallTurn,
			DispatchGeneration: run.RunGeneration,
		}, nil
	}

	state, err := store.FindNonTerminalSessionAgentStateByRun(
		eventID,
		conn.ownerID,
		conn.agentID,
	)
	if err != nil {
		return nil, &SendError{Code: 5001, Msg: "load event_result run failed"}
	}
	if state == nil {
		return nil, nil
	}
	record := &durablePendingDelegateRecord{
		Event: DelegateEventPayload{
			EventID:   eventID,
			AgentID:   conn.agentID,
			OwnerID:   conn.ownerID,
			SessionID: state.SessionID,
			SenderID:  conn.ownerID,
		},
		Attempt:            0,
		Stage:              durablePendingDelegateStageResult,
		DispatchGeneration: state.RunGeneration,
	}
	if state.StartedAt != nil {
		record.StartedAt = state.StartedAt.UnixMilli()
	}
	return record, nil
}

func (m *Manager) handleLocalEventResult(
	conn *agentConn,
	pkt *protocol.Packet,
	payload EventResultPayload,
) {
	if guardErr := m.ensureEventOwnedByConnection(
		payload.EventID,
		conn.agentID,
		conn.ownerID,
	); guardErr != nil {
		conn.recordViolation()
		m.sendEventResultError(conn, pkt, guardErr.Code, guardErr.Msg)
		return
	}
	eventID := strings.TrimSpace(payload.EventID)
	verdict := eventResultVerdict(payload)
	m.eventResultsMu.Lock()
	if m.eventResultsInFlight == nil {
		m.eventResultsInFlight = make(map[string]struct{})
	}
	if m.eventResultVerdicts == nil {
		m.eventResultVerdicts = make(map[string]string)
	}
	if m.eventResultsSettled == nil {
		m.eventResultsSettled = make(map[string]string)
	}
	if settled, ok := m.eventResultsSettled[eventID]; ok {
		m.eventResultsMu.Unlock()
		if settled == verdict {
			m.sendEventResultAck(conn, pkt, payload)
		} else {
			m.sendEventResultError(conn, pkt, 4001, "conflicting event_result verdict")
		}
		return
	}
	if _, inFlight := m.eventResultsInFlight[eventID]; inFlight {
		existing := m.eventResultVerdicts[eventID]
		m.eventResultsMu.Unlock()
		if existing != verdict {
			m.sendEventResultError(conn, pkt, 4001, "conflicting event_result verdict")
		}
		// Same-verdict loser intentionally receives no ACK until settlement.
		return
	}
	m.eventResultsInFlight[eventID] = struct{}{}
	m.eventResultVerdicts[eventID] = verdict
	m.eventResultsMu.Unlock()

	if record, ok := loadDurablePendingDelegate(context.Background(), eventID); ok && record != nil {
		if err := m.settleAgentQuestionReplyCard(record.Event, payload); err != nil {
			logger.L.Warnf("settle agent question card failed event=%s err=%v", eventID, err)
		}
	}
	m.maybeHandleGeminiEventResult(conn, payload)
	m.resolvePendingEventResult(payload)

	m.eventResultsMu.Lock()
	delete(m.eventResultsInFlight, eventID)
	m.eventResultsSettled[eventID] = verdict
	m.eventResultsMu.Unlock()
	m.sendEventResultAck(conn, pkt, payload)
}

func (m *Manager) handleEventStopAck(conn *agentConn, pkt *protocol.Packet) {
	var payload protocol.AgentEventStopAckPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.sendPayload("error", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "invalid event_stop_ack payload",
		})
		return
	}
	scope := strings.TrimSpace(payload.Scope)
	sessionID := strings.TrimSpace(payload.SessionID)
	eventID := strings.TrimSpace(payload.EventID)
	if scope == protocol.AgentEventStopScopeSession {
		if sessionID == "" {
			conn.sendPayload("error", pkt.Seq, SendNackPayload{
				Code: 4001,
				Msg:  "session_id required for session-scoped event_stop_ack",
			})
			return
		}
		// A composing-only stop is intentionally not tied to an event record.
		// The connector may echo the uniquely resolved active event ID, but this
		// ACK is only an observation of command acceptance.
		logger.L.Infof(
			"agent_output_stop session event_stop_ack agent=%d session=%s event_id=%s stop_id=%s accepted=%t",
			conn.agentID,
			sessionID,
			eventID,
			strings.TrimSpace(payload.StopID),
			payload.Accepted,
		)
		return
	}
	if eventID == "" {
		conn.sendPayload("error", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "event_id required",
		})
		return
	}
	if guardErr := m.ensureEventOwnedByConnection(eventID, conn.agentID, conn.ownerID); guardErr != nil && !guardErr.NotFound {
		conn.recordViolation()
		conn.sendPayload("error", pkt.Seq, SendNackPayload{Code: guardErr.Code, Msg: guardErr.Msg})
		return
	}
	logger.L.Infof(
		"agent_output_stop event_stop_ack agent=%d event_id=%s stop_id=%s accepted=%t",
		conn.agentID,
		eventID,
		strings.TrimSpace(payload.StopID),
		payload.Accepted,
	)
}

func (m *Manager) handleEventStopResult(conn *agentConn, pkt *protocol.Packet) {
	var payload protocol.AgentEventStopResultPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.sendPayload("error", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "invalid event_stop_result payload",
		})
		return
	}
	eventID := strings.TrimSpace(payload.EventID)
	stopStatus := strings.TrimSpace(payload.Status)
	switch stopStatus {
	case "stopped", "already_finished", "failed":
	default:
		conn.sendPayload("error", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "unsupported event_stop_result status",
		})
		return
	}
	scope := strings.TrimSpace(payload.Scope)
	sessionID := strings.TrimSpace(payload.SessionID)
	if scope == protocol.AgentEventStopScopeSession {
		if sessionID == "" {
			conn.sendPayload("error", pkt.Seq, SendNackPayload{
				Code: 4001,
				Msg:  "session_id required for session-scoped event_stop_result",
			})
			return
		}
		// Session-scoped results close the composing-only stop command. They do
		// not own an event lifecycle, even when the connector safely resolved
		// and echoed one active event ID, so skip ownership and terminal state.
		logger.L.Infof(
			"agent_output_stop session event_stop_result agent=%d session=%s event_id=%s stop_id=%s status=%s code=%s msg=%s",
			conn.agentID,
			sessionID,
			eventID,
			strings.TrimSpace(payload.StopID),
			stopStatus,
			strings.TrimSpace(payload.Code),
			strings.TrimSpace(payload.Msg),
		)
		return
	}
	if eventID == "" {
		conn.sendPayload("error", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "event_id required",
		})
		return
	}
	if stopStatus == "failed" {
		if guardErr := m.ensureEventOwnedByConnection(eventID, conn.agentID, conn.ownerID); guardErr != nil {
			if guardErr.NotFound {
				logger.L.Warnf(
					"agent_output_stop late failed result has no tracking record event_id=%s agent=%d owner=%d",
					eventID, conn.agentID, conn.ownerID,
				)
				return
			}
			conn.recordViolation()
			conn.sendPayload("error", pkt.Seq, SendNackPayload{Code: guardErr.Code, Msg: guardErr.Msg})
			return
		}
	}
	logger.L.Infof(
		"agent_output_stop event_stop_result agent=%d event_id=%s stop_id=%s status=%s code=%s msg=%s",
		conn.agentID,
		eventID,
		strings.TrimSpace(payload.StopID),
		strings.TrimSpace(payload.Status),
		strings.TrimSpace(payload.Code),
		strings.TrimSpace(payload.Msg),
	)

	if stopStatus == "failed" {
		// The stop command failed; this is explicit evidence that execution may
		// still be running, not evidence that execution itself failed.
		m.MarkRunStopFailed(
			eventID,
			firstNonEmpty(
				strings.TrimSpace(payload.Code),
				strings.TrimSpace(payload.Msg),
				"event_stop_failed",
			),
		)
		return
	}

	// stopped/already_finished may be the only terminal packet for a queued
	// event that the connector discarded during restart. Commit it through the
	// same immutable DB ledger and per-effect outbox as event_result. The
	// ledger's dispatch generation is also the chat-state CAS fence, so an old
	// stop cannot overwrite a newer same-session run on another node.
	settled, ledger, settleErr := m.settleEventStopTerminal(conn, payload)
	if settleErr != nil {
		if settleErr.Code == 4003 {
			conn.recordViolation()
		}
		conn.sendPayload("error", pkt.Seq, SendNackPayload{
			Code: settleErr.Code,
			Msg:  settleErr.Msg,
		})
		logger.L.Warnf(
			"agent_output_stop terminal settlement failed event_id=%s status=%s agent=%d owner=%d code=%d msg=%s",
			eventID,
			stopStatus,
			conn.agentID,
			conn.ownerID,
			settleErr.Code,
			settleErr.Msg,
		)
		return
	}
	if settled && ledger != nil && strings.TrimSpace(ledger.TerminalCommitToken) != "" {
		m.sendEventResultAck(conn, pkt, terminalResultFromLedger(ledger))
	}
}

func (m *Manager) handleClientStreamChunk(conn *agentConn, pkt *protocol.Packet) {
	var payload AgentStreamChunkPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.recordViolation()
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: protocol.CodeInvalidPayload,
			Msg:  "invalid client_stream_chunk payload",
		})
		return
	}
	if strings.TrimSpace(payload.SessionID) == "" {
		conn.recordViolation()
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			ClientMsgID: payload.ClientMsgID,
			Code:        protocol.CodeInvalidPayload,
			Msg:         "session_id required",
		})
		return
	}
	// Every user-visible output path shares the same authorization/fallback
	// policy. A late packet whose event tracking record has expired is still
	// accepted through the authorized session route; a known foreign event
	// remains a hard permission failure.
	originalEventID := strings.TrimSpace(payload.EventID)
	authorization, guardErr := m.authorizeInboundOutput(
		context.Background(), conn, originalEventID, payload.SessionID,
	)
	if guardErr != nil {
		conn.recordViolation()
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			ClientMsgID: payload.ClientMsgID,
			Code:        guardErr.Code,
			Msg:         guardErr.Msg,
		})
		return
	}
	if authorization.AbsorbTerminal {
		conn.sendPayload(protocol.CmdSendAck, pkt.Seq, map[string]any{
			"event_id":          originalEventID,
			"client_msg_id":     payload.ClientMsgID,
			"terminal_absorbed": true,
			"received_at":       time.Now().UnixMilli(),
		})
		return
	}
	resolvedEventID := authorization.EventID
	if originalEventID == "" {
		if sessionErr := m.ensureSessionWritableBy(
			context.Background(), conn.agentID, conn.ownerID, payload.SessionID,
		); sessionErr != nil {
			conn.recordViolation()
			conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
				ClientMsgID: payload.ClientMsgID,
				Code:        sessionErr.Code,
				Msg:         sessionErr.Msg,
			})
			return
		}
	}
	if originalEventID != "" && resolvedEventID == "" && payload.ChunkSeq <= 1 {
		logger.L.Warnf(
			"stream_chunk: event_id not found, accepting via authorized session route: event_id=%s session_id=%s agent=%d owner=%d",
			originalEventID, payload.SessionID, conn.agentID, conn.ownerID,
		)
	}
	payload.EventID = resolvedEventID
	isNoReplyOutput := ShouldSilentlyAckInboundOutput(
		payload.DeltaContent,
		m.IsNoReplyProtocolContext(firstNonEmpty(resolvedEventID, originalEventID)),
	)
	if payload.DeltaContent == "" && !payload.IsFinish {
		conn.recordViolation()
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			ClientMsgID: payload.ClientMsgID,
			Code:        protocol.CodeInvalidPayload,
			Msg:         "delta_content required for non-finish chunk",
		})
		return
	}
	// Phase 1.2: 协议层硬上限。
	if utf8.RuneCountInString(payload.DeltaContent) > protocol.MaxDeltaContentChars {
		conn.recordViolation()
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			ClientMsgID: payload.ClientMsgID,
			Code:        protocol.CodePayloadTooLarge,
			Msg:         "delta_content too large",
		})
		return
	}
	if payload.ChunkSeq <= 0 {
		conn.recordViolation()
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			ClientMsgID: payload.ClientMsgID,
			Code:        protocol.CodeInvalidPayload,
			Msg:         "chunk_seq required",
		})
		return
	}
	if strings.TrimSpace(payload.ClientMsgID) == "" {
		// 同一条回复的所有分片必须共用稳定的 client_msg_id（服务端据此重排/落库）。
		// 优先按 event_id 派生，确保缺省 client_msg_id 时同一回复的分片仍归并到
		// 同一个流/气泡。tracking 过期时 resolved event_id 会因授权边界被清空，
		// 但已通过 session 授权的原始 ID 仍可安全地只用于本地归并键，不能继续
		// 作为事件归属凭据下传。只有主动流确实没有任何 event_id 时才退化为时间戳。
		if ev := firstNonEmpty(strings.TrimSpace(payload.EventID), originalEventID); ev != "" {
			payload.ClientMsgID = "agentapi_stream_" + ev
		} else {
			payload.ClientMsgID = fmt.Sprintf("agentapi_stream_%d", time.Now().UnixNano())
		}
	}
	// 分片数量是观测指标而不是正确性条件。Kimi 等 Agent 的细粒度 thinking
	// 流可合法超过数千片；越过软阈值只告警，继续完整接收，且不改变 event 状态。
	trackerKey := strings.TrimSpace(payload.EventID)
	if trackerKey == "" {
		trackerKey = payload.ClientMsgID
	}
	if count, crossed := m.streamChunkTrackers.observe(trackerKey, payload.ChunkSeq); crossed {
		logger.L.Warnf(
			"agent api stream chunk count crossed soft threshold; continuing event=%s session=%s agent=%d client_msg_id=%s count=%d threshold=%d",
			strings.TrimSpace(payload.EventID),
			strings.TrimSpace(payload.SessionID),
			conn.agentID,
			strings.TrimSpace(payload.ClientMsgID),
			count,
			protocol.StreamChunkCountWarnThreshold,
		)
	}

	// 每个被接受的分片都是事件自身活动：刷新 selfTouchAt（空 event_id 时
	// TouchPendingEventResult 自动跳过），用于非终态观测和诊断。
	m.TouchPendingEventResult(payload.EventID)

	// NOTE: We intentionally do NOT finalize the tool execution accumulator here.
	// The accumulator is only reset when a new user message is dispatched to
	// the agent (see dispatchDelegateEventWithAttempt), so that all tool
	// executions within one user turn are merged into a single card regardless
	// of interleaved text output.

	if m.streamChunkFn == nil {
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			ClientMsgID: payload.ClientMsgID,
			Code:        protocol.CodeServerInternal,
			Msg:         "stream handler unavailable",
		})
		return
	}

	if err := m.streamChunkFn(context.Background(), conn.agentID, conn.ownerID, payload); err != nil {
		code := protocol.CodeServerInternal
		msg := "stream chunk failed"
		var sendErr *SendError
		if errors.As(err, &sendErr) {
			if sendErr.Code > 0 {
				code = sendErr.Code
			}
			if strings.TrimSpace(sendErr.Msg) != "" {
				msg = sendErr.Msg
			}
		}
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			ClientMsgID: payload.ClientMsgID,
			Code:        code,
			Msg:         msg,
		})
		return
	}

	if isNoReplyOutput {
		m.streamChunkTrackers.release(trackerKey)
		conn.sendPayload(protocol.CmdSendAck, pkt.Seq, map[string]any{
			"event_id":      firstNonEmpty(strings.TrimSpace(payload.EventID), originalEventID),
			"client_msg_id": payload.ClientMsgID,
			"no_reply":      true,
			"received_at":   time.Now().UnixMilli(),
		})
		return
	}
	if payload.IsFinish {
		m.streamChunkTrackers.release(trackerKey)
		conn.sendPayload("send_ack", pkt.Seq, protocol.SendAckPayload{
			SessionID:   payload.SessionID,
			ClientMsgID: payload.ClientMsgID,
			CreatedAt:   time.Now().UnixMilli(),
		})
	}
}

func (m *Manager) handleCodexEvent(conn *agentConn, pkt *protocol.Packet) {
	var payload CodexEventPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.sendPayload("error", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "invalid codex_event payload",
		})
		return
	}

	if strings.TrimSpace(payload.SessionID) == "" {
		return
	}

	method := payload.CodexMethod
	switch method {
	case "item/agentMessage/delta":
		m.handleCodexDelta(conn, pkt, &payload)
	case "item/completed":
		m.handleCodexItemCompleted(conn, pkt, &payload)
	case "turn/completed":
		m.handleCodexTurnCompleted(conn, pkt, &payload)
	case "item/permissions/requestApproval":
		m.handleCodexPermissionApproval(conn, pkt, &payload)
	case "item/commandExecution/requestApproval":
		m.handleCodexExecApproval(conn, pkt, &payload, "exec")
	case "item/fileChange/requestApproval":
		m.handleCodexExecApproval(conn, pkt, &payload, "file")
	default:
		// Acknowledge other events (token usage, thread status, etc.)
		m.refreshAgentLease(conn)
	}
}

func (m *Manager) handleCodexItemCompleted(conn *agentConn, pkt *protocol.Packet, cep *CodexEventPayload) {
	// Extract item from codex_payload
	var inner struct {
		Params struct {
			Item struct {
				Type             string `json:"type"`
				Command          string `json:"command"`
				AggregatedOutput string `json:"aggregatedOutput"`
				ExitCode         *int   `json:"exitCode"`
				DurationMs       *int64 `json:"durationMs"`
			} `json:"item"`
		} `json:"params"`
	}
	if err := json.Unmarshal(cep.CodexPayload, &inner); err != nil {
		return
	}

	if inner.Params.Item.Type != "commandExecution" {
		return
	}

	cmd := strings.TrimSpace(inner.Params.Item.Command)
	if cmd == "" {
		return
	}

	summary := cmd
	var detailParts []string
	if output := strings.TrimSpace(inner.Params.Item.AggregatedOutput); output != "" {
		detailParts = append(detailParts, output)
	}
	if inner.Params.Item.ExitCode != nil {
		detailParts = append(detailParts, fmt.Sprintf("exit code: %d", *inner.Params.Item.ExitCode))
	}
	if inner.Params.Item.DurationMs != nil {
		detailParts = append(detailParts, fmt.Sprintf("duration: %dms", *inner.Params.Item.DurationMs))
	}
	detail := strings.Join(detailParts, "\n")

	// Build send_msg with channel_data.grix.toolExecution → adapter converts to card
	extra := map[string]any{
		"channel_data": map[string]any{
			"grix": map[string]any{
				"toolExecution": map[string]any{
					"summary_text": summary,
					"detail_text":  detail,
				},
			},
		},
	}
	extraRaw, _ := json.Marshal(extra)

	sendPayload := SendMsgPayload{
		EventID:     cep.EventID,
		SessionID:   cep.SessionID,
		ThreadID:    cep.ThreadID,
		ClientMsgID: fmt.Sprintf("codex_tool_%d", cep.CodexSequence),
		MsgType:     1,
		Content:     summary,
		Extra:       extraRaw,
	}

	// Run through adapter normalization (converts toolExecution → grix://card link)
	if conn.adapter != nil {
		rawPayload, _ := json.Marshal(sendPayload)
		normalized, err := conn.adapter.NormalizeInbound(context.Background(), rawPayload)
		if err == nil && normalized != nil {
			sendPayload.Content = normalized.Content
			sendPayload.Extra = normalized.Extra
		}
	}

	if m.sendFn == nil {
		return
	}
	if m.shouldSuppressToolExecutionCards(sendPayload.EventID, sendPayload.SessionID, conn.ownerID) {
		return
	}

	var toolMeta toolExecPayloadMeta
	var isToolCard bool
	sendPayload.Content, sendPayload.Extra, toolMeta, isToolCard = compactToolExecutionPayload(
		sendPayload.Content,
		sendPayload.Extra,
	)
	if !isToolCard {
		return
	}
	accumResult := m.tryAccumulateToolExec(
		context.Background(),
		conn,
		sendPayload.SessionID,
		sendPayload.EventID,
		sendPayload.ClientMsgID,
		toolMeta,
	)
	if accumResult.handled {
		return
	}
	if accumResult.children != nil {
		sendPayload.Content = accumResult.modifiedContent
	}

	adapterKey := strings.TrimSpace(conn.adapterID)
	if adapterKey == "" {
		adapterKey = strings.TrimSpace(conn.clientType)
	}
	cardVisibleTo := ownerVisibleToForAdapterCard(adapterKey, sendPayload.Content, sendPayload.Extra, conn.ownerID)
	triggerVisibleTo := m.resolveTriggerVisibleTo(sendPayload.EventID, sendPayload.SessionID)
	visibleTo := mergeVisibleToForSendMsg(cardVisibleTo, triggerVisibleTo)

	result, err := m.sendFn(context.Background(), SendMessageReq{
		EventID:     sendPayload.EventID,
		AgentID:     conn.agentID,
		OwnerID:     conn.ownerID,
		SessionID:   sendPayload.SessionID,
		ThreadID:    sendPayload.ThreadID,
		ClientMsgID: sendPayload.ClientMsgID,
		MsgType:     sendPayload.MsgType,
		Content:     sendPayload.Content,
		Extra:       sendPayload.Extra,
		VisibleTo:   visibleTo,
	})
	if err != nil || result == nil {
		releaseToolExecDedup(context.Background(), accumResult.dedupKey)
		return
	}
	finishFirstToolExecAccum(
		context.Background(),
		conn.agentID,
		sendPayload.SessionID,
		accumResult,
		result.MsgID,
		visibleTo,
	)
}

func (m *Manager) handleCodexDelta(conn *agentConn, pkt *protocol.Packet, cep *CodexEventPayload) {
	// Extract delta text from codex_payload
	var inner struct {
		Params struct {
			Delta string `json:"delta"`
		} `json:"params"`
	}
	if err := json.Unmarshal(cep.CodexPayload, &inner); err != nil {
		return
	}
	delta := inner.Params.Delta
	if delta == "" {
		return
	}

	// Remap global codex_sequence to per-turn sequential counter starting at 1.
	// The stream infrastructure expects chunks with seq 1, 2, 3, ... but
	// codex_sequence is a global counter that may start at any value (e.g. 100).
	seqPtr, _ := m.codexChunkSeq.LoadOrStore(cep.EventID, new(int64))
	seq := atomic.AddInt64(seqPtr.(*int64), 1)
	quotedMessageID := m.resolveReplyQuotedMessageID(cep.EventID, cep.QuotedMessageID)

	chunk := AgentStreamChunkPayload{
		EventID:         cep.EventID,
		SessionID:       cep.SessionID,
		ThreadID:        cep.ThreadID,
		DeltaContent:    delta,
		ChunkSeq:        seq,
		IsFinish:        false,
		ClientMsgID:     fmt.Sprintf("codex_%s", cep.EventID),
		QuotedMessageID: quotedMessageID,
	}

	if m.streamChunkFn == nil {
		return
	}

	if err := m.streamChunkFn(context.Background(), conn.agentID, conn.ownerID, chunk); err != nil {
		conn.sendPayload("error", pkt.Seq, SendNackPayload{
			ClientMsgID: chunk.ClientMsgID,
			Code:        5001,
			Msg:         "codex stream chunk failed",
		})
	}
}

func (m *Manager) handleCodexTurnCompleted(conn *agentConn, pkt *protocol.Packet, cep *CodexEventPayload) {
	if m.streamChunkFn == nil {
		return
	}

	// Use next sequence after the last delta, then clean up the counter.
	var finishSeq int64 = 1
	if val, ok := m.codexChunkSeq.LoadAndDelete(cep.EventID); ok {
		finishSeq = *val.(*int64) + 1
	}
	quotedMessageID := m.resolveReplyQuotedMessageID(cep.EventID, cep.QuotedMessageID)

	chunk := AgentStreamChunkPayload{
		EventID:         cep.EventID,
		SessionID:       cep.SessionID,
		ThreadID:        cep.ThreadID,
		DeltaContent:    "",
		ChunkSeq:        finishSeq,
		IsFinish:        true,
		ClientMsgID:     fmt.Sprintf("codex_%s", cep.EventID),
		QuotedMessageID: quotedMessageID,
	}

	if err := m.streamChunkFn(context.Background(), conn.agentID, conn.ownerID, chunk); err != nil {
		conn.sendPayload("error", pkt.Seq, SendNackPayload{
			ClientMsgID: chunk.ClientMsgID,
			Code:        5001,
			Msg:         "codex stream finish failed",
		})
		return
	}

	conn.sendPayload("send_ack", pkt.Seq, protocol.SendAckPayload{
		SessionID:   cep.SessionID,
		ClientMsgID: chunk.ClientMsgID,
		CreatedAt:   time.Now().UnixMilli(),
	})
}

func (m *Manager) resolveReplyQuotedMessageID(eventID string, explicit int64) int64 {
	if explicit > 0 {
		return explicit
	}
	run := m.LookupActiveRun(eventID)
	if run == nil {
		return 0
	}
	if run.TriggerQuoted > 0 {
		return run.TriggerQuoted
	}
	if run.TriggerMsgID > 0 {
		return run.TriggerMsgID
	}
	return 0
}

func (m *Manager) handleCodexPermissionApproval(conn *agentConn, _ *protocol.Packet, cep *CodexEventPayload) {
	var inner struct {
		ID     json.RawMessage `json:"id"`
		Params struct {
			ThreadID    string          `json:"threadId"`
			TurnID      string          `json:"turnId"`
			ItemID      string          `json:"itemId"`
			Reason      string          `json:"reason"`
			Permissions json.RawMessage `json:"permissions"`
		} `json:"params"`
	}
	if err := json.Unmarshal(cep.CodexPayload, &inner); err != nil {
		return
	}

	var requestID string
	if inner.ID != nil {
		requestID = strings.Trim(string(inner.ID), `"`)
	}
	if requestID == "" {
		return
	}

	reason := strings.TrimSpace(inner.Params.Reason)
	if reason == "" {
		reason = "Codex requests additional permissions"
	}

	command := reason
	if perms := strings.TrimSpace(string(inner.Params.Permissions)); perms != "" && perms != "{}" && perms != "null" {
		command = fmt.Sprintf("%s\nPermissions: %s", reason, compactReplyText(perms, 200))
	}

	approvalPayload := map[string]any{
		"approval_id":         requestID,
		"approval_slug":       requestID,
		"approval_command_id": requestID,
		"command":             command,
		"host":                "Codex",
		"allowed_decisions":   []string{"allow-once", "deny"},
		"warning_text":        "Review the requested permissions before approving.",
	}

	card := buildExecApprovalCardMessage(approvalPayload)
	if card.content == "" {
		return
	}

	if m.sendFn == nil {
		return
	}

	// 托管代答场景审批卡改投主人私聊（口径见 handleSendMsg 的同款处理）。
	originSessionID := cep.SessionID
	cep.SessionID = resolveApprovalCardSessionID(context.Background(), cep.SessionID, conn.agentID, conn.ownerID)
	if cep.SessionID != originSessionID {
		cep.ThreadID = ""
	}

	adapterKey := strings.TrimSpace(conn.adapterID)
	if adapterKey == "" {
		adapterKey = strings.TrimSpace(conn.clientType)
	}
	cardVisibleTo := ownerVisibleToForAdapterCard(adapterKey, card.content, card.extra, conn.ownerID)
	triggerVisibleTo := m.resolveTriggerVisibleTo(cep.EventID, cep.SessionID)
	visibleTo := mergeVisibleToForSendMsg(cardVisibleTo, triggerVisibleTo)

	result, err := m.sendFn(context.Background(), SendMessageReq{
		EventID:     cep.EventID,
		AgentID:     conn.agentID,
		OwnerID:     conn.ownerID,
		SessionID:   cep.SessionID,
		ThreadID:    cep.ThreadID,
		ClientMsgID: fmt.Sprintf("codex_perm_approval_%s_%d", requestID, cep.CodexSequence),
		MsgType:     1,
		Content:     card.content,
		Extra:       card.extra,
		VisibleTo:   visibleTo,
	})
	if err == nil && result != nil && result.MsgID > 0 {
		saveApprovalCardMsgIDWithType(context.Background(), conn.agentID, cep.SessionID, requestID, result.MsgID, "permission")
	}
}

func (m *Manager) handleCodexExecApproval(conn *agentConn, _ *protocol.Packet, cep *CodexEventPayload, kind string) {
	var inner struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if err := json.Unmarshal(cep.CodexPayload, &inner); err != nil {
		return
	}

	var requestID string
	if inner.ID != nil {
		requestID = strings.Trim(string(inner.ID), `"`)
	}
	if requestID == "" {
		return
	}

	method := inner.Method
	if method == "" {
		method = cep.CodexMethod
	}

	// Extract params as map for codex adapter utilities
	var rawParams struct {
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(cep.CodexPayload, &rawParams); err != nil {
		return
	}
	paramsMap := make(map[string]any)
	if len(rawParams.Params) > 0 {
		_ = json.Unmarshal(rawParams.Params, &paramsMap)
	}

	approvalCommandID := strings.TrimSpace(requestID)
	approvalID := codexadapter.BuildApprovalID(cep.SessionID, approvalCommandID)
	command := codexadapter.SummarizeCodexApprovalRequest(method, paramsMap)
	if command == "" {
		return
	}
	allowedDecisions := codexadapter.NormalizeCodexApprovalDecisions(method, paramsMap)

	approvalPayload := map[string]any{
		"approval_id":         approvalID,
		"approval_slug":       approvalCommandID,
		"approval_command_id": approvalCommandID,
		"command":             command,
		"host":                "Codex",
		"allowed_decisions":   allowedDecisions,
	}
	if warningText := codexadapter.WarningTextForCodexApproval(method); warningText != "" {
		approvalPayload["warning_text"] = warningText
	}

	card := buildExecApprovalCardMessage(approvalPayload)
	if card.content == "" {
		return
	}

	if m.sendFn == nil {
		return
	}

	// 托管代答场景审批卡改投主人私聊（口径见 handleSendMsg 的同款处理）。
	originSessionID := cep.SessionID
	cep.SessionID = resolveApprovalCardSessionID(context.Background(), cep.SessionID, conn.agentID, conn.ownerID)
	if cep.SessionID != originSessionID {
		cep.ThreadID = ""
	}

	adapterKey := strings.TrimSpace(conn.adapterID)
	if adapterKey == "" {
		adapterKey = strings.TrimSpace(conn.clientType)
	}
	cardVisibleTo := ownerVisibleToForAdapterCard(adapterKey, card.content, card.extra, conn.ownerID)
	triggerVisibleTo := m.resolveTriggerVisibleTo(cep.EventID, cep.SessionID)
	visibleTo := mergeVisibleToForSendMsg(cardVisibleTo, triggerVisibleTo)

	result, err := m.sendFn(context.Background(), SendMessageReq{
		EventID:     cep.EventID,
		AgentID:     conn.agentID,
		OwnerID:     conn.ownerID,
		SessionID:   cep.SessionID,
		ThreadID:    cep.ThreadID,
		ClientMsgID: fmt.Sprintf("codex_%s_approval_%s_%d", kind, approvalCommandID, cep.CodexSequence),
		MsgType:     1,
		Content:     card.content,
		Extra:       card.extra,
		VisibleTo:   visibleTo,
	})
	if err == nil && result != nil && result.MsgID > 0 {
		saveApprovalCardMsgIDWithType(context.Background(), conn.agentID, cep.SessionID, approvalCommandID, result.MsgID, kind)
	}
}

func (m *Manager) handleSessionActivitySet(conn *agentConn, pkt *protocol.Packet) {
	var payload protocol.SessionActivitySetPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "invalid session_activity_set payload",
		})
		return
	}
	if strings.TrimSpace(payload.SessionID) == "" {
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "session_id required",
		})
		return
	}
	switch strings.TrimSpace(payload.Kind) {
	case protocol.SessionActivityKindComposing:
		// already normalized
	case "typing":
		payload.Kind = protocol.SessionActivityKindComposing
	default:
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "unsupported session activity kind",
		})
		return
	}
	if m.activityFn == nil {
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 5001,
			Msg:  "session activity handler unavailable",
		})
		return
	}
	if err := m.activityFn(context.Background(), conn.agentID, conn.ownerID, payload); err != nil {
		code := 5001
		msg := "session activity update failed"
		var sendErr *SendError
		if errors.As(err, &sendErr) {
			if sendErr.Code > 0 {
				code = sendErr.Code
			}
			if strings.TrimSpace(sendErr.Msg) != "" {
				msg = sendErr.Msg
			}
		}
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: code,
			Msg:  msg,
		})
		return
	}
	if payload.Active {
		m.TouchPendingEventResult(payload.RefEventID)
	}
}

func (m *Manager) handleDeleteMsg(conn *agentConn, pkt *protocol.Packet) {
	var payload DeleteMsgPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "invalid delete_msg payload",
		})
		return
	}
	if strings.TrimSpace(payload.SessionID) == "" || payload.MsgID <= 0 {
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "session_id and msg_id required",
		})
		return
	}
	if m.deleteMsgFn == nil {
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 5001,
			Msg:  "delete handler unavailable",
		})
		return
	}

	if err := m.deleteMsgFn(context.Background(), conn.agentID, conn.ownerID, payload); err != nil {
		code := 5001
		msg := "delete message failed"
		var sendErr *SendError
		if errors.As(err, &sendErr) {
			if sendErr.Code > 0 {
				code = sendErr.Code
			}
			if strings.TrimSpace(sendErr.Msg) != "" {
				msg = sendErr.Msg
			}
		}
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: code,
			Msg:  msg,
		})
		return
	}

	conn.sendPayload("send_ack", pkt.Seq, map[string]interface{}{
		"msg_id":     fmt.Sprintf("%d", payload.MsgID),
		"session_id": payload.SessionID,
		"deleted":    true,
	})
}

func (m *Manager) handleUpdateBindingCard(conn *agentConn, pkt *protocol.Packet) {
	var payload struct {
		SessionID    string         `json:"session_id"`
		WorkerStatus string         `json:"worker_status"`
		Cwd          string         `json:"cwd"`
		Meta         map[string]any `json:"meta"`
	}
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "invalid update_binding_card payload",
		})
		return
	}
	if strings.TrimSpace(payload.SessionID) == "" || strings.TrimSpace(payload.WorkerStatus) == "" {
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "session_id and worker_status required",
		})
		return
	}

	sessionID := strings.TrimSpace(payload.SessionID)
	cwd := strings.TrimSpace(payload.Cwd)
	workerStatus := strings.TrimSpace(payload.WorkerStatus)

	// Persist toolbar binding so the agent toolbar becomes visible.
	m.persistBindingFromCard(conn, sessionID, cwd, workerStatus, payload.Meta)
	if svc := agenttoolbar.GetGlobal(); svc != nil && conn.ownerID > 0 {
		_ = svc.RefreshSession(context.Background(), conn.ownerID, sessionID, "binding_update")
	}
	// Hermes uses this command to publish immutable runtime metadata (for example,
	// the configured model) into the toolbar binding. It did not previously have
	// binding-card support, so keep this path metadata-only and avoid creating a
	// visible chat message as a side effect.
	if isHermesConn(conn) {
		conn.sendPayload(protocol.CmdSendAck, pkt.Seq, map[string]interface{}{
			"session_id":    sessionID,
			"updated":       true,
			"metadata_only": true,
		})
		return
	}

	msgID := loadBindingCardMsgID(context.Background(), conn.agentID, sessionID)
	if msgID <= 0 {
		// No existing binding card message — send a new one.
		m.sendNewBindingCard(conn, pkt, sessionID, cwd, workerStatus)
		return
	}
	if m.editMsgFn == nil {
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 5001,
			Msg:  "edit handler unavailable",
		})
		return
	}

	summary, cardStatus := bindingCardSummary(ownerCardLanguage(conn.ownerID), cwd, workerStatus)

	cardPayload := map[string]any{
		"category": "session",
		"status":   cardStatus,
		"summary":  summary,
	}
	content := buildLocalGrixCardLink(
		fmt.Sprintf("[Agent Status] %s", compactReplyText(summary, 180)),
		"agent_status",
		cardPayload,
	)

	if err := m.editMsgFn(context.Background(), conn.agentID, conn.ownerID, EditMsgPayload{
		SessionID: sessionID,
		MsgID:     msgID,
		Content:   content,
	}); err != nil {
		code := 5001
		msg := "update binding card failed"
		var sendErr *SendError
		if errors.As(err, &sendErr) {
			if sendErr.Code > 0 {
				code = sendErr.Code
			}
			if strings.TrimSpace(sendErr.Msg) != "" {
				msg = sendErr.Msg
			}
		}
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: code,
			Msg:  msg,
		})
		return
	}

	// Renew the 48h TTL so the mapping stays alive as long as the
	// connector keeps pushing binding-card updates. Without this, the
	// Redis key expires and the next update creates a new chat message
	// (with an unread notification).
	saveBindingCardMsgID(context.Background(), conn.agentID, sessionID, msgID)

	conn.sendPayload("send_ack", pkt.Seq, map[string]interface{}{
		"msg_id":     fmt.Sprintf("%d", msgID),
		"session_id": sessionID,
		"updated":    true,
	})
}

// bindingCardSummary builds the one-line summary for a session binding card.
// 只保留一句结论；关联 ID、Workspace、Worker 等技术细节不再下发。
func bindingCardSummary(lang, cwd, workerStatus string) (summary, cardStatus string) {
	if workerStatus == "session_expired" {
		return tooli18n.T(lang, "session_expired"), "error"
	}
	if cwd != "" {
		return tooli18n.Tf(lang, "bound_path", cwd), "success"
	}
	return tooli18n.T(lang, "bound_ok"), "success"
}

// persistBindingFromCard upserts a toolbar binding record from the update_binding_card payload,
// making the agent toolbar visible for the session.
func (m *Manager) persistBindingFromCard(conn *agentConn, sessionID, cwd, workerStatus string, meta map[string]any) {
	if conn == nil || conn.agentID <= 0 || sessionID == "" {
		return
	}
	ctx := context.Background()
	record, _, _ := toolstore.LoadBinding(ctx, conn.agentID, sessionID)
	record.AgentID = conn.agentID
	record.SessionID = sessionID
	record.ProviderKey = firstNonEmpty(record.ProviderKey, normalizeToolbarProviderKey(conn))
	if cwd != "" {
		record.Cwd = cwd
	}
	if workerStatus != "" {
		record.WorkerStatus = workerStatus
	}
	record.Meta = mergeToolbarMeta(record.Meta, meta)
	if err := toolstore.UpsertBinding(ctx, record); err != nil {
		logger.L.Warnf("persist binding from update_binding_card failed agent=%d session=%s err=%v", conn.agentID, sessionID, err)
	}
}

// sendNewBindingCard creates and sends a new binding card message when no existing one is found.
func (m *Manager) sendNewBindingCard(conn *agentConn, pkt *protocol.Packet, sessionID, cwd, workerStatus string) {
	if m.sendFn == nil {
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 5001,
			Msg:  "send handler unavailable",
		})
		return
	}

	summary, cardStatus := bindingCardSummary(ownerCardLanguage(conn.ownerID), cwd, workerStatus)

	cardPayload := map[string]any{
		"category": "session",
		"status":   cardStatus,
		"summary":  summary,
	}
	content := buildLocalGrixCardLink(
		fmt.Sprintf("[Agent Status] %s", compactReplyText(summary, 180)),
		"agent_status",
		cardPayload,
	)

	clientMsgID := fmt.Sprintf("binding_card_%d_%s", conn.agentID, sessionID)
	result, err := m.sendFn(context.Background(), SendMessageReq{
		AgentID:     conn.agentID,
		OwnerID:     conn.ownerID,
		SessionID:   sessionID,
		ClientMsgID: clientMsgID,
		MsgType:     1,
		Content:     content,
	})
	if err != nil {
		logger.L.Warnf("send new binding card failed agent=%d session=%s err=%v", conn.agentID, sessionID, err)
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 5001,
			Msg:  "failed to send binding card",
		})
		return
	}

	sentMsgID := int64(0)
	if result != nil {
		sentMsgID = result.MsgID
		if sentMsgID > 0 {
			saveBindingCardMsgID(context.Background(), conn.agentID, sessionID, sentMsgID)
		}
	}

	conn.sendPayload("send_ack", pkt.Seq, map[string]interface{}{
		"msg_id":     fmt.Sprintf("%d", sentMsgID),
		"session_id": sessionID,
		"updated":    false,
	})
}

func (m *Manager) handleEditMsg(conn *agentConn, pkt *protocol.Packet) {
	var payload EditMsgPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "invalid edit_msg payload",
		})
		return
	}
	if strings.TrimSpace(payload.SessionID) == "" || payload.MsgID <= 0 || strings.TrimSpace(payload.Content) == "" {
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "session_id, msg_id, and content required",
		})
		return
	}
	if m.editMsgFn == nil {
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 5001,
			Msg:  "edit handler unavailable",
		})
		return
	}

	if err := m.editMsgFn(context.Background(), conn.agentID, conn.ownerID, payload); err != nil {
		code := 5001
		msg := "edit message failed"
		var sendErr *SendError
		if errors.As(err, &sendErr) {
			if sendErr.Code > 0 {
				code = sendErr.Code
			}
			if strings.TrimSpace(sendErr.Msg) != "" {
				msg = sendErr.Msg
			}
		}
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: code,
			Msg:  msg,
		})
		return
	}

	conn.sendPayload("send_ack", pkt.Seq, map[string]interface{}{
		"msg_id":     fmt.Sprintf("%d", payload.MsgID),
		"session_id": payload.SessionID,
		"edited":     true,
	})
}

func (m *Manager) handleReactMsg(conn *agentConn, pkt *protocol.Packet) {
	var payload ReactMsgPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "invalid react_msg payload",
		})
		return
	}
	if strings.TrimSpace(payload.SessionID) == "" || payload.MsgID <= 0 || strings.TrimSpace(payload.Emoji) == "" {
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "session_id, msg_id, and emoji required",
		})
		return
	}
	switch strings.TrimSpace(payload.Op) {
	case "", "add":
		payload.Op = "add"
	case "remove":
		// already normalized
	default:
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "op must be add or remove",
		})
		return
	}
	if m.reactMsgFn == nil {
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 5001,
			Msg:  "react handler unavailable",
		})
		return
	}

	if err := m.reactMsgFn(context.Background(), conn.agentID, conn.ownerID, payload); err != nil {
		code := 5001
		msg := "react failed"
		var sendErr *SendError
		if errors.As(err, &sendErr) {
			if sendErr.Code > 0 {
				code = sendErr.Code
			}
			if strings.TrimSpace(sendErr.Msg) != "" {
				msg = sendErr.Msg
			}
		}
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: code,
			Msg:  msg,
		})
		return
	}

	conn.sendPayload("send_ack", pkt.Seq, map[string]interface{}{
		"msg_id":     fmt.Sprintf("%d", payload.MsgID),
		"session_id": payload.SessionID,
		"emoji":      payload.Emoji,
		"op":         payload.Op,
	})
}

func (m *Manager) handleMediaUploadInit(conn *agentConn, pkt *protocol.Packet) {
	var payload MediaUploadInitPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "invalid media_upload_init payload",
		})
		return
	}
	payload.UploadID = strings.TrimSpace(payload.UploadID)
	payload.Name = strings.TrimSpace(payload.Name)
	payload.Mime = strings.TrimSpace(payload.Mime)
	payload.Purpose = strings.TrimSpace(payload.Purpose)
	if payload.UploadID == "" {
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "upload_id required",
		})
		return
	}
	if payload.Name == "" {
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "name required",
		})
		return
	}
	if payload.SizeBytes < 0 {
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "size_bytes must be non-negative",
		})
		return
	}
	if m.mediaUploadInitFn == nil {
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 5001,
			Msg:  "media upload init handler unavailable",
		})
		return
	}
	if err := checkAgentScope(conn.agentID, agentscope.ScopeMediaUpload); err != nil {
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 4003,
			Msg:  err.Error(),
		})
		return
	}

	result, err := m.mediaUploadInitFn(context.Background(), conn.agentID, conn.ownerID, payload)
	if err != nil {
		code := 5001
		msg := "media upload init failed"
		var sendErr *SendError
		if errors.As(err, &sendErr) {
			if sendErr.Code > 0 {
				code = sendErr.Code
			}
			if strings.TrimSpace(sendErr.Msg) != "" {
				msg = sendErr.Msg
			}
		}
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: code,
			Msg:  msg,
		})
		return
	}

	if result == nil {
		result = &MediaUploadInitResult{}
	}
	if strings.TrimSpace(result.UploadID) == "" {
		result.UploadID = payload.UploadID
	}
	if strings.TrimSpace(result.Method) == "" {
		result.Method = "PUT"
	}

	conn.sendPayload("send_ack", pkt.Seq, result)
}

func (m *Manager) handleSendMsg(conn *agentConn, pkt *protocol.Packet) {
	var payload SendMsgPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "invalid send_msg payload",
		})
		return
	}
	if conn != nil && conn.adapter != nil {
		normalized, err := conn.adapter.NormalizeInbound(context.Background(), pkt.Payload)
		if err != nil {
			logger.L.Warnf("adapter NormalizeInbound failed agent=%d adapter=%s err=%v, falling back to raw send_msg", conn.agentID, conn.adapterID, err)
		} else if normalized != nil && normalized.Drop {
			conn.sendPayload("send_ack", pkt.Seq, map[string]any{
				"client_msg_id": payload.ClientMsgID,
			})
			return
		} else if normalized != nil {
			payload.SessionID = normalized.SessionID
			if strings.TrimSpace(normalized.ThreadID) != "" {
				payload.ThreadID = normalized.ThreadID
			}
			payload.Content = normalized.Content
			payload.Extra = normalized.Extra
		}
	}
	payload.Extra = MergeMediaURLIntoExtra(payload.Extra, payload.MediaURL)
	var toolMeta toolExecPayloadMeta
	payload.Content, payload.Extra, toolMeta, _ = compactToolExecutionPayload(payload.Content, payload.Extra)
	if strings.TrimSpace(payload.SessionID) == "" || strings.TrimSpace(payload.Content) == "" {
		conn.recordViolation()
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			ClientMsgID: payload.ClientMsgID,
			Code:        protocol.CodeInvalidPayload,
			Msg:         "session_id/content required",
		})
		return
	}
	// Use the same late-output fallback as client_stream_chunk. Event tracking
	// can disappear because of a restart or retention expiry while the
	// connector still has valid output; session authorization is sufficient
	// to accept that information without trusting a foreign live event.
	originalEventID := strings.TrimSpace(payload.EventID)
	authorization, guardErr := m.authorizeInboundOutput(
		context.Background(), conn, originalEventID, payload.SessionID,
	)
	if guardErr != nil {
		conn.recordViolation()
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			ClientMsgID: payload.ClientMsgID,
			Code:        guardErr.Code,
			Msg:         guardErr.Msg,
		})
		return
	}
	if authorization.AbsorbTerminal {
		conn.sendPayload(protocol.CmdSendAck, pkt.Seq, map[string]any{
			"event_id":          originalEventID,
			"client_msg_id":     payload.ClientMsgID,
			"terminal_absorbed": true,
			"received_at":       time.Now().UnixMilli(),
		})
		return
	}
	resolvedEventID := authorization.EventID
	if originalEventID != "" && resolvedEventID == "" {
		logger.L.Warnf(
			"send_msg: event_id not found, accepting via authorized session route: event_id=%s session_id=%s agent=%d owner=%d client_msg_id=%s",
			originalEventID, payload.SessionID, conn.agentID, conn.ownerID, strings.TrimSpace(payload.ClientMsgID),
		)
	}
	payload.EventID = resolvedEventID
	if ShouldSilentlyAckInboundOutput(payload.Content, m.IsNoReplyProtocolContext(firstNonEmpty(resolvedEventID, originalEventID))) {
		if originalEventID == "" {
			if sessionErr := m.ensureSessionWritableBy(
				context.Background(), conn.agentID, conn.ownerID, payload.SessionID,
			); sessionErr != nil {
				conn.recordViolation()
				conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
					ClientMsgID: payload.ClientMsgID,
					Code:        sessionErr.Code,
					Msg:         sessionErr.Msg,
				})
				return
			}
		}
		conn.sendPayload(protocol.CmdSendAck, pkt.Seq, protocol.SendAckPayload{
			SessionID:   payload.SessionID,
			ClientMsgID: payload.ClientMsgID,
			CreatedAt:   time.Now().UnixMilli(),
		})
		return
	}
	// Phase 1.2: send_msg 大小硬上限。
	if utf8.RuneCountInString(payload.Content) > protocol.MaxContentChars {
		conn.recordViolation()
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			ClientMsgID: payload.ClientMsgID,
			Code:        protocol.CodePayloadTooLarge,
			Msg:         "content too large",
		})
		return
	}
	if len(payload.Extra) > protocol.MaxExtraBytes {
		conn.recordViolation()
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			ClientMsgID: payload.ClientMsgID,
			Code:        protocol.CodePayloadTooLarge,
			Msg:         "extra too large",
		})
		return
	}
	// 单条消息的投递结果不等于整个 Agent 任务的终态。只要 event 归属校验已经
	// 通过，这次尝试本身就是事件活动，应刷新观测时间；真正终态由明确的
	// event_result 决定。后续即使返回 send_nack，也允许 Agent 修正/重试或继续输出。
	m.TouchPendingEventResult(payload.EventID)
	if payload.MsgType <= 0 {
		payload.MsgType = 1
	}
	if strings.TrimSpace(payload.ClientMsgID) == "" {
		payload.ClientMsgID = fmt.Sprintf("agentapi_%d", time.Now().UnixNano())
	}
	isToolExecutionCardPayload := isToolExecutionCard(payload.Content)
	if isToolExecutionCardPayload && m.shouldSuppressToolExecutionCards(payload.EventID, payload.SessionID, conn.ownerID) {
		conn.sendPayload("send_ack", pkt.Seq, protocol.SendAckPayload{
			SessionID:   payload.SessionID,
			ClientMsgID: payload.ClientMsgID,
			CreatedAt:   time.Now().UnixMilli(),
		})
		return
	}
	// A connector may legitimately mix fine-grained stream chunks with a
	// final send_msg or an interaction/tool card. Persist every independently
	// valid message; stream history is context, not a rejection condition.
	if m.sendFn == nil {
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			ClientMsgID: payload.ClientMsgID,
			Code:        5001,
			Msg:         "message handler unavailable",
		})
		return
	}

	// 托管代答形态（1:1 会话里除主人外还有其他真人成员，如客服/访客会话）下，
	// 审批卡必须改投「主人↔agent」私聊：visible_to 只在群聊分发层强制，1:1 会话
	// 不过滤，落进原会话客户就能看到并代为批准（越权批准安全隐患）。与 agent
	// 类型无关，凡 exec_approval 卡统一处理。ThreadID/QuotedMessageID 属于原
	// 会话语境，改投后一并丢弃。
	approvalCardID := extractApprovalIDFromCard(payload.Content, payload.Extra)
	originSessionID := payload.SessionID
	if approvalCardID != "" {
		payload.SessionID = resolveApprovalCardSessionID(context.Background(), payload.SessionID, conn.agentID, conn.ownerID)
		if payload.SessionID != originSessionID {
			payload.ThreadID = ""
			payload.QuotedMessageID = 0
		}
	}

	adapterKey := strings.TrimSpace(conn.adapterID)
	if adapterKey == "" {
		adapterKey = strings.TrimSpace(conn.clientType)
	}

	// If this is a binding card and one already exists for this session,
	// edit the existing card in-place instead of creating a new message.
	isBindingCard := isOwnerVisibilityCard(payload.Content, payload.Extra) &&
		strings.Contains(payload.Content, "grix://card/agent_open_session")
	if isBindingCard {
		existingMsgID := loadBindingCardMsgID(context.Background(), conn.agentID, payload.SessionID)
		if existingMsgID > 0 && m.editMsgFn != nil {
			editErr := m.editMsgFn(context.Background(), conn.agentID, conn.ownerID, EditMsgPayload{
				SessionID: payload.SessionID,
				MsgID:     existingMsgID,
				Content:   payload.Content,
				Extra:     payload.Extra,
			})
			if editErr == nil {
				conn.sendPayload("send_ack", pkt.Seq, protocol.SendAckPayload{
					SessionID:   payload.SessionID,
					MsgID:       existingMsgID,
					ClientMsgID: payload.ClientMsgID,
					CreatedAt:   time.Now().UnixMilli(),
				})
				return
			}
			logger.L.Warnf("edit existing binding card failed, creating new message: agent=%d session=%s msg_id=%d err=%v",
				conn.agentID, payload.SessionID, existingMsgID, editErr)
		}
	}

	// Tool execution accumulator: aggregate consecutive tool_execution
	// cards into a single message using edit-in-place updates.
	accumResult := m.tryAccumulateToolExec(
		context.Background(),
		conn,
		payload.SessionID,
		payload.EventID,
		payload.ClientMsgID,
		toolMeta,
	)
	if accumResult.handled {
		conn.sendPayload("send_ack", pkt.Seq, protocol.SendAckPayload{
			SessionID:   payload.SessionID,
			MsgID:       accumResult.msgID,
			ClientMsgID: payload.ClientMsgID,
			CreatedAt:   time.Now().UnixMilli(),
		})
		return
	}
	if accumResult.children != nil {
		payload.Content = accumResult.modifiedContent
	}
	if strings.Contains(payload.Content, "grix://card/agent_open_session") {
		payload.Content, _ = ensureOpenSessionCardInstanceID(
			payload.Content,
			buildOpenSessionCardInstanceID(
				conn.agentID,
				payload.SessionID,
				firstNonEmpty(strings.TrimSpace(payload.EventID), strings.TrimSpace(payload.ClientMsgID)),
				payload.QuotedMessageID,
			),
		)
	}

	cardVisibleTo := ownerVisibleToForAdapterCard(adapterKey, payload.Content, payload.Extra, conn.ownerID)
	triggerVisibleTo := m.resolveTriggerVisibleTo(payload.EventID, payload.SessionID)
	visibleTo := mergeVisibleToForSendMsg(cardVisibleTo, triggerVisibleTo)
	result, err := m.sendFn(context.Background(), SendMessageReq{
		EventID:         payload.EventID,
		AgentID:         conn.agentID,
		OwnerID:         conn.ownerID,
		SessionID:       payload.SessionID,
		ThreadID:        payload.ThreadID,
		ClientMsgID:     payload.ClientMsgID,
		MsgType:         payload.MsgType,
		Content:         payload.Content,
		Extra:           payload.Extra,
		VisibleTo:       visibleTo,
		MediaURL:        payload.MediaURL,
		QuotedMessageID: payload.QuotedMessageID,
	})
	if err != nil {
		releaseToolExecDedup(context.Background(), accumResult.dedupKey)
		code := 5001
		msg := "send message failed"
		var sendErr *SendError
		if errors.As(err, &sendErr) {
			if sendErr.Code > 0 {
				code = sendErr.Code
			}
			if strings.TrimSpace(sendErr.Msg) != "" {
				msg = sendErr.Msg
			}
		}
		trimmed := strings.TrimSpace(payload.Content)
		logger.L.Warnf(
			"agentapi send_msg failed: agent=%d owner=%d session=%s event_id=%s client_msg_id=%s code=%d msg=%s content_bytes=%d content_runes=%d err=%v",
			conn.agentID,
			conn.ownerID,
			payload.SessionID,
			payload.EventID,
			payload.ClientMsgID,
			code,
			msg,
			len(trimmed),
			utf8.RuneCountInString(trimmed),
			err,
		)
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			ClientMsgID: payload.ClientMsgID,
			Code:        code,
			Msg:         msg,
		})
		return
	}
	conn.sendPayload("send_ack", pkt.Seq, protocol.SendAckPayload{
		SessionID:   payload.SessionID,
		MsgID:       result.MsgID,
		ClientMsgID: payload.ClientMsgID,
		InboxSeq:    result.InboxSeq,
		CreatedAt:   result.CreatedAt,
	})

	// Track the msg_id of agent_open_session binding cards so we can
	// edit them in-place when the session_control result arrives.
	if result.MsgID > 0 && isOwnerVisibilityCard(payload.Content, payload.Extra) &&
		strings.Contains(payload.Content, "grix://card/agent_open_session") {
		saveBindingCardMsgID(context.Background(), conn.agentID, payload.SessionID, result.MsgID)
	}

	// Track the msg_id of exec_approval cards so we can edit them in-place
	// when the approval result arrives.
	if result.MsgID > 0 {
		if approvalCardID != "" {
			// 卡片索引按实际投递会话登记（托管场景已被改投到主人私聊），
			// 审批回传在同一会话里查找时才能命中。
			saveApprovalCardMsgID(context.Background(), conn.agentID, payload.SessionID, approvalCardID, result.MsgID)
			// Reset tool execution accumulator so post-approval tool
			// executions start a fresh batch instead of merging with
			// pre-approval cards. 累积器/流都挂在原会话（agent 的工作会话），
			// 与审批卡改投后的会话无关。
			deleteToolExecAccum(context.Background(), conn.agentID, originSessionID)
			// Force-finalize active streaming sessions so post-approval
			// chunks start a new message instead of appending to the
			// pre-approval stream.
			if m.forceFinalizeStreamsFn != nil {
				m.forceFinalizeStreamsFn(context.Background(), conn.agentID, conn.ownerID, originSessionID)
			}
			// Notify the owner so they can approve/deny/stop offline.
			m.publishApprovalNotification(conn.ownerID, conn.agentID, payload.SessionID, payload.Content, approvalCardID)
		} else if questionID, ok := extractQuestionFromCard(payload.Content, payload.Extra); ok {
			// Agent explicitly declared a question_card.
			m.publishQuestionNotification(conn.ownerID, conn.agentID, payload.SessionID, payload.Content, questionID, result.MsgID)
		}
		// Track agent_question 提问卡消息号，供回卡结果按 request_id 原地编辑
		// 卡片（记录成功/过期），否则卡片会永远停在提交中。
		if requestID := extractAgentQuestionRequestID(payload.Content); requestID != "" {
			saveApprovalCardMsgID(context.Background(), conn.agentID, payload.SessionID, requestID, result.MsgID)
		}
	}

	// Save tool execution accumulator state for the first tool_execution
	// in a sequence, so subsequent ones can edit this message in-place.
	if accumResult.children != nil {
		finishFirstToolExecAccum(
			context.Background(),
			conn.agentID,
			payload.SessionID,
			accumResult,
			result.MsgID,
			visibleTo,
		)
	}
}
