package grixactions

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const (
	questionReplyType       = "agent_question_reply"
	openSessionActionHost   = "open"
	openSessionActionPath   = "session"
	legacyQuestionPrefix    = "/grix question"
	legacyOpenSessionPrefix = "/grix open"
	questionAcceptAction    = "accept"
	questionCancelAction    = "cancel"
	legacyAcceptToken       = "__grix_accept__"
	legacyCancelToken       = "__grix_cancel__"
)

type QuestionReply struct {
	RequestID string
	Action    string
	Response  map[string]any
}

type OpenSessionSubmit struct {
	Cwd            string
	CardInstanceID string
}

func ParseQuestionReply(raw string) (QuestionReply, bool, error) {
	parsed, matched, err := parseActionURI(raw, questionReplyType)
	if !matched || err != nil {
		return QuestionReply{}, matched, err
	}
	payload, err := decodeJSONPayload(parsed)
	if err != nil {
		return QuestionReply{}, true, err
	}
	requestID := normalizeOptionalString(payload["request_id"])
	if requestID == "" {
		return QuestionReply{}, true, fmt.Errorf("request_id required")
	}
	action := normalizeOptionalString(payload["action"])
	if action != "" {
		if action != questionAcceptAction && action != questionCancelAction {
			return QuestionReply{}, true, fmt.Errorf("unsupported action")
		}
		return QuestionReply{RequestID: requestID, Action: action}, true, nil
	}

	response, ok := payload["response"].(map[string]any)
	if !ok || !isValidQuestionResponse(response) {
		return QuestionReply{}, true, fmt.Errorf("response required")
	}
	return QuestionReply{
		RequestID: requestID,
		Response:  response,
	}, true, nil
}

func ParseOpenSessionSubmit(raw string) (OpenSessionSubmit, bool, error) {
	parsed, matched, err := parseOpenSessionURI(raw)
	if !matched || err != nil {
		return OpenSessionSubmit{}, matched, err
	}
	cwd := strings.TrimSpace(parsed.Query().Get("cwd"))
	if cwd == "" {
		return OpenSessionSubmit{}, true, fmt.Errorf("cwd required")
	}
	return OpenSessionSubmit{
		Cwd:            cwd,
		CardInstanceID: strings.TrimSpace(parsed.Query().Get("card_instance_id")),
	}, true, nil
}

func BuildQuestionReplyURI(reply QuestionReply) string {
	payload := map[string]any{
		"request_id": strings.TrimSpace(reply.RequestID),
	}
	if action := strings.TrimSpace(reply.Action); action != "" {
		payload["action"] = action
	}
	if reply.Response != nil {
		payload["response"] = reply.Response
	}
	data, _ := json.Marshal(payload)
	return (&url.URL{
		Scheme:   "grix",
		Host:     "card",
		Path:     "/" + questionReplyType,
		RawQuery: url.Values{"d": []string{string(data)}}.Encode(),
	}).String()
}

func BuildOpenSessionSubmitURI(submit OpenSessionSubmit) string {
	query := url.Values{"cwd": []string{strings.TrimSpace(submit.Cwd)}}
	if cardInstanceID := strings.TrimSpace(submit.CardInstanceID); cardInstanceID != "" {
		query.Set("card_instance_id", cardInstanceID)
	}
	return (&url.URL{
		Scheme:   "grix",
		Host:     openSessionActionHost,
		Path:     "/" + openSessionActionPath,
		RawQuery: query.Encode(),
	}).String()
}

func LegacyQuestionCommand(reply QuestionReply) string {
	requestID := strings.TrimSpace(reply.RequestID)
	if requestID == "" {
		return ""
	}
	if reply.Action != "" {
		token := ""
		switch reply.Action {
		case questionAcceptAction:
			token = legacyAcceptToken
		case questionCancelAction:
			token = legacyCancelToken
		}
		if token == "" {
			return ""
		}
		return fmt.Sprintf("%s %s %s", legacyQuestionPrefix, requestID, token)
	}
	responseType := strings.TrimSpace(fmt.Sprint(reply.Response["type"]))
	switch responseType {
	case "single":
		value := strings.TrimSpace(fmt.Sprint(reply.Response["value"]))
		if value == "" {
			return ""
		}
		return fmt.Sprintf("%s %s %s", legacyQuestionPrefix, requestID, value)
	case "map":
		rawEntries, ok := reply.Response["entries"].([]any)
		if !ok || len(rawEntries) == 0 {
			return ""
		}
		segments := make([]string, 0, len(rawEntries))
		for _, rawEntry := range rawEntries {
			entry, ok := rawEntry.(map[string]any)
			if !ok {
				return ""
			}
			key := strings.TrimSpace(fmt.Sprint(entry["key"]))
			value := strings.TrimSpace(fmt.Sprint(entry["value"]))
			if key == "" || value == "" {
				return ""
			}
			segments = append(segments, fmt.Sprintf("%s=%s", key, value))
		}
		if len(segments) == 0 {
			return ""
		}
		return fmt.Sprintf("%s %s %s", legacyQuestionPrefix, requestID, strings.Join(segments, "; "))
	default:
		return ""
	}
}

func LegacyOpenSessionCommand(submit OpenSessionSubmit) string {
	cwd := strings.TrimSpace(submit.Cwd)
	if cwd == "" {
		return ""
	}
	return fmt.Sprintf("%s %s", legacyOpenSessionPrefix, cwd)
}

func RewriteToLegacyCommand(raw string) string {
	if reply, matched, err := ParseQuestionReply(raw); matched && err == nil {
		if command := LegacyQuestionCommand(reply); command != "" {
			return command
		}
	}
	if submit, matched, err := ParseOpenSessionSubmit(raw); matched && err == nil {
		if command := LegacyOpenSessionCommand(submit); command != "" {
			return command
		}
	}
	return raw
}

func parseActionURI(raw string, expectedType string) (*url.URL, bool, error) {
	return parseLegacyCardActionURI(raw, expectedType)
}

func parseLegacyCardActionURI(raw string, expectedType string) (*url.URL, bool, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, false, nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, false, nil
	}
	if !strings.EqualFold(parsed.Scheme, "grix") ||
		!strings.EqualFold(parsed.Host, "card") {
		return nil, false, nil
	}
	actionType := strings.Trim(strings.TrimSpace(parsed.Path), "/")
	if actionType != expectedType {
		return nil, false, nil
	}
	return parsed, true, nil
}

func parseOpenSessionURI(raw string) (*url.URL, bool, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, false, nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, false, nil
	}
	if !strings.EqualFold(parsed.Scheme, "grix") ||
		!strings.EqualFold(parsed.Host, openSessionActionHost) {
		return nil, false, nil
	}
	actionPath := strings.Trim(strings.TrimSpace(parsed.Path), "/")
	if actionPath != openSessionActionPath {
		return nil, false, nil
	}
	return parsed, true, nil
}

func decodeJSONPayload(parsed *url.URL) (map[string]any, error) {
	if parsed == nil {
		return nil, fmt.Errorf("uri required")
	}
	raw := strings.TrimSpace(parsed.Query().Get("d"))
	if raw == "" {
		return nil, fmt.Errorf("d required")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func isValidQuestionResponse(response map[string]any) bool {
	responseType := normalizeOptionalString(response["type"])
	switch responseType {
	case "single":
		return normalizeOptionalString(response["value"]) != ""
	case "map":
		rawEntries, ok := response["entries"].([]any)
		if !ok || len(rawEntries) == 0 {
			return false
		}
		for _, rawEntry := range rawEntries {
			entry, ok := rawEntry.(map[string]any)
			if !ok {
				return false
			}
			key := normalizeOptionalString(entry["key"])
			value := normalizeOptionalString(entry["value"])
			if key == "" || value == "" {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func normalizeOptionalString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
