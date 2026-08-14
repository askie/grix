package agentapi

import (
	"context"
	"testing"
	"time"

	toolstore "github.com/askie/grix/backend/internal/agenttoolbar/store"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	appstore "github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestPersistToolbarBinding_ClaudeEffortCanonicalAndLegacyMeta(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := appstore.DB
	appstore.DB = testDB.DB
	t.Cleanup(func() {
		appstore.DB = originalDB
	})

	mgr := NewManager("", 30*time.Second, (&mockSendMessageHandler{}).handle, nil, nil, nil)
	conn := &agentConn{
		agentID:    9901,
		ownerID:    1101,
		clientID:   "claude-effort",
		clientType: model.AgentClientTypeClaude,
		adapterID:  "claude/base",
		send:       make(chan []byte, 2),
	}
	const sessionID = "sess-claude-effort-persist"

	mgr.persistBindingFromCard(conn, sessionID, "/workspace/claude", "ready", map[string]any{
		"available_efforts": []any{"low", "high", "auto"},
		"effort":            "high",
	})

	mgr.persistToolbarBinding(conn, &pendingLocalAction{
		actionID:    "act-claude-effort-auto",
		kind:        "set_reasoning_effort",
		agentID:     conn.agentID,
		ownerID:     conn.ownerID,
		sessionID:   sessionID,
		referenceID: "auto",
	}, protocol.LocalActionResultPayload{
		Status: "ok",
		Result: map[string]any{
			"session_context": map[string]any{
				"effort": "auto",
			},
		},
	})

	record, ok, err := toolstore.LoadBinding(context.Background(), conn.agentID, sessionID)
	if err != nil {
		t.Fatalf("LoadBinding() error = %v", err)
	}
	if !ok {
		t.Fatal("expected Claude toolbar binding")
	}
	if got := record.Meta["effort"]; got != "auto" {
		t.Fatalf("effort=%v want=auto", got)
	}
	if got := record.Meta["reasoning_effort"]; got != "auto" {
		t.Fatalf("reasoning_effort=%v want=auto", got)
	}
}
