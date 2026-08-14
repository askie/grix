package handler

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	"github.com/askie/grix/backend/internal/conversationaudit"
	"github.com/askie/grix/backend/internal/featuregate"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func setupConversationAuditSetTest(t *testing.T) {
	t.Helper()
	tdb := testutil.NewTestDB()
	previous := store.DB
	store.DB = tdb.DB
	featuregate.InvalidateCache()
	previousResolve := resolveAuditToolbarSnapshot
	t.Cleanup(func() {
		resolveAuditToolbarSnapshot = previousResolve
		featuregate.InvalidateCache()
		store.DB = previous
		tdb.Close()
	})
}

func sendConversationAuditSet(t *testing.T, userID int64, payload protocol.ConversationAuditSetPayload) protocol.ConversationAuditSetRespPayload {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	conn := &MockConn{userID: userID, authed: true}
	HandleConversationAuditSet(nil, conn, &protocol.Packet{Seq: 7, Payload: raw})
	resp, ok := conn.lastPayload.(protocol.ConversationAuditSetRespPayload)
	if !ok {
		t.Fatalf("unexpected response type: %T", conn.lastPayload)
	}
	return resp
}

func TestHandleConversationAuditSetRejectsInvalidPayload(t *testing.T) {
	setupConversationAuditSetTest(t)

	resp := sendConversationAuditSet(t, 7101, protocol.ConversationAuditSetPayload{SessionID: "", AgentID: 0, Enabled: true})
	if resp.Code != 4001 {
		t.Fatalf("expected 4001 for invalid payload, got %+v", resp)
	}
}

func TestHandleConversationAuditSetRequiresFeatureGate(t *testing.T) {
	setupConversationAuditSetTest(t)

	resp := sendConversationAuditSet(t, 7101, protocol.ConversationAuditSetPayload{SessionID: "s1", AgentID: 8101, Enabled: true})
	if resp.Code != 4003 || resp.SessionID != "s1" || resp.AgentID != 8101 {
		t.Fatalf("expected 4003 echoing request coordinates, got %+v", resp)
	}
}

func TestHandleConversationAuditSetToolbarServiceUnavailable(t *testing.T) {
	setupConversationAuditSetTest(t)
	if _, err := featuregate.CreateGate(conversationaudit.FeatureGateKey, "test audit", model.FeatureStatusEnabled); err != nil {
		t.Fatalf("enable audit gate: %v", err)
	}
	resolveAuditToolbarSnapshot = func(context.Context, int64, string, int64) (toolprotocol.Snapshot, error) {
		return toolprotocol.Snapshot{}, errAuditToolbarUnavailable
	}

	resp := sendConversationAuditSet(t, 7101, protocol.ConversationAuditSetPayload{SessionID: "s1", AgentID: 8101, Enabled: true})
	if resp.Code != 5000 {
		t.Fatalf("expected 5000 when toolbar service missing, got %+v", resp)
	}
}

func TestHandleConversationAuditSetAccessDenied(t *testing.T) {
	setupConversationAuditSetTest(t)
	if _, err := featuregate.CreateGate(conversationaudit.FeatureGateKey, "test audit", model.FeatureStatusEnabled); err != nil {
		t.Fatalf("enable audit gate: %v", err)
	}
	resolveAuditToolbarSnapshot = func(context.Context, int64, string, int64) (toolprotocol.Snapshot, error) {
		return toolprotocol.Snapshot{}, errors.New("forbidden")
	}

	resp := sendConversationAuditSet(t, 7101, protocol.ConversationAuditSetPayload{SessionID: "s1", AgentID: 8101, Enabled: true})
	if resp.Code != 4003 {
		t.Fatalf("expected 4003 when snapshot access denied, got %+v", resp)
	}
}

// 私聊场景 resolver 忽略客户端传入的 target_agent_id、按会话成员解析真实 Agent；
// 写库与应答必须使用解析后的 AgentID，否则伪造 agent_id 会把开关写到未授权 Agent 上。
func TestHandleConversationAuditSetPersistsResolvedAgentID(t *testing.T) {
	setupConversationAuditSetTest(t)
	if _, err := featuregate.CreateGate(conversationaudit.FeatureGateKey, "test audit", model.FeatureStatusEnabled); err != nil {
		t.Fatalf("enable audit gate: %v", err)
	}
	resolveAuditToolbarSnapshot = func(_ context.Context, _ int64, sessionID string, _ int64) (toolprotocol.Snapshot, error) {
		return toolprotocol.Snapshot{SessionID: sessionID, AgentID: 9999}, nil
	}

	resp := sendConversationAuditSet(t, 7101, protocol.ConversationAuditSetPayload{SessionID: "s1", AgentID: 8101, Enabled: true})
	if resp.Code != 0 || resp.AgentID != 9999 || !resp.Enabled {
		t.Fatalf("expected success with resolved agent id, got %+v", resp)
	}
	enabled, err := conversationaudit.GetAuditEnabled(7101, 9999)
	if err != nil || !enabled {
		t.Fatalf("pref must persist for resolved agent 9999, enabled=%v err=%v", enabled, err)
	}
	forged, err := conversationaudit.GetAuditEnabled(7101, 8101)
	if err != nil || forged {
		t.Fatalf("pref must not persist for payload agent 8101, enabled=%v err=%v", forged, err)
	}
}
