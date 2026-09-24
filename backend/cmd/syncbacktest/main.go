// Command syncbacktest replays user_sync_events through the runtime page
// shaping in internal/syncstream/fold and reports what each delivery variant
// would send, per user in pages of syncstream.MaxBatchSize rows:
//
//	baseline  stored rows to a client without compound_v1 (today's stream)
//	A         compound rows to a compound_v1 client, not folded
//	B         stored rows to a compound_v1 client, folded
//	C         compound rows to a compound_v1 client, folded
//
// Compound rows are rebuilt from the stored classic rows the way the writer
// groups them: a message.upsert takes the session.upsert and then the
// session.unread_set that directly follow it in the same write (same user,
// session and created_at) and carry no receipt of their own. The payload is
// built by the writer's own fold.CompoundPayload. The tool also checks that
// expanding the compound rows restores every stored row exactly.
//
// It only reads. Point SYNC_BACKTEST_DSN at a replica.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/syncstream"
	"github.com/askie/grix/backend/internal/syncstream/fold"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// passSavings is the acceptance bar for variant C against the baseline.
const passSavings = 0.50

type variant struct {
	name    string
	label   string
	options fold.Options
	// compound selects the rebuilt compound rows instead of the stored rows.
	compound bool
}

var variants = []variant{
	{"baseline", "today, classic", fold.Options{MaxEvents: syncstream.MaxBatchSize}, false},
	{"A", "compound", fold.Options{Compound: true}, true},
	{"B", "fold", fold.Options{Compound: true, Fold: true}, false},
	{"C", "compound + fold", fold.Options{Compound: true, Fold: true}, true},
}

type tally struct {
	events       int64
	payloadBytes int64
	wireBytes    int64
	receipts     int64
	// chainBreaks counts events a client would reject: a classic event not
	// right after the previous cursor, or a compound_v1 event whose
	// first_cursor does not follow the previous event.
	chainBreaks int64
	// foldedPages and stateDiffs compare every folded page with the same page
	// unfolded: the final state of each entity and the set of receipts must
	// be identical.
	foldedPages int64
	stateDiffs  int64
	// reordered counts session.upsert parts the fold kept again so that they
	// still precede the unread state of their session; unreadAhead counts
	// unread events a client would still apply before their session.upsert
	// although the unfolded page had one before them.
	reordered   int64
	unreadAhead int64
}

func (t *tally) add(o tally) {
	t.events += o.events
	t.payloadBytes += o.payloadBytes
	t.wireBytes += o.wireBytes
	t.receipts += o.receipts
	t.chainBreaks += o.chainBreaks
	t.foldedPages += o.foldedPages
	t.stateDiffs += o.stateDiffs
	t.reordered += o.reordered
	t.unreadAhead += o.unreadAhead
}

type userReport struct {
	userID int64
	rows   int
	byName map[string]tally
}

type report struct {
	users         []userReport
	storedRows    int64
	compoundRows  int64
	partsByCount  map[int]int64
	totals        map[string]tally
	expandedSame  int64
	expandedDiffs int64
	firstDiffs    []string
}

func main() {
	dsn := strings.TrimSpace(os.Getenv("SYNC_BACKTEST_DSN"))
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "SYNC_BACKTEST_DSN is required (a read replica DSN)")
		os.Exit(2)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		fmt.Fprintf(os.Stderr, "open database: %v\n", err)
		os.Exit(2)
	}
	// One connection, so the read-only session setting covers every query.
	sqlDB, err := db.DB()
	if err != nil {
		fmt.Fprintf(os.Stderr, "open database: %v\n", err)
		os.Exit(2)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.Exec("SET SESSION CHARACTERISTICS AS TRANSACTION READ ONLY").Error; err != nil {
		fmt.Fprintf(os.Stderr, "set read only: %v\n", err)
		os.Exit(2)
	}
	started := time.Now()
	rep, err := run(db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backtest: %v\n", err)
		os.Exit(1)
	}
	if !rep.print(time.Since(started)) {
		os.Exit(1)
	}
}

func run(db *gorm.DB) (*report, error) {
	var userIDs []int64
	if err := db.Model(&model.UserSyncEvent{}).Distinct("user_id").Order("user_id").Pluck("user_id", &userIDs).Error; err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	rep := &report{partsByCount: map[int]int64{}, totals: map[string]tally{}}
	for _, userID := range userIDs {
		var rows []model.UserSyncEvent
		if err := db.Where("user_id = ?", userID).Order("stream_cursor ASC").Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("load user %d: %w", userID, err)
		}
		if len(rows) == 0 {
			continue
		}
		compoundRows, err := rebuildCompoundRows(rows)
		if err != nil {
			return nil, fmt.Errorf("rebuild user %d: %w", userID, err)
		}
		rep.storedRows += int64(len(rows))
		rep.compoundRows += int64(len(compoundRows))
		for _, row := range compoundRows {
			if row.EventKind == "message.upsert" && row.EntityType == "message" {
				span, err := fold.Span(json.RawMessage(row.Payload))
				if err != nil {
					return nil, fmt.Errorf("user %d cursor %d: %w", userID, row.StreamCursor, err)
				}
				rep.partsByCount[span]++
			} else {
				rep.partsByCount[0]++
			}
		}
		// Replay from just before the user's first stored cursor.
		from := rows[0].StreamCursor - 1
		user := userReport{userID: userID, rows: len(rows), byName: map[string]tally{}}
		var baselineEvents []protocol.SyncEventPayload
		for _, v := range variants {
			source := rows
			if v.compound {
				source = compoundRows
			}
			events, t, err := replay(source, from, v.options)
			if err != nil {
				return nil, fmt.Errorf("user %d variant %s: %w", userID, v.name, err)
			}
			if v.name == "baseline" {
				baselineEvents = events
			}
			user.byName[v.name] = t
			total := rep.totals[v.name]
			total.add(t)
			rep.totals[v.name] = total
		}
		// A client without compound_v1 must see exactly today's stream.
		expanded, _, err := replay(compoundRows, from, fold.Options{MaxEvents: syncstream.MaxBatchSize})
		if err != nil {
			return nil, fmt.Errorf("user %d classic expansion: %w", userID, err)
		}
		rep.compareExpansion(userID, baselineEvents, expanded)
		rep.users = append(rep.users, user)
	}
	return rep, nil
}

// replay drains rows the way the runtime does: one page of at most
// MaxBatchSize rows at a time, resuming from the page's next cursor.
func replay(rows []model.UserSyncEvent, from int64, opts fold.Options) ([]protocol.SyncEventPayload, tally, error) {
	var all []protocol.SyncEventPayload
	var t tally
	for start := 0; start < len(rows); {
		end := min(start+syncstream.MaxBatchSize, len(rows))
		page, err := fold.Page(rows[start:end], from, opts)
		if err != nil {
			return nil, t, err
		}
		if page.Rows == 0 {
			return nil, t, errors.New("page consumed no rows")
		}
		last := from
		for _, event := range page.Events {
			first := event.Cursor
			if opts.Compound && event.FirstCursor != 0 {
				first = event.FirstCursor
			}
			if first != last+1 || event.Cursor < first {
				t.chainBreaks++
			}
			last = event.Cursor
			wire, err := json.Marshal(event)
			if err != nil {
				return nil, t, err
			}
			t.events++
			t.payloadBytes += int64(compactLen(event.Payload))
			t.wireBytes += int64(len(wire))
		}
		if last != page.NextCursor {
			t.chainBreaks++
		}
		t.receipts += int64(page.Receipts)
		if opts.Fold {
			unfolded, err := fold.Page(rows[start:start+page.Rows], from, fold.Options{})
			if err != nil {
				return nil, t, err
			}
			same, err := sameFinalState(unfolded.Events, page.Events)
			if err != nil {
				return nil, t, err
			}
			t.foldedPages++
			if !same {
				t.stateDiffs++
			}
			ahead, err := unreadAheadOfSession(unfolded.Events, page.Events)
			if err != nil {
				return nil, t, err
			}
			t.unreadAhead += int64(ahead)
			t.reordered += int64(page.Reordered)
		}
		all = append(all, page.Events...)
		start += page.Rows
		from = page.NextCursor
	}
	return all, t, nil
}

// entityState is what a client keeps for one entity after a page.
type entityState struct {
	version   int64
	tombstone bool
	payload   string
}

// sameFinalState applies both event lists the way a client does, splitting
// compound events back into their parts, and compares the last state of every
// entity plus the receipts they carry. Message upserts and revokes share the
// message entity; other events are kept per kind and entity.
func sameFinalState(unfolded, folded []protocol.SyncEventPayload) (bool, error) {
	a, aReceipts, err := finalState(unfolded)
	if err != nil {
		return false, err
	}
	b, bReceipts, err := finalState(folded)
	if err != nil {
		return false, err
	}
	return reflect.DeepEqual(a, b) && reflect.DeepEqual(aReceipts, bReceipts), nil
}

func finalState(events []protocol.SyncEventPayload) (map[string]entityState, map[string]bool, error) {
	state := map[string]entityState{}
	receipts := map[string]bool{}
	for _, event := range events {
		parts, err := clientParts(event)
		if err != nil {
			return nil, nil, err
		}
		for _, p := range parts {
			key := p.Kind + "|" + p.EntityType + "|" + p.EntityID
			if p.EntityType == "message" {
				key = "message|" + p.EntityID
			}
			var value any
			if err := json.Unmarshal(p.Payload, &value); err != nil {
				return nil, nil, err
			}
			canonical, err := json.Marshal(value)
			if err != nil {
				return nil, nil, err
			}
			state[key] = entityState{version: p.EntityVersion, tombstone: p.Tombstone, payload: string(canonical)}
			if p.CommandID != "" {
				receipts[p.CommandID] = true
			}
		}
	}
	return state, receipts, nil
}

// unreadAheadOfSession counts unread events (session.unread_set, or the
// client's own session.read_state) that the folded page applies before any
// session.upsert of their session, although the unfolded page applied one
// before them. A client drops such an unread for a session it does not have.
func unreadAheadOfSession(unfolded, folded []protocol.SyncEventPayload) (int, error) {
	type unreadKey struct {
		kind, sessionID string
		version         int64
	}
	isUnread := func(p protocol.SyncEventPayload) bool {
		return p.EntityType == "session_member" && !strings.Contains(p.EntityID, ":") &&
			(p.Kind == "session.unread_set" || p.Kind == "session.read_state")
	}
	walk := func(events []protocol.SyncEventPayload, visit func(protocol.SyncEventPayload, bool)) error {
		covered := map[string]bool{}
		for _, event := range events {
			parts, err := clientParts(event)
			if err != nil {
				return err
			}
			for _, p := range parts {
				switch {
				case p.Kind == "session.upsert" && p.EntityType == "session":
					covered[p.EntityID] = true
				case p.Kind == "session.remove" && p.EntityType == "session":
					covered[p.EntityID] = false
				case isUnread(p):
					visit(p, covered[p.EntityID])
				}
			}
		}
		return nil
	}
	coveredBefore := map[unreadKey]bool{}
	if err := walk(unfolded, func(p protocol.SyncEventPayload, covered bool) {
		coveredBefore[unreadKey{p.Kind, p.EntityID, p.EntityVersion}] = covered
	}); err != nil {
		return 0, err
	}
	ahead := 0
	err := walk(folded, func(p protocol.SyncEventPayload, covered bool) {
		if !covered && coveredBefore[unreadKey{p.Kind, p.EntityID, p.EntityVersion}] {
			ahead++
		}
	})
	return ahead, err
}

// clientParts splits a compound message event into the classic parts a
// client applies. It is written independently of the fold package on purpose.
func clientParts(event protocol.SyncEventPayload) ([]protocol.SyncEventPayload, error) {
	if event.Kind != "message.upsert" || event.EntityType != "message" {
		return []protocol.SyncEventPayload{event}, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(event.Payload, &fields); err != nil {
		return nil, err
	}
	session, unread := fields["session"], fields["unread"]
	if session == nil && unread == nil {
		return []protocol.SyncEventPayload{event}, nil
	}
	delete(fields, "session")
	delete(fields, "unread")
	message := event
	message.FirstCursor = 0
	payload, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	message.Payload = payload
	parts := []protocol.SyncEventPayload{message}
	for _, embedded := range []struct {
		kind, entity string
		raw          json.RawMessage
	}{{"session.upsert", "session", session}, {"session.unread_set", "session_member", unread}} {
		if embedded.raw == nil {
			continue
		}
		var ref struct {
			SessionID    string          `json:"session_id"`
			StateVersion json.RawMessage `json:"state_version"`
		}
		if err := json.Unmarshal(embedded.raw, &ref); err != nil {
			return nil, err
		}
		text := strings.Trim(strings.TrimSpace(string(ref.StateVersion)), `"`)
		version, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("embedded %s state_version %q: %w", embedded.kind, text, err)
		}
		parts = append(parts, protocol.SyncEventPayload{Kind: embedded.kind, EntityType: embedded.entity,
			EntityID: ref.SessionID, EntityVersion: version, Payload: embedded.raw})
	}
	return parts, nil
}

func compactLen(raw json.RawMessage) int {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return len(raw)
	}
	return buf.Len()
}

type pendingCompound struct {
	row       model.UserSyncEvent
	sessionID string
	session   json.RawMessage
	unread    json.RawMessage
	last      int64
}

// rebuildCompoundRows regroups stored classic rows into the rows the compound
// writer produces for the same writes.
func rebuildCompoundRows(rows []model.UserSyncEvent) ([]model.UserSyncEvent, error) {
	out := make([]model.UserSyncEvent, 0, len(rows))
	var pending *pendingCompound
	flush := func() error {
		if pending == nil {
			return nil
		}
		row := pending.row
		if pending.session != nil || pending.unread != nil {
			payload, err := fold.CompoundPayload(json.RawMessage(row.Payload), pending.session, pending.unread)
			if err != nil {
				return fmt.Errorf("cursor %d: %w", row.StreamCursor, err)
			}
			row.Payload = datatypes.JSON(payload)
			row.StreamCursor = pending.last
		}
		out = append(out, row)
		pending = nil
		return nil
	}
	for _, row := range rows {
		if pending != nil && pending.accepts(row) {
			if row.EventKind == "session.upsert" {
				pending.session = json.RawMessage(row.Payload)
			} else {
				pending.unread = json.RawMessage(row.Payload)
			}
			pending.last = row.StreamCursor
			continue
		}
		if err := flush(); err != nil {
			return nil, err
		}
		if row.EventKind == "message.upsert" && row.EntityType == "message" && !row.Tombstone {
			pending = &pendingCompound{row: row, sessionID: payloadSessionID(row.Payload), last: row.StreamCursor}
			continue
		}
		out = append(out, row)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return out, nil
}

// accepts mirrors syncstream.MessageDelivery: the session part comes before
// the unread part, neither carries its own receipt, and both name the
// message's session.
func (p *pendingCompound) accepts(row model.UserSyncEvent) bool {
	if p.sessionID == "" || row.StreamCursor != p.last+1 || !row.CreatedAt.Equal(p.row.CreatedAt) ||
		row.CommandID != "" || row.Tombstone || row.EntityID != p.sessionID || payloadSessionID(row.Payload) != p.sessionID {
		return false
	}
	switch {
	case row.EventKind == "session.upsert" && row.EntityType == "session":
		return p.session == nil && p.unread == nil
	case row.EventKind == "session.unread_set" && row.EntityType == "session_member":
		return p.unread == nil
	}
	return false
}

func payloadSessionID(payload datatypes.JSON) string {
	var ref struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(payload, &ref); err != nil {
		return ""
	}
	return ref.SessionID
}

func (r *report) compareExpansion(userID int64, want, got []protocol.SyncEventPayload) {
	for i := 0; i < max(len(want), len(got)); i++ {
		if i < len(want) && i < len(got) && sameEvent(want[i], got[i]) {
			r.expandedSame++
			continue
		}
		r.expandedDiffs++
		if len(r.firstDiffs) < 5 {
			r.firstDiffs = append(r.firstDiffs, fmt.Sprintf("user %d event %d", userID, i))
		}
	}
}

func sameEvent(a, b protocol.SyncEventPayload) bool {
	pa, pb := a.Payload, b.Payload
	a.Payload, b.Payload = nil, nil
	if !reflect.DeepEqual(a, b) {
		return false
	}
	var va, vb any
	if json.Unmarshal(pa, &va) != nil || json.Unmarshal(pb, &vb) != nil {
		return false
	}
	return reflect.DeepEqual(va, vb)
}

func saved(base, value int64) string {
	if base == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(base-value)/float64(base))
}

func mb(value int64) string { return fmt.Sprintf("%.2f", float64(value)/1e6) }

// print writes the report and returns whether the algorithm passes.
func (r *report) print(elapsed time.Duration) bool {
	base := r.totals["baseline"]
	fmt.Printf("syncbacktest: %d users, %d stored rows, page=%d rows, %s\n",
		len(r.users), r.storedRows, syncstream.MaxBatchSize, elapsed.Round(time.Millisecond))
	fmt.Printf("compound rows rebuilt: %d (message+session+unread %d, message+session %d, message only %d, other rows %d)\n\n",
		r.compoundRows, r.partsByCount[3], r.partsByCount[2], r.partsByCount[1], r.partsByCount[0])
	fmt.Printf("%-28s %9s %8s %11s %8s %9s %8s %9s\n", "variant", "events", "saved", "payload MB", "saved", "wire MB", "saved", "receipts")
	for _, v := range variants {
		t := r.totals[v.name]
		receipts := "-"
		if v.options.Fold {
			receipts = fmt.Sprint(t.receipts)
		}
		fmt.Printf("%-28s %9d %8s %11s %8s %9s %8s %9s\n", v.name+"  "+v.label, t.events, saved(base.events, t.events),
			mb(t.payloadBytes), saved(base.payloadBytes, t.payloadBytes), mb(t.wireBytes), saved(base.wireBytes, t.wireBytes), receipts)
	}
	fmt.Printf("\nreceipt events kept by rule 4 (superseded, command_id carried by no survivor): B=%d C=%d\n",
		r.totals["B"].receipts, r.totals["C"].receipts)
	fmt.Printf("session.upsert parts kept again to precede their session's unread: B=%d C=%d\n",
		r.totals["B"].reordered, r.totals["C"].reordered)
	fmt.Printf("classic expansion of compound rows vs stored rows: %d identical, %d different", r.expandedSame, r.expandedDiffs)
	if len(r.firstDiffs) > 0 {
		fmt.Printf(" (first: %s)", strings.Join(r.firstDiffs, "; "))
	}
	fmt.Println()
	breaks, stateDiffs := 0, 0
	fmt.Print("cursor chain breaks a client would reject:")
	for _, v := range variants {
		fmt.Printf(" %s=%d", v.name, r.totals[v.name].chainBreaks)
		breaks += int(r.totals[v.name].chainBreaks)
	}
	fmt.Println()
	fmt.Print("folded pages whose final entity state or receipts differ from unfolded:")
	unreadAhead := 0
	for _, v := range variants {
		if v.options.Fold {
			t := r.totals[v.name]
			fmt.Printf(" %s=%d/%d", v.name, t.stateDiffs, t.foldedPages)
			stateDiffs += int(t.stateDiffs)
			unreadAhead += int(t.unreadAhead)
		}
	}
	fmt.Println()
	fmt.Printf("unread applied before its session.upsert only because of folding: B=%d C=%d\n",
		r.totals["B"].unreadAhead, r.totals["C"].unreadAhead)

	users := append([]userReport(nil), r.users...)
	sort.Slice(users, func(i, j int) bool {
		return users[i].byName["baseline"].events > users[j].byName["baseline"].events
	})
	fmt.Printf("\ntop 5 users by baseline events:\n%-20s %9s %9s %9s %9s %8s %11s %11s\n",
		"user_id", "baseline", "A", "B", "C", "C saved", "base MB", "C MB")
	for _, u := range users[:min(5, len(users))] {
		b, c := u.byName["baseline"], u.byName["C"]
		fmt.Printf("%-20d %9d %9d %9d %9d %8s %11s %11s\n", u.userID, b.events, u.byName["A"].events,
			u.byName["B"].events, c.events, saved(b.events, c.events), mb(b.payloadBytes), mb(c.payloadBytes))
	}

	c := r.totals["C"]
	savings := 0.0
	if base.events > 0 {
		savings = float64(base.events-c.events) / float64(base.events)
	}
	pass := savings >= passSavings && r.expandedDiffs == 0 && breaks == 0 && stateDiffs == 0 && unreadAhead == 0
	verdict := "PASS"
	if !pass {
		verdict = "FAIL"
	}
	fmt.Printf("\nverdict: %s (C saves %.1f%% of events, bar %.0f%%; classic expansion diffs %d; chain breaks %d; folded state diffs %d; unread ahead %d)\n",
		verdict, 100*savings, 100*passSavings, r.expandedDiffs, breaks, stateDiffs, unreadAhead)
	return pass
}
