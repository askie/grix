package openclaw

import (
	"bytes"
	"encoding/json"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/agentadapter/agentcards"
	"github.com/askie/grix/backend/internal/agentadapter/approvalcards"
	"github.com/askie/grix/backend/internal/agentadapter/grixcards"
	"github.com/askie/grix/backend/internal/agentadapter/internal/util"
)

func normalizeOutboundCardContent(event agentadapter.DomainOutboundEvent) string {
	p := &agentadapter.InboundSendMsgPayload{
		Content:     event.Content,
		Extra:       buildOutboundStructuredExtra(event),
		BizCard:     event.BizCard,
		ChannelData: event.ChannelData,
	}
	if cardContent, _, ok := approvalcards.Normalize(p); ok {
		return cardContent
	}
	if cardContent, _, ok := grixcards.Normalize(p); ok {
		return cardContent
	}
	if cardContent, _, ok := agentcards.Normalize(p); ok {
		return cardContent
	}
	return event.Content
}

func buildOutboundStructuredExtra(event agentadapter.DomainOutboundEvent) json.RawMessage {
	base := decodeOutboundExtraObject(event.Extra)
	if len(base) == 0 {
		if len(event.BizCard) == 0 && len(event.ChannelData) == 0 {
			return util.CloneRawMessage(event.Extra)
		}
		base = make(map[string]any, 2)
	}

	if bizCard := decodeOutboundExtraValue(event.BizCard); bizCard != nil {
		base["biz_card"] = bizCard
	}
	if channelData := decodeOutboundExtraValue(event.ChannelData); channelData != nil {
		base["channel_data"] = channelData
	}

	if len(base) == 0 {
		return util.CloneRawMessage(event.Extra)
	}

	encoded, err := json.Marshal(base)
	if err != nil {
		return util.CloneRawMessage(event.Extra)
	}
	return encoded
}

func decodeOutboundExtraObject(raw json.RawMessage) map[string]any {
	decoded := decodeOutboundExtraValue(raw)
	object, _ := decoded.(map[string]any)
	return object
}

func decodeOutboundExtraValue(raw json.RawMessage) any {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || !json.Valid(trimmed) {
		return nil
	}

	var decoded any
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		return nil
	}
	return decoded
}
