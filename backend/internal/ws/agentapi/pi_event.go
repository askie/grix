package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func (m *Manager) handlePiEvent(conn *agentConn, pkt *protocol.Packet) {
	var payload PiEventPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.sendPayload("error", pkt.Seq, SendNackPayload{Code: 4001, Msg: "invalid pi_event payload"})
		return
	}
	if payload.SessionID == "" {
		return
	}
	switch payload.PiEventType {
	case "agent_start", "turn_start", "message_start", "message_end":
		m.refreshAgentLease(conn)
	case "message_update":
		m.handlePiMessageUpdate(conn, pkt, &payload)
	case "turn_end", "agent_end":
		m.handlePiStreamFinish(conn, pkt, &payload)
	case "tool_execution_start":
		m.handlePiToolExecStart(conn, pkt, &payload)
	case "tool_execution_update":
		m.handlePiToolExecUpdate(conn, pkt, &payload)
	case "tool_execution_end":
		m.handlePiToolExecEnd(conn, pkt, &payload)
	default:
		logger.L.Warnf("pi_event: unknown pi_event_type %q for event %s", payload.PiEventType, payload.EventID)
		m.refreshAgentLease(conn)
	}
}

func (m *Manager) handlePiMessageUpdate(conn *agentConn, pkt *protocol.Packet, pep *PiEventPayload) {
	var inner struct {
		A struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
		} `json:"assistantMessageEvent"`
	}
	if err := json.Unmarshal(pep.PiPayload, &inner); err != nil {
		logger.L.Warnf("pi_event: unmarshal message_update for %s: %v", pep.EventID, err)
		return
	}
	switch inner.A.Type {
	case "text_delta":
		m.flushPiThinking(conn, pkt, pep)
		m.handlePiTextDelta(conn, pkt, pep, inner.A.Delta)
	case "thinking_delta":
		if inner.A.Delta != "" {
			m.handlePiThinkingDelta(conn, pkt, pep, inner.A.Delta)
		}
	case "done":
		m.flushPiThinking(conn, pkt, pep)
		m.handlePiStreamFinish(conn, pkt, pep)
	}
}

func (m *Manager) handlePiThinkingDelta(conn *agentConn, pkt *protocol.Packet, pep *PiEventPayload, delta string) {
	if m.streamChunkFn == nil {
		return
	}
	thinkingKey := pep.EventID + "_thinking"
	m.piThinkingBuf.Store(thinkingKey, true)
	s, _ := m.piChunkSeq.LoadOrStore(thinkingKey, new(int64))
	seq := atomic.AddInt64(s.(*int64), 1)
	qid := m.resolveReplyQuotedMessageID(pep.EventID, 0)
	c := AgentStreamChunkPayload{EventID: pep.EventID, SessionID: pep.SessionID, ThreadID: pep.ThreadID, DeltaContent: delta, ChunkSeq: seq, ClientMsgID: fmt.Sprintf("pi_%s_thinking", pep.EventID), QuotedMessageID: qid}
	if err := m.streamChunkFn(context.Background(), conn.agentID, conn.ownerID, c); err != nil {
		conn.sendPayload("error", pkt.Seq, SendNackPayload{ClientMsgID: c.ClientMsgID, Code: 5001, Msg: "pi thinking stream chunk failed"})
	}
}

func (m *Manager) flushPiThinking(conn *agentConn, pkt *protocol.Packet, pep *PiEventPayload) {
	thinkingKey := pep.EventID + "_thinking"
	if _, ok := m.piThinkingBuf.LoadAndDelete(thinkingKey); !ok {
		return
	}
	s, ok := m.piChunkSeq.LoadAndDelete(thinkingKey)
	var fs int64 = 1
	if ok {
		fs = *s.(*int64) + 1
	}
	qid := m.resolveReplyQuotedMessageID(pep.EventID, 0)
	c := AgentStreamChunkPayload{EventID: pep.EventID, SessionID: pep.SessionID, ThreadID: pep.ThreadID, ChunkSeq: fs, IsFinish: true, ClientMsgID: fmt.Sprintf("pi_%s_thinking", pep.EventID), QuotedMessageID: qid}
	m.streamChunkFn(context.Background(), conn.agentID, conn.ownerID, c)
}

func (m *Manager) handlePiTextDelta(conn *agentConn, pkt *protocol.Packet, pep *PiEventPayload, delta string) {
	if delta == "" || m.streamChunkFn == nil {
		return
	}
	s, _ := m.piChunkSeq.LoadOrStore(pep.EventID, new(int64))
	seq := atomic.AddInt64(s.(*int64), 1)
	qid := m.resolveReplyQuotedMessageID(pep.EventID, 0)
	c := AgentStreamChunkPayload{EventID: pep.EventID, SessionID: pep.SessionID, ThreadID: pep.ThreadID, DeltaContent: delta, ChunkSeq: seq, ClientMsgID: fmt.Sprintf("pi_%s", pep.EventID), QuotedMessageID: qid}
	if err := m.streamChunkFn(context.Background(), conn.agentID, conn.ownerID, c); err != nil {
		conn.sendPayload("error", pkt.Seq, SendNackPayload{ClientMsgID: c.ClientMsgID, Code: 5001, Msg: "pi stream chunk failed"})
	}
}

func (m *Manager) handlePiStreamFinish(conn *agentConn, pkt *protocol.Packet, pep *PiEventPayload) {
	if m.streamChunkFn == nil {
		return
	}
	m.flushPiThinking(conn, pkt, pep)
	var fs int64 = 1
	if v, ok := m.piChunkSeq.LoadAndDelete(pep.EventID); ok {
		fs = *v.(*int64) + 1
	}
	qid := m.resolveReplyQuotedMessageID(pep.EventID, 0)
	c := AgentStreamChunkPayload{EventID: pep.EventID, SessionID: pep.SessionID, ThreadID: pep.ThreadID, ChunkSeq: fs, IsFinish: true, ClientMsgID: fmt.Sprintf("pi_%s", pep.EventID), QuotedMessageID: qid}
	if err := m.streamChunkFn(context.Background(), conn.agentID, conn.ownerID, c); err != nil {
		conn.sendPayload("error", pkt.Seq, SendNackPayload{ClientMsgID: c.ClientMsgID, Code: 5001, Msg: "pi stream finish failed"})
		return
	}
	conn.sendPayload("send_ack", pkt.Seq, protocol.SendAckPayload{SessionID: pep.SessionID, ClientMsgID: c.ClientMsgID, CreatedAt: time.Now().UnixMilli()})
}

func (m *Manager) handlePiToolExecStart(conn *agentConn, pkt *protocol.Packet, pep *PiEventPayload) {
	var inner struct {
		TID  string `json:"toolCallId"`
		TN   string `json:"toolName"`
		Args struct {
			Cmd  string `json:"command"`
			Path string `json:"path"`
			FP   string `json:"filePath"`
			TP   string `json:"targetPath"`
			Desc string `json:"description"`
		} `json:"args"`
	}
	if err := json.Unmarshal(pep.PiPayload, &inner); err != nil {
		logger.L.Warnf("pi_event: unmarshal tool_execution_start for %s: %v", pep.EventID, err)
		return
	}
	summary := summarizePiTool(inner.TN, inner.Args)
	if summary == "" {
		return
	}
	extra, _ := json.Marshal(map[string]any{"channel_data": map[string]any{"grix": map[string]any{"toolExecution": map[string]any{"summary_text": summary, "detail_text": ""}}}})
	sp := SendMsgPayload{EventID: pep.EventID, SessionID: pep.SessionID, ClientMsgID: fmt.Sprintf("pi_tool_%s_%d", inner.TID, pep.PiSequence), MsgType: 1, Content: summary, Extra: extra}
	if conn.adapter != nil {
		rp, _ := json.Marshal(sp)
		n, e := conn.adapter.NormalizeInbound(context.Background(), rp)
		if e == nil && n != nil {
			sp.Content = n.Content
			sp.Extra = n.Extra
		}
	}
	if m.sendFn == nil {
		return
	}
	if m.shouldSuppressToolExecutionCards(sp.EventID, sp.SessionID, conn.ownerID) {
		return
	}
	var toolMeta toolExecPayloadMeta
	var isToolCard bool
	sp.Content, sp.Extra, toolMeta, isToolCard = compactToolExecutionPayload(sp.Content, sp.Extra)
	if !isToolCard {
		return
	}
	ar := m.tryAccumulateToolExec(
		context.Background(),
		conn,
		sp.SessionID,
		sp.EventID,
		sp.ClientMsgID,
		toolMeta,
	)
	if ar.handled {
		return
	}
	ak := firstNonEmpty(strings.TrimSpace(conn.adapterID), strings.TrimSpace(conn.clientType))
	vt := ownerVisibleToForAdapterCard(ak, sp.Content, sp.Extra, conn.ownerID)
	if ar.children != nil {
		sp.Content = ar.modifiedContent
	}
	r, e := m.sendFn(context.Background(), SendMessageReq{EventID: sp.EventID, AgentID: conn.agentID, OwnerID: conn.ownerID, SessionID: sp.SessionID, ClientMsgID: sp.ClientMsgID, MsgType: sp.MsgType, Content: sp.Content, Extra: sp.Extra, VisibleTo: vt})
	if e != nil || r == nil {
		releaseToolExecDedup(context.Background(), ar.dedupKey)
		return
	}
	finishFirstToolExecAccum(context.Background(), conn.agentID, sp.SessionID, ar, r.MsgID, vt)
}

func (m *Manager) handlePiToolExecUpdate(conn *agentConn, pkt *protocol.Packet, pep *PiEventPayload) {
}

func (m *Manager) handlePiToolExecEnd(conn *agentConn, pkt *protocol.Packet, pep *PiEventPayload) {
	var inner struct {
		TID   string `json:"toolCallId"`
		TN    string `json:"toolName"`
		IsErr bool   `json:"isError"`
		Res   struct {
			C []struct {
				T    string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(pep.PiPayload, &inner); err != nil {
		logger.L.Warnf("pi_event: unmarshal tool_execution_end for %s: %v", pep.EventID, err)
		return
	}
	if !inner.IsErr {
		return
	}
	var dp []string
	for _, c := range inner.Res.C {
		if c.Text != "" {
			dp = append(dp, c.Text)
		}
	}
	detail := truncateString(strings.Join(dp, "\n"), 2000)
	summary := inner.TN + " (error)"
	extra, _ := json.Marshal(map[string]any{"channel_data": map[string]any{"grix": map[string]any{"toolExecution": map[string]any{"summary_text": summary, "detail_text": detail}}}})
	sp := SendMsgPayload{EventID: pep.EventID, SessionID: pep.SessionID, ClientMsgID: fmt.Sprintf("pi_tool_result_%s_%d", inner.TID, pep.PiSequence), MsgType: 1, Content: summary, Extra: extra}
	if conn.adapter != nil {
		rp, _ := json.Marshal(sp)
		n, e := conn.adapter.NormalizeInbound(context.Background(), rp)
		if e == nil && n != nil {
			sp.Content = n.Content
			sp.Extra = n.Extra
		}
	}
	if m.sendFn == nil {
		return
	}
	if m.shouldSuppressToolExecutionCards(sp.EventID, sp.SessionID, conn.ownerID) {
		return
	}
	var toolMeta toolExecPayloadMeta
	var isToolCard bool
	sp.Content, sp.Extra, toolMeta, isToolCard = compactToolExecutionPayload(sp.Content, sp.Extra)
	if !isToolCard {
		return
	}
	ar := m.tryAccumulateToolExec(
		context.Background(),
		conn,
		sp.SessionID,
		sp.EventID,
		sp.ClientMsgID,
		toolMeta,
	)
	if ar.handled {
		return
	}
	ak := firstNonEmpty(strings.TrimSpace(conn.adapterID), strings.TrimSpace(conn.clientType))
	vt := ownerVisibleToForAdapterCard(ak, sp.Content, sp.Extra, conn.ownerID)
	if ar.children != nil {
		sp.Content = ar.modifiedContent
	}
	r, e := m.sendFn(context.Background(), SendMessageReq{EventID: sp.EventID, AgentID: conn.agentID, OwnerID: conn.ownerID, SessionID: sp.SessionID, ClientMsgID: sp.ClientMsgID, MsgType: sp.MsgType, Content: sp.Content, Extra: sp.Extra, VisibleTo: vt})
	if e != nil || r == nil {
		releaseToolExecDedup(context.Background(), ar.dedupKey)
		return
	}
	finishFirstToolExecAccum(context.Background(), conn.agentID, sp.SessionID, ar, r.MsgID, vt)
}

func summarizePiTool(name string, args struct {
	Cmd  string `json:"command"`
	Path string `json:"path"`
	FP   string `json:"filePath"`
	TP   string `json:"targetPath"`
	Desc string `json:"description"`
}) string {
	switch name {
	case "bash", "shell", "execute":
		if args.Cmd != "" {
			return args.Cmd
		}
	case "read_file", "readFile":
		if p := firstNonEmpty(args.Path, args.FP); p != "" {
			return "Read " + p
		}
	case "write_file", "writeFile":
		if p := firstNonEmpty(args.Path, args.FP, args.TP); p != "" {
			return "Write " + p
		}
	case "edit_file", "editFile":
		if p := firstNonEmpty(args.Path, args.FP); p != "" {
			return "Edit " + p
		}
	case "list_directory", "listDirectory":
		if args.Path != "" {
			return "List " + args.Path
		}
	}
	if args.Desc != "" {
		return name + ": " + args.Desc
	}
	return name
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
