// Package hermes provides the Hermes AI family adapter.
//
// Hermes follows the strict public aibot-agent-api-v1 profile. Backend-only
// extensions used by other families stay outside this adapter on purpose.
package hermes

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/agentadapter/agentcards"
	"github.com/askie/grix/backend/internal/agentadapter/approvalcards"
	"github.com/askie/grix/backend/internal/agentadapter/grixcards"
	"github.com/askie/grix/backend/internal/agentadapter/internal/util"
	"github.com/askie/grix/backend/internal/grixactions"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

const (
	Family    = "hermes"
	AdapterID = "hermes/base"
)

type Adapter struct{}

func NewAdapter() *Adapter {
	return &Adapter{}
}

func (a *Adapter) Family() string    { return Family }
func (a *Adapter) AdapterID() string { return AdapterID }

func (a *Adapter) Supports(meta agentadapter.AgentClientMeta) bool {
	family := meta.HostType
	if family == "" {
		family = meta.ClientType
	}
	return family == Family
}

func (a *Adapter) VersionRange() string { return "" }
func (a *Adapter) RequiredCapabilities() []string {
	return protocol.HermesRequiredCapabilities()
}
func (a *Adapter) OptionalCapabilities() []string {
	return protocol.HermesStableCapabilities()
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
	if normalizedContent, normalizedExtra, ok := normalizeHermesStructuredInbound(payload); ok {
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
		if extraBytes, err := json.Marshal(envelope); err == nil {
			extra = extraBytes
		}
	}

	return &agentadapter.NormalizedInboundEvent{
		SessionID: strings.TrimSpace(payload.SessionID),
		ThreadID:  strings.TrimSpace(payload.ThreadID),
		Content:   strings.TrimSpace(content),
		Extra:     extra,
	}, nil
}

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
	_ = event
	return nil, nil
}

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

func normalizeHermesStructuredInbound(p *agentadapter.InboundSendMsgPayload) (string, json.RawMessage, bool) {
	if len(p.ChannelData) == 0 && len(p.BizCard) == 0 && !strings.Contains(p.Content, "grix://card/") {
		return "", nil, false
	}

	if normalizedContent, _, ok := approvalcards.Normalize(p); ok {
		return normalizedContent, util.CloneRawMessage(p.Extra), true
	}
	if normalizedContent, _, ok := grixcards.Normalize(p); ok {
		return normalizedContent, util.CloneRawMessage(p.Extra), true
	}
	if normalizedContent, _, ok := agentcards.Normalize(p); ok {
		return normalizedContent, util.CloneRawMessage(p.Extra), true
	}

	channelData := util.DecodeJSONObject(p.ChannelData)
	if len(channelData) > 0 {
		if cardContent, normalizedChannelData, ok := buildExecApprovalCardContentFromHermesChannelData(channelData); ok {
			normalizedExtra, err := json.Marshal(map[string]any{
				"channel_data": normalizedChannelData,
			})
			if err == nil {
				return cardContent, normalizedExtra, true
			}
		}
	}
	return "", nil, false
}

func buildHermesInboundExtra(extraRaw, bizCardRaw, channelDataRaw json.RawMessage) json.RawMessage {
	if len(bizCardRaw) == 0 && len(channelDataRaw) == 0 {
		return util.CloneRawMessage(extraRaw)
	}

	envelope := util.DecodeJSONObject(extraRaw)
	if len(envelope) == 0 {
		envelope = make(map[string]any)
	}
	if bizCard := util.DecodeJSONObject(bizCardRaw); len(bizCard) > 0 {
		envelope["biz_card"] = bizCard
	}
	if channelData := util.DecodeJSONObject(channelDataRaw); len(channelData) > 0 {
		envelope["channel_data"] = channelData
	}
	if len(envelope) == 0 {
		return nil
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return util.CloneRawMessage(extraRaw)
	}
	return encoded
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
