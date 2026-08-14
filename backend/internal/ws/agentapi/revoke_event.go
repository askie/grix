package agentapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

var errInvalidRevokePayload = errors.New("invalid event_revoke payload")

func decodeRevokeEventPayload(payload any) (*protocol.AgentRevokeEventPayload, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	var event protocol.AgentRevokeEventPayload
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil, err
	}
	if strings.TrimSpace(event.SessionID) == "" || event.MsgID <= 0 {
		return nil, errInvalidRevokePayload
	}
	return &event, nil
}

func buildDomainRevokeEvent(payload any) (*agentadapter.DomainRevokeEvent, error) {
	revokePayload, err := decodeRevokeEventPayload(payload)
	if err != nil {
		return nil, err
	}
	return &agentadapter.DomainRevokeEvent{
		EventID:     strings.TrimSpace(revokePayload.EventID),
		SessionID:   strings.TrimSpace(revokePayload.SessionID),
		ThreadID:    strings.TrimSpace(revokePayload.ThreadID),
		SessionType: revokePayload.SessionType,
		MsgID:       revokePayload.MsgID,
		SenderID:    revokePayload.SenderID,
		IsRevoked:   revokePayload.IsRevoked,
	}, nil
}

func (c *agentConn) resolveAgentEventOutbound(cmd string, payload any) (string, any) {
	if strings.TrimSpace(cmd) != "event_revoke" {
		return cmd, payload
	}

	revokePayload, err := decodeRevokeEventPayload(payload)
	if err != nil {
		logger.L.Warnf("decode event_revoke payload failed agent=%d adapter=%s err=%v", c.agentID, c.adapterID, err)
		return cmd, payload
	}
	canonicalPayload := *revokePayload

	if c == nil || c.adapter == nil {
		return cmd, canonicalPayload
	}

	revokeAdapter, ok := c.adapter.(agentadapter.RevokeEventAdapter)
	if !ok {
		return cmd, canonicalPayload
	}

	outbound, err := revokeAdapter.NormalizeRevoke(context.Background(), agentadapter.DomainRevokeEvent{
		EventID:     strings.TrimSpace(revokePayload.EventID),
		SessionID:   strings.TrimSpace(revokePayload.SessionID),
		ThreadID:    strings.TrimSpace(revokePayload.ThreadID),
		SessionType: revokePayload.SessionType,
		MsgID:       revokePayload.MsgID,
		SenderID:    revokePayload.SenderID,
		IsRevoked:   revokePayload.IsRevoked,
	})
	if err != nil {
		logger.L.Warnf("adapter NormalizeRevoke failed agent=%d adapter=%s err=%v, falling back to canonical event_revoke", c.agentID, c.adapterID, err)
		return cmd, canonicalPayload
	}
	if outbound == nil {
		return cmd, canonicalPayload
	}

	normalizedCmd := strings.TrimSpace(outbound.Cmd)
	if normalizedCmd == "" {
		normalizedCmd = cmd
	}
	if len(outbound.Payload) == 0 || !json.Valid(outbound.Payload) {
		return normalizedCmd, canonicalPayload
	}

	return normalizedCmd, json.RawMessage(outbound.Payload)
}
