package agentadapter

import "encoding/json"

// ParseInboundPayload deserializes raw send_msg payload bytes into
// InboundSendMsgPayload. It promotes biz_card and channel_data from inside
// extra to top-level fields (backward compatibility with older plugins) and
// ensures Extra contains these fields so that normalizers returning
// cloneRawMessage(p.Extra) preserve the data.
func ParseInboundPayload(raw []byte) (*InboundSendMsgPayload, error) {
	var p InboundSendMsgPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	promoteLegacyFields(&p)
	mergeCardsIntoExtra(&p)
	return &p, nil
}

// MergeCardsIntoExtra ensures p.Extra contains biz_card and channel_data
// from the payload's top-level fields. This is needed so that normalizers
// returning cloneRawMessage(p.Extra) preserve structured card data.
func MergeCardsIntoExtra(p *InboundSendMsgPayload) {
	mergeCardsIntoExtra(p)
}

func mergeCardsIntoExtra(p *InboundSendMsgPayload) {
	if len(p.BizCard) == 0 && len(p.ChannelData) == 0 {
		return
	}
	envelope := make(map[string]any)
	if len(p.Extra) > 0 {
		_ = json.Unmarshal(p.Extra, &envelope)
	}
	changed := false
	if len(p.BizCard) > 0 {
		var bizCard any
		if json.Unmarshal(p.BizCard, &bizCard) == nil {
			envelope["biz_card"] = bizCard
			changed = true
		}
	}
	if len(p.ChannelData) > 0 {
		var channelData any
		if json.Unmarshal(p.ChannelData, &channelData) == nil {
			envelope["channel_data"] = channelData
			changed = true
		}
	}
	if changed {
		encoded, _ := json.Marshal(envelope)
		p.Extra = encoded
	}
}

func promoteLegacyFields(p *InboundSendMsgPayload) {
	if len(p.BizCard) > 0 && len(p.ChannelData) > 0 {
		return
	}
	if len(p.Extra) == 0 {
		return
	}
	var legacy struct {
		BizCard     json.RawMessage `json:"biz_card"`
		ChannelData json.RawMessage `json:"channel_data"`
	}
	if err := json.Unmarshal(p.Extra, &legacy); err != nil {
		return
	}
	if len(p.BizCard) == 0 {
		p.BizCard = legacy.BizCard
	}
	if len(p.ChannelData) == 0 {
		p.ChannelData = legacy.ChannelData
	}
}
