package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/google/uuid"
)

func (m *Manager) PushDelegateEvent(evt DelegateEventPayload) bool {
	return m.pushDelegateEvent(evt, true)
}

// DispatchDelegateEventWithoutQueue 走与 PushDelegateEvent 完全相同的投递路径,
// 但 agent 离线时不落离线队列,直接返回 false。用于强时效的后端主动事件:
// 这类事件描述的是"此刻"的上下文,数小时后被重放只会造成困惑。
func (m *Manager) DispatchDelegateEventWithoutQueue(evt DelegateEventPayload) bool {
	return m.pushDelegateEvent(evt, false)
}

func (m *Manager) pushDelegateEvent(evt DelegateEventPayload, allowQueue bool) bool {
	if evt.AgentID <= 0 {
		return false
	}
	// owner 缺失的事件一律拒绝（fail-closed）：绝不允许把事件回退投到主连接，
	// 否则被共享者 B 的事件会串到主人 A 的 connector。
	if evt.OwnerID <= 0 {
		logger.L.Warnf("reject delegate event with missing owner: agent_id=%d event_type=%s session=%s event_id=%s", evt.AgentID, evt.EventType, evt.SessionID, evt.EventID)
		return false
	}
	// 用户纯文本 /grix 开头：对非白名单 verb 加前导空格转义，
	// 确保这些命令不会被当作 session control 拦截，而是作为普通消息发给 agent。
	// 白名单内的 verb（stop/status/where/restart）保留原样，由 connector 处理。
	// 卡片按钮走 URI 格式（grix://），不受影响。
	if !evt.Command && evt.Content != "" && !strings.HasPrefix(evt.Content, "grix://") && strings.HasPrefix(evt.Content, "/grix") {
		if !isWhitelistedGrixVerb(evt.Content) {
			evt.Content = " " + evt.Content
		}
	}
	// hermes 危险命令兜底审批（hd_ 前缀审批 ID）在 agent 侧没有结构化上下文，
	// 走 local_action 回传必然报 unknown/expired；改写成兜底协议约定的纯文本
	// 回复（/approve、/approve always、/deny）按普通消息透传，并跳过审批拦截器。
	var fallbackApproval hermesFallbackApprovalContext
	fallbackApprovalRewritten := false
	if !evt.Command {
		if fallbackCtx, ok := m.rewriteHermesFallbackApprovalResolution(&evt); ok {
			// evt.Content 已改写为纯文本回复，直接走下方正常投递。
			fallbackApproval = fallbackCtx
			fallbackApprovalRewritten = true
		} else if m.tryInterceptDelegateEvent(evt) {
			return true
		}
	}

	logger.L.Debugf("Agent API pushing delegate event: agent_id=%d owner_id=%d event_type=%s session=%s content=%s", evt.AgentID, evt.OwnerID, evt.EventType, evt.SessionID, evt.Content)
	if conn := m.lookupConnForDelegate(evt); conn != nil {
		if m.dispatchDelegateEvent(conn, evt) {
			return true
		}
	}

	// 按 evt.OwnerID 找该 owner 连接所在节点(agent 共享多连接物理隔离);
	// 退化到主路由表兼容旧路径。
	targetNode := loadAgentRouteForOwner(context.Background(), evt.AgentID, evt.OwnerID)
	if targetNode != "" && targetNode != m.getNodeID() {
		if m.forwardDelegateEvent(targetNode, evt) {
			return true
		}
	}

	if evt.Command {
		return false
	}
	if allowQueue && enqueueDelegateEvent(context.Background(), evt) {
		return true
	}
	if fallbackApprovalRewritten {
		// 改写后的兜底审批文本最终也没投递出去（agent 离线且离线队列不可用），
		// 把已置为「已提交」的卡片回补为失败，避免用户误以为批准已生效。
		m.failHermesFallbackApprovalCard(evt, fallbackApproval)
	}
	return false
}

// DispatchOwnerCommandText 以 owner 身份向 agent 连接器直投一条命令式文本消息
// （如工具栏 /stop）：不持久化（MsgID=0）、不注册 run/pending ack（Command=true）。
// 本地连接优先；否则转发到 agent 所在节点；离线则返回 false（不入队，避免投递过期命令）。
func (m *Manager) DispatchOwnerCommandText(agentID, ownerID int64, sessionID, content string) bool {
	if agentID <= 0 {
		return false
	}
	if ownerID <= 0 {
		logger.L.Warnf("reject owner command text with missing owner: agent_id=%d session=%s", agentID, sessionID)
		return false
	}
	evt := DelegateEventPayload{
		EventID:   fmt.Sprintf("toolbar_cmd_%d", time.Now().UnixNano()),
		EventType: "user_chat",
		AgentID:   agentID,
		OwnerID:   ownerID,
		SenderID:  ownerID,
		SessionID: sessionID,
		MsgType:   1,
		Content:   content,
		Command:   true,
		CreatedAt: time.Now().UnixMilli(),
	}
	if conn := m.lookupConnForDelegate(evt); conn != nil {
		if m.dispatchDelegateEvent(conn, evt) {
			return true
		}
	}
	// 按 ownerID 找目标节点(共享场景下被共享者连接可能在另一节点)。
	targetNode := loadAgentRouteForOwner(context.Background(), agentID, ownerID)
	if targetNode != "" && targetNode != m.getNodeID() {
		return m.forwardDelegateEvent(targetNode, evt)
	}
	return false
}

func (m *Manager) HandleForwardedDelegateEvent(evt DelegateEventPayload) bool {
	if evt.AgentID <= 0 {
		return false
	}
	if evt.OwnerID <= 0 {
		logger.L.Warnf("reject forwarded delegate event with missing owner: agent_id=%d event_type=%s session=%s event_id=%s", evt.AgentID, evt.EventType, evt.SessionID, evt.EventID)
		return false
	}
	conn := m.lookupConnForDelegate(evt)
	if conn != nil && m.dispatchDelegateEvent(conn, evt) {
		return true
	}
	targetNode := loadAgentRouteForOwner(context.Background(), evt.AgentID, evt.OwnerID)
	if targetNode != "" && targetNode != m.getNodeID() {
		if m.forwardDelegateEvent(targetNode, evt) {
			return true
		}
	}
	if evt.Command {
		return false
	}
	return enqueueDelegateEvent(context.Background(), evt)
}

// HandleForwardedDelegateRetry consumes an explicit Redis retry claim. It must
// never register pending/run state or enter the generic offline queue: the
// durable ACK record is the only retry source.
func (m *Manager) HandleForwardedDelegateRetry(envelope durableRetryEnvelope) bool {
	if envelope.AgentID <= 0 || envelope.OwnerID <= 0 ||
		strings.TrimSpace(envelope.EventID) == "" || strings.TrimSpace(envelope.Token) == "" {
		return false
	}
	conn := m.lookupConnByOwner(envelope.AgentID, envelope.OwnerID)
	if conn != nil && m.dispatchClaimedDelegateRetry(conn, envelope) {
		return true
	}
	targetNode := loadAgentRouteForOwner(
		context.Background(),
		envelope.AgentID,
		envelope.OwnerID,
	)
	if targetNode != "" && targetNode != m.getNodeID() {
		return m.forwardDelegateRetry(targetNode, envelope)
	}
	return false
}

// lookupConn 返回某个 agent 的主连接（agent 主人），无主连接时返回 nil。
// 用于只有 agentID、语义为「agent 级」的调用点（如读取 adapter 元数据）。
// 需要按使用者隔离的分发请用 lookupConnByOwner。
func (m *Manager) lookupConn(agentID int64) *agentConn {
	if agentID <= 0 {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.primaryConnLocked(agentID)
}

// primaryConnLocked 返回某 agent 的主连接（agent 主人），无主连接时返回 nil。
// 绝不退而返回「任一」连接：那会把主人的流量投到被共享者的连接上（隔离破坏）。
// 调用方必须已持有 m.mu 的读或写锁。
func (m *Manager) primaryConnLocked(agentID int64) *agentConn {
	owners := m.conns[agentID]
	if len(owners) == 0 {
		return nil
	}
	for _, c := range owners {
		if c != nil && c.isPrimary {
			return c
		}
	}
	return nil
}

// connByOwnerLocked 精确返回某 agent 在某 owner 身份下的连接：
// ownerID>0 只精确匹配 owners[ownerID]，无对应连接返回 nil（绝不回退其他 owner 的连接）；
// ownerID<=0 仅退主连接（同样无「任一」回退）。
// 调用方必须已持有 m.mu 的读或写锁。用于持锁内同时取多个连接的场景。
func (m *Manager) connByOwnerLocked(agentID, ownerID int64) *agentConn {
	if ownerID > 0 {
		if owners := m.conns[agentID]; owners != nil {
			return owners[ownerID]
		}
		return nil
	}
	return m.primaryConnLocked(agentID)
}

// lookupConnByOwner 精确返回某 agent 在某 owner 身份下的连接（agent 共享物理隔离的核心路由）。
func (m *Manager) lookupConnByOwner(agentID, ownerID int64) *agentConn {
	if agentID <= 0 || ownerID <= 0 {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if owners := m.conns[agentID]; owners != nil {
		return owners[ownerID]
	}
	return nil
}

// lookupConnForDelegate 按事件的使用者(OwnerID)严格路由到对应连接(agent 共享物理隔离的核心):
//   - OwnerID>0: 必须精确匹配。找不到返回 nil(让事件入离线队列,等对应连接重连后 drain),
//     绝不 fallback 到主连接 —— 否则被共享者 B 的事件会在 B 连接暂断时误投到主人 A,隔离被破。
//   - OwnerID<=0: 非法,返回 nil。路由层入口(PushDelegateEvent 等)已 fail-closed 拒绝
//     owner 缺失的事件,此处不再提供任何回退。
//
// 注: direct_session_route 在群场景(sessionType==2)负责把 OwnerID 设为 agent.OwnerID,
//
//	保证群事件精确命中主连接,而不是依赖路由层 fallback(后者会有"投错主人"的隔离风险)。
func (m *Manager) lookupConnForDelegate(evt DelegateEventPayload) *agentConn {
	if evt.OwnerID > 0 {
		return m.lookupConnByOwner(evt.AgentID, evt.OwnerID)
	}
	return nil
}

func (m *Manager) dispatchDelegateEvent(conn *agentConn, evt DelegateEventPayload) bool {
	return m.dispatchDelegateEventWithAttempt(conn, evt, 1)
}

func (m *Manager) dispatchDelegateEventWithAttempt(conn *agentConn, evt DelegateEventPayload, attempt int) bool {
	if conn == nil {
		return false
	}
	if attempt <= 0 {
		attempt = 1
	}
	if !evt.Command {
		token := strings.TrimSpace(evt.TerminalCommitToken)
		supportsTerminalCommit := hasDeclaredName(
			conn.capabilities,
			terminalCommitCapability,
		)
		if token != "" && !supportsTerminalCommit {
			// A token is part of the event's durable identity. Never strip it
			// merely because a reconnect landed on an older connector.
			return false
		}
		if token == "" && supportsTerminalCommit {
			evt.TerminalCommitToken = uuid.NewString()
		}
	}

	// 先注册 pending 再发送，避免本地 loopback 场景下 ack 在 register 之前到达的竞态。
	// 命令式事件（evt.Command）不注册 run、不登记 pending ack。
	dispatchStartedAt := time.Now().UTC()
	dispatchCallTurn := false
	if !evt.Command {
		dispatchCallTurn = senderInVoiceCall(evt.SenderID)
		switch m.registerPendingEventAckWithMetadata(
			evt,
			attempt,
			dispatchStartedAt,
			dispatchCallTurn,
		) {
		case pendingEventRegistrationExisting:
			// A duplicate upstream dispatch found authoritative ACK/result or
			// terminal state. Accept it without another wire send.
			return true
		case pendingEventRegistrationFailed:
			return false
		}
	}
	if !evt.IsRecordOnly() && !evt.Command {
		// Observe old silent work without changing its state. Only an explicit
		// connector result may settle it; the new event is still dispatched.
		m.observeStaleResultEventsForNewEvent(evt)
		m.registerActiveRunForDispatch(evt, dispatchStartedAt, dispatchCallTurn)
		// Reset tool execution accumulator for this session so the new
		// agent turn starts with a fresh card instead of appending to
		// the previous turn's card.
		deleteToolExecAccum(context.Background(), conn.agentID, evt.SessionID)
	}
	if m.sendDelegateEventAttempt(conn, evt, attempt) {
		if !evt.IsRecordOnly() && !evt.Command {
			m.persistActiveRunRunning(evt.EventID)
		}
		return true
	}
	// 发送失败，回滚本次首投/恢复注册。ACK 超时重投不会走这里，
	// 它必须保留原 pending/run，并只重发 wire packet。
	m.rollbackPendingEventAck(evt.EventID)
	return false
}

// sendDelegateEventAttempt only writes the event packet. It deliberately does
// not register pending/run state: ACK-timeout retries already have authoritative
// tracking, and recreating it here can race a late ACK/result and regress
// received/terminal state back to queued.
func (m *Manager) sendDelegateEventAttempt(conn *agentConn, evt DelegateEventPayload, attempt int) bool {
	if conn == nil {
		return false
	}
	if !m.ensureAgentConnectionAuthoritative(conn) {
		return false
	}
	if strings.TrimSpace(evt.TerminalCommitToken) != "" &&
		!hasDeclaredName(conn.capabilities, terminalCommitCapability) {
		return false
	}
	seq := conn.nextSeq()
	cmd, payload := conn.resolveDelegateOutbound(evt)
	if !conn.sendPayload(cmd, seq, payload) {
		return false
	}
	logger.L.Infof(
		"agent api event dispatched session=%s owner=%d agent=%d msg_id=%d event_type=%s adapter=%s attempt=%d/%d",
		evt.SessionID,
		evt.OwnerID,
		evt.AgentID,
		evt.MsgID,
		evt.EventType,
		conn.adapterID,
		attempt,
		agentAPIDeliveryMaxAttempts,
	)
	return true
}

func (c *agentConn) resolveDelegateOutbound(evt DelegateEventPayload) (string, any) {
	evt = applySafeConnectorRuntimeConfig(evt)
	if ShouldAttachNoReplyProtocol(evt) {
		evt.Content = AppendNoReplyProtocolInstruction(evt.Content)
	}
	cmd := "event_msg"
	payload := any(evt)
	if c == nil || c.adapter == nil {
		return cmd, payload
	}
	evt = c.applyGeminiSessionContext(evt)

	outbound, err := c.adapter.NormalizeOutbound(context.Background(), buildDomainOutboundEvent(evt))
	if err != nil {
		logger.L.Warnf("adapter NormalizeOutbound failed agent=%d adapter=%s err=%v, falling back to event_msg", c.agentID, c.adapterID, err)
		return cmd, payload
	}
	if outbound == nil {
		return cmd, payload
	}
	if normalizedCmd := strings.TrimSpace(outbound.Cmd); normalizedCmd != "" {
		cmd = normalizedCmd
	}
	if len(outbound.Payload) > 0 && json.Valid(outbound.Payload) {
		normalizedPayload := append(json.RawMessage(nil), outbound.Payload...)
		if token := strings.TrimSpace(evt.TerminalCommitToken); token != "" {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(normalizedPayload, &fields); err == nil && fields != nil {
				rawToken, _ := json.Marshal(token)
				fields["terminal_commit_token"] = rawToken
				if enriched, marshalErr := json.Marshal(fields); marshalErr == nil {
					normalizedPayload = enriched
				}
			}
		}
		payload = normalizedPayload
	}
	return cmd, payload
}

// applySafeConnectorRuntimeConfig prevents intermediate thinking/tool output
// and token-split /no_reply commands from becoming temporarily visible. Group
// sessions already use the same conservative delivery mode; commands and
// explicit internal events now do so regardless of session type.
func applySafeConnectorRuntimeConfig(evt DelegateEventPayload) DelegateEventPayload {
	if evt.SessionType != 2 && !evt.Command && !ShouldAttachNoReplyProtocol(evt) {
		return evt
	}

	envelope := map[string]any{}
	if len(evt.Extra) > 0 {
		_ = json.Unmarshal(evt.Extra, &envelope)
		if envelope == nil {
			envelope = map[string]any{}
		}
	}

	connector := map[string]any{}
	if rawConnector, ok := envelope["connector"].(map[string]any); ok && rawConnector != nil {
		connector = rawConnector
	}
	connector["tool_events"] = "drop"
	connector["thinking_events"] = "drop"
	connector["response_delivery"] = "single_message"
	envelope["connector"] = connector

	merged, err := json.Marshal(envelope)
	if err != nil {
		return evt
	}
	evt.Extra = merged
	return evt
}

func buildDomainOutboundEvent(evt DelegateEventPayload) agentadapter.DomainOutboundEvent {
	return agentadapter.DomainOutboundEvent{
		EventID:         evt.EventID,
		EventType:       evt.EventType,
		MirrorMode:      evt.MirrorMode,
		AgentID:         evt.AgentID,
		OwnerID:         evt.OwnerID,
		SessionID:       evt.SessionID,
		ThreadID:        evt.ThreadID,
		SessionType:     evt.SessionType,
		MsgID:           evt.MsgID,
		QuotedMessageID: evt.QuotedMessageID,
		SenderID:        evt.SenderID,
		MsgType:         evt.MsgType,
		Content:         evt.Content,
		Extra:           evt.Extra,
		Attachments:     buildDomainAttachmentPayloads(evt.Attachments),
		BizCard:         evt.BizCard,
		ChannelData:     evt.ChannelData,
		MentionUserIDs:  protocol.StringInt64s(append([]int64(nil), evt.MentionUserIDs...)),
		ContextMessages: append([]protocol.ContextMessagePayload(nil), evt.ContextMessages...),
		CreatedAt:       evt.CreatedAt,
	}
}

type delegateRetryOutcome struct {
	Routed bool
	Record *durablePendingDelegateRecord
}

func (m *Manager) redeliverDelegateEvent(evt DelegateEventPayload, attempt int) bool {
	return m.redeliverDelegateEventOutcome(evt, attempt).Routed
}

func (m *Manager) redeliverDelegateEventOutcome(
	evt DelegateEventPayload,
	attempt int,
) delegateRetryOutcome {
	if evt.AgentID <= 0 || attempt <= 1 {
		return delegateRetryOutcome{}
	}
	if store.RDB != nil {
		claim, err := claimDurablePendingDelegateRetry(
			context.Background(),
			evt.EventID,
			evt.AgentID,
			evt.OwnerID,
			agentAPIDeliveryMaxAttempts,
			m.eventAckWait,
		)
		if err != nil {
			logger.L.Warnf(
				"claim durable agent retry failed event=%s agent=%d owner=%d err=%v",
				strings.TrimSpace(evt.EventID), evt.AgentID, evt.OwnerID, err,
			)
			return delegateRetryOutcome{}
		}
		if !claim.Won {
			return delegateRetryOutcome{Record: claim.Record}
		}
		m.updatePendingAttemptFromDurable(evt.EventID, claim.Envelope.Attempt)
		routed := m.routeClaimedDelegateRetry(claim.Envelope)
		record, _ := loadDurablePendingDelegate(context.Background(), evt.EventID)
		return delegateRetryOutcome{Routed: routed, Record: record}
	}

	// 按事件使用者(OwnerID)精确路由，与首投一致；避免重投误落到主连接(主人)而串连接。
	if conn := m.lookupConnForDelegate(evt); conn != nil {
		if m.sendDelegateEventAttempt(conn, evt, attempt) {
			return delegateRetryOutcome{Routed: true}
		}
	}

	targetNode := loadAgentRouteForOwner(context.Background(), evt.AgentID, evt.OwnerID)
	if targetNode != "" && targetNode != m.getNodeID() {
		if m.forwardDelegateEvent(targetNode, evt) {
			return delegateRetryOutcome{Routed: true}
		}
	}

	// ACK retries never enter the generic offline queue. The durable ACK record
	// is replayed on reconnect and owns the monotonic attempt counter.
	return delegateRetryOutcome{}
}

func (m *Manager) updatePendingAttemptFromDurable(eventID string, attempt int) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" || attempt <= 0 {
		return
	}
	m.acksMu.Lock()
	if entry := m.pending[eventID]; entry != nil &&
		entry.kind == pendingEventKindDelegate &&
		entry.stage == pendingEventStageAck &&
		attempt > entry.attempt {
		entry.attempt = attempt
	}
	m.acksMu.Unlock()
}

func (m *Manager) routeClaimedDelegateRetry(envelope durableRetryEnvelope) bool {
	if conn := m.lookupConnByOwner(envelope.AgentID, envelope.OwnerID); conn != nil {
		if m.dispatchClaimedDelegateRetry(conn, envelope) {
			return true
		}
	}
	targetNode := loadAgentRouteForOwner(
		context.Background(),
		envelope.AgentID,
		envelope.OwnerID,
	)
	if targetNode != "" && targetNode != m.getNodeID() {
		return m.forwardDelegateRetry(targetNode, envelope)
	}
	return false
}

func (m *Manager) dispatchClaimedDelegateRetry(
	conn *agentConn,
	envelope durableRetryEnvelope,
) bool {
	if conn == nil || conn.agentID != envelope.AgentID || conn.ownerID != envelope.OwnerID {
		return false
	}
	if !m.ensureAgentConnectionAuthoritative(conn) {
		return false
	}
	accepted, err := markDurablePendingDelegateRetryDispatched(
		context.Background(),
		envelope,
	)
	if err != nil {
		logger.L.Warnf(
			"mark durable agent retry dispatched failed event=%s attempt=%d err=%v",
			envelope.EventID, envelope.Attempt, err,
		)
		return false
	}
	if !accepted {
		return false
	}
	record, ok := loadDurablePendingDelegate(context.Background(), envelope.EventID)
	if !ok || record.Stage != durablePendingDelegateStageAck ||
		record.Attempt != envelope.Attempt ||
		record.RetryToken != envelope.Token {
		releaseDurablePendingDelegateRetryDispatch(context.Background(), envelope)
		return false
	}
	if m.sendDelegateEventAttempt(conn, record.Event, envelope.Attempt) {
		return true
	}
	releaseDurablePendingDelegateRetryDispatch(context.Background(), envelope)
	return false
}

func (m *Manager) forwardDelegateRetry(
	targetNode string,
	envelope durableRetryEnvelope,
) bool {
	if strings.TrimSpace(targetNode) == "" || store.RDB == nil {
		return false
	}
	data, err := json.Marshal(map[string]any{
		"cmd":     redisCmdForwardDelegateRetry,
		"payload": envelope,
	})
	if err != nil {
		return false
	}
	if err := store.RDB.Publish(
		context.Background(),
		fmt.Sprintf("chan:%s", targetNode),
		data,
	).Err(); err != nil {
		logger.L.Warnf(
			"publish forwarded agent retry failed node=%s event=%s attempt=%d err=%v",
			targetNode, envelope.EventID, envelope.Attempt, err,
		)
		return false
	}
	return true
}

func (m *Manager) forwardDelegateEvent(targetNode string, evt DelegateEventPayload) bool {
	return m.forwardDelegateEventWithContext(context.Background(), targetNode, evt)
}

func (m *Manager) forwardDelegateEventWithContext(ctx context.Context, targetNode string, evt DelegateEventPayload) bool {
	if strings.TrimSpace(targetNode) == "" || store.RDB == nil {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}

	data, err := json.Marshal(map[string]any{
		"cmd":     redisCmdForwardDelegateEvent,
		"payload": evt,
	})
	if err != nil {
		logger.L.Warnf("marshal forwarded agent api event failed node=%s agent=%d err=%v", targetNode, evt.AgentID, err)
		return false
	}

	if err := store.RDB.Publish(ctx, fmt.Sprintf("chan:%s", targetNode), data).Err(); err != nil {
		logger.L.Warnf("publish forwarded agent api event failed node=%s agent=%d err=%v", targetNode, evt.AgentID, err)
		return false
	}

	logger.L.Infof(
		"agent api event forwarded node=%s session=%s owner=%d agent=%d msg_id=%d",
		targetNode,
		evt.SessionID,
		evt.OwnerID,
		evt.AgentID,
		evt.MsgID,
	)
	return true
}

func (m *Manager) getNodeID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.nodeID
}

var (
	globalMu      sync.RWMutex
	globalManager *Manager
)

func SetGlobal(manager *Manager) {
	globalMu.Lock()
	globalManager = manager
	globalMu.Unlock()
}

func GetGlobalManager() *Manager {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalManager
}

func PushDelegateEvent(evt DelegateEventPayload) bool {
	if evt.OwnerID <= 0 {
		logger.L.Warnf("reject delegate event with missing owner (global entry): agent_id=%d event_type=%s session=%s event_id=%s", evt.AgentID, evt.EventType, evt.SessionID, evt.EventID)
		return false
	}
	globalMu.RLock()
	manager := globalManager
	globalMu.RUnlock()
	if manager == nil {
		return enqueueDelegateEvent(context.Background(), evt)
	}
	return manager.PushDelegateEvent(evt)
}

// DispatchCommandDelegateEvent sends an internal command-style delegate event
// without durable queueing. It is intended for backend-originated control or
// context-injection events that must not become visible user messages and must
// not be replayed later if the target agent is offline.
// DispatchDelegateEventWithContext 是 DispatchDelegateEventWithoutQueue 的全局入口:
// manager 未就绪时直接返回 false,不回退到离线队列。
func DispatchDelegateEventWithContext(ctx context.Context, evt DelegateEventPayload) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return false
	}
	if evt.OwnerID <= 0 {
		logger.L.Warnf("reject delegate event with missing owner (no-queue entry): agent_id=%d event_type=%s session=%s event_id=%s", evt.AgentID, evt.EventType, evt.SessionID, evt.EventID)
		return false
	}
	manager := GetGlobalManager()
	if manager == nil {
		return false
	}
	return manager.DispatchDelegateEventWithoutQueue(evt)
}

func DispatchCommandDelegateEvent(evt DelegateEventPayload) bool {
	return DispatchCommandDelegateEventWithContext(context.Background(), evt)
}

func DispatchCommandDelegateEventWithContext(ctx context.Context, evt DelegateEventPayload) bool {
	evt.Command = true
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return false
	}
	if evt.OwnerID <= 0 {
		logger.L.Warnf("reject command delegate event with missing owner (global entry): agent_id=%d event_type=%s session=%s event_id=%s", evt.AgentID, evt.EventType, evt.SessionID, evt.EventID)
		return false
	}
	globalMu.RLock()
	manager := globalManager
	globalMu.RUnlock()
	if manager == nil {
		return false
	}
	return manager.PushCommandDelegateEvent(ctx, evt)
}

func (m *Manager) PushCommandDelegateEvent(ctx context.Context, evt DelegateEventPayload) bool {
	evt.Command = true
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return false
	}
	if evt.AgentID <= 0 {
		return false
	}
	if evt.OwnerID <= 0 {
		logger.L.Warnf("reject command delegate event with missing owner: agent_id=%d event_type=%s session=%s event_id=%s", evt.AgentID, evt.EventType, evt.SessionID, evt.EventID)
		return false
	}
	if conn := m.lookupConnForDelegate(evt); conn != nil {
		if m.dispatchDelegateEvent(conn, evt) {
			return true
		}
	}

	targetNode := loadAgentRouteForOwner(ctx, evt.AgentID, evt.OwnerID)
	if targetNode != "" && targetNode != m.getNodeID() {
		return m.forwardDelegateEventWithContext(ctx, targetNode, evt)
	}
	return false
}

// PushAgentEvent 把 cmd/payload 推给 agentID 在 ownerID 维度下的连接。
// ownerID 必须 >0：严格按 (agentID, ownerID) 路由(agent 共享多连接物理隔离)；
// ownerID<=0 非法，直接拒绝（fail-closed），不再回退主连接。
// 事件投不出去时若是可入离线队列的 cmd 则入队。
func PushAgentEvent(agentID, ownerID int64, cmd string, payload interface{}) bool {
	if ownerID <= 0 {
		logger.L.Warnf("reject agent event with missing owner (global entry): agent_id=%d cmd=%s", agentID, cmd)
		return false
	}
	globalMu.RLock()
	manager := globalManager
	globalMu.RUnlock()
	if manager == nil {
		if evt, ok := buildQueuedAgentEvent(agentID, ownerID, cmd, payload); ok {
			return enqueueQueuedAgentEvent(context.Background(), *evt)
		}
		return false
	}
	return manager.PushToAgent(agentID, ownerID, cmd, payload)
}

func GetGlobal() *Manager {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalManager
}

// PushToAgent 按 (agentID, ownerID) 严格路由推送；ownerID<=0 非法，直接拒绝（fail-closed）。
func (m *Manager) PushToAgent(agentID, ownerID int64, cmd string, payload interface{}) bool {
	if ownerID <= 0 {
		logger.L.Warnf("reject push to agent with missing owner: agent_id=%d cmd=%s", agentID, cmd)
		return false
	}
	queuedEvt, canQueue := buildQueuedAgentEvent(agentID, ownerID, cmd, payload)
	conn := m.lookupConnByOwner(agentID, ownerID)
	if conn == nil || conn.closed.Load() {
		if canQueue {
			return enqueueQueuedAgentEvent(context.Background(), *queuedEvt)
		}
		return false
	}
	if !m.ensureAgentConnectionAuthoritative(conn) {
		if targetNode := loadAgentRouteForOwner(
			context.Background(),
			agentID,
			ownerID,
		); targetNode != "" && targetNode != m.getNodeID() {
			if m.forwardEventLifecycleCommand(agentID, ownerID, cmd, payload) {
				return true
			}
		}
		if canQueue {
			return enqueueQueuedAgentEvent(context.Background(), *queuedEvt)
		}
		return false
	}
	outboundPayload := payload
	if canQueue && queuedEvt != nil {
		outboundPayload = json.RawMessage(queuedEvt.Payload)
	}
	outboundCmd, outbound := conn.resolveAgentEventOutbound(cmd, outboundPayload)
	if conn.sendPayload(outboundCmd, 0, outbound) {
		return true
	}
	if canQueue {
		return enqueueQueuedAgentEvent(context.Background(), *queuedEvt)
	}
	return false
}

// whitelistedGrixVerbs 是允许作为纯文本命令保留的 /grix verb，
// 这些命令参数固定或无参数，不会被误判为普通文字。
var whitelistedGrixVerbs = map[string]bool{
	"stop":    true,
	"status":  true,
	"where":   true,
	"restart": true,
}

// isWhitelistedGrixVerb 检查纯文本 /grix 命令的 verb 是否在白名单中。
func isWhitelistedGrixVerb(content string) bool {
	fields := strings.Fields(content)
	if len(fields) < 2 {
		return false
	}
	return whitelistedGrixVerbs[strings.ToLower(fields[1])]
}
