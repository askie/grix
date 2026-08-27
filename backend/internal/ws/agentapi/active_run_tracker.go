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
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/agentstream"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

type activeAgentRun struct {
	EventID             string
	TerminalCommitToken string
	SessionID           string
	ThreadID            string
	Scope               string
	SessionType         int16
	SenderID            int64
	OwnerID             int64
	AgentID             int64
	TriggerMsgID        int64
	TriggerQuoted       int64
	StreamMsgID         int64
	ClientStream        bool
	State               string
	CanStop             bool
	StopRequested       bool
	StopReason          string
	TriggerVisibleTo    []int64
	VisibleOutput       bool
	StartedAt           int64
	RunGeneration       int64
	// CallTurn marks runs triggered while the sender was in a voice call.
	// Call turns are conversation, not tasks — they never produce task_*
	// notifications (captured at registration so late results after hangup
	// stay silent too).
	CallTurn  bool
	UpdatedAt int64
}

type ActiveRunSnapshot struct {
	EventID             string
	TerminalCommitToken string
	SessionID           string
	ThreadID            string
	Scope               string
	SessionType         int16
	SenderID            int64
	OwnerID             int64
	AgentID             int64
	TriggerMsgID        int64
	TriggerQuoted       int64
	StreamMsgID         int64
	ClientStream        bool
	State               string
	CanStop             bool
	StopReason          string
	TriggerVisibleTo    []int64
	StartedAt           int64
	RunGeneration       int64
	CallTurn            bool
	UpdatedAt           int64
}

func activeRunSessionOwnerKey(sessionID string, ownerID int64) string {
	return fmt.Sprintf("%s:%d", strings.TrimSpace(sessionID), ownerID)
}

func (m *Manager) emitOutputStatus(run *activeAgentRun) {
	if run == nil {
		return
	}
	snapshot := *run
	if snapshot.UpdatedAt <= 0 {
		snapshot.UpdatedAt = time.Now().UnixMilli()
	}
	revision := nextAgentStatusRevision("output", snapshot.EventID)

	m.mu.RLock()
	fn := m.outputStatusFn
	m.mu.RUnlock()
	if fn == nil {
		return
	}

	fn(protocol.AgentOutputStatusPayload{
		RunID:              snapshot.EventID,
		SessionID:          snapshot.SessionID,
		DispatchGeneration: snapshot.RunGeneration,
		Revision:           revision,
		Scope:              snapshot.Scope,
		OwnerID:            snapshot.OwnerID,
		AgentID:            snapshot.AgentID,
		TriggerMsgID:       snapshot.TriggerMsgID,
		StreamMsgID:        snapshot.StreamMsgID,
		State:              snapshot.State,
		CanStop:            snapshot.CanStop,
		StopReason:         snapshot.StopReason,
		UpdatedAt:          snapshot.UpdatedAt,
	})
}

func snapshotActiveRun(run *activeAgentRun) *ActiveRunSnapshot {
	if run == nil {
		return nil
	}
	return &ActiveRunSnapshot{
		EventID:             run.EventID,
		TerminalCommitToken: run.TerminalCommitToken,
		SessionID:           run.SessionID,
		ThreadID:            run.ThreadID,
		Scope:               run.Scope,
		SessionType:         run.SessionType,
		SenderID:            run.SenderID,
		OwnerID:             run.OwnerID,
		AgentID:             run.AgentID,
		TriggerMsgID:        run.TriggerMsgID,
		TriggerQuoted:       run.TriggerQuoted,
		StreamMsgID:         run.StreamMsgID,
		ClientStream:        run.ClientStream,
		State:               run.State,
		CanStop:             run.CanStop,
		StopReason:          run.StopReason,
		TriggerVisibleTo:    run.TriggerVisibleTo,
		StartedAt:           run.StartedAt,
		RunGeneration:       run.RunGeneration,
		CallTurn:            run.CallTurn,
		UpdatedAt:           run.UpdatedAt,
	}
}

func (m *Manager) LookupActiveRunBySessionOwner(ownerID int64, sessionID string) *ActiveRunSnapshot {
	sessionID = strings.TrimSpace(sessionID)
	if ownerID <= 0 || sessionID == "" {
		return nil
	}
	// Queue-aware connectors are authoritative for whether the session still
	// has work. An empty queue_snapshot sets this marker before terminal
	// event_result handling, so stale in-memory pointers must not resurrect a
	// completed run in toolbars or other session-level projections.
	if IsSessionQueueIdle(context.Background(), ownerID, sessionID) {
		return nil
	}

	// 队列快照镜像优先：连接器是队列权威，running 列表指明真正在跑的事件；
	// runBySX 指针只反映"最新注册"，有排队时会指向队尾而非运行中的 run。
	if snap := m.resolveRunFromQueueMirror(ownerID, sessionID); snap != nil {
		return snap
	}

	m.runsMu.Lock()
	defer m.runsMu.Unlock()

	eventID := strings.TrimSpace(m.runBySX[activeRunSessionOwnerKey(sessionID, ownerID)])
	if eventID == "" {
		return nil
	}
	run := m.runs[eventID]
	if run == nil {
		delete(m.runBySX, activeRunSessionOwnerKey(sessionID, ownerID))
		return nil
	}
	cp := *run
	return snapshotActiveRun(&cp)
}

func (m *Manager) LookupActiveRun(eventID string) *ActiveRunSnapshot {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return nil
	}

	m.runsMu.Lock()
	defer m.runsMu.Unlock()

	run := m.runs[eventID]
	if run == nil {
		return nil
	}
	cp := *run
	return snapshotActiveRun(&cp)
}

func (m *Manager) LookupActiveRunStatusBySessionOwner(
	ownerID int64,
	sessionID string,
) (protocol.AgentOutputStatusPayload, bool) {
	snapshot := m.LookupActiveRunBySessionOwner(ownerID, sessionID)
	if snapshot == nil {
		return protocol.AgentOutputStatusPayload{}, false
	}
	return protocol.AgentOutputStatusPayload{
		RunID:              snapshot.EventID,
		SessionID:          snapshot.SessionID,
		DispatchGeneration: snapshot.RunGeneration,
		Revision:           nextAgentStatusRevision("output", snapshot.EventID),
		Scope:              snapshot.Scope,
		OwnerID:            snapshot.OwnerID,
		AgentID:            snapshot.AgentID,
		TriggerMsgID:       snapshot.TriggerMsgID,
		StreamMsgID:        snapshot.StreamMsgID,
		State:              snapshot.State,
		CanStop:            snapshot.CanStop,
		StopReason:         snapshot.StopReason,
		UpdatedAt:          snapshot.UpdatedAt,
	}, true
}

func (m *Manager) registerActiveRun(evt DelegateEventPayload) {
	m.registerActiveRunInternal(
		evt,
		true,
		time.Now().UTC(),
		senderInVoiceCall(evt.SenderID),
	)
}

func (m *Manager) registerActiveRunForDispatch(
	evt DelegateEventPayload,
	startedAt time.Time,
	callTurn bool,
) {
	var generation int64
	m.acksMu.Lock()
	if pending := m.pending[strings.TrimSpace(evt.EventID)]; pending != nil {
		generation = pending.dispatchGeneration
	}
	m.acksMu.Unlock()
	m.registerActiveRunInternal(evt, false, startedAt, callTurn, generation)
}

func (m *Manager) registerActiveRunInternal(
	evt DelegateEventPayload,
	persistRunning bool,
	startedAt time.Time,
	callTurn bool,
	runGeneration ...int64,
) {
	eventID := strings.TrimSpace(evt.EventID)
	if eventID == "" || evt.OwnerID <= 0 || strings.TrimSpace(evt.SessionID) == "" {
		return
	}

	sessionID := strings.TrimSpace(evt.SessionID)
	// New work is starting: allow composing again even if the previous
	// queue_snapshot left the session marked idle.
	clearSessionQueueIdle(context.Background(), evt.OwnerID, sessionID)
	scope := resolveDelegateEventScope(evt)
	triggerVisibleTo := loadTriggerVisibleTo(evt.MsgID, sessionID)
	m.rememberOutboundVisibility(evt.AgentID, evt.OwnerID, sessionID, evt.SessionType, triggerVisibleTo)
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	now := startedAt.UnixMilli()
	var generation int64
	if len(runGeneration) > 0 {
		generation = runGeneration[0]
	}
	var run *activeAgentRun
	shouldEmit := false
	m.runsMu.Lock()
	delete(m.stopFenceUntil, eventID)
	if existing := m.runs[eventID]; existing != nil {
		run = existing
		if run.TerminalCommitToken == "" {
			run.TerminalCommitToken = strings.TrimSpace(evt.TerminalCommitToken)
		}
		if run.ThreadID == "" {
			run.ThreadID = strings.TrimSpace(evt.ThreadID)
		}
		m.runBySX[activeRunSessionOwnerKey(existing.SessionID, existing.OwnerID)] = eventID
	} else {
		run = &activeAgentRun{
			EventID:             eventID,
			TerminalCommitToken: strings.TrimSpace(evt.TerminalCommitToken),
			SessionID:           sessionID,
			ThreadID:            strings.TrimSpace(evt.ThreadID),
			Scope:               scope,
			SessionType:         evt.SessionType,
			SenderID:            evt.SenderID,
			OwnerID:             evt.OwnerID,
			AgentID:             evt.AgentID,
			TriggerMsgID:        evt.MsgID,
			TriggerQuoted:       evt.QuotedMessageID,
			TriggerVisibleTo:    triggerVisibleTo,
			State:               protocol.AgentOutputStateQueued,
			CanStop:             true,
			StartedAt:           now,
			RunGeneration:       generation,
			CallTurn:            callTurn,
			UpdatedAt:           now,
		}
		m.runs[eventID] = run
		m.runBySX[activeRunSessionOwnerKey(run.SessionID, run.OwnerID)] = eventID
		shouldEmit = true
	}
	m.runsMu.Unlock()
	if shouldEmit {
		m.emitOutputStatus(run)
		if persistRunning {
			m.persistActiveRunRunning(eventID)
		}
	}
}

// persistActiveRunRunning starts only after the event packet is accepted by
// the connection send queue. A failed initial channel write is requeued for
// reconnect and must not leave a ghost DB "running" row behind.
func (m *Manager) persistActiveRunRunning(eventID string) {
	run := m.LookupActiveRun(eventID)
	if run == nil || !taskStateEligible(&activeAgentRun{
		EventID:  run.EventID,
		OwnerID:  run.OwnerID,
		SenderID: run.SenderID,
		CallTurn: run.CallTurn,
	}) {
		return
	}
	startedAt := time.UnixMilli(run.StartedAt).UTC()
	m.goBackground(func() {
		store.UpsertSessionAgentStateRunningWithGeneration(
			run.SessionID,
			run.OwnerID,
			run.AgentID,
			run.EventID,
			startedAt,
			run.RunGeneration,
		)
	})
}

// isLLMProxyEvent reports the classic "AI 代主人应答对端" private-chat shape:
// session_type=1 and the trigger sender is not the agent owner. Kept for call
// sites that only care about that shape; tool-card suppression uses the broader
// shouldSuppressToolExecutionCards.
func (m *Manager) isLLMProxyEvent(eventID string) bool {
	sessionType, senderID, ownerID, ok := m.lookupEventParticipants(eventID)
	if !ok {
		return false
	}
	return sessionType == model.SessionTypeDirect && senderID != ownerID
}

// shouldSuppressToolExecutionCards decides whether inbound tool_execution cards
// must be ack'd and dropped instead of written to the chat bubble feed.
//
// Matches the outbound connector.tool_events=drop contract:
//   - private hosted / LLM-proxy (session_type=1, sender != owner)
//   - all group sessions (session_type=2; applyGroupConnectorRuntimeConfig)
//
// Direct owner↔agent chats (sender == owner) keep tool cards visible.
//
// sessionID/ownerID are a late-output fallback: when event tracking has expired
// (empty or stale event_id) and lookup fails, an active im:delegate:* for this
// owner still means the agent is mid hosted-reply and tool cards stay internal.
func (m *Manager) shouldSuppressToolExecutionCards(eventID, sessionID string, ownerID int64) bool {
	if sessionType, senderID, eventOwnerID, ok := m.lookupEventParticipants(eventID); ok {
		if suppressToolExecutionCardsForParticipants(sessionType, senderID, eventOwnerID) {
			return true
		}
		if m.pendingEventDropsToolEvents(eventID) {
			return true
		}
		return false
	}
	// Event unknown/expired (empty or stale id): if text-delegate is still active
	// on this session, treat inbound tool cards as hosted-reply noise.
	return sessionHasActiveTextDelegate(sessionID, ownerID)
}

func suppressToolExecutionCardsForParticipants(sessionType int16, senderID, ownerID int64) bool {
	if sessionType == model.SessionTypeGroup {
		return true
	}
	return sessionType == model.SessionTypeDirect && senderID != ownerID
}

func (m *Manager) lookupEventParticipants(eventID string) (sessionType int16, senderID, ownerID int64, ok bool) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return 0, 0, 0, false
	}
	if run := m.LookupActiveRun(eventID); run != nil {
		return run.SessionType, run.SenderID, run.OwnerID, true
	}
	m.acksMu.Lock()
	entry, found := m.pending[eventID]
	m.acksMu.Unlock()
	if found && entry != nil {
		evt := entry.event
		return evt.SessionType, evt.SenderID, evt.OwnerID, true
	}
	return 0, 0, 0, false
}

func (m *Manager) pendingEventDropsToolEvents(eventID string) bool {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return false
	}
	m.acksMu.Lock()
	entry, ok := m.pending[eventID]
	m.acksMu.Unlock()
	if !ok || entry == nil {
		return false
	}
	return connectorToolEventsDrop(entry.event.Extra)
}

func connectorToolEventsDrop(extra json.RawMessage) bool {
	if len(extra) == 0 {
		return false
	}
	var envelope struct {
		Connector struct {
			ToolEvents string `json:"tool_events"`
		} `json:"connector"`
	}
	if err := json.Unmarshal(extra, &envelope); err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(envelope.Connector.ToolEvents), "drop")
}

func sessionHasActiveTextDelegate(sessionID string, ownerID int64) bool {
	sessionID = strings.TrimSpace(sessionID)
	if store.RDB == nil || sessionID == "" || ownerID <= 0 {
		return false
	}
	key := fmt.Sprintf("im:delegate:%s:%d", sessionID, ownerID)
	agentID, err := store.RDB.HGet(context.Background(), key, "agent_id").Result()
	if err != nil {
		return false
	}
	return strings.TrimSpace(agentID) != ""
}

func (m *Manager) updateRunState(eventID string, fn func(run *activeAgentRun) bool) *activeAgentRun {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return nil
	}

	m.runsMu.Lock()
	defer m.runsMu.Unlock()

	run := m.runs[eventID]
	if run == nil {
		return nil
	}
	if fn != nil && !fn(run) {
		return nil
	}
	run.UpdatedAt = time.Now().UnixMilli()
	cp := *run
	return &cp
}

func (m *Manager) rememberStopFenceLocked(eventID string) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return
	}
	ttl := m.stopFenceTTL
	if ttl <= 0 {
		ttl = agentstream.DefaultStoppedFenceTTL
	}
	if m.stopFenceUntil == nil {
		m.stopFenceUntil = make(map[string]int64)
	}
	m.stopFenceUntil[eventID] = time.Now().Add(ttl).UnixMilli()
}

func (m *Manager) removeRun(eventID string) *activeAgentRun {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return nil
	}

	m.runsMu.Lock()
	defer m.runsMu.Unlock()

	run := m.runs[eventID]
	if run == nil {
		return nil
	}
	delete(m.runs, eventID)
	m.codexChunkSeq.Delete(eventID)
	m.piChunkSeq.Delete(eventID)
	piThinkingKey := eventID + "_thinking"
	m.piChunkSeq.Delete(piThinkingKey)
	m.piThinkingBuf.Delete(piThinkingKey)
	if key := activeRunSessionOwnerKey(run.SessionID, run.OwnerID); m.runBySX[key] == eventID {
		delete(m.runBySX, key)
	}
	cp := *run
	return &cp
}

// settleRun applies a terminal state and removes the run under one lock. A
// terminal transition is absorbing: concurrent/duplicate connector results
// find no run and therefore cannot emit a second, conflicting output status.
func (m *Manager) settleRun(eventID, state, stopReason string) (*activeAgentRun, bool) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return nil, false
	}

	m.runsMu.Lock()
	defer m.runsMu.Unlock()

	run := m.runs[eventID]
	if run == nil {
		return nil, false
	}
	run.State = state
	run.CanStop = false
	run.StopReason = strings.TrimSpace(stopReason)
	run.UpdatedAt = time.Now().UnixMilli()

	delete(m.runs, eventID)
	m.codexChunkSeq.Delete(eventID)
	m.piChunkSeq.Delete(eventID)
	piThinkingKey := eventID + "_thinking"
	m.piChunkSeq.Delete(piThinkingKey)
	m.piThinkingBuf.Delete(piThinkingKey)
	if key := activeRunSessionOwnerKey(run.SessionID, run.OwnerID); m.runBySX[key] == eventID {
		delete(m.runBySX, key)
	}
	cp := *run
	lastForSessionOwner := true
	for _, active := range m.runs {
		if active != nil && active.SessionID == run.SessionID && active.OwnerID == run.OwnerID {
			lastForSessionOwner = false
			break
		}
	}
	return &cp, lastForSessionOwner
}

func (m *Manager) MarkRunReceived(eventID string) {
	var prevState string
	run := m.updateRunState(eventID, func(run *activeAgentRun) bool {
		prevState = run.State
		// Delivery ACK is weaker evidence than visible output or a stop request.
		// Reconnect replay and independent connector writers can make a stream
		// packet arrive before event_ack, so received must never move an already
		// streaming/stopping run backwards.
		if run.StopRequested ||
			run.State == protocol.AgentOutputStateStreaming ||
			run.State == protocol.AgentOutputStateStopping {
			return false
		}
		run.State = protocol.AgentOutputStateReceived
		run.CanStop = true
		return true
	})
	if run != nil {
		logger.L.Infof(
			"agent_output_status mark received run_id=%s session=%s owner=%d agent=%d state=%s->%s can_stop=%t stream_msg_id=%d visible_output=%t",
			run.EventID,
			run.SessionID,
			run.OwnerID,
			run.AgentID,
			strings.TrimSpace(prevState),
			run.State,
			run.CanStop,
			run.StreamMsgID,
			run.VisibleOutput,
		)
	}
	m.emitOutputStatus(run)
}

func (m *Manager) MarkRunStreaming(eventID string, streamMsgID int64) {
	m.markRunStreaming(eventID, streamMsgID, false)
}

func (m *Manager) MarkRunClientStreamStarted(eventID string, streamMsgID int64) {
	m.markRunStreaming(eventID, streamMsgID, true)
}

func (m *Manager) markRunStreaming(eventID string, streamMsgID int64, clientStream bool) {
	var (
		prevState        string
		prevStreamMsgID  int64
		prevVisible      bool
		prevClientStream bool
	)
	run := m.updateRunState(eventID, func(run *activeAgentRun) bool {
		prevState = run.State
		prevStreamMsgID = run.StreamMsgID
		prevVisible = run.VisibleOutput
		prevClientStream = run.ClientStream
		if streamMsgID > 0 && run.StreamMsgID == 0 {
			run.StreamMsgID = streamMsgID
		}
		if clientStream {
			run.ClientStream = true
		}
		run.VisibleOutput = true
		if !run.StopRequested {
			run.State = protocol.AgentOutputStateStreaming
			run.CanStop = true
		}
		return true
	})
	if run != nil &&
		(prevStreamMsgID != run.StreamMsgID ||
			prevState != run.State ||
			prevVisible != run.VisibleOutput ||
			prevClientStream != run.ClientStream) {
		logger.L.Infof(
			"agent_output_status mark streaming run_id=%s session=%s owner=%d agent=%d state=%s->%s stream_msg_id=%d->%d client_stream=%t->%t visible_output=%t->%t stop_requested=%t can_stop=%t",
			run.EventID,
			run.SessionID,
			run.OwnerID,
			run.AgentID,
			strings.TrimSpace(prevState),
			run.State,
			prevStreamMsgID,
			run.StreamMsgID,
			prevClientStream,
			run.ClientStream,
			prevVisible,
			run.VisibleOutput,
			run.StopRequested,
			run.CanStop,
		)
	}
	m.emitOutputStatus(run)
	// 回复消息落库 ack / 流开始都属于事件自身活动：刷新 pending 的
	// selfTouchAt，供非终态观测使用。
	if run != nil {
		m.TouchPendingEventResult(eventID)
	}
}

func (m *Manager) MarkRunCompleted(eventID string) error {
	return m.markRunCompleted(eventID, true, true, nil)
}

func (m *Manager) markRunCompleted(
	eventID string,
	persist bool,
	notificationAllowed bool,
	effectFence func() error,
) error {
	run, lastForSessionOwner := m.settleRun(eventID, protocol.AgentOutputStateCompleted, "")
	allowNotification := notificationAllowed
	if persist && run != nil && lastForSessionOwner && taskStateEligible(run) {
		changed, err := store.SettleSessionAgentStateByRun(model.SessionAgentState{
			SessionID:   run.SessionID,
			OwnerID:     run.OwnerID,
			AgentID:     run.AgentID,
			State:       model.SessionAgentStateCompleted,
			LastRunID:   run.EventID,
			StartedAt:   timePtrFromMillis(run.StartedAt),
			CompletedAt: nowPtr(),
		})
		if err != nil {
			return err
		}
		allowNotification = changed
	}
	if run != nil {
		logger.L.Infof(
			"agent_output_status mark completed run_id=%s session=%s owner=%d agent=%d stream_msg_id=%d visible_output=%t stop_requested=%t stop_reason=%s",
			run.EventID,
			run.SessionID,
			run.OwnerID,
			run.AgentID,
			run.StreamMsgID,
			run.VisibleOutput,
			run.StopRequested,
			strings.TrimSpace(run.StopReason),
		)
	}
	m.emitOutputStatus(run)
	// 任务完成的严格定义：该会话的处理队列清零才算完成。还有同会话
	// 在途 run 时保持沉默，最后一条完成时只推一次。
	if run != nil && lastForSessionOwner && allowNotification {
		if effectFence != nil {
			if err := effectFence(); err != nil {
				return err
			}
		}
		return publishTaskNotification(run, notification.EventTaskCompleted, "", !persist)
	}
	return nil
}

// hasActiveRunForSessionOwner reports whether any in-flight run remains for
// the session+owner pair.
func (m *Manager) hasActiveRunForSessionOwner(sessionID string, ownerID int64) bool {
	m.runsMu.Lock()
	defer m.runsMu.Unlock()
	for _, r := range m.runs {
		if r != nil && r.SessionID == sessionID && r.OwnerID == ownerID {
			return true
		}
	}
	return false
}

// ShouldClearComposingForTerminal reports whether a terminal frame belongs to
// the last active run for this exact session/owner/agent. A delayed terminal
// frame from an older run must not clear a newer run's composing indicator.
func (m *Manager) ShouldClearComposingForTerminal(
	runID string,
	sessionID string,
	ownerID int64,
	agentID int64,
) bool {
	runID = strings.TrimSpace(runID)
	sessionID = strings.TrimSpace(sessionID)
	if runID == "" || sessionID == "" || ownerID <= 0 || agentID <= 0 {
		return false
	}
	m.runsMu.Lock()
	for _, run := range m.runs {
		if run == nil || run.EventID == runID {
			continue
		}
		if run.SessionID == sessionID && run.OwnerID == ownerID && run.AgentID == agentID {
			m.runsMu.Unlock()
			return false
		}
	}
	m.runsMu.Unlock()
	if newer, err := store.HasNewerPendingAgentEventDispatch(
		runID,
		sessionID,
		ownerID,
		agentID,
	); err != nil {
		// A failed global check must be conservative: clearing a live newer
		// run is worse than leaving composing visible until its next tick.
		logger.L.Warnf(
			"terminal composing fence lookup failed run=%s session=%s owner=%d agent=%d err=%v",
			runID, sessionID, ownerID, agentID, err,
		)
		return false
	} else if newer {
		return false
	}
	return !hasOtherDurableActiveRun(
		context.Background(),
		runID,
		sessionID,
		ownerID,
		agentID,
	)
}

// MarkRunStreamDone marks a run's current stream as finished without removing
// the run. The run stays active so that subsequent turns within the same
// delegate event can update it. The run is removed when event_result arrives
// and calls MarkRunCompleted/MarkRunFailed/MarkRunStopped.
func (m *Manager) MarkRunStreamDone(eventID string) {
	run := m.updateRunState(eventID, func(run *activeAgentRun) bool {
		if run.StopRequested {
			return false
		}
		run.VisibleOutput = false
		return true
	})
	if run != nil {
		logger.L.Infof(
			"agent_output_status mark stream_done run_id=%s session=%s owner=%d agent=%d state=%s can_stop=%t stream_msg_id=%d",
			run.EventID,
			run.SessionID,
			run.OwnerID,
			run.AgentID,
			run.State,
			run.CanStop,
			run.StreamMsgID,
		)
	}
}

func (m *Manager) MarkRunFailed(eventID string, stopReason string) error {
	return m.markRunFailed(eventID, stopReason, stopReason, true, true, nil)
}

// MarkRunFailedNotify is MarkRunFailed with the notification reason decoupled
// from the stored stop reason: stopReason stays the verbatim text surfaced in
// output_status / chat_states, while notifyReason carries the protocol code
// the push copy layer localizes. Use when stopReason is free text.
func (m *Manager) MarkRunFailedNotify(eventID, stopReason, notifyReason string) error {
	return m.markRunFailed(eventID, stopReason, notifyReason, true, true, nil)
}

func (m *Manager) markRunFailed(
	eventID string,
	stopReason string,
	notifyReason string,
	persist bool,
	notificationAllowed bool,
	effectFence func() error,
) error {
	run, lastForSessionOwner := m.settleRun(eventID, protocol.AgentOutputStateFailed, stopReason)
	allowNotification := notificationAllowed
	if persist && run != nil && lastForSessionOwner && taskStateEligible(run) {
		state := model.SessionAgentStateFailed
		if isUserInitiatedStopReason(stopReason) {
			state = model.SessionAgentStateIdle
		}
		changed, err := store.SettleSessionAgentStateByRun(model.SessionAgentState{
			SessionID:   run.SessionID,
			OwnerID:     run.OwnerID,
			AgentID:     run.AgentID,
			State:       state,
			LastRunID:   run.EventID,
			StopReason:  strings.TrimSpace(stopReason),
			StartedAt:   timePtrFromMillis(run.StartedAt),
			CompletedAt: nowPtr(),
		})
		if err != nil {
			return err
		}
		allowNotification = changed
	}
	if run != nil {
		logger.L.Infof(
			"agent_output_status mark failed run_id=%s session=%s owner=%d agent=%d stream_msg_id=%d visible_output=%t stop_requested=%t stop_reason=%s",
			run.EventID,
			run.SessionID,
			run.OwnerID,
			run.AgentID,
			run.StreamMsgID,
			run.VisibleOutput,
			run.StopRequested,
			strings.TrimSpace(run.StopReason),
		)
	}
	m.emitOutputStatus(run)
	// User-initiated interruptions (stop button, hangup) are not failures, and
	// timeout/cleanup verdicts push only when they plausibly reflect a live
	// failure (shouldNotifyTaskFailed). Eligibility is checked first so
	// ineligible runs never cost the last-agent-message lookup.
	if allowNotification && taskNotificationEligible(run) &&
		!isUserInitiatedStopReason(stopReason) &&
		shouldNotifyTaskFailed(run, notifyReason) {
		if effectFence != nil {
			if err := effectFence(); err != nil {
				return err
			}
		}
		return publishTaskNotification(
			run,
			notification.EventTaskFailed,
			taskFailedSummary(notifyReason),
			!persist,
		)
	}
	return nil
}

func (m *Manager) MarkRunStopped(eventID string, stopReason string) error {
	return m.markRunStopped(eventID, stopReason, true, true, nil)
}

func (m *Manager) markRunStopped(
	eventID string,
	stopReason string,
	persist bool,
	notificationAllowed bool,
	effectFence func() error,
) error {
	run, lastForSessionOwner := m.settleRun(eventID, protocol.AgentOutputStateStopped, stopReason)
	allowNotification := notificationAllowed
	if persist && run != nil && lastForSessionOwner && taskStateEligible(run) {
		changed, err := store.SettleSessionAgentStateByRun(model.SessionAgentState{
			SessionID:   run.SessionID,
			OwnerID:     run.OwnerID,
			AgentID:     run.AgentID,
			State:       model.SessionAgentStateIdle,
			LastRunID:   run.EventID,
			StopReason:  strings.TrimSpace(run.StopReason),
			StartedAt:   timePtrFromMillis(run.StartedAt),
			CompletedAt: nowPtr(),
		})
		if err != nil {
			return err
		}
		allowNotification = changed
	}
	if run != nil {
		logger.L.Infof(
			"agent_output_status mark stopped run_id=%s session=%s owner=%d agent=%d stream_msg_id=%d visible_output=%t stop_requested=%t stop_reason=%s",
			run.EventID,
			run.SessionID,
			run.OwnerID,
			run.AgentID,
			run.StreamMsgID,
			run.VisibleOutput,
			run.StopRequested,
			strings.TrimSpace(run.StopReason),
		)
	}
	m.emitOutputStatus(run)
	// Only notify when the stop was not user-initiated.
	if run != nil && allowNotification && !isUserInitiatedStopReason(run.StopReason) {
		if effectFence != nil {
			if err := effectFence(); err != nil {
				return err
			}
		}
		return publishTaskNotification(
			run,
			notification.EventTaskStoppedUnexpected,
			"",
			!persist,
		)
	}
	return nil
}

func timePtrFromMillis(value int64) *time.Time {
	if value <= 0 {
		return nil
	}
	resolved := time.UnixMilli(value).UTC()
	return &resolved
}

// MarkRunStopFailed rolls back only the stop-request state. A failed stop
// command says the connector may still be executing the event, so the run
// stays active and later output/event_result remains authoritative.
func (m *Manager) MarkRunStopFailed(eventID string, reason string) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return
	}

	m.runsMu.Lock()
	run := m.runs[eventID]
	if run == nil {
		m.runsMu.Unlock()
		return
	}
	run.StopRequested = false
	run.CanStop = true
	run.StopReason = strings.TrimSpace(reason)
	if run.VisibleOutput {
		run.State = protocol.AgentOutputStateStreaming
	} else {
		run.State = protocol.AgentOutputStateReceived
	}
	run.UpdatedAt = time.Now().UnixMilli()
	delete(m.stopFenceUntil, eventID)
	cp := *run
	m.runsMu.Unlock()

	logger.L.Warnf(
		"agent_output_stop failed; run remains active run_id=%s session=%s owner=%d agent=%d state=%s reason=%s",
		cp.EventID, cp.SessionID, cp.OwnerID, cp.AgentID, cp.State, strings.TrimSpace(reason),
	)
	m.emitOutputStatus(&cp)
	m.TouchPendingEventResult(eventID)
}

func (m *Manager) ShouldFenceEventReply(eventID string) bool {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return false
	}
	m.runsMu.Lock()
	defer m.runsMu.Unlock()
	if run := m.runs[eventID]; run != nil && run.StopRequested {
		return true
	}
	if until := m.stopFenceUntil[eventID]; until > time.Now().UnixMilli() {
		return true
	}
	delete(m.stopFenceUntil, eventID)
	return false
}

func (m *Manager) RequestOutputStop(ownerID int64, sessionID string, eventID string) (protocol.AgentOutputStopAckPayload, *ActiveRunSnapshot, error) {
	ack := protocol.AgentOutputStopAckPayload{
		SessionID: strings.TrimSpace(sessionID),
		RunID:     strings.TrimSpace(eventID),
		Accepted:  false,
		UpdatedAt: time.Now().UnixMilli(),
	}
	if ownerID <= 0 || ack.SessionID == "" {
		ack.Msg = "invalid stop target"
		logger.L.Warnf(
			"agent_output_stop reject invalid target owner=%d session=%s run_id=%s",
			ownerID,
			ack.SessionID,
			ack.RunID,
		)
		return ack, nil, errors.New("invalid stop target")
	}
	if IsSessionQueueIdle(context.Background(), ownerID, ack.SessionID) {
		ack.Msg = "active output not found"
		logger.L.Infof(
			"agent_output_stop reject authoritative empty queue owner=%d session=%s requested_run=%s",
			ownerID,
			ack.SessionID,
			ack.RunID,
		)
		return ack, nil, errors.New("active output not found")
	}

	// 未指明 run 时优先按队列快照镜像解析"正在运行"的事件（连接器上报的
	// 队列权威），避免 runBySX"最新注册"指针在有排队时把停止打到队尾。
	// 镜像解析涉及 Redis 读取，必须在 runsMu 之外完成。
	if ack.RunID == "" {
		if snap := m.resolveRunFromQueueMirror(ownerID, ack.SessionID); snap != nil {
			ack.RunID = snap.EventID
		}
	}

	m.runsMu.Lock()
	if ack.RunID == "" {
		ack.RunID = strings.TrimSpace(m.runBySX[activeRunSessionOwnerKey(ack.SessionID, ownerID)])
	}
	run := m.runs[ack.RunID]
	if run == nil || run.OwnerID != ownerID || run.SessionID != ack.SessionID {
		m.runsMu.Unlock()
		// 本节点无 in-memory run：跨节点/重启场景下从 durable 只读恢复，
		// 由上层 DispatchOutputStop 跨副本把 event_stop 转发到 agent 所在节点。
		// run 权威留在 agent 节点，这里不重建本地 run。
		if snap := m.lookupDurableRunByEvent(ack.RunID, ownerID, ack.SessionID); snap != nil {
			ack.Accepted = true
			ack.RunID = snap.EventID
			ack.StopID = fmt.Sprintf("stop_%d", time.Now().UnixNano())
			m.markCrossNodeStopPending(ownerID, ack.SessionID, snap.EventID)
			logger.L.Infof(
				"agent_output_stop accept cross-node durable owner=%d session=%s run_id=%s agent=%d",
				ownerID, ack.SessionID, snap.EventID, snap.AgentID,
			)
			return ack, snap, nil
		}
		ack.Msg = "active output not found"
		logger.L.Warnf(
			"agent_output_stop reject active output not found owner=%d session=%s requested_run=%s resolved_run=%s",
			ownerID,
			ack.SessionID,
			strings.TrimSpace(eventID),
			ack.RunID,
		)
		return ack, nil, errors.New("active output not found")
	}
	if run.StopRequested || !run.CanStop {
		m.rememberStopFenceLocked(run.EventID)
		runCopy := *run
		m.runsMu.Unlock()
		ack.Accepted = true
		ack.RunID = runCopy.EventID
		ack.StopID = fmt.Sprintf("stop_%d", time.Now().UnixNano())
		logger.L.Infof(
			"agent_output_stop accept existing-stop owner=%d session=%s run_id=%s state=%s can_stop=%t stream_msg_id=%d visible_output=%t",
			ownerID,
			ack.SessionID,
			runCopy.EventID,
			runCopy.State,
			runCopy.CanStop,
			runCopy.StreamMsgID,
			runCopy.VisibleOutput,
		)
		return ack, snapshotActiveRun(&runCopy), nil
	}

	run.StopRequested = true
	m.rememberStopFenceLocked(run.EventID)
	run.State = protocol.AgentOutputStateStopping
	run.CanStop = false
	run.StopReason = "owner_requested_stop"
	run.UpdatedAt = time.Now().UnixMilli()
	runCopy := *run
	m.runsMu.Unlock()

	ack.RunID = runCopy.EventID
	ack.StopID = fmt.Sprintf("stop_%d", time.Now().UnixNano())
	ack.Accepted = true
	logger.L.Infof(
		"agent_output_stop accept owner=%d session=%s run_id=%s state=%s stream_msg_id=%d visible_output=%t trigger_msg_id=%d agent=%d",
		ownerID,
		ack.SessionID,
		runCopy.EventID,
		runCopy.State,
		runCopy.StreamMsgID,
		runCopy.VisibleOutput,
		runCopy.TriggerMsgID,
		runCopy.AgentID,
	)
	m.emitOutputStatus(&runCopy)
	return ack, snapshotActiveRun(&runCopy), nil
}

func (m *Manager) DispatchOutputStop(ack protocol.AgentOutputStopAckPayload, run *ActiveRunSnapshot) error {
	if run == nil {
		logger.L.Warnf(
			"agent_output_stop dispatch skipped session=%s run_id=%s: active run required",
			strings.TrimSpace(ack.SessionID),
			strings.TrimSpace(ack.RunID),
		)
		return errors.New("active run required")
	}
	logger.L.Infof(
		"agent_output_stop dispatch event_stop owner=%d session=%s run_id=%s agent=%d stop_id=%s stream_msg_id=%d",
		run.OwnerID,
		run.SessionID,
		run.EventID,
		run.AgentID,
		strings.TrimSpace(ack.StopID),
		run.StreamMsgID,
	)
	// 走统一收口 dispatchAgentCommand：本节点直发，否则跨副本转发到 agent 所在节点。
	// 按发起者 owner(=run.OwnerID) 精确路由,避免 B 的 stop 落到主人 A 的 connector。
	if !m.dispatchAgentCommand(run.AgentID, run.OwnerID, protocol.CmdEventStop, protocol.AgentEventStopPayload{
		StopID:              ack.StopID,
		EventID:             run.EventID,
		TerminalCommitToken: strings.TrimSpace(run.TerminalCommitToken),
		SessionID:           run.SessionID,
		Scope:               run.Scope,
		OwnerID:             run.OwnerID,
		AgentID:             run.AgentID,
		TriggerMsgID:        run.TriggerMsgID,
		StreamMsgID:         run.StreamMsgID,
		Reason:              "owner_requested_stop",
		RequestedAt:         ack.UpdatedAt,
	}) {
		logger.L.Warnf(
			"agent_output_stop dispatch failed owner=%d session=%s run_id=%s agent=%d stop_id=%s: agent channel unavailable",
			run.OwnerID,
			run.SessionID,
			run.EventID,
			run.AgentID,
			strings.TrimSpace(ack.StopID),
		)
		m.clearCrossNodeStopPending(run.OwnerID, run.SessionID, run.EventID)
		m.MarkRunStopFailed(run.EventID, protocol.AgentDeliveryCodeChannelUnavailable)
		return errors.New("agent channel unavailable")
	}
	return nil
}

func loadTriggerVisibleTo(msgID int64, sessionID string) []int64 {
	if msgID <= 0 || store.DB == nil {
		return nil
	}
	var msg model.Message
	if err := store.DB.Select("sender_id, visible_to").
		Where("msg_id = ? AND session_id = ?", msgID, sessionID).
		Find(&msg).Error; err != nil || msg.VisibleTo == nil {
		return nil
	}
	var ids []int64
	if json.Unmarshal(msg.VisibleTo, &ids) == nil && len(ids) > 0 && msg.SenderID > 0 {
		// The trigger was a hidden message; the reply should go back to
		// the sender, not inherit the same audience list.
		return []int64{msg.SenderID}
	}
	return nil
}

// DispatchComposingStop sends an event_stop to the agent without a specific
// active run. Used when the agent has composing activity but no tracked run.
// 走统一收口 dispatchAgentCommand：本节点直发，否则跨副本转发到 agent 所在节点。
// payload.OwnerID 是发起者(被共享者或主人),用于按 owner 精确路由(agent 共享多连接物理隔离)。
func (m *Manager) DispatchComposingStop(agentID int64, payload protocol.AgentEventStopPayload) bool {
	return m.dispatchAgentCommand(agentID, payload.OwnerID, protocol.CmdEventStop, payload)
}

const crossNodeStopPendingTTL = 120 * time.Second

func crossNodeStopKey(ownerID int64, sessionID, eventID string) string {
	return fmt.Sprintf("%s:%s", activeRunSessionOwnerKey(strings.TrimSpace(sessionID), ownerID), strings.TrimSpace(eventID))
}

// markCrossNodeStopPending 在跨节点停止被接受后记录标记，使本节点的 LoadRunState
// 能把 durable 快照状态覆盖为 "stopping"，立即推出工具栏快照，清除前端 loading。
func (m *Manager) markCrossNodeStopPending(ownerID int64, sessionID, eventID string) {
	sessionID = strings.TrimSpace(sessionID)
	eventID = strings.TrimSpace(eventID)
	if ownerID <= 0 || sessionID == "" || eventID == "" {
		return
	}
	key := crossNodeStopKey(ownerID, sessionID, eventID)
	m.runsMu.Lock()
	if m.crossNodeStopUntil == nil {
		m.crossNodeStopUntil = make(map[string]int64)
	}
	m.crossNodeStopUntil[key] = time.Now().Add(crossNodeStopPendingTTL).UnixMilli()
	m.runsMu.Unlock()
}

func (m *Manager) clearCrossNodeStopPending(ownerID int64, sessionID, eventID string) {
	sessionID = strings.TrimSpace(sessionID)
	eventID = strings.TrimSpace(eventID)
	if ownerID <= 0 || sessionID == "" || eventID == "" {
		return
	}
	m.runsMu.Lock()
	delete(m.crossNodeStopUntil, crossNodeStopKey(ownerID, sessionID, eventID))
	m.runsMu.Unlock()
}

// IsCrossNodeStopPending 返回该 session+owner+event 的跨节点停止标记是否有效。
func (m *Manager) IsCrossNodeStopPending(ownerID int64, sessionID, eventID string) bool {
	sessionID = strings.TrimSpace(sessionID)
	eventID = strings.TrimSpace(eventID)
	if ownerID <= 0 || sessionID == "" || eventID == "" {
		return false
	}
	key := crossNodeStopKey(ownerID, sessionID, eventID)
	now := time.Now().UnixMilli()
	m.runsMu.Lock()
	until, ok := m.crossNodeStopUntil[key]
	if ok && until <= now {
		delete(m.crossNodeStopUntil, key)
		ok = false
	}
	m.runsMu.Unlock()
	return ok
}
