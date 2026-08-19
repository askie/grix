package agentapi

import (
	"context"
	"strings"
	"testing"
	"time"

	toolstore "github.com/askie/grix/backend/internal/agenttoolbar/store"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	appstore "github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// setupUnbindFeedbackTest 准备 DB + MockRedis，并返回清理函数。
func setupUnbindFeedbackTest(t *testing.T) func() {
	t.Helper()
	testDB := testutil.NewTestDB()
	originalDB := appstore.DB
	appstore.DB = testDB.DB
	originalRDB := appstore.RDB
	appstore.RDB = testutil.NewMockRedis()
	return func() {
		appstore.DB = originalDB
		_ = appstore.RDB.Close()
		appstore.RDB = originalRDB
		testDB.Close()
	}
}

// stopped + 空 cwd 是连接器 session_control unbind 的解绑终态：持久化 cwd 清空、
// binding 卡消息映射删除（本次解绑卡走新消息而不是原地编辑旧卡）。
func TestHandleUpdateBindingCard_UnbindStoppedEmptyCwd(t *testing.T) {
	cleanup := setupUnbindFeedbackTest(t)
	defer cleanup()

	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{MsgID: 55002, InboxSeq: 1, CreatedAt: time.Now().UnixMilli()},
	}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:   9910,
		ownerID:   11010,
		clientID:  "claude-unbind-card",
		adapterID: "claude/base",
		send:      make(chan []byte, 2),
	}
	mgr.putConnForTest(conn)

	ctx := context.Background()
	if err := toolstore.UpsertBinding(ctx, toolstore.BindingRecord{
		AgentID: conn.agentID, SessionID: "sess-unbind-card", Cwd: "/workspace/demo", WorkerStatus: "ready",
	}); err != nil {
		t.Fatalf("seed binding: %v", err)
	}
	saveBindingCardMsgID(ctx, conn.agentID, "sess-unbind-card", 55001)

	mgr.handleUpdateBindingCard(conn, makePacket(t, protocol.CmdUpdateBindingCard, 1, map[string]any{
		"session_id":    "sess-unbind-card",
		"worker_status": "stopped",
		"cwd":           "",
	}))

	record, ok, err := toolstore.LoadBinding(ctx, conn.agentID, "sess-unbind-card")
	if err != nil || !ok {
		t.Fatalf("LoadBinding() record=%+v found=%v err=%v", record, ok, err)
	}
	if record.Cwd != "" {
		t.Fatalf("cwd=%q want empty after unbind", record.Cwd)
	}
	if record.WorkerStatus != "stopped" {
		t.Fatalf("worker_status=%q want=stopped", record.WorkerStatus)
	}

	// 映射删除 → 本次解绑卡走 sendNewBindingCard 发新消息（而非原地编辑旧卡）
	if len(sendHandler.calls) != 1 {
		t.Fatalf("send handler calls=%d want=1 (new unbind card)", len(sendHandler.calls))
	}
	if !strings.Contains(sendHandler.calls[0].Content, "已解绑工作目录。") {
		t.Fatalf("unbind card content=%q", sendHandler.calls[0].Content)
	}
	// 新卡的消息号取代旧映射（旧 55001 已删）
	if got := loadBindingCardMsgID(ctx, conn.agentID, "sess-unbind-card"); got != 55002 {
		t.Fatalf("binding card msg_id=%d want=55002", got)
	}
}

// 回归：stopped 带 cwd（claude stop worker 等合法用途）不是解绑——cwd 保留、
// 映射保留、仍原地编辑旧卡。
func TestHandleUpdateBindingCard_StoppedWithCwdKeepsBinding(t *testing.T) {
	cleanup := setupUnbindFeedbackTest(t)
	defer cleanup()

	sendHandler := &mockSendMessageHandler{}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	var editCalls []EditMsgPayload
	mgr.SetEditMsgHandler(func(_ context.Context, _, _ int64, payload EditMsgPayload) error {
		editCalls = append(editCalls, payload)
		return nil
	})
	conn := &agentConn{
		agentID:   9911,
		ownerID:   11011,
		clientID:  "claude-stop-card",
		adapterID: "claude/base",
		send:      make(chan []byte, 2),
	}
	mgr.putConnForTest(conn)

	ctx := context.Background()
	if err := toolstore.UpsertBinding(ctx, toolstore.BindingRecord{
		AgentID: conn.agentID, SessionID: "sess-stop-card", Cwd: "/workspace/demo", WorkerStatus: "ready",
	}); err != nil {
		t.Fatalf("seed binding: %v", err)
	}
	saveBindingCardMsgID(ctx, conn.agentID, "sess-stop-card", 55001)

	mgr.handleUpdateBindingCard(conn, makePacket(t, protocol.CmdUpdateBindingCard, 1, map[string]any{
		"session_id":    "sess-stop-card",
		"worker_status": "stopped",
		"cwd":           "/workspace/demo",
	}))

	record, _, _ := toolstore.LoadBinding(ctx, conn.agentID, "sess-stop-card")
	if record.Cwd != "/workspace/demo" {
		t.Fatalf("cwd=%q want=/workspace/demo (stop 不是解绑)", record.Cwd)
	}
	if got := loadBindingCardMsgID(ctx, conn.agentID, "sess-stop-card"); got != 55001 {
		t.Fatalf("binding card msg_id=%d want=55001 (映射保留)", got)
	}
	if len(editCalls) != 1 || editCalls[0].MsgID != 55001 {
		t.Fatalf("edit calls=%+v want in-place edit of msg 55001", editCalls)
	}
	if len(sendHandler.calls) != 0 {
		t.Fatalf("send handler calls=%d want=0 (不应发新卡)", len(sendHandler.calls))
	}
}

// local_action_result outcome="unbound"：状态清理（清 cwd、删 binding 卡映射）照常，
// 但不发聊天回执气泡——连接器 unbind 同连接保序先发的 update_binding_card(stopped)
// 已产生「已解绑工作目录。」新卡，回执会同文案双气泡。pending 带提交时快照的
// bindingCardMsgID（生产路径，session_control_bridge.go 提交即快照）。
func TestSessionControlResult_UnboundClearsBinding(t *testing.T) {
	cleanup := setupUnbindFeedbackTest(t)
	defer cleanup()

	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{MsgID: 55010, InboxSeq: 1, CreatedAt: time.Now().UnixMilli()},
	}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	var editCalls []EditMsgPayload
	mgr.SetEditMsgHandler(func(_ context.Context, _, _ int64, payload EditMsgPayload) error {
		editCalls = append(editCalls, payload)
		return nil
	})
	conn := &agentConn{
		agentID:      9912,
		ownerID:      11012,
		clientID:     "claude-unbind-result",
		adapterID:    "claude/base",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"session_control"},
		send:         make(chan []byte, 2),
	}
	mgr.putConnForTest(conn)

	ctx := context.Background()
	if err := toolstore.UpsertBinding(ctx, toolstore.BindingRecord{
		AgentID: conn.agentID, SessionID: "sess-unbind-result", Cwd: "/workspace/demo", WorkerStatus: "ready",
	}); err != nil {
		t.Fatalf("seed binding: %v", err)
	}
	saveBindingCardMsgID(ctx, conn.agentID, "sess-unbind-result", 55001)

	mgr.storePendingLocalAction(&pendingLocalAction{
		actionID:    "act-unbind-1",
		kind:        "session_control",
		agentID:     conn.agentID,
		ownerID:     conn.ownerID,
		sessionID:   "sess-unbind-result",
		actionType:  "session_control",
		referenceID: "unbind",
		// 生产路径：提交 local_action 时快照当时的绑定卡 msg_id
		bindingCardMsgID: 55001,
	})
	mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
		ActionID: "act-unbind-1",
		Status:   "ok",
		Result: map[string]any{
			"domain":    "session_control",
			"verb":      "unbind",
			"outcome":   "unbound",
			"sessionId": "sess-unbind-result",
		},
	}))

	record, ok, err := toolstore.LoadBinding(ctx, conn.agentID, "sess-unbind-result")
	if err != nil || !ok {
		t.Fatalf("LoadBinding() record=%+v found=%v err=%v", record, ok, err)
	}
	if record.Cwd != "" {
		t.Fatalf("cwd=%q want empty after unbound", record.Cwd)
	}
	// 绑定卡映射已删（后续 binding-missing 卡发新消息）
	if got := loadBindingCardMsgID(ctx, conn.agentID, "sess-unbind-result"); got != 0 {
		t.Fatalf("binding card msg_id=%d want=0 (映射已删)", got)
	}
	// 双气泡抑制：不发聊天回执（update_binding_card 的解绑卡已带同文案）
	if len(sendHandler.calls) != 0 {
		t.Fatalf("send handler calls=%d want=0 (unbound 不发回执气泡)", len(sendHandler.calls))
	}
	if len(editCalls) != 0 {
		t.Fatalf("edit calls=%+v want=0 (unbound 不 push_edit 旧卡)", editCalls)
	}
}
