// Package openclaw provides the OpenClaw AI family adapter.
//
// This adapter handles the translation between backend domain events and
// OpenClaw-specific wire format. It is the first adapter to be implemented
// and serves as the reference implementation for future adapters.
package openclaw

import (
	"context"
	"encoding/json"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/grixactions"
)

const (
	Family    = "openclaw"
	AdapterID = "openclaw/base"

	// VersionRangeStr declares the supported OpenClaw host version range.
	// The base adapter covers the broad range >=2026.1.
	// When breaking changes arise, create a new adapter with a narrower range.
	VersionRangeStr = ">=2026.1"
)

// Adapter implements agentadapter.AgentAdapter and AdapterMeta for OpenClaw.
type Adapter struct{}

// NewAdapter creates a new OpenClaw adapter.
func NewAdapter() *Adapter {
	return &Adapter{}
}

func (a *Adapter) Family() string    { return Family }
func (a *Adapter) AdapterID() string { return AdapterID }

// Supports returns true for all OpenClaw-family connections.
// The base adapter covers the broad version range ">=2026.1".
// When breaking changes arise, a new adapter (e.g. openclaw/v2) should
// be registered alongside this one with narrower version ranges.
func (a *Adapter) Supports(meta agentadapter.AgentClientMeta) bool {
	family := meta.HostType
	if family == "" {
		family = meta.ClientType
	}
	return family == Family
}

// AdapterMeta interface implementation.

func (a *Adapter) VersionRange() string           { return VersionRangeStr }
func (a *Adapter) RequiredCapabilities() []string { return nil } // base adapter requires nothing
func (a *Adapter) OptionalCapabilities() []string {
	return []string{
		"stream_chunk",
		"session_route",
		"local_action_v1",
		"agent_invoke",
		"inbound_media_v1",
		"reaction_v1",
		"thread_v1",
		"tailnet_file_v1",
	}
}
func (a *Adapter) DegradePolicy() agentadapter.DegradePolicy {
	return agentadapter.DegradeToReadOnly
}

// NormalizeInbound translates an OpenClaw inbound send_msg payload into a
// normalized domain message. Structured card payloads are converted here so
// the plugin only forwards protocol data.
func (a *Adapter) NormalizeInbound(_ context.Context, rawPayload []byte) (*agentadapter.NormalizedInboundEvent, error) {
	return normalizeInboundSendMsg(rawPayload)
}

// NormalizeOutbound translates a domain outbound event into an OpenClaw
// event_msg packet. The Agent API protocol uses event_msg for delegate events.
func (a *Adapter) NormalizeOutbound(_ context.Context, event agentadapter.DomainOutboundEvent) (*agentadapter.AdapterOutboundPacket, error) {
	// Rewrite mention placeholders (@[user_id]) to display text if the client
	// doesn't support structured mentions or if we need to ensure visibility.
	// For OpenClaw, we keep them as-is but ensure they are included in the payload.
	event.Content = normalizeOutboundCardContent(event)
	event.Content = grixactions.RewriteToLegacyCommand(event.Content)

	// Note: structured mentions (@[user_id]) are kept as-is for OpenClaw clients.

	payload, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	return &agentadapter.AdapterOutboundPacket{
		Cmd:     "event_msg",
		Payload: payload,
	}, nil
}

// NormalizeApproval translates a domain approval event into an OpenClaw
// local_action packet.
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

// NormalizeStatus translates a domain status event into an OpenClaw
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
