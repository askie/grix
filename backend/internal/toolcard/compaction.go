package toolcard

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	SummaryMaxRunes       = 500
	FailureDetailMaxBytes = 2000
	GroupMaxBytes         = 48 << 10
)

// Metadata is the bounded tool execution data retained by the server.
type Metadata struct {
	SummaryText string
	DetailText  string
	ToolCallID  string
	Failed      bool
}

// CompactHistoricalForStorage compacts both individual and already-aggregated
// tool cards. Runtime writes only call CompactForStorage for individual cards;
// the broader historical entry point also removes legacy raw fields left on
// group rows by older accumulator implementations.
func CompactHistoricalForStorage(
	content string,
	extra json.RawMessage,
) (string, json.RawMessage, bool) {
	if compactContent, compactExtra, _, ok := CompactForStorage(content, extra); ok {
		return compactContent, compactExtra, true
	}
	if !IsExecutionGroupCard(content) {
		return content, extra, false
	}
	compactContent, ok := compactExecutionGroupContent(content, isCompactedExtra(extra))
	if !ok {
		compactContent = content
	}
	return compactContent, compactExtra(extra), true
}

// CompactForStorage removes provider-specific and duplicated tool payloads
// before a tool execution message is persisted. It is intentionally
// idempotent so the same function can compact both new writes and historical
// rows during an upgrade.
func CompactForStorage(
	content string,
	extra json.RawMessage,
) (string, json.RawMessage, Metadata, bool) {
	if !IsExecutionCard(content) {
		return content, extra, Metadata{}, false
	}
	meta, ok := ExtractMetadata(content, extra)
	if !ok {
		return content, extra, Metadata{}, false
	}
	payload := map[string]any{"summary_text": meta.SummaryText}
	if meta.DetailText != "" {
		payload["detail_text"] = meta.DetailText
	}
	if meta.Failed {
		payload["failed"] = true
	}
	fallback := fmt.Sprintf("[Tool] %s", truncateRunes(meta.SummaryText, 180))
	compactContent := "[" + fallback + "](" + BuildExecutionCardURI(payload) + ")"
	return compactContent, compactExtra(extra), meta, true
}

func IsExecutionCard(content string) bool {
	return strings.Contains(content, "grix://card/tool_execution") &&
		!strings.Contains(content, "grix://card/tool_execution_group")
}

func IsExecutionGroupCard(content string) bool {
	return strings.Contains(content, "grix://card/tool_execution_group")
}

func ExtractMetadata(content string, extra json.RawMessage) (Metadata, bool) {
	idx := strings.Index(content, "grix://card/tool_execution?")
	if idx < 0 {
		return Metadata{}, false
	}
	uriStart := idx
	uriEnd := strings.IndexByte(content[uriStart:], ')')
	if uriEnd < 0 {
		uriEnd = len(content)
	} else {
		uriEnd = uriStart + uriEnd
	}
	uriStr := content[uriStart:uriEnd]

	u, err := url.Parse(uriStr)
	if err != nil {
		return Metadata{}, false
	}
	q := u.Query()
	meta := Metadata{
		SummaryText: q.Get("summary_text"),
		DetailText:  q.Get("detail_text"),
	}
	explicitFailed := false

	if d := q.Get("d"); d != "" {
		var payload map[string]any
		if err := json.Unmarshal([]byte(d), &payload); err == nil {
			if summary, _ := payload["summary_text"].(string); summary != "" {
				meta.SummaryText = summary
			}
			if detail, _ := payload["detail_text"].(string); detail != "" {
				meta.DetailText = detail
			}
			meta.ToolCallID = firstStringValue(payload, "tool_call_id", "toolCallId", "call_id", "callId")
			explicitFailed, _ = payload["failed"].(bool)
		}
	}
	if meta.ToolCallID == "" {
		meta.ToolCallID = findNestedStringValue(extra, "tool_call_id", "toolCallId", "call_id", "callId")
	}
	meta.SummaryText = truncateRunes(meta.SummaryText, SummaryMaxRunes)
	meta.Failed = explicitFailed ||
		isExecutionFailure(meta.SummaryText, meta.DetailText, extra) ||
		(isCompactedExtra(extra) && meta.DetailText != "")
	if meta.Failed {
		meta.DetailText = truncateUTF8Bytes(meta.DetailText, FailureDetailMaxBytes)
	} else {
		meta.DetailText = ""
	}
	return meta, meta.SummaryText != ""
}

func BuildExecutionCardURI(payload map[string]any) string {
	values := url.Values{}
	data, _ := json.Marshal(payload)
	values.Set("d", string(data))
	return (&url.URL{
		Scheme:   "grix",
		Host:     "card",
		Path:     "/tool_execution",
		RawQuery: values.Encode(),
	}).String()
}

type groupChild struct {
	SummaryText string
	DetailText  string
	Failed      bool
}

func compactExecutionGroupContent(content string, alreadyCompacted bool) (string, bool) {
	payload, ok := extractCardPayload(content, "grix://card/tool_execution_group?")
	if !ok {
		return content, false
	}
	rawChildren, _ := payload["children"].([]any)
	children := make([]groupChild, 0, len(rawChildren))
	for _, rawChild := range rawChildren {
		childPayload, ok := rawChild.(map[string]any)
		if !ok {
			continue
		}
		summary, _ := childPayload["summary_text"].(string)
		summary = truncateRunes(summary, SummaryMaxRunes)
		if summary == "" {
			continue
		}
		detail, _ := childPayload["detail_text"].(string)
		explicitFailed, _ := childPayload["failed"].(bool)
		failed := explicitFailed ||
			isExecutionFailure(summary, detail, nil) ||
			(alreadyCompacted && detail != "")
		if failed {
			detail = truncateUTF8Bytes(detail, FailureDetailMaxBytes)
		} else {
			detail = ""
		}
		children = append(children, groupChild{
			SummaryText: summary,
			DetailText:  detail,
			Failed:      failed,
		})
	}
	if len(children) == 0 {
		return content, false
	}

	totalCount := maxInt(
		intValue(payload["total_count"]),
		intValue(payload["count"]),
		len(children)+intValue(payload["omitted_count"]),
	)
	if totalCount < len(children) {
		totalCount = len(children)
	}
	initialOmitted := maxInt(intValue(payload["omitted_count"]), totalCount-len(children))
	retained := make([]groupChild, 0, len(children))
	omitted := initialOmitted
	for _, child := range children {
		candidate := append(append([]groupChild(nil), retained...), child)
		if len(buildExecutionGroupCard(candidate, totalCount, omitted)) <= GroupMaxBytes {
			retained = candidate
			continue
		}
		if child.Failed {
			replaced := false
			for i := len(retained) - 1; i >= 0; i-- {
				if retained[i].Failed {
					continue
				}
				prior := retained[i]
				retained[i] = child
				if len(buildExecutionGroupCard(retained, totalCount, omitted+1)) <= GroupMaxBytes {
					omitted++
					replaced = true
				} else {
					retained[i] = prior
				}
				break
			}
			if replaced {
				continue
			}
		}
		omitted++
	}
	return buildExecutionGroupCard(retained, totalCount, omitted), true
}

func extractCardPayload(content, marker string) (map[string]any, bool) {
	idx := strings.Index(content, marker)
	if idx < 0 {
		return nil, false
	}
	uriEnd := strings.IndexByte(content[idx:], ')')
	if uriEnd < 0 {
		uriEnd = len(content)
	} else {
		uriEnd += idx
	}
	u, err := url.Parse(content[idx:uriEnd])
	if err != nil {
		return nil, false
	}
	encoded := u.Query().Get("d")
	if encoded == "" {
		return nil, false
	}
	var payload map[string]any
	if json.Unmarshal([]byte(encoded), &payload) != nil {
		return nil, false
	}
	return payload, true
}

func buildExecutionGroupCard(children []groupChild, totalCount, omittedCount int) string {
	payload := map[string]any{
		"count":       totalCount,
		"total_count": totalCount,
	}
	if omittedCount > 0 {
		payload["omitted_count"] = omittedCount
		payload["truncated"] = true
	}
	childPayloads := make([]map[string]any, 0, len(children))
	for _, child := range children {
		record := map[string]any{"summary_text": child.SummaryText}
		if child.DetailText != "" {
			record["detail_text"] = child.DetailText
		}
		if child.Failed {
			record["failed"] = true
		}
		childPayloads = append(childPayloads, record)
	}
	payload["children"] = childPayloads

	fallback := fmt.Sprintf("[Tools] %d executions", totalCount)
	if totalCount == 1 && len(children) == 1 {
		fallback = fmt.Sprintf("[Tools] %s", truncateRunes(children[0].SummaryText, 60))
	}
	values := url.Values{}
	data, _ := json.Marshal(payload)
	values.Set("d", string(data))
	cardURI := (&url.URL{
		Scheme:   "grix",
		Host:     "card",
		Path:     "/tool_execution_group",
		RawQuery: values.Encode(),
	}).String()
	return "[" + fallback + "](" + cardURI + ")"
}

func intValue(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case json.Number:
		parsed, _ := strconv.Atoi(typed.String())
		return parsed
	}
	return 0
}

func maxInt(values ...int) int {
	max := 0
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	return max
}

func compactExtra(extra json.RawMessage) json.RawMessage {
	envelope := map[string]any{
		"channel_data": map[string]any{
			"grix": map[string]any{
				"toolExecution": map[string]any{"compacted": true},
			},
		},
	}
	if len(extra) > 0 {
		var incoming map[string]any
		if json.Unmarshal(extra, &incoming) == nil {
			for _, key := range []string{"media_url", "thread_id", "mention_user_ids"} {
				if value, ok := incoming[key]; ok {
					envelope[key] = value
				}
			}
			if attachments := compactAttachments(incoming["attachments"]); len(attachments) > 0 {
				envelope["attachments"] = attachments
			}
		}
	}
	encoded, _ := json.Marshal(envelope)
	return encoded
}

func isCompactedExtra(extra json.RawMessage) bool {
	if len(extra) == 0 {
		return false
	}
	var envelope struct {
		ChannelData struct {
			Grix struct {
				ToolExecution struct {
					Compacted bool `json:"compacted"`
				} `json:"toolExecution"`
			} `json:"grix"`
		} `json:"channel_data"`
	}
	return json.Unmarshal(extra, &envelope) == nil &&
		envelope.ChannelData.Grix.ToolExecution.Compacted
}

func compactAttachments(value any) []map[string]string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]map[string]string, 0, len(items))
	for _, item := range items {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		compact := make(map[string]string, 4)
		for _, key := range []string{"media_url", "file_name", "content_type", "attachment_type"} {
			if text, ok := record[key].(string); ok && strings.TrimSpace(text) != "" {
				compact[key] = strings.TrimSpace(text)
			}
		}
		if len(compact) > 0 {
			result = append(result, compact)
		}
	}
	return result
}

func truncateRunes(value string, maxLen int) string {
	value = strings.TrimSpace(value)
	if maxLen <= 0 || utf8.RuneCountInString(value) <= maxLen {
		return value
	}
	runes := []rune(value)
	if maxLen > 3 {
		return string(runes[:maxLen-3]) + "..."
	}
	return string(runes[:maxLen])
}

func truncateUTF8Bytes(value string, maxBytes int) string {
	value = strings.TrimSpace(value)
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	if maxBytes <= 3 {
		return truncateRunes(value, maxBytes)
	}
	limit := maxBytes - 3
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit] + "..."
}

func firstStringValue(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func findNestedStringValue(raw json.RawMessage, keys ...string) string {
	if len(raw) == 0 {
		return ""
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	keySet := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		keySet[key] = struct{}{}
	}
	return findNestedString(value, keySet)
}

func findNestedString(value any, keys map[string]struct{}) string {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if _, ok := keys[key]; ok {
				if text, ok := child.(string); ok && strings.TrimSpace(text) != "" {
					return strings.TrimSpace(text)
				}
			}
		}
		for _, child := range typed {
			if found := findNestedString(child, keys); found != "" {
				return found
			}
		}
	case []any:
		for _, child := range typed {
			if found := findNestedString(child, keys); found != "" {
				return found
			}
		}
	}
	return ""
}

func isExecutionFailure(summary, detail string, extra json.RawMessage) bool {
	summaryLower := strings.ToLower(strings.TrimSpace(summary))
	for _, marker := range []string{" (error)", "[error]", " failed", " failure", "timed out", "timeout"} {
		if strings.Contains(summaryLower, marker) {
			return true
		}
	}
	detailLower := strings.ToLower(strings.TrimSpace(detail))
	if strings.HasPrefix(detailLower, "error:") || strings.HasPrefix(detailLower, "failed:") {
		return true
	}
	if exitCode := parseExitCode(detailLower); exitCode != 0 {
		return true
	}
	if len(extra) == 0 {
		return false
	}
	var value any
	if json.Unmarshal(extra, &value) != nil {
		return false
	}
	return nestedToolFailure(value)
}

func nestedToolFailure(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			switch strings.ToLower(key) {
			case "is_error", "iserror":
				if failed, ok := child.(bool); ok && failed {
					return true
				}
			case "status", "state":
				if status, ok := child.(string); ok {
					switch strings.ToLower(strings.TrimSpace(status)) {
					case "error", "failed", "failure", "denied", "timeout", "timed_out":
						return true
					}
				}
			case "exit_code", "exitcode":
				switch code := child.(type) {
				case float64:
					if code != 0 {
						return true
					}
				case string:
					if parsed, err := strconv.Atoi(strings.TrimSpace(code)); err == nil && parsed != 0 {
						return true
					}
				}
			}
		}
		for _, child := range typed {
			if nestedToolFailure(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if nestedToolFailure(child) {
				return true
			}
		}
	}
	return false
}

func parseExitCode(detail string) int {
	const marker = "exit code:"
	idx := strings.Index(detail, marker)
	if idx < 0 {
		return 0
	}
	fields := strings.Fields(detail[idx+len(marker):])
	if len(fields) == 0 {
		return 0
	}
	code, _ := strconv.Atoi(strings.Trim(fields[0], ".,;"))
	return code
}
