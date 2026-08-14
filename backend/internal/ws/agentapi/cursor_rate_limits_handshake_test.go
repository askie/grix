package agentapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/agenttoolbar/agents/cursor"
	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	toolstore "github.com/askie/grix/backend/internal/agenttoolbar/store"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/ws/protocol"
	appstore "github.com/askie/grix/backend/internal/store"
)

func TestCursorRateLimitsHandshake_PersistAndToolbar(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "cursor-rate-limits-handshake.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fx struct {
		LocalActionResult     map[string]any `json:"local_action_result"`
		BindingMetaRateLimits map[string]any `json:"binding_meta_rate_limits"`
		Toolbar               struct {
			Items []struct {
				ItemID      string  `json:"itemId"`
				CenterText  string  `json:"centerText"`
				Percent     float64 `json:"percent"`
				LocalAction string  `json:"localAction"`
			} `json:"items"`
		} `json:"toolbar"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	testDB := testutil.NewTestDB()
	defer testDB.Close()
	originalDB := appstore.DB
	appstore.DB = testDB.DB
	t.Cleanup(func() { appstore.DB = originalDB })

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:      91001,
		ownerID:      11001,
		clientID:     "cursor-rate-limits-hs",
		adapterID:    "cursor/base",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"get_rate_limits"},
		send:         make(chan []byte, 2),
	}
	mgr.putConnForTest(conn)
	mgr.storePendingLocalAction(&pendingLocalAction{
		actionID:   "act-cursor-rl-hs",
		kind:       "get_rate_limits",
		agentID:    conn.agentID,
		ownerID:    conn.ownerID,
		sessionID:  "sess-cursor-rl-hs",
		actionType: "get_rate_limits",
	})

	mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
		ActionID: "act-cursor-rl-hs",
		Status:   "ok",
		Result:   fx.LocalActionResult,
	}))

	record, ok, err := toolstore.LoadBinding(context.Background(), conn.agentID, "sess-cursor-rl-hs")
	if err != nil {
		t.Fatalf("LoadBinding() error = %v", err)
	}
	if !ok {
		t.Fatal("expected toolbar binding to be persisted")
	}
	metaRL, ok := record.Meta["rate_limits"].(map[string]any)
	if !ok {
		t.Fatalf("rate_limits=%#v want map", record.Meta["rate_limits"])
	}
	if got := metaRL["sampledAt"]; got != float64(1700000000000) {
		t.Fatalf("sampledAt=%v want 1700000000000", got)
	}

	pkg := cursor.New()
	snapshot, err := pkg.Build(context.Background(), core.BuildInput{
		OwnerID: conn.ownerID,
		Session: core.SessionInfo{SessionID: "sess-cursor-rl-hs"},
		Agent: core.AgentInfo{
			AgentID:      conn.agentID,
			OwnerID:      conn.ownerID,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypeCursor,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"get_rate_limits", "session_control", "set_model", "set_mode"},
		},
		Binding: core.BindingInfo{
			Cwd:  "/workspace/project",
			Meta: record.Meta,
		},
		Run: toolruntime.RunState{},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	for _, want := range fx.Toolbar.Items {
		item, ok := snapshot.FindItem(want.ItemID)
		if !ok {
			t.Fatalf("missing toolbar item %q after persist", want.ItemID)
		}
		if item.CenterText != want.CenterText || item.Percent != want.Percent || item.LocalAction != want.LocalAction {
			t.Fatalf("item %+v want center=%s percent=%v action=%s", item, want.CenterText, want.Percent, want.LocalAction)
		}
	}
}
