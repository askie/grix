package threadmeta

import (
	"encoding/json"
	"fmt"
	"strings"
)

const extraKeyThreadID = "thread_id"

func Extract(raw json.RawMessage) string {
	if len(raw) == 0 || !json.Valid(raw) {
		return ""
	}

	var extra map[string]any
	if err := json.Unmarshal(raw, &extra); err != nil {
		return ""
	}
	return normalize(extra[extraKeyThreadID])
}

func Merge(raw json.RawMessage, threadID string) json.RawMessage {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return clone(raw)
	}

	extra := make(map[string]any)
	if len(raw) > 0 && json.Valid(raw) {
		if err := json.Unmarshal(raw, &extra); err != nil {
			extra = make(map[string]any)
		}
	}
	extra[extraKeyThreadID] = threadID

	encoded, err := json.Marshal(extra)
	if err != nil {
		return clone(raw)
	}
	return encoded
}

func clone(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func normalize(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return strings.TrimSpace(typed.String())
	case float64:
		return strings.TrimSpace(fmt.Sprint(typed))
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}
