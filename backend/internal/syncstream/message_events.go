package syncstream

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/syncstream/fold"
	"gorm.io/gorm"
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
	visitorMeta := loadDeliveryVisitorMeta(deliveries)
	events := make([]Event, 0, len(deliveries)*3)
	for _, delivery := range deliveries {
		meta := visitorMeta[delivery.SessionID]
		if compound && delivery.compoundable() {
			events = append(events, delivery.compoundEvent(meta))
			continue
		}
		events = append(events, delivery.classicEvents(meta)...)
	}
	return events
}

// loadDeliveryVisitorMeta loads the widget attribution of every session that a
// delivery projects, in one query.
func loadDeliveryVisitorMeta(deliveries []MessageDelivery) map[string]VisitorSessionMeta {
	if !Enabled() {
		return nil
	}
	sessionIDs := make([]string, 0, len(deliveries))
	for _, delivery := range deliveries {
		if delivery.Session != nil {
			sessionIDs = append(sessionIDs, delivery.SessionID)
		}
	}
	return LoadVisitorSessionMeta(sessionIDs...)
}

// SessionUpsertEvent is the session.upsert event of a session projection.
// It carries the widget attribution of visitor sessions so a v2 client that
// creates the session from this event still recognizes the visitor.
func SessionUpsertEvent(userID int64, sessionID string, session model.Session, commandID string) Event {
	meta := VisitorSessionMeta{}
	if Enabled() {
		meta = LoadVisitorSessionMeta(sessionID)[sessionID]
	}
	return SessionUpsertEventWithVisitorMeta(userID, sessionID, session, commandID, meta)
}

// SessionUpsertEventWithVisitorMeta builds a session.upsert event with an
// already loaded visitor attribution, so batch producers query once.
func SessionUpsertEventWithVisitorMeta(userID int64, sessionID string, session model.Session, commandID string, meta VisitorSessionMeta) Event {
	return Event{UserID: userID, Kind: "session.upsert", EntityType: "session", EntityID: sessionID,
		EntityVersion: session.StateVersion, CommandID: commandID, Payload: SessionProjection(session, meta)}
}

// VisitorSessionMeta is the widget attribution of one visitor session.
type VisitorSessionMeta struct {
	IsVisitor bool
	SiteID    int64
	Domain    string
}

// sessionUpsertPayload adds the visitor attribution to a session projection.
// Ordinary sessions keep the exact payload they had before: the added fields
// are omitempty and never set for non-visitors.
type sessionUpsertPayload struct {
	model.Session
	IsVisitor         bool   `json:"is_visitor,omitempty"`
	VisitorSiteID     string `json:"visitor_site_id,omitempty"`
	VisitorSiteDomain string `json:"visitor_site_domain,omitempty"`
}

// SessionProjection returns the session payload of a v2 session projection:
// the session row plus the visitor attribution when it belongs to a widget
// visitor. Ordinary sessions keep their previous payload exactly.
func SessionProjection(session model.Session, meta VisitorSessionMeta) any {
	if !meta.IsVisitor {
		return session
	}
	return sessionUpsertPayload{
		Session:           session,
		IsVisitor:         true,
		VisitorSiteID:     strconv.FormatInt(meta.SiteID, 10),
		VisitorSiteDomain: meta.Domain,
	}
}

// LoadVisitorSessionMeta returns the widget attribution of the visitor
// sessions among sessionIDs, in one query. Best effort: a lookup failure only
// loses the grouping fields, it must never fail message delivery.
func LoadVisitorSessionMeta(sessionIDs ...string) map[string]VisitorSessionMeta {
	return LoadVisitorSessionMetaTx(store.DB, sessionIDs...)
}

// LoadVisitorSessionMetaTx is LoadVisitorSessionMeta on an explicit connection,
// for callers inside a transaction that just created the widget session.
func LoadVisitorSessionMetaTx(db *gorm.DB, sessionIDs ...string) map[string]VisitorSessionMeta {
	result := make(map[string]VisitorSessionMeta, len(sessionIDs))
	if db == nil {
		return result
	}
	ids := make([]string, 0, len(sessionIDs))
	seen := make(map[string]struct{}, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		sessionID = strings.TrimSpace(sessionID)
		if sessionID == "" {
			continue
		}
		if _, ok := seen[sessionID]; ok {
			continue
		}
		seen[sessionID] = struct{}{}
		ids = append(ids, sessionID)
	}
	if len(ids) == 0 {
		return result
	}
	var rows []struct {
		SessionID   string `gorm:"column:session_id"`
		SiteID      int64  `gorm:"column:site_id"`
		SiteName    string `gorm:"column:site_name"`
		FirstOrigin string `gorm:"column:first_origin"`
	}
	if err := db.Table("widget_sessions AS ws").
		Select("ws.session_id", "ws.site_id", "s.site_name", "s.first_origin").
		Joins("JOIN widget_sites AS s ON s.id = ws.site_id").
		Where("ws.session_id IN ? AND ws.status IN ?", ids, []int16{model.WidgetSessionStatusActive, model.WidgetSessionStatusClosed}).
		Scan(&rows).Error; err != nil {
		if logger.L != nil {
			logger.L.Warnf("syncstream: load visitor session meta: %v", err)
		}
		return result
	}
	for _, row := range rows {
		sessionID := strings.TrimSpace(row.SessionID)
		if sessionID == "" {
			continue
		}
		result[sessionID] = VisitorSessionMeta{
			IsVisitor: true,
			SiteID:    row.SiteID,
			Domain:    model.WidgetSite{SiteName: row.SiteName, FirstOrigin: row.FirstOrigin}.VisitorDomain(),
		}
	}
	return result
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

func (d MessageDelivery) classicEvents(visitor VisitorSessionMeta) []Event {
	events := []Event{d.messageEvent(d.Message)}
	if d.Session != nil {
		events = append(events, SessionUpsertEventWithVisitorMeta(d.UserID, d.SessionID, *d.Session, d.SessionCommandID, visitor))
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

func (d MessageDelivery) compoundEvent(visitor VisitorSessionMeta) Event {
	payload := compoundMessagePayload{message: d.Message, session: d.Session, visitor: visitor}
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
	visitor VisitorSessionMeta
	unread  map[string]any
}

func (p compoundMessagePayload) MarshalJSON() ([]byte, error) {
	message, err := json.Marshal(p.message)
	if err != nil {
		return nil, err
	}
	var session, unread json.RawMessage
	if p.session != nil {
		if session, err = json.Marshal(SessionProjection(*p.session, p.visitor)); err != nil {
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
