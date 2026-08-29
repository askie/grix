package agentapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/call"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/notification"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/textutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/gorm"
)

// notification_hooks.go bridges agent-side events to the notification channel.
// Every publish is best-effort and must never affect the agent message path.

const questionSummaryMaxRunes = 100

// publishApprovalNotification emits an approval_requested event when an agent
// sends an exec_approval card. The run lookup supplies run_id so the offline
// callback can also stop the task.
func (m *Manager) publishApprovalNotification(ownerID, agentID int64, sessionID, summary, approvalCommandID string) {
	if ownerID == 0 || approvalCommandID == "" {
		return
	}
	runID := ""
	if run := m.LookupActiveRunBySessionOwner(ownerID, sessionID); run != nil {
		runID = run.EventID
	}
	notification.Publish(notification.AgentNotificationEvent{
		EventKey:   notification.EventApprovalRequested,
		UserID:     ownerID,
		AgentID:    agentID,
		SessionID:  sessionID,
		RunID:      runID,
		RunEventID: runID,
		Summary:    notificationSummary(summary),
		ActionMeta: &notification.ActionMeta{
			AvailableActions:  []string{notification.ActionApprove, notification.ActionDeny, notification.ActionStop},
			ApprovalCommandID: approvalCommandID,
		},
		// 同一张审批卡只推一次：NATS 重投 / push 服务重启后的补投都被回执栅栏挡住。
		IdempotencyKey: fmt.Sprintf("approval:%s:%s", sessionID, approvalCommandID),
	})
	m.goBackground(func() {
		store.SetSessionAgentStateWaiting(sessionID, ownerID, model.SessionAgentStateWaitingApproval)
	})
}

// publishQuestionNotification emits an agent_question event when an agent sends
// a question_card.
func (m *Manager) publishQuestionNotification(ownerID, agentID int64, sessionID, summary, questionID string, questionMsgID int64) {
	if ownerID == 0 {
		return
	}
	runID := ""
	if run := m.LookupActiveRunBySessionOwner(ownerID, sessionID); run != nil {
		runID = run.EventID
	}
	notification.Publish(notification.AgentNotificationEvent{
		EventKey:   notification.EventAgentQuestion,
		UserID:     ownerID,
		AgentID:    agentID,
		SessionID:  sessionID,
		RunID:      runID,
		RunEventID: runID,
		Summary:    notificationSummary(summary),
		ActionMeta: &notification.ActionMeta{
			AvailableActions:  []string{notification.ActionReply, notification.ActionStop},
			QuestionID:        questionID,
			QuestionMessageID: questionMsgID,
		},
		IdempotencyKey: fmt.Sprintf("question:%s:%d", sessionID, questionMsgID),
	})
	m.goBackground(func() {
		store.SetSessionAgentStateWaiting(sessionID, ownerID, model.SessionAgentStateWaitingQuestion)
	})
}

// userInitiatedStopReasons are interruption codes caused by a deliberate user
// action (stop button, hangup, cancel). They are never failures and never
// "unexpected" — task notifications stay silent for them.
var userInitiatedStopReasons = map[string]struct{}{
	"owner_requested_stop":             {},
	protocol.AgentDeliveryCodeCanceled: {},
	"call_ended":                       {},
	"hangup":                           {},
}

func isUserInitiatedStopReason(reason string) bool {
	_, ok := userInitiatedStopReasons[strings.TrimSpace(reason)]
	return ok
}

// 失败推送误报治理（依据 2026-07-10 CN 生产 chat_states 全量 failed 审计）：
// 超时/收尾类判定只能证明"后端与任务失联"，不能证明任务失败，分两级处理。

// suppressedFailureNotifyReasons never produce a task_failed push: the verdict
// is the backend's result-wait fallback, which in production fired 1.5–5.5h
// after the agent's last message — post-hoc reaping, never a live failure.
// chat_states still records failed + the code for auditing; only the push is
// dropped.
var suppressedFailureNotifyReasons = map[string]struct{}{
	protocol.AgentDeliveryCodeResultTimeout: {},
}

// deferredCleanupNotifyReasons can be emitted long after the agent actually
// stopped — a ws-node restart or connector reconnect flushes stale pending
// events in one sweep and each lands here as a "failure". For these codes a
// push is meaningful only while the agent was recently active; otherwise it is
// reaping and stays silent. Codes carrying a fresh connector verdict
// (idle/hard timeout, stop failure, invalid cwd, worker interrupted) are
// deliberately absent: their decision is new even when the last output is old
// — agent_idle_timeout by definition always has a stale last activity.
var deferredCleanupNotifyReasons = map[string]struct{}{
	protocol.AgentDeliveryCodeProcessingFailed: {},
	protocol.AgentDeliveryCodeEventStale:       {},
}

// staleFailureNotifyWindow separates live failures from reaping for
// deferredCleanupNotifyReasons. 30min per production replay: every mass-sweep
// record trailed the agent's last message by ≥194min, while genuine failures
// after quiet long tool runs showed only 15–21min gaps.
const staleFailureNotifyWindow = 30 * time.Minute

// shouldNotifyTaskFailed decides whether a task_failed push is warranted for
// this stop-reason code. Callers must pass a non-nil run (guarded by
// taskNotificationEligible). Fail-open on lookup problems: missing a real
// failure is worse than one extra misfire.
func shouldNotifyTaskFailed(run *activeAgentRun, notifyReason string) bool {
	reason := strings.TrimSpace(notifyReason)
	if _, ok := suppressedFailureNotifyReasons[reason]; ok {
		logger.L.Infof(
			"task_failed push suppressed (timeout fallback proves loss of contact, not failure) run=%s session=%s reason=%s",
			run.EventID, run.SessionID, reason,
		)
		return false
	}
	if _, ok := deferredCleanupNotifyReasons[reason]; !ok {
		return true
	}
	lastMsgAt, found, err := lastAgentMessageAt(run.SessionID, run.AgentID)
	if err != nil || !found {
		return true
	}
	if age := time.Since(lastMsgAt); age > staleFailureNotifyWindow {
		logger.L.Infof(
			"task_failed push suppressed as post-hoc reaping run=%s session=%s reason=%s last_agent_msg_age=%s",
			run.EventID, run.SessionID, reason, age.Truncate(time.Second),
		)
		return false
	}
	return true
}

// lastAgentMessageAtTimeout bounds the guard's DB lookup: a ws-restart sweep
// and a slow DB tend to coincide, and N runs failing at once each query here —
// better to fail-open fast than pile up blocked timer goroutines.
const lastAgentMessageAtTimeout = 3 * time.Second

// lastAgentMessageAt returns the creation time of the agent's newest message
// in the session — the same measure the production misfire audit used, and one
// that survives ws-node restarts (in-memory run timestamps are reset when a
// run is recovered from Redis, exactly the mass-sweep scenario). Served by the
// (session_id, msg_id DESC) partition index; only reached on failure paths.
//
// 边界：delegate 会话里 agent 消息落库 sender_id = OwnerID（ResolveIdentity），
// 这里按 sender_id = agentID 查不到行 → fail-open 照推。守卫只对 agent 以
// 自己身份发言的会话生效；漏拦是安全方向，误拦才是要防的。
func lastAgentMessageAt(sessionID string, agentID int64) (time.Time, bool, error) {
	if store.DB == nil || strings.TrimSpace(sessionID) == "" || agentID <= 0 {
		return time.Time{}, false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), lastAgentMessageAtTimeout)
	defer cancel()
	var msg model.Message
	err := store.DB.WithContext(ctx).
		Select("created_at").
		Where("session_id = ? AND sender_id = ?", sessionID, agentID).
		Order("msg_id DESC").
		Limit(1).
		Take(&msg).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	return msg.CreatedAt, true, nil
}

// senderInVoiceCall reports whether the user currently holds the cross-node
// voice-call busy marker. Checked once at run registration so the flag also
// covers results arriving after the call ended.
func senderInVoiceCall(senderID int64) bool {
	if senderID <= 0 || store.RDB == nil {
		return false
	}
	n, err := store.RDB.Exists(context.Background(), call.UserBusyKey(senderID)).Result()
	return err == nil && n > 0
}

// taskStateEligible controls which runs appear in the owner's chat task state.
// Group-member turns are real session work and must be tracked; voice-call
// turns are conversation, not tasks.
// Internal protocol events (customer coach snapshots and other no-reply
// events) are backend-originated even though they carry the owner as sender,
// so they never surface as chat task state or owner task notifications.
func taskStateEligible(run *activeAgentRun) bool {
	return run != nil && run.OwnerID != 0 && !run.CallTurn &&
		!isNoReplyProtocolEventID(run.EventID)
}

// taskNotificationEligible is intentionally stricter than state tracking:
// only owner-triggered runs may push owner task notifications.
func taskNotificationEligible(run *activeAgentRun) bool {
	return taskStateEligible(run) && run.SenderID == run.OwnerID
}

// publishTaskNotification emits a pure-告知 lifecycle event (no offline action).
func publishTaskNotification(
	run *activeAgentRun,
	eventKey string,
	summary string,
	detail string,
	reliable bool,
) error {
	if !taskNotificationEligible(run) {
		return nil
	}
	event := notification.AgentNotificationEvent{
		EventKey:   eventKey,
		UserID:     run.OwnerID,
		AgentID:    run.AgentID,
		SessionID:  run.SessionID,
		RunID:      run.EventID,
		RunEventID: run.EventID,
		Summary:    summary,
		Detail:     detail,
		IdempotencyKey: fmt.Sprintf(
			"agent-terminal:%s:%s:%s",
			run.EventID,
			run.State,
			eventKey,
		),
	}
	if reliable {
		return notification.PublishReliable(event)
	}
	notification.Publish(event)
	return nil
}

// extractQuestionFromCard returns the request_id when a send_msg carries a
// declared agent_question card — either as a rendered grix://card/agent_question
// link in content (the normal path: a client's biz_card gets turned into this
// link by agentcards.Normalize before the message is stored), or as a raw
// biz_card.type == "agent_question" still present in extra. Returns ok=false
// otherwise — questions are recognized only when the agent declares them,
// never by text heuristics.
//
// The card's payload is always complex (nested "questions" array), so the
// URI carries it as a JSON-encoded "d" query param rather than a flat
// "question_id" param — see agentcards.buildGrixCardURI / hasComplexPayload.
func extractQuestionFromCard(content string, extra json.RawMessage) (string, bool) {
	if !strings.Contains(content, "question") && !strings.Contains(string(extra), "agent_question") {
		return "", false
	}

	if idx := strings.Index(content, "grix://card/agent_question"); idx >= 0 {
		uriStr := content[idx:]
		if end := strings.IndexByte(uriStr, ')'); end >= 0 {
			uriStr = uriStr[:end]
		}
		if parsed, err := url.Parse(uriStr); err == nil {
			if id := extractAgentQuestionCardRequestID(parsed); id != "" {
				return id, true
			}
			// A declared question card with no request_id still counts as a question.
			return "", true
		}
	}

	var envelope struct {
		BizCard struct {
			Type    string `json:"type"`
			Payload struct {
				RequestID string `json:"request_id"`
			} `json:"payload"`
		} `json:"biz_card"`
	}
	if err := json.Unmarshal(extra, &envelope); err == nil && envelope.BizCard.Type == "agent_question" {
		return strings.TrimSpace(envelope.BizCard.Payload.RequestID), true
	}

	return "", false
}

// extractAgentQuestionCardRequestID reads request_id out of the agent_question
// card's JSON-encoded "d" query param. Mirrors extractAgentQuestionRequestID
// in claude_question_bridge.go (kept separate: that one takes bare content,
// this one an already-parsed URI).
func extractAgentQuestionCardRequestID(parsed *url.URL) string {
	raw := parsed.Query().Get("d")
	if raw == "" {
		return ""
	}
	var decoded struct {
		RequestID string `json:"request_id"`
	}
	if json.Unmarshal([]byte(raw), &decoded) != nil {
		return ""
	}
	return strings.TrimSpace(decoded.RequestID)
}

// taskFailedSummary carries the stop-reason code only; the dispatcher renders
// the localized "task failed" copy around it in the recipient's language.
func taskFailedSummary(stopReason string) string {
	reason := strings.TrimSpace(stopReason)
	if reason == "" {
		return ""
	}
	return textutil.TruncateRunes(reason, 80)
}

// taskFailedDetail carries the agent's own free-text failure message, which is
// often the only place the real cause appears (a code-less connector result, or
// a generic processing_failed code wrapping a specific message). Bounded to the
// same length as the summary so the copy layer never has to render an
// unbounded body.
func taskFailedDetail(msg string) string {
	detail := strings.TrimSpace(msg)
	if detail == "" {
		return ""
	}
	return textutil.TruncateRunes(detail, 80)
}

func notificationSummary(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return textutil.TruncateRunes(s, questionSummaryMaxRunes)
}
