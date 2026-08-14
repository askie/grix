// Package claude provides the Claude AI family adapter.
//
// This adapter handles the translation between backend domain events and
// Claude-specific wire format. Claude uses the same Agent API WebSocket
// protocol as OpenClaw but may have different behavior expectations,
// approval formats, and context mapping.
package claude

import (
	"context"
	"encoding/json"

	"github.com/askie/grix/backend/internal/agentadapter"
)

const (
	Family    = "claude"
	AdapterID = "claude/base"

	// VersionRangeStr declares the supported Claude host version range.
	VersionRangeStr = ">=1.0"
)

// Adapter implements agentadapter.AgentAdapter and AdapterMeta for Claude.
type Adapter struct{}

// NewAdapter creates a new Claude adapter.
func NewAdapter() *Adapter {
	return &Adapter{}
}

func (a *Adapter) Family() string    { return Family }
func (a *Adapter) AdapterID() string { return AdapterID }

// Supports returns true for all Claude-family connections.
func (a *Adapter) Supports(meta agentadapter.AgentClientMeta) bool {
	family := meta.HostType
	if family == "" {
		family = meta.ClientType
	}
	return family == Family
}

// AdapterMeta interface implementation.

func (a *Adapter) VersionRange() string           { return VersionRangeStr }
func (a *Adapter) RequiredCapabilities() []string { return nil }
func (a *Adapter) OptionalCapabilities() []string {
	return []string{"session_route", "local_action_v1", "agent_invoke"}
}
func (a *Adapter) DegradePolicy() agentadapter.DegradePolicy {
	return agentadapter.DegradeToBasic
}

// NormalizeInbound translates a Claude inbound event into a domain event.
// Claude approval and grix cards share the same normalization path as the
// other native adapters so frontend handling stays consistent across families.
func (a *Adapter) NormalizeInbound(_ context.Context, rawPayload []byte) (*agentadapter.NormalizedInboundEvent, error) {
	return normalizeInboundSendMsg(rawPayload)
}

// NormalizeOutbound translates a domain outbound event into a Claude
// outbound packet. Claude uses the same Agent API protocol with event_msg.
func (a *Adapter) NormalizeOutbound(_ context.Context, event agentadapter.DomainOutboundEvent) (*agentadapter.AdapterOutboundPacket, error) {
	payload, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	return &agentadapter.AdapterOutboundPacket{
		Cmd:     "event_msg",
		Payload: payload,
	}, nil
}

// NormalizeApproval translates a domain approval event into a Claude
// local_action packet. Claude approval behavior may differ from OpenClaw.
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

// NormalizeStatus translates a domain status event into a Claude
// status packet.
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
