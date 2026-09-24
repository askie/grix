package syncstream

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/syncstream/fold"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openNamedTestDB(t *testing.T, name string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+name+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.UserSyncHead{}, &model.UserSyncEvent{}); err != nil {
		t.Fatal(err)
	}
	return db
}

type deliveryFixture struct {
	session   model.Session
	recipient model.SessionMember
	message   model.Message
	edited    model.Message
}

func newDeliveryFixture() *deliveryFixture {
	f := &deliveryFixture{
		session:   model.Session{SessionID: "s1", OwnerID: 10, SessionType: 1, LastMsgSummary: "hi", StateVersion: 7},
		recipient: model.SessionMember{SessionID: "s1", MemberID: 20, MemberType: 1, UnreadCount: 3, LastReadMsgID: 11, StateVersion: 9},
		message:   model.Message{MsgID: 101, SessionID: "s1", SenderID: 10, Content: "hi", StateVersion: 1},
	}
	f.edited = f.message
	f.edited.Content, f.edited.StateVersion = "hi!", 2
	return f
}

// deliveries covers every shape the write points produce: a sender (session
// only), a recipient (session and unread), an edit (the session event is its
// own receipt) and a history import message (message only).
func (f *deliveryFixture) deliveries() []MessageDelivery {
	return []MessageDelivery{
		{UserID: 10, SessionID: "s1", Message: f.message, CommandID: "client-1", Session: &f.session},
		{UserID: 20, SessionID: "s1", Message: f.message, CommandID: "client-1", Session: &f.session, Member: &f.recipient},
		{UserID: 20, SessionID: "s1", Message: f.edited, CommandID: "edit-1", Session: &f.session, SessionCommandID: "edit-1"},
		{UserID: 20, SessionID: "s1", Message: f.message},
	}
}

func TestMessageDeliveryEventsAreClassicRowsWhenCompoundDisabled(t *testing.T) {
	t.Setenv("AIBOT_SYNC_COMPOUND_ENABLED", "")
	f := newDeliveryFixture()
	got := MessageDeliveryEvents(f.deliveries()...)
	unread := map[string]any{"session_id": "s1", "unread_count": 3, "last_read_msg_id": int64(11), "state_version": int64(9)}
	// Exactly the events the write points built inline before the helper.
	want := []Event{
		{UserID: 10, Kind: "message.upsert", EntityType: "message", EntityID: "101", EntityVersion: 1, CommandID: "client-1", Payload: f.message},
		{UserID: 10, Kind: "session.upsert", EntityType: "session", EntityID: "s1", EntityVersion: 7, Payload: f.session},
		{UserID: 20, Kind: "message.upsert", EntityType: "message", EntityID: "101", EntityVersion: 1, CommandID: "client-1", Payload: f.message},
		{UserID: 20, Kind: "session.upsert", EntityType: "session", EntityID: "s1", EntityVersion: 7, Payload: f.session},
		{UserID: 20, Kind: "session.unread_set", EntityType: "session_member", EntityID: "s1", EntityVersion: 9, Payload: unread},
		{UserID: 20, Kind: "message.upsert", EntityType: "message", EntityID: "101", EntityVersion: 2, CommandID: "edit-1", Payload: f.edited},
		{UserID: 20, Kind: "session.upsert", EntityType: "session", EntityID: "s1", EntityVersion: 7, CommandID: "edit-1", Payload: f.session},
		{UserID: 20, Kind: "message.upsert", EntityType: "message", EntityID: "101", EntityVersion: 1, Payload: f.message},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("classic events\n got %#v\nwant %#v", got, want)
	}
}

func rowEvent(row model.UserSyncEvent) protocol.SyncEventPayload {
	return protocol.SyncEventPayload{Cursor: row.StreamCursor, Kind: row.EventKind, EntityType: row.EntityType,
		EntityID: row.EntityID, EntityVersion: row.EntityVersion, Tombstone: row.Tombstone, CommandID: row.CommandID,
		Payload: json.RawMessage(row.Payload)}
}

func decodeJSON(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("payload %s: %v", raw, err)
	}
	return value
}

// Compound rows reserve one cursor per part, so expanding them gives a client
// without compound_v1 the classic rows at exactly the cursors they had.
func TestCompoundRowsExpandToClassicRowsAtSameCursors(t *testing.T) {
	appendAll := func(db *gorm.DB) {
		t.Helper()
		if err := db.Transaction(func(tx *gorm.DB) error {
			_, err := AppendTx(tx, MessageDeliveryEvents(newDeliveryFixture().deliveries()...))
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	classicDB := openNamedTestDB(t, "classic")
	t.Setenv("AIBOT_SYNC_COMPOUND_ENABLED", "")
	appendAll(classicDB)
	compoundDB := openNamedTestDB(t, "compound")
	t.Setenv("AIBOT_SYNC_COMPOUND_ENABLED", "1")
	appendAll(compoundDB)

	for userID, wantRows := range map[int64]int{10: 1, 20: 4} {
		var classicHead, compoundHead model.UserSyncHead
		if err := classicDB.First(&classicHead, "user_id = ?", userID).Error; err != nil {
			t.Fatal(err)
		}
		if err := compoundDB.First(&compoundHead, "user_id = ?", userID).Error; err != nil {
			t.Fatal(err)
		}
		if classicHead.HeadCursor != compoundHead.HeadCursor {
			t.Fatalf("user %d head classic=%d compound=%d", userID, classicHead.HeadCursor, compoundHead.HeadCursor)
		}
		var classicRows, compoundRows []model.UserSyncEvent
		if err := classicDB.Where("user_id = ?", userID).Order("stream_cursor").Find(&classicRows).Error; err != nil {
			t.Fatal(err)
		}
		if err := compoundDB.Where("user_id = ?", userID).Order("stream_cursor").Find(&compoundRows).Error; err != nil {
			t.Fatal(err)
		}
		if len(compoundRows) != wantRows {
			t.Fatalf("user %d compound rows=%d want %d", userID, len(compoundRows), wantRows)
		}
		// Clients apply each embedded part behind its own version barrier.
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(compoundRows[0].Payload, &fields); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"session", "unread"} {
			raw, ok := fields[key]
			if !ok {
				if key == "session" || userID == 20 {
					t.Fatalf("user %d compound lacks %s", userID, key)
				}
				continue
			}
			var part map[string]any
			if err := json.Unmarshal(raw, &part); err != nil {
				t.Fatal(err)
			}
			if part["state_version"] == nil || part["session_id"] != "s1" {
				t.Fatalf("user %d embedded %s lacks session_id/state_version: %v", userID, key, part)
			}
		}
		expanded, err := fold.Page(compoundRows, 0, fold.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if len(expanded.Events) != len(classicRows) || expanded.NextCursor != classicHead.HeadCursor {
			t.Fatalf("user %d expanded=%d classic=%d next=%d head=%d", userID, len(expanded.Events), len(classicRows), expanded.NextCursor, classicHead.HeadCursor)
		}
		for i, row := range classicRows {
			want, got := rowEvent(row), expanded.Events[i]
			wantPayload, gotPayload := decodeJSON(t, want.Payload), decodeJSON(t, got.Payload)
			want.Payload, got.Payload = nil, nil
			if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(gotPayload, wantPayload) {
				t.Fatalf("user %d event %d\n got %+v %v\nwant %+v %v", userID, i, got, gotPayload, want, wantPayload)
			}
		}
	}
}

func TestAppendTxRejectsSpanThatDisagreesWithPayload(t *testing.T) {
	compound, err := fold.CompoundPayload(json.RawMessage(`{"msg_id":"1","session_id":"s1"}`),
		json.RawMessage(`{"session_id":"s1","state_version":"1"}`), json.RawMessage(`{"session_id":"s1","state_version":1}`))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]Event{
		"span-without-parts":   {Kind: "message.upsert", EntityType: "message", Payload: map[string]any{"msg_id": "1"}, Span: 3},
		"parts-without-span":   {Kind: "message.upsert", EntityType: "message", Payload: compound},
		"span-on-other-kind":   {Kind: "session.upsert", EntityType: "session", Payload: map[string]any{}, Span: 2},
		"span-beyond-max-part": {Kind: "message.upsert", EntityType: "message", Payload: compound, Span: 4},
	}
	for name, event := range cases {
		db := openNamedTestDB(t, name)
		event.UserID, event.EntityID = 1, "1"
		err := db.Transaction(func(tx *gorm.DB) error {
			_, err := AppendTx(tx, []Event{event})
			return err
		})
		if err == nil {
			t.Fatalf("%s: AppendTx accepted a span the readers would disagree with", name)
		}
	}
}
