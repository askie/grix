package handler

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/datatypes"
)

const defaultDelegateMaxConsecutiveReplies = 10
const maxDelegateMaxConsecutiveReplies = 50

func normalizeDelegateMaxConsecutiveReplies(v int) int {
	if v <= 0 {
		return defaultDelegateMaxConsecutiveReplies
	}
	if v > maxDelegateMaxConsecutiveReplies {
		return maxDelegateMaxConsecutiveReplies
	}
	return v
}

func delegateMaxRepliesFromAgentConfig(raw datatypes.JSON) int {
	if len(raw) == 0 {
		return defaultDelegateMaxConsecutiveReplies
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return defaultDelegateMaxConsecutiveReplies
	}

	for _, key := range []string{
		"delegate_max_consecutive_replies",
		"max_consecutive_replies",
		"auto_reply_max_consecutive",
	} {
		if v, ok := cfg[key]; ok {
			if n, ok := parseIntFromAny(v); ok {
				return normalizeDelegateMaxConsecutiveReplies(n)
			}
		}
	}
	return defaultDelegateMaxConsecutiveReplies
}

func delegateMaxRepliesFromRedis(raw any) int {
	if n, ok := parseIntFromAny(raw); ok {
		return normalizeDelegateMaxConsecutiveReplies(n)
	}
	return defaultDelegateMaxConsecutiveReplies
}

func delegateStreakKey(sessionID string, ownerID int64) string {
	return fmt.Sprintf("im:delegate:streak:%s:%d", sessionID, ownerID)
}

func parseIntFromAny(v any) (int, bool) {
	switch x := v.(type) {
	case nil:
		return 0, false
	case int:
		return x, true
	case int8:
		return int(x), true
	case int16:
		return int(x), true
	case int32:
		return int(x), true
	case int64:
		return int(x), true
	case uint:
		return int(x), true
	case uint8:
		return int(x), true
	case uint16:
		return int(x), true
	case uint32:
		return int(x), true
	case uint64:
		return int(x), true
	case float32:
		return int(x), true
	case float64:
		return int(x), true
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return int(n), true
		}
	case string:
		s := strings.TrimSpace(x)
		if s == "" || s == "<nil>" {
			return 0, false
		}
		n, err := strconv.Atoi(s)
		if err == nil {
			return n, true
		}
	default:
		s := strings.TrimSpace(fmt.Sprint(x))
		if s == "" || s == "<nil>" {
			return 0, false
		}
		n, err := strconv.Atoi(s)
		if err == nil {
			return n, true
		}
	}
	return 0, false
}
