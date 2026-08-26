package agentapi

import (
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

// outboundVisibilityTTL bounds how long a remembered trigger visibility keeps
// steering eventless output for one agent in one group session.
const outboundVisibilityTTL = 10 * time.Minute

// outboundVisibilityEntry is the cached visibility of the latest trigger that
// dispatched this agent in one group session. hidden=false records a public
// trigger so an older hidden decision is not reused.
type outboundVisibilityEntry struct {
	visibleTo []int64
	expireAt  int64
}

// outboundVisibilityLedgerWindow bounds how old a ledger trigger may be to
// steer eventless output. Older hidden triggers no longer hide later
// self-driven speech (reminders, cron, broadcasts) from the group.
const outboundVisibilityLedgerWindow = 24 * time.Hour

// outboundVisibilityKey isolates by owner too: a shared agent serves several
// owners in one group and must not leak one owner's hidden trigger into
// another owner's output.
func outboundVisibilityKey(agentID, ownerID int64, sessionID string) string {
	return strings.TrimSpace(sessionID) + "|" + itoa(agentID) + "|" + itoa(ownerID)
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}

// rememberOutboundVisibility records the latest trigger visibility for the
// agent+owner in a session. Hidden delivery only exists in groups, so any
// other session type is remembered as public; that negative entry keeps
// private-chat eventless output off the ledger query.
func (m *Manager) rememberOutboundVisibility(agentID, ownerID int64, sessionID string, sessionType int16, visibleTo []int64) {
	if m == nil || agentID <= 0 {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	if sessionType != 2 {
		visibleTo = nil
	}
	entry := outboundVisibilityEntry{
		visibleTo: append([]int64(nil), visibleTo...),
		expireAt:  time.Now().Add(outboundVisibilityTTL).UnixMilli(),
	}
	m.outboundVisMu.Lock()
	if m.outboundVis == nil {
		m.outboundVis = make(map[string]outboundVisibilityEntry)
	}
	m.outboundVis[outboundVisibilityKey(agentID, ownerID, sessionID)] = entry
	m.outboundVisMu.Unlock()
}

func (m *Manager) lookupOutboundVisibility(agentID, ownerID int64, sessionID string) ([]int64, bool) {
	if m == nil {
		return nil, false
	}
	key := outboundVisibilityKey(agentID, ownerID, sessionID)
	m.outboundVisMu.Lock()
	defer m.outboundVisMu.Unlock()
	entry, ok := m.outboundVis[key]
	if !ok {
		return nil, false
	}
	if time.Now().UnixMilli() >= entry.expireAt {
		delete(m.outboundVis, key)
		return nil, false
	}
	return append([]int64(nil), entry.visibleTo...), true
}

// ResolveOutboundVisibleTo is the single authority for who may see one agent
// output message. Priority:
//  1. explicit visibility already decided by the caller (callers must pass
//     the trigger-merged value, e.g. mergeVisibleToForSendMsg output, never a
//     bare card-only list, or a hidden trigger would be overridden);
//  2. a hidden quoted message (reply goes back to its sender);
//  3. the live run for event_id (its trigger visibility, hidden or public);
//  4. eventless/expired output: the latest trigger for this agent in the
//     session, from the in-memory cache, the session's active run, or the
//     durable event ledger. Only group sessions are considered.
//
// Steps 1-3 never touch the database beyond the existing quoted lookup; step
// 4 costs at most one indexed ledger read per agent+session per TTL window.
func (m *Manager) ResolveOutboundVisibleTo(
	agentID, ownerID int64,
	sessionID, eventID string,
	quotedMessageID int64,
	explicit []int64,
) []int64 {
	if len(explicit) > 0 {
		return append([]int64(nil), explicit...)
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	if quotedMessageID > 0 {
		if vt := loadTriggerVisibleTo(quotedMessageID, sessionID); len(vt) > 0 {
			return vt
		}
	}
	if m == nil {
		return nil
	}
	if eventID = strings.TrimSpace(eventID); eventID != "" {
		if run := m.LookupActiveRun(eventID); run != nil {
			return append([]int64(nil), run.TriggerVisibleTo...)
		}
	}
	return m.resolveSessionFallbackVisibleTo(agentID, ownerID, sessionID)
}

func (m *Manager) resolveSessionFallbackVisibleTo(agentID, ownerID int64, sessionID string) []int64 {
	if agentID <= 0 {
		return nil
	}
	if vt, ok := m.lookupOutboundVisibility(agentID, ownerID, sessionID); ok {
		return vt
	}
	if run := m.LookupActiveRunBySessionOwner(ownerID, sessionID); run != nil && run.AgentID == agentID {
		m.rememberOutboundVisibility(agentID, ownerID, sessionID, run.SessionType, run.TriggerVisibleTo)
		return append([]int64(nil), run.TriggerVisibleTo...)
	}
	ledger, err := store.LoadLatestAgentEventTriggerForSession(sessionID, agentID, ownerID, time.Now().Add(-outboundVisibilityLedgerWindow))
	if err != nil {
		logger.L.Warnf("outbound visibility: load latest trigger failed agent=%d session=%s err=%v", agentID, sessionID, err)
		return nil
	}
	var vt []int64
	sessionType := int16(0)
	if ledger != nil {
		sessionType = ledger.SessionType
		if sessionType == 2 {
			vt = loadTriggerVisibleTo(ledger.TriggerMsgID, sessionID)
		}
	}
	// Negative results are cached too so a session without a recent trigger
	// does not re-query the ledger on every eventless output.
	m.rememberOutboundVisibility(agentID, ownerID, sessionID, sessionType, vt)
	return vt
}
