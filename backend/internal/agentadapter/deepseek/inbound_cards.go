package deepseek

import (
	"encoding/json"
	"net/url"
	"strings"

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
		SessionID: strings.TrimSpace(payload.SessionID),
		ThreadID:  strings.TrimSpace(payload.ThreadID),
		Content:   content,
		Extra:     extra,
	}, nil
}

func normalizeStructuredInboundCard(payload *agentadapter.InboundSendMsgPayload) (string, json.RawMessage) {
	if content, extra, ok := approvalcards.Normalize(payload); ok {
		return content, extra
	}
	if content, extra, ok := grixcards.Normalize(payload); ok {
		return content, extra
	}
	if content, extra, ok := agentcards.Normalize(payload); ok {
		return content, extra
	}

	if len(payload.ChannelData) > 0 {
		channelData := util.DecodeJSONObject(payload.ChannelData)
		if len(channelData) > 0 {
			if cardContent, ok := buildDeepSeekSessionBindingCardContent(channelData); ok {
				return cardContent, util.CloneRawMessage(payload.Extra)
			}
		}
	}

	return payload.Content, append(json.RawMessage(nil), payload.Extra...)
}

func buildDeepSeekSessionBindingCardContent(channelData map[string]any) (string, bool) {
	record := util.NestedObject(util.NestedObject(channelData, "deepseek"), "sessionBinding")
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
		"detail_text":  "先提交一个工作目录。校验通过后，DeepSeek 会自动继续处理刚才那条消息。",
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

	return buildDeepSeekCardLink(
		"[Open Workspace] 当前对话还没有打开工作目录。",
		"agent_open_session",
		payload,
	), true
}

func buildDeepSeekCardLink(fallbackText, cardType string, payload map[string]any) string {
	return "[" + fallbackText + "](" + buildDeepSeekCardURI(cardType, payload) + ")"
}

func buildDeepSeekCardURI(cardType string, payload map[string]any) string {
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
		Path:     "/" + strings.TrimSpace(cardType),
		RawQuery: values.Encode(),
	}).String()
}
