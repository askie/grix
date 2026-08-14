package gemini

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/askie/grix/backend/internal/agentadapter/internal/util"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

type SessionConfig struct {
	Cwd     string
	ModeID  string
	ModelID string
}

func ExtractSessionConfig(extra json.RawMessage) SessionConfig {
	return extractSessionConfigFromScopes(collectOutboundScopes(extra))
}

func AppendPromptText(extra json.RawMessage, content string, contextMessages []protocol.ContextMessagePayload, text string) json.RawMessage {
	supplement := strings.TrimSpace(text)
	if supplement == "" {
		return util.CloneRawMessage(extra)
	}

	base := decodeOutboundExtraObject(extra)
	if len(base) == 0 {
		base = map[string]any{}
	}

	scopes := collectOutboundScopes(extra)
	prompt := firstObjectArray(scopes, "prompt")
	if len(prompt) == 0 {
		if fallback := buildFallbackPromptText(content, contextMessages); fallback != "" {
			prompt = []map[string]any{
				{
					"type": "text",
					"text": fallback,
				},
			}
		}
	}
	prompt = append(prompt, map[string]any{
		"type": "text",
		"text": supplement,
	})

	acp := mapValue(base["acp"])
	if len(acp) == 0 {
		acp = map[string]any{}
	}
	acp["prompt"] = prompt
	base["acp"] = acp

	encoded, err := json.Marshal(base)
	if err != nil {
		return util.CloneRawMessage(extra)
	}
	return encoded
}

func MergeSessionConfig(extra json.RawMessage, config SessionConfig) json.RawMessage {
	base := decodeOutboundExtraObject(extra)
	if len(base) == 0 {
		base = map[string]any{}
	}

	acp := mapValue(base["acp"])
	if len(acp) == 0 {
		acp = map[string]any{}
	}

	if cwd := strings.TrimSpace(config.Cwd); cwd != "" {
		acp["cwd"] = cwd
	}
	if modeID := strings.TrimSpace(config.ModeID); modeID != "" {
		acp["mode_id"] = modeID
	}
	if modelID := strings.TrimSpace(config.ModelID); modelID != "" {
		acp["model_id"] = modelID
	}

	if len(acp) > 0 {
		base["acp"] = acp
	}

	encoded, err := json.Marshal(base)
	if err != nil {
		return util.CloneRawMessage(extra)
	}
	return encoded
}

func collectOutboundScopes(extra json.RawMessage) []map[string]any {
	base := decodeOutboundExtraObject(extra)
	if len(base) == 0 {
		return nil
	}

	scopes := []map[string]any{base}
	if nested := mapValue(base["acp"]); len(nested) > 0 {
		scopes = append(scopes, nested)
	}
	if nested := mapValue(base["gemini_acp"]); len(nested) > 0 {
		scopes = append(scopes, nested)
	}
	return scopes
}

func extractSessionConfigFromScopes(scopes []map[string]any) SessionConfig {
	return SessionConfig{
		Cwd:     firstString(scopes, "cwd", "workdir", "working_directory"),
		ModeID:  firstString(scopes, "mode_id", "modeId"),
		ModelID: firstString(scopes, "model_id", "modelId"),
	}
}

func buildFallbackPromptText(content string, contextMessages []protocol.ContextMessagePayload) string {
	current := strings.TrimSpace(content)
	history := formatContextMessages(contextMessages)

	switch {
	case history == "" && current == "":
		return ""
	case history == "":
		return current
	case current == "":
		return "Conversation context:\n" + history
	default:
		return "Conversation context:\n" + history + "\n\nLatest user message:\n" + current
	}
}

func formatContextMessages(messages []protocol.ContextMessagePayload) string {
	if len(messages) == 0 {
		return ""
	}

	lines := make([]string, 0, len(messages))
	for _, item := range messages {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		role := "User"
		if item.SenderType == 2 {
			role = "Assistant"
		}
		lines = append(lines, role+": "+content)
	}
	return strings.Join(lines, "\n")
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

func mapValue(value any) map[string]any {
	object, _ := value.(map[string]any)
	return object
}

func firstString(scopes []map[string]any, keys ...string) string {
	for _, scope := range scopes {
		for _, key := range keys {
			value, _ := scope[key].(string)
			value = strings.TrimSpace(value)
			if value != "" {
				return value
			}
		}
	}
	return ""
}

func firstStringArray(scopes []map[string]any, keys ...string) []string {
	for _, scope := range scopes {
		for _, key := range keys {
			rawItems, ok := scope[key].([]any)
			if !ok {
				continue
			}
			items := make([]string, 0, len(rawItems))
			for _, raw := range rawItems {
				value, _ := raw.(string)
				value = strings.TrimSpace(value)
				if value == "" {
					continue
				}
				items = append(items, value)
			}
			if len(items) > 0 {
				return items
			}
		}
	}
	return nil
}

func firstObjectArray(scopes []map[string]any, keys ...string) []map[string]any {
	for _, scope := range scopes {
		for _, key := range keys {
			rawItems, ok := scope[key].([]any)
			if !ok {
				continue
			}
			items := make([]map[string]any, 0, len(rawItems))
			valid := true
			for _, raw := range rawItems {
				object, ok := raw.(map[string]any)
				if !ok {
					valid = false
					break
				}
				items = append(items, object)
			}
			if valid && len(items) > 0 {
				return items
			}
		}
	}
	return nil
}

func firstPositiveInt(scopes []map[string]any, keys ...string) int {
	for _, scope := range scopes {
		for _, key := range keys {
			switch value := scope[key].(type) {
			case float64:
				if value > 0 {
					return int(value)
				}
			case int:
				if value > 0 {
					return value
				}
			}
		}
	}
	return 0
}
