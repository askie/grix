package fold

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/datatypes"
)

func classicRow(cursor int64, kind, entity, id string, version int64, commandID, payload string) model.UserSyncEvent {
	return model.UserSyncEvent{UserID: 1, StreamCursor: cursor, EventKind: kind, EntityType: entity, EntityID: id,
		EntityVersion: version, Tombstone: kind == "message.revoke" || kind == "session.remove",
		CommandID: commandID, Payload: datatypes.JSON(payload)}
}

func messageJSON(msgID, sessionID string, version int64) string {
	return fmt.Sprintf(`{"msg_id":%q,"session_id":%q,"content":"hi","state_version":"%d"}`, msgID, sessionID, version)
}

func sessionJSON(sessionID string, version int64) string {
	return fmt.Sprintf(`{"session_id":%q,"last_msg_summary":"hi","state_version":"%d"}`, sessionID, version)
}

func unreadJSON(sessionID string, count, version int64) string {
	return fmt.Sprintf(`{"session_id":%q,"unread_count":%d,"last_read_msg_id":0,"state_version":%d}`, sessionID, count, version)
}

// compoundRow is a compound message row whose span ends at cursor.
func compoundRow(t *testing.T, cursor int64, msgID, sessionID string, version int64, commandID, session, unread string) model.UserSyncEvent {
	t.Helper()
	payload, err := CompoundPayload(json.RawMessage(messageJSON(msgID, sessionID, version)), raw(session), raw(unread))
	if err != nil {
		t.Fatal(err)
	}
	return model.UserSyncEvent{UserID: 1, StreamCursor: cursor, EventKind: "message.upsert", EntityType: "message",
		EntityID: msgID, EntityVersion: version, CommandID: commandID, Payload: datatypes.JSON(payload)}
}

func raw(value string) json.RawMessage {
	if value == "" {
		return nil
	}
	return json.RawMessage(value)
}

func page(t *testing.T, rows []model.UserSyncEvent, from int64, opts Options) Result {
	t.Helper()
	result, err := Page(rows, from, opts)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

var folded = Options{Compound: true, Fold: true}

// assertCovered is the compound_v1 client check: each event covers
// first_cursor..cursor right after the previous one, and the last ends at next.
func assertCovered(t *testing.T, events []protocol.SyncEventPayload, from, next int64) {
	t.Helper()
	last := from
	for _, event := range events {
		first := event.FirstCursor
		if first == 0 {
			first = event.Cursor
		} else if first == event.Cursor {
			t.Fatalf("first_cursor must be omitted when it equals cursor: %+v", event)
		}
		if first != last+1 || event.Cursor < first {
			t.Fatalf("event %d..%d does not follow cursor %d", first, event.Cursor, last)
		}
		last = event.Cursor
	}
	if last != next {
		t.Fatalf("last event cursor=%d next_cursor=%d", last, next)
	}
}

// assertContiguous is the check every client before compound_v1 applies.
func assertContiguous(t *testing.T, events []protocol.SyncEventPayload, from, next int64) {
	t.Helper()
	last := from
	for _, event := range events {
		if event.Cursor != last+1 || event.FirstCursor != 0 {
			t.Fatalf("classic event cursor=%d first=%d after %d", event.Cursor, event.FirstCursor, last)
		}
		last = event.Cursor
	}
	if last != next {
		t.Fatalf("last event cursor=%d next_cursor=%d", last, next)
	}
}

func cursors(events []protocol.SyncEventPayload) []int64 {
	out := make([]int64, 0, len(events))
	for _, event := range events {
		out = append(out, event.Cursor)
	}
	return out
}

func sameJSON(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	var a, b any
	if err := json.Unmarshal(got, &a); err != nil {
		t.Fatalf("payload %s: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("payload\n got %s\nwant %s", got, want)
	}
}

// Rule 1: session and session_member events keep the last per kind and entity.
func TestFoldKeepsLastSessionStatePerKindAndEntity(t *testing.T) {
	rows := []model.UserSyncEvent{
		classicRow(1, "session.upsert", "session", "s1", 1, "", sessionJSON("s1", 1)),
		classicRow(2, "session.unread_set", "session_member", "s1", 1, "", unreadJSON("s1", 1, 1)),
		classicRow(3, "session.upsert", "session", "s2", 1, "", sessionJSON("s2", 1)),
		classicRow(4, "session.upsert", "session", "s1", 2, "", sessionJSON("s1", 2)),
		classicRow(5, "session.unread_set", "session_member", "s1", 2, "", unreadJSON("s1", 2, 2)),
		classicRow(6, "session.read_state", "session_member", "s1:9", 1, "", `{"session_id":"s1"}`),
		classicRow(7, "session.read_state", "session_member", "s1:9", 2, "", `{"session_id":"s1"}`),
	}
	result := page(t, rows, 0, folded)
	if got := cursors(result.Events); !reflect.DeepEqual(got, []int64{3, 4, 5, 7}) {
		t.Fatalf("kept cursors=%v", got)
	}
	if result.Events[1].EntityVersion != 2 || result.Events[2].EntityVersion != 2 || result.Events[3].EntityVersion != 2 {
		t.Fatalf("kept versions: %+v", result.Events)
	}
	assertCovered(t, result.Events, 0, 7)
}

// Rule 2: message.upsert keeps the last per msg_id.
func TestFoldKeepsLastUpsertPerMessage(t *testing.T) {
	rows := []model.UserSyncEvent{
		classicRow(1, "message.upsert", "message", "m1", 1, "", messageJSON("m1", "s1", 1)),
		classicRow(2, "message.upsert", "message", "m2", 1, "", messageJSON("m2", "s1", 1)),
		classicRow(3, "message.upsert", "message", "m1", 2, "", messageJSON("m1", "s1", 2)),
	}
	result := page(t, rows, 0, folded)
	if got := cursors(result.Events); !reflect.DeepEqual(got, []int64{2, 3}) {
		t.Fatalf("kept cursors=%v", got)
	}
	sameJSON(t, result.Events[1].Payload, messageJSON("m1", "s1", 2))
	assertCovered(t, result.Events, 0, 3)
}

// Rule 3: an upsert followed by a revoke of the same message keeps the revoke.
// A revoke is never replaced by a later upsert.
func TestFoldRevokeReplacesEarlierUpsertOnly(t *testing.T) {
	rows := []model.UserSyncEvent{
		classicRow(1, "message.upsert", "message", "m1", 1, "", messageJSON("m1", "s1", 1)),
		classicRow(2, "message.revoke", "message", "m1", 2, "", messageJSON("m1", "s1", 2)),
		classicRow(3, "message.revoke", "message", "m2", 1, "", messageJSON("m2", "s1", 1)),
		classicRow(4, "message.upsert", "message", "m2", 2, "", messageJSON("m2", "s1", 2)),
	}
	result := page(t, rows, 0, folded)
	if got := cursors(result.Events); !reflect.DeepEqual(got, []int64{2, 3, 4}) {
		t.Fatalf("kept cursors=%v", got)
	}
	if result.Events[0].Kind != "message.revoke" || result.Events[0].FirstCursor != 1 {
		t.Fatalf("revoke event: %+v", result.Events[0])
	}
	assertCovered(t, result.Events, 0, 4)
}

// Rule 4: an embedded part is cleared when the same session changes later in
// the page; a replaced message keeps its still-latest parts as classic events.
func TestFoldCompoundClearsSupersededEmbeddedParts(t *testing.T) {
	rows := []model.UserSyncEvent{
		compoundRow(t, 3, "m1", "s1", 1, "", sessionJSON("s1", 1), unreadJSON("s1", 1, 1)),
		compoundRow(t, 6, "m2", "s1", 1, "", sessionJSON("s1", 2), unreadJSON("s1", 2, 2)),
	}
	result := page(t, rows, 0, folded)
	if len(result.Events) != 2 || result.Reordered != 0 {
		t.Fatalf("result: %+v", result)
	}
	first := result.Events[0]
	if first.Kind != "message.upsert" || first.EntityID != "m1" || first.Cursor != 1 || first.FirstCursor != 0 {
		t.Fatalf("first compound: %+v", first)
	}
	sameJSON(t, first.Payload, messageJSON("m1", "s1", 1))
	if second := result.Events[1]; second.FirstCursor != 2 || second.Cursor != 6 || string(second.Payload) != string(rows[1].Payload) {
		t.Fatalf("whole compound must pass unchanged: %+v", second)
	}
	assertCovered(t, result.Events, 0, 6)

	edited := []model.UserSyncEvent{
		compoundRow(t, 3, "m1", "s1", 1, "", sessionJSON("s1", 1), unreadJSON("s1", 1, 1)),
		classicRow(4, "message.upsert", "message", "m1", 2, "edit-1", messageJSON("m1", "s1", 2)),
		classicRow(5, "session.upsert", "session", "s1", 2, "edit-1", sessionJSON("s1", 2)),
	}
	result = page(t, edited, 0, folded)
	if got := cursors(result.Events); !reflect.DeepEqual(got, []int64{2, 3, 4, 5}) {
		t.Fatalf("kept cursors=%v", got)
	}
	session, unread := result.Events[0], result.Events[1]
	if session.Kind != "session.upsert" || session.EntityID != "s1" || session.EntityVersion != 1 || session.FirstCursor != 1 {
		t.Fatalf("the unread's session must come first: %+v", session)
	}
	if unread.Kind != "session.unread_set" || unread.EntityType != "session_member" || unread.EntityID != "s1" ||
		unread.EntityVersion != 1 || unread.FirstCursor != 0 || unread.CommandID != "" {
		t.Fatalf("still-latest unread part must go out as a classic event: %+v", unread)
	}
	sameJSON(t, unread.Payload, unreadJSON("s1", 1, 1))
	assertCovered(t, result.Events, 0, 5)
}

// A client applies unread only to a session row it already has, so folding
// must never leave a session's unread ahead of every session.upsert that the
// unfolded page had before it.
func TestFoldKeepsSessionUpsertBeforeUnreadOfItsSession(t *testing.T) {
	// Kept unread and a superseded session part of the same compound row are
	// embedded together again.
	together := []model.UserSyncEvent{
		compoundRow(t, 3, "m1", "s1", 1, "", sessionJSON("s1", 1), unreadJSON("s1", 1, 1)),
		compoundRow(t, 5, "m2", "s1", 1, "", sessionJSON("s1", 2), ""),
	}
	result := page(t, together, 0, folded)
	if result.Reordered != 1 || len(result.Events) != 2 {
		t.Fatalf("result: %+v", result)
	}
	if first := result.Events[0]; first.Cursor != 3 || first.FirstCursor != 1 || string(first.Payload) != string(together[0].Payload) {
		t.Fatalf("session and unread must stay embedded together: %+v", first)
	}
	assertCovered(t, result.Events, 0, 5)

	// A revoke writes its unread before its session.upsert; the send's
	// session part stays as the classic event ahead of them.
	revoked := []model.UserSyncEvent{
		compoundRow(t, 3, "m1", "s1", 1, "", sessionJSON("s1", 1), unreadJSON("s1", 1, 1)),
		classicRow(4, "message.revoke", "message", "m1", 2, "revoke-1", messageJSON("m1", "s1", 2)),
		classicRow(5, "session.unread_set", "session_member", "s1", 2, "", unreadJSON("s1", 0, 2)),
		classicRow(6, "session.upsert", "session", "s1", 3, "revoke-1", sessionJSON("s1", 3)),
	}
	result = page(t, revoked, 0, folded)
	if got := cursors(result.Events); !reflect.DeepEqual(got, []int64{2, 4, 5, 6}) || result.Reordered != 1 {
		t.Fatalf("kept cursors=%v reordered=%d", got, result.Reordered)
	}
	if first := result.Events[0]; first.Kind != "session.upsert" || first.EntityVersion != 1 || first.FirstCursor != 1 {
		t.Fatalf("session must precede the revoke's unread: %+v", first)
	}
	assertCovered(t, result.Events, 0, 6)

	// The client's own read_state is unread state too.
	read := []model.UserSyncEvent{
		classicRow(1, "session.upsert", "session", "s1", 1, "", sessionJSON("s1", 1)),
		classicRow(2, "session.read_state", "session_member", "s1", 1, "read-1", unreadJSON("s1", 0, 1)),
		classicRow(3, "session.upsert", "session", "s1", 2, "", sessionJSON("s1", 2)),
	}
	if result = page(t, read, 0, folded); result.Reordered != 1 || len(result.Events) != 3 {
		t.Fatalf("read_state: %+v", result)
	}

	// A removal ends the session: an upsert before it does not precede an
	// unread after it, in the unfolded page either.
	removed := []model.UserSyncEvent{
		classicRow(1, "session.upsert", "session", "s1", 1, "", sessionJSON("s1", 1)),
		classicRow(2, "session.remove", "session", "s1", 2, "", `{"reason":"access_revoked"}`),
		classicRow(3, "session.unread_set", "session_member", "s1", 2, "", unreadJSON("s1", 1, 2)),
		classicRow(4, "session.upsert", "session", "s1", 3, "", sessionJSON("s1", 3)),
	}
	result = page(t, removed, 0, folded)
	if got := cursors(result.Events); !reflect.DeepEqual(got, []int64{2, 3, 4}) || result.Reordered != 0 {
		t.Fatalf("kept cursors=%v reordered=%d", got, result.Reordered)
	}
}

// Rule ④: a superseded event whose command_id no survivor carries is the only
// receipt of that client command and stays.
func TestFoldKeepsReceiptsOfSupersededCommands(t *testing.T) {
	rows := []model.UserSyncEvent{
		classicRow(1, "session.unread_set", "session_member", "s1", 1, "read-1", unreadJSON("s1", 0, 1)),
		classicRow(2, "session.unread_set", "session_member", "s1", 2, "", unreadJSON("s1", 1, 2)),
		classicRow(3, "message.upsert", "message", "m1", 1, "send-1", messageJSON("m1", "s1", 1)),
		classicRow(4, "message.upsert", "message", "m1", 2, "send-1", messageJSON("m1", "s1", 2)),
		compoundRow(t, 6, "m2", "s2", 1, "send-2", sessionJSON("s2", 1), ""),
		compoundRow(t, 8, "m2", "s2", 2, "", sessionJSON("s2", 2), ""),
	}
	result := page(t, rows, 0, folded)
	if result.Receipts != 2 {
		t.Fatalf("receipts=%d", result.Receipts)
	}
	if got := cursors(result.Events); !reflect.DeepEqual(got, []int64{1, 2, 4, 5, 8}) {
		t.Fatalf("kept cursors=%v", got)
	}
	receipt := result.Events[3]
	if receipt.CommandID != "send-2" || receipt.Kind != "message.upsert" {
		t.Fatalf("compound receipt: %+v", receipt)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(receipt.Payload, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["session"]; ok {
		t.Fatalf("superseded session part kept with the receipt: %s", receipt.Payload)
	}
	assertCovered(t, result.Events, 0, 8)
}

// A session.remove reason decides whether messages are deleted, so removals
// are never folded into one another.
func TestFoldNeverFoldsSessionRemove(t *testing.T) {
	rows := []model.UserSyncEvent{
		classicRow(1, "session.remove", "session", "s1", 1, "", `{"reason":"history_reset"}`),
		classicRow(2, "session.upsert", "session", "s1", 2, "", sessionJSON("s1", 2)),
		classicRow(3, "session.remove", "session", "s1", 3, "", `{"reason":"access_revoked"}`),
	}
	result := page(t, rows, 0, folded)
	if got := cursors(result.Events); !reflect.DeepEqual(got, []int64{1, 2, 3}) {
		t.Fatalf("kept cursors=%v", got)
	}
}

// Folding never moves the page bound: next_cursor and the consumed rows are
// those of the unfolded page, and the events still cover every cursor.
func TestFoldKeepsPageCursorBound(t *testing.T) {
	rows := []model.UserSyncEvent{
		classicRow(11, "session.upsert", "session", "s1", 1, "", sessionJSON("s1", 1)),
		compoundRow(t, 14, "m1", "s1", 1, "", sessionJSON("s1", 2), unreadJSON("s1", 1, 2)),
		compoundRow(t, 17, "m1", "s1", 2, "", sessionJSON("s1", 3), unreadJSON("s1", 1, 3)),
		classicRow(18, "membership.changed", "membership", "s1", 3, "", `{}`),
		compoundRow(t, 21, "m2", "s1", 1, "", sessionJSON("s1", 4), unreadJSON("s1", 2, 4)),
	}
	unfolded := page(t, rows, 10, Options{Compound: true})
	foldedPage := page(t, rows, 10, folded)
	if unfolded.NextCursor != 21 || foldedPage.NextCursor != 21 || foldedPage.Rows != len(rows) || unfolded.Rows != len(rows) {
		t.Fatalf("bounds unfolded=%+v folded=%+v", unfolded, foldedPage)
	}
	if len(unfolded.Events) != 5 || len(foldedPage.Events) != 3 {
		t.Fatalf("events unfolded=%d folded=%d", len(unfolded.Events), len(foldedPage.Events))
	}
	assertCovered(t, unfolded.Events, 10, 21)
	assertCovered(t, foldedPage.Events, 10, 21)
	if got := cursors(foldedPage.Events); !reflect.DeepEqual(got, []int64{15, 18, 21}) {
		t.Fatalf("kept cursors=%v", got)
	}
	if foldedPage.Events[0].FirstCursor != 11 || foldedPage.Events[0].EntityID != "m1" {
		t.Fatalf("m1 must keep only its message part: %+v", foldedPage.Events[0])
	}
}

// A connection without compound_v1 gets the classic rows back at their own
// cursors, never folded.
func TestExpandRestoresClassicRowsAtReservedCursors(t *testing.T) {
	rows := []model.UserSyncEvent{
		classicRow(4, "session.upsert", "session", "s0", 1, "", sessionJSON("s0", 1)),
		compoundRow(t, 7, "m1", "s1", 3, "send-1", sessionJSON("s1", 5), unreadJSON("s1", 2, 9)),
		compoundRow(t, 9, "m1", "s1", 4, "", sessionJSON("s1", 6), ""),
	}
	result := page(t, rows, 3, Options{Fold: true})
	assertContiguous(t, result.Events, 3, 9)
	want := []struct {
		kind, entity, id, commandID, payload string
		version                              int64
	}{
		{"session.upsert", "session", "s0", "", sessionJSON("s0", 1), 1},
		{"message.upsert", "message", "m1", "send-1", messageJSON("m1", "s1", 3), 3},
		{"session.upsert", "session", "s1", "", sessionJSON("s1", 5), 5},
		{"session.unread_set", "session_member", "s1", "", unreadJSON("s1", 2, 9), 9},
		{"message.upsert", "message", "m1", "", messageJSON("m1", "s1", 4), 4},
		{"session.upsert", "session", "s1", "", sessionJSON("s1", 6), 6},
	}
	if len(result.Events) != len(want) || result.Rows != 3 || result.NextCursor != 9 {
		t.Fatalf("result: %+v", result)
	}
	for i, w := range want {
		got := result.Events[i]
		if got.Kind != w.kind || got.EntityType != w.entity || got.EntityID != w.id || got.CommandID != w.commandID || got.EntityVersion != w.version || got.Tombstone {
			t.Fatalf("event %d: %+v want %+v", i, got, w)
		}
		sameJSON(t, got.Payload, w.payload)
	}
	if string(result.Events[0].Payload) != string(rows[0].Payload) {
		t.Fatal("classic rows must pass through unchanged")
	}
}

func TestExpandCapsEventsAtWholeRows(t *testing.T) {
	rows := make([]model.UserSyncEvent, 0, 40)
	for i := int64(1); i <= 40; i++ {
		rows = append(rows, compoundRow(t, i*3, fmt.Sprint(i), "s1", i, "", sessionJSON("s1", i), unreadJSON("s1", i, i)))
	}
	result := page(t, rows, 0, Options{MaxEvents: 100})
	if result.Rows != 33 || result.NextCursor != 99 || len(result.Events) != 99 {
		t.Fatalf("capped page rows=%d next=%d events=%d", result.Rows, result.NextCursor, len(result.Events))
	}
	assertContiguous(t, result.Events, 0, 99)
	rest := page(t, rows[result.Rows:], result.NextCursor, Options{MaxEvents: 100})
	if rest.Rows != 7 || rest.NextCursor != 120 {
		t.Fatalf("second page rows=%d next=%d", rest.Rows, rest.NextCursor)
	}
	assertContiguous(t, rest.Events, 99, 120)
}

func TestCompoundWithoutFoldSendsOneEventPerRow(t *testing.T) {
	rows := []model.UserSyncEvent{
		classicRow(1, "session.upsert", "session", "s1", 1, "", sessionJSON("s1", 1)),
		compoundRow(t, 4, "m1", "s1", 1, "send-1", sessionJSON("s1", 2), unreadJSON("s1", 1, 1)),
	}
	result := page(t, rows, 0, Options{Compound: true})
	if len(result.Events) != 2 || result.Receipts != 0 {
		t.Fatalf("events: %+v", result.Events)
	}
	event := result.Events[1]
	if event.Cursor != 4 || event.FirstCursor != 2 || event.CommandID != "send-1" || string(event.Payload) != string(rows[1].Payload) {
		t.Fatalf("compound event: %+v", event)
	}
	assertCovered(t, result.Events, 0, 4)
}

func TestPageEmptyStillHasEventList(t *testing.T) {
	for _, opts := range []Options{{}, {Compound: true, Fold: true}} {
		result := page(t, nil, 5, opts)
		raw, err := json.Marshal(result.Events)
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != "[]" || result.NextCursor != 5 || result.Rows != 0 {
			t.Fatalf("opts=%+v events=%s next=%d", opts, raw, result.NextCursor)
		}
	}
}

// A resume cursor inside a span (never produced by a client) only gets the
// cursors after it.
func TestPageResumeInsideSpanSkipsDeliveredParts(t *testing.T) {
	rows := []model.UserSyncEvent{compoundRow(t, 7, "m1", "s1", 1, "", sessionJSON("s1", 2), unreadJSON("s1", 1, 3))}
	classic := page(t, rows, 5, Options{})
	assertContiguous(t, classic.Events, 5, 7)
	if classic.Events[0].Kind != "session.upsert" {
		t.Fatalf("classic: %+v", classic.Events)
	}
	compound := page(t, rows, 5, folded)
	assertCovered(t, compound.Events, 5, 7)
}

func TestCompoundPayloadAndSpan(t *testing.T) {
	if _, err := CompoundPayload(json.RawMessage(`{"session":{}}`), nil, raw(unreadJSON("s1", 1, 1))); err == nil {
		t.Fatal("a message payload with a session field must be rejected")
	}
	// Clients apply every embedded part behind its own version barrier.
	for _, part := range []string{`{"session_id":"s1"}`, `{"state_version":"1"}`} {
		if _, err := CompoundPayload(json.RawMessage(messageJSON("m1", "s1", 1)), raw(part), nil); err == nil {
			t.Fatalf("embedded part %s must be rejected", part)
		}
	}
	if span, err := Span(json.RawMessage(`{"msg_id":"1","content":"session"}`)); err != nil || span != 1 {
		t.Fatalf("classic payload mentioning session: span=%d err=%v", span, err)
	}
	payload, err := CompoundPayload(json.RawMessage(messageJSON("m1", "s1", 1)), raw(sessionJSON("s1", 1)), nil)
	if err != nil {
		t.Fatal(err)
	}
	if span, err := Span(payload); err != nil || span != 2 {
		t.Fatalf("session-only compound: span=%d err=%v", span, err)
	}
	if _, err := Span(json.RawMessage(`{"msg_id":"1","unread":{"session_id":"s1"}}`)); err == nil {
		t.Fatal("an unread part without state_version must be rejected")
	}
}
