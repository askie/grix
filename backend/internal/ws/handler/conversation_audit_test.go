package handler

import (
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/internal/conversationaudit"
	"github.com/askie/grix/backend/internal/featuregate"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestHandleAuditManifestRequiresAgentSelectionForMultipleTurns(t *testing.T) {
	tdb := testutil.NewTestDB()
	previous := store.DB
	store.DB = tdb.DB
	featuregate.InvalidateCache()
	t.Cleanup(func() {
		featuregate.InvalidateCache()
		store.DB = previous
		tdb.Close()
	})

	const ownerID int64 = 7101
	if _, err := featuregate.CreateGate(conversationaudit.FeatureGateKey, "test audit", model.FeatureStatusEnabled); err != nil {
		t.Fatalf("enable audit gate: %v", err)
	}
	for _, turn := range []model.ConversationAuditTurn{
		{ID: 1, OwnerID: ownerID, AgentID: 8102, SessionID: "audit-session", MsgID: 9101, EventID: "event-2", State: "ready", Revision: 2},
		{ID: 2, OwnerID: ownerID, AgentID: 8101, SessionID: "audit-session", MsgID: 9101, EventID: "event-1", State: "failed", Revision: 1},
	} {
		if err := store.DB.Create(&turn).Error; err != nil {
			t.Fatalf("seed audit turn: %v", err)
		}
	}

	payload, err := json.Marshal(protocol.AuditTurnRequest{SessionID: "audit-session", MsgID: 9101})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	conn := &MockConn{userID: ownerID, authed: true}
	HandleAuditGetManifest(nil, conn, &protocol.Packet{Seq: 99, Payload: payload})

	response, ok := conn.lastPayload.(protocol.AuditTurnResponse)
	if !ok {
		t.Fatalf("unexpected response type: %T", conn.lastPayload)
	}
	if response.State != "selection_required" || len(response.Targets) != 2 {
		t.Fatalf("expected agent choices, got %+v", response)
	}
	if response.Targets[0].AgentID != 8101 || response.Targets[1].AgentID != 8102 {
		t.Fatalf("targets must be ordered and complete: %+v", response.Targets)
	}
}
