package mention

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

const mentionUserIDsKey = "mention_user_ids"

type Candidate struct {
	UserID  int64
	Aliases []string
}

func ShouldNormalize(extraRaw json.RawMessage, content string, implicitUserIDs ...int64) bool {
	if strings.Contains(content, "@") {
		return true
	}
	if hasPositiveInt64(implicitUserIDs) {
		return true
	}
	if len(extraRaw) == 0 {
		return false
	}
	return bytes.Contains(extraRaw, []byte(mentionUserIDsKey))
}

func ContainsMentionToken(content string, token string) bool {
	normalizedToken := normalizeAlias(token)
	if normalizedToken == "" || content == "" {
		return false
	}

	for _, mentionToken := range extractMentionTokens(content) {
		if normalizeAlias(mentionToken) == normalizedToken {
			return true
		}
	}
	return false
}

// ParseUserIDs resolves explicit mention user IDs from structured extra first,
// then applies plain-text "@123" fallback on content. Implicit mention targets
// such as quoted-message owners are only used when no explicit mention was
// found in the message.
func ParseUserIDs(extraRaw json.RawMessage, content string, implicitUserIDs ...int64) []int64 {
	return parseUserIDs(extraRaw, content, nil, implicitUserIDs)
}

// ParseUserIDsWithCandidates resolves mention user IDs using:
// 1) structured extra.mention_user_ids
// 2) plain-text numeric fallback (@123)
// 3) plain-text alias fallback (@username/@nickname), based on candidates
// 4) implicit mention targets such as quoted-message owners, but only when no
// explicit mention is resolved from steps 1-3
func ParseUserIDsWithCandidates(
	extraRaw json.RawMessage,
	content string,
	candidates []Candidate,
	implicitUserIDs ...int64,
) []int64 {
	return parseUserIDs(extraRaw, content, candidates, implicitUserIDs)
}

func parseUserIDs(
	extraRaw json.RawMessage,
	content string,
	candidates []Candidate,
	implicitUserIDs []int64,
) []int64 {
	explicitMentions := make([]int64, 0, 4)

	if len(extraRaw) > 0 {
		var extra map[string]any
		if err := json.Unmarshal(extraRaw, &extra); err == nil {
			explicitMentions = append(explicitMentions, parseMentionIDsFromAny(extra[mentionUserIDsKey])...)
		}
	}
	explicitMentions = append(explicitMentions, parseMentionIDsFromContent(content)...)
	if len(candidates) > 0 {
		explicitMentions = append(explicitMentions, parseMentionIDsFromAliases(content, candidates)...)
	}

	explicitMentions = dedupePositiveInt64(explicitMentions)
	if len(explicitMentions) > 0 {
		return explicitMentions
	}
	return dedupePositiveInt64(implicitUserIDs)
}

// NormalizeExtra returns a normalized extra payload where mention_user_ids is
// written in canonical int64[] form when any mention is detected.
func NormalizeExtra(extraRaw json.RawMessage, content string, implicitUserIDs ...int64) json.RawMessage {
	return normalizeExtraWithMentions(extraRaw, ParseUserIDs(extraRaw, content, implicitUserIDs...))
}

// NormalizeExtraWithCandidates writes canonical mention_user_ids after applying
// structured + numeric + alias mention discovery. Implicit mentions such as
// quoted-message owners are only written when the message itself has no
// explicit mention target.
func NormalizeExtraWithCandidates(
	extraRaw json.RawMessage,
	content string,
	candidates []Candidate,
	implicitUserIDs ...int64,
) json.RawMessage {
	mentions := ParseUserIDsWithCandidates(extraRaw, content, candidates, implicitUserIDs...)
	return normalizeExtraWithMentions(extraRaw, mentions)
}

// RemoveMentionUserIDs strips mention_user_ids from extra while preserving
// other fields.
func RemoveMentionUserIDs(extraRaw json.RawMessage) json.RawMessage {
	if len(extraRaw) == 0 || !bytes.Contains(extraRaw, []byte(mentionUserIDsKey)) {
		return cloneRaw(extraRaw)
	}

	var extra map[string]any
	if err := json.Unmarshal(extraRaw, &extra); err != nil {
		return cloneRaw(extraRaw)
	}
	delete(extra, mentionUserIDsKey)
	if len(extra) == 0 {
		return nil
	}

	merged, err := json.Marshal(extra)
	if err != nil {
		return cloneRaw(extraRaw)
	}
	return json.RawMessage(merged)
}

func int64SliceToStringSlice(ids []int64) []string {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = strconv.FormatInt(id, 10)
	}
	return s
}

func normalizeExtraWithMentions(extraRaw json.RawMessage, mentions []int64) json.RawMessage {
	if len(mentions) == 0 {
		return cloneRaw(extraRaw)
	}

	extra := make(map[string]any, 4)
	if len(extraRaw) > 0 {
		var incoming map[string]any
		if err := json.Unmarshal(extraRaw, &incoming); err == nil {
			for k, v := range incoming {
				extra[k] = v
			}
		}
	}
	extra[mentionUserIDsKey] = int64SliceToStringSlice(mentions)

	merged, err := json.Marshal(extra)
	if err != nil {
		return cloneRaw(extraRaw)
	}
	return json.RawMessage(merged)
}

func parseMentionIDsFromAliases(content string, candidates []Candidate) []int64 {
	if len(content) == 0 || len(candidates) == 0 {
		return nil
	}
	aliasIndex := buildAliasIndex(candidates)
	if len(aliasIndex) == 0 {
		return nil
	}

	tokens := extractMentionTokens(content)
	if len(tokens) == 0 {
		return nil
	}

	mentions := make([]int64, 0, len(tokens))
	for _, token := range tokens {
		key := normalizeAlias(token)
		if key == "" {
			continue
		}
		if id, ok := aliasIndex[key]; ok {
			mentions = append(mentions, id)
		}
	}
	return mentions
}

func buildAliasIndex(candidates []Candidate) map[string]int64 {
	if len(candidates) == 0 {
		return nil
	}
	index := make(map[string]int64, len(candidates)*2)
	ambiguous := make(map[string]bool)

	for _, c := range candidates {
		if c.UserID <= 0 {
			continue
		}
		for _, alias := range c.Aliases {
			key := normalizeAlias(alias)
			if key == "" {
				continue
			}
			if ambiguous[key] {
				continue
			}
			if existing, ok := index[key]; ok && existing != c.UserID {
				delete(index, key)
				ambiguous[key] = true
				continue
			}
			index[key] = c.UserID
		}
	}
	return index
}

func normalizeAlias(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "@")
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return strings.ToLower(s)
}

func extractMentionTokens(content string) []string {
	if content == "" {
		return nil
	}
	runes := []rune(content)
	tokens := make([]string, 0, 4)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '@' {
			continue
		}
		if !isMentionStart(runes, i) {
			continue
		}

		j := i + 1
		for j < len(runes) && isMentionTokenRune(runes[j]) {
			j++
		}
		if j <= i+1 {
			continue
		}
		token := strings.TrimSpace(string(runes[i+1 : j]))
		if token == "" {
			continue
		}
		tokens = append(tokens, token)
	}
	return tokens
}

func isMentionStart(runes []rune, at int) bool {
	if at <= 0 {
		return true
	}
	prev := runes[at-1]
	if isASCIIWordRune(prev) {
		return false
	}
	switch prev {
	case '.', '_', '+', '-':
		return false
	}
	return true
}

func isMentionTokenRune(r rune) bool {
	if unicode.IsLetter(r) || unicode.IsNumber(r) {
		return true
	}
	switch r {
	case '_', '-', '.', '+':
		return true
	}
	return false
}

func isASCIIWordRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

func parseMentionIDsFromAny(v any) []int64 {
	mentions := make([]int64, 0, 4)
	switch list := v.(type) {
	case []any:
		for _, item := range list {
			if id, ok := parseInt64FromAny(item); ok {
				mentions = append(mentions, id)
			}
		}
	case []int64:
		mentions = append(mentions, list...)
	case []int:
		for _, n := range list {
			mentions = append(mentions, int64(n))
		}
	case []string:
		for _, s := range list {
			if id, ok := parseInt64FromAny(s); ok {
				mentions = append(mentions, id)
			}
		}
	}
	return mentions
}

func parseMentionIDsFromContent(content string) []int64 {
	if len(content) <= 1 {
		return nil
	}
	mentions := make([]int64, 0, 4)
	for i := 0; i < len(content); i++ {
		if content[i] != '@' {
			continue
		}
		j := i + 1
		for j < len(content) && content[j] >= '0' && content[j] <= '9' {
			j++
		}
		if j > i+1 {
			if id, err := strconv.ParseInt(content[i+1:j], 10, 64); err == nil {
				mentions = append(mentions, id)
			}
		}
	}
	return mentions
}

func dedupePositiveInt64(src []int64) []int64 {
	if len(src) == 0 {
		return nil
	}
	uniq := make([]int64, 0, len(src))
	seen := make(map[int64]bool, len(src))
	for _, id := range src {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return nil
	}
	return uniq
}

func hasPositiveInt64(src []int64) bool {
	for _, id := range src {
		if id > 0 {
			return true
		}
	}
	return false
}

func parseInt64FromAny(v any) (int64, bool) {
	switch x := v.(type) {
	case nil:
		return 0, false
	case int:
		return int64(x), true
	case int8:
		return int64(x), true
	case int16:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	case uint:
		return int64(x), true
	case uint8:
		return int64(x), true
	case uint16:
		return int64(x), true
	case uint32:
		return int64(x), true
	case uint64:
		return int64(x), true
	case float32:
		return int64(x), true
	case float64:
		return int64(x), true
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return n, true
		}
	case string:
		s := strings.TrimSpace(x)
		if s == "" || s == "<nil>" {
			return 0, false
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n, true
		}
	default:
		s := strings.TrimSpace(fmt.Sprint(x))
		if s == "" || s == "<nil>" {
			return 0, false
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n, true
		}
	}
	return 0, false
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	out := make([]byte, len(raw))
	copy(out, raw)
	return json.RawMessage(out)
}
