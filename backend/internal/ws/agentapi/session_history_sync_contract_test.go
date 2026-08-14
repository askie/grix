package agentapi

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestSessionHistorySyncWireContract(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:      98101,
		ownerID:      98102,
		clientID:     "history-sync-contract",
		adapterID:    "claude/base",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"session_control"},
		send:         make(chan []byte, 4),
	}
	mgr.putConnForTest(conn)

	resultCh := make(chan struct {
		resp *SessionHistorySyncResponse
		err  error
	}, 1)
	go func() {
		resp, err := mgr.SendSessionHistorySyncActionAndWait(
			conn.agentID,
			conn.ownerID,
			"sess-history-wire",
			"98102",
			"/workspace/project",
			"claude",
			"native-session-1",
			"cursor-1",
			100,
			"sync-run-1",
		)
		resultCh <- struct {
			resp *SessionHistorySyncResponse
			err  error
		}{resp: resp, err: err}
	}()

	var outbound protocol.Packet
	select {
	case raw := <-conn.send:
		if err := json.Unmarshal(raw, &outbound); err != nil {
			t.Fatalf("unmarshal outbound packet: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for sync_history local_action")
	}
	if outbound.Cmd != protocol.CmdLocalAction {
		t.Fatalf("cmd=%q want=%q", outbound.Cmd, protocol.CmdLocalAction)
	}
	var action protocol.LocalActionPayload
	if err := json.Unmarshal(outbound.Payload, &action); err != nil {
		t.Fatalf("unmarshal local_action payload: %v", err)
	}
	if action.ActionType != "session_control" {
		t.Fatalf("action_type=%q want=session_control", action.ActionType)
	}
	wants := map[string]any{
		"session_id":       "sess-history-wire",
		"verb":             "sync_history",
		"actor_id":         "98102",
		"provider_key":     "claude",
		"agent_session_id": "native-session-1",
		"cwd":              "/workspace/project",
		"cursor":           "cursor-1",
		"limit":            100,
		"sync_run_id":      "sync-run-1",
	}
	for key, want := range wants {
		got := action.Params[key]
		if number, ok := got.(float64); ok {
			got = int(number)
		}
		if got != want {
			t.Fatalf("params[%s]=%v want=%v", key, got, want)
		}
	}

	mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
		ActionID: action.ActionID,
		Status:   "ok",
		Result: map[string]any{
			"outcome":     "history_synced",
			"messages":    []any{},
			"has_more":    false,
			"next_cursor": "cursor-final",
			"sync_run_id": "sync-run-1",
		},
	}))

	select {
	case got := <-resultCh:
		if got.err != nil {
			t.Fatalf("SendSessionHistorySyncActionAndWait() error=%v", got.err)
		}
		if got.resp == nil || got.resp.NextCursor != "cursor-final" || got.resp.SyncRunID != "sync-run-1" || got.resp.HasMore {
			t.Fatalf("response=%+v", got.resp)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for decoded sync_history result")
	}
}

func TestSessionHistorySyncWireContractPreservesErrorCode(t *testing.T) {
	resultCh := make(chan *SessionHistorySyncResponse, 1)
	pending := &pendingLocalAction{
		sessionSyncResultCh: resultCh,
	}
	mgr := &Manager{}
	mgr.handleSessionHistorySyncPendingResult(pending, protocol.LocalActionResultPayload{
		Status:    "failed",
		ErrorCode: SessionHistorySyncErrorInvalidCursor,
		ErrorMsg:  "cursor no longer points into the transcript",
	})
	resp := <-resultCh
	err := sessionHistorySyncResponseError(resp)
	var syncErr *SessionHistorySyncError
	if !errors.As(err, &syncErr) {
		t.Fatalf("error=%v want *SessionHistorySyncError", err)
	}
	if syncErr.Code != SessionHistorySyncErrorInvalidCursor {
		t.Fatalf("error code=%q want=%q", syncErr.Code, SessionHistorySyncErrorInvalidCursor)
	}
}
