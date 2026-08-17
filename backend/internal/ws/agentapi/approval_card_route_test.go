package agentapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func setupApprovalCardRouteTest(t *testing.T) func() {
	t.Helper()
	previousDB := store.DB
	previousRDB := store.RDB

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	return func() {
		_ = store.RDB.Close()
		testDB.Close()
		store.DB = previousDB
		store.RDB = previousRDB
	}
}

func seedAgent(t *testing.T, agentID, ownerID int64) {
	t.Helper()
	if err := store.DB.Create(&model.Agent{
		ID:        agentID,
		OwnerID:   ownerID,
		AgentName: "route-test-agent",
		Status:    1,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}
}

func seedDirectSession(t *testing.T, sessionID string, ownerID int64, humanMemberIDs ...int64) {
	t.Helper()
	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: model.SessionTypeDirect,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := make([]model.SessionMember, 0, len(humanMemberIDs))
	for _, id := range humanMemberIDs {
		members = append(members, model.SessionMember{
			SessionID: sessionID, MemberID: id, MemberType: 1, JoinedAt: now, LastActiveAt: now,
		})
	}
	if err := store.DB.Create(&members).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}
}

// 托管代答形态（主人↔客户 1:1 会话）：审批卡必须改投主人↔agent 私聊。
func TestResolveApprovalCardSessionID_ReroutesProxySession(t *testing.T) {
	cleanup := setupApprovalCardRouteTest(t)
	defer cleanup()

	const (
		agentID    = int64(7001)
		ownerID    = int64(8001)
		customerID = int64(9001)
	)
	seedAgent(t, agentID, ownerID)
	seedDirectSession(t, "sess-proxy", ownerID, ownerID, customerID)

	got := resolveApprovalCardSessionID(context.Background(), "sess-proxy", agentID, ownerID)
	if got == "sess-proxy" {
		t.Fatal("proxy session should be rerouted to owner-agent private session")
	}

	// 改投目标必须是 owner↔agent 的 1:1 会话，成员为主人与该 agent。
	var session model.Session
	if err := store.DB.Where("session_id = ?", got).First(&session).Error; err != nil {
		t.Fatalf("rerouted session %s not found: %v", got, err)
	}
	if session.SessionType != model.SessionTypeDirect {
		t.Fatalf("rerouted session_type=%d want=%d", session.SessionType, model.SessionTypeDirect)
	}
	var members []model.SessionMember
	if err := store.DB.Where("session_id = ?", got).Find(&members).Error; err != nil {
		t.Fatalf("load members error: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("rerouted session member count=%d want=2 (owner+agent)", len(members))
	}
	hasOwner, hasAgent := false, false
	for _, m := range members {
		if m.MemberType == 1 && m.MemberID == ownerID {
			hasOwner = true
		}
		if m.MemberType == 2 && m.MemberID == agentID {
			hasAgent = true
		}
	}
	if !hasOwner || !hasAgent {
		t.Fatalf("rerouted session members=%+v want owner %d + agent %d", members, ownerID, agentID)
	}

	// 幂等：再次改投复用同一条私聊，不重复建会话。
	again := resolveApprovalCardSessionID(context.Background(), "sess-proxy", agentID, ownerID)
	if again != got {
		t.Fatalf("second reroute session=%s want=%s (must reuse existing private session)", again, got)
	}
}

// 主人↔agent 私聊：审批卡留在原会话。
func TestResolveApprovalCardSessionID_KeepsOwnerAgentSession(t *testing.T) {
	cleanup := setupApprovalCardRouteTest(t)
	defer cleanup()

	const (
		agentID = int64(7002)
		ownerID = int64(8002)
	)
	seedAgent(t, agentID, ownerID)
	seedDirectSession(t, "sess-owner-agent", ownerID, ownerID)
	// agent 成员
	if err := store.DB.Create(&model.SessionMember{
		SessionID: "sess-owner-agent", MemberID: agentID, MemberType: 2,
		JoinedAt: time.Now(), LastActiveAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("create agent member error: %v", err)
	}

	got := resolveApprovalCardSessionID(context.Background(), "sess-owner-agent", agentID, ownerID)
	if got != "sess-owner-agent" {
		t.Fatalf("session=%s want sess-owner-agent (no reroute for owner-agent private chat)", got)
	}
}

// 群聊：分发层已按 visible_to=[owner] 强制过滤，不改投。
func TestResolveApprovalCardSessionID_KeepsGroupSession(t *testing.T) {
	cleanup := setupApprovalCardRouteTest(t)
	defer cleanup()

	const (
		agentID    = int64(7003)
		ownerID    = int64(8003)
		customerID = int64(9003)
	)
	seedAgent(t, agentID, ownerID)
	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID: "sess-group", OwnerID: ownerID, SessionType: 2, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create group error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: "sess-group", MemberID: ownerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: "sess-group", MemberID: customerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: "sess-group", MemberID: agentID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
	}
	if err := store.DB.Create(&members).Error; err != nil {
		t.Fatalf("create group members error: %v", err)
	}

	got := resolveApprovalCardSessionID(context.Background(), "sess-group", agentID, ownerID)
	if got != "sess-group" {
		t.Fatalf("session=%s want sess-group (group fanout already enforces visible_to)", got)
	}
}

// agent 停用/不存在时改投失败，退回原会话（卡片仍带 visible_to=[owner]）。
func TestResolveApprovalCardSessionID_FallbackWhenAgentUnavailable(t *testing.T) {
	cleanup := setupApprovalCardRouteTest(t)
	defer cleanup()

	const (
		agentID    = int64(7004)
		ownerID    = int64(8004)
		customerID = int64(9004)
	)
	// 不 seedAgent：agent 不存在，SessionOpenLatest 校验失败。
	seedDirectSession(t, "sess-proxy-no-agent", ownerID, ownerID, customerID)

	got := resolveApprovalCardSessionID(context.Background(), "sess-proxy-no-agent", agentID, ownerID)
	if got != "sess-proxy-no-agent" {
		t.Fatalf("session=%s want original session when agent unavailable", got)
	}
}

// handleSendMsg 端到端：托管会话里的 exec_approval 卡被改投主人私聊；
// 普通文本消息不受影响，仍发往原会话。与 agent 类型无关（内容判定）。
func TestHandleSendMsg_ReroutesApprovalCardInProxySession(t *testing.T) {
	cleanup := setupApprovalCardRouteTest(t)
	defer cleanup()

	const (
		agentID    = int64(7101)
		ownerID    = int64(8101)
		customerID = int64(9101)
	)
	seedAgent(t, agentID, ownerID)
	seedDirectSession(t, "sess-proxy-e2e", ownerID, ownerID, customerID)

	handler := &mockSendMessageHandler{result: &SendMessageResult{MsgID: 5001, InboxSeq: 1, CreatedAt: 1704067300000}}
	mgr := NewManager("", 30*time.Second, handler.handle, nil, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID: agentID,
		ownerID: ownerID,
		send:    make(chan []byte, 64),
	}

	approvalExtra, _ := json.Marshal(map[string]any{
		"channel_data": map[string]any{
			"grix": map[string]any{
				"execApproval": map[string]any{"host": "acp"},
			},
		},
	})
	approvalPkt := makePacket(t, protocol.CmdSendMsg, 1, SendMsgPayload{
		SessionID:       "sess-proxy-e2e",
		ClientMsgID:     "cmsg-approval-reroute",
		MsgType:         1,
		ThreadID:        "thread-in-proxy",
		QuotedMessageID: 4242,
		Content:         "[Exec Approval](grix://card/exec_approval?approval_id=req_reroute&approval_command_id=req_reroute&command=echo%20hi)",
		Extra:           approvalExtra,
	})
	mgr.handleSendMsg(conn, approvalPkt)

	if len(handler.calls) != 1 {
		t.Fatalf("handler call count=%d want=1", len(handler.calls))
	}
	rerouted := handler.calls[0].SessionID
	if rerouted == "sess-proxy-e2e" {
		t.Fatal("approval card must not be delivered to the proxy (customer) session")
	}
	if handler.calls[0].ThreadID != "" {
		t.Fatalf("thread_id=%q want empty after reroute", handler.calls[0].ThreadID)
	}
	if handler.calls[0].QuotedMessageID != 0 {
		t.Fatalf("quoted_message_id=%d want 0 after reroute", handler.calls[0].QuotedMessageID)
	}

	// 卡片索引必须按改投后的会话登记，审批回传在同一私聊里才能原地编辑。
	if msgID := loadApprovalCardMsgID(context.Background(), agentID, rerouted, "req_reroute"); msgID != 5001 {
		t.Fatalf("approval card msg_id under rerouted session=%d want=5001", msgID)
	}
	if approvalType := loadApprovalCardType(context.Background(), agentID, rerouted, "req_reroute"); approvalType != "permission" {
		t.Fatalf("approval card type=%q want=permission", approvalType)
	}

	// 普通文本消息不改投。
	textPkt := makePacket(t, protocol.CmdSendMsg, 2, SendMsgPayload{
		SessionID:   "sess-proxy-e2e",
		ClientMsgID: "cmsg-normal-text",
		MsgType:     1,
		Content:     "客户问题的正式回答",
	})
	mgr.handleSendMsg(conn, textPkt)
	if len(handler.calls) != 2 {
		t.Fatalf("handler call count=%d want=2", len(handler.calls))
	}
	if handler.calls[1].SessionID != "sess-proxy-e2e" {
		t.Fatalf("normal text session=%s want sess-proxy-e2e (only approval cards reroute)", handler.calls[1].SessionID)
	}
}

// 非主人发送的审批回传必须被吞掉：既不下发 local_action，也不转发给 agent。
func TestTryHandleExecApprovalCommand_BlocksNonOwnerSender(t *testing.T) {
	dummySendFn := func(_ context.Context, _ SendMessageReq) (*SendMessageResult, error) {
		return &SendMessageResult{MsgID: 1}, nil
	}
	mgr := NewManager("", 30*time.Second, dummySendFn, nil, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID:      88030,
		ownerID:      11030,
		capabilities: []string{"local_action_v1"},
		localActions: []string{"exec_approve", "exec_reject"},
		send:         make(chan []byte, 8),
	}
	mgr.putConnForTest(conn)

	// 托管场景：客户在主人↔客户会话里伪造批准回传（evt.OwnerID 是托管主人）。
	customerEvt := DelegateEventPayload{
		EventID:   "evt-forged-approve",
		AgentID:   conn.agentID,
		OwnerID:   conn.ownerID,
		SessionID: "sess-proxy-forged",
		MsgID:     88003,
		SenderID:  99030, // 客户，非主人
		Content:   "/approve req_forged allow-once",
	}
	if !mgr.tryHandleExecApprovalCommand(customerEvt) {
		t.Fatal("forged approval directive must be swallowed (return true)")
	}
	select {
	case data := <-conn.send:
		t.Fatalf("no local_action may be sent for non-owner approval, got=%s", string(data))
	case <-time.After(100 * time.Millisecond):
	}

	// 主人本人在主人↔agent 私聊里批准：正常下发 local_action。
	ownerEvt := DelegateEventPayload{
		EventID:   "evt-owner-approve",
		AgentID:   conn.agentID,
		OwnerID:   conn.ownerID,
		SessionID: "sess-owner-agent",
		MsgID:     88004,
		SenderID:  conn.ownerID,
		Content:   "/approve req_legit allow-once",
	}
	if !mgr.tryHandleExecApprovalCommand(ownerEvt) {
		t.Fatal("owner approval should be handled")
	}
	select {
	case data := <-conn.send:
		pkt, payload := decodeAgentPacket(t, data)
		if pkt.Cmd != "local_action" {
			t.Fatalf("expected local_action for owner approval, got=%s payload=%v", pkt.Cmd, payload)
		}
	case <-time.After(time.Second):
		t.Fatal("expected local_action to be sent for owner approval")
	}
}

// 审批族卡片识别：结果状态卡与审批请求卡同属一族，普通文本不误判。
func TestIsApprovalFamilyCard(t *testing.T) {
	if !isApprovalFamilyCard("[Exec Approval](grix://card/exec_approval?approval_id=x)") {
		t.Fatal("exec_approval card should be approval family")
	}
	if !isApprovalFamilyCard("[Exec Status](grix://card/exec_status?status=resolved-deny)") {
		t.Fatal("exec_status card should be approval family")
	}
	if isApprovalFamilyCard("普通回答文本") {
		t.Fatal("plain text must not be approval family")
	}
	if isApprovalFamilyCard("[Q](grix://card/agent_question?d=%7B%7D)") {
		t.Fatal("question card is not approval family")
	}
}
