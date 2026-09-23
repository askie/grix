package syncstream

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/syncstream/fold"
)

// CompoundEnabled reports whether a message delivery is written as one
// compound message.upsert row instead of the classic message, session and
// unread rows.
func CompoundEnabled() bool {
	return strings.TrimSpace(os.Getenv("AIBOT_SYNC_COMPOUND_ENABLED")) == "1"
}

// ReplayFoldEnabled reports whether sync pages are folded for connections
// that declared compound_v1.
func ReplayFoldEnabled() bool {
	return strings.TrimSpace(os.Getenv("AIBOT_SYNC_REPLAY_FOLD_ENABLED")) == "1"
}

// MessageDelivery is one message change as one user receives it, with the
// session projection and unread state that the same write moved.
type MessageDelivery struct {
	UserID    int64
	SessionID string
	Message   model.Message
	// CommandID is the receipt carried by the message event.
	CommandID string
	// Session is the session projection the write moved, if any.
	Session *model.Session
	// SessionCommandID is a receipt carried by the session event itself. A
	// delivery with one stays classic rows so the receipt keeps its row.
	SessionCommandID string
	// Member is the user's membership after the write; it carries unread.
	Member *model.SessionMember
}

// MessageDeliveryEvents turns deliveries into sync events, in order. With
// AIBOT_SYNC_COMPOUND_ENABLED=1 an eligible delivery becomes one compound
// message.upsert that reserves one cursor per part, so a client without
// compound_v1 still receives the classic rows at the very same cursors.
func MessageDeliveryEvents(deliveries ...MessageDelivery) []Event {
	compound := CompoundEnabled()
	events := make([]Event, 0, len(deliveries)*3)
	for _, delivery := range deliveries {
		if compound && delivery.compoundable() {
			events = append(events, delivery.compoundEvent())
			continue
		}
		events = append(events, delivery.classicEvents()...)
	}
	return events
}

// SessionUpsertEvent is the session.upsert event of a session projection.
func SessionUpsertEvent(userID int64, sessionID string, session model.Session, commandID string) Event {
	return Event{UserID: userID, Kind: "session.upsert", EntityType: "session", EntityID: sessionID,
		EntityVersion: session.StateVersion, CommandID: commandID, Payload: session}
}

// UnreadSetEvent is the session.unread_set event of one member's unread state.
func UnreadSetEvent(userID int64, sessionID string, member model.SessionMember) Event {
	return Event{UserID: userID, Kind: "session.unread_set", EntityType: "session_member", EntityID: sessionID,
		EntityVersion: member.StateVersion, Payload: unreadPayload(sessionID, member)}
}

func unreadPayload(sessionID string, member model.SessionMember) map[string]any {
	return map[string]any{"session_id": sessionID, "unread_count": member.UnreadCount,
		"last_read_msg_id": member.LastReadMsgID, "state_version": member.StateVersion}
}

func (d MessageDelivery) messageEvent(payload any) Event {
	return Event{UserID: d.UserID, Kind: "message.upsert", EntityType: "message",
		EntityID: fmt.Sprintf("%d", d.Message.MsgID), EntityVersion: d.Message.StateVersion,
		CommandID: d.CommandID, Payload: payload}
}

func (d MessageDelivery) classicEvents() []Event {
	events := []Event{d.messageEvent(d.Message)}
	if d.Session != nil {
		events = append(events, SessionUpsertEvent(d.UserID, d.SessionID, *d.Session, d.SessionCommandID))
	}
	if d.Member != nil {
		events = append(events, UnreadSetEvent(d.UserID, d.SessionID, *d.Member))
	}
	return events
}

// compoundable keeps the expansion exact: embedded parts carry no receipt of
// their own, and the embedded session names the delivery's session.
func (d MessageDelivery) compoundable() bool {
	if d.Session == nil && d.Member == nil {
		return false
	}
	return d.SessionCommandID == "" && (d.Session == nil || d.Session.SessionID == d.SessionID)
}

func (d MessageDelivery) compoundEvent() Event {
	payload := compoundMessagePayload{message: d.Message, session: d.Session}
	span := 1
	if d.Session != nil {
		span++
	}
	if d.Member != nil {
		payload.unread = unreadPayload(d.SessionID, *d.Member)
		span++
	}
	event := d.messageEvent(payload)
	event.Span = span
	return event
}

// compoundMessagePayload marshals as the message object with the session and
// unread payloads of the classic rows embedded.
type compoundMessagePayload struct {
	message model.Message
	session *model.Session
	unread  map[string]any
}

func (p compoundMessagePayload) MarshalJSON() ([]byte, error) {
	message, err := json.Marshal(p.message)
	if err != nil {
		return nil, err
	}
	var session, unread json.RawMessage
	if p.session != nil {
		if session, err = json.Marshal(p.session); err != nil {
			return nil, err
		}
	}
	if p.unread != nil {
		if unread, err = json.Marshal(p.unread); err != nil {
			return nil, err
		}
	}
	return fold.CompoundPayload(message, session, unread)
}
