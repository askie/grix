package deepseek

import (
	"context"
	"encoding/json"

	"github.com/askie/grix/backend/internal/agentadapter"
)

const (
	Family    = "deepseek"
	AdapterID = "deepseek/jsonrpc-v1"
)

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

func (a *Adapter) VersionRange() string           { return "" }
func (a *Adapter) RequiredCapabilities() []string { return nil }
func (a *Adapter) OptionalCapabilities() []string {
	return []string{"stream_chunk", "local_action_v1"}
}
func (a *Adapter) DegradePolicy() agentadapter.DegradePolicy {
	return agentadapter.DegradeToBasic
}

func (a *Adapter) NormalizeInbound(_ context.Context, rawPayload []byte) (*agentadapter.NormalizedInboundEvent, error) {
	return normalizeInboundSendMsg(rawPayload)
}

func (a *Adapter) NormalizeOutbound(_ context.Context, event agentadapter.DomainOutboundEvent) (*agentadapter.AdapterOutboundPacket, error) {
	payload, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	return &agentadapter.AdapterOutboundPacket{Cmd: "event_msg", Payload: payload}, nil
}

func (a *Adapter) NormalizeApproval(_ context.Context, event agentadapter.DomainApprovalEvent) (*agentadapter.AdapterApprovalPacket, error) {
	payload, err := json.Marshal(map[string]any{
		"action_id":   event.ActionID,
		"event_id":    "",
		"action_type": event.ActionType,
		"params":      json.RawMessage(event.Params),
		"timeout_ms":  event.TimeoutMs,
	})
	if err != nil {
		return nil, err
	}
	return &agentadapter.AdapterApprovalPacket{Cmd: "local_action", Payload: payload}, nil
}

func (a *Adapter) NormalizeStatus(_ context.Context, event agentadapter.DomainStatusEvent) (*agentadapter.AdapterStatusPacket, error) {
	payload, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	return &agentadapter.AdapterStatusPacket{Cmd: "agent_state_sync", Payload: payload}, nil
}
