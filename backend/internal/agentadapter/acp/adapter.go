// Package acp provides the ACP (Agent Client Protocol) adapter.
//
// ACP agents are bridged through grix-acp which speaks the aibot-agent-api-v1
// protocol over WebSocket. This adapter handles the normalization between
// aibot's domain events and the ACP bridge's wire format.
package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/agentadapter/agentcards"
	"github.com/askie/grix/backend/internal/agentadapter/approvalcards"
	"github.com/askie/grix/backend/internal/agentadapter/grixcards"
	"github.com/askie/grix/backend/internal/grixactions"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

const (
	Family    = "acp"
	AdapterID = "acp/base"
)

var requiredCapabilities = []string{"stream_chunk", "local_action_v1"}

var optionalCapabilities = []string{
	"session_route",
	"agent_invoke",
	"inbound_media_v1",
	"reaction_v1",
	"thread_v1",
}

type Adapter struct{}

func NewAdapter() *Adapter { return &Adapter{} }

func (a *Adapter) Family() string    { return Family }
func (a *Adapter) AdapterID() string { return AdapterID }

func (a *Adapter) Supports(meta agentadapter.AgentClientMeta) bool {
	family := meta.HostType
	if family == "" {
		family = meta.ClientType
	}
	return family == Family
}

// AdapterMeta

func (a *Adapter) VersionRange() string              { return "" }
func (a *Adapter) RequiredCapabilities() []string     { return requiredCapabilities }
func (a *Adapter) OptionalCapabilities() []string     { return optionalCapabilities }
func (a *Adapter) DegradePolicy() agentadapter.DegradePolicy {
	return agentadapter.DegradeToBasic
}

// NormalizeInbound converts an agent's send_msg payload into a normalized event.
func (a *Adapter) NormalizeInbound(_ context.Context, rawPayload []byte) (*agentadapter.NormalizedInboundEvent, error) {
	payload, err := agentadapter.ParseInboundPayload(rawPayload)
	if err != nil {
		return nil, err
	}

	content := payload.Content
	extra := cloneRaw(payload.Extra)

	// Handle structured cards (approval, grix, agent cards)
	if normalizedContent, normalizedExtra, ok := normalizeStructuredInbound(payload); ok {
		if payload.Content != "" && !strings.Contains(payload.Content, "grix://card/") {
			if normalizedContent != "" {
				content = normalizedContent + "\n\n" + payload.Content
			} else {
				content = payload.Content
			}
		} else {
			content = normalizedContent
		}
		extra = normalizedExtra
	} else if cardContent, channelData, ok := approvalcards.NormalizeDangerousCommandText(payload.Content); ok {
		content = cardContent
		envelope := map[string]any{}
		if len(payload.Extra) > 0 {
			_ = json.Unmarshal(payload.Extra, &envelope)
		}
		envelope["channel_data"] = channelData
		if b, err := json.Marshal(envelope); err == nil {
			extra = b
		}
	}

	return &agentadapter.NormalizedInboundEvent{
		SessionID: strings.TrimSpace(payload.SessionID),
		ThreadID:  strings.TrimSpace(payload.ThreadID),
		Content:   strings.TrimSpace(content),
		Extra:     extra,
	}, nil
}

// NormalizeOutbound converts a domain outbound event into an event_msg packet.
func (a *Adapter) NormalizeOutbound(_ context.Context, event agentadapter.DomainOutboundEvent) (*agentadapter.AdapterOutboundPacket, error) {
	event.Content = grixactions.RewriteToLegacyCommand(event.Content)
	payload, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	return &agentadapter.AdapterOutboundPacket{
		Cmd:     "event_msg",
		Payload: payload,
	}, nil
}

// NormalizeApproval converts a domain approval event into a local_action packet.
func (a *Adapter) NormalizeApproval(_ context.Context, event agentadapter.DomainApprovalEvent) (*agentadapter.AdapterApprovalPacket, error) {
	payload, err := json.Marshal(map[string]interface{}{
		"action_id":   event.ActionID,
		"event_id":    "",
		"action_type": event.ActionType,
		"params":      json.RawMessage(event.Params),
		"timeout_ms":  event.TimeoutMs,
	})
	if err != nil {
		return nil, err
	}
	return &agentadapter.AdapterApprovalPacket{
		Cmd:     "local_action",
		Payload: payload,
	}, nil
}

// NormalizeStatus converts a domain status event into an agent_state_sync packet.
func (a *Adapter) NormalizeStatus(_ context.Context, event agentadapter.DomainStatusEvent) (*agentadapter.AdapterStatusPacket, error) {
	payload, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	return &agentadapter.AdapterStatusPacket{
		Cmd:     "agent_state_sync",
		Payload: payload,
	}, nil
}

// NormalizeRevoke converts a domain revoke event into an event_revoke packet.
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

// --- internal helpers ---

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func normalizeStructuredInbound(p *agentadapter.InboundSendMsgPayload) (string, json.RawMessage, bool) {
	if len(p.ChannelData) == 0 && len(p.BizCard) == 0 && !strings.Contains(p.Content, "grix://card/") {
		return "", nil, false
	}
	if content, _, ok := approvalcards.Normalize(p); ok {
		return content, cloneRaw(p.Extra), true
	}
	if content, _, ok := grixcards.Normalize(p); ok {
		return content, cloneRaw(p.Extra), true
	}
	if content, _, ok := agentcards.Normalize(p); ok {
		return content, cloneRaw(p.Extra), true
	}
	return "", nil, false
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
	parts := []string{
		fmt.Sprintf("session_id=%s", sessionID),
		fmt.Sprintf("msg_id=%d", event.MsgID),
	}
	if event.SenderID > 0 {
		parts = append(parts, fmt.Sprintf("sender_id=%d", event.SenderID))
	}
	return &protocol.RevokeSystemEventPayload{
		Text:       fmt.Sprintf("ACP %s deleted [%s]", chatType, strings.Join(parts, " ")),
		ContextKey: fmt.Sprintf("acp:revoke:%s:%d", sessionID, event.MsgID),
	}
}
