package agentapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/toolcard"
)

const (
	toolExecAccumTTL        = 120 * time.Second
	toolExecDedupTTL        = 6 * time.Hour
	toolExecDedupPendingTTL = 2 * time.Minute
	// Keep package-local size aliases for accumulator tests while the shared
	// compactor remains the source of truth.
	toolExecFailureDetailMaxBytes = toolcard.FailureDetailMaxBytes
	toolExecGroupMaxBytes         = toolcard.GroupMaxBytes
)

type toolExecAccumState struct {
	MsgID        int64                `json:"msg_id"`
	Children     []toolExecAccumChild `json:"children"`
	VisibleTo    []int64              `json:"visible_to,omitempty"`
	SeenKeys     []string             `json:"seen_keys,omitempty"`
	TotalCount   int                  `json:"total_count,omitempty"`
	OmittedCount int                  `json:"omitted_count,omitempty"`
}

type toolExecAccumChild struct {
	SummaryText string `json:"summary_text"`
	DetailText  string `json:"detail_text,omitempty"`
	Failed      bool   `json:"failed,omitempty"`
}

type toolExecPayloadMeta struct {
	SummaryText string
	DetailText  string
	ToolCallID  string
	Failed      bool
}

func toolExecAccumKey(agentID int64, sessionID string) string {
	return fmt.Sprintf("im:agent_api:tool_exec_accum:%d:%s", agentID, strings.TrimSpace(sessionID))
}

func toolExecDedupKey(agentID int64, sessionID, stableKey string) string {
	sum := sha256.Sum256([]byte(stableKey))
	return fmt.Sprintf(
		"im:agent_api:tool_exec_dedup:%d:%s:%x",
		agentID,
		strings.TrimSpace(sessionID),
		sum[:16],
	)
}

func loadToolExecAccum(ctx context.Context, agentID int64, sessionID string) (*toolExecAccumState, error) {
	if store.RDB == nil || agentID <= 0 || strings.TrimSpace(sessionID) == "" {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	raw, err := store.RDB.Get(ctx, toolExecAccumKey(agentID, sessionID)).Bytes()
	if err != nil {
		return nil, nil
	}
	var state toolExecAccumState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func saveToolExecAccum(ctx context.Context, agentID int64, sessionID string, state *toolExecAccumState) {
	if store.RDB == nil || agentID <= 0 || strings.TrimSpace(sessionID) == "" || state == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	raw, err := json.Marshal(state)
	if err != nil {
		logger.L.Warnf("marshal tool exec accum failed: agent=%d session=%s err=%v", agentID, sessionID, err)
		return
	}
	if err := store.RDB.Set(ctx, toolExecAccumKey(agentID, sessionID), raw, toolExecAccumTTL).Err(); err != nil {
		logger.L.Warnf("save tool exec accum failed: agent=%d session=%s err=%v", agentID, sessionID, err)
	}
}

func deleteToolExecAccum(ctx context.Context, agentID int64, sessionID string) {
	if store.RDB == nil || agentID <= 0 || strings.TrimSpace(sessionID) == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := store.RDB.Del(ctx, toolExecAccumKey(agentID, sessionID)).Err(); err != nil {
		logger.L.Warnf("delete tool exec accum failed: agent=%d session=%s err=%v", agentID, sessionID, err)
	}
}

func isToolExecutionCard(content string) bool {
	return strings.Contains(content, "grix://card/tool_execution") &&
		!strings.Contains(content, "grix://card/tool_execution_group")
}

// isInteractionCard reports whether the message carries a card that asks the
// user to act (approve/deny a tool, answer a question). These cards are the
// user's only way to unblock a turn that is waiting on them, so they must
// never be suppressed by the stream-conflict gate in handleSendMsg: an agent
// that streams part of its turn and THEN requests approval delivers the card
// on the same event_id as the stream, and rejecting it deadlocks the turn
// (agent waits for a decision the user can never give).
func isInteractionCard(content string) bool {
	return strings.Contains(content, "grix://card/exec_approval") ||
		strings.Contains(content, "grix://card/agent_question")
}

// isGrixInternalToolCard checks whether a tool_execution card represents an
// internal grix tool (e.g. grix_message_send, mcp__grix-claude__*) that should
// be suppressed from the user-facing tool call card feed.
func isGrixInternalToolCard(content string) bool {
	if !isToolExecutionCard(content) {
		return false
	}
	summary, _, ok := extractToolExecutionParams(content)
	if !ok {
		return false
	}
	return isGrixInternalToolName(summary)
}

// isGrixInternalToolName returns true when the tool summary indicates a grix
// platform-internal tool that should not be shown to the end user.
func isGrixInternalToolName(name string) bool {
	if strings.HasPrefix(name, "grix_") {
		return true
	}
	if strings.HasPrefix(name, "mcp__grix") {
		return true
	}
	return false
}

func extractToolExecutionParams(content string) (summary, detail string, ok bool) {
	meta, ok := extractToolExecutionPayload(content, nil)
	if !ok {
		return "", "", false
	}
	return meta.SummaryText, meta.DetailText, true
}

func extractToolExecutionPayload(content string, extra json.RawMessage) (toolExecPayloadMeta, bool) {
	sharedMeta, ok := toolcard.ExtractMetadata(content, extra)
	return toolExecPayloadMeta{
		SummaryText: sharedMeta.SummaryText,
		DetailText:  sharedMeta.DetailText,
		ToolCallID:  sharedMeta.ToolCallID,
		Failed:      sharedMeta.Failed,
	}, ok
}

func buildToolExecutionGroupCard(children []toolExecAccumChild) string {
	return buildToolExecutionGroupCardWithCounts(children, len(children), 0)
}

func buildToolExecutionGroupCardWithCounts(children []toolExecAccumChild, totalCount, omittedCount int) string {
	if totalCount < len(children) {
		totalCount = len(children)
	}
	payload := map[string]any{
		"count":       totalCount,
		"total_count": totalCount,
	}
	if omittedCount > 0 {
		payload["omitted_count"] = omittedCount
		payload["truncated"] = true
	}
	childPayloads := make([]map[string]any, 0, len(children))
	for _, c := range children {
		child := map[string]any{
			"summary_text": c.SummaryText,
		}
		if c.DetailText != "" {
			child["detail_text"] = c.DetailText
		}
		childPayloads = append(childPayloads, child)
	}
	payload["children"] = childPayloads

	fallback := fmt.Sprintf("[Tools] %d executions", totalCount)
	if totalCount == 1 && len(children) == 1 {
		fallback = fmt.Sprintf("[Tools] %s", truncateText(children[0].SummaryText, 60))
	}

	return buildToolExecutionGroupCardLink(fallback, payload)
}

func buildToolExecutionGroupCardLink(fallbackText string, payload map[string]any) string {
	return "[" + fallbackText + "](" + buildToolExecutionGroupCardURI(payload) + ")"
}

func buildToolExecutionGroupCardURI(payload map[string]any) string {
	values := url.Values{}
	data, _ := json.Marshal(payload)
	values.Set("d", string(data))

	return (&url.URL{
		Scheme:   "grix",
		Host:     "card",
		Path:     "/tool_execution_group",
		RawQuery: values.Encode(),
	}).String()
}

func truncateText(s string, maxLen int) string {
	return truncateRunes(s, maxLen)
}

func truncateRunes(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if maxLen <= 0 || utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	if maxLen > 3 {
		return string(runes[:maxLen-3]) + "..."
	}
	return string(runes[:maxLen])
}

// toolExecAccumResult holds the outcome of tryAccumulateToolExec.
//
//   - handled=true:  the message was edited in-place; caller should send_ack and return.
//   - handled=false, children!=nil:  first tool_execution in sequence; use modifiedContent for sendFn,
//     then save accum with the returned children.
//   - handled=false, children==nil:  not a tool_execution card (accum already cleaned); proceed normally.
type toolExecAccumResult struct {
	handled         bool
	msgID           int64
	modifiedContent string
	children        []toolExecAccumChild
	dedupKey        string
	seenKeys        []string
	totalCount      int
	omittedCount    int
}

func (m *Manager) tryAccumulateToolExec(
	ctx context.Context,
	conn *agentConn,
	sessionID string,
	eventID string,
	clientMsgID string,
	meta toolExecPayloadMeta,
) toolExecAccumResult {
	if strings.TrimSpace(meta.SummaryText) == "" {
		return toolExecAccumResult{}
	}

	if isGrixInternalToolName(meta.SummaryText) {
		return toolExecAccumResult{handled: true}
	}

	stableKey := buildToolExecStableKey(eventID, clientMsgID, meta)
	dedupKey := ""
	if stableKey != "" {
		var reserved bool
		var existingMsgID int64
		dedupKey, reserved, existingMsgID = reserveToolExecDedup(ctx, conn.agentID, sessionID, stableKey)
		if !reserved {
			return toolExecAccumResult{handled: true, msgID: existingMsgID}
		}
	}

	child := toolExecAccumChild{
		SummaryText: meta.SummaryText,
		DetailText:  meta.DetailText,
		Failed:      meta.Failed,
	}
	accum, _ := loadToolExecAccum(ctx, conn.agentID, sessionID)

	if accum != nil && accum.MsgID > 0 && m.editMsgFn != nil {
		appendToolExecChildBounded(accum, child)
		if stableKey != "" {
			accum.SeenKeys = appendUniqueString(accum.SeenKeys, stableKey)
		}
		newContent := buildToolExecutionGroupCardWithCounts(accum.Children, accum.TotalCount, accum.OmittedCount)
		editErr := m.editMsgFn(ctx, conn.agentID, conn.ownerID, EditMsgPayload{
			SessionID: sessionID,
			MsgID:     accum.MsgID,
			Content:   newContent,
		})
		if editErr == nil {
			saveToolExecAccum(ctx, conn.agentID, sessionID, accum)
			completeToolExecDedup(ctx, dedupKey, accum.MsgID)
			return toolExecAccumResult{handled: true, msgID: accum.MsgID}
		}
		logger.L.Warnf("edit tool exec accum failed, falling back to new message: agent=%d session=%s msg_id=%d err=%v",
			conn.agentID, sessionID, accum.MsgID, editErr)
		deleteToolExecAccum(ctx, conn.agentID, sessionID)
	}

	firstChildren := []toolExecAccumChild{child}
	return toolExecAccumResult{
		modifiedContent: buildToolExecutionGroupCardWithCounts(firstChildren, 1, 0),
		children:        firstChildren,
		dedupKey:        dedupKey,
		seenKeys:        nonEmptyStrings(stableKey),
		totalCount:      1,
	}
}

func finishFirstToolExecAccum(
	ctx context.Context,
	agentID int64,
	sessionID string,
	result toolExecAccumResult,
	msgID int64,
	visibleTo []int64,
) {
	if result.children == nil || msgID <= 0 {
		releaseToolExecDedup(ctx, result.dedupKey)
		return
	}
	saveToolExecAccum(ctx, agentID, sessionID, &toolExecAccumState{
		MsgID:        msgID,
		Children:     result.children,
		VisibleTo:    visibleTo,
		SeenKeys:     result.seenKeys,
		TotalCount:   result.totalCount,
		OmittedCount: result.omittedCount,
	})
	completeToolExecDedup(ctx, result.dedupKey, msgID)
}

func compactToolExecutionPayload(
	content string,
	extra json.RawMessage,
) (string, json.RawMessage, toolExecPayloadMeta, bool) {
	compactContent, compactExtra, sharedMeta, ok := toolcard.CompactForStorage(content, extra)
	return compactContent, compactExtra, toolExecPayloadMeta{
		SummaryText: sharedMeta.SummaryText,
		DetailText:  sharedMeta.DetailText,
		ToolCallID:  sharedMeta.ToolCallID,
		Failed:      sharedMeta.Failed,
	}, ok
}

func buildToolExecutionCardURI(payload map[string]any) string {
	return toolcard.BuildExecutionCardURI(payload)
}

func buildToolExecStableKey(eventID, clientMsgID string, meta toolExecPayloadMeta) string {
	eventID = strings.TrimSpace(eventID)
	clientMsgID = strings.TrimSpace(clientMsgID)
	toolCallID := strings.TrimSpace(meta.ToolCallID)
	phase := "execution"
	if meta.Failed {
		phase = "failure"
	}
	switch {
	case toolCallID != "":
		return strings.Join([]string{eventID, "tool_call", toolCallID, phase}, "\x00")
	case clientMsgID != "":
		return strings.Join([]string{eventID, "client_msg", clientMsgID}, "\x00")
	default:
		return ""
	}
}

func reserveToolExecDedup(
	ctx context.Context,
	agentID int64,
	sessionID string,
	stableKey string,
) (dedupKey string, reserved bool, existingMsgID int64) {
	if store.RDB == nil || stableKey == "" {
		return "", true, 0
	}
	if ctx == nil {
		ctx = context.Background()
	}
	dedupKey = toolExecDedupKey(agentID, sessionID, stableKey)
	ok, err := store.RDB.SetNX(ctx, dedupKey, "pending", toolExecDedupPendingTTL).Result()
	if err != nil {
		logger.L.Warnf("reserve tool exec dedup failed: agent=%d session=%s err=%v", agentID, sessionID, err)
		return "", true, 0
	}
	if ok {
		return dedupKey, true, 0
	}
	raw, _ := store.RDB.Get(ctx, dedupKey).Result()
	msgID, _ := strconv.ParseInt(raw, 10, 64)
	return dedupKey, false, msgID
}

func completeToolExecDedup(ctx context.Context, dedupKey string, msgID int64) {
	if store.RDB == nil || dedupKey == "" || msgID <= 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := store.RDB.Set(ctx, dedupKey, strconv.FormatInt(msgID, 10), toolExecDedupTTL).Err(); err != nil {
		logger.L.Warnf("complete tool exec dedup failed: key=%s err=%v", dedupKey, err)
	}
}

func releaseToolExecDedup(ctx context.Context, dedupKey string) {
	if store.RDB == nil || dedupKey == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	const releasePendingScript = `
if redis.call("GET", KEYS[1]) == "pending" then
  return redis.call("DEL", KEYS[1])
end
return 0
`
	if err := store.RDB.Eval(ctx, releasePendingScript, []string{dedupKey}).Err(); err != nil {
		logger.L.Warnf("release tool exec dedup failed: key=%s err=%v", dedupKey, err)
	}
}

func appendToolExecChildBounded(state *toolExecAccumState, child toolExecAccumChild) {
	if state == nil {
		return
	}
	if state.TotalCount < len(state.Children)+state.OmittedCount {
		state.TotalCount = len(state.Children) + state.OmittedCount
	}
	state.TotalCount++
	candidate := append(append([]toolExecAccumChild(nil), state.Children...), child)
	if len(buildToolExecutionGroupCardWithCounts(candidate, state.TotalCount, state.OmittedCount)) <= toolExecGroupMaxBytes {
		state.Children = candidate
		return
	}

	if child.Failed {
		for i := len(state.Children) - 1; i >= 0; i-- {
			if state.Children[i].Failed {
				continue
			}
			prior := state.Children[i]
			state.Children[i] = child
			if len(buildToolExecutionGroupCardWithCounts(state.Children, state.TotalCount, state.OmittedCount+1)) <= toolExecGroupMaxBytes {
				state.OmittedCount++
				return
			}
			state.Children[i] = prior
			break
		}
	}
	state.OmittedCount++
}

func appendUniqueString(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func nonEmptyStrings(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}
