package notification

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var allCopyKeys = []string{
	copyTitleApproval, copyTitleQuestion, copyTitleCompleted, copyTitleFailed,
	copyTitleStopped, copyTitleStarted, copyTitleUnknown, copyTitleDefault,
	copyBodyStarted, copyBodyCompleted, copyBodyStopped, copyBodyFailed,
	copyBodyUnknown, copyFailedPrefix,
	copyTitleAgentOnline, copyTitleAgentOffline,
	copyBodyAgentOnlineOne, copyBodyAgentOnlineMany,
	copyBodyAgentOfflineOne, copyBodyAgentOfflineMany,
}

// Every supported language must define every copy key — a missing entry would
// silently fall back to Chinese for that string only.
func TestPushCopyComplete(t *testing.T) {
	expectedLangs := []string{"zh", "en", "ja", "ko", "de", "fr", "es", "pt", "ru", "ar", "hi"}
	require.Len(t, pushCopy, len(expectedLangs))
	for _, lang := range expectedLangs {
		table, ok := pushCopy[lang]
		require.True(t, ok, "missing language %s", lang)
		for _, key := range allCopyKeys {
			assert.NotEmpty(t, table[key], "lang=%s key=%s", lang, key)
		}
	}
}

func TestPushTitle(t *testing.T) {
	titleOf := func(eventKey, summary, lang string) string {
		return pushTitle(&AgentNotificationEvent{EventKey: eventKey, Summary: summary}, lang)
	}
	assert.Equal(t, "审批请求", titleOf(EventApprovalRequested, "", "zh"))
	assert.Equal(t, "Approval request", titleOf(EventApprovalRequested, "", "en"))
	assert.Equal(t, "Task completed", titleOf(EventTaskCompleted, "", "en"))
	assert.Equal(t, "タスク失敗", titleOf(EventTaskFailed, "", "ja"))
	// Unknown event key falls back to the generic title.
	assert.Equal(t, "Agent notification", titleOf("something_new", "", "en"))
	// Unknown language falls back to Chinese.
	assert.Equal(t, "任务完成", titleOf(EventTaskCompleted, "", "tlh"))
	assert.Equal(t, "任务完成", titleOf(EventTaskCompleted, "", ""))

	// ack timeout only proves the event went unconfirmed — the push must claim
	// "status unknown", never "failed".
	assert.Equal(t, "任务状态未知", titleOf(EventTaskFailed, "agent_api_event_ack_timeout", "zh"))
	assert.Equal(t, "Task status unknown", titleOf(EventTaskFailed, "agent_api_event_ack_timeout", "en"))
	// Other codes keep the failed title.
	assert.Equal(t, "任务失败", titleOf(EventTaskFailed, "agent_api_event_processing_failed", "zh"))
}

func TestPushBody(t *testing.T) {
	// Lifecycle events get fixed localized copy regardless of Summary.
	assert.Equal(t, "The task has completed",
		pushBody(&AgentNotificationEvent{EventKey: EventTaskCompleted}, "en"))
	assert.Equal(t, "任务开始执行",
		pushBody(&AgentNotificationEvent{EventKey: EventTaskStarted}, "zh"))

	// task_failed maps known stop-reason codes to localized phrases.
	assert.Equal(t, "Task failed: no result was returned in time",
		pushBody(&AgentNotificationEvent{EventKey: EventTaskFailed, Summary: "agent_api_event_result_timeout"}, "en"))
	assert.Equal(t, "任务失败：消息处理出错",
		pushBody(&AgentNotificationEvent{EventKey: EventTaskFailed, Summary: "agent_api_event_processing_failed"}, "zh"))
	assert.Equal(t, "任务失败：Agent 中断，未能完成回复",
		pushBody(&AgentNotificationEvent{EventKey: EventTaskFailed, Summary: "agent_stop_failure"}, "zh"))
	// Unknown codes and free-text reasons must NOT leak to users — collapse to
	// the generic localized body.
	assert.Equal(t, "The task failed",
		pushBody(&AgentNotificationEvent{EventKey: EventTaskFailed, Summary: "connection lost"}, "en"))
	assert.Equal(t, "任务失败",
		pushBody(&AgentNotificationEvent{EventKey: EventTaskFailed, Summary: "Claude exited before completing its reply. Please try again."}, "zh"))
	assert.Equal(t, "The task failed",
		pushBody(&AgentNotificationEvent{EventKey: EventTaskFailed}, "en"))
	// Unknown language falls back to Chinese for the mapped reason.
	assert.Equal(t, "任务失败：长时间未返回结果",
		pushBody(&AgentNotificationEvent{EventKey: EventTaskFailed, Summary: "agent_api_event_result_timeout"}, "tlh"))

	// ack timeout renders the dedicated unknown-status body, not a failure claim.
	assert.Equal(t, "Agent 未确认收到任务，请打开会话查看",
		pushBody(&AgentNotificationEvent{EventKey: EventTaskFailed, Summary: "agent_api_event_ack_timeout"}, "zh"))
	assert.Equal(t, "The agent did not confirm receiving the task. Open the chat to check",
		pushBody(&AgentNotificationEvent{EventKey: EventTaskFailed, Summary: "agent_api_event_ack_timeout"}, "en"))
	// Unknown language falls back to Chinese for the unknown-status body too.
	assert.Equal(t, "Agent 未确认收到任务，请打开会话查看",
		pushBody(&AgentNotificationEvent{EventKey: EventTaskFailed, Summary: "agent_api_event_ack_timeout"}, "tlh"))

	// Callbackable events pass agent content through untranslated.
	assert.Equal(t, "rm -rf /tmp/build 可以执行吗？",
		pushBody(&AgentNotificationEvent{EventKey: EventApprovalRequested, Summary: "rm -rf /tmp/build 可以执行吗？"}, "en"))
}

// Every supported language must define every failure-reason key, and the
// reason tables must cover the same languages as the main copy table.
func TestFailReasonCopyComplete(t *testing.T) {
	require.Len(t, failReasonCopy, len(pushCopy))
	allReasonKeys := make(map[string]struct{}, len(stopReasonCopyKey))
	for _, key := range stopReasonCopyKey {
		allReasonKeys[key] = struct{}{}
	}
	for lang := range pushCopy {
		table, ok := failReasonCopy[lang]
		require.True(t, ok, "missing language %s", lang)
		for key := range allReasonKeys {
			assert.NotEmpty(t, table[key], "lang=%s key=%s", lang, key)
		}
	}
}

func TestLocalizedFailReason(t *testing.T) {
	assert.Equal(t, "Agent 连接不可用", localizedFailReason("agent_api_channel_unavailable", "zh"))
	assert.Equal(t, "the agent connection is unavailable", localizedFailReason("agent_api_channel_unavailable", "en"))
	// Unknown code / free text → empty (caller renders generic body).
	assert.Equal(t, "", localizedFailReason("some_new_code", "zh"))
	assert.Equal(t, "", localizedFailReason("", "zh"))
	// Unknown language falls back to Chinese.
	assert.Equal(t, "任务已过期", localizedFailReason("event_stale", "tlh"))
}

func TestUserPreferredLanguageFallback(t *testing.T) {
	// No DB wired in unit tests — must fall back to zh, not panic.
	assert.Equal(t, "zh", userPreferredLanguage(0))
	assert.Equal(t, "zh", userPreferredLanguage(12345))
}
