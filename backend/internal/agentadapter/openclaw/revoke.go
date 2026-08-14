package openclaw

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func (a *Adapter) NormalizeRevoke(_ context.Context, event agentadapter.DomainRevokeEvent) (*agentadapter.AdapterOutboundPacket, error) {
	payload := protocol.AgentRevokeEventPayload{
		EventID:     strings.TrimSpace(event.EventID),
		SessionID:   strings.TrimSpace(event.SessionID),
		ThreadID:    strings.TrimSpace(event.ThreadID),
		SessionType: event.SessionType,
		MsgID:       event.MsgID,
		SenderID:    event.SenderID,
		IsRevoked:   event.IsRevoked,
		SystemEvent: buildRevokeSystemEvent(event),
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	return &agentadapter.AdapterOutboundPacket{
		Cmd:     "event_revoke",
		Payload: raw,
	}, nil
}

func buildRevokeSystemEvent(event agentadapter.DomainRevokeEvent) *protocol.RevokeSystemEventPayload {
	sessionID := strings.TrimSpace(event.SessionID)
	if sessionID == "" || event.MsgID <= 0 {
		return nil
	}

	chatType := "message"
	switch event.SessionType {
	case 1:
		chatType = "direct message"
	case 2:
		chatType = "group message"
	}

	metadataParts := []string{
		fmt.Sprintf("session_id=%s", sessionID),
		fmt.Sprintf("msg_id=%d", event.MsgID),
	}
	if event.SenderID > 0 {
		metadataParts = append(metadataParts, fmt.Sprintf("sender_id=%d", event.SenderID))
	}

	return &protocol.RevokeSystemEventPayload{
		Text:       fmt.Sprintf("Grix %s deleted [%s]", chatType, strings.Join(metadataParts, " ")),
		ContextKey: fmt.Sprintf("grix:revoke:%s:%d", sessionID, event.MsgID),
	}
}
