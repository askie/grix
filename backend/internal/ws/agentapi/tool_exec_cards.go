package agentapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/toolcard"
)

const (
	toolExecDedupTTL        = 6 * time.Hour
	toolExecDedupPendingTTL = 2 * time.Minute
	// Keep a package-local alias for tests while the shared compactor remains
	// the source of truth.
	toolExecFailureDetailMaxBytes = toolcard.FailureDetailMaxBytes
)

type toolExecPayloadMeta struct {
	SummaryText string
	DetailText  string
	ToolCallID  string
	Failed      bool
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

// toolExecCardReservation is the outcome of reserveToolExecCard.
//
//   - handled=true: the card must not be persisted again (grix-internal tool,
//     or an exact retry already stored as msgID); the caller acks and returns.
//   - handled=false: persist the card as its own message, then call
//     finishToolExecCard with the result so retries resolve to it.
//
// Each tool call is stored as an independent message. Clients fold adjacent
// tool_execution messages into one group for display, so any other message in
// between (text, approval card, ...) naturally starts a new group.
type toolExecCardReservation struct {
	handled  bool
	msgID    int64
	dedupKey string
}

func reserveToolExecCard(
	ctx context.Context,
	conn *agentConn,
	sessionID string,
	eventID string,
	clientMsgID string,
	meta toolExecPayloadMeta,
) toolExecCardReservation {
	if strings.TrimSpace(meta.SummaryText) == "" {
		return toolExecCardReservation{}
	}
	if isGrixInternalToolName(meta.SummaryText) {
		return toolExecCardReservation{handled: true}
	}
	stableKey := buildToolExecStableKey(eventID, clientMsgID, meta)
	if stableKey == "" {
		return toolExecCardReservation{}
	}
	dedupKey, reserved, existingMsgID := reserveToolExecDedup(ctx, conn.agentID, sessionID, stableKey)
	if !reserved {
		return toolExecCardReservation{handled: true, msgID: existingMsgID}
	}
	return toolExecCardReservation{dedupKey: dedupKey}
}

// finishToolExecCard records the persisted msgID for the reservation, or
// releases it when persisting failed so a retry can store the card.
func finishToolExecCard(ctx context.Context, reservation toolExecCardReservation, msgID int64) {
	if msgID <= 0 {
		releaseToolExecDedup(ctx, reservation.dedupKey)
		return
	}
	completeToolExecDedup(ctx, reservation.dedupKey, msgID)
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
