package service

import (
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// privateSessionMemberPushLimit caps how many member identities travel with a
// message. A direct session holds two members; the cap only guards against a
// malformed row set inflating the packet.
const privateSessionMemberPushLimit = 8

// notWidgetSessionCondition keeps website-visitor sessions out of the resolved
// member set in the same round trip, so the hot send path stays at one query.
const notWidgetSessionCondition = "NOT EXISTS (SELECT 1 FROM widget_sessions ws WHERE ws.session_id = session_members.session_id)"

// PrivateSessionMemberIdentities returns the member identities that a direct
// (private) session message should carry, so a receiving client can resolve the
// conversation peer at ingest time instead of waiting for a session-detail
// round trip.
//
// Group sessions return nil: their conversation group key is the session id, so
// member identities would only add payload without changing any client
// grouping decision. The result is computed once per message and is not
// per-recipient — the client picks the member that is not itself, matching the
// conversation summary rule "exclude only the human member whose id is me".
//
// Widget (website visitor) sessions are excluded as well. Clients render every
// visitor session as one synthetic "visitor" row and never group them by peer,
// so member identities buy nothing there — while the receiving end of such a
// session is an anonymous web visitor who would otherwise learn the site
// owner's user id from a message the owner never sent.
func PrivateSessionMemberIdentities(sessionID string, sessionType int16) []protocol.SessionMemberIdentity {
	if sessionType != model.SessionTypeDirect {
		return nil
	}
	sid := strings.TrimSpace(sessionID)
	if sid == "" || store.DB == nil {
		return nil
	}
	var rows []model.SessionMember
	if err := store.Read().Model(&model.SessionMember{}).
		Select("member_id", "member_type").
		Where("session_id = ?", sid).
		Where(notWidgetSessionCondition).
		Order("member_type ASC, member_id ASC").
		Limit(privateSessionMemberPushLimit).
		Find(&rows).Error; err != nil {
		logger.L.Warnf("resolve private session members failed session=%s: %v", sid, err)
		return nil
	}
	if len(rows) == 0 {
		return nil
	}
	members := make([]protocol.SessionMemberIdentity, 0, len(rows))
	for _, row := range rows {
		if row.MemberID == 0 {
			continue
		}
		members = append(members, protocol.SessionMemberIdentity{
			MemberID:   row.MemberID,
			MemberType: row.MemberType,
		})
	}
	if len(members) == 0 {
		return nil
	}
	return members
}

// PrivateSessionMemberIdentitiesBatch resolves member identities for several
// direct sessions in one query. pull_sync returns up to a full page of messages
// spanning many sessions, so the per-message lookup would otherwise turn into
// an N+1 scan on every reconnect.
func PrivateSessionMemberIdentitiesBatch(sessionIDs []string) map[string][]protocol.SessionMemberIdentity {
	if len(sessionIDs) == 0 || store.DB == nil {
		return nil
	}
	unique := make([]string, 0, len(sessionIDs))
	seen := make(map[string]struct{}, len(sessionIDs))
	for _, raw := range sessionIDs {
		sid := strings.TrimSpace(raw)
		if sid == "" {
			continue
		}
		if _, ok := seen[sid]; ok {
			continue
		}
		seen[sid] = struct{}{}
		unique = append(unique, sid)
	}
	if len(unique) == 0 {
		return nil
	}
	var rows []model.SessionMember
	if err := store.Read().Model(&model.SessionMember{}).
		Select("session_id", "member_id", "member_type").
		Where("session_id IN ?", unique).
		Where(notWidgetSessionCondition).
		Order("session_id ASC, member_type ASC, member_id ASC").
		Find(&rows).Error; err != nil {
		logger.L.Warnf("resolve private session members batch failed count=%d: %v", len(unique), err)
		return nil
	}
	if len(rows) == 0 {
		return nil
	}
	result := make(map[string][]protocol.SessionMemberIdentity, len(unique))
	for _, row := range rows {
		if row.MemberID == 0 {
			continue
		}
		sid := row.SessionID
		if len(result[sid]) >= privateSessionMemberPushLimit {
			continue
		}
		result[sid] = append(result[sid], protocol.SessionMemberIdentity{
			MemberID:   row.MemberID,
			MemberType: row.MemberType,
		})
	}
	return result
}
