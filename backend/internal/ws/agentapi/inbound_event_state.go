package agentapi

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/ws/protocol"
)

// observeInboundPacketActivity is the single gateway-level activity path for
// all Agent implementations. Generic, Codex, Pi, and lifecycle packets carry
// the same event_id envelope; composing uses ref_event_id. Only an
// owner-scoped matching event can be touched.
func (m *Manager) observeInboundPacketActivity(conn *agentConn, pkt *protocol.Packet) {
	if m == nil || conn == nil || pkt == nil {
		return
	}
	switch pkt.Cmd {
	case protocol.CmdEventAck,
		protocol.CmdEventResult,
		protocol.CmdEventStopAck,
		protocol.CmdEventStopResult,
		protocol.CmdSendMsg,
		protocol.CmdClientStreamChunk,
		protocol.CmdCodexEvent,
		protocol.CmdPiEvent:
		var ref struct {
			EventID string `json:"event_id"`
		}
		if json.Unmarshal(pkt.Payload, &ref) == nil {
			m.touchPendingEventResultOwned(ref.EventID, conn.agentID, conn.ownerID)
		}
	case protocol.CmdSessionActivitySet:
		var ref struct {
			RefEventID string `json:"ref_event_id"`
		}
		if json.Unmarshal(pkt.Payload, &ref) == nil {
			m.touchPendingEventResultOwned(ref.RefEventID, conn.agentID, conn.ownerID)
		}
	}
}

// touchPendingEventResultOwned records exact event activity without allowing a
// packet on another shared-owner connection to keep this event alive.
func (m *Manager) touchPendingEventResultOwned(eventID string, agentID, ownerID int64) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" || agentID <= 0 {
		return
	}

	m.acksMu.Lock()
	defer m.acksMu.Unlock()

	entry := m.pending[eventID]
	if entry == nil || entry.stage != pendingEventStageResult || entry.agentID != agentID {
		return
	}
	if ownerID > 0 && entry.event.OwnerID > 0 && entry.event.OwnerID != ownerID {
		return
	}
	entry.selfTouchAt = time.Now().UnixMilli()
	m.resetPendingEventTimerLocked(entry, eventID, m.eventResultWait)
}
