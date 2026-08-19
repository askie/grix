package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/agentadapter"
	acpadapter "github.com/askie/grix/backend/internal/agentadapter/acp"
	codexadapter "github.com/askie/grix/backend/internal/agentadapter/codex"
	geminiadapter "github.com/askie/grix/backend/internal/agentadapter/gemini"
	hermesadapter "github.com/askie/grix/backend/internal/agentadapter/hermes"
	openclawadapter "github.com/askie/grix/backend/internal/agentadapter/openclaw"
	qwenadapter "github.com/askie/grix/backend/internal/agentadapter/qwen"
	"github.com/askie/grix/backend/internal/geminisession"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func init() {
	logger.Init()
}

// mockStreamChunkHandler records calls to the stream chunk handler.
type mockStreamChunkHandler struct {
	calls []AgentStreamChunkPayload
	err   error
}

func (h *mockStreamChunkHandler) handle(_ context.Context, agentID, ownerID int64, payload AgentStreamChunkPayload) error {
	h.calls = append(h.calls, payload)
	return h.err
}

type mockSendMessageHandler struct {
	calls  []SendMessageReq
	result *SendMessageResult
	err    error
}

func (h *mockSendMessageHandler) handle(_ context.Context, req SendMessageReq) (*SendMessageResult, error) {
	h.calls = append(h.calls, req)
	return h.result, h.err
}

type mockReactMsgHandler struct {
	calls []ReactMsgPayload
	err   error
}

func (h *mockReactMsgHandler) handle(_ context.Context, _ int64, _ int64, payload ReactMsgPayload) error {
	h.calls = append(h.calls, payload)
	return h.err
}

type mockMediaUploadInitHandler struct {
	calls  []MediaUploadInitPayload
	result *MediaUploadInitResult
	err    error
}

func (h *mockMediaUploadInitHandler) handle(_ context.Context, _ int64, _ int64, payload MediaUploadInitPayload) (*MediaUploadInitResult, error) {
	h.calls = append(h.calls, payload)
	return h.result, h.err
}

type recordingOutboundAdapter struct {
	cmd          string
	payload      json.RawMessage
	lastOutbound *agentadapter.DomainOutboundEvent
}

func (a *recordingOutboundAdapter) Family() string { return "test" }

func (a *recordingOutboundAdapter) AdapterID() string { return "test/recording" }

func (a *recordingOutboundAdapter) Supports(agentadapter.AgentClientMeta) bool { return true }

func (a *recordingOutboundAdapter) NormalizeInbound(context.Context, []byte) (*agentadapter.NormalizedInboundEvent, error) {
	return nil, nil
}

func (a *recordingOutboundAdapter) NormalizeOutbound(_ context.Context, event agentadapter.DomainOutboundEvent) (*agentadapter.AdapterOutboundPacket, error) {
	eventCopy := event
	a.lastOutbound = &eventCopy
	return &agentadapter.AdapterOutboundPacket{
		Cmd:     a.cmd,
		Payload: a.payload,
	}, nil
}

func (a *recordingOutboundAdapter) NormalizeApproval(context.Context, agentadapter.DomainApprovalEvent) (*agentadapter.AdapterApprovalPacket, error) {
	return nil, nil
}

func (a *recordingOutboundAdapter) NormalizeStatus(context.Context, agentadapter.DomainStatusEvent) (*agentadapter.AdapterStatusPacket, error) {
	return nil, nil
}

func makePacket(t *testing.T, cmd string, seq int64, payload interface{}) *protocol.Packet {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal packet payload: %v", err)
	}
	return &protocol.Packet{Cmd: cmd, Seq: seq, Payload: raw}
}

// registerStreamChunkOwnership 给 client_stream_chunk 类测试注册一个 pending event,
// 让 ensureEventOwnedBy / ensureSessionConsistentWithEvent 通过校验。
// 仅用于纯前置参数校验类测试,生产路径仍走 dispatchDelegateEvent。
func registerStreamChunkOwnership(t *testing.T, mgr *Manager, eventID, sessionID string, agentID, ownerID int64) {
	t.Helper()
	withoutDurableStores(t)
	mgr.registerPendingEventAck(DelegateEventPayload{
		EventID:   eventID,
		AgentID:   agentID,
		OwnerID:   ownerID,
		SessionID: sessionID,
	}, 1)
}

func setupQueuedAgentEventDBTest(t *testing.T) func() {
	t.Helper()
	testDB := testutil.NewTestDB()
	previousDB := store.DB
	store.DB = testDB.DB
	return func() {
		store.DB = previousDB
		testDB.Close()
	}
}

func withoutDurableStores(t *testing.T) {
	t.Helper()
	store.DB, store.RDB = nil, nil
	t.Cleanup(func() {
		store.DB, store.RDB = nil, nil
	})
}

func TestHandleClientStreamChunk_MissingSessionID(t *testing.T) {
	handler := &mockStreamChunkHandler{}
	mgr := NewManager("", 30*time.Second, nil, handler.handle, nil, nil)
	defer mgr.Shutdown()
	_ = mgr // ensure manager is constructed

	conn := &agentConn{
		agentID:  100,
		ownerID:  200,
		clientID: "test",
		send:     make(chan []byte, 64),
	}

	pkt := makePacket(t, "client_stream_chunk", 1, AgentStreamChunkPayload{
		SessionID:    "",
		DeltaContent: "hello",
		ChunkSeq:     1,
		IsFinish:     false,
	})

	mgr.handleClientStreamChunk(conn, pkt)

	if len(handler.calls) != 0 {
		t.Fatalf("handler should not be called for missing session_id, got %d calls", len(handler.calls))
	}

	// Check that a nack was sent.
	select {
	case data := <-conn.send:
		var resp protocol.Packet
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.Cmd != "send_nack" {
			t.Fatalf("expected send_nack, got=%s", resp.Cmd)
		}
		var nack SendNackPayload
		if err := json.Unmarshal(resp.Payload, &nack); err != nil {
			t.Fatalf("unmarshal nack payload: %v", err)
		}
		if nack.Code != 4001 {
			t.Fatalf("expected code=4001, got=%d", nack.Code)
		}
	default:
		t.Fatalf("expected send_nack to be sent")
	}
}

func TestHandleSendMsg_NormalizesStructuredCardContentViaAdapter(t *testing.T) {
	handler := &mockSendMessageHandler{result: &SendMessageResult{MsgID: 3001, InboxSeq: 11, CreatedAt: 1704067205000}}
	mgr := NewManager("", 30*time.Second, handler.handle, nil, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID:   100,
		ownerID:   200,
		clientID:  "openclaw-agent",
		send:      make(chan []byte, 64),
		adapter:   openclawadapter.NewAdapter(),
		adapterID: openclawadapter.AdapterID,
	}

	pkt := makePacket(t, protocol.CmdSendMsg, 41, SendMsgPayload{
		SessionID:   "sess-approval",
		ClientMsgID: "cmsg-approval",
		MsgType:     1,
		Content:     "审批请求",
		Extra: json.RawMessage(`{
			"channel_data":{
				"execApproval":{
					"approvalId":"74569573",
					"approvalSlug":"74569573",
					"allowedDecisions":["allow-once","allow-always","deny"]
				},
				"grix":{
					"execApproval":{
						"approval_command_id":"74569573",
						"command":"echo hi",
						"host":"gateway",
						"cwd":"/tmp/demo"
					}
				}
			}
		}`),
	})

	mgr.handleSendMsg(conn, pkt)

	if len(handler.calls) != 1 {
		t.Fatalf("handler call count=%d want=1", len(handler.calls))
	}
	if got := handler.calls[0].Content; !strings.Contains(got, "grix://card/exec_approval") {
		t.Fatalf("normalized content=%q should contain exec approval card uri", got)
	}
	if got := string(handler.calls[0].Extra); !strings.Contains(got, `"channel_data"`) {
		t.Fatalf("normalized extra=%s should keep channel_data", got)
	}
	if len(handler.calls[0].VisibleTo) != 1 || handler.calls[0].VisibleTo[0] != 200 {
		t.Fatalf("visible_to=%v want=[200] for openclaw adapter exec_approval card", handler.calls[0].VisibleTo)
	}

	select {
	case data := <-conn.send:
		var resp protocol.Packet
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.Cmd != protocol.CmdSendAck {
			t.Fatalf("expected send_ack, got=%s", resp.Cmd)
		}
	default:
		t.Fatalf("expected send_ack to be sent")
	}
}

func TestHandleSendMsg_ScopesVisibleToForOwnerCardsOnTargetAdapters(t *testing.T) {
	testCases := []struct {
		name      string
		adapter   agentadapter.AgentAdapter
		adapterID string
		content   string
		ownerID   int64
	}{
		{
			name:      "codex approval card",
			adapter:   codexadapter.NewAdapter(),
			adapterID: codexadapter.AdapterID,
			content:   "[Exec Approval](grix://card/exec_approval?approval_id=req_1)",
			ownerID:   1201,
		},
		{
			name:      "qwen open session card",
			adapter:   qwenadapter.NewAdapter(),
			adapterID: qwenadapter.AdapterID,
			content:   "[Open Workspace](grix://card/agent_open_session?summary_text=missing)",
			ownerID:   1202,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			handler := &mockSendMessageHandler{result: &SendMessageResult{MsgID: 3999, InboxSeq: 21, CreatedAt: 1704067215000}}
			mgr := NewManager("", 30*time.Second, handler.handle, nil, nil, nil)
			defer mgr.Shutdown()

			conn := &agentConn{
				agentID:   777,
				ownerID:   tc.ownerID,
				clientID:  "owner-visibility-test",
				send:      make(chan []byte, 64),
				adapter:   tc.adapter,
				adapterID: tc.adapterID,
			}
			pkt := makePacket(t, protocol.CmdSendMsg, 61, SendMsgPayload{
				SessionID:   "sess-owner-visibility",
				ClientMsgID: "cmsg-owner-visibility",
				MsgType:     1,
				Content:     tc.content,
			})

			mgr.handleSendMsg(conn, pkt)

			if len(handler.calls) != 1 {
				t.Fatalf("handler call count=%d want=1", len(handler.calls))
			}
			if len(handler.calls[0].VisibleTo) != 1 || handler.calls[0].VisibleTo[0] != tc.ownerID {
				t.Fatalf("visible_to=%v want=[%d]", handler.calls[0].VisibleTo, tc.ownerID)
			}
		})
	}
}

func TestHandleSendMsg_NoReplyCommandAckDoesNotCallSendHandler(t *testing.T) {
	withoutDurableStores(t)
	const (
		eventID   = "evt-send-no-reply"
		sessionID = "sess-no-reply"
		agentID   = int64(104)
		ownerID   = int64(204)
	)
	handler := &mockSendMessageHandler{result: &SendMessageResult{MsgID: 3007, InboxSeq: 17, CreatedAt: 1704067211000}}
	mgr := NewManager("", 30*time.Second, handler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.registerPendingEventAck(DelegateEventPayload{
		EventID:   eventID,
		AgentID:   agentID,
		OwnerID:   ownerID,
		SessionID: sessionID,
	}, 1)

	conn := &agentConn{
		agentID:  agentID,
		ownerID:  ownerID,
		clientID: "no-reply-agent",
		send:     make(chan []byte, 64),
	}
	pkt := makePacket(t, protocol.CmdSendMsg, 47, SendMsgPayload{
		EventID:     eventID,
		SessionID:   sessionID,
		ClientMsgID: "cmsg-no-reply",
		MsgType:     1,
		Content:     "/no_reply",
	})

	mgr.handleSendMsg(conn, pkt)

	if len(handler.calls) != 0 {
		t.Fatalf("handler call count=%d want=0", len(handler.calls))
	}
	select {
	case data := <-conn.send:
		var resp protocol.Packet
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.Cmd != protocol.CmdSendAck {
			t.Fatalf("expected send_ack, got=%s", resp.Cmd)
		}
		var ack protocol.SendAckPayload
		if err := json.Unmarshal(resp.Payload, &ack); err != nil {
			t.Fatalf("unmarshal ack payload: %v", err)
		}
		if ack.MsgID != 0 {
			t.Fatalf("ack msg_id=%d want=0", ack.MsgID)
		}
	default:
		t.Fatalf("expected send_ack to be sent")
	}
}

func TestHandleSendMsg_NoReplyCommandWithoutEventStillRequiresSessionWritable(t *testing.T) {
	withoutDurableStores(t)
	handler := &mockSendMessageHandler{result: &SendMessageResult{MsgID: 3010, InboxSeq: 20, CreatedAt: 1704067214000}}
	mgr := NewManager("", 30*time.Second, handler.handle, nil, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID:  107,
		ownerID:  0,
		clientID: "no-reply-unauthorized-agent",
		send:     make(chan []byte, 64),
	}
	pkt := makePacket(t, protocol.CmdSendMsg, 50, SendMsgPayload{
		SessionID:   "sess-no-reply-unauthorized",
		ClientMsgID: "cmsg-no-reply-unauthorized",
		MsgType:     1,
		Content:     "/no_reply",
	})

	mgr.handleSendMsg(conn, pkt)

	if len(handler.calls) != 0 {
		t.Fatalf("handler call count=%d want=0", len(handler.calls))
	}
	select {
	case data := <-conn.send:
		var resp protocol.Packet
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.Cmd != protocol.CmdSendNack {
			t.Fatalf("expected send_nack, got=%s", resp.Cmd)
		}
		var nack SendNackPayload
		if err := json.Unmarshal(resp.Payload, &nack); err != nil {
			t.Fatalf("unmarshal nack payload: %v", err)
		}
		if nack.Code != 4003 {
			t.Fatalf("nack code=%d want=4003", nack.Code)
		}
	default:
		t.Fatalf("expected send_nack to be sent")
	}
}

func TestHandleSendMsg_InternalNoReplyExplanationAckDoesNotCallSendHandler(t *testing.T) {
	withoutDurableStores(t)
	const (
		eventID   = "customer_coach:204:client_open:1"
		sessionID = "sess-internal-no-reply"
		agentID   = int64(105)
		ownerID   = int64(205)
	)
	handler := &mockSendMessageHandler{result: &SendMessageResult{MsgID: 3008, InboxSeq: 18, CreatedAt: 1704067212000}}
	mgr := NewManager("", 30*time.Second, handler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.registerPendingEventAck(DelegateEventPayload{
		EventID:   eventID,
		EventType: "customer_coach_snapshot",
		AgentID:   agentID,
		OwnerID:   ownerID,
		SessionID: sessionID,
	}, 1)

	conn := &agentConn{
		agentID:  agentID,
		ownerID:  ownerID,
		clientID: "internal-no-reply-agent",
		send:     make(chan []byte, 64),
	}
	pkt := makePacket(t, protocol.CmdSendMsg, 48, SendMsgPayload{
		EventID:     eventID,
		SessionID:   sessionID,
		ClientMsgID: "cmsg-internal-no-reply",
		MsgType:     1,
		Content:     "核心新手路径已完成，正处于活跃使用状态，选择沉默。",
	})

	mgr.handleSendMsg(conn, pkt)

	if len(handler.calls) != 0 {
		t.Fatalf("handler call count=%d want=0", len(handler.calls))
	}
	select {
	case data := <-conn.send:
		var resp protocol.Packet
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.Cmd != protocol.CmdSendAck {
			t.Fatalf("expected send_ack, got=%s", resp.Cmd)
		}
	default:
		t.Fatalf("expected send_ack to be sent")
	}
}

func TestHandleSendMsg_LongInternalCustomerCoachReasoningAckDoesNotCallSendHandler(t *testing.T) {
	withoutDurableStores(t)
	const (
		eventID   = "customer_coach:2089662004397604864:client_open:1"
		sessionID = "sess-long-internal-no-reply"
		agentID   = int64(108)
		ownerID   = int64(208)
	)
	handler := &mockSendMessageHandler{result: &SendMessageResult{MsgID: 3011, InboxSeq: 21, CreatedAt: 1704067215000}}
	mgr := NewManager("", 30*time.Second, handler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.registerPendingEventAck(DelegateEventPayload{
		EventID:   eventID,
		EventType: "customer_coach_snapshot",
		AgentID:   agentID,
		OwnerID:   ownerID,
		SessionID: sessionID,
	}, 1)

	conn := &agentConn{
		agentID:  agentID,
		ownerID:  ownerID,
		clientID: "long-internal-no-reply-agent",
		send:     make(chan []byte, 64),
	}
	pkt := makePacket(t, protocol.CmdSendMsg, 51, SendMsgPayload{
		EventID:     eventID,
		SessionID:   sessionID,
		ClientMsgID: "cmsg-long-internal-no-reply",
		MsgType:     1,
		Content: `我来判断是否需要给这位用户发引导消息。

根据快照，用户ID是2089662004397604864，这是注册时间等于触发时间，说明是刚注册的新用户。
根据我的记忆规则，无Agent的情况应该引导"极速接入"。
我需要用 grix_reply 发送这条引导消息。让我先查看它的 schema。`,
	})

	mgr.handleSendMsg(conn, pkt)

	if len(handler.calls) != 0 {
		t.Fatalf("handler call count=%d want=0", len(handler.calls))
	}
	select {
	case data := <-conn.send:
		var resp protocol.Packet
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.Cmd != protocol.CmdSendAck {
			t.Fatalf("expected send_ack, got=%s", resp.Cmd)
		}
	default:
		t.Fatalf("expected send_ack to be sent")
	}
}

func TestHandleSendMsg_NaturalUserMessageIsNotNoReplySuppressed(t *testing.T) {
	withoutDurableStores(t)
	handler := &mockSendMessageHandler{result: &SendMessageResult{MsgID: 3009, InboxSeq: 19, CreatedAt: 1704067213000}}
	mgr := NewManager("", 30*time.Second, handler.handle, nil, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID:  106,
		ownerID:  206,
		clientID: "natural-message-agent",
		send:     make(chan []byte, 64),
	}
	content := "客户目前正处于活跃使用状态，暂时无需额外引导。"
	pkt := makePacket(t, protocol.CmdSendMsg, 49, SendMsgPayload{
		SessionID:   "sess-natural-message",
		ClientMsgID: "cmsg-natural-message",
		MsgType:     1,
		Content:     content,
	})

	mgr.handleSendMsg(conn, pkt)

	if len(handler.calls) != 1 {
		t.Fatalf("handler call count=%d want=1", len(handler.calls))
	}
	if handler.calls[0].Content != content {
		t.Fatalf("content=%q want=%q", handler.calls[0].Content, content)
	}
	select {
	case data := <-conn.send:
		var resp protocol.Packet
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.Cmd != protocol.CmdSendAck {
			t.Fatalf("expected send_ack, got=%s", resp.Cmd)
		}
	default:
		t.Fatalf("expected send_ack to be sent")
	}
}

func TestHandleSendMsg_MergesMediaURLIntoExtra(t *testing.T) {
	handler := &mockSendMessageHandler{result: &SendMessageResult{MsgID: 3006, InboxSeq: 16, CreatedAt: 1704067210000}}
	mgr := NewManager("", 30*time.Second, handler.handle, nil, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID:  103,
		ownerID:  203,
		clientID: "media-agent",
		send:     make(chan []byte, 64),
	}

	pkt := makePacket(t, protocol.CmdSendMsg, 46, SendMsgPayload{
		SessionID:   "sess-media",
		ClientMsgID: "cmsg-media",
		MsgType:     2,
		Content:     "[media]",
		MediaURL:    "https://cdn.example.com/demo.png",
	})

	mgr.handleSendMsg(conn, pkt)

	if len(handler.calls) != 1 {
		t.Fatalf("handler call count=%d want=1", len(handler.calls))
	}
	if handler.calls[0].MediaURL != "https://cdn.example.com/demo.png" {
		t.Fatalf("media_url=%q want=%q", handler.calls[0].MediaURL, "https://cdn.example.com/demo.png")
	}

	var extra map[string]any
	if err := json.Unmarshal(handler.calls[0].Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	if extra["media_url"] != "https://cdn.example.com/demo.png" {
		t.Fatalf("extra media_url=%v want=%q", extra["media_url"], "https://cdn.example.com/demo.png")
	}
	attachments, ok := extra["attachments"].([]any)
	if !ok || len(attachments) != 1 {
		t.Fatalf("attachments=%#v want=1 item", extra["attachments"])
	}
	firstAttachment, ok := attachments[0].(map[string]any)
	if !ok {
		t.Fatalf("first attachment=%#v want object", attachments[0])
	}
	if firstAttachment["media_url"] != "https://cdn.example.com/demo.png" {
		t.Fatalf("attachment media_url=%v want=%q", firstAttachment["media_url"], "https://cdn.example.com/demo.png")
	}
}

func TestHandleSendMsg_NormalizesPlainTextExecApprovalViaAdapter(t *testing.T) {
	handler := &mockSendMessageHandler{result: &SendMessageResult{MsgID: 3002, InboxSeq: 12, CreatedAt: 1704067206000}}
	mgr := NewManager("", 30*time.Second, handler.handle, nil, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID:   100,
		ownerID:   200,
		clientID:  "openclaw-agent",
		send:      make(chan []byte, 64),
		adapter:   openclawadapter.NewAdapter(),
		adapterID: openclawadapter.AdapterID,
	}

	pkt := makePacket(t, protocol.CmdSendMsg, 42, SendMsgPayload{
		SessionID:   "sess-approval-plain",
		ClientMsgID: "cmsg-approval-plain",
		MsgType:     1,
		Content: strings.Join([]string{
			"🔒 Exec approval required",
			"ID: approval_full_123",
			"Command: `npm run deploy`",
			"CWD: /srv/app",
			"Host: gateway",
			"Expires in: 120s",
			"Mode: foreground (interactive approvals available in this chat).",
			"Background mode note: non-interactive runs cannot wait for chat approvals; use pre-approved policy (allow-always or ask=off).",
			"Reply with: /approve <id> allow-once|allow-always|deny",
		}, "\n"),
	})

	mgr.handleSendMsg(conn, pkt)

	if len(handler.calls) != 1 {
		t.Fatalf("handler call count=%d want=1", len(handler.calls))
	}
	if got := handler.calls[0].Content; !strings.Contains(got, "grix://card/exec_approval") {
		t.Fatalf("normalized content=%q should contain exec approval card uri", got)
	}
	if got := string(handler.calls[0].Extra); !strings.Contains(got, `"channel_data"`) {
		t.Fatalf("normalized extra=%s should keep synthesized channel_data", got)
	}
}

func TestHandleSendMsg_NormalizesStructuredCardContentViaHermesAdapter(t *testing.T) {
	handler := &mockSendMessageHandler{result: &SendMessageResult{MsgID: 3003, InboxSeq: 13, CreatedAt: 1704067207000}}
	mgr := NewManager("", 30*time.Second, handler.handle, nil, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID:   100,
		ownerID:   200,
		clientID:  "hermes-agent",
		send:      make(chan []byte, 64),
		adapter:   hermesadapter.NewAdapter(),
		adapterID: hermesadapter.AdapterID,
	}

	pkt := makePacket(t, protocol.CmdSendMsg, 43, SendMsgPayload{
		SessionID:   "sess-hermes-approval",
		ClientMsgID: "cmsg-hermes-approval",
		MsgType:     1,
		Content:     "审批请求",
		Extra: json.RawMessage(`{
			"biz_card":{
				"version":1,
				"type":"exec_approval",
				"payload":{
					"approval_id":"74569573",
					"approval_slug":"74569573",
					"approval_command_id":"74569573",
					"command":"echo hi",
					"host":"gateway",
					"cwd":"/tmp/demo",
					"allowed_decisions":["allow-once","allow-always","deny"]
				}
			}
		}`),
	})

	mgr.handleSendMsg(conn, pkt)

	if len(handler.calls) != 1 {
		t.Fatalf("handler call count=%d want=1", len(handler.calls))
	}
	if got := handler.calls[0].Content; !strings.Contains(got, "grix://card/exec_approval") {
		t.Fatalf("normalized content=%q should contain exec approval card uri", got)
	}
	if got := string(handler.calls[0].Extra); !strings.Contains(got, `"biz_card"`) {
		t.Fatalf("normalized extra=%s should keep biz_card", got)
	}
}

func TestHandleSendMsg_PreservesThreadIDViaHermesAdapter(t *testing.T) {
	handler := &mockSendMessageHandler{result: &SendMessageResult{MsgID: 3004, InboxSeq: 14, CreatedAt: 1704067208000}}
	mgr := NewManager("", 30*time.Second, handler.handle, nil, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID:   101,
		ownerID:   201,
		clientID:  "hermes-agent-thread",
		send:      make(chan []byte, 64),
		adapter:   hermesadapter.NewAdapter(),
		adapterID: hermesadapter.AdapterID,
	}

	pkt := makePacket(t, protocol.CmdSendMsg, 44, SendMsgPayload{
		SessionID:   "sess-hermes-thread",
		ThreadID:    "topic-a",
		ClientMsgID: "cmsg-hermes-thread",
		MsgType:     1,
		Content:     "thread reply",
	})

	mgr.handleSendMsg(conn, pkt)

	if len(handler.calls) != 1 {
		t.Fatalf("handler call count=%d want=1", len(handler.calls))
	}
	if handler.calls[0].ThreadID != "topic-a" {
		t.Fatalf("thread_id=%q want=topic-a", handler.calls[0].ThreadID)
	}
}

func TestHandleSendMsg_AllowsSameEventAfterNonStreamOutput(t *testing.T) {
	handler := &mockSendMessageHandler{result: &SendMessageResult{MsgID: 3101, InboxSeq: 21, CreatedAt: 1704067210000}}
	mgr := NewManager("", 30*time.Second, handler.handle, nil, nil, nil)
	defer mgr.Shutdown()

	event := DelegateEventPayload{
		EventID:     "evt-send-before-stream",
		AgentID:     100,
		OwnerID:     200,
		SessionID:   "sess-send-before-stream",
		SessionType: 1,
		MsgID:       300,
	}
	mgr.registerPendingEventAck(event, 1)
	mgr.registerActiveRun(event)
	mgr.MarkRunStreaming(event.EventID, 88001)

	conn := &agentConn{
		agentID:   event.AgentID,
		ownerID:   event.OwnerID,
		clientID:  "hermes-agent",
		send:      make(chan []byte, 64),
		adapter:   hermesadapter.NewAdapter(),
		adapterID: hermesadapter.AdapterID,
	}
	pkt := makePacket(t, protocol.CmdSendMsg, 46, SendMsgPayload{
		EventID:     event.EventID,
		SessionID:   event.SessionID,
		ClientMsgID: "cmsg-send-before-stream",
		MsgType:     1,
		Content:     "direct final reply",
	})

	mgr.handleSendMsg(conn, pkt)

	if len(handler.calls) != 1 {
		t.Fatalf("handler call count=%d want=1", len(handler.calls))
	}
	select {
	case data := <-conn.send:
		var resp protocol.Packet
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.Cmd != protocol.CmdSendAck {
			t.Fatalf("expected send_ack, got=%s", resp.Cmd)
		}
	default:
		t.Fatalf("expected send_ack to be sent")
	}
}

func TestHandleSendMsg_AcceptsSameEventAfterStreamStarts(t *testing.T) {
	handler := &mockSendMessageHandler{result: &SendMessageResult{MsgID: 3102, InboxSeq: 22, CreatedAt: 1704067211000}}
	mgr := NewManager("", 30*time.Second, handler.handle, nil, nil, nil)
	defer mgr.Shutdown()

	event := DelegateEventPayload{
		EventID:     "evt-stream-then-send",
		AgentID:     100,
		OwnerID:     200,
		SessionID:   "sess-stream-then-send",
		SessionType: 1,
		MsgID:       301,
	}
	mgr.registerPendingEventAck(event, 1)
	mgr.registerActiveRun(event)
	mgr.MarkRunClientStreamStarted(event.EventID, 99001)

	conn := &agentConn{
		agentID:   event.AgentID,
		ownerID:   event.OwnerID,
		clientID:  "hermes-agent",
		send:      make(chan []byte, 64),
		adapter:   hermesadapter.NewAdapter(),
		adapterID: hermesadapter.AdapterID,
	}
	pkt := makePacket(t, protocol.CmdSendMsg, 47, SendMsgPayload{
		EventID:     event.EventID,
		SessionID:   event.SessionID,
		ClientMsgID: "cmsg-duplicate-final",
		MsgType:     1,
		Content:     "duplicate final reply",
	})

	mgr.handleSendMsg(conn, pkt)

	if len(handler.calls) != 1 {
		t.Fatalf("handler call count=%d want=1", len(handler.calls))
	}
	select {
	case data := <-conn.send:
		var resp protocol.Packet
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.Cmd != protocol.CmdSendAck {
			t.Fatalf("expected send_ack, got=%s payload=%s", resp.Cmd, string(resp.Payload))
		}
	default:
		t.Fatalf("expected send_ack to be sent")
	}

}

func TestHandleSendMsg_AllowsToolExecutionCardAfterStreamStarts(t *testing.T) {
	handler := &mockSendMessageHandler{result: &SendMessageResult{MsgID: 3103, InboxSeq: 23, CreatedAt: 1704067212000}}
	mgr := NewManager("", 30*time.Second, handler.handle, nil, nil, nil)
	defer mgr.Shutdown()

	event := DelegateEventPayload{
		EventID:     "evt-stream-tool-card",
		AgentID:     100,
		OwnerID:     200,
		SenderID:    200, // direct agent chat: sender == owner
		SessionID:   "sess-stream-tool-card",
		SessionType: 1,
		MsgID:       302,
	}
	mgr.registerPendingEventAck(event, 1)
	mgr.registerActiveRun(event)
	mgr.MarkRunClientStreamStarted(event.EventID, 99002)

	conn := &agentConn{
		agentID:   event.AgentID,
		ownerID:   event.OwnerID,
		clientID:  "acp-agent",
		send:      make(chan []byte, 64),
		adapter:   acpadapter.NewAdapter(),
		adapterID: acpadapter.AdapterID,
	}
	pkt := makePacket(t, protocol.CmdSendMsg, 48, SendMsgPayload{
		EventID:     event.EventID,
		SessionID:   event.SessionID,
		ClientMsgID: "cmsg-tool-card",
		MsgType:     1,
		Content:     "",
		Extra: json.RawMessage(`{
			"channel_data":{
				"grix":{
					"toolExecution":{
						"summary_text":"bash: pwd",
						"detail_text":"pwd"
					}
				}
			}
		}`),
	})

	mgr.handleSendMsg(conn, pkt)

	if len(handler.calls) != 1 {
		t.Fatalf("handler call count=%d want=1", len(handler.calls))
	}
	if got := handler.calls[0].Content; !strings.Contains(got, "grix://card/tool_execution") {
		t.Fatalf("content=%q should be normalized tool card", got)
	}
	select {
	case data := <-conn.send:
		var resp protocol.Packet
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.Cmd == protocol.CmdSendNack {
			t.Fatalf("tool card should not be rejected after stream: %s", string(resp.Payload))
		}
	default:
		t.Fatalf("expected send_ack to be sent")
	}
}

func TestHandleSendMsg_SuppressesToolExecutionCardInLLMProxyChat(t *testing.T) {
	handler := &mockSendMessageHandler{result: &SendMessageResult{MsgID: 3104, InboxSeq: 24, CreatedAt: 1704067213000}}
	mgr := NewManager("", 30*time.Second, handler.handle, nil, nil, nil)
	defer mgr.Shutdown()

	// LLM proxy scenario: session_type=1, sender (999) != owner (200).
	// Another person's message triggered the owner's hosted LLM.
	event := DelegateEventPayload{
		EventID:     "evt-llm-proxy-tool-card",
		AgentID:     100,
		OwnerID:     200,
		SenderID:    999, // different person triggered the AI
		SessionID:   "sess-llm-proxy-tool-card",
		SessionType: 1,
		MsgID:       303,
	}
	mgr.registerPendingEventAck(event, 1)
	mgr.registerActiveRun(event)

	conn := &agentConn{
		agentID:  event.AgentID,
		ownerID:  event.OwnerID,
		clientID: "acp-agent",
		send:     make(chan []byte, 64),
	}
	pkt := makePacket(t, protocol.CmdSendMsg, 49, SendMsgPayload{
		EventID:     event.EventID,
		SessionID:   event.SessionID,
		ClientMsgID: "cmsg-proxy-tool-card",
		MsgType:     1,
		Content:     "grix://card/tool_execution?summary_text=bash%3A+pwd&detail_text=pwd",
	})

	mgr.handleSendMsg(conn, pkt)

	if len(handler.calls) != 0 {
		t.Fatalf("tool execution card should be suppressed in LLM proxy chat, got %d handler calls", len(handler.calls))
	}
	select {
	case data := <-conn.send:
		var resp protocol.Packet
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.Cmd != protocol.CmdSendAck {
			t.Fatalf("expected send_ack for suppressed tool card, got cmd=%s", resp.Cmd)
		}
	default:
		t.Fatalf("expected send_ack to be sent")
	}
}

func TestHandleSendMsg_SuppressesToolExecutionCardInGroupChat(t *testing.T) {
	handler := &mockSendMessageHandler{result: &SendMessageResult{MsgID: 3105, InboxSeq: 25, CreatedAt: 1704067214000}}
	mgr := NewManager("", 30*time.Second, handler.handle, nil, nil, nil)
	defer mgr.Shutdown()

	// Group chats always set connector.tool_events=drop outbound; enforce inbound too.
	// Even when the owner themselves triggered the agent (sender == owner), tools stay hidden.
	event := DelegateEventPayload{
		EventID:     "evt-group-tool-card",
		AgentID:     100,
		OwnerID:     200,
		SenderID:    200,
		SessionID:   "sess-group-tool-card",
		SessionType: 2,
		MsgID:       304,
	}
	mgr.registerPendingEventAck(event, 1)
	mgr.registerActiveRun(event)

	conn := &agentConn{
		agentID:  event.AgentID,
		ownerID:  event.OwnerID,
		clientID: "acp-agent",
		send:     make(chan []byte, 64),
	}
	pkt := makePacket(t, protocol.CmdSendMsg, 50, SendMsgPayload{
		EventID:     event.EventID,
		SessionID:   event.SessionID,
		ClientMsgID: "cmsg-group-tool-card",
		MsgType:     1,
		Content:     "grix://card/tool_execution?summary_text=bash%3A+ls&detail_text=ls",
	})

	mgr.handleSendMsg(conn, pkt)

	if len(handler.calls) != 0 {
		t.Fatalf("tool execution card should be suppressed in group chat, got %d handler calls", len(handler.calls))
	}
	select {
	case data := <-conn.send:
		var resp protocol.Packet
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.Cmd != protocol.CmdSendAck {
			t.Fatalf("expected send_ack for suppressed tool card, got cmd=%s", resp.Cmd)
		}
	default:
		t.Fatalf("expected send_ack to be sent")
	}
}

func TestHandleSendMsg_SuppressesToolCardWhenDelegateActiveAndEventLost(t *testing.T) {
	handler := &mockSendMessageHandler{result: &SendMessageResult{MsgID: 3106, InboxSeq: 26, CreatedAt: 1704067215000}}
	mgr := NewManager("", 30*time.Second, handler.handle, nil, nil, nil)
	defer mgr.Shutdown()

	previousRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		_ = store.RDB.Close()
		store.RDB = previousRDB
	})

	sessionID := "sess-delegate-lost-event"
	ownerID := int64(200)
	ctx := context.Background()
	key := fmt.Sprintf("im:delegate:%s:%d", sessionID, ownerID)
	if err := store.RDB.HSet(ctx, key, "agent_id", "100").Err(); err != nil {
		t.Fatalf("seed delegate: %v", err)
	}

	conn := &agentConn{
		agentID:  100,
		ownerID:  ownerID,
		clientID: "acp-agent",
		send:     make(chan []byte, 64),
	}
	// Empty event_id simulates late output after tracking expiry (session-authorized path).
	pkt := makePacket(t, protocol.CmdSendMsg, 51, SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-lost-event-tool",
		MsgType:     1,
		Content:     "grix://card/tool_execution?summary_text=bash%3A+pwd&detail_text=pwd",
	})

	mgr.handleSendMsg(conn, pkt)

	if len(handler.calls) != 0 {
		t.Fatalf("tool card should be suppressed when hosted-delegate is active without event tracking, got %d calls", len(handler.calls))
	}
}

func TestShouldSuppressToolExecutionCards_StaleEventFallsBackToDelegate(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	previousRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		_ = store.RDB.Close()
		store.RDB = previousRDB
	})

	sessionID := "sess-stale-event-delegate"
	ownerID := int64(200)
	key := fmt.Sprintf("im:delegate:%s:%d", sessionID, ownerID)
	if err := store.RDB.HSet(context.Background(), key, "agent_id", "100").Err(); err != nil {
		t.Fatalf("seed delegate: %v", err)
	}

	if !mgr.shouldSuppressToolExecutionCards("evt-expired-unknown", sessionID, ownerID) {
		t.Fatal("stale event_id with active text-delegate should suppress tool cards")
	}
	if mgr.shouldSuppressToolExecutionCards("evt-expired-unknown", "sess-no-delegate", ownerID) {
		t.Fatal("stale event_id without delegate must not suppress")
	}
}

func TestHandleSendMsg_SuppressesToolCardWhenPendingExtraDropsTools(t *testing.T) {
	handler := &mockSendMessageHandler{result: &SendMessageResult{MsgID: 3107, InboxSeq: 27, CreatedAt: 1704067216000}}
	mgr := NewManager("", 30*time.Second, handler.handle, nil, nil, nil)
	defer mgr.Shutdown()

	// Private hosted path can have sender==owner (e.g. voice/self paths) while
	// still carrying connector.tool_events=drop on the pending event Extra.
	event := DelegateEventPayload{
		EventID:     "evt-extra-drop-tool-card",
		AgentID:     100,
		OwnerID:     200,
		SenderID:    200,
		SessionID:   "sess-extra-drop-tool-card",
		SessionType: 1,
		MsgID:       305,
		Extra:       json.RawMessage(`{"connector":{"tool_events":"drop","thinking_events":"drop","text_events":"drop"}}`),
	}
	mgr.registerPendingEventAck(event, 1)
	mgr.registerActiveRun(event)

	conn := &agentConn{
		agentID:  event.AgentID,
		ownerID:  event.OwnerID,
		clientID: "acp-agent",
		send:     make(chan []byte, 64),
	}
	pkt := makePacket(t, protocol.CmdSendMsg, 60, SendMsgPayload{
		EventID:     event.EventID,
		SessionID:   event.SessionID,
		ClientMsgID: "cmsg-extra-drop-tool",
		MsgType:     1,
		Content:     "grix://card/tool_execution?summary_text=bash%3A+echo&detail_text=echo",
	})

	mgr.handleSendMsg(conn, pkt)

	if len(handler.calls) != 0 {
		t.Fatalf("pending Extra tool_events=drop should suppress tool card even when sender==owner, got %d calls", len(handler.calls))
	}
}

func TestShouldSuppressToolExecutionCards_DirectOwnerAgentKeepsTools(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	event := DelegateEventPayload{
		EventID:     "evt-owner-agent-tools",
		AgentID:     100,
		OwnerID:     200,
		SenderID:    200, // owner talking to own agent
		SessionID:   "sess-owner-agent",
		SessionType: 1,
		MsgID:       1,
	}
	mgr.registerPendingEventAck(event, 1)
	mgr.registerActiveRun(event)

	if mgr.shouldSuppressToolExecutionCards(event.EventID, event.SessionID, event.OwnerID) {
		t.Fatal("direct owner↔agent chat must keep tool cards")
	}
}

func TestHandleSendMsg_NormalizesLegacyExecApprovalViaHermesAdapter(t *testing.T) {
	handler := &mockSendMessageHandler{result: &SendMessageResult{MsgID: 3005, InboxSeq: 15, CreatedAt: 1704067209000}}
	mgr := NewManager("", 30*time.Second, handler.handle, nil, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID:   102,
		ownerID:   202,
		clientID:  "hermes-agent",
		send:      make(chan []byte, 64),
		adapter:   hermesadapter.NewAdapter(),
		adapterID: hermesadapter.AdapterID,
	}

	pkt := makePacket(t, protocol.CmdSendMsg, 45, SendMsgPayload{
		SessionID:   "sess-hermes-legacy",
		ClientMsgID: "cmsg-hermes-legacy",
		MsgType:     1,
		Content:     "审批请求",
		Extra: json.RawMessage(`{
			"channel_data":{
				"execApproval":{
					"approvalId":"74569573",
					"approvalSlug":"74569573"
				},
				"grix":{
					"execApproval":{
						"approval_command_id":"74569573",
						"command":"echo hi",
						"host":"gateway"
					}
				}
			}
		}`),
	})

	mgr.handleSendMsg(conn, pkt)

	if len(handler.calls) != 1 {
		t.Fatalf("handler call count=%d want=1", len(handler.calls))
	}
	if got := handler.calls[0].Content; !strings.Contains(got, "grix://card/exec_approval") {
		t.Fatalf("normalized content=%q should contain exec_approval card", got)
	}
}

func TestHandleSendMsg_NormalizesAgentQuestionBizCardViaHermesAdapter(t *testing.T) {
	handler := &mockSendMessageHandler{result: &SendMessageResult{MsgID: 3004, InboxSeq: 14, CreatedAt: 1704067208000}}
	mgr := NewManager("", 30*time.Second, handler.handle, nil, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID:   101,
		ownerID:   201,
		clientID:  "hermes-agent",
		send:      make(chan []byte, 64),
		adapter:   hermesadapter.NewAdapter(),
		adapterID: hermesadapter.AdapterID,
	}

	pkt := makePacket(t, protocol.CmdSendMsg, 44, SendMsgPayload{
		SessionID:   "sess-hermes-question",
		ClientMsgID: "cmsg-hermes-question",
		MsgType:     1,
		Content:     "请确认环境",
		Extra: json.RawMessage(`{
			"biz_card":{
				"version":1,
				"type":"agent_question",
				"payload":{
					"request_id":"req_env_1",
					"questions":[
						{
							"index":1,
							"header":"Environment",
							"prompt":"Choose an environment.",
							"options":["production","staging"]
						}
					]
				}
			}
		}`),
	})

	mgr.handleSendMsg(conn, pkt)

	if len(handler.calls) != 1 {
		t.Fatalf("handler call count=%d want=1", len(handler.calls))
	}
	if got := handler.calls[0].Content; !strings.Contains(got, "grix://card/agent_question") {
		t.Fatalf("normalized content=%q should contain agent question card uri", got)
	}
	if got := string(handler.calls[0].Extra); !strings.Contains(got, `"biz_card"`) {
		t.Fatalf("normalized extra=%s should keep biz_card", got)
	}
}

func TestHandleClientStreamChunk_MissingDeltaContentNonFinish(t *testing.T) {
	installDurableLifecycleTestStores(t, false)
	if err := store.RDB.HSet(
		context.Background(),
		"im:delegate:sess-1:200",
		"agent_id",
		"100",
	).Err(); err != nil {
		t.Fatalf("seed delegate identity: %v", err)
	}

	handler := &mockStreamChunkHandler{}
	mgr := NewManager("", 30*time.Second, nil, handler.handle, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID: 100, ownerID: 200, clientID: "test",
		send: make(chan []byte, 64),
	}

	pkt := makePacket(t, "client_stream_chunk", 2, AgentStreamChunkPayload{
		SessionID:    "sess-1",
		DeltaContent: "",
		ChunkSeq:     1,
		IsFinish:     false,
	})

	mgr.handleClientStreamChunk(conn, pkt)

	if len(handler.calls) != 0 {
		t.Fatalf("handler should not be called for empty delta_content with is_finish=false")
	}

	select {
	case data := <-conn.send:
		var resp protocol.Packet
		json.Unmarshal(data, &resp)
		if resp.Cmd != "send_nack" {
			t.Fatalf("expected send_nack, got=%s", resp.Cmd)
		}
	default:
		t.Fatalf("expected send_nack to be sent")
	}
}

func TestHandleClientStreamChunkNoReplyCallsStreamHandlerBeforeAck(t *testing.T) {
	handler := &mockStreamChunkHandler{}
	mgr := NewManager("", 30*time.Second, nil, handler.handle, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID: 100, ownerID: 200, clientID: "test",
		send: make(chan []byte, 64),
	}
	registerStreamChunkOwnership(t, mgr, "evt-stream-no-reply", "sess-stream-no-reply", conn.agentID, conn.ownerID)

	pkt := makePacket(t, protocol.CmdClientStreamChunk, 23, AgentStreamChunkPayload{
		EventID:      "evt-stream-no-reply",
		SessionID:    "sess-stream-no-reply",
		DeltaContent: "/no_reply",
		ChunkSeq:     1,
		ClientMsgID:  "cmsg-stream-no-reply",
	})

	mgr.handleClientStreamChunk(conn, pkt)

	if len(handler.calls) != 1 {
		t.Fatalf("stream handler call count=%d want=1", len(handler.calls))
	}
	if handler.calls[0].DeltaContent != "/no_reply" {
		t.Fatalf("delta_content=%q want /no_reply", handler.calls[0].DeltaContent)
	}
	select {
	case data := <-conn.send:
		var resp protocol.Packet
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.Cmd != protocol.CmdSendAck {
			t.Fatalf("expected send_ack, got=%s", resp.Cmd)
		}
		var ack map[string]any
		if err := json.Unmarshal(resp.Payload, &ack); err != nil {
			t.Fatalf("unmarshal ack payload: %v", err)
		}
		if ack["no_reply"] != true {
			t.Fatalf("ack no_reply=%v want true", ack["no_reply"])
		}
	default:
		t.Fatal("expected send_ack")
	}
}

func TestHandleClientStreamChunkNoReplyStillRequiresEventOwnership(t *testing.T) {
	handler := &mockStreamChunkHandler{}
	mgr := NewManager("", 30*time.Second, nil, handler.handle, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID: 100, ownerID: 200, clientID: "test",
		send: make(chan []byte, 64),
	}
	registerStreamChunkOwnership(t, mgr, "evt-stream-no-reply-foreign", "sess-stream-no-reply", 999, conn.ownerID)

	pkt := makePacket(t, protocol.CmdClientStreamChunk, 24, AgentStreamChunkPayload{
		EventID:      "evt-stream-no-reply-foreign",
		SessionID:    "sess-stream-no-reply",
		DeltaContent: "/no_reply",
		ChunkSeq:     1,
		ClientMsgID:  "cmsg-stream-no-reply-foreign",
	})

	mgr.handleClientStreamChunk(conn, pkt)

	if len(handler.calls) != 0 {
		t.Fatalf("stream handler should not be called for foreign no_reply, got %d calls", len(handler.calls))
	}
	select {
	case data := <-conn.send:
		var resp protocol.Packet
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.Cmd != protocol.CmdSendNack {
			t.Fatalf("expected send_nack, got=%s", resp.Cmd)
		}
		var nack SendNackPayload
		if err := json.Unmarshal(resp.Payload, &nack); err != nil {
			t.Fatalf("unmarshal nack payload: %v", err)
		}
		if nack.Code != 4003 {
			t.Fatalf("nack code=%d want=4003", nack.Code)
		}
	default:
		t.Fatal("expected send_nack")
	}
}

func TestHandleReactMsg_PropagatesRemoveOperation(t *testing.T) {
	handler := &mockReactMsgHandler{}
	mgr := NewManager("", 30*time.Second, nil, nil, nil, handler.handle)
	defer mgr.Shutdown()
	conn := &agentConn{agentID: 100, ownerID: 200, clientID: "test", send: make(chan []byte, 64)}

	pkt := makePacket(t, "react_msg", 47, ReactMsgPayload{
		SessionID: "sess-react",
		MsgID:     12345,
		Emoji:     "👍",
		Op:        "remove",
	})

	mgr.handleReactMsg(conn, pkt)

	if len(handler.calls) != 1 {
		t.Fatalf("handler call count=%d want=1", len(handler.calls))
	}
	if handler.calls[0].Op != "remove" {
		t.Fatalf("handler op=%q want=remove", handler.calls[0].Op)
	}

	select {
	case data := <-conn.send:
		var resp protocol.Packet
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.Cmd != "send_ack" {
			t.Fatalf("expected send_ack, got=%s", resp.Cmd)
		}
		var ack map[string]any
		if err := json.Unmarshal(resp.Payload, &ack); err != nil {
			t.Fatalf("unmarshal ack payload: %v", err)
		}
		if ack["op"] != "remove" {
			t.Fatalf("ack op=%v want=remove", ack["op"])
		}
	default:
		t.Fatal("expected send_ack")
	}
}

func TestHandleMediaUploadInit_Success(t *testing.T) {
	cleanup := setupQueuedAgentEventDBTest(t)
	defer cleanup()
	if err := store.DB.Create(&model.AgentAPIScope{
		AgentID: 100,
		Scope:   agentscope.ScopeMediaUpload,
	}).Error; err != nil {
		t.Fatalf("seed media.upload scope: %v", err)
	}

	handler := &mockMediaUploadInitHandler{
		result: &MediaUploadInitResult{
			UploadID:  "upload-1",
			UploadURL: "https://upload.example.com/demo.png",
			Method:    "PUT",
			MediaURL:  "https://cdn.example.com/demo.png",
		},
	}
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.SetMediaUploadInitHandler(handler.handle)
	conn := &agentConn{agentID: 100, ownerID: 200, clientID: "test", send: make(chan []byte, 64)}

	pkt := makePacket(t, "media_upload_init", 48, MediaUploadInitPayload{
		UploadID:  "upload-1",
		Name:      "demo.png",
		SizeBytes: 123,
		Mime:      "image/png",
	})

	mgr.handleMediaUploadInit(conn, pkt)

	if len(handler.calls) != 1 {
		t.Fatalf("handler call count=%d want=1", len(handler.calls))
	}
	if handler.calls[0].Name != "demo.png" {
		t.Fatalf("handler name=%q want=demo.png", handler.calls[0].Name)
	}

	select {
	case data := <-conn.send:
		var resp protocol.Packet
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.Cmd != "send_ack" {
			t.Fatalf("expected send_ack, got=%s", resp.Cmd)
		}
		var ack MediaUploadInitResult
		if err := json.Unmarshal(resp.Payload, &ack); err != nil {
			t.Fatalf("unmarshal ack payload: %v", err)
		}
		if ack.UploadID != "upload-1" || ack.UploadURL == "" || ack.MediaURL == "" {
			t.Fatalf("ack=%#v want upload result fields", ack)
		}
	default:
		t.Fatal("expected send_ack")
	}
}

func TestHandleMediaUploadInit_ScopeDenied(t *testing.T) {
	cleanup := setupQueuedAgentEventDBTest(t)
	defer cleanup()

	handler := &mockMediaUploadInitHandler{result: &MediaUploadInitResult{}}
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.SetMediaUploadInitHandler(handler.handle)
	conn := &agentConn{agentID: 101, ownerID: 200, clientID: "test", send: make(chan []byte, 64)}

	pkt := makePacket(t, "media_upload_init", 49, MediaUploadInitPayload{
		UploadID:  "upload-2",
		Name:      "demo.png",
		SizeBytes: 123,
		Mime:      "image/png",
	})

	mgr.handleMediaUploadInit(conn, pkt)

	if len(handler.calls) != 0 {
		t.Fatalf("handler should not be called without media.upload scope, got %d calls", len(handler.calls))
	}

	select {
	case data := <-conn.send:
		var resp protocol.Packet
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.Cmd != "send_nack" {
			t.Fatalf("expected send_nack, got=%s", resp.Cmd)
		}
		var nack SendNackPayload
		if err := json.Unmarshal(resp.Payload, &nack); err != nil {
			t.Fatalf("unmarshal nack payload: %v", err)
		}
		if nack.Code != 4003 {
			t.Fatalf("nack code=%d want=4003", nack.Code)
		}
	default:
		t.Fatal("expected send_nack")
	}
}

func TestHandleClientStreamChunk_MissingChunkSeq(t *testing.T) {
	handler := &mockStreamChunkHandler{}
	mgr := NewManager("", 30*time.Second, nil, handler.handle, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID: 100, ownerID: 200, clientID: "test",
		send: make(chan []byte, 64),
	}
	registerStreamChunkOwnership(t, mgr, "evt-missing-seq", "sess-1", conn.agentID, conn.ownerID)

	pkt := makePacket(t, "client_stream_chunk", 21, AgentStreamChunkPayload{
		EventID:      "evt-missing-seq",
		SessionID:    "sess-1",
		DeltaContent: "hello",
		IsFinish:     false,
		ClientMsgID:  "cmsg-missing-seq",
	})

	mgr.handleClientStreamChunk(conn, pkt)

	if len(handler.calls) != 0 {
		t.Fatalf("handler should not be called when chunk_seq is missing")
	}

	select {
	case data := <-conn.send:
		var resp protocol.Packet
		json.Unmarshal(data, &resp)
		if resp.Cmd != "send_nack" {
			t.Fatalf("expected send_nack, got=%s", resp.Cmd)
		}
		var nack SendNackPayload
		json.Unmarshal(resp.Payload, &nack)
		if nack.Msg != "chunk_seq required" {
			t.Fatalf("expected chunk_seq required nack, got=%q", nack.Msg)
		}
	default:
		t.Fatalf("expected send_nack to be sent")
	}

	if err := mgr.ensureEventOwnedBy("evt-missing-seq", conn.agentID); err != nil {
		t.Fatalf("one invalid chunk must not terminate the event, got=%v", err)
	}
	mgr.handleClientStreamChunk(conn, makePacket(t, "client_stream_chunk", 22, AgentStreamChunkPayload{
		EventID:      "evt-missing-seq",
		SessionID:    "sess-1",
		DeltaContent: "recovered",
		ChunkSeq:     1,
		IsFinish:     false,
		ClientMsgID:  "cmsg-missing-seq",
	}))
	if len(handler.calls) != 1 || handler.calls[0].DeltaContent != "recovered" {
		t.Fatalf("valid follow-up chunk should be accepted, calls=%+v", handler.calls)
	}
}

func TestHandleClientStreamChunk_ContinuesPastSoftCountThreshold(t *testing.T) {
	handler := &mockStreamChunkHandler{}
	mgr := NewManager("", 30*time.Second, nil, handler.handle, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID: 100, ownerID: 200, clientID: "test",
		send: make(chan []byte, 64),
	}
	eventID := "evt-long-kimi-thinking"
	sessionID := "sess-long-kimi-thinking"
	registerStreamChunkOwnership(t, mgr, eventID, sessionID, conn.agentID, conn.ownerID)
	mgr.resolvePendingEventAck(eventID, time.Now().UnixMilli())

	total := int64(protocol.StreamChunkCountWarnThreshold) + 2
	for seq := int64(1); seq <= total; seq++ {
		mgr.handleClientStreamChunk(conn, makePacket(t, protocol.CmdClientStreamChunk, seq, AgentStreamChunkPayload{
			EventID:      eventID,
			SessionID:    sessionID,
			DeltaContent: "x",
			ChunkSeq:     seq,
			ClientMsgID:  eventID + "_thinking",
			IsThinking:   true,
		}))
	}

	if got := int64(len(handler.calls)); got != total {
		t.Fatalf("accepted chunk count=%d want=%d", got, total)
	}
	if err := mgr.ensureEventOwnedBy(eventID, conn.agentID); err != nil {
		t.Fatalf("long stream must keep its pending event, got=%v", err)
	}
	select {
	case data := <-conn.send:
		var resp protocol.Packet
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("unmarshal unexpected response: %v", err)
		}
		t.Fatalf("long stream must not be rejected, got cmd=%s payload=%s", resp.Cmd, string(resp.Payload))
	default:
	}
}

func TestHandleClientStreamChunk_EmptyDeltaAcceptedOnFinish(t *testing.T) {
	handler := &mockStreamChunkHandler{}
	mgr := NewManager("", 30*time.Second, nil, handler.handle, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID: 100, ownerID: 200, clientID: "test",
		send: make(chan []byte, 64),
	}
	registerStreamChunkOwnership(t, mgr, "evt-empty-finish", "sess-1", conn.agentID, conn.ownerID)

	pkt := makePacket(t, "client_stream_chunk", 3, AgentStreamChunkPayload{
		EventID:      "evt-empty-finish",
		SessionID:    "sess-1",
		ThreadID:     "topic-stream-finish",
		DeltaContent: "",
		ChunkSeq:     1,
		IsFinish:     true,
		ClientMsgID:  "cmsg-finish",
	})

	mgr.handleClientStreamChunk(conn, pkt)

	if len(handler.calls) != 1 {
		t.Fatalf("handler should be called for empty finish chunk, got %d", len(handler.calls))
	}
}

func TestHandleSessionActivitySet_AcceptsTypingAlias(t *testing.T) {
	var calls []protocol.SessionActivitySetPayload
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.activityFn = func(_ context.Context, _ int64, _ int64, payload protocol.SessionActivitySetPayload) error {
		calls = append(calls, payload)
		return nil
	}

	conn := &agentConn{
		agentID:  100,
		ownerID:  200,
		clientID: "test",
		send:     make(chan []byte, 64),
	}

	pkt := makePacket(t, protocol.CmdSessionActivitySet, 7, protocol.SessionActivitySetPayload{
		SessionID: "sess-typing",
		Kind:      "typing",
		Active:    true,
		TTLMS:     8000,
	})

	mgr.handleSessionActivitySet(conn, pkt)

	if len(calls) != 1 {
		t.Fatalf("activity handler call count=%d want=1", len(calls))
	}
	if calls[0].Kind != protocol.SessionActivityKindComposing {
		t.Fatalf("activity kind=%q want=%q", calls[0].Kind, protocol.SessionActivityKindComposing)
	}
	select {
	case data := <-conn.send:
		t.Fatalf("typing alias should not produce nack, got=%s", string(data))
	default:
	}
}

func TestHandleClientStreamChunk_WhitespaceDeltaAllowedNonFinishNoAck(t *testing.T) {
	handler := &mockStreamChunkHandler{}
	mgr := NewManager("", 30*time.Second, nil, handler.handle, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID: 100, ownerID: 200, clientID: "test",
		send: make(chan []byte, 64),
	}
	registerStreamChunkOwnership(t, mgr, "evt-whitespace", "sess-1", conn.agentID, conn.ownerID)

	pkt := makePacket(t, "client_stream_chunk", 9, AgentStreamChunkPayload{
		EventID:      "evt-whitespace",
		SessionID:    "sess-1",
		DeltaContent: "\n\n",
		ChunkSeq:     1,
		IsFinish:     false,
		ClientMsgID:  "cmsg-whitespace",
	})

	mgr.handleClientStreamChunk(conn, pkt)

	if len(handler.calls) != 1 {
		t.Fatalf("handler should be called once for whitespace delta, got %d", len(handler.calls))
	}
	if handler.calls[0].DeltaContent != "\n\n" {
		t.Fatalf("handler delta_content mismatch, got=%q", handler.calls[0].DeltaContent)
	}

	select {
	case data := <-conn.send:
		t.Fatalf("non-finish whitespace chunk should not produce response, got=%s", string(data))
	default:
		// OK
	}
}

func TestHandleClientStreamChunk_IntermediateChunkNoAck(t *testing.T) {
	handler := &mockStreamChunkHandler{}
	mgr := NewManager("", 30*time.Second, nil, handler.handle, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID: 100, ownerID: 200, clientID: "test",
		send: make(chan []byte, 64),
	}
	registerStreamChunkOwnership(t, mgr, "evt-intermediate", "sess-1", conn.agentID, conn.ownerID)

	pkt := makePacket(t, "client_stream_chunk", 4, AgentStreamChunkPayload{
		EventID:      "evt-intermediate",
		SessionID:    "sess-1",
		DeltaContent: "some text",
		ChunkSeq:     1,
		IsFinish:     false,
		ClientMsgID:  "cmsg-mid",
	})

	mgr.handleClientStreamChunk(conn, pkt)

	if len(handler.calls) != 1 {
		t.Fatalf("handler should be called once, got %d", len(handler.calls))
	}
	if handler.calls[0].DeltaContent != "some text" {
		t.Fatalf("handler delta_content mismatch, got=%q", handler.calls[0].DeltaContent)
	}

	// Intermediate chunks should NOT receive ack.
	select {
	case data := <-conn.send:
		t.Fatalf("intermediate chunk should not produce any response, got=%s", string(data))
	default:
		// OK
	}
}

func TestHandleClientStreamChunk_HandlerError(t *testing.T) {
	handler := &mockStreamChunkHandler{
		err: &SendError{Code: 4003, Msg: "permission denied"},
	}
	mgr := NewManager("", 30*time.Second, nil, handler.handle, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID: 100, ownerID: 200, clientID: "test",
		send: make(chan []byte, 64),
	}
	registerStreamChunkOwnership(t, mgr, "evt-handler-err", "sess-1", conn.agentID, conn.ownerID)

	pkt := makePacket(t, "client_stream_chunk", 5, AgentStreamChunkPayload{
		EventID:      "evt-handler-err",
		SessionID:    "sess-1",
		DeltaContent: "hello",
		ChunkSeq:     1,
		IsFinish:     false,
		ClientMsgID:  "cmsg-err",
	})

	mgr.handleClientStreamChunk(conn, pkt)

	select {
	case data := <-conn.send:
		var resp protocol.Packet
		json.Unmarshal(data, &resp)
		if resp.Cmd != "send_nack" {
			t.Fatalf("expected send_nack on handler error, got=%s", resp.Cmd)
		}
		var nack SendNackPayload
		json.Unmarshal(resp.Payload, &nack)
		if nack.Code != 4003 {
			t.Fatalf("expected code=4003, got=%d", nack.Code)
		}
		if nack.Msg != "permission denied" {
			t.Fatalf("expected msg='permission denied', got=%q", nack.Msg)
		}
	default:
		t.Fatalf("expected send_nack to be sent")
	}
}

func TestHandleClientStreamChunk_HandlerErrorKeepsPendingEventAndRun(t *testing.T) {
	handler := &mockStreamChunkHandler{
		err: &SendError{Code: 4003, Msg: "group is muted"},
	}
	mgr := NewManager("", 30*time.Second, nil, handler.handle, nil, nil)
	defer mgr.Shutdown()
	statusCh := make(chan protocol.AgentDeliveryStatusPayload, 8)
	outputCh := make(chan protocol.AgentOutputStatusPayload, 8)
	mgr.SetDeliveryStatusHandler(func(payload protocol.AgentDeliveryStatusPayload) {
		statusCh <- payload
	})
	mgr.SetOutputStatusHandler(func(payload protocol.AgentOutputStatusPayload) {
		outputCh <- payload
	})

	event := DelegateEventPayload{
		EventID:     "evt-stream-fail",
		EventType:   "group_mention",
		AgentID:     100,
		OwnerID:     200,
		SessionID:   "sess-1",
		SessionType: 2,
		MsgID:       300,
		SenderID:    200,
		Content:     "@agent hello",
	}
	mgr.registerPendingEventAck(event, 1)
	mgr.registerActiveRun(event)

	if first := <-statusCh; first.Status != protocol.AgentDeliveryStatusQueued {
		t.Fatalf("first status=%q want=%q", first.Status, protocol.AgentDeliveryStatusQueued)
	}
	if firstOutput := <-outputCh; firstOutput.State != protocol.AgentOutputStateQueued {
		t.Fatalf("first output state=%q want=%q", firstOutput.State, protocol.AgentOutputStateQueued)
	}

	mgr.resolvePendingEventAck(event.EventID, time.Now().UnixMilli())
	if second := <-statusCh; second.Status != protocol.AgentDeliveryStatusReceived {
		t.Fatalf("second status=%q want=%q", second.Status, protocol.AgentDeliveryStatusReceived)
	}
	if secondOutput := <-outputCh; secondOutput.State != protocol.AgentOutputStateReceived {
		t.Fatalf("second output state=%q want=%q", secondOutput.State, protocol.AgentOutputStateReceived)
	}

	conn := &agentConn{
		agentID:  event.AgentID,
		ownerID:  event.OwnerID,
		clientID: "test",
		send:     make(chan []byte, 64),
	}
	pkt := makePacket(t, protocol.CmdClientStreamChunk, 10, AgentStreamChunkPayload{
		EventID:      event.EventID,
		SessionID:    event.SessionID,
		DeltaContent: "hello",
		ChunkSeq:     1,
		ClientMsgID:  "stream-fail-msg",
	})

	mgr.handleClientStreamChunk(conn, pkt)

	if len(handler.calls) != 1 {
		t.Fatalf("handler call count=%d want=1", len(handler.calls))
	}
	select {
	case data := <-conn.send:
		var resp protocol.Packet
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.Cmd != protocol.CmdSendNack {
			t.Fatalf("expected send_nack, got=%s", resp.Cmd)
		}
		var nack SendNackPayload
		if err := json.Unmarshal(resp.Payload, &nack); err != nil {
			t.Fatalf("unmarshal nack payload: %v", err)
		}
		if nack.Msg != "group is muted" {
			t.Fatalf("nack msg=%q want=%q", nack.Msg, "group is muted")
		}
	default:
		t.Fatalf("expected send_nack to be sent")
	}

	select {
	case status := <-statusCh:
		t.Fatalf("packet-level send_nack must not emit terminal delivery status, got=%#v", status)
	case <-time.After(50 * time.Millisecond):
	}
	select {
	case output := <-outputCh:
		t.Fatalf("packet-level send_nack must not emit terminal output status, got=%#v", output)
	case <-time.After(50 * time.Millisecond):
	}
	if snapshot := mgr.LookupActiveRunBySessionOwner(event.OwnerID, event.SessionID); snapshot == nil {
		t.Fatal("expected run to remain active after one rejected stream chunk")
	}

	mgr.resolvePendingEventResult(EventResultPayload{
		EventID:   event.EventID,
		Status:    protocol.AgentEventResultResponded,
		UpdatedAt: time.Now().UnixMilli(),
	})

	if status := <-statusCh; status.Status != protocol.AgentDeliveryStatusResponded {
		t.Fatalf("explicit event result status=%q want=%q", status.Status, protocol.AgentDeliveryStatusResponded)
	}
	if output := <-outputCh; output.State != protocol.AgentOutputStateCompleted {
		t.Fatalf("explicit event result output state=%q want=%q", output.State, protocol.AgentOutputStateCompleted)
	}
}

func TestPushDelegateEvent_UsesMsgIDField(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:  9992,
		ownerID:  1001,
		clientID: "test",
		send:     make(chan []byte, 1),
	}
	mgr.putConnForTest(conn)

	ok := mgr.PushDelegateEvent(DelegateEventPayload{
		EventID:         "g_3001:1001:18889990222",
		EventType:       "group_mention",
		AgentID:         9992,
		OwnerID:         1001,
		SessionID:       "g_3001",
		SessionType:     2,
		MsgID:           18889990222,
		QuotedMessageID: 18889990111,
		SenderID:        2003,
		Content:         "@1001 请确认下周排班",
		CreatedAt:       1704067202000,
	})
	if !ok {
		t.Fatalf("PushDelegateEvent should succeed")
	}

	select {
	case data := <-conn.send:
		if strings.Contains(string(data), "trigger_msg_id") {
			t.Fatalf("event_msg should not contain trigger_msg_id: %s", string(data))
		}
		if !strings.Contains(string(data), "\"msg_id\":\"18889990222\"") {
			t.Fatalf("event_msg should contain msg_id: %s", string(data))
		}
		var pkt protocol.Packet
		if err := json.Unmarshal(data, &pkt); err != nil {
			t.Fatalf("unmarshal packet: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload["msg_id"] != "18889990222" {
			t.Fatalf("msg_id mismatch: got=%v", payload["msg_id"])
		}
		if payload["quoted_message_id"] != "18889990111" {
			t.Fatalf("quoted_message_id mismatch: got=%v", payload["quoted_message_id"])
		}
	default:
		t.Fatalf("expected event_msg to be queued")
	}
}

func TestPushDelegateEvent_RecordOnlyUsesAckWithoutStatusesOrActiveRun(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	statusCh := make(chan protocol.AgentDeliveryStatusPayload, 4)
	outputCh := make(chan protocol.AgentOutputStatusPayload, 4)
	mgr.SetDeliveryStatusHandler(func(payload protocol.AgentDeliveryStatusPayload) {
		statusCh <- payload
	})
	mgr.SetOutputStatusHandler(func(payload protocol.AgentOutputStatusPayload) {
		outputCh <- payload
	})

	conn := &agentConn{
		agentID:  9994,
		ownerID:  1001,
		clientID: "test-record-only",
		send:     make(chan []byte, 4),
	}
	mgr.putConnForTest(conn)

	event := DelegateEventPayload{
		EventID:     "g_3001:1001:9994:18889990225:mirror",
		EventType:   "group_message",
		MirrorMode:  MirrorModeRecordOnly,
		AgentID:     9994,
		OwnerID:     1001,
		SessionID:   "g_3001",
		SessionType: 2,
		MsgID:       18889990225,
		SenderID:    2003,
		Content:     "只记录，不处理",
		CreatedAt:   1704067202000,
	}
	if ok := mgr.PushDelegateEvent(event); !ok {
		t.Fatalf("PushDelegateEvent should succeed")
	}

	select {
	case data := <-conn.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(data, &pkt); err != nil {
			t.Fatalf("unmarshal packet: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload["mirror_mode"] != MirrorModeRecordOnly {
			t.Fatalf("mirror_mode=%v want=%q", payload["mirror_mode"], MirrorModeRecordOnly)
		}
	default:
		t.Fatalf("expected event_msg to be queued")
	}

	select {
	case status := <-statusCh:
		t.Fatalf("did not expect delivery status for record_only event, got=%#v", status)
	case <-time.After(50 * time.Millisecond):
	}
	select {
	case output := <-outputCh:
		t.Fatalf("did not expect output status for record_only event, got=%#v", output)
	case <-time.After(50 * time.Millisecond):
	}
	if snapshot := mgr.LookupActiveRunBySessionOwner(event.OwnerID, event.SessionID); snapshot != nil {
		t.Fatalf("record_only event should not register active run, got=%+v", snapshot)
	}

	mgr.handleEventAck(conn, makePacket(t, protocol.CmdEventAck, 100, EventAckPayload{
		EventID:    event.EventID,
		SessionID:  event.SessionID,
		MsgID:      event.MsgID,
		ReceivedAt: time.Now().UnixMilli(),
	}))

	select {
	case status := <-statusCh:
		t.Fatalf("did not expect delivery status after record_only ack, got=%#v", status)
	case <-time.After(50 * time.Millisecond):
	}
	select {
	case output := <-outputCh:
		t.Fatalf("did not expect output status after record_only ack, got=%#v", output)
	case <-time.After(50 * time.Millisecond):
	}
	if _, ok := mgr.pending[event.EventID]; ok {
		t.Fatalf("record_only event should clear pending ack after event_ack")
	}
}

func TestLookupActiveRunStatusBySessionOwnerReturnsCurrentSnapshot(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	event := DelegateEventPayload{
		EventID:   "evt-status-1",
		AgentID:   9001,
		OwnerID:   2001,
		SessionID: "session-status-1",
		MsgID:     7001,
	}

	mgr.registerActiveRun(event)
	mgr.MarkRunReceived(event.EventID)
	mgr.MarkRunStreaming(event.EventID, 8001)

	status, ok := mgr.LookupActiveRunStatusBySessionOwner(
		event.OwnerID,
		event.SessionID,
	)
	if !ok {
		t.Fatal("expected active run status snapshot")
	}
	if status.RunID != event.EventID {
		t.Fatalf("run_id=%q want=%q", status.RunID, event.EventID)
	}
	if status.SessionID != event.SessionID {
		t.Fatalf("session_id=%q want=%q", status.SessionID, event.SessionID)
	}
	if status.AgentID != event.AgentID {
		t.Fatalf("agent_id=%d want=%d", status.AgentID, event.AgentID)
	}
	if status.TriggerMsgID != event.MsgID {
		t.Fatalf("trigger_msg_id=%d want=%d", status.TriggerMsgID, event.MsgID)
	}
	if status.StreamMsgID != 8001 {
		t.Fatalf("stream_msg_id=%d want=%d", status.StreamMsgID, 8001)
	}
	if status.State != protocol.AgentOutputStateStreaming {
		t.Fatalf("state=%q want=%q", status.State, protocol.AgentOutputStateStreaming)
	}
	if !status.CanStop {
		t.Fatal("expected can_stop=true")
	}
	if status.UpdatedAt <= 0 {
		t.Fatalf("updated_at=%d want > 0", status.UpdatedAt)
	}
}

func TestPushDelegateEvent_IncludesStructuredMessagePayload(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:  9993,
		ownerID:  1001,
		clientID: "test-structured",
		send:     make(chan []byte, 1),
	}
	mgr.putConnForTest(conn)

	event := DelegateEventPayload{
		EventID:         "evt-structured-1",
		EventType:       "group_message",
		AgentID:         9993,
		OwnerID:         1001,
		SessionID:       "g_3002",
		SessionType:     2,
		MsgID:           18889990333,
		QuotedMessageID: 18889990222,
		SenderID:        2004,
		Content:         "请看附件和卡片",
		CreatedAt:       1704067203000,
	}
	ApplyStructuredMessagePayload(&event, 2, json.RawMessage(`{
		"attachments":[
			{
				"media_url":"https://cdn.example.com/demo.png",
				"attachment_type":"image",
				"file_name":"demo.png",
				"content_type":"image/png"
			}
		],
		"biz_card":{"version":1,"type":"exec_status","payload":{"status":"running"}},
		"channel_data":{"grix":{"execStatus":{"status":"running"}}}
	}`))

	if ok := mgr.PushDelegateEvent(event); !ok {
		t.Fatalf("PushDelegateEvent should succeed")
	}

	select {
	case data := <-conn.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(data, &pkt); err != nil {
			t.Fatalf("unmarshal packet: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload["msg_type"] != float64(2) {
			t.Fatalf("msg_type mismatch: got=%v want=2", payload["msg_type"])
		}
		if _, ok := payload["extra"].(map[string]any); !ok {
			t.Fatalf("extra should be an object: %#v", payload["extra"])
		}
		attachments, ok := payload["attachments"].([]any)
		if !ok || len(attachments) != 1 {
			t.Fatalf("attachments mismatch: %#v", payload["attachments"])
		}
		firstAttachment, ok := attachments[0].(map[string]any)
		if !ok {
			t.Fatalf("attachment payload should be an object: %#v", attachments[0])
		}
		if firstAttachment["media_url"] != "https://cdn.example.com/demo.png" {
			t.Fatalf("attachment media_url mismatch: %#v", firstAttachment)
		}
		if _, ok := payload["biz_card"].(map[string]any); !ok {
			t.Fatalf("biz_card should be an object: %#v", payload["biz_card"])
		}
		if _, ok := payload["channel_data"].(map[string]any); !ok {
			t.Fatalf("channel_data should be an object: %#v", payload["channel_data"])
		}
	default:
		t.Fatalf("expected event_msg to be queued")
	}
}

func TestPushDelegateEvent_UsesAdapterNormalizedPayload(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	adapter := &recordingOutboundAdapter{
		cmd:     "event_msg",
		payload: json.RawMessage(`{"normalized":true,"source":"adapter"}`),
	}
	conn := &agentConn{
		agentID:   9994,
		ownerID:   1001,
		clientID:  "test-adapter-payload",
		send:      make(chan []byte, 1),
		adapter:   adapter,
		adapterID: adapter.AdapterID(),
	}
	mgr.putConnForTest(conn)

	event := DelegateEventPayload{
		EventID:         "evt-adapter-payload-1",
		EventType:       "group_message",
		AgentID:         9994,
		OwnerID:         1001,
		SessionID:       "g_3003",
		SessionType:     2,
		MsgID:           18889990444,
		QuotedMessageID: 18889990333,
		SenderID:        2005,
		MsgType:         2,
		Content:         "请按适配层规范发送",
		CreatedAt:       1704067204000,
	}
	ApplyStructuredMessagePayload(&event, 2, json.RawMessage(`{
		"thread_id":"topic-a",
		"attachments":[
			{
				"media_url":"https://cdn.example.com/adapter.png",
				"attachment_type":"image",
				"file_name":"adapter.png",
				"content_type":"image/png"
			}
		],
		"biz_card":{"version":1,"type":"agent_status","payload":{"status":"running"}},
		"channel_data":{"grix":{"execStatus":{"status":"running"}}}
	}`))

	if ok := mgr.PushDelegateEvent(event); !ok {
		t.Fatalf("PushDelegateEvent should succeed")
	}
	if adapter.lastOutbound == nil {
		t.Fatal("expected adapter to receive normalized outbound event")
	}
	if adapter.lastOutbound.EventID != event.EventID {
		t.Fatalf("adapter event_id=%q want=%q", adapter.lastOutbound.EventID, event.EventID)
	}
	if adapter.lastOutbound.MsgID != event.MsgID {
		t.Fatalf("adapter msg_id=%d want=%d", adapter.lastOutbound.MsgID, event.MsgID)
	}
	if adapter.lastOutbound.ThreadID != "topic-a" {
		t.Fatalf("adapter thread_id=%q want=topic-a", adapter.lastOutbound.ThreadID)
	}
	if len(adapter.lastOutbound.Attachments) != 1 {
		t.Fatalf("adapter attachments=%d want=1", len(adapter.lastOutbound.Attachments))
	}
	if adapter.lastOutbound.Attachments[0].MediaURL != "https://cdn.example.com/adapter.png" {
		t.Fatalf("adapter attachment media_url mismatch: %#v", adapter.lastOutbound.Attachments[0])
	}
	if len(adapter.lastOutbound.BizCard) == 0 {
		t.Fatal("expected adapter biz_card payload")
	}
	if len(adapter.lastOutbound.ChannelData) == 0 {
		t.Fatal("expected adapter channel_data payload")
	}

	select {
	case data := <-conn.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(data, &pkt); err != nil {
			t.Fatalf("unmarshal packet: %v", err)
		}
		if pkt.Cmd != "event_msg" {
			t.Fatalf("packet cmd=%s want=event_msg", pkt.Cmd)
		}
		var payload map[string]any
		if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
			t.Fatalf("unmarshal adapter payload: %v", err)
		}
		if payload["normalized"] != true {
			t.Fatalf("expected adapter payload to be used, got %#v", payload)
		}
		if payload["source"] != "adapter" {
			t.Fatalf("expected adapter marker, got %#v", payload)
		}
		if _, exists := payload["event_id"]; exists {
			t.Fatalf("expected packet payload from adapter, got delegate payload %#v", payload)
		}
	default:
		t.Fatalf("expected event_msg to be queued")
	}
}

func TestResolveDelegateOutbound_GeminiReusesStoredSessionContext(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = originalDB
	})

	conn := &agentConn{
		agentID:   91021,
		ownerID:   82031,
		clientID:  "grix-gemini",
		send:      make(chan []byte, 1),
		adapter:   geminiadapter.NewAdapter(),
		adapterID: geminiadapter.AdapterID,
	}

	firstCmd, firstPayload := conn.resolveDelegateOutbound(DelegateEventPayload{
		EventID:   "evt-gemini-context-1",
		EventType: "user_chat",
		AgentID:   conn.agentID,
		OwnerID:   conn.ownerID,
		SessionID: "gemini-session-context-1",
		MsgID:     18889994001,
		SenderID:  conn.ownerID,
		Content:   "first turn",
		Extra: json.RawMessage(`{
			"acp":{
				"cwd":"/workspace/gemini-context",
				"mode_id":"plan",
				"model_id":"gemini-2.5-pro"
			}
		}`),
	})
	if firstCmd != "event_msg" {
		t.Fatalf("first cmd=%q want=event_msg", firstCmd)
	}

	var firstDecoded struct {
		Extra json.RawMessage `json:"extra"`
	}
	firstRaw, ok := firstPayload.(json.RawMessage)
	if !ok {
		t.Fatalf("first payload type=%T want json.RawMessage", firstPayload)
	}
	if err := json.Unmarshal(firstRaw, &firstDecoded); err != nil {
		t.Fatalf("unmarshal first payload: %v", err)
	}
	var firstExtra struct {
		ACP struct {
			Cwd     string `json:"cwd"`
			ModeID  string `json:"mode_id"`
			ModelID string `json:"model_id"`
		} `json:"acp"`
	}
	if err := json.Unmarshal(firstDecoded.Extra, &firstExtra); err != nil {
		t.Fatalf("unmarshal first extra: %v", err)
	}
	if firstExtra.ACP.Cwd != "/workspace/gemini-context" {
		t.Fatalf("first acp.cwd=%q want=/workspace/gemini-context", firstExtra.ACP.Cwd)
	}

	stored, ok, err := geminisession.Load(context.Background(), conn.agentID, "gemini-session-context-1")
	if err != nil {
		t.Fatalf("load stored gemini session context: %v", err)
	}
	if !ok {
		t.Fatal("expected gemini session context to be stored")
	}
	if stored.ModeID != "plan" {
		t.Fatalf("stored mode_id=%q want=plan", stored.ModeID)
	}
	if stored.ModelID != "gemini-2.5-pro" {
		t.Fatalf("stored model_id=%q want=gemini-2.5-pro", stored.ModelID)
	}

	secondCmd, secondPayload := conn.resolveDelegateOutbound(DelegateEventPayload{
		EventID:   "evt-gemini-context-2",
		EventType: "user_chat",
		AgentID:   conn.agentID,
		OwnerID:   conn.ownerID,
		SessionID: "gemini-session-context-1",
		MsgID:     18889994002,
		SenderID:  conn.ownerID,
		Content:   "second turn",
	})
	if secondCmd != "event_msg" {
		t.Fatalf("second cmd=%q want=event_msg", secondCmd)
	}

	var secondDecoded struct {
		Content string          `json:"content"`
		Extra   json.RawMessage `json:"extra"`
	}
	secondRaw, ok := secondPayload.(json.RawMessage)
	if !ok {
		t.Fatalf("second payload type=%T want json.RawMessage", secondPayload)
	}
	if err := json.Unmarshal(secondRaw, &secondDecoded); err != nil {
		t.Fatalf("unmarshal second payload: %v", err)
	}
	var secondExtra struct {
		ACP struct {
			Cwd     string `json:"cwd"`
			ModeID  string `json:"mode_id"`
			ModelID string `json:"model_id"`
		} `json:"acp"`
	}
	if err := json.Unmarshal(secondDecoded.Extra, &secondExtra); err != nil {
		t.Fatalf("unmarshal second extra: %v", err)
	}
	if secondDecoded.Content != "second turn" {
		t.Fatalf("second content=%q want=second turn", secondDecoded.Content)
	}
	if secondExtra.ACP.Cwd != "" {
		t.Fatalf("second acp.cwd=%q want empty (CWD is managed by session_control, not DB)", secondExtra.ACP.Cwd)
	}
	if secondExtra.ACP.ModeID != "plan" {
		t.Fatalf("second acp.mode_id=%q want=plan", secondExtra.ACP.ModeID)
	}
	if secondExtra.ACP.ModelID != "gemini-2.5-pro" {
		t.Fatalf("second acp.model_id=%q want=gemini-2.5-pro", secondExtra.ACP.ModelID)
	}
}

func TestPushDelegateEvent_CodexOutboundPreservesAuditExtra(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:   9996,
		ownerID:   1001,
		clientID:  "test-codex-audit",
		send:      make(chan []byte, 1),
		adapter:   codexadapter.NewAdapter(),
		adapterID: codexadapter.AdapterID,
	}
	mgr.putConnForTest(conn)

	event := DelegateEventPayload{
		EventID:     "sess-codex-audit:1001:9996:18889990666",
		EventType:   "user_chat",
		AgentID:     9996,
		OwnerID:     1001,
		SessionID:   "sess-codex-audit",
		SessionType: 1,
		MsgID:       18889990666,
		SenderID:    1001,
		MsgType:     1,
		Content:     "hello",
		Extra:       json.RawMessage(`{"audit":{"enabled":true,"scope":"turn"},"client_probe":"front_toolbar"}`),
		CreatedAt:   1704067206000,
	}

	if ok := mgr.PushDelegateEvent(event); !ok {
		t.Fatalf("PushDelegateEvent should succeed")
	}

	select {
	case data := <-conn.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(data, &pkt); err != nil {
			t.Fatalf("unmarshal packet: %v", err)
		}
		if pkt.Cmd != protocol.CmdEventMsg {
			t.Fatalf("packet cmd=%q want=%q", pkt.Cmd, protocol.CmdEventMsg)
		}

		var payload struct {
			EventID string          `json:"event_id"`
			MsgID   int64           `json:"msg_id,string"`
			Extra   json.RawMessage `json:"extra"`
		}
		if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
			t.Fatalf("unmarshal event payload: %v", err)
		}
		if payload.EventID != event.EventID {
			t.Fatalf("event_id=%q want=%q", payload.EventID, event.EventID)
		}
		if payload.MsgID != event.MsgID {
			t.Fatalf("msg_id=%d want=%d", payload.MsgID, event.MsgID)
		}

		var extra struct {
			Audit struct {
				Enabled bool   `json:"enabled"`
				Scope   string `json:"scope"`
			} `json:"audit"`
			ClientProbe string `json:"client_probe"`
		}
		if err := json.Unmarshal(payload.Extra, &extra); err != nil {
			t.Fatalf("unmarshal extra: %v raw=%s", err, string(payload.Extra))
		}
		if !extra.Audit.Enabled || extra.Audit.Scope != "turn" {
			t.Fatalf("audit extra mismatch: enabled=%v scope=%q raw=%s", extra.Audit.Enabled, extra.Audit.Scope, string(payload.Extra))
		}
		if extra.ClientProbe != "front_toolbar" {
			t.Fatalf("client_probe=%q want=front_toolbar raw=%s", extra.ClientProbe, string(payload.Extra))
		}
	default:
		t.Fatal("expected Codex event_msg to be sent")
	}
}

func TestResolveDelegateOutbound_GroupSessionInjectsConnectorDropConfig(t *testing.T) {
	conn := &agentConn{}

	cmd, payload := conn.resolveDelegateOutbound(DelegateEventPayload{
		EventID:     "evt-group-connector-1",
		SessionID:   "sess-group-1",
		SessionType: 2,
		Content:     "group message",
		Extra: json.RawMessage(`{
			"foo":"bar",
			"connector":{"response_delivery":"stream","tool_events":"send","thinking_events":"send"}
		}`),
	})
	if cmd != "event_msg" {
		t.Fatalf("cmd=%q want=event_msg", cmd)
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var decoded struct {
		Extra map[string]any `json:"extra"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got := decoded.Extra["foo"]; got != "bar" {
		t.Fatalf("extra.foo=%v want=bar", got)
	}
	connector, _ := decoded.Extra["connector"].(map[string]any)
	if got := connector["response_delivery"]; got != "single_message" {
		t.Fatalf("connector.response_delivery=%v want=single_message", got)
	}
	if got := connector["tool_events"]; got != "drop" {
		t.Fatalf("connector.tool_events=%v want=drop", got)
	}
	if got := connector["thinking_events"]; got != "drop" {
		t.Fatalf("connector.thinking_events=%v want=drop", got)
	}
}

func TestResolveDelegateOutbound_PrivateSessionDoesNotInjectConnectorDropConfig(t *testing.T) {
	conn := &agentConn{}

	cmd, payload := conn.resolveDelegateOutbound(DelegateEventPayload{
		EventID:     "evt-private-connector-1",
		SessionID:   "sess-private-1",
		SessionType: 1,
		Content:     "private message",
	})
	if cmd != "event_msg" {
		t.Fatalf("cmd=%q want=event_msg", cmd)
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var decoded struct {
		Extra map[string]any `json:"extra"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if decoded.Extra != nil {
		t.Fatalf("extra=%v want=nil for private session", decoded.Extra)
	}
}

func TestResolveDelegateOutbound_PrivateInternalEventInjectsConnectorDropConfig(t *testing.T) {
	conn := &agentConn{}

	cmd, payload := conn.resolveDelegateOutbound(DelegateEventPayload{
		EventID:     "customer_coach:1001:client_open:1",
		EventType:   "customer_coach_snapshot",
		SessionID:   "sess-private-coach-1",
		SessionType: 1,
		Content:     "internal snapshot",
		Extra:       json.RawMessage(`{"foo":"bar","connector":{"response_delivery":"stream","tool_events":"send","thinking_events":"send"}}`),
	})
	if cmd != "event_msg" {
		t.Fatalf("cmd=%q want=event_msg", cmd)
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var decoded struct {
		Extra map[string]any `json:"extra"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got := decoded.Extra["foo"]; got != "bar" {
		t.Fatalf("extra.foo=%v want=bar", got)
	}
	connector, _ := decoded.Extra["connector"].(map[string]any)
	if got := connector["response_delivery"]; got != "single_message" {
		t.Fatalf("connector.response_delivery=%v want=single_message", got)
	}
	if got := connector["tool_events"]; got != "drop" {
		t.Fatalf("connector.tool_events=%v want=drop", got)
	}
	if got := connector["thinking_events"]; got != "drop" {
		t.Fatalf("connector.thinking_events=%v want=drop", got)
	}
}

func TestResolveDelegateOutbound_PrivateCommandInjectsConnectorDropConfig(t *testing.T) {
	conn := &agentConn{}

	_, payload := conn.resolveDelegateOutbound(DelegateEventPayload{
		EventID:     "toolbar_cmd:1001:1",
		EventType:   "user_chat",
		SessionID:   "sess-private-command-1",
		SessionType: 1,
		Content:     "/stop",
		Command:     true,
	})

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var decoded struct {
		Content string         `json:"content"`
		Extra   map[string]any `json:"extra"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	connector, _ := decoded.Extra["connector"].(map[string]any)
	if got := connector["response_delivery"]; got != "single_message" {
		t.Fatalf("connector.response_delivery=%v want=single_message", got)
	}
	if got := connector["tool_events"]; got != "drop" {
		t.Fatalf("connector.tool_events=%v want=drop", got)
	}
	if got := connector["thinking_events"]; got != "drop" {
		t.Fatalf("connector.thinking_events=%v want=drop", got)
	}
	if decoded.Content != "/stop" {
		t.Fatalf("content=%q want exact /stop command", decoded.Content)
	}
}

func TestResolveDelegateOutbound_UserContentCannotEnableNoReplyProtocol(t *testing.T) {
	conn := &agentConn{}

	_, payload := conn.resolveDelegateOutbound(DelegateEventPayload{
		EventID:     "evt-private-user-guide-1",
		EventType:   "user_chat",
		SessionID:   "sess-private-user-guide-1",
		SessionType: 1,
		Content:     "请帮我写一份新手引导，并说明哪些段落无需回复。",
	})

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var decoded struct {
		Content string         `json:"content"`
		Extra   map[string]any `json:"extra"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if decoded.Extra != nil {
		t.Fatalf("extra=%v want=nil for ordinary user event", decoded.Extra)
	}
	if strings.Contains(decoded.Content, NoReplyProtocolInstruction) {
		t.Fatalf("ordinary user content unexpectedly received no_reply protocol: %q", decoded.Content)
	}
}

func TestNoReplyProtocolEventRequiresTrustedMetadata(t *testing.T) {
	tests := []struct {
		name string
		evt  DelegateEventPayload
		want bool
	}{
		{
			name: "profile update event id",
			evt:  DelegateEventPayload{EventID: "profile-update:1001:2001:3001", EventType: "user_chat"},
			want: true,
		},
		{
			name: "customer coach event type",
			evt:  DelegateEventPayload{EventID: "evt-coach-1", EventType: "customer_coach_snapshot"},
			want: true,
		},
		{
			name: "ordinary user content",
			evt: DelegateEventPayload{
				EventID:   "evt-user-1",
				EventType: "user_chat",
				Content:   "请帮我写一份新手引导，并说明哪些段落无需回复。",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNoReplyProtocolEvent(tt.evt); got != tt.want {
				t.Fatalf("isNoReplyProtocolEvent()=%v want=%v event=%+v", got, tt.want, tt.evt)
			}
		})
	}
}

func TestResolveDelegateOutbound_GroupSessionInjectsDropConfigBeforeAdapterNormalize(t *testing.T) {
	adapter := &recordingOutboundAdapter{
		cmd:     "event_msg",
		payload: json.RawMessage(`{"normalized":true}`),
	}
	conn := &agentConn{
		adapter: adapter,
	}

	cmd, payload := conn.resolveDelegateOutbound(DelegateEventPayload{
		EventID:     "evt-group-adapter-1",
		SessionID:   "sess-group-adapter-1",
		SessionType: 2,
		Content:     "group adapter message",
		Extra:       json.RawMessage(`{"connector":{"response_delivery":"stream"}}`),
	})
	if cmd != "event_msg" {
		t.Fatalf("cmd=%q want=event_msg", cmd)
	}
	if adapter.lastOutbound == nil {
		t.Fatal("expected adapter NormalizeOutbound to be called")
	}

	var extra map[string]any
	if err := json.Unmarshal(adapter.lastOutbound.Extra, &extra); err != nil {
		t.Fatalf("unmarshal adapter outbound extra: %v", err)
	}
	connector, _ := extra["connector"].(map[string]any)
	if got := connector["response_delivery"]; got != "single_message" {
		t.Fatalf("connector.response_delivery=%v want=single_message", got)
	}
	if got := connector["tool_events"]; got != "drop" {
		t.Fatalf("connector.tool_events=%v want=drop", got)
	}
	if got := connector["thinking_events"]; got != "drop" {
		t.Fatalf("connector.thinking_events=%v want=drop", got)
	}

	if raw, ok := payload.(json.RawMessage); !ok || string(raw) != `{"normalized":true}` {
		t.Fatalf("payload=%v want adapter payload", payload)
	}
}

func TestPushDelegateEvent_QueuesWhenChannelUnavailableAndDrainsOnRegister(t *testing.T) {
	previousRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = previousRDB
	}()

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	statuses := make([]protocol.AgentDeliveryStatusPayload, 0, 2)
	mgr.SetDeliveryStatusHandler(func(payload protocol.AgentDeliveryStatusPayload) {
		statuses = append(statuses, payload)
	})

	event := DelegateEventPayload{
		EventID:     "evt-queued-drain-1",
		EventType:   "user_chat",
		AgentID:     99001,
		OwnerID:     1001,
		SessionID:   "u_1001_u_2001",
		ThreadID:    "topic-a",
		SessionType: 1,
		MsgID:       20001,
		SenderID:    1001,
		Content:     "queue then deliver",
		CreatedAt:   1704067202000,
	}
	if ok := mgr.PushDelegateEvent(event); !ok {
		t.Fatal("PushDelegateEvent should queue when channel unavailable")
	}
	if len(statuses) != 0 {
		t.Fatalf("unavailable queueing should not emit immediate status, got=%#v", statuses)
	}

	conn := &agentConn{
		agentID:  event.AgentID,
		ownerID:  event.OwnerID,
		clientID: "agent-drain",
		send:     make(chan []byte, 4),
	}
	mgr.register(conn)

	select {
	case data := <-conn.send:
		var packet protocol.Packet
		if err := json.Unmarshal(data, &packet); err != nil {
			t.Fatalf("unmarshal queued packet: %v", err)
		}
		if packet.Cmd != "event_msg" {
			t.Fatalf("packet cmd=%s want=event_msg", packet.Cmd)
		}
		var payload map[string]any
		if err := json.Unmarshal(packet.Payload, &payload); err != nil {
			t.Fatalf("unmarshal queued payload: %v", err)
		}
		if payload["thread_id"] != "topic-a" {
			t.Fatalf("thread_id=%v want=topic-a", payload["thread_id"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected queued event to be drained after register")
	}

	if len(statuses) != 1 {
		t.Fatalf("expected one queued status after drain, got=%d", len(statuses))
	}
	if statuses[0].Status != protocol.AgentDeliveryStatusQueued {
		t.Fatalf("status=%q want=%q", statuses[0].Status, protocol.AgentDeliveryStatusQueued)
	}

	mgr.resolvePendingEventAck(event.EventID, time.Now().UnixMilli())
	mgr.resolvePendingEventResult(EventResultPayload{
		EventID: event.EventID,
		Status:  protocol.AgentEventResultResponded,
	})
}

func TestPushDelegateEvent_GlobalFallbackQueuesWithoutManager(t *testing.T) {
	previousRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = previousRDB
	}()

	previousManager := GetGlobal()
	SetGlobal(nil)
	defer SetGlobal(previousManager)

	event := DelegateEventPayload{
		EventID:     "evt-global-fallback-1",
		EventType:   "user_chat",
		AgentID:     99002,
		OwnerID:     1002,
		SessionID:   "u_1002_u_2002",
		SessionType: 1,
		MsgID:       20002,
		SenderID:    1002,
		Content:     "queue without manager",
		CreatedAt:   1704067202000,
	}
	if ok := PushDelegateEvent(event); !ok {
		t.Fatal("PushDelegateEvent should queue even when global manager is nil")
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	statuses := make([]protocol.AgentDeliveryStatusPayload, 0, 2)
	mgr.SetDeliveryStatusHandler(func(payload protocol.AgentDeliveryStatusPayload) {
		statuses = append(statuses, payload)
	})
	conn := &agentConn{
		agentID:  event.AgentID,
		ownerID:  event.OwnerID,
		clientID: "agent-fallback-drain",
		send:     make(chan []byte, 4),
	}
	mgr.register(conn)

	select {
	case data := <-conn.send:
		var packet protocol.Packet
		if err := json.Unmarshal(data, &packet); err != nil {
			t.Fatalf("unmarshal drained packet: %v", err)
		}
		if packet.Cmd != "event_msg" {
			t.Fatalf("packet cmd=%s want=event_msg", packet.Cmd)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected queued event to be drained after manager register")
	}

	if len(statuses) != 1 || statuses[0].Status != protocol.AgentDeliveryStatusQueued {
		t.Fatalf("queued status=%#v", statuses)
	}
	mgr.resolvePendingEventAck(event.EventID, time.Now().UnixMilli())
	mgr.resolvePendingEventResult(EventResultPayload{
		EventID: event.EventID,
		Status:  protocol.AgentEventResultResponded,
	})
}

func TestPushToAgent_QueuesOfflineRevokeAndDrainsOnRegister(t *testing.T) {
	cleanupDB := setupQueuedAgentEventDBTest(t)
	defer cleanupDB()

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	payload := map[string]any{
		"msg_id":       "18889990001",
		"session_id":   "agent-revoke-session-1",
		"thread_id":    "topic-revoke-1",
		"session_type": 1,
		"sender_id":    "9001",
		"is_revoked":   true,
	}
	if ok := mgr.PushToAgent(99123, 1001, "event_revoke", payload); !ok {
		t.Fatal("PushToAgent should queue revoke when agent is offline")
	}
	var queuedCount int64
	if err := store.DB.Model(&model.AgentQueuedEvent{}).
		Where("agent_id = ?", int64(99123)).
		Count(&queuedCount).Error; err != nil {
		t.Fatalf("count queued revoke events error: %v", err)
	}
	if queuedCount != 1 {
		t.Fatalf("queued revoke event count=%d want=1", queuedCount)
	}

	conn := &agentConn{
		agentID:   99123,
		ownerID:   1001,
		isPrimary: true,
		clientID:  "agent-revoke-drain",
		send:      make(chan []byte, 4),
	}
	mgr.register(conn)

	select {
	case data := <-conn.send:
		var packet protocol.Packet
		if err := json.Unmarshal(data, &packet); err != nil {
			t.Fatalf("unmarshal queued revoke packet: %v", err)
		}
		if packet.Cmd != "event_revoke" {
			t.Fatalf("packet cmd=%s want=event_revoke", packet.Cmd)
		}

		var decoded map[string]any
		if err := json.Unmarshal(packet.Payload, &decoded); err != nil {
			t.Fatalf("unmarshal queued revoke payload: %v", err)
		}
		if decoded["msg_id"] != "18889990001" {
			t.Fatalf("payload msg_id=%v want=18889990001", decoded["msg_id"])
		}
		if decoded["session_id"] != "agent-revoke-session-1" {
			t.Fatalf("payload session_id=%v want=agent-revoke-session-1", decoded["session_id"])
		}
		if decoded["thread_id"] != "topic-revoke-1" {
			t.Fatalf("payload thread_id=%v want=topic-revoke-1", decoded["thread_id"])
		}
		eventID, _ := decoded["event_id"].(string)
		if eventID == "" {
			t.Fatal("event_revoke payload should include event_id")
		}
		mgr.handleEventAck(conn, makePacket(t, protocol.CmdEventAck, 100, EventAckPayload{
			EventID:    eventID,
			ReceivedAt: time.Now().UnixMilli(),
		}))
	case <-time.After(2 * time.Second):
		t.Fatal("expected queued revoke event to drain after register")
	}
	if err := store.DB.Model(&model.AgentQueuedEvent{}).
		Where("agent_id = ?", int64(99123)).
		Count(&queuedCount).Error; err != nil {
		t.Fatalf("count queued revoke events after drain error: %v", err)
	}
	if queuedCount != 0 {
		t.Fatalf("queued revoke events should be ack-drained, got=%d", queuedCount)
	}
}

func TestPushToAgent_QueuesOfflineEditAndDrainsOnRegister(t *testing.T) {
	cleanupDB := setupQueuedAgentEventDBTest(t)
	defer cleanupDB()

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	payload := protocol.EditEventPayload{
		MsgID:       18889990011,
		SessionID:   "agent-edit-session-1",
		ThreadID:    "topic-edit-1",
		SessionType: 1,
		SenderID:    9001,
		SenderType:  2,
		MsgType:     1,
		Content:     "edited content",
		CreatedAt:   1704067205000,
	}
	if ok := mgr.PushToAgent(99127, 1005, protocol.CmdEventEdit, payload); !ok {
		t.Fatal("PushToAgent should queue edit when agent is offline")
	}

	var queuedCount int64
	if err := store.DB.Model(&model.AgentQueuedEvent{}).
		Where("agent_id = ?", int64(99127)).
		Count(&queuedCount).Error; err != nil {
		t.Fatalf("count queued edit events error: %v", err)
	}
	if queuedCount != 1 {
		t.Fatalf("queued edit event count=%d want=1", queuedCount)
	}

	conn := &agentConn{
		agentID:   99127,
		ownerID:   1005,
		isPrimary: true,
		clientID:  "agent-edit-drain",
		send:      make(chan []byte, 4),
	}
	mgr.register(conn)

	select {
	case data := <-conn.send:
		var packet protocol.Packet
		if err := json.Unmarshal(data, &packet); err != nil {
			t.Fatalf("unmarshal queued edit packet: %v", err)
		}
		if packet.Cmd != protocol.CmdEventEdit {
			t.Fatalf("packet cmd=%s want=%s", packet.Cmd, protocol.CmdEventEdit)
		}

		var decoded protocol.EditEventPayload
		if err := json.Unmarshal(packet.Payload, &decoded); err != nil {
			t.Fatalf("unmarshal queued edit payload: %v", err)
		}
		if decoded.MsgID != payload.MsgID {
			t.Fatalf("payload msg_id=%d want=%d", decoded.MsgID, payload.MsgID)
		}
		if decoded.SessionID != payload.SessionID {
			t.Fatalf("payload session_id=%q want=%q", decoded.SessionID, payload.SessionID)
		}
		if decoded.ThreadID != payload.ThreadID {
			t.Fatalf("payload thread_id=%q want=%q", decoded.ThreadID, payload.ThreadID)
		}
		if decoded.Content != payload.Content {
			t.Fatalf("payload content=%q want=%q", decoded.Content, payload.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected queued edit event to drain after register")
	}

	if err := store.DB.Model(&model.AgentQueuedEvent{}).
		Where("agent_id = ?", int64(99127)).
		Count(&queuedCount).Error; err != nil {
		t.Fatalf("count queued edit events after drain error: %v", err)
	}
	if queuedCount != 0 {
		t.Fatalf("queued edit events should be send-drained, got=%d", queuedCount)
	}
}

func TestPushToAgent_LiveOpenClawRevokeUsesAdapterPayload(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:   99126,
		ownerID:   1004,
		clientID:  "agent-revoke-live",
		send:      make(chan []byte, 4),
		adapter:   openclawadapter.NewAdapter(),
		adapterID: openclawadapter.AdapterID,
	}
	mgr.register(conn)

	payload := map[string]any{
		"msg_id":       "18889990004",
		"session_id":   "agent-revoke-session-live",
		"thread_id":    "topic-revoke-live",
		"session_type": 1,
		"sender_id":    "9004",
		"is_revoked":   true,
	}
	if ok := mgr.PushToAgent(99126, 1004, "event_revoke", payload); !ok {
		t.Fatal("PushToAgent should send live revoke to connected agent")
	}

	select {
	case data := <-conn.send:
		var packet protocol.Packet
		if err := json.Unmarshal(data, &packet); err != nil {
			t.Fatalf("unmarshal live revoke packet: %v", err)
		}
		if packet.Cmd != "event_revoke" {
			t.Fatalf("packet cmd=%s want=event_revoke", packet.Cmd)
		}

		var decoded protocol.AgentRevokeEventPayload
		if err := json.Unmarshal(packet.Payload, &decoded); err != nil {
			t.Fatalf("unmarshal live revoke payload: %v", err)
		}
		if decoded.EventID == "" {
			t.Fatal("live event_revoke payload should include event_id")
		}
		if decoded.ThreadID != "topic-revoke-live" {
			t.Fatalf("live thread_id=%q want=topic-revoke-live", decoded.ThreadID)
		}
		if decoded.SystemEvent == nil {
			t.Fatal("live event_revoke payload should include system_event")
		}
		if decoded.SystemEvent.Text != "Grix direct message deleted [session_id=agent-revoke-session-live msg_id=18889990004 sender_id=9004]" {
			t.Fatalf("system_event.text=%q", decoded.SystemEvent.Text)
		}
		if decoded.SystemEvent.ContextKey != "grix:revoke:agent-revoke-session-live:18889990004" {
			t.Fatalf("system_event.context_key=%q", decoded.SystemEvent.ContextKey)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected live revoke event to be delivered")
	}
}

func TestPushAgentEvent_GlobalFallbackPersistsOfflineRevoke(t *testing.T) {
	cleanupDB := setupQueuedAgentEventDBTest(t)
	defer cleanupDB()

	previousManager := GetGlobal()
	SetGlobal(nil)
	defer SetGlobal(previousManager)

	payload := map[string]any{
		"msg_id":       "18889990002",
		"session_id":   "agent-revoke-session-2",
		"thread_id":    "topic-revoke-2",
		"session_type": 1,
		"sender_id":    "9002",
		"is_revoked":   true,
	}
	if ok := PushAgentEvent(99124, 1002, "event_revoke", payload); !ok {
		t.Fatal("PushAgentEvent should persist revoke even when global manager is nil")
	}
	var queuedCount int64
	if err := store.DB.Model(&model.AgentQueuedEvent{}).
		Where("agent_id = ?", int64(99124)).
		Count(&queuedCount).Error; err != nil {
		t.Fatalf("count persisted revoke events error: %v", err)
	}
	if queuedCount != 1 {
		t.Fatalf("persisted revoke event count=%d want=1", queuedCount)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:   99124,
		ownerID:   1002,
		isPrimary: true,
		clientID:  "agent-revoke-global",
		send:      make(chan []byte, 4),
	}
	mgr.register(conn)

	select {
	case data := <-conn.send:
		var packet protocol.Packet
		if err := json.Unmarshal(data, &packet); err != nil {
			t.Fatalf("unmarshal persisted revoke packet: %v", err)
		}
		if packet.Cmd != "event_revoke" {
			t.Fatalf("packet cmd=%s want=event_revoke", packet.Cmd)
		}
		var decoded map[string]any
		if err := json.Unmarshal(packet.Payload, &decoded); err != nil {
			t.Fatalf("unmarshal persisted revoke payload: %v", err)
		}
		eventID, _ := decoded["event_id"].(string)
		if eventID == "" {
			t.Fatal("persisted event_revoke payload should include event_id")
		}
		if decoded["thread_id"] != "topic-revoke-2" {
			t.Fatalf("payload thread_id=%v want=topic-revoke-2", decoded["thread_id"])
		}
		mgr.handleEventAck(conn, makePacket(t, protocol.CmdEventAck, 101, EventAckPayload{
			EventID:    eventID,
			ReceivedAt: time.Now().UnixMilli(),
		}))
	case <-time.After(2 * time.Second):
		t.Fatal("expected persisted revoke event to drain after manager register")
	}
	if err := store.DB.Model(&model.AgentQueuedEvent{}).
		Where("agent_id = ?", int64(99124)).
		Count(&queuedCount).Error; err != nil {
		t.Fatalf("count persisted revoke events after drain error: %v", err)
	}
	if queuedCount != 0 {
		t.Fatalf("persisted revoke events should be drained, got=%d", queuedCount)
	}
}

func TestPushAgentEvent_GlobalFallbackPersistsOfflineEdit(t *testing.T) {
	cleanupDB := setupQueuedAgentEventDBTest(t)
	defer cleanupDB()

	previousManager := GetGlobal()
	SetGlobal(nil)
	defer SetGlobal(previousManager)

	payload := protocol.EditEventPayload{
		MsgID:       18889990012,
		SessionID:   "agent-edit-session-2",
		ThreadID:    "topic-edit-2",
		SessionType: 1,
		SenderID:    9002,
		SenderType:  2,
		MsgType:     1,
		Content:     "edited from global fallback",
		CreatedAt:   1704067206000,
	}
	if ok := PushAgentEvent(99128, 1006, protocol.CmdEventEdit, payload); !ok {
		t.Fatal("PushAgentEvent should persist edit even when global manager is nil")
	}

	var queuedCount int64
	if err := store.DB.Model(&model.AgentQueuedEvent{}).
		Where("agent_id = ?", int64(99128)).
		Count(&queuedCount).Error; err != nil {
		t.Fatalf("count persisted edit events error: %v", err)
	}
	if queuedCount != 1 {
		t.Fatalf("persisted edit event count=%d want=1", queuedCount)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:   99128,
		ownerID:   1006,
		isPrimary: true,
		clientID:  "agent-edit-global",
		send:      make(chan []byte, 4),
	}
	mgr.register(conn)

	select {
	case data := <-conn.send:
		var packet protocol.Packet
		if err := json.Unmarshal(data, &packet); err != nil {
			t.Fatalf("unmarshal persisted edit packet: %v", err)
		}
		if packet.Cmd != protocol.CmdEventEdit {
			t.Fatalf("packet cmd=%s want=%s", packet.Cmd, protocol.CmdEventEdit)
		}

		var decoded protocol.EditEventPayload
		if err := json.Unmarshal(packet.Payload, &decoded); err != nil {
			t.Fatalf("unmarshal persisted edit payload: %v", err)
		}
		if decoded.Content != payload.Content {
			t.Fatalf("payload content=%q want=%q", decoded.Content, payload.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected persisted edit event to drain after manager register")
	}

	if err := store.DB.Model(&model.AgentQueuedEvent{}).
		Where("agent_id = ?", int64(99128)).
		Count(&queuedCount).Error; err != nil {
		t.Fatalf("count persisted edit events after drain error: %v", err)
	}
	if queuedCount != 0 {
		t.Fatalf("persisted edit events should be drained, got=%d", queuedCount)
	}
}

func TestQueuedRevokeEvent_DeletesRecordAfterAckTimeout(t *testing.T) {
	cleanupDB := setupQueuedAgentEventDBTest(t)
	defer cleanupDB()

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.eventAckWait = 30 * time.Millisecond

	payload := map[string]any{
		"msg_id":       "18889990003",
		"session_id":   "agent-revoke-session-timeout",
		"session_type": 1,
		"sender_id":    "9003",
		"is_revoked":   true,
	}
	if ok := mgr.PushToAgent(99125, 1003, "event_revoke", payload); !ok {
		t.Fatal("PushToAgent should queue revoke when agent is offline")
	}

	conn := &agentConn{
		agentID:   99125,
		ownerID:   1003,
		isPrimary: true,
		clientID:  "agent-revoke-timeout",
		send:      make(chan []byte, 8),
	}
	mgr.register(conn)

	readEventID := func() string {
		t.Helper()
		select {
		case data := <-conn.send:
			var packet protocol.Packet
			if err := json.Unmarshal(data, &packet); err != nil {
				t.Fatalf("unmarshal queued revoke packet: %v", err)
			}
			if packet.Cmd != "event_revoke" {
				t.Fatalf("packet cmd=%s want=event_revoke", packet.Cmd)
			}
			var decoded map[string]any
			if err := json.Unmarshal(packet.Payload, &decoded); err != nil {
				t.Fatalf("unmarshal queued revoke payload: %v", err)
			}
			eventID, _ := decoded["event_id"].(string)
			if eventID == "" {
				t.Fatal("event_revoke payload should include event_id")
			}
			return eventID
		case <-time.After(2 * time.Second):
			t.Fatal("expected queued revoke event to be delivered")
			return ""
		}
	}

	firstEventID := readEventID()

	var queued model.AgentQueuedEvent
	if err := store.DB.Where("agent_id = ?", int64(99125)).First(&queued).Error; err != nil {
		t.Fatalf("load queued revoke event error: %v", err)
	}
	if queued.DispatchAttempts != 1 {
		t.Fatalf("dispatch attempts after first send=%d want=1", queued.DispatchAttempts)
	}
	if queued.DispatchedAt == nil {
		t.Fatal("dispatched_at should be set after first send")
	}

	time.Sleep(80 * time.Millisecond)

	var queuedCount int64
	if err := store.DB.Model(&model.AgentQueuedEvent{}).
		Where("agent_id = ?", int64(99125)).
		Count(&queuedCount).Error; err != nil {
		t.Fatalf("count queued revoke events after timeout error: %v", err)
	}
	if queuedCount != 0 {
		t.Fatalf("queued revoke events should be deleted after ack timeout, got=%d", queuedCount)
	}

	_ = firstEventID
	mgr.refreshAgentLease(conn)

	select {
	case <-conn.send:
		t.Fatal("revoke event should not be redelivered after ack timeout cleanup")
	default:
	}
}

func TestHandleEventAck_ResolvesPendingDeliveryStatus(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	statuses := make([]protocol.AgentDeliveryStatusPayload, 0, 2)
	mgr.SetDeliveryStatusHandler(func(payload protocol.AgentDeliveryStatusPayload) {
		statuses = append(statuses, payload)
	})

	conn := &agentConn{
		agentID:  9992,
		ownerID:  1001,
		clientID: "test",
		send:     make(chan []byte, 2),
	}
	mgr.putConnForTest(conn)

	event := DelegateEventPayload{
		EventID:     "u_1001_u_2001:1001:18889990223",
		EventType:   "user_chat",
		AgentID:     9992,
		OwnerID:     1001,
		SessionID:   "u_1001_u_2001",
		SessionType: 1,
		MsgID:       18889990223,
		SenderID:    1001,
		Content:     "hello openclaw",
		CreatedAt:   1704067202000,
	}
	if ok := mgr.PushDelegateEvent(event); !ok {
		t.Fatalf("PushDelegateEvent should succeed")
	}
	if len(statuses) != 1 || statuses[0].Status != protocol.AgentDeliveryStatusQueued {
		t.Fatalf("expected queued status, got=%#v", statuses)
	}

	mgr.handleEventAck(conn, makePacket(t, protocol.CmdEventAck, 99, EventAckPayload{
		EventID:    event.EventID,
		ReceivedAt: 1704067202999,
	}))

	if len(statuses) != 2 {
		t.Fatalf("expected queued and received statuses, got=%d", len(statuses))
	}
	if statuses[1].Status != protocol.AgentDeliveryStatusReceived {
		t.Fatalf("second status=%q want=%q", statuses[1].Status, protocol.AgentDeliveryStatusReceived)
	}
	if statuses[1].ReceivedAt != 1704067202999 {
		t.Fatalf("received_at=%d want=%d", statuses[1].ReceivedAt, int64(1704067202999))
	}
	mgr.resolvePendingEventResult(EventResultPayload{
		EventID: event.EventID,
		Status:  protocol.AgentEventResultResponded,
	})
}

func TestHandleEventResult_ResolvesPendingDeliveryStatus(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	statuses := make([]protocol.AgentDeliveryStatusPayload, 0, 3)
	outputs := make([]protocol.AgentOutputStatusPayload, 0, 3)
	mgr.SetDeliveryStatusHandler(func(payload protocol.AgentDeliveryStatusPayload) {
		statuses = append(statuses, payload)
	})
	mgr.SetOutputStatusHandler(func(payload protocol.AgentOutputStatusPayload) {
		outputs = append(outputs, payload)
	})

	conn := &agentConn{
		agentID:  9993,
		ownerID:  1001,
		clientID: "test",
		send:     make(chan []byte, 2),
	}
	mgr.putConnForTest(conn)

	event := DelegateEventPayload{
		EventID:     "u_1001_u_2001:1001:18889990225",
		EventType:   "user_chat",
		AgentID:     9993,
		OwnerID:     1001,
		SessionID:   "u_1001_u_2001",
		SessionType: 1,
		MsgID:       18889990225,
		SenderID:    1001,
		Content:     "hello result",
		CreatedAt:   1704067202000,
	}
	if ok := mgr.PushDelegateEvent(event); !ok {
		t.Fatalf("PushDelegateEvent should succeed")
	}
	mgr.handleEventAck(conn, makePacket(t, protocol.CmdEventAck, 100, EventAckPayload{
		EventID:    event.EventID,
		ReceivedAt: 1704067203000,
	}))
	mgr.streamChunkTrackers.observe(event.EventID, 1)

	beforeResult := time.Now().UnixMilli()
	mgr.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 101, EventResultPayload{
		EventID:   event.EventID,
		Status:    protocol.AgentEventResultResponded,
		UpdatedAt: 1704067203999,
	}))

	if len(statuses) != 3 {
		t.Fatalf("expected queued, received, responded statuses, got=%d", len(statuses))
	}
	if statuses[2].Status != protocol.AgentDeliveryStatusResponded {
		t.Fatalf("third status=%q want=%q", statuses[2].Status, protocol.AgentDeliveryStatusResponded)
	}
	if statuses[2].UpdatedAt < beforeResult || statuses[2].UpdatedAt > time.Now().UnixMilli() {
		t.Fatalf("updated_at=%d must use server observation window [%d,%d]", statuses[2].UpdatedAt, beforeResult, time.Now().UnixMilli())
	}
	if len(outputs) != 3 {
		t.Fatalf("expected queued, received, completed output states, got=%d", len(outputs))
	}
	if outputs[2].State != protocol.AgentOutputStateCompleted {
		t.Fatalf("third output state=%q want=%q", outputs[2].State, protocol.AgentOutputStateCompleted)
	}
	if _, tracked := mgr.streamChunkTrackers.m.Load(event.EventID); tracked {
		t.Fatal("terminal event_result must release unfinished stream accounting")
	}
}

func TestHandleEventResult_ResolvesPendingDeliveryStatusWithoutAck(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.eventAckWait = 20 * time.Millisecond
	statusCh := make(chan protocol.AgentDeliveryStatusPayload, 8)
	outputCh := make(chan protocol.AgentOutputStatusPayload, 8)
	mgr.SetDeliveryStatusHandler(func(payload protocol.AgentDeliveryStatusPayload) {
		statusCh <- payload
	})
	mgr.SetOutputStatusHandler(func(payload protocol.AgentOutputStatusPayload) {
		outputCh <- payload
	})

	conn := &agentConn{
		agentID:  99931,
		ownerID:  1001,
		clientID: "test-no-ack",
		send:     make(chan []byte, 2),
	}
	mgr.putConnForTest(conn)

	eventID := "u_1001_u_2001:1001:18889990225:no_ack"
	if ok := mgr.PushDelegateEvent(DelegateEventPayload{
		EventID:     eventID,
		EventType:   "user_chat",
		AgentID:     conn.agentID,
		OwnerID:     conn.ownerID,
		SessionID:   "u_1001_u_2001",
		SessionType: 1,
		MsgID:       18889990225,
		SenderID:    1001,
		Content:     "hello result without ack",
		CreatedAt:   1704067202000,
	}); !ok {
		t.Fatalf("PushDelegateEvent should succeed")
	}

	if first := <-statusCh; first.Status != protocol.AgentDeliveryStatusQueued {
		t.Fatalf("first status=%q want=%q", first.Status, protocol.AgentDeliveryStatusQueued)
	}
	if firstOutput := <-outputCh; firstOutput.State != protocol.AgentOutputStateQueued {
		t.Fatalf("first output state=%q want=%q", firstOutput.State, protocol.AgentOutputStateQueued)
	}

	beforeResult := time.Now().UnixMilli()
	mgr.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 101, EventResultPayload{
		EventID:   eventID,
		Status:    protocol.AgentEventResultResponded,
		UpdatedAt: 1704067203999,
	}))

	select {
	case second := <-statusCh:
		if second.Status != protocol.AgentDeliveryStatusResponded {
			t.Fatalf("second status=%q want=%q", second.Status, protocol.AgentDeliveryStatusResponded)
		}
		if second.UpdatedAt < beforeResult || second.UpdatedAt > time.Now().UnixMilli() {
			t.Fatalf("updated_at=%d must use server observation window [%d,%d]", second.UpdatedAt, beforeResult, time.Now().UnixMilli())
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected responded status without prior ack")
	}
	select {
	case output := <-outputCh:
		if output.State != protocol.AgentOutputStateCompleted {
			t.Fatalf("output state=%q want=%q", output.State, protocol.AgentOutputStateCompleted)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected completed output status without prior ack")
	}

	select {
	case status := <-statusCh:
		t.Fatalf("did not expect additional status after terminal result, got=%#v", status)
	case <-time.After(80 * time.Millisecond):
	}
	select {
	case output := <-outputCh:
		t.Fatalf("did not expect additional output status after terminal result, got=%#v", output)
	case <-time.After(80 * time.Millisecond):
	}
}

func TestHandleEventResult_DoesNotAckUnknownEventWhenCapabilityDeclared(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:      99932,
		ownerID:      1001,
		clientID:     "test-result-ack",
		capabilities: []string{"event_result_ack"},
		send:         make(chan []byte, 2),
	}

	mgr.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 101, EventResultPayload{
		EventID:   "event-result-ack-1",
		Status:    protocol.AgentEventResultResponded,
		UpdatedAt: 1704067203999,
	}))

	select {
	case raw := <-conn.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(raw, &pkt); err != nil {
			t.Fatalf("decode error packet: %v", err)
		}
		if pkt.Cmd != "error" {
			t.Fatalf("cmd=%q want=error", pkt.Cmd)
		}
		if pkt.Seq != 101 {
			t.Fatalf("error seq=%d want=101", pkt.Seq)
		}
		var payload SendNackPayload
		if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
			t.Fatalf("decode error payload: %v", err)
		}
		if payload.Code != 4003 {
			t.Fatalf("error payload=%#v", payload)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected unknown event_result error")
	}
}

func TestHandleEventResult_FailedStatusesResolvePendingDeliveryStatus(t *testing.T) {
	cases := []struct {
		name     string
		result   EventResultPayload
		wantStat string
		wantCode string
		wantMsg  string
	}{
		{
			name: "failed uses explicit code and msg",
			result: EventResultPayload{
				Status:    protocol.AgentEventResultFailed,
				Code:      "grix_dispatch_failed",
				Msg:       "dispatch failed",
				UpdatedAt: 1704067204998,
			},
			wantStat: protocol.AgentDeliveryStatusFailed,
			wantCode: "grix_dispatch_failed",
			wantMsg:  "dispatch failed",
		},
		{
			name: "canceled uses default code and msg",
			result: EventResultPayload{
				Status:    protocol.AgentEventResultCanceled,
				UpdatedAt: 1704067204999,
			},
			wantStat: protocol.AgentDeliveryStatusCanceled,
			wantCode: protocol.AgentDeliveryCodeCanceled,
			wantMsg:  "agent api event canceled",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
			defer mgr.Shutdown()
			statuses := make([]protocol.AgentDeliveryStatusPayload, 0, 3)
			mgr.SetDeliveryStatusHandler(func(payload protocol.AgentDeliveryStatusPayload) {
				statuses = append(statuses, payload)
			})

			conn := &agentConn{
				agentID:  9996,
				ownerID:  1001,
				clientID: "test",
				send:     make(chan []byte, 2),
			}
			mgr.putConnForTest(conn)

			eventID := "u_1001_u_2001:1001:18889990228"
			if ok := mgr.PushDelegateEvent(DelegateEventPayload{
				EventID:     eventID,
				EventType:   "user_chat",
				AgentID:     conn.agentID,
				OwnerID:     conn.ownerID,
				SessionID:   "u_1001_u_2001",
				SessionType: 1,
				MsgID:       18889990228,
				SenderID:    1001,
				Content:     "result failed",
				CreatedAt:   1704067202000,
			}); !ok {
				t.Fatalf("PushDelegateEvent should succeed")
			}
			mgr.handleEventAck(conn, makePacket(t, protocol.CmdEventAck, 102, EventAckPayload{
				EventID:    eventID,
				ReceivedAt: 1704067203001,
			}))

			result := tc.result
			result.EventID = eventID
			beforeResult := time.Now().UnixMilli()
			mgr.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 103, result))

			if len(statuses) != 3 {
				t.Fatalf("expected queued, received, failed statuses, got=%d", len(statuses))
			}
			last := statuses[2]
			if last.Status != tc.wantStat {
				t.Fatalf("last status=%q want=%q", last.Status, tc.wantStat)
			}
			if last.Code != tc.wantCode {
				t.Fatalf("last code=%q want=%q", last.Code, tc.wantCode)
			}
			if last.Msg != tc.wantMsg {
				t.Fatalf("last msg=%q want=%q", last.Msg, tc.wantMsg)
			}
			if last.UpdatedAt < beforeResult || last.UpdatedAt > time.Now().UnixMilli() {
				t.Fatalf("updated_at=%d must use server observation window [%d,%d]", last.UpdatedAt, beforeResult, time.Now().UnixMilli())
			}
		})
	}
}

func TestRequestOutputStop_DispatchAndFinalize(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:  9997,
		ownerID:  1001,
		clientID: "test",
		send:     make(chan []byte, 8),
	}
	mgr.putConnForTest(conn)

	outputStatuses := make([]protocol.AgentOutputStatusPayload, 0, 4)
	mgr.SetOutputStatusHandler(func(payload protocol.AgentOutputStatusPayload) {
		outputStatuses = append(outputStatuses, payload)
	})

	eventID := "evt-stop-1"
	mgr.registerActiveRun(DelegateEventPayload{
		EventID:             eventID,
		TerminalCommitToken: "terminal-token-stop-1",
		EventType:           "user_chat",
		AgentID:             conn.agentID,
		OwnerID:             conn.ownerID,
		SessionID:           "u_1001_u_2001",
		MsgID:               20001,
		SenderID:            1001,
	})
	mgr.MarkRunStreaming(eventID, 30001)

	ack, run, err := mgr.RequestOutputStop(conn.ownerID, "u_1001_u_2001", "")
	if err != nil {
		t.Fatalf("RequestOutputStop err=%v", err)
	}
	if !ack.Accepted {
		t.Fatalf("expected stop accepted")
	}
	if run == nil || run.EventID != eventID {
		t.Fatalf("run mismatch: %+v", run)
	}
	if run.State != protocol.AgentOutputStateStopping {
		t.Fatalf("run state=%q want=%q", run.State, protocol.AgentOutputStateStopping)
	}
	if !mgr.ShouldFenceEventReply(eventID) {
		t.Fatalf("ShouldFenceEventReply should be true after stop request")
	}

	if err := mgr.DispatchOutputStop(ack, run); err != nil {
		t.Fatalf("DispatchOutputStop err=%v", err)
	}

	select {
	case raw := <-conn.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(raw, &pkt); err != nil {
			t.Fatalf("unmarshal sent packet: %v", err)
		}
		if pkt.Cmd != protocol.CmdEventStop {
			t.Fatalf("sent cmd=%q want=%q", pkt.Cmd, protocol.CmdEventStop)
		}
		var payload protocol.AgentEventStopPayload
		if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
			t.Fatalf("unmarshal event_stop payload: %v", err)
		}
		if payload.EventID != eventID {
			t.Fatalf("payload event_id=%q want=%q", payload.EventID, eventID)
		}
		if payload.TerminalCommitToken != "terminal-token-stop-1" {
			t.Fatalf(
				"payload terminal_commit_token=%q want=%q",
				payload.TerminalCommitToken,
				"terminal-token-stop-1",
			)
		}
		if payload.StreamMsgID != 30001 {
			t.Fatalf("payload stream_msg_id=%d want=%d", payload.StreamMsgID, 30001)
		}
	case <-time.After(time.Second):
		t.Fatalf("expected event_stop packet")
	}

	mgr.handleEventStopResult(conn, makePacket(t, protocol.CmdEventStopResult, 201, protocol.AgentEventStopResultPayload{
		EventID:   eventID,
		Status:    "stopped",
		UpdatedAt: 1704067205999,
	}))

	if !mgr.ShouldFenceEventReply(eventID) {
		t.Fatalf("ShouldFenceEventReply should stay true after stop result removes run")
	}

	if got := len(outputStatuses); got < 4 {
		t.Fatalf("expected at least 4 output status updates, got=%d", got)
	}
	last := outputStatuses[len(outputStatuses)-1]
	if last.State != protocol.AgentOutputStateStopped {
		t.Fatalf("last output state=%q want=%q", last.State, protocol.AgentOutputStateStopped)
	}
	if last.StopReason == "" {
		t.Fatalf("expected stop reason to be set")
	}
}

func TestDispatchOutputStopUnavailableRollsBackStopWithoutTerminalizingRun(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	event := DelegateEventPayload{
		EventID:   "evt-stop-dispatch-unavailable",
		AgentID:   9191,
		OwnerID:   9291,
		SessionID: "sess-stop-dispatch-unavailable",
		MsgID:     9391,
		SenderID:  9291,
	}
	mgr.registerActiveRun(event)
	mgr.MarkRunStreaming(event.EventID, 9491)
	ack, run, err := mgr.RequestOutputStop(event.OwnerID, event.SessionID, event.EventID)
	if err != nil || run == nil || !ack.Accepted {
		t.Fatalf("request stop failed ack=%+v run=%+v err=%v", ack, run, err)
	}
	if err := mgr.DispatchOutputStop(ack, run); err == nil {
		t.Fatal("dispatch without agent connection should fail")
	}

	remaining := mgr.LookupActiveRun(event.EventID)
	if remaining == nil {
		t.Fatal("stop delivery failure terminalized a still-running event")
	}
	if remaining.State != protocol.AgentOutputStateStreaming {
		t.Fatalf("run state=%q want streaming", remaining.State)
	}
	if !remaining.CanStop {
		t.Fatalf("stop request was not rolled back: %+v", remaining)
	}
	if mgr.ShouldFenceEventReply(event.EventID) {
		t.Fatal("failed stop delivery must not fence later event output")
	}
}

func TestHandleEventStopResult_AlreadyFinishedMarksCompleted(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	states := make([]protocol.AgentOutputStatusPayload, 0, 4)
	mgr.SetOutputStatusHandler(func(payload protocol.AgentOutputStatusPayload) {
		states = append(states, payload)
	})

	eventID := "evt-stop-finished"
	mgr.registerActiveRun(DelegateEventPayload{
		EventID:   eventID,
		EventType: "user_chat",
		AgentID:   9998,
		OwnerID:   1001,
		SessionID: "u_1001_u_2002",
		MsgID:     20002,
		SenderID:  1001,
	})

	conn := &agentConn{agentID: 9998, ownerID: 1001, clientID: "test", send: make(chan []byte, 1)}
	mgr.handleEventStopResult(conn, makePacket(t, protocol.CmdEventStopResult, 202, protocol.AgentEventStopResultPayload{
		EventID:   eventID,
		Status:    "already_finished",
		UpdatedAt: 1704067206001,
	}))

	if mgr.ShouldFenceEventReply(eventID) {
		t.Fatalf("ShouldFenceEventReply should be false once run is completed")
	}
	last := states[len(states)-1]
	if last.State != protocol.AgentOutputStateCompleted {
		t.Fatalf("last output state=%q want=%q", last.State, protocol.AgentOutputStateCompleted)
	}
}

func TestPendingEventResultOverduePreservesNonTerminalState(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.eventAckWait = 20 * time.Millisecond
	mgr.eventResultWait = 25 * time.Millisecond
	statusCh := make(chan protocol.AgentDeliveryStatusPayload, 8)
	outputCh := make(chan protocol.AgentOutputStatusPayload, 8)
	mgr.SetDeliveryStatusHandler(func(payload protocol.AgentDeliveryStatusPayload) {
		statusCh <- payload
	})
	mgr.SetOutputStatusHandler(func(payload protocol.AgentOutputStatusPayload) {
		outputCh <- payload
	})

	conn := &agentConn{
		agentID:  9994,
		ownerID:  1001,
		clientID: "test",
		send:     make(chan []byte, 8),
	}
	mgr.putConnForTest(conn)

	eventID := "u_1001_u_2001:1001:18889990226"
	if ok := mgr.PushDelegateEvent(DelegateEventPayload{
		EventID:     eventID,
		EventType:   "user_chat",
		AgentID:     9994,
		OwnerID:     1001,
		SessionID:   "u_1001_u_2001",
		SessionType: 1,
		MsgID:       18889990226,
		SenderID:    1001,
		Content:     "result timeout",
		CreatedAt:   1704067202000,
	}); !ok {
		t.Fatalf("PushDelegateEvent should succeed")
	}
	if first := <-statusCh; first.Status != protocol.AgentDeliveryStatusQueued {
		t.Fatalf("first status=%q want=%q", first.Status, protocol.AgentDeliveryStatusQueued)
	}
	if firstOutput := <-outputCh; firstOutput.State != protocol.AgentOutputStateQueued {
		t.Fatalf("first output state=%q want=%q", firstOutput.State, protocol.AgentOutputStateQueued)
	}

	mgr.resolvePendingEventAck(eventID, time.Now().UnixMilli())
	if second := <-statusCh; second.Status != protocol.AgentDeliveryStatusReceived {
		t.Fatalf("second status=%q want=%q", second.Status, protocol.AgentDeliveryStatusReceived)
	}
	if secondOutput := <-outputCh; secondOutput.State != protocol.AgentOutputStateReceived {
		t.Fatalf("second output state=%q want=%q", secondOutput.State, protocol.AgentOutputStateReceived)
	}

	select {
	case third := <-statusCh:
		t.Fatalf("inactivity must not emit terminal delivery status, got=%#v", third)
	case <-time.After(80 * time.Millisecond):
	}

	select {
	case output := <-outputCh:
		t.Fatalf("inactivity must not emit terminal output status, got=%#v", output)
	case <-time.After(20 * time.Millisecond):
	}

	mgr.acksMu.Lock()
	_, pending := mgr.pending[eventID]
	mgr.acksMu.Unlock()
	if !pending {
		t.Fatal("overdue event must stay pending for late output/result")
	}
	if run := mgr.LookupActiveRun(eventID); run == nil || run.State != protocol.AgentOutputStateReceived {
		t.Fatalf("overdue run must remain received, got=%+v", run)
	}

	mgr.resolvePendingEventResult(EventResultPayload{
		EventID: eventID,
		Status:  protocol.AgentEventResultResponded,
	})
	if status := <-statusCh; status.Status != protocol.AgentDeliveryStatusResponded {
		t.Fatalf("late result status=%q want=%q", status.Status, protocol.AgentDeliveryStatusResponded)
	}
	if output := <-outputCh; output.State != protocol.AgentOutputStateCompleted {
		t.Fatalf("late result output=%q want=%q", output.State, protocol.AgentOutputStateCompleted)
	}
}

func TestPendingEventResultTouchExtendsTimeout(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.eventResultWait = 30 * time.Millisecond
	statusCh := make(chan protocol.AgentDeliveryStatusPayload, 8)
	mgr.SetDeliveryStatusHandler(func(payload protocol.AgentDeliveryStatusPayload) {
		statusCh <- payload
	})

	conn := &agentConn{
		agentID:  9995,
		ownerID:  1001,
		clientID: "test",
		send:     make(chan []byte, 8),
	}
	mgr.putConnForTest(conn)

	eventID := "u_1001_u_2001:1001:18889990227"
	if ok := mgr.PushDelegateEvent(DelegateEventPayload{
		EventID:     eventID,
		EventType:   "user_chat",
		AgentID:     9995,
		OwnerID:     1001,
		SessionID:   "u_1001_u_2001",
		SessionType: 1,
		MsgID:       18889990227,
		SenderID:    1001,
		Content:     "result touch",
		CreatedAt:   1704067202000,
	}); !ok {
		t.Fatalf("PushDelegateEvent should succeed")
	}
	<-statusCh // queued
	mgr.resolvePendingEventAck(eventID, time.Now().UnixMilli())
	<-statusCh // received

	time.Sleep(20 * time.Millisecond)
	mgr.TouchPendingEventResult(eventID)

	select {
	case status := <-statusCh:
		t.Fatalf("did not expect status before extended timeout, got=%#v", status)
	case <-time.After(20 * time.Millisecond):
	}

	select {
	case status := <-statusCh:
		t.Fatalf("elapsed observation deadline must remain non-terminal, got=%#v", status)
	case <-time.After(80 * time.Millisecond):
	}
}

func TestPushDelegateEvent_AckOverduePreservesQueuedState(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.eventAckWait = 20 * time.Millisecond
	statusCh := make(chan protocol.AgentDeliveryStatusPayload, 8)
	outputCh := make(chan protocol.AgentOutputStatusPayload, 8)
	mgr.SetDeliveryStatusHandler(func(payload protocol.AgentDeliveryStatusPayload) {
		statusCh <- payload
	})
	mgr.SetOutputStatusHandler(func(payload protocol.AgentOutputStatusPayload) {
		outputCh <- payload
	})

	conn := &agentConn{
		agentID:  9992,
		ownerID:  1001,
		clientID: "test",
		send:     make(chan []byte, 8),
	}
	mgr.putConnForTest(conn)

	if ok := mgr.PushDelegateEvent(DelegateEventPayload{
		EventID:     "u_1001_u_2001:1001:18889990224",
		EventType:   "user_chat",
		AgentID:     9992,
		OwnerID:     1001,
		SessionID:   "u_1001_u_2001",
		SessionType: 1,
		MsgID:       18889990224,
		SenderID:    1001,
		Content:     "timeout test",
		CreatedAt:   1704067202000,
	}); !ok {
		t.Fatalf("PushDelegateEvent should succeed")
	}

	first := <-statusCh
	if first.Status != protocol.AgentDeliveryStatusQueued {
		t.Fatalf("first status=%q want=%q", first.Status, protocol.AgentDeliveryStatusQueued)
	}
	if firstOutput := <-outputCh; firstOutput.State != protocol.AgentOutputStateQueued {
		t.Fatalf("first output state=%q want=%q", firstOutput.State, protocol.AgentOutputStateQueued)
	}

	statuses := []protocol.AgentDeliveryStatusPayload{first}
	deadline := time.After(250 * time.Millisecond)
collect:
	for {
		select {
		case status := <-statusCh:
			statuses = append(statuses, status)
			if status.Status != protocol.AgentDeliveryStatusQueued {
				t.Fatalf("ack silence must not emit terminal status, got=%#v", status)
			}
		case <-deadline:
			break collect
		}
	}

	// Retries resend only the wire packet. They must not recreate pending/run
	// state or re-emit queued, otherwise a concurrent ACK/result can visibly
	// regress received/terminal state.
	expectedStatusCount := 1
	if len(statuses) != expectedStatusCount {
		t.Fatalf("status count=%d want=%d statuses=%#v", len(statuses), expectedStatusCount, statuses)
	}
	if statuses[0].Status != protocol.AgentDeliveryStatusQueued {
		t.Fatalf("status[0]=%q want=%q", statuses[0].Status, protocol.AgentDeliveryStatusQueued)
	}
	select {
	case output := <-outputCh:
		t.Fatalf("ack silence must not emit terminal output status, got=%#v", output)
	default:
	}

	eventID := "u_1001_u_2001:1001:18889990224"
	mgr.acksMu.Lock()
	_, pending := mgr.pending[eventID]
	mgr.acksMu.Unlock()
	if !pending {
		t.Fatal("ack-overdue event must remain pending")
	}
	if run := mgr.LookupActiveRun(eventID); run == nil || run.State != protocol.AgentOutputStateQueued {
		t.Fatalf("ack-overdue run must remain queued, got=%+v", run)
	}
}

func TestRedeliverDelegateEventDoesNotRecreateSettledRun(t *testing.T) {
	installDurableLifecycleTestStores(t, true)
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID:  9994,
		ownerID:  1001,
		clientID: "settled-redelivery",
		send:     make(chan []byte, 8),
	}
	mgr.putConnForTest(conn)
	event := DelegateEventPayload{
		EventID:     "u_1001_u_2001:1001:18889990226",
		EventType:   "user_chat",
		AgentID:     conn.agentID,
		OwnerID:     conn.ownerID,
		SessionID:   "u_1001_u_2001",
		SessionType: 1,
		MsgID:       18889990226,
		SenderID:    1001,
		Content:     "settled before stale retry",
		CreatedAt:   1704067202000,
	}
	mgr.registerActiveRun(event)
	mgr.registerPendingEventAck(event, 1)
	mgr.resolvePendingEventAck(event.EventID, time.Now().UnixMilli())
	mgr.resolvePendingEventResult(EventResultPayload{
		EventID: event.EventID,
		Status:  protocol.AgentEventResultResponded,
	})

	if run := mgr.LookupActiveRun(event.EventID); run != nil {
		t.Fatalf("expected terminal result to remove run, got=%+v", run)
	}
	mgr.acksMu.Lock()
	_, pendingBefore := mgr.pending[event.EventID]
	mgr.acksMu.Unlock()
	if pendingBefore {
		t.Fatal("expected terminal result to remove pending tracking")
	}

	// Model an ACK-timeout callback that already captured the old event before
	// the terminal result won the race. The terminal verdict is authoritative:
	// a stale callback must not emit another wire packet or recreate state.
	if mgr.redeliverDelegateEvent(event, 2) {
		t.Fatal("did not expect retry after terminal settlement")
	}
	select {
	case data := <-conn.send:
		t.Fatalf("did not expect stale wire packet: %s", data)
	default:
	}
	if run := mgr.LookupActiveRun(event.EventID); run != nil {
		t.Fatalf("stale retry recreated a terminal run: %+v", run)
	}
	mgr.acksMu.Lock()
	_, pendingAfter := mgr.pending[event.EventID]
	mgr.acksMu.Unlock()
	if pendingAfter {
		t.Fatal("stale retry recreated pending tracking")
	}
}

func TestPushDelegateEvent_RetryPreservesStopRequestedRun(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:  9993,
		ownerID:  1001,
		clientID: "test",
		send:     make(chan []byte, 8),
	}
	mgr.putConnForTest(conn)

	eventID := "u_1001_u_2001:1001:18889990225"
	if ok := mgr.PushDelegateEvent(DelegateEventPayload{
		EventID:     eventID,
		EventType:   "user_chat",
		AgentID:     conn.agentID,
		OwnerID:     conn.ownerID,
		SessionID:   "u_1001_u_2001",
		SessionType: 1,
		MsgID:       18889990225,
		SenderID:    1001,
		Content:     "retry preserve stop",
		CreatedAt:   1704067202000,
	}); !ok {
		t.Fatalf("PushDelegateEvent should succeed")
	}

	mgr.MarkRunStreaming(eventID, 30001)
	if _, _, err := mgr.RequestOutputStop(conn.ownerID, "u_1001_u_2001", eventID); err != nil {
		t.Fatalf("RequestOutputStop err=%v", err)
	}

	mgr.timeoutPendingEvent(eventID)

	mgr.runsMu.Lock()
	run := mgr.runs[eventID]
	if run == nil {
		mgr.runsMu.Unlock()
		t.Fatal("expected run to remain active after retry dispatch")
	}
	runCopy := *run
	mgr.runsMu.Unlock()

	if !runCopy.StopRequested {
		t.Fatal("expected stop request to survive retry dispatch")
	}
	if runCopy.State != protocol.AgentOutputStateStopping {
		t.Fatalf("run state=%q want=%q", runCopy.State, protocol.AgentOutputStateStopping)
	}
	if runCopy.CanStop {
		t.Fatal("expected run CanStop=false after stop request")
	}
	if runCopy.StopReason != "owner_requested_stop" {
		t.Fatalf("run stop reason=%q want=%q", runCopy.StopReason, "owner_requested_stop")
	}
	if runCopy.StreamMsgID != 30001 {
		t.Fatalf("run stream msg id=%d want=%d", runCopy.StreamMsgID, 30001)
	}
	if !mgr.ShouldFenceEventReply(eventID) {
		t.Fatal("expected retry-dispatched run to keep fencing replies")
	}
}

func TestUnregister_PreservesReceivedRunDuringReconnectWindow(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.eventResultWait = 30 * time.Millisecond
	mgr.disconnectRecoveryWait = 120 * time.Millisecond

	statusCh := make(chan protocol.AgentDeliveryStatusPayload, 8)
	outputCh := make(chan protocol.AgentOutputStatusPayload, 8)
	mgr.SetDeliveryStatusHandler(func(payload protocol.AgentDeliveryStatusPayload) {
		statusCh <- payload
	})
	mgr.SetOutputStatusHandler(func(payload protocol.AgentOutputStatusPayload) {
		outputCh <- payload
	})

	event := DelegateEventPayload{
		EventID:     "evt-disconnect-recovery-1",
		EventType:   "user_chat",
		AgentID:     92001,
		OwnerID:     1201,
		SessionID:   "u_1201_u_2201",
		SessionType: 1,
		MsgID:       32001,
		SenderID:    1201,
		Content:     "recover after disconnect",
	}

	mgr.registerPendingEventAck(event, 1)
	mgr.registerActiveRun(event)
	<-statusCh
	<-outputCh

	mgr.resolvePendingEventAck(event.EventID, time.Now().UnixMilli())
	if status := <-statusCh; status.Status != protocol.AgentDeliveryStatusReceived {
		t.Fatalf("received status=%q want=%q", status.Status, protocol.AgentDeliveryStatusReceived)
	}
	if output := <-outputCh; output.State != protocol.AgentOutputStateReceived {
		t.Fatalf("received output state=%q want=%q", output.State, protocol.AgentOutputStateReceived)
	}

	conn := &agentConn{
		agentID:  event.AgentID,
		ownerID:  event.OwnerID,
		clientID: "disconnect-recovery",
		send:     make(chan []byte, 4),
	}
	mgr.register(conn)
	mgr.unregister(conn)

	if snapshot := mgr.LookupActiveRunBySessionOwner(event.OwnerID, event.SessionID); snapshot == nil {
		t.Fatal("expected active run snapshot after disconnect")
	} else if snapshot.State != protocol.AgentOutputStateReceived {
		t.Fatalf("snapshot state=%q want=%q", snapshot.State, protocol.AgentOutputStateReceived)
	}

	time.Sleep(60 * time.Millisecond)

	select {
	case status := <-statusCh:
		t.Fatalf("did not expect failure status during reconnect window, got=%#v", status)
	default:
	}
	select {
	case output := <-outputCh:
		t.Fatalf("did not expect failure output during reconnect window, got=%#v", output)
	default:
	}

	mgr.resolvePendingEventResult(EventResultPayload{
		EventID:   event.EventID,
		Status:    protocol.AgentEventResultResponded,
		UpdatedAt: time.Now().UnixMilli(),
	})

	if status := <-statusCh; status.Status != protocol.AgentDeliveryStatusResponded {
		t.Fatalf("final status=%q want=%q", status.Status, protocol.AgentDeliveryStatusResponded)
	}
	if output := <-outputCh; output.State != protocol.AgentOutputStateCompleted {
		t.Fatalf("final output state=%q want=%q", output.State, protocol.AgentOutputStateCompleted)
	}
	if snapshot := mgr.LookupActiveRunBySessionOwner(event.OwnerID, event.SessionID); snapshot != nil {
		t.Fatalf("expected active run removed after completion, got=%+v", snapshot)
	}
}

func TestRegister_ReplacementPreservesReceivedRun(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	statusCh := make(chan protocol.AgentDeliveryStatusPayload, 8)
	outputCh := make(chan protocol.AgentOutputStatusPayload, 8)
	mgr.SetDeliveryStatusHandler(func(payload protocol.AgentDeliveryStatusPayload) {
		statusCh <- payload
	})
	mgr.SetOutputStatusHandler(func(payload protocol.AgentOutputStatusPayload) {
		outputCh <- payload
	})

	event := DelegateEventPayload{
		EventID:     "evt-connection-replace-1",
		EventType:   "user_chat",
		AgentID:     92002,
		OwnerID:     1202,
		SessionID:   "u_1202_u_2202",
		SessionType: 1,
		MsgID:       32002,
		SenderID:    1202,
		Content:     "replace connection",
	}

	mgr.registerPendingEventAck(event, 1)
	mgr.registerActiveRun(event)
	<-statusCh
	<-outputCh

	mgr.resolvePendingEventAck(event.EventID, time.Now().UnixMilli())
	<-statusCh
	<-outputCh

	oldConn := &agentConn{
		agentID:  event.AgentID,
		ownerID:  event.OwnerID,
		clientID: "old",
		send:     make(chan []byte, 4),
	}
	newConn := &agentConn{
		agentID:  event.AgentID,
		ownerID:  event.OwnerID,
		clientID: "new",
		send:     make(chan []byte, 4),
	}
	mgr.register(oldConn)
	mgr.register(newConn)

	select {
	case output := <-outputCh:
		t.Fatalf("did not expect failure output on connection replacement, got=%#v", output)
	default:
	}
	select {
	case status := <-statusCh:
		t.Fatalf("did not expect failure status on connection replacement, got=%#v", status)
	default:
	}

	mgr.resolvePendingEventResult(EventResultPayload{
		EventID:   event.EventID,
		Status:    protocol.AgentEventResultResponded,
		UpdatedAt: time.Now().UnixMilli(),
	})

	if status := <-statusCh; status.Status != protocol.AgentDeliveryStatusResponded {
		t.Fatalf("final status=%q want=%q", status.Status, protocol.AgentDeliveryStatusResponded)
	}
	if output := <-outputCh; output.State != protocol.AgentOutputStateCompleted {
		t.Fatalf("final output state=%q want=%q", output.State, protocol.AgentOutputStateCompleted)
	}
}

func TestTimeoutPendingEvent_HoldsAckStageWhileAgentDisconnected(t *testing.T) {
	previousRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = previousRDB
	}()

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.eventAckWait = 20 * time.Millisecond
	mgr.disconnectRecoveryWait = 40 * time.Millisecond

	statuses := make([]protocol.AgentDeliveryStatusPayload, 0, 2)
	mgr.SetDeliveryStatusHandler(func(payload protocol.AgentDeliveryStatusPayload) {
		statuses = append(statuses, payload)
	})

	event := DelegateEventPayload{
		EventID:     "evt-offline-ack-hold-1",
		EventType:   "user_chat",
		AgentID:     92003,
		OwnerID:     1203,
		SessionID:   "u_1203_u_2203",
		SessionType: 1,
		MsgID:       32003,
		SenderID:    1203,
		Content:     "hold ack while offline",
	}

	mgr.registerPendingEventAck(event, 1)
	if len(statuses) != 1 || statuses[0].Status != protocol.AgentDeliveryStatusQueued {
		t.Fatalf("queued statuses=%#v", statuses)
	}

	time.Sleep(35 * time.Millisecond)

	if len(statuses) != 1 {
		t.Fatalf("did not expect timeout while disconnected, got=%#v", statuses)
	}
	mgr.acksMu.Lock()
	_, ok := mgr.pending[event.EventID]
	mgr.acksMu.Unlock()
	if !ok {
		t.Fatal("expected pending ack entry to remain while agent is offline")
	}
}

func TestRegister_DoesNotRedeliverDurableAckEventWhenPendingInMemory(t *testing.T) {
	previousRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = previousRDB
	}()

	event := DelegateEventPayload{
		EventID:     "evt-durable-no-replay-1",
		EventType:   "user_chat",
		AgentID:     92014,
		OwnerID:     1214,
		SessionID:   "u_1214_u_2214",
		SessionType: 1,
		MsgID:       32104,
		SenderID:    1214,
		Content:     "keep pending in memory",
		CreatedAt:   1704067214000,
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.registerPendingEventAck(event, 1)

	conn := &agentConn{
		agentID:  event.AgentID,
		ownerID:  event.OwnerID,
		clientID: "durable-no-replay",
		send:     make(chan []byte, 4),
	}
	mgr.register(conn)

	select {
	case data := <-conn.send:
		var packet protocol.Packet
		if err := json.Unmarshal(data, &packet); err != nil {
			t.Fatalf("unmarshal unexpected packet: %v", err)
		}
		t.Fatalf("did not expect durable replay packet cmd=%s", packet.Cmd)
	case <-time.After(200 * time.Millisecond):
	}

	record, ok := loadDurablePendingDelegate(context.Background(), event.EventID)
	if !ok {
		t.Fatal("expected durable record to stay in ack stage")
	}
	if record.Attempt != 1 {
		t.Fatalf("attempt=%d want=1", record.Attempt)
	}
	if record.Stage != durablePendingDelegateStageAck {
		t.Fatalf("stage=%s want=%s", record.Stage, durablePendingDelegateStageAck)
	}

	mgr.acksMu.Lock()
	_, pending := mgr.pending[event.EventID]
	mgr.acksMu.Unlock()
	if !pending {
		t.Fatal("expected in-memory pending event to remain tracked")
	}
}

func TestRegister_RedeliversDurableAckEventAfterManagerRestart(t *testing.T) {
	previousRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = previousRDB
	}()

	event := DelegateEventPayload{
		EventID:     "evt-durable-redeliver-1",
		EventType:   "user_chat",
		AgentID:     92004,
		OwnerID:     1204,
		SessionID:   "u_1204_u_2204",
		SessionType: 1,
		MsgID:       32004,
		SenderID:    1204,
		Content:     "redeliver after restart",
		CreatedAt:   1704067204000,
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.registerPendingEventAck(event, 1)

	mgr = NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown() // 重新赋值后的 Manager 也要关停（defer 的接收者在语句执行时求值）
	conn := &agentConn{
		agentID:  event.AgentID,
		ownerID:  event.OwnerID,
		clientID: "durable-redeliver",
		send:     make(chan []byte, 4),
	}
	mgr.register(conn)

	select {
	case data := <-conn.send:
		var packet protocol.Packet
		if err := json.Unmarshal(data, &packet); err != nil {
			t.Fatalf("unmarshal replay packet: %v", err)
		}
		if packet.Cmd != "event_msg" {
			t.Fatalf("packet cmd=%s want=%s", packet.Cmd, "event_msg")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected durable ack event to be redelivered on register")
	}

	record, ok := loadDurablePendingDelegate(context.Background(), event.EventID)
	if !ok {
		t.Fatal("expected durable record after redelivery")
	}
	if record.Stage != durablePendingDelegateStageAck {
		t.Fatalf("stage=%s want=%s", record.Stage, durablePendingDelegateStageAck)
	}
	if record.Attempt != 2 {
		t.Fatalf("attempt=%d want=2", record.Attempt)
	}

	mgr.handleEventAck(conn, makePacket(t, protocol.CmdEventAck, 104, EventAckPayload{
		EventID:    event.EventID,
		ReceivedAt: time.Now().UnixMilli(),
	}))

	record, ok = loadDurablePendingDelegate(context.Background(), event.EventID)
	if !ok {
		t.Fatal("expected durable record after ack")
	}
	if record.Stage != durablePendingDelegateStageResult {
		t.Fatalf("stage=%s want=%s", record.Stage, durablePendingDelegateStageResult)
	}
}

func TestResolvePendingEventResult_AcceptsDurableLateResultAfterManagerRestart(t *testing.T) {
	previousRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = previousRDB
	}()

	event := DelegateEventPayload{
		EventID:     "evt-durable-result-1",
		EventType:   "user_chat",
		AgentID:     92005,
		OwnerID:     1205,
		SessionID:   "u_1205_u_2205",
		SessionType: 1,
		MsgID:       32005,
		SenderID:    1205,
		Content:     "late result after restart",
		CreatedAt:   1704067205000,
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.registerActiveRun(event)
	mgr.registerPendingEventAck(event, 1)
	mgr.resolvePendingEventAck(event.EventID, time.Now().UnixMilli())

	mgr = NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown() // 重新赋值后的 Manager 也要关停（defer 的接收者在语句执行时求值）
	statuses := make([]protocol.AgentDeliveryStatusPayload, 0, 2)
	outputs := make([]protocol.AgentOutputStatusPayload, 0, 4)
	mgr.SetDeliveryStatusHandler(func(payload protocol.AgentDeliveryStatusPayload) {
		statuses = append(statuses, payload)
	})
	mgr.SetOutputStatusHandler(func(payload protocol.AgentOutputStatusPayload) {
		outputs = append(outputs, payload)
	})

	mgr.resolvePendingEventResult(EventResultPayload{
		EventID:   event.EventID,
		Status:    protocol.AgentEventResultResponded,
		UpdatedAt: time.Now().UnixMilli(),
	})

	if len(statuses) != 1 {
		t.Fatalf("delivery statuses=%#v want=1", statuses)
	}
	if statuses[0].Status != protocol.AgentDeliveryStatusResponded {
		t.Fatalf("delivery status=%q want=%q", statuses[0].Status, protocol.AgentDeliveryStatusResponded)
	}
	if len(outputs) == 0 || outputs[len(outputs)-1].State != protocol.AgentOutputStateCompleted {
		t.Fatalf("final output statuses=%#v", outputs)
	}
	if snapshot := mgr.LookupActiveRunBySessionOwner(event.OwnerID, event.SessionID); snapshot != nil {
		t.Fatalf("expected durable late result to clear active run, got=%+v", snapshot)
	}
	if _, ok := loadDurablePendingDelegate(context.Background(), event.EventID); ok {
		t.Fatal("expected durable record to be deleted after terminal result")
	}
}

func TestHandleClientStreamChunk_NilHandler(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID: 100, ownerID: 200, clientID: "test",
		send: make(chan []byte, 64),
	}
	registerStreamChunkOwnership(t, mgr, "evt-nil-handler", "sess-1", conn.agentID, conn.ownerID)

	pkt := makePacket(t, "client_stream_chunk", 6, AgentStreamChunkPayload{
		EventID:      "evt-nil-handler",
		SessionID:    "sess-1",
		DeltaContent: "hello",
		ChunkSeq:     1,
		IsFinish:     false,
	})

	mgr.handleClientStreamChunk(conn, pkt)

	select {
	case data := <-conn.send:
		var resp protocol.Packet
		json.Unmarshal(data, &resp)
		if resp.Cmd != "send_nack" {
			t.Fatalf("expected send_nack when handler is nil, got=%s", resp.Cmd)
		}
		var nack SendNackPayload
		json.Unmarshal(resp.Payload, &nack)
		if nack.Code != 5001 {
			t.Fatalf("expected code=5001, got=%d", nack.Code)
		}
	default:
		t.Fatalf("expected send_nack to be sent")
	}
}

func TestHandleClientStreamChunk_AutoGeneratesClientMsgID(t *testing.T) {
	handler := &mockStreamChunkHandler{}
	mgr := NewManager("", 30*time.Second, nil, handler.handle, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID: 100, ownerID: 200, clientID: "test",
		send: make(chan []byte, 64),
	}
	registerStreamChunkOwnership(t, mgr, "evt-auto-id", "sess-1", conn.agentID, conn.ownerID)

	pkt := makePacket(t, "client_stream_chunk", 7, AgentStreamChunkPayload{
		EventID:      "evt-auto-id",
		SessionID:    "sess-1",
		DeltaContent: "auto id test",
		ChunkSeq:     1,
		IsFinish:     false,
		ClientMsgID:  "", // empty, should be auto-generated
	})

	mgr.handleClientStreamChunk(conn, pkt)

	if len(handler.calls) != 1 {
		t.Fatalf("handler should be called once, got %d", len(handler.calls))
	}
	if handler.calls[0].ClientMsgID == "" {
		t.Fatalf("client_msg_id should be auto-generated, got empty string")
	}
}

func TestHandleClientStreamChunk_PreservesQuotedMessageID(t *testing.T) {
	handler := &mockStreamChunkHandler{}
	mgr := NewManager("", 30*time.Second, nil, handler.handle, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID: 100, ownerID: 200, clientID: "test",
		send: make(chan []byte, 64),
	}
	registerStreamChunkOwnership(t, mgr, "evt-quoted", "sess-1", conn.agentID, conn.ownerID)

	pkt := makePacket(t, "client_stream_chunk", 8, AgentStreamChunkPayload{
		EventID:         "evt-quoted",
		SessionID:       "sess-1",
		DeltaContent:    "reply chunk",
		ChunkSeq:        1,
		IsFinish:        false,
		ClientMsgID:     "stream-reply-1",
		QuotedMessageID: 18889990222,
	})

	mgr.handleClientStreamChunk(conn, pkt)

	if len(handler.calls) != 1 {
		t.Fatalf("handler should be called once, got %d", len(handler.calls))
	}
	if handler.calls[0].QuotedMessageID != 18889990222 {
		t.Fatalf("quoted_message_id mismatch: got=%d", handler.calls[0].QuotedMessageID)
	}
}

func TestHandleCodexDelta_PreservesQuotedMessageID(t *testing.T) {
	handler := &mockStreamChunkHandler{}
	mgr := NewManager("", 30*time.Second, nil, handler.handle, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID: 100, ownerID: 200, clientID: "codex-test",
		send: make(chan []byte, 64),
	}

	pkt := makePacket(t, "codex_event", 9, CodexEventPayload{
		EventID:         "evt-delta-1",
		SessionID:       "sess-1",
		ThreadID:        "thread-1",
		QuotedMessageID: 18889990444,
		CodexEventType:  "event",
		CodexMethod:     "item/agentMessage/delta",
		CodexSequence:   101,
		CodexPayload:    json.RawMessage(`{"params":{"delta":"reply chunk"}}`),
		CodexAt:         time.Now().Format(time.RFC3339),
	})

	mgr.handleCodexEvent(conn, pkt)

	if len(handler.calls) != 1 {
		t.Fatalf("handler should be called once, got %d", len(handler.calls))
	}
	if handler.calls[0].QuotedMessageID != 18889990444 {
		t.Fatalf("quoted_message_id mismatch: got=%d", handler.calls[0].QuotedMessageID)
	}
}

func TestHandleCodexTurnCompleted_PreservesQuotedMessageID(t *testing.T) {
	handler := &mockStreamChunkHandler{}
	mgr := NewManager("", 30*time.Second, nil, handler.handle, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID: 100, ownerID: 200, clientID: "codex-test",
		send: make(chan []byte, 64),
	}

	seqPtr := new(int64)
	*seqPtr = 2
	mgr.codexChunkSeq.Store("evt-finish-1", seqPtr)

	pkt := makePacket(t, "codex_event", 10, CodexEventPayload{
		EventID:         "evt-finish-1",
		SessionID:       "sess-1",
		ThreadID:        "thread-1",
		QuotedMessageID: 18889990555,
		CodexEventType:  "event",
		CodexMethod:     "turn/completed",
		CodexSequence:   103,
		CodexPayload:    json.RawMessage(`{}`),
		CodexAt:         time.Now().Format(time.RFC3339),
	})

	mgr.handleCodexEvent(conn, pkt)

	if len(handler.calls) != 1 {
		t.Fatalf("handler should be called once, got %d", len(handler.calls))
	}
	if handler.calls[0].QuotedMessageID != 18889990555 {
		t.Fatalf("quoted_message_id mismatch: got=%d", handler.calls[0].QuotedMessageID)
	}
}

func TestHandleCodexDelta_FallsBackToTriggerQuotedMessageID(t *testing.T) {
	handler := &mockStreamChunkHandler{}
	mgr := NewManager("", 30*time.Second, nil, handler.handle, nil, nil)
	defer mgr.Shutdown()
	mgr.registerActiveRun(DelegateEventPayload{
		EventID:         "evt-delta-fallback-1",
		SessionID:       "sess-1",
		ThreadID:        "thread-1",
		OwnerID:         200,
		AgentID:         100,
		MsgID:           18889990661,
		QuotedMessageID: 18889990660,
	})

	conn := &agentConn{
		agentID: 100, ownerID: 200, clientID: "codex-test",
		send: make(chan []byte, 64),
	}

	pkt := makePacket(t, "codex_event", 11, CodexEventPayload{
		EventID:        "evt-delta-fallback-1",
		SessionID:      "sess-1",
		ThreadID:       "thread-1",
		CodexEventType: "event",
		CodexMethod:    "item/agentMessage/delta",
		CodexSequence:  201,
		CodexPayload:   json.RawMessage(`{"params":{"delta":"reply chunk"}}`),
		CodexAt:        time.Now().Format(time.RFC3339),
	})

	mgr.handleCodexEvent(conn, pkt)

	if len(handler.calls) != 1 {
		t.Fatalf("handler should be called once, got %d", len(handler.calls))
	}
	if handler.calls[0].QuotedMessageID != 18889990660 {
		t.Fatalf("quoted_message_id mismatch: got=%d", handler.calls[0].QuotedMessageID)
	}
}

func TestHandleCodexTurnCompleted_FallsBackToTriggerMsgID(t *testing.T) {
	handler := &mockStreamChunkHandler{}
	mgr := NewManager("", 30*time.Second, nil, handler.handle, nil, nil)
	defer mgr.Shutdown()
	mgr.registerActiveRun(DelegateEventPayload{
		EventID:   "evt-finish-fallback-1",
		SessionID: "sess-1",
		ThreadID:  "thread-1",
		OwnerID:   200,
		AgentID:   100,
		MsgID:     18889990771,
	})

	conn := &agentConn{
		agentID: 100, ownerID: 200, clientID: "codex-test",
		send: make(chan []byte, 64),
	}

	seqPtr := new(int64)
	*seqPtr = 2
	mgr.codexChunkSeq.Store("evt-finish-fallback-1", seqPtr)

	pkt := makePacket(t, "codex_event", 12, CodexEventPayload{
		EventID:        "evt-finish-fallback-1",
		SessionID:      "sess-1",
		ThreadID:       "thread-1",
		CodexEventType: "event",
		CodexMethod:    "turn/completed",
		CodexSequence:  203,
		CodexPayload:   json.RawMessage(`{}`),
		CodexAt:        time.Now().Format(time.RFC3339),
	})

	mgr.handleCodexEvent(conn, pkt)

	if len(handler.calls) != 1 {
		t.Fatalf("handler should be called once, got %d", len(handler.calls))
	}
	if handler.calls[0].QuotedMessageID != 18889990771 {
		t.Fatalf("quoted_message_id mismatch: got=%d", handler.calls[0].QuotedMessageID)
	}
}

func TestHandleClientStreamChunk_InvalidPayload(t *testing.T) {
	handler := &mockStreamChunkHandler{}
	mgr := NewManager("", 30*time.Second, nil, handler.handle, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID: 100, ownerID: 200, clientID: "test",
		send: make(chan []byte, 64),
	}

	pkt := &protocol.Packet{
		Cmd:     "client_stream_chunk",
		Seq:     8,
		Payload: json.RawMessage(`{invalid}`),
	}

	mgr.handleClientStreamChunk(conn, pkt)

	if len(handler.calls) != 0 {
		t.Fatalf("handler should not be called for invalid payload")
	}

	select {
	case data := <-conn.send:
		var resp protocol.Packet
		json.Unmarshal(data, &resp)
		if resp.Cmd != "send_nack" {
			t.Fatalf("expected send_nack for invalid payload, got=%s", resp.Cmd)
		}
	default:
		t.Fatalf("expected send_nack to be sent")
	}
}

func TestHandleSendMsg_HandlerErrorKeepsPendingEventAndRun(t *testing.T) {
	handler := &mockSendMessageHandler{
		err: &SendError{Code: 4003, Msg: "group is muted"},
	}
	mgr := NewManager("", 30*time.Second, handler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	statusCh := make(chan protocol.AgentDeliveryStatusPayload, 8)
	outputCh := make(chan protocol.AgentOutputStatusPayload, 8)
	mgr.SetDeliveryStatusHandler(func(payload protocol.AgentDeliveryStatusPayload) {
		statusCh <- payload
	})
	mgr.SetOutputStatusHandler(func(payload protocol.AgentOutputStatusPayload) {
		outputCh <- payload
	})

	event := DelegateEventPayload{
		EventID:     "evt-send-fail",
		EventType:   "group_mention",
		AgentID:     100,
		OwnerID:     200,
		SessionID:   "sess-2",
		SessionType: 2,
		MsgID:       301,
		SenderID:    200,
		Content:     "@agent hello",
	}
	mgr.registerPendingEventAck(event, 1)
	mgr.registerActiveRun(event)

	if first := <-statusCh; first.Status != protocol.AgentDeliveryStatusQueued {
		t.Fatalf("first status=%q want=%q", first.Status, protocol.AgentDeliveryStatusQueued)
	}
	if firstOutput := <-outputCh; firstOutput.State != protocol.AgentOutputStateQueued {
		t.Fatalf("first output state=%q want=%q", firstOutput.State, protocol.AgentOutputStateQueued)
	}

	mgr.resolvePendingEventAck(event.EventID, time.Now().UnixMilli())
	if second := <-statusCh; second.Status != protocol.AgentDeliveryStatusReceived {
		t.Fatalf("second status=%q want=%q", second.Status, protocol.AgentDeliveryStatusReceived)
	}
	if secondOutput := <-outputCh; secondOutput.State != protocol.AgentOutputStateReceived {
		t.Fatalf("second output state=%q want=%q", secondOutput.State, protocol.AgentOutputStateReceived)
	}

	conn := &agentConn{
		agentID:  event.AgentID,
		ownerID:  event.OwnerID,
		clientID: "test",
		send:     make(chan []byte, 64),
	}
	pkt := makePacket(t, protocol.CmdSendMsg, 11, SendMsgPayload{
		EventID:     event.EventID,
		SessionID:   event.SessionID,
		ClientMsgID: "send-fail-msg",
		MsgType:     1,
		Content:     "blocked",
	})

	mgr.handleSendMsg(conn, pkt)

	if len(handler.calls) != 1 {
		t.Fatalf("handler call count=%d want=1", len(handler.calls))
	}
	select {
	case data := <-conn.send:
		var resp protocol.Packet
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.Cmd != protocol.CmdSendNack {
			t.Fatalf("expected send_nack, got=%s", resp.Cmd)
		}
		var nack SendNackPayload
		if err := json.Unmarshal(resp.Payload, &nack); err != nil {
			t.Fatalf("unmarshal nack payload: %v", err)
		}
		if nack.Msg != "group is muted" {
			t.Fatalf("nack msg=%q want=%q", nack.Msg, "group is muted")
		}
	default:
		t.Fatalf("expected send_nack to be sent")
	}

	select {
	case status := <-statusCh:
		t.Fatalf("packet-level send_nack must not emit terminal delivery status, got=%#v", status)
	case <-time.After(50 * time.Millisecond):
	}
	select {
	case output := <-outputCh:
		t.Fatalf("packet-level send_nack must not emit terminal output status, got=%#v", output)
	case <-time.After(50 * time.Millisecond):
	}
	if snapshot := mgr.LookupActiveRunBySessionOwner(event.OwnerID, event.SessionID); snapshot == nil {
		t.Fatal("expected run to remain active after one rejected send_msg")
	}

	mgr.resolvePendingEventResult(EventResultPayload{
		EventID:   event.EventID,
		Status:    protocol.AgentEventResultResponded,
		UpdatedAt: time.Now().UnixMilli(),
	})

	if status := <-statusCh; status.Status != protocol.AgentDeliveryStatusResponded {
		t.Fatalf("explicit event result status=%q want=%q", status.Status, protocol.AgentDeliveryStatusResponded)
	}
	if output := <-outputCh; output.State != protocol.AgentOutputStateCompleted {
		t.Fatalf("explicit event result output state=%q want=%q", output.State, protocol.AgentOutputStateCompleted)
	}
}

// --- delete_msg tests ---

type mockDeleteMsgHandler struct {
	calls []DeleteMsgPayload
	err   error
}

func (h *mockDeleteMsgHandler) handle(_ context.Context, agentID, ownerID int64, payload DeleteMsgPayload) error {
	h.calls = append(h.calls, payload)
	return h.err
}

func TestHandleDeleteMsg_MissingSessionID(t *testing.T) {
	handler := &mockDeleteMsgHandler{}
	mgr := NewManager("", 30*time.Second, nil, nil, handler.handle, nil)
	defer mgr.Shutdown()
	conn := &agentConn{agentID: 100, ownerID: 200, clientID: "test", send: make(chan []byte, 64)}
	pkt := makePacket(t, "delete_msg", 1, DeleteMsgPayload{SessionID: "", MsgID: 123})
	mgr.handleDeleteMsg(conn, pkt)
	if len(handler.calls) != 0 {
		t.Fatalf("handler should not be called")
	}
	select {
	case data := <-conn.send:
		var resp protocol.Packet
		json.Unmarshal(data, &resp)
		if resp.Cmd != "send_nack" {
			t.Fatalf("expected send_nack, got=%s", resp.Cmd)
		}
	default:
		t.Fatalf("expected send_nack")
	}
}

func TestHandleDeleteMsg_MissingMsgID(t *testing.T) {
	handler := &mockDeleteMsgHandler{}
	mgr := NewManager("", 30*time.Second, nil, nil, handler.handle, nil)
	defer mgr.Shutdown()
	conn := &agentConn{agentID: 100, ownerID: 200, clientID: "test", send: make(chan []byte, 64)}
	pkt := makePacket(t, "delete_msg", 2, DeleteMsgPayload{SessionID: "sess-1", MsgID: 0})
	mgr.handleDeleteMsg(conn, pkt)
	if len(handler.calls) != 0 {
		t.Fatalf("handler should not be called")
	}
	select {
	case data := <-conn.send:
		var resp protocol.Packet
		json.Unmarshal(data, &resp)
		if resp.Cmd != "send_nack" {
			t.Fatalf("expected send_nack, got=%s", resp.Cmd)
		}
	default:
		t.Fatalf("expected send_nack")
	}
}

func TestHandleDeleteMsg_NilHandler(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{agentID: 100, ownerID: 200, clientID: "test", send: make(chan []byte, 64)}
	pkt := makePacket(t, "delete_msg", 3, DeleteMsgPayload{SessionID: "sess-1", MsgID: 123})
	mgr.handleDeleteMsg(conn, pkt)
	select {
	case data := <-conn.send:
		var resp protocol.Packet
		json.Unmarshal(data, &resp)
		if resp.Cmd != "send_nack" {
			t.Fatalf("expected send_nack, got=%s", resp.Cmd)
		}
		var nack SendNackPayload
		json.Unmarshal(resp.Payload, &nack)
		if nack.Code != 5001 {
			t.Fatalf("expected code=5001, got=%d", nack.Code)
		}
	default:
		t.Fatalf("expected send_nack")
	}
}

func TestHandleDeleteMsg_HandlerError(t *testing.T) {
	handler := &mockDeleteMsgHandler{err: &SendError{Code: 4003, Msg: "can only revoke own messages"}}
	mgr := NewManager("", 30*time.Second, nil, nil, handler.handle, nil)
	defer mgr.Shutdown()
	conn := &agentConn{agentID: 100, ownerID: 200, clientID: "test", send: make(chan []byte, 64)}
	pkt := makePacket(t, "delete_msg", 4, DeleteMsgPayload{SessionID: "sess-1", MsgID: 999})
	mgr.handleDeleteMsg(conn, pkt)
	if len(handler.calls) != 1 {
		t.Fatalf("handler should be called once")
	}
	select {
	case data := <-conn.send:
		var resp protocol.Packet
		json.Unmarshal(data, &resp)
		if resp.Cmd != "send_nack" {
			t.Fatalf("expected send_nack, got=%s", resp.Cmd)
		}
		var nack SendNackPayload
		json.Unmarshal(resp.Payload, &nack)
		if nack.Code != 4003 {
			t.Fatalf("expected code=4003, got=%d", nack.Code)
		}
	default:
		t.Fatalf("expected send_nack")
	}
}

func TestHandleDeleteMsg_Success(t *testing.T) {
	handler := &mockDeleteMsgHandler{}
	mgr := NewManager("", 30*time.Second, nil, nil, handler.handle, nil)
	defer mgr.Shutdown()
	conn := &agentConn{agentID: 100, ownerID: 200, clientID: "test", send: make(chan []byte, 64)}
	pkt := makePacket(t, "delete_msg", 5, DeleteMsgPayload{SessionID: "sess-1", MsgID: 12345})
	mgr.handleDeleteMsg(conn, pkt)
	if len(handler.calls) != 1 {
		t.Fatalf("handler should be called once, got %d", len(handler.calls))
	}
	if handler.calls[0].MsgID != 12345 || handler.calls[0].SessionID != "sess-1" {
		t.Fatalf("handler received wrong payload")
	}
	select {
	case data := <-conn.send:
		var resp protocol.Packet
		json.Unmarshal(data, &resp)
		if resp.Cmd != "send_ack" {
			t.Fatalf("expected send_ack, got=%s", resp.Cmd)
		}
	default:
		t.Fatalf("expected send_ack")
	}
}

type mockEditMsgHandler struct {
	calls []EditMsgPayload
	err   error
}

func (h *mockEditMsgHandler) handle(_ context.Context, agentID, ownerID int64, payload EditMsgPayload) error {
	h.calls = append(h.calls, payload)
	return h.err
}

func TestHandleEditMsg_MissingContent(t *testing.T) {
	handler := &mockEditMsgHandler{}
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.SetEditMsgHandler(handler.handle)
	conn := &agentConn{agentID: 100, ownerID: 200, clientID: "test", send: make(chan []byte, 64)}
	pkt := makePacket(t, "edit_msg", 6, EditMsgPayload{SessionID: "sess-1", MsgID: 123, Content: ""})
	mgr.handleEditMsg(conn, pkt)
	if len(handler.calls) != 0 {
		t.Fatalf("handler should not be called")
	}
	select {
	case data := <-conn.send:
		var resp protocol.Packet
		json.Unmarshal(data, &resp)
		if resp.Cmd != "send_nack" {
			t.Fatalf("expected send_nack, got=%s", resp.Cmd)
		}
	default:
		t.Fatalf("expected send_nack")
	}
}

func TestHandleEditMsg_HandlerError(t *testing.T) {
	handler := &mockEditMsgHandler{err: &SendError{Code: 4004, Msg: "message not found"}}
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.SetEditMsgHandler(handler.handle)
	conn := &agentConn{agentID: 100, ownerID: 200, clientID: "test", send: make(chan []byte, 64)}
	pkt := makePacket(t, "edit_msg", 7, EditMsgPayload{SessionID: "sess-1", MsgID: 999, Content: "updated"})
	mgr.handleEditMsg(conn, pkt)
	if len(handler.calls) != 1 {
		t.Fatalf("handler should be called once")
	}
	select {
	case data := <-conn.send:
		var resp protocol.Packet
		json.Unmarshal(data, &resp)
		if resp.Cmd != "send_nack" {
			t.Fatalf("expected send_nack, got=%s", resp.Cmd)
		}
	default:
		t.Fatalf("expected send_nack")
	}
}

func TestHandleEditMsg_Success(t *testing.T) {
	handler := &mockEditMsgHandler{}
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.SetEditMsgHandler(handler.handle)
	conn := &agentConn{agentID: 100, ownerID: 200, clientID: "test", send: make(chan []byte, 64)}
	pkt := makePacket(t, "edit_msg", 8, EditMsgPayload{SessionID: "sess-1", MsgID: 12345, Content: "updated"})
	mgr.handleEditMsg(conn, pkt)
	if len(handler.calls) != 1 {
		t.Fatalf("handler should be called once, got %d", len(handler.calls))
	}
	if handler.calls[0].MsgID != 12345 || handler.calls[0].SessionID != "sess-1" || handler.calls[0].Content != "updated" {
		t.Fatalf("handler received wrong payload")
	}
	select {
	case data := <-conn.send:
		var resp protocol.Packet
		json.Unmarshal(data, &resp)
		if resp.Cmd != "send_ack" {
			t.Fatalf("expected send_ack, got=%s", resp.Cmd)
		}
	default:
		t.Fatalf("expected send_ack")
	}
}

// TestAgentStreamChunkPayloadIsThinkingRoundTrip 守卫:入站 client_stream_chunk 的
// is_thinking 字段必须能被反序列化(connector 显式对齐)且 false 时省略,防止协议字段被静默移除。
func TestAgentStreamChunkPayloadIsThinkingRoundTrip(t *testing.T) {
	var decoded AgentStreamChunkPayload
	if err := json.Unmarshal([]byte(`{"session_id":"s1","delta_content":"x","is_thinking":true}`), &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if !decoded.IsThinking {
		t.Fatalf("inbound is_thinking not parsed")
	}

	raw, err := json.Marshal(AgentStreamChunkPayload{SessionID: "s1", DeltaContent: "x"})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if strings.Contains(string(raw), "is_thinking") {
		t.Fatalf("non-thinking inbound payload must omit is_thinking, got %s", raw)
	}
}

// Backend-observed silence is not connector execution evidence. Even when a
// new event arrives, old silent work stays non-terminal and is not stopped.
func TestObserveStaleResultEventsForNewEventPreservesState(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.staleRunReapWait = 50 * time.Millisecond

	conn := &agentConn{
		agentID:  9996,
		ownerID:  1001,
		clientID: "test",
		send:     make(chan []byte, 8),
	}
	mgr.putConnForTest(conn)

	const sessionID = "u_1001_u_2003"
	staleEvt := DelegateEventPayload{
		EventID:   "evt-stale",
		EventType: "user_chat",
		AgentID:   conn.agentID,
		OwnerID:   conn.ownerID,
		SessionID: sessionID,
		MsgID:     21001,
		SenderID:  conn.ownerID,
	}
	mgr.registerActiveRun(staleEvt)
	mgr.registerPendingEventAck(staleEvt, 1)
	mgr.resolvePendingEventAck(staleEvt.EventID, time.Now().UnixMilli())

	newEvt := DelegateEventPayload{
		EventID:   "evt-new",
		EventType: "user_chat",
		AgentID:   conn.agentID,
		OwnerID:   conn.ownerID,
		SessionID: sessionID,
		MsgID:     21002,
		SenderID:  conn.ownerID,
	}

	mgr.observeStaleResultEventsForNewEvent(newEvt)
	mgr.acksMu.Lock()
	_, stillPending := mgr.pending[staleEvt.EventID]
	mgr.acksMu.Unlock()
	if !stillPending {
		t.Fatalf("fresh event must stay pending")
	}
	if run := mgr.LookupActiveRun(staleEvt.EventID); run == nil {
		t.Fatalf("fresh run must stay active")
	}

	// 把事件自身活动时间回拨到阈值之外，模拟长时间无上行活动。
	mgr.acksMu.Lock()
	mgr.pending[staleEvt.EventID].selfTouchAt = time.Now().Add(-time.Hour).UnixMilli()
	mgr.acksMu.Unlock()

	mgr.observeStaleResultEventsForNewEvent(newEvt)

	mgr.acksMu.Lock()
	_, stillPending = mgr.pending[staleEvt.EventID]
	mgr.acksMu.Unlock()
	if !stillPending {
		t.Fatalf("stale observation must preserve pending event")
	}
	if run := mgr.LookupActiveRun(staleEvt.EventID); run == nil {
		t.Fatal("stale observation must preserve active run")
	}

	select {
	case raw := <-conn.send:
		t.Fatalf("stale observation must not send event_stop, got=%s", string(raw))
	default:
	}
}

// TestObserveStaleResultEventsSkipsOtherSession 守卫:stale observation 严格限定同
// session+owner，其它会话/其它 owner 的同名僵尸不受影响。
func TestObserveStaleResultEventsSkipsOtherSession(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.staleRunReapWait = 50 * time.Millisecond

	otherEvt := DelegateEventPayload{
		EventID:   "evt-other-session",
		EventType: "user_chat",
		AgentID:   9995,
		OwnerID:   1001,
		SessionID: "u_1001_u_2999",
		MsgID:     22001,
		SenderID:  1001,
	}
	mgr.registerActiveRun(otherEvt)
	mgr.registerPendingEventAck(otherEvt, 1)
	mgr.resolvePendingEventAck(otherEvt.EventID, time.Now().UnixMilli())

	mgr.acksMu.Lock()
	mgr.pending[otherEvt.EventID].selfTouchAt = time.Now().Add(-time.Hour).UnixMilli()
	mgr.acksMu.Unlock()

	mgr.observeStaleResultEventsForNewEvent(DelegateEventPayload{
		EventID:   "evt-new",
		EventType: "user_chat",
		AgentID:   9995,
		OwnerID:   1001,
		SessionID: "u_1001_u_2004",
		MsgID:     22002,
		SenderID:  1001,
	})

	mgr.acksMu.Lock()
	_, stillPending := mgr.pending[otherEvt.EventID]
	mgr.acksMu.Unlock()
	if !stillPending {
		t.Fatalf("event of another session must stay pending")
	}
}

// TestObserveStaleResultEventsSkipsZeroTimestamps 守卫：selfTouchAt/ReceivedAt/
// UpdatedAt 全零（异常构造，正常注册路径总会写时间戳）的 pending 无法判断
// 年龄，不得被误判为僵尸回收——误杀比漏收危害大。
func TestObserveStaleResultEventsSkipsZeroTimestamps(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.staleRunReapWait = 50 * time.Millisecond

	evt := DelegateEventPayload{
		EventID:   "evt-zero-ts",
		EventType: "user_chat",
		AgentID:   9994,
		OwnerID:   1001,
		SessionID: "u_1001_u_2005",
		MsgID:     23001,
		SenderID:  1001,
	}
	mgr.registerActiveRun(evt)
	mgr.registerPendingEventAck(evt, 1)
	mgr.resolvePendingEventAck(evt.EventID, time.Now().UnixMilli())

	// 侵入式清零三个时间戳，模拟异常构造的 pending。
	mgr.acksMu.Lock()
	entry := mgr.pending[evt.EventID]
	entry.selfTouchAt = 0
	entry.status.ReceivedAt = 0
	entry.status.UpdatedAt = 0
	mgr.acksMu.Unlock()

	mgr.observeStaleResultEventsForNewEvent(DelegateEventPayload{
		EventID:   "evt-new",
		EventType: "user_chat",
		AgentID:   9994,
		OwnerID:   1001,
		SessionID: "u_1001_u_2005",
		MsgID:     23002,
		SenderID:  1001,
	})

	mgr.acksMu.Lock()
	_, stillPending := mgr.pending[evt.EventID]
	mgr.acksMu.Unlock()
	if !stillPending {
		t.Fatalf("event with undeterminable age must stay pending")
	}
}
