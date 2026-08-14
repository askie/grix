package pi

import (
	"context"
	"encoding/json"
	"github.com/askie/grix/backend/internal/agentadapter"
)

const (
	Family    = "pi"
	AdapterID = "pi/base"
)

type Adapter struct{}

func NewAdapter() *Adapter           { return &Adapter{} }
func (a *Adapter) Family() string    { return Family }
func (a *Adapter) AdapterID() string { return AdapterID }
func (a *Adapter) Supports(meta agentadapter.AgentClientMeta) bool {
	f := meta.HostType
	if f == "" {
		f = meta.ClientType
	}
	return f == Family
}
func (a *Adapter) VersionRange() string           { return "" }
func (a *Adapter) RequiredCapabilities() []string { return nil }
func (a *Adapter) OptionalCapabilities() []string {
	return []string{"session_route", "local_action_v1", "agent_invoke"}
}
func (a *Adapter) DegradePolicy() agentadapter.DegradePolicy { return agentadapter.DegradeToBasic }
func (a *Adapter) NormalizeInbound(_ context.Context, rawPayload []byte) (*agentadapter.NormalizedInboundEvent, error) {
	return normalizeInboundSendMsg(rawPayload)
}
func (a *Adapter) NormalizeOutbound(_ context.Context, event agentadapter.DomainOutboundEvent) (*agentadapter.AdapterOutboundPacket, error) {
	event.Extra = mergeConnectorRuntimeConfigForPI(event.Extra)
	payload, _ := json.Marshal(event)
	return &agentadapter.AdapterOutboundPacket{Cmd: "event_msg", Payload: payload}, nil
}
func (a *Adapter) NormalizeApproval(_ context.Context, event agentadapter.DomainApprovalEvent) (*agentadapter.AdapterApprovalPacket, error) {
	payload, _ := json.Marshal(map[string]interface{}{"action_id": event.ActionID, "event_id": "", "action_type": event.ActionType, "params": json.RawMessage(event.Params), "timeout_ms": event.TimeoutMs})
	return &agentadapter.AdapterApprovalPacket{Cmd: "local_action", Payload: payload}, nil
}
func (a *Adapter) NormalizeStatus(_ context.Context, event agentadapter.DomainStatusEvent) (*agentadapter.AdapterStatusPacket, error) {
	payload, _ := json.Marshal(event)
	return &agentadapter.AdapterStatusPacket{Cmd: "agent_state_sync", Payload: payload}, nil
}

func mergeConnectorRuntimeConfigForPI(extra json.RawMessage) json.RawMessage {
	envelope := map[string]any{}
	if len(extra) > 0 {
		_ = json.Unmarshal(extra, &envelope)
		if envelope == nil {
			envelope = map[string]any{}
		}
	}

	connector := map[string]any{}
	if rawConnector, ok := envelope["connector"].(map[string]any); ok && rawConnector != nil {
		connector = rawConnector
	}
	// PI adapter always suppresses thinking display events in connector.
	connector["thinking_events"] = "drop"
	envelope["connector"] = connector

	merged, err := json.Marshal(envelope)
	if err != nil {
		return extra
	}
	return merged
}
