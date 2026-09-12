package agentapi

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/pkg/agentscope"
)

func TestDispatchMessageEditRequiresScope(t *testing.T) {
	_, cleanup := setupAgentInvokeDispatchTest(t)
	defer cleanup()

	_, code, msg := dispatchAgentInvokeWithHooks(44120, 44121, "message_edit", map[string]interface{}{
		"session_id": "sess-1",
		"msg_id":     float64(1001),
		"content":    "updated",
	}, agentInvokeHooks{})
	if code != 4003 {
		t.Fatalf("message_edit without scope code=%d msg=%q, want 4003", code, msg)
	}
}

func TestDispatchMessageEditValidatesParams(t *testing.T) {
	const (
		ownerID = int64(44220)
		agentID = int64(44221)
	)
	testDB, cleanup := setupAgentInvokeDispatchTest(t)
	defer cleanup()
	seedAgentInvokeDispatchActor(t, testDB, ownerID, agentID, "ak_message_edit_validate")
	seedAgentInvokeDispatchScope(t, agentID, agentscope.ScopeMessageEdit)

	cases := []map[string]interface{}{
		{"msg_id": float64(1), "content": "x"},
		{"session_id": "sess-1", "content": "x"},
		{"session_id": "sess-1", "msg_id": float64(1)},
	}
	for _, params := range cases {
		_, code, _ := dispatchAgentInvokeWithHooks(agentID, ownerID, "message_edit", params, agentInvokeHooks{})
		if code != 4001 {
			t.Fatalf("params=%v code=%d want 4001", params, code)
		}
	}
}

func TestDispatchMessageEditWithScopeInvokesEditHookAndPassesThroughServiceError(t *testing.T) {
	const (
		ownerID = int64(44320)
		agentID = int64(44321)
	)
	testDB, cleanup := setupAgentInvokeDispatchTest(t)
	defer cleanup()
	seedAgentInvokeDispatchActor(t, testDB, ownerID, agentID, "ak_message_edit_success")
	seedAgentInvokeDispatchScope(t, agentID, agentscope.ScopeMessageEdit)

	var captured EditMsgPayload
	hooks := agentInvokeHooks{
		editMsg: func(ctx context.Context, gotAgentID, gotOwnerID int64, payload EditMsgPayload) error {
			captured = payload
			return nil
		},
	}

	data, code, msg := dispatchAgentInvokeWithHooks(agentID, ownerID, "message_edit", map[string]interface{}{
		"session_id": "sess-42",
		"msg_id":     float64(1002),
		"content":    "new content",
	}, hooks)
	if code != 0 {
		t.Fatalf("code=%d msg=%q", code, msg)
	}
	if captured.SessionID != "sess-42" || captured.MsgID != 1002 || captured.Content != "new content" {
		t.Fatalf("captured payload=%+v", captured)
	}
	// The invoke path must never grant the card/non-text bypass: only
	// trusted server-internal callers may set AllowCardMessage.
	if captured.AllowCardMessage {
		t.Fatal("message_edit invoke action must not set AllowCardMessage")
	}
	if edited, _ := data.(map[string]interface{})["edited"].(bool); !edited {
		t.Fatalf("data=%v want edited=true", data)
	}

	hooks.editMsg = func(ctx context.Context, gotAgentID, gotOwnerID int64, payload EditMsgPayload) error {
		return &SendError{Code: 20009, Msg: "card or non-text messages cannot be edited"}
	}
	_, code, msg = dispatchAgentInvokeWithHooks(agentID, ownerID, "message_edit", map[string]interface{}{
		"session_id": "sess-42",
		"msg_id":     float64(1003),
		"content":    "trying to edit a card",
	}, hooks)
	if code != 20009 {
		t.Fatalf("card edit rejection code=%d msg=%q, want 20009", code, msg)
	}
}
