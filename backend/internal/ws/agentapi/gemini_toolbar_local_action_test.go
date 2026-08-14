package agentapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	agenttoolbar "github.com/askie/grix/backend/internal/agenttoolbar"
	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	"github.com/askie/grix/backend/internal/geminisession"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

type mockEditMessageHandler struct {
	calls []EditMsgPayload
	err   error
}

func (h *mockEditMessageHandler) handle(_ context.Context, _ int64, _ int64, payload EditMsgPayload) error {
	h.calls = append(h.calls, payload)
	return h.err
}

type toolbarFanoutCall struct {
	ownerID int64
	cmd     string
	payload any
}

type toolbarFanoutRecorder struct {
	calls []toolbarFanoutCall
}

func (r *toolbarFanoutRecorder) handle(_ context.Context, ownerID int64, cmd string, payload any) {
	r.calls = append(r.calls, toolbarFanoutCall{
		ownerID: ownerID,
		cmd:     cmd,
		payload: payload,
	})
}

type noopToolbarExecutor struct{}

func (noopToolbarExecutor) DispatchLocalAction(context.Context, core.LocalActionRequest) error {
	return nil
}

func (noopToolbarExecutor) StopOutput(context.Context, core.StopOutputRequest) error {
	return nil
}

func (noopToolbarExecutor) SendStopText(context.Context, core.StopOutputRequest) error {
	return nil
}

func TestHandleLocalActionResult_GeminiToolbarSelectionSuccessEditsCardAndPersistsContext(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	previousDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = previousDB
	})

	if err := geminisession.Upsert(context.Background(), geminisession.Snapshot{
		AgentID:   9001,
		SessionID: "sess-gemini-toolbar",
		ModeID:    "plan",
		ModelID:   "gemini-2.5-flash",
	}); err != nil {
		t.Fatalf("seed gemini session context: %v", err)
	}

	editHandler := &mockEditMessageHandler{}
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.editMsgFn = editHandler.handle

	conn := &agentConn{
		agentID:      9001,
		ownerID:      1001,
		isPrimary:    true,
		clientID:     "grix-gemini",
		adapterID:    "gemini/base",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"set_model"},
		send:         make(chan []byte, 2),
	}
	mgr.putConnForTest(conn)

	mgr.storePendingLocalAction(&pendingLocalAction{
		actionID:         "toolbar-success",
		kind:             "set_model",
		agentID:          conn.agentID,
		ownerID:          conn.ownerID,
		sessionID:        "sess-gemini-toolbar",
		actionType:       "set_model",
		referenceID:      "gemini-2.5-pro",
		displayLabel:     "Gemini 2.5 Pro",
		bindingCardMsgID: 7788,
	})

	mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
		ActionID: "toolbar-success",
		Status:   "ok",
		Result: map[string]any{
			"domain":  "session_control",
			"verb":    "set_model",
			"outcome": "model_set",
			"binding": map[string]any{
				"aibotSessionId": "sess-gemini-toolbar",
				"cwd":            "/workspace/gemini",
				"workerStatus":   "ready",
			},
		},
	}))

	if len(editHandler.calls) != 1 {
		t.Fatalf("edit calls=%d want=1", len(editHandler.calls))
	}
	if got := editHandler.calls[0].Content; got == "" {
		t.Fatal("edited card content should not be empty")
	}
	if got := editHandler.calls[0].Content; !strings.Contains(got, "grix://card/agent_status") {
		t.Fatalf("edited content=%q should contain agent_status card", got)
	}
	if got := editHandler.calls[0].Content; !strings.Contains(got, "Gemini 模型已切换为 Gemini 2.5 Pro。") {
		t.Fatalf("edited content=%q should contain success summary", got)
	}

	stored, ok, err := geminisession.Load(context.Background(), conn.agentID, "sess-gemini-toolbar")
	if err != nil {
		t.Fatalf("load gemini session context: %v", err)
	}
	if !ok {
		t.Fatal("expected gemini session context to exist")
	}
	if stored.ModeID != "plan" {
		t.Fatalf("stored mode=%q want=plan", stored.ModeID)
	}
	if stored.ModelID != "gemini-2.5-pro" {
		t.Fatalf("stored model=%q want=gemini-2.5-pro", stored.ModelID)
	}
}

func TestGeminiToolbarSelectionPendingReplies_FailureAndTimeout(t *testing.T) {
	testCases := []struct {
		name        string
		kind        string
		status      string
		errorMsg    string
		wantStatus  string
		wantSummary string
	}{
		{
			name:        "failed",
			kind:        "set_mode",
			status:      "failed",
			errorMsg:    "mode is not available",
			wantStatus:  "error",
			wantSummary: "切换 Gemini 审批模式失败。",
		},
		{
			name:        "timeout",
			kind:        "set_model",
			status:      "timeout",
			wantStatus:  "warning",
			wantSummary: "切换 Gemini 模型超时。",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			editHandler := &mockEditMessageHandler{}
			mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
			defer mgr.Shutdown()
			mgr.editMsgFn = editHandler.handle
			conn := &agentConn{
				agentID:      9002,
				ownerID:      1002,
				isPrimary:    true,
				clientID:     "grix-gemini",
				adapterID:    "gemini/base",
				capabilities: []string{"local_action_v1"},
				localActions: []string{"set_model", "set_mode"},
				send:         make(chan []byte, 2),
			}
			mgr.putConnForTest(conn)

			pending := &pendingLocalAction{
				actionID:         "toolbar-" + tc.name,
				kind:             tc.kind,
				agentID:          conn.agentID,
				ownerID:          conn.ownerID,
				sessionID:        "sess-gemini-toolbar",
				actionType:       tc.kind,
				referenceID:      "plan",
				displayLabel:     "只读计划",
				bindingCardMsgID: 8899,
			}

			if tc.status == "timeout" {
				mgr.storePendingLocalAction(pending)
				mgr.timeoutPendingLocalAction(pending.actionID)
			} else {
				mgr.storePendingLocalAction(pending)
				mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
					ActionID: pending.actionID,
					Status:   tc.status,
					ErrorMsg: tc.errorMsg,
				}))
			}

			if len(editHandler.calls) != 1 {
				t.Fatalf("edit calls=%d want=1", len(editHandler.calls))
			}
			if got := editHandler.calls[0].Content; !strings.Contains(got, "grix://card/agent_status") {
				t.Fatalf("edited content=%q should contain agent_status card", got)
			}
			if got := editHandler.calls[0].Content; !strings.Contains(got, tc.wantSummary) {
				t.Fatalf("edited content=%q want summary %q", got, tc.wantSummary)
			}
		})
	}
}

func TestDispatchToolbarLocalAction_ClaudeModeSwitchDoesNotSendGeminiSelectionCards(t *testing.T) {
	sendHandler := &mockSendMessageHandler{}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:      9301,
		ownerID:      1301,
		isPrimary:    true,
		clientID:     "grix-claude",
		adapterID:    "claude/base",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"session_control", "set_mode"},
		send:         make(chan []byte, 2),
	}
	mgr.putConnForTest(conn)

	ok := mgr.DispatchToolbarLocalAction(ToolbarLocalActionRequest{
		ActionID:     "toolbar-claude-set-mode",
		ActionType:   "set_mode",
		OwnerID:      conn.ownerID,
		AgentID:      conn.agentID,
		SessionID:    "sess-claude-toolbar",
		ReferenceID:  "full_auto",
		DisplayLabel: "全自动",
		Params: map[string]interface{}{
			"session_id": "sess-claude-toolbar",
			"mode_id":    "full_auto",
		},
		TimeoutMs: 300,
	})
	if !ok {
		t.Fatal("dispatch toolbar local action should succeed for online claude agent")
	}
	if len(sendHandler.calls) != 0 {
		t.Fatalf("send calls=%d want=0 before timeout", len(sendHandler.calls))
	}

	mgr.timeoutPendingLocalAction("toolbar-claude-set-mode")
	if len(sendHandler.calls) != 0 {
		t.Fatalf("send calls=%d want=0 after timeout", len(sendHandler.calls))
	}
}

func TestHandleLocalActionResult_GeminiToolbarSelectionRefreshesToolbarSnapshot(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	mockRedis := testutil.NewMockRedis()
	defer mockRedis.Close()

	previousDB := store.DB
	previousRDB := store.RDB
	store.DB = testDB.DB
	store.RDB = mockRedis
	t.Cleanup(func() {
		store.DB = previousDB
		store.RDB = previousRDB
		agenttoolbar.SetGlobal(nil)
	})

	const (
		ownerID   int64 = 1201
		agentID   int64 = 9201
		sessionID       = "sess-gemini-refresh"
	)

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:        sessionID,
		OwnerID:          ownerID,
		SessionType:      model.SessionTypeDirect,
		ModerationStatus: model.SessionModerationStatusActive,
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{
		SessionID:    sessionID,
		MemberID:     ownerID,
		MemberType:   1,
		LastActiveAt: now,
		JoinedAt:     now,
	}).Error; err != nil {
		t.Fatalf("create human member: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{
		SessionID:    sessionID,
		MemberID:     agentID,
		MemberType:   2,
		LastActiveAt: now,
		JoinedAt:     now,
	}).Error; err != nil {
		t.Fatalf("create agent member: %v", err)
	}
	if err := store.DB.Create(&model.Agent{
		ID:              agentID,
		AgentName:       "Gemini",
		OwnerID:         ownerID,
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: model.AgentClientTypeGemini,
		Status:          model.AgentStatusActive,
	}).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := toolruntime.StoreProfile(context.Background(), toolruntime.Profile{
		AgentID:      agentID,
		OwnerID:      ownerID,
		ClientType:   model.AgentClientTypeGemini,
		LocalActions: []string{"session_control", "set_model", "set_mode"},
		Online:       true,
	}, 0); err != nil {
		t.Fatalf("store runtime profile: %v", err)
	}
	if err := geminisession.Upsert(context.Background(), geminisession.Snapshot{
		AgentID:   agentID,
		SessionID: sessionID,
		ModeID:    "default",
		ModelID:   "gemini-2.5-flash",
	}); err != nil {
		t.Fatalf("seed gemini session context: %v", err)
	}

	fanout := &toolbarFanoutRecorder{}
	agenttoolbar.SetGlobal(agenttoolbar.NewService(agenttoolbar.Dependencies{
		Fanout:   fanout.handle,
		Executor: noopToolbarExecutor{},
	}))

	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{
			MsgID:     99101,
			InboxSeq:  1,
			CreatedAt: time.Now().UnixMilli(),
		},
	}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:      agentID,
		ownerID:      ownerID,
		isPrimary:    true,
		clientID:     "grix-gemini",
		adapterID:    "gemini/base",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"session_control", "set_model", "set_mode"},
		send:         make(chan []byte, 2),
	}
	mgr.putConnForTest(conn)
	mgr.storePendingLocalAction(&pendingLocalAction{
		actionID:     "toolbar-refresh",
		kind:         "set_model",
		agentID:      agentID,
		ownerID:      ownerID,
		sessionID:    sessionID,
		actionType:   "set_model",
		referenceID:  "gemini-2.5-pro",
		displayLabel: "Gemini 2.5 Pro",
	})

	mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
		ActionID: "toolbar-refresh",
		Status:   "ok",
		Result: map[string]any{
			"domain":  "session_control",
			"verb":    "set_model",
			"outcome": "model_set",
			"binding": map[string]any{
				"aibotSessionId": sessionID,
				"cwd":            "/workspace/gemini-refresh",
				"workerStatus":   "ready",
			},
		},
	}))

	if len(fanout.calls) == 0 {
		t.Fatal("expected toolbar refresh fanout")
	}

	last := fanout.calls[len(fanout.calls)-1]
	if last.cmd != protocol.CmdAgentToolbarSync {
		t.Fatalf("fanout cmd=%q want=%q", last.cmd, protocol.CmdAgentToolbarSync)
	}

	payloadMap, ok := last.payload.(map[string]any)
	if !ok {
		raw, err := json.Marshal(last.payload)
		if err != nil {
			t.Fatalf("marshal fanout payload: %v", err)
		}
		if err := json.Unmarshal(raw, &payloadMap); err != nil {
			t.Fatalf("decode fanout payload: %v", err)
		}
	}
	rawItems, _ := payloadMap["items"].([]any)
	if len(rawItems) == 0 {
		t.Fatal("expected toolbar items in sync payload")
	}
	hasSessionControl := false
	for _, rawItem := range rawItems {
		item, _ := rawItem.(map[string]any)
		if item["action_id"] == "session_control" {
			hasSessionControl = true
			break
		}
	}
	if !hasSessionControl {
		t.Fatal("session_control action missing from sync payload")
	}

	var modelItem map[string]any
	for _, rawItem := range rawItems {
		item, _ := rawItem.(map[string]any)
		if item["item_id"] == "select_model" && item["value"] == "gemini-2.5-pro" {
			modelItem = item
			break
		}
	}
	if modelItem == nil {
		t.Fatal("select_model item with value gemini-2.5-pro missing from sync payload")
	}
	if got := modelItem["value"]; got != "gemini-2.5-pro" {
		t.Fatalf("model value=%v want=gemini-2.5-pro", got)
	}
	if got, ok := modelItem["badge_text"]; ok && got != "Gemini 2.5 Pro" {
		t.Fatalf("model badge_text=%v want=Gemini 2.5 Pro when present", got)
	}
}
