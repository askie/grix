package kimi

import (
	"encoding/json"

	"net/url"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/agentadapter/agentcards"
	"github.com/askie/grix/backend/internal/agentadapter/approvalcards"
	"github.com/askie/grix/backend/internal/agentadapter/grixcards"
	"github.com/askie/grix/backend/internal/agentadapter/internal/util"
)

func normalizeInboundSendMsg(rawPayload []byte) (*agentadapter.NormalizedInboundEvent, error) {
	payload, err := agentadapter.ParseInboundPayload(rawPayload)
	if err != nil {
		return nil, err
	}

	content, extra := normalizeStructuredInboundCard(payload)
	return &agentadapter.NormalizedInboundEvent{
		SessionID: util.NormalizeText(payload.SessionID),
		Content:   content,
		Extra:     extra,
	}, nil
}

func normalizeStructuredInboundCard(p *agentadapter.InboundSendMsgPayload) (string, json.RawMessage) {
	// First: unwrap any ACP raw_event envelope (permission_request today).
	// The connector's acp-adapter forwards Kimi's permission RPC through
	// `channel_data.acp.raw_event`; without this rewrite the payload
	// bypasses approvalcards.Normalize entirely and the user's later
	// `/approve <id> allow` has no `approval_command_id` to resolve — the
	// turn stays `running` forever. See `raw_event_cards.go`.
	if synthesized, ok := synthesizeKimiRawEventPayload(p); ok {
		if cardContent, normalizedExtra, ok := approvalcards.Normalize(synthesized); ok {
			return cardContent, normalizedExtra
		}
		if cardContent, normalizedExtra, ok := grixcards.Normalize(synthesized); ok {
			return cardContent, normalizedExtra
		}
		if cardContent, normalizedExtra, ok := agentcards.Normalize(synthesized); ok {
			return cardContent, normalizedExtra
		}
		return synthesized.Content, util.CloneRawMessage(synthesized.Extra)
	}
	if cardContent, normalizedExtra, ok := approvalcards.Normalize(p); ok {
		return cardContent, normalizedExtra
	}
	if cardContent, normalizedExtra, ok := grixcards.Normalize(p); ok {
		return cardContent, normalizedExtra
	}
	if cardContent, normalizedExtra, ok := agentcards.Normalize(p); ok {
		return cardContent, normalizedExtra
	}

	if len(p.ChannelData) > 0 {
		channelData := util.DecodeJSONObject(p.ChannelData)
		if len(channelData) > 0 {
			if cardContent, ok := buildKimiSessionBindingCardContent(channelData); ok {
				return cardContent, util.CloneRawMessage(p.Extra)
			}
		}
	}

	return p.Content, util.CloneRawMessage(p.Extra)
}

func buildKimiSessionBindingCardContent(channelData map[string]any) (string, bool) {
	record := util.NestedObject(util.NestedObject(channelData, "acp"), "sessionBinding")
	if len(record) == 0 {
		return "", false
	}

	status := util.NormalizeText(record["status"])
	reason := util.NormalizeText(record["reason"])
	errorCode := util.NormalizeText(record["error_code"])
	if status != "missing" && reason != "binding_missing" && errorCode != "session_binding_missing" {
		return "", false
	}

	payload := map[string]any{
		"summary_text": "当前对话还没有打开工作目录。",
		"detail_text":  "先提交一个工作目录。校验通过后，Kimi 会自动继续处理刚才那条消息。",
	}
	if initialCwd := util.FirstNonEmpty(
		util.NormalizeText(record["initial_cwd"]),
		util.NormalizeText(record["initialCwd"]),
	); initialCwd != "" {
		payload["initial_cwd"] = initialCwd
	}
	if submittedPath := util.FirstNonEmpty(
		util.NormalizeText(record["submitted_path"]),
		util.NormalizeText(record["submittedPath"]),
	); submittedPath != "" {
		payload["submitted_path"] = submittedPath
	}

	return buildKimiCardLink(
		"[Open Workspace] 当前对话还没有打开工作目录。",
		"agent_open_session",
		payload,
	), true
}

func buildKimiCardLink(fallbackText, cardType string, payload map[string]any) string {
	return "[" + fallbackText + "](" + buildKimiCardURI(cardType, payload) + ")"
}

func buildKimiCardURI(cardType string, payload map[string]any) string {
	values := url.Values{}
	for key, value := range payload {
		text := util.NormalizeText(value)
		if text == "" {
			continue
		}
		values.Set(key, text)
	}
	return (&url.URL{
		Scheme:   "grix",
		Host:     "card",
		Path:     cardType,
		RawQuery: values.Encode(),
	}).String()
}
