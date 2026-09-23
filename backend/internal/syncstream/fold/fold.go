// Package fold shapes one page of durable sync rows into the events one
// connection receives.
//
// A connection without compound_v1 gets every compound row expanded back
// into its classic rows at their reserved cursors, which is exactly the
// stream it received before compound rows existed; it is never folded.
// A connection with compound_v1 gets one event per row and, when replay
// folding is enabled, loses the events superseded later in the same page.
// Its events carry first_cursor whenever they cover more than their own
// cursor, so the client can still verify that no cursor went missing.
//
// The package is pure so the runtime and the backtest share one algorithm.
package fold

import (
	"encoding/json"
	"fmt"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

const (
	kindMessageUpsert = "message.upsert"
	kindMessageRevoke = "message.revoke"
	kindSessionUpsert = "session.upsert"
	kindUnreadSet     = "session.unread_set"

	entityMessage       = "message"
	entitySession       = "session"
	entitySessionMember = "session_member"
)

// Options selects how a page is shaped for one connection.
type Options struct {
	// Compound is set when the connection declared compound_v1.
	Compound bool
	// Fold drops events superseded later in the same page. It applies only
	// to compound connections.
	Fold bool
	// MaxEvents caps the expanded events of a classic page. Only whole rows
	// are taken and at least one. Zero means no cap.
	MaxEvents int
}

// Result is one shaped page.
type Result struct {
	Events []protocol.SyncEventPayload
	// Rows is how many leading rows the page consumed.
	Rows int
	// NextCursor is the stream cursor of the last consumed row, or the
	// starting cursor when no row was consumed. Folding never changes it.
	NextCursor int64
	// Receipts counts superseded events that were kept only because their
	// command_id is a receipt that no surviving event carries.
	Receipts int
}

type role uint8

const (
	roleRow role = iota // a classic row: the part is the row itself
	roleMessage
	roleSession
	roleUnread
)

type part struct {
	role      role
	cursor    int64
	kind      string
	entity    string
	entityID  string
	version   int64
	tombstone bool
	commandID string
	kept      bool
}

func (p part) event(payload json.RawMessage) protocol.SyncEventPayload {
	return protocol.SyncEventPayload{Cursor: p.cursor, Kind: p.kind, EntityType: p.entity,
		EntityID: p.entityID, EntityVersion: p.version, Tombstone: p.tombstone,
		CommandID: p.commandID, Payload: payload}
}

type unit struct {
	row   model.UserSyncEvent
	split *compound
	parts []part // cursor order
}

func (u unit) payload(p part) (json.RawMessage, error) {
	switch p.role {
	case roleMessage:
		return json.Marshal(u.split.fields)
	case roleSession:
		return u.split.session, nil
	case roleUnread:
		return u.split.unread, nil
	default:
		return json.RawMessage(u.row.Payload), nil
	}
}

// Page shapes rows, which must be one user's rows after cursor from, in
// stream_cursor order.
func Page(rows []model.UserSyncEvent, from int64, opts Options) (Result, error) {
	units := make([]unit, 0, len(rows))
	for _, row := range rows {
		u, err := resolve(row, from)
		if err != nil {
			return Result{NextCursor: from}, err
		}
		units = append(units, u)
	}
	if !opts.Compound {
		return expand(units, from, opts.MaxEvents)
	}
	result := Result{Rows: len(units), NextCursor: from}
	if len(units) > 0 {
		result.NextCursor = units[len(units)-1].row.StreamCursor
	}
	if opts.Fold {
		result.Receipts = fold(units)
	}
	events, err := emit(units, from)
	if err != nil {
		return Result{NextCursor: from}, err
	}
	result.Events = events
	return result, nil
}

func resolve(row model.UserSyncEvent, from int64) (unit, error) {
	u := unit{row: row}
	whole := part{role: roleRow, cursor: row.StreamCursor, kind: row.EventKind, entity: row.EntityType,
		entityID: row.EntityID, version: row.EntityVersion, tombstone: row.Tombstone, commandID: row.CommandID}
	split, ok := compound{}, false
	if row.EventKind == kindMessageUpsert && row.EntityType == entityMessage {
		split, ok = splitCompound(json.RawMessage(row.Payload))
	}
	if !ok {
		u.parts = []part{whole}
	} else {
		u.split = &split
		message := whole
		message.role = roleMessage
		message.cursor = row.StreamCursor - int64(split.partCount()) + 1
		u.parts = append(u.parts, message)
		cursor := message.cursor
		for _, embedded := range []struct {
			role   role
			kind   string
			entity string
			raw    json.RawMessage
		}{
			{roleSession, kindSessionUpsert, entitySession, split.session},
			{roleUnread, kindUnreadSet, entitySessionMember, split.unread},
		} {
			if embedded.raw == nil {
				continue
			}
			entityID, version, err := embeddedEntity(embedded.raw)
			if err != nil {
				return unit{}, fmt.Errorf("fold: row cursor=%d: %w", row.StreamCursor, err)
			}
			cursor++
			u.parts = append(u.parts, part{role: embedded.role, cursor: cursor, kind: embedded.kind,
				entity: embedded.entity, entityID: entityID, version: version})
		}
	}
	// A cursor at or before from was already delivered.
	for i := range u.parts {
		u.parts[i].kept = u.parts[i].cursor > from
	}
	return u, nil
}

// expand shapes a page for a connection without compound_v1: every part is
// its own classic event at its own cursor.
func expand(units []unit, from int64, maxEvents int) (Result, error) {
	// Clients reject a batch whose events are not a list, even an empty one.
	result := Result{Events: make([]protocol.SyncEventPayload, 0, len(units)), NextCursor: from}
	for _, u := range units {
		if maxEvents > 0 && result.Rows > 0 && len(result.Events)+len(u.parts) > maxEvents {
			break
		}
		for _, p := range u.parts {
			if !p.kept {
				continue
			}
			payload, err := u.payload(p)
			if err != nil {
				return Result{NextCursor: from}, err
			}
			result.Events = append(result.Events, p.event(payload))
		}
		result.Rows++
		result.NextCursor = u.row.StreamCursor
	}
	return result, nil
}

// fold clears kept on the parts superseded later in the page:
//   - message.upsert by any later upsert or revoke of the same message;
//   - message.revoke by a later revoke of the same message;
//   - a session or session_member event by a later event of the same kind
//     and entity, embedded parts included.
//
// Tombstones (session.remove) are never folded: their reason decides whether
// the client also deletes the session's messages. A superseded part whose
// non-empty command_id differs from its survivor's stays, since it is the
// only receipt of that client command; fold returns how many stayed so.
func fold(units []unit) int {
	lastMessage := map[string]string{} // message id -> command_id of its last message part
	lastRevoke := map[string]string{}  // message id -> command_id of its last revoke
	lastState := map[string]string{}   // kind/entity/id -> command_id of its last event
	receipts := 0
	supersede := func(p *part, survivorCommandID string) {
		if p.commandID != "" && p.commandID != survivorCommandID {
			receipts++
			return
		}
		p.kept = false
	}
	for i := len(units) - 1; i >= 0; i-- {
		for j := len(units[i].parts) - 1; j >= 0; j-- {
			p := &units[i].parts[j]
			if !p.kept {
				continue
			}
			switch {
			case p.entity == entityMessage && p.kind == kindMessageUpsert:
				if survivor, seen := lastMessage[p.entityID]; seen {
					supersede(p, survivor)
				} else {
					lastMessage[p.entityID] = p.commandID
				}
			case p.entity == entityMessage && p.kind == kindMessageRevoke:
				if survivor, seen := lastRevoke[p.entityID]; seen {
					supersede(p, survivor)
				} else {
					lastRevoke[p.entityID] = p.commandID
				}
				if _, seen := lastMessage[p.entityID]; !seen {
					lastMessage[p.entityID] = p.commandID
				}
			case (p.entity == entitySession || p.entity == entitySessionMember) && !p.tombstone:
				key := p.kind + "\x00" + p.entity + "\x00" + p.entityID
				if survivor, seen := lastState[key]; seen {
					supersede(p, survivor)
				} else {
					lastState[key] = p.commandID
				}
			}
		}
	}
	return receipts
}

// emit shapes a page for a compound_v1 connection: one event per row, with
// the row's surviving parts. first_cursor is the previous event's cursor + 1
// whenever that is below the event's own cursor.
func emit(units []unit, from int64) ([]protocol.SyncEventPayload, error) {
	events := make([]protocol.SyncEventPayload, 0, len(units))
	previous := from
	add := func(event protocol.SyncEventPayload) {
		if first := previous + 1; first < event.Cursor {
			event.FirstCursor = first
		}
		events = append(events, event)
		previous = event.Cursor
	}
	for _, u := range units {
		if u.split == nil {
			if u.parts[0].kept {
				add(u.parts[0].event(json.RawMessage(u.row.Payload)))
			}
			continue
		}
		message := u.parts[0]
		if !message.kept {
			// A later event replaces the message, but the session or unread
			// state it carried is still the latest one: send those parts as
			// classic events at their reserved cursors.
			for _, p := range u.parts[1:] {
				if !p.kept {
					continue
				}
				payload, err := u.payload(p)
				if err != nil {
					return nil, err
				}
				add(p.event(payload))
			}
			continue
		}
		event := message.event(json.RawMessage(u.row.Payload))
		var session, unread json.RawMessage
		whole := true
		for _, p := range u.parts[1:] {
			if !p.kept {
				whole = false
				continue
			}
			if p.role == roleSession {
				session = u.split.session
			} else {
				unread = u.split.unread
			}
			event.Cursor = p.cursor
		}
		if !whole {
			payload, err := joinCompound(u.split.fields, session, unread)
			if err != nil {
				return nil, err
			}
			event.Payload = payload
		}
		add(event)
	}
	return events, nil
}
