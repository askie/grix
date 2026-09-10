// Package deveco provides the adapter for Huawei DevEco Code CLI agents.
//
// DevEco Code is a branded fork of OpenCode: grix-connector's round4 probe
// found `deveco serve` exposes the exact same REST+SSE OpenAPI surface as
// upstream opencode (info.title is literally "opencode"), so the connector
// reuses opencode-adapter.ts wholesale rather than a bespoke ACP adapter
// (the `deveco acp` path was probed too — session/prompt failed with an
// internal service error and available_models/modes came back in a
// non-standard `configOptions` shape, missing 3 of the 4 required signals).
// Family/AdapterID are therefore modeled after opencode, not the generic ACP
// family: deveco does not implement RevokeEventAdapter, matching opencode's
// own adapter (no revoke support there either — see opencode/adapter.go).
package deveco

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/agentadapter/agentcards"
	"github.com/askie/grix/backend/internal/agentadapter/approvalcards"
	"github.com/askie/grix/backend/internal/agentadapter/grixcards"
	"github.com/askie/grix/backend/internal/agentadapter/internal/util"
)

const (
	Family    = "deveco"
	AdapterID = "deveco/base"
)

type Adapter struct{}

func NewAdapter() *Adapter           { return &Adapter{} }
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
	payload, err := agentadapter.ParseInboundPayload(rawPayload)
	if err != nil {
		return nil, err
	}

	content := payload.Content
	extra := util.CloneRawMessage(payload.Extra)
	if c, e, ok := approvalcards.Normalize(payload); ok {
		content = c
		extra = e
	} else if c, e, ok := grixcards.Normalize(payload); ok {
		content = c
		extra = e
	} else if c, e, ok := agentcards.Normalize(payload); ok {
		content = c
		extra = e
	}

	if len(payload.ChannelData) > 0 {
		channelData := util.DecodeJSONObject(payload.ChannelData)
		if len(channelData) > 0 {
			if cardContent, ok := buildDevecoSessionBindingCardContent(channelData); ok {
				content = cardContent
				extra = util.CloneRawMessage(payload.Extra)
			}
		}
	}

	return &agentadapter.NormalizedInboundEvent{
		SessionID: strings.TrimSpace(payload.SessionID),
		Content:   content,
		Extra:     extra,
	}, nil
}

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
