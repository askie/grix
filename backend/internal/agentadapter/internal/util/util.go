package util

import (
	"encoding/json"
	"fmt"
	"strings"
)

// NormalizeText converts any value to a trimmed string, normalizing \r\n to \n.
// Handles json.Number and falls back to fmt.Sprint for other types.
func NormalizeText(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(strings.ReplaceAll(typed, "\r\n", "\n"))
	case json.Number:
		return strings.TrimSpace(typed.String())
	default:
		return strings.TrimSpace(strings.ReplaceAll(fmt.Sprint(value), "\r\n", "\n"))
	}
}

// NestedObject extracts a nested map[string]any from a parent map by key.
// Returns nil if the key doesn't exist or the value isn't a map.
func NestedObject(parent map[string]any, key string) map[string]any {
	return AsJSONObject(parent[key])
}

// DecodeJSONObject parses a json.RawMessage into a map[string]any.
// Returns nil if the input is empty or invalid JSON.
func DecodeJSONObject(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var record map[string]any
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil
	}
	return record
}

// CloneRawMessage creates a copy of a json.RawMessage.
// Returns nil if the input is nil or empty.
func CloneRawMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

// FirstNonEmpty returns the first non-empty (after trimming whitespace) string
// from the variadic arguments. Returns "" if all are empty.
func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// CloneJSONObject creates a deep copy of a map[string]any via JSON round-trip.
// Returns nil if the input is nil or empty.
func CloneJSONObject(record map[string]any) map[string]any {
	if len(record) == 0 {
		return nil
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return map[string]any{}
	}
	cloned := map[string]any{}
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return map[string]any{}
	}
	return cloned
}

// AsJSONObject type-asserts a value to map[string]any.
// Returns nil if the value isn't a map.
func AsJSONObject(value any) map[string]any {
	record, _ := value.(map[string]any)
	return record
}
