package agentapi

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskNotificationEligible(t *testing.T) {
	t.Run("owner-triggered run qualifies", func(t *testing.T) {
		assert.True(t, taskNotificationEligible(&activeAgentRun{OwnerID: 1, SenderID: 1}))
	})

	t.Run("visitor or proxy triggered run does not qualify", func(t *testing.T) {
		assert.False(t, taskNotificationEligible(&activeAgentRun{OwnerID: 1, SenderID: 2}))
	})

	t.Run("voice call turn does not qualify", func(t *testing.T) {
		assert.False(t, taskNotificationEligible(&activeAgentRun{OwnerID: 1, SenderID: 1, CallTurn: true}))
	})

	t.Run("nil or ownerless run does not qualify", func(t *testing.T) {
		assert.False(t, taskNotificationEligible(nil))
		assert.False(t, taskNotificationEligible(&activeAgentRun{SenderID: 1}))
	})
}

func TestTaskStateEligible(t *testing.T) {
	t.Run("owner-triggered run qualifies", func(t *testing.T) {
		assert.True(t, taskStateEligible(&activeAgentRun{OwnerID: 1, SenderID: 1}))
	})

	t.Run("group member triggered run qualifies", func(t *testing.T) {
		assert.True(t, taskStateEligible(&activeAgentRun{OwnerID: 1, SenderID: 2}))
	})

	t.Run("voice call turn does not qualify", func(t *testing.T) {
		assert.False(t, taskStateEligible(&activeAgentRun{OwnerID: 1, SenderID: 2, CallTurn: true}))
	})

	t.Run("nil or ownerless run does not qualify", func(t *testing.T) {
		assert.False(t, taskStateEligible(nil))
		assert.False(t, taskStateEligible(&activeAgentRun{SenderID: 1}))
	})
}

func TestGroupMemberRunPersistsChatTaskState(t *testing.T) {
	previousDB, previousRDB, previousJS := store.DB, store.RDB, store.JS
	store.DB, store.RDB, store.JS = testutil.NewTestDB().DB, nil, nil
	t.Cleanup(func() {
		store.DB, store.RDB, store.JS = previousDB, previousRDB, previousJS
	})

	mgr := NewManager("", 0, nil, nil, nil, nil)
	defer mgr.Shutdown()
	evt := DelegateEventPayload{
		EventID: "evt-group-member", AgentID: 42, OwnerID: 7, SenderID: 8,
		SessionID: "sess-group-member", SessionType: 2, MsgID: 100,
	}
	mgr.registerActiveRunForDispatch(evt, time.Now().UTC(), false)
	mgr.persistActiveRunRunning(evt.EventID)

	assertState := func(expected string) bool {
		var state model.SessionAgentState
		err := store.DB.Where("session_id = ? AND owner_id = ?", evt.SessionID, evt.OwnerID).
			First(&state).Error
		return err == nil && state.State == expected && state.LastRunID == evt.EventID
	}
	require.Eventually(t, func() bool {
		return assertState(model.SessionAgentStateRunning)
	}, time.Second, 10*time.Millisecond)

	require.NoError(t, mgr.MarkRunCompleted(evt.EventID))
	require.Eventually(t, func() bool {
		return assertState(model.SessionAgentStateCompleted)
	}, time.Second, 10*time.Millisecond)
}

func TestHasActiveRunForSessionOwner(t *testing.T) {
	mgr := NewManager("", 0, nil, nil, nil, nil)
	defer mgr.Shutdown()
	first := DelegateEventPayload{
		EventID: "evt-queue-1", AgentID: 100, OwnerID: 200, SenderID: 200,
		SessionID: "sess-queue", SessionType: 1, MsgID: 300,
	}
	second := first
	second.EventID = "evt-queue-2"
	second.MsgID = 301
	mgr.registerActiveRun(first)
	mgr.registerActiveRun(second)

	// Completing the first run leaves the second in flight — queue not empty.
	mgr.MarkRunCompleted(first.EventID)
	assert.True(t, mgr.hasActiveRunForSessionOwner("sess-queue", 200))

	// Completing the last run drains the queue.
	mgr.MarkRunCompleted(second.EventID)
	assert.False(t, mgr.hasActiveRunForSessionOwner("sess-queue", 200))
}

func TestIsUserInitiatedStopReason(t *testing.T) {
	assert.True(t, isUserInitiatedStopReason("owner_requested_stop"))
	assert.True(t, isUserInitiatedStopReason(protocol.AgentDeliveryCodeCanceled))
	assert.True(t, isUserInitiatedStopReason("call_ended"))
	assert.True(t, isUserInitiatedStopReason("hangup"))
	assert.True(t, isUserInitiatedStopReason("  owner_requested_stop  "))

	assert.False(t, isUserInitiatedStopReason(""))
	assert.False(t, isUserInitiatedStopReason(protocol.AgentDeliveryCodeProcessingFailed))
	assert.False(t, isUserInitiatedStopReason(protocol.AgentDeliveryCodeChannelUnavailable))
}

func TestShouldNotifyTaskFailed(t *testing.T) {
	run := &activeAgentRun{EventID: "evt-1", SessionID: "sess-guard", OwnerID: 1, SenderID: 1, AgentID: 42}

	seedAgentMessage := func(t *testing.T, createdAt time.Time) {
		t.Helper()
		prev := store.DB
		store.DB = testutil.NewTestDB().DB
		t.Cleanup(func() { store.DB = prev })
		require.NoError(t, store.DB.Create(&model.Message{
			MsgID: 1001, SessionID: run.SessionID, SenderID: run.AgentID, CreatedAt: createdAt,
		}).Error)
	}

	t.Run("result timeout never notifies — it proves loss of contact, not failure", func(t *testing.T) {
		assert.False(t, shouldNotifyTaskFailed(run, protocol.AgentDeliveryCodeResultTimeout))
	})

	t.Run("fresh connector verdicts notify even when the last output is old", func(t *testing.T) {
		seedAgentMessage(t, time.Now().Add(-6*time.Hour))
		for _, reason := range []string{
			"agent_idle_timeout",
			"agent_hard_timeout",
			protocol.AgentDeliveryCodeAgentStopFailure,
			"session_invalid_cwd",
			"worker_interrupted",
			protocol.AgentDeliveryCodeChannelUnavailable,
			protocol.AgentDeliveryCodeAckTimeout,
			"some free text reason",
		} {
			assert.True(t, shouldNotifyTaskFailed(run, reason), "reason=%s", reason)
		}
	})

	t.Run("cleanup verdict with a recently active agent notifies", func(t *testing.T) {
		seedAgentMessage(t, time.Now().Add(-1*time.Minute))
		assert.True(t, shouldNotifyTaskFailed(run, protocol.AgentDeliveryCodeProcessingFailed))
	})

	t.Run("cleanup verdict long after the agent's last message is reaping — silent", func(t *testing.T) {
		seedAgentMessage(t, time.Now().Add(-staleFailureNotifyWindow-time.Minute))
		assert.False(t, shouldNotifyTaskFailed(run, protocol.AgentDeliveryCodeProcessingFailed))
		assert.False(t, shouldNotifyTaskFailed(run, "event_stale"))
	})

	t.Run("cleanup verdict with no agent message at all fails open and notifies", func(t *testing.T) {
		prev := store.DB
		store.DB = testutil.NewTestDB().DB
		t.Cleanup(func() { store.DB = prev })
		assert.True(t, shouldNotifyTaskFailed(run, protocol.AgentDeliveryCodeProcessingFailed))
	})

	t.Run("cleanup verdict without a DB fails open and notifies", func(t *testing.T) {
		prev := store.DB
		store.DB = nil
		t.Cleanup(func() { store.DB = prev })
		assert.True(t, shouldNotifyTaskFailed(run, protocol.AgentDeliveryCodeProcessingFailed))
	})
}

func TestLastAgentMessageAtPicksNewestByMsgID(t *testing.T) {
	prev := store.DB
	store.DB = testutil.NewTestDB().DB
	t.Cleanup(func() { store.DB = prev })

	old := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	fresh := time.Now().Add(-30 * time.Second).UTC().Truncate(time.Second)
	require.NoError(t, store.DB.Create(&model.Message{
		MsgID: 1, SessionID: "sess-x", SenderID: 42, CreatedAt: old,
	}).Error)
	require.NoError(t, store.DB.Create(&model.Message{
		MsgID: 2, SessionID: "sess-x", SenderID: 42, CreatedAt: fresh,
	}).Error)
	// A newer message from a different sender must be ignored.
	require.NoError(t, store.DB.Create(&model.Message{
		MsgID: 3, SessionID: "sess-x", SenderID: 7, CreatedAt: time.Now().UTC(),
	}).Error)

	got, found, err := lastAgentMessageAt("sess-x", 42)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, fresh, got.UTC().Truncate(time.Second))

	_, found, err = lastAgentMessageAt("sess-x", 999)
	require.NoError(t, err)
	assert.False(t, found)
}

// MarkRunFailedNotify must keep the verbatim msg as the stored/displayed stop
// reason — only the notification layer sees the protocol code.
func TestMarkRunFailedNotifyKeepsVerbatimStopReason(t *testing.T) {
	previousDB, previousRDB, previousJS := store.DB, store.RDB, store.JS
	store.DB, store.RDB, store.JS = nil, nil, nil
	t.Cleanup(func() {
		store.DB, store.RDB, store.JS = previousDB, previousRDB, previousJS
	})

	mgr := NewManager("", 0, nil, nil, nil, nil)
	defer mgr.Shutdown()
	outputCh := make(chan protocol.AgentOutputStatusPayload, 4)
	mgr.SetOutputStatusHandler(func(p protocol.AgentOutputStatusPayload) { outputCh <- p })
	mgr.registerActiveRun(DelegateEventPayload{
		EventID: "evt-notify-1", AgentID: 42, OwnerID: 7, SenderID: 7,
		SessionID: "sess-notify", SessionType: 1, MsgID: 1,
	})
	select {
	case <-outputCh: // queued
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for queued output status")
	}

	mgr.MarkRunFailedNotify("evt-notify-1", "delta_content too large", protocol.AgentDeliveryCodeProcessingFailed)
	select {
	case failed := <-outputCh:
		assert.Equal(t, protocol.AgentOutputStateFailed, failed.State)
		assert.Equal(t, "delta_content too large", failed.StopReason)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for failed output status")
	}
}

func TestExtractQuestionFromCard(t *testing.T) {
	t.Run("recognizes a real agent_question card link in content", func(t *testing.T) {
		// This is the actual shape produced by both grix-hermes's clarify
		// override and the server's own Claude elicitation bridge
		// (buildLocalGrixCardLink(..., "agent_question", ...)) — the only two
		// real-world producers of declared question cards in this codebase.
		content := `[Agent Question] 你今天想聊点什么？](grix://card/agent_question?d=%7B%22request_id%22%3A%22872e1f70fd%22%2C%22mode%22%3A%22form%22%2C%22questions%22%3A%5B%7B%22index%22%3A1%2C%22header%22%3A%22h%22%2C%22prompt%22%3A%22p%22%7D%5D%7D)`
		id, ok := extractQuestionFromCard(content, nil)
		assert.True(t, ok, "a declared agent_question card must be recognized")
		assert.Equal(t, "872e1f70fd", id)
	})

	t.Run("recognizes a declared card with no request_id", func(t *testing.T) {
		content := `[Agent Question] x](grix://card/agent_question?d=%7B%22mode%22%3A%22form%22%2C%22questions%22%3A%5B%5D%7D)`
		_, ok := extractQuestionFromCard(content, nil)
		assert.True(t, ok)
	})

	t.Run("recognizes biz_card.type agent_question surviving in extra", func(t *testing.T) {
		extra := []byte(`{"biz_card":{"type":"agent_question","payload":{"request_id":"req-1"}}}`)
		id, ok := extractQuestionFromCard("some content mentioning question", extra)
		assert.True(t, ok)
		assert.Equal(t, "req-1", id)
	})

	t.Run("does not match the legacy grix://card/question / question_card literals", func(t *testing.T) {
		// Neither literal is ever produced anywhere in this codebase — this
		// pins down that the detector no longer depends on them.
		content := `[Question](grix://card/question?question_id=abc)`
		extra := []byte(`{"biz_card":{"type":"question_card","payload":{"question_id":"abc"}}}`)
		_, ok := extractQuestionFromCard(content, nil)
		assert.False(t, ok, "grix://card/question is not a real card URI produced anywhere")
		_, ok2 := extractQuestionFromCard("plain question text", extra)
		assert.False(t, ok2, "question_card is not a real biz_card.type produced anywhere")
	})

	t.Run("plain text with no declared card is never treated as a question", func(t *testing.T) {
		_, ok := extractQuestionFromCard("what do you think about this question?", nil)
		assert.False(t, ok)
	})
}
