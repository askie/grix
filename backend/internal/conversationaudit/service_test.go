package conversationaudit

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/datatypes"
)

func init() { _ = snowflake.Init(1) }

func TestRequestedTurnAcceptsOnlyTurnScope(t *testing.T) {
	requested, err := RequestedTurn(json.RawMessage(`{"audit":{"enabled":true,"scope":"turn"}}`))
	if err != nil || !requested {
		t.Fatalf("turn audit should be accepted, requested=%v err=%v", requested, err)
	}
	requested, err = RequestedTurn(json.RawMessage(`{"audit":{"enabled":true,"scope":"session"}}`))
	if requested || !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("session audit should be rejected, requested=%v err=%v", requested, err)
	}
	requested, err = RequestedTurn(json.RawMessage(`{"audit":{"enabled":false}}`))
	if err != nil || requested {
		t.Fatalf("disabled audit should be ignored, requested=%v err=%v", requested, err)
	}
}

func TestRecordStateRequiresMarkedMessageAndPreservesCorrelation(t *testing.T) {
	tdb := testutil.NewTestDB()
	previous := store.DB
	store.DB = tdb.DB
	t.Cleanup(func() {
		store.DB = previous
		tdb.Close()
	})

	message := model.Message{
		MsgID: 9001, SessionID: "audit-session", SenderID: 7001, SenderType: 1, MsgType: 1,
		Content: "audit this", Extra: datatypes.JSON([]byte(`{"audit":{"enabled":true,"scope":"turn"}}`)),
	}
	if err := store.DB.Create(&message).Error; err != nil {
		t.Fatalf("seed message: %v", err)
	}
	if err := SetAuditEnabled(7001, 8001, true); err != nil {
		t.Fatalf("seed audit pref: %v", err)
	}

	accepted, err := RecordState(8001, 7001, StatePayload{
		EventID: "event-1", SessionID: "audit-session", MsgID: 9001,
		AuditID: "audit-1", TurnID: "turn-1", State: "accepted",
	})
	if err != nil {
		t.Fatalf("record accepted: %v", err)
	}
	if accepted.ID <= 0 || accepted.State != "accepted" || accepted.AuditID != "audit-1" {
		t.Fatalf("unexpected accepted state: %+v", accepted)
	}

	ready, err := RecordState(8001, 7001, StatePayload{
		EventID: "event-1", SessionID: "audit-session", MsgID: 9001,
		AuditID: "audit-1", TurnID: "turn-1", State: "ready", Revision: 2, Quality: "complete",
	})
	if err != nil {
		t.Fatalf("record ready: %v", err)
	}
	if ready.State != "ready" || ready.Revision != 2 || ready.Quality != "complete" {
		t.Fatalf("unexpected ready state: %+v", ready)
	}

	lookup, err := LookupTurn(7001, "audit-session", 9001, 8001)
	if err != nil || lookup.ID != ready.ID {
		t.Fatalf("lookup mismatch: turn=%+v err=%v", lookup, err)
	}
	byAuditID, err := LookupTurnByAuditID(7001, "audit-1")
	if err != nil || byAuditID.ID != ready.ID || byAuditID.AgentID != 8001 {
		t.Fatalf("lookup by audit id mismatch: turn=%+v err=%v", byAuditID, err)
	}
	if _, err := LookupTurnByAuditID(7002, "audit-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign owner lookup must be rejected, err=%v", err)
	}
	if _, err := RecordState(8001, 7002, StatePayload{
		EventID: "event-1", SessionID: "audit-session", MsgID: 9001,
		AuditID: "audit-1", TurnID: "turn-1", State: "failed",
	}); !errors.Is(err, ErrCorrelation) {
		t.Fatalf("wrong owner must be rejected, err=%v", err)
	}
}

func TestRecordStateIsMonotonicAcrossOutOfOrderPackets(t *testing.T) {
	tdb := testutil.NewTestDB()
	previous := store.DB
	store.DB = tdb.DB
	t.Cleanup(func() {
		store.DB = previous
		tdb.Close()
	})
	if err := store.DB.Create(&model.Message{
		MsgID: 9010, SessionID: "audit-session", SenderID: 7001, SenderType: 1, MsgType: 1,
		Extra: datatypes.JSON([]byte(`{"audit":{"enabled":true,"scope":"turn"}}`)),
	}).Error; err != nil {
		t.Fatalf("seed message: %v", err)
	}
	if err := SetAuditEnabled(7001, 8001, true); err != nil {
		t.Fatalf("seed audit pref: %v", err)
	}
	_, err := RecordState(8001, 7001, StatePayload{
		EventID: "event-order", SessionID: "audit-session", MsgID: 9010,
		AuditID: "audit-order", TurnID: "turn-order", State: "ready", Revision: 2,
		Quality: "complete", Truncated: true,
	})
	if err != nil {
		t.Fatalf("record ready: %v", err)
	}
	for _, stale := range []StatePayload{
		{EventID: "event-order", SessionID: "audit-session", MsgID: 9010, AuditID: "audit-order", TurnID: "turn-order", State: "failed", Revision: 1, ErrorCode: "late"},
		{EventID: "event-order", SessionID: "audit-session", MsgID: 9010, AuditID: "audit-order", TurnID: "turn-order", State: "partial", Revision: 2, Quality: "partial"},
		{EventID: "event-order", SessionID: "audit-session", MsgID: 9010, AuditID: "audit-order", TurnID: "turn-order", State: "ready", Revision: 1},
	} {
		actual, err := RecordState(8001, 7001, stale)
		if err != nil {
			t.Fatalf("record stale state: %v", err)
		}
		if actual.State != "ready" || actual.Revision != 2 || actual.Quality != "complete" || !actual.Truncated {
			t.Fatalf("stale state changed terminal record: %+v", actual)
		}
	}
	advanced, err := RecordState(8001, 7001, StatePayload{
		EventID: "event-order", SessionID: "audit-session", MsgID: 9010,
		AuditID: "audit-order", TurnID: "turn-order", State: "partial", Revision: 3, Quality: "partial",
	})
	if err != nil || advanced.State != "partial" || advanced.Revision != 3 {
		t.Fatalf("newer revision must replace prior terminal state: %+v err=%v", advanced, err)
	}
}

func TestListTurnsRequiresExplicitAgentSelection(t *testing.T) {
	tdb := testutil.NewTestDB()
	previous := store.DB
	store.DB = tdb.DB
	t.Cleanup(func() {
		store.DB = previous
		tdb.Close()
	})
	if err := store.DB.Create(&model.Message{
		MsgID: 9011, SessionID: "audit-session", SenderID: 7001, SenderType: 1, MsgType: 1,
		Extra: datatypes.JSON([]byte(`{"audit":{"enabled":true,"scope":"turn"}}`)),
	}).Error; err != nil {
		t.Fatalf("seed message: %v", err)
	}
	for _, agentID := range []int64{8001, 8002} {
		if err := SetAuditEnabled(7001, agentID, true); err != nil {
			t.Fatalf("seed audit pref agent=%d: %v", agentID, err)
		}
	}
	for _, input := range []struct {
		agentID int64
		eventID string
		state   string
	}{
		{8002, "event-agent-2", "ready"},
		{8001, "event-agent-1", "failed"},
	} {
		if _, err := RecordState(input.agentID, 7001, StatePayload{
			EventID: input.eventID, SessionID: "audit-session", MsgID: 9011,
			AuditID: "audit-" + input.eventID, TurnID: "turn-" + input.eventID, State: input.state,
		}); err != nil {
			t.Fatalf("record agent %d: %v", input.agentID, err)
		}
	}
	turns, err := ListTurns(7001, "audit-session", 9011)
	if err != nil || len(turns) != 2 || turns[0].AgentID != 8001 || turns[1].AgentID != 8002 {
		t.Fatalf("turn list must be deterministic: %+v err=%v", turns, err)
	}
	targets := Targets(turns)
	if len(targets) != 2 || targets[0].AgentID != 8001 || targets[1].State != "ready" {
		t.Fatalf("unexpected target list: %+v", targets)
	}
	selected, err := LookupTurn(7001, "audit-session", 9011, 8002)
	if err != nil || selected.AgentID != 8002 {
		t.Fatalf("explicit agent selection failed: %+v err=%v", selected, err)
	}
}

func TestRecordStateRejectsMessageWithoutTurnMarker(t *testing.T) {
	tdb := testutil.NewTestDB()
	previous := store.DB
	store.DB = tdb.DB
	t.Cleanup(func() {
		store.DB = previous
		tdb.Close()
	})
	if err := store.DB.Create(&model.Message{
		MsgID: 9002, SessionID: "audit-session", SenderID: 7001, SenderType: 1, MsgType: 1, Content: "ordinary",
	}).Error; err != nil {
		t.Fatalf("seed message: %v", err)
	}
	_, err := RecordState(8001, 7001, StatePayload{
		EventID: "event-2", SessionID: "audit-session", MsgID: 9002, State: "accepted",
	})
	if !errors.Is(err, ErrNotAudited) {
		t.Fatalf("unmarked message must be rejected, err=%v", err)
	}
}

func TestRecordStateRejectsAgentWithoutPreference(t *testing.T) {
	tdb := testutil.NewTestDB()
	previous := store.DB
	store.DB = tdb.DB
	t.Cleanup(func() {
		store.DB = previous
		tdb.Close()
	})
	if err := store.DB.Create(&model.Message{
		MsgID: 9012, SessionID: "audit-session", SenderID: 7001, SenderType: 1, MsgType: 1,
		Extra: datatypes.JSON([]byte(`{"audit":{"enabled":true,"scope":"turn"}}`)),
	}).Error; err != nil {
		t.Fatalf("seed message: %v", err)
	}
	// 群聊场景：消息因 agent 8001 的偏好被打标，但 8002 的开关是关的，
	// 它的 audit_state 必须被拒，伪造也一样。
	if err := SetAuditEnabled(7001, 8001, true); err != nil {
		t.Fatalf("seed audit pref: %v", err)
	}
	_, err := RecordState(8002, 7001, StatePayload{
		EventID: "event-foreign", SessionID: "audit-session", MsgID: 9012, State: "accepted",
	})
	if !errors.Is(err, ErrNotAudited) {
		t.Fatalf("agent without preference must be rejected, err=%v", err)
	}
	// 开关关掉后立即生效，进行中的事件也不再落库。
	if err := SetAuditEnabled(7001, 8001, false); err != nil {
		t.Fatalf("disable audit pref: %v", err)
	}
	_, err = RecordState(8001, 7001, StatePayload{
		EventID: "event-disabled", SessionID: "audit-session", MsgID: 9012, State: "accepted",
	})
	if !errors.Is(err, ErrNotAudited) {
		t.Fatalf("disabled preference must reject in-flight state, err=%v", err)
	}
}
