package pi

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/agentadapter/agentcards"
	"github.com/askie/grix/backend/internal/agentadapter/approvalcards"
	"github.com/askie/grix/backend/internal/agentadapter/grixcards"
	"github.com/askie/grix/backend/internal/agentadapter/internal/util"
)

func normalizeInboundSendMsg(rawPayload []byte) (*agentadapter.NormalizedInboundEvent, error) {
	p, err := agentadapter.ParseInboundPayload(rawPayload)
	if err != nil {
		return nil, err
	}

	content, extra := normalizeStructuredInboundCard(p)
	return &agentadapter.NormalizedInboundEvent{
		SessionID: util.NormalizeText(p.SessionID),
		Content:   content,
		Extra:     extra,
	}, nil
}

func normalizeStructuredInboundCard(p *agentadapter.InboundSendMsgPayload) (string, json.RawMessage) {
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
			if cardContent, ok := buildPiSessionBindingCardContent(channelData); ok {
				return cardContent, util.CloneRawMessage(p.Extra)
			}
		}
	}
	return p.Content, p.Extra
}

func buildPiSessionBindingCardContent(channelData map[string]any) (string, bool) {
	record := util.NestedObject(util.NestedObject(channelData, "pi"), "sessionBinding")
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
		"detail_text":  "先提交一个工作目录。校验通过后，Pi 会自动继续处理刚才那条消息。",
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

	return buildPiCardLink(
		"[Open Workspace] 当前对话还没有打开工作目录。",
		"agent_open_session",
		payload,
	), true
}

func buildPiCardLink(fallbackText, cardType string, payload map[string]any) string {
	return "[" + fallbackText + "](" + buildPiCardURI(cardType, payload) + ")"
}

func buildPiCardURI(cardType string, payload map[string]any) string {
	u := url.URL{Scheme: "grix", Host: "card", Path: cardType}
	q := u.Query()
	for k, v := range payload {
		q.Set(k, fmt.Sprintf("%v", v))
	}
	u.RawQuery = q.Encode()
	return u.String()
}
