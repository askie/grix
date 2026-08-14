package grixcards

import (
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/agentadapter/internal/util"
)

// NormalizeBizCard rewrites known grix biz_card payloads into markdown card links.
// It returns ok=false when the payload does not contain a supported biz_card.
func NormalizeBizCard(p *agentadapter.InboundSendMsgPayload) (normalizedContent string, normalizedExtra json.RawMessage, ok bool) {
	if strings.Contains(p.Content, "grix://card/") {
		return p.Content, util.CloneRawMessage(p.Extra), true
	}

	bizCard := util.DecodeJSONObject(p.BizCard)
	if len(bizCard) == 0 {
		return "", nil, false
	}

	cardContent, matched := buildGrixCardContentFromBizCard(bizCard)
	if !matched {
		return "", nil, false
	}
	return cardContent, util.CloneRawMessage(p.Extra), true
}

// Normalize rewrites known structured grix card payloads into markdown card links.
// It returns ok=false when the payload does not match a supported card shape.
func Normalize(p *agentadapter.InboundSendMsgPayload) (normalizedContent string, normalizedExtra json.RawMessage, ok bool) {
	if strings.Contains(p.Content, "grix://card/") {
		return p.Content, util.CloneRawMessage(p.Extra), true
	}

	if len(p.ChannelData) > 0 || len(p.BizCard) > 0 {
		if cardContent, matched := buildStructuredGrixCardContent(p.ChannelData, p.BizCard); matched {
			return cardContent, util.CloneRawMessage(p.Extra), true
		}
	}

	return "", nil, false
}

func buildStructuredGrixCardContent(channelDataRaw, bizCardRaw json.RawMessage) (string, bool) {
	channelData := util.DecodeJSONObject(channelDataRaw)
	if len(channelData) > 0 {
		if cardContent, matched := buildEggInstallStatusCardContent(channelData); matched {
			return cardContent, true
		}
		if cardContent, matched := buildUserProfileCardContent(channelData); matched {
			return cardContent, true
		}
		if cardContent, matched := buildConversationCardContent(channelData); matched {
			return cardContent, true
		}
		if cardContent, matched := buildToolExecutionCardContent(channelData); matched {
			return cardContent, true
		}
		if cardContent, matched := buildThinkingCardContent(channelData); matched {
			return cardContent, true
		}
	}

	bizCard := util.DecodeJSONObject(bizCardRaw)
	if len(bizCard) == 0 {
		return "", false
	}
	return buildGrixCardContentFromBizCard(bizCard)
}

func buildGrixCardContentFromBizCard(bizCard map[string]any) (string, bool) {
	cardType := util.NormalizeText(bizCard["type"])
	payload := util.NestedObject(bizCard, "payload")
	if cardType == "" || len(payload) == 0 {
		return "", false
	}

	switch cardType {
	case "egg_install_status":
		return buildEggInstallStatusCardContentFromPayload(payload)
	case "user_profile":
		return buildUserProfileCardContentFromPayload(payload)
	case "conversation":
		return buildConversationCardContentFromPayload(payload)
	case "tool_execution":
		return buildToolExecutionCardContentFromPayload(payload)
	case "thinking":
		return buildThinkingCardContentFromPayload(payload)
	default:
		return "", false
	}
}

func buildEggInstallStatusCardContent(channelData map[string]any) (string, bool) {
	record := util.NestedObject(util.NestedObject(channelData, "grix"), "eggInstall")
	if len(record) == 0 {
		return "", false
	}

	installID := util.NormalizeText(record["install_id"])
	status := util.NormalizeText(record["status"])
	if installID == "" || !isOneOf(status, "running", "success", "failed") {
		return "", false
	}

	payload := map[string]any{
		"install_id": installID,
		"status":     status,
	}
	if step := util.NormalizeText(record["step"]); step != "" {
		payload["step"] = step
	}
	summary := util.NormalizeText(record["summary"])
	if summary == "" {
		summary = defaultEggInstallSummary(status, util.NormalizeText(record["step"]))
	}
	payload["summary"] = summary
	if detailText := util.NormalizeText(record["detail_text"]); detailText != "" {
		payload["detail_text"] = detailText
	}
	if targetAgentID := util.NormalizeText(record["target_agent_id"]); targetAgentID != "" {
		payload["target_agent_id"] = targetAgentID
	}
	if errorCode := util.NormalizeText(record["error_code"]); errorCode != "" {
		payload["error_code"] = errorCode
	}
	if errorMsg := util.NormalizeText(record["error_msg"]); errorMsg != "" {
		payload["error_msg"] = errorMsg
	}

	return buildEggInstallStatusCardContentFromPayload(payload)
}

func buildEggInstallStatusCardContentFromPayload(payload map[string]any) (string, bool) {
	installID := util.NormalizeText(payload["install_id"])
	status := util.NormalizeText(payload["status"])
	if installID == "" || !isOneOf(status, "running", "success", "failed") {
		return "", false
	}

	normalizedPayload := map[string]any{
		"install_id": installID,
		"status":     status,
	}
	if step := util.NormalizeText(payload["step"]); step != "" {
		normalizedPayload["step"] = step
	}
	summary := util.NormalizeText(payload["summary"])
	if summary == "" {
		summary = defaultEggInstallSummary(status, util.NormalizeText(payload["step"]))
	}
	normalizedPayload["summary"] = summary
	if detailText := util.NormalizeText(payload["detail_text"]); detailText != "" {
		normalizedPayload["detail_text"] = detailText
	}
	if targetAgentID := util.NormalizeText(payload["target_agent_id"]); targetAgentID != "" {
		normalizedPayload["target_agent_id"] = targetAgentID
	}
	if errorCode := util.NormalizeText(payload["error_code"]); errorCode != "" {
		normalizedPayload["error_code"] = errorCode
	}
	if errorMsg := util.NormalizeText(payload["error_msg"]); errorMsg != "" {
		normalizedPayload["error_msg"] = errorMsg
	}

	return buildGrixCardLink(
		fmt.Sprintf("[Egg Install] %s", compactText(summary, 180)),
		"egg_install_status",
		normalizedPayload,
	), true
}

func buildUserProfileCardContent(channelData map[string]any) (string, bool) {
	record := util.NestedObject(util.NestedObject(channelData, "grix"), "userProfile")
	if len(record) == 0 {
		return "", false
	}

	userID := util.NormalizeText(record["user_id"])
	nickname := util.NormalizeText(record["nickname"])
	if userID == "" || nickname == "" {
		return "", false
	}

	payload := map[string]any{
		"user_id":  userID,
		"nickname": nickname,
	}
	if peerType, ok := normalizePeerType(record["peer_type"]); ok {
		payload["peer_type"] = peerType
	} else {
		payload["peer_type"] = 1
	}
	if avatarURL := util.NormalizeText(record["avatar_url"]); avatarURL != "" {
		payload["avatar_url"] = avatarURL
	}

	return buildUserProfileCardContentFromPayload(payload)
}

func buildUserProfileCardContentFromPayload(payload map[string]any) (string, bool) {
	userID := util.NormalizeText(payload["user_id"])
	nickname := util.NormalizeText(payload["nickname"])
	if userID == "" || nickname == "" {
		return "", false
	}

	normalizedPayload := map[string]any{
		"user_id":  userID,
		"nickname": nickname,
	}
	if peerType, ok := normalizePeerType(payload["peer_type"]); ok {
		normalizedPayload["peer_type"] = peerType
	} else {
		normalizedPayload["peer_type"] = 1
	}
	if avatarURL := util.NormalizeText(payload["avatar_url"]); avatarURL != "" {
		normalizedPayload["avatar_url"] = avatarURL
	}

	return buildGrixCardLink(
		fmt.Sprintf("[Profile Card] %s", compactText(nickname, 120)),
		"user_profile",
		normalizedPayload,
	), true
}

func buildConversationCardContent(channelData map[string]any) (string, bool) {
	record := util.NestedObject(util.NestedObject(channelData, "grix"), "conversation")
	if len(record) == 0 {
		return "", false
	}

	payload := map[string]any{
		"session_id":   util.NormalizeText(record["session_id"]),
		"session_type": util.NormalizeText(record["session_type"]),
		"title":        util.NormalizeText(record["title"]),
	}
	if peerID := util.NormalizeText(record["peer_id"]); peerID != "" {
		payload["peer_id"] = peerID
	}
	if peerNickname := util.NormalizeText(record["peer_nickname"]); peerNickname != "" {
		payload["peer_nickname"] = peerNickname
	}
	if avatarURL := util.NormalizeText(record["avatar_url"]); avatarURL != "" {
		payload["avatar_url"] = avatarURL
	}

	return buildConversationCardContentFromPayload(payload)
}

func buildConversationCardContentFromPayload(payload map[string]any) (string, bool) {
	sessionID := util.NormalizeText(payload["session_id"])
	sessionType := strings.ToLower(util.NormalizeText(payload["session_type"]))
	title := util.NormalizeText(payload["title"])
	if sessionID == "" || !isOneOf(sessionType, "group", "private") || title == "" {
		return "", false
	}

	normalizedPayload := map[string]any{
		"session_id":   sessionID,
		"session_type": sessionType,
		"title":        title,
	}
	if peerID := util.NormalizeText(payload["peer_id"]); peerID != "" {
		normalizedPayload["peer_id"] = peerID
	}
	if peerNickname := util.NormalizeText(payload["peer_nickname"]); peerNickname != "" {
		normalizedPayload["peer_nickname"] = peerNickname
	}
	if avatarURL := util.NormalizeText(payload["avatar_url"]); avatarURL != "" {
		normalizedPayload["avatar_url"] = avatarURL
	}

	return buildGrixCardLink(
		fmt.Sprintf("[Conversation] %s", compactText(title, 120)),
		"conversation",
		normalizedPayload,
	), true
}

func buildToolExecutionCardContent(channelData map[string]any) (string, bool) {
	record := util.NestedObject(util.NestedObject(channelData, "grix"), "toolExecution")
	if len(record) == 0 {
		return "", false
	}

	summary := util.NormalizeText(record["summary_text"])
	if summary == "" {
		return "", false
	}

	payload := map[string]any{
		"summary_text": summary,
	}
	if detailText := util.NormalizeText(record["detail_text"]); detailText != "" {
		payload["detail_text"] = detailText
	}

	return buildToolExecutionCardContentFromPayload(payload)
}

func buildToolExecutionCardContentFromPayload(payload map[string]any) (string, bool) {
	summary := util.NormalizeText(payload["summary_text"])
	if summary == "" {
		return "", false
	}

	normalizedPayload := map[string]any{
		"summary_text": summary,
	}
	if detailText := util.NormalizeText(payload["detail_text"]); detailText != "" {
		normalizedPayload["detail_text"] = detailText
	}

	return buildGrixCardLink(
		fmt.Sprintf("[Tool] %s", compactText(summary, 180)),
		"tool_execution",
		normalizedPayload,
	), true
}

func buildThinkingCardContent(channelData map[string]any) (string, bool) {
	record := util.NestedObject(util.NestedObject(channelData, "grix"), "thinking")
	if len(record) == 0 {
		return "", false
	}

	content := util.NormalizeText(record["content"])
	if content == "" {
		return "", false
	}

	return buildThinkingCardContentFromPayload(map[string]any{"content": content})
}

func buildThinkingCardContentFromPayload(payload map[string]any) (string, bool) {
	content := util.NormalizeText(payload["content"])
	if content == "" {
		return "", false
	}

	normalizedPayload := map[string]any{"content": content}

	return buildGrixCardLink(
		"[Thinking]",
		"thinking",
		normalizedPayload,
	), true
}

func buildGrixCardLink(fallbackText, cardType string, payload map[string]any) string {
	return "[" + fallbackText + "](" + buildGrixCardURI(cardType, payload) + ")"
}

func buildGrixCardURI(cardType string, payload map[string]any) string {
	values := url.Values{}
	if hasComplexPayload(payload) {
		data, _ := json.Marshal(payload)
		values.Set("d", string(data))
	} else {
		for key, value := range payload {
			switch typed := value.(type) {
			case nil:
				continue
			case string:
				if typed == "" {
					continue
				}
				values.Set(key, typed)
			default:
				values.Set(key, fmt.Sprint(value))
			}
		}
	}

	return (&url.URL{
		Scheme:   "grix",
		Host:     "card",
		Path:     "/" + strings.TrimSpace(cardType),
		RawQuery: values.Encode(),
	}).String()
}

func hasComplexPayload(payload map[string]any) bool {
	for _, value := range payload {
		if value == nil {
			continue
		}
		switch reflect.ValueOf(value).Kind() {
		case reflect.Map, reflect.Slice, reflect.Array:
			return true
		}
	}
	return false
}

func defaultEggInstallSummary(status, step string) string {
	switch status {
	case "running":
		if step != "" {
			return "Installation in progress: " + step
		}
		return "Installation in progress"
	case "success":
		if step != "" {
			return "Installation completed: " + step
		}
		return "Installation completed"
	case "failed":
		if step != "" {
			return "Installation failed: " + step
		}
		return "Installation failed"
	default:
		return "Installation status updated"
	}
}

func compactText(text string, limit int) string {
	compact := strings.Join(strings.Fields(util.NormalizeText(text)), " ")
	if limit <= 3 || len(compact) <= limit {
		return compact
	}
	return compact[:limit-3] + "..."
}

func normalizePeerType(value any) (int, bool) {
	switch util.NormalizeText(value) {
	case "1":
		return 1, true
	case "2":
		return 2, true
	default:
		return 0, false
	}
}

func isOneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
