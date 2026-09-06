package agentapi

import (
	"context"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/require"
)

// 手机上点问答卡回复后，客户端发出的就是这条裸 URI 消息。
const ownerQuestionReplyContent = `grix://card/agent_question_reply?d=%7B%22request_id%22%3A%22req-1%22%2C%22response%22%3A%7B%22q1%22%3A%22ok%22%7D%7D`

func ownerAnswerTestManager(t *testing.T) *Manager {
	t.Helper()
	sendFn := func(_ context.Context, _ SendMessageReq) (*SendMessageResult, error) {
		return &SendMessageResult{MsgID: 1}, nil
	}
	manager := NewManager("", 30*time.Second, sendFn, nil, nil, nil)
	t.Cleanup(manager.Shutdown)
	return manager
}

func requireChatStateEventually(
	t *testing.T,
	sessionID string,
	ownerID int64,
	state string,
	lastRunID string,
) {
	t.Helper()
	var last model.SessionAgentState
	require.Eventuallyf(t, func() bool {
		row, err := store.GetSessionAgentState(sessionID, ownerID)
		if err != nil || row == nil {
			return false
		}
		last = *row
		return row.State == state && row.LastRunID == lastRunID
	}, 3*time.Second, 10*time.Millisecond,
		"chat_states 应为 state=%s last_run_id=%s，实际 state=%s last_run_id=%s",
		state, lastRunID, last.State, last.LastRunID)
}

// 复现 2026-09-05 CN 线上 e42b7055 的时序：一轮任务还在跑，主人在手机上回了
// 问答卡，回执被当成新任务 run 派给连接器并秒回 responded，chat_states 被写成
// completed；随后 agent 再发的问题卡因为行已终态而翻不动，手表待办永远为空。
//
// 修复后：回执事件既不抢 running 也不写终态，行始终归原 run，问题卡照常能把它
// 翻成 waiting_question。
func TestOwnerQuestionReplyDoesNotOverwriteRunningChatState(t *testing.T) {
	installDurableLifecycleTestStores(t, true)
	manager := ownerAnswerTestManager(t)

	const (
		agentID = int64(9101)
		ownerID = int64(9201)
	)
	runA := durableLifecycleEvent("owner-answer-run-a", agentID, ownerID)
	sessionID := runA.SessionID

	conn := &agentConn{
		agentID:      agentID,
		ownerID:      ownerID,
		capabilities: []string{"event_result_ack"},
		send:         make(chan []byte, 16),
	}
	manager.putConnForTest(conn)

	// 1) 主人触发的任务 run A 持久化为 running。
	require.True(t, manager.PushDelegateEvent(runA))
	requireDurablePacket(t, conn.send, protocol.CmdEventMsg)
	requireChatStateEventually(t, sessionID, ownerID, model.SessionAgentStateRunning, runA.EventID)

	// 2) 主人回问答卡：同一会话的一条普通 event_msg，连接器秒回 responded。
	replyB := durableLifecycleEvent("owner-answer-reply-b", agentID, ownerID)
	replyB.SessionID = sessionID
	replyB.MsgID = 2096141861697093632
	replyB.Content = ownerQuestionReplyContent
	require.True(t, manager.PushDelegateEvent(replyB))
	requireDurablePacket(t, conn.send, protocol.CmdEventMsg)

	// 回执事件不进任务台账，也就不该被算成一轮任务。
	ledger, err := store.LoadAgentEventTerminalLedger(replyB.EventID)
	require.NoError(t, err)
	require.NotNil(t, ledger)
	require.False(t, ledger.TaskEligible, "主人回答类事件不算任务 run")

	manager.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 41, EventResultPayload{
		EventID: replyB.EventID,
		Status:  protocol.AgentEventResultResponded,
	}))

	// 3) 行仍然是 running 且仍归 run A —— 修复前这里是 completed + last_run_id=replyB。
	requireChatStateEventually(t, sessionID, ownerID, model.SessionAgentStateRunning, runA.EventID)

	// 4) agent 再发问题卡时，waiting_question 必须还能翻上去（手表待办的来源）。
	store.SetSessionAgentStateWaiting(sessionID, ownerID, model.SessionAgentStateWaitingQuestion)
	row, err := store.GetSessionAgentState(sessionID, ownerID)
	require.NoError(t, err)
	require.NotNil(t, row)
	require.Equal(t, model.SessionAgentStateWaitingQuestion, row.State)
	require.Equal(t, runA.EventID, row.LastRunID)
}

// 主人回答到达时，还卡在"等主人"的行要拨回 running：任务确实又跑起来了，
// 待办列表不该继续把它算成待处理。
func TestOwnerQuestionReplyResumesWaitingChatState(t *testing.T) {
	installDurableLifecycleTestStores(t, true)
	manager := ownerAnswerTestManager(t)

	const (
		agentID = int64(9301)
		ownerID = int64(9401)
	)
	runA := durableLifecycleEvent("owner-answer-waiting-run", agentID, ownerID)
	sessionID := runA.SessionID

	conn := &agentConn{
		agentID:      agentID,
		ownerID:      ownerID,
		capabilities: []string{"event_result_ack"},
		send:         make(chan []byte, 16),
	}
	manager.putConnForTest(conn)

	require.True(t, manager.PushDelegateEvent(runA))
	requireDurablePacket(t, conn.send, protocol.CmdEventMsg)
	requireChatStateEventually(t, sessionID, ownerID, model.SessionAgentStateRunning, runA.EventID)

	// agent 发问题卡，行进入 waiting_question。
	store.SetSessionAgentStateWaiting(sessionID, ownerID, model.SessionAgentStateWaitingQuestion)
	requireChatStateEventually(t, sessionID, ownerID, model.SessionAgentStateWaitingQuestion, runA.EventID)

	replyB := durableLifecycleEvent("owner-answer-waiting-reply", agentID, ownerID)
	replyB.SessionID = sessionID
	replyB.MsgID = 2096141861697093633
	replyB.Content = ownerQuestionReplyContent
	require.True(t, manager.PushDelegateEvent(replyB))
	requireDurablePacket(t, conn.send, protocol.CmdEventMsg)

	// 拨回 running，且 last_run_id 仍是原 run。
	requireChatStateEventually(t, sessionID, ownerID, model.SessionAgentStateRunning, runA.EventID)
}

func TestIsOwnerAnswerEvent(t *testing.T) {
	base := func(content string) DelegateEventPayload {
		return DelegateEventPayload{
			OwnerID:  11,
			SenderID: 11,
			Content:  content,
		}
	}

	t.Run("question reply card", func(t *testing.T) {
		require.True(t, isOwnerAnswerEvent(base(ownerQuestionReplyContent)))
	})
	t.Run("question reply card rendered as markdown link", func(t *testing.T) {
		require.True(t, isOwnerAnswerEvent(base(
			"[已回复](grix://card/agent_question_reply?d=%7B%22request_id%22%3A%22req-7%22%7D)",
		)))
	})
	t.Run("exec approval resolution directive", func(t *testing.T) {
		require.True(t, isOwnerAnswerEvent(base(
			"[[exec-approval-resolution|approval_id=req_1|approval_command_id=req_1|decision=allow-once]]",
		)))
	})
	t.Run("plain approve command", func(t *testing.T) {
		require.True(t, isOwnerAnswerEvent(base("/approve req_1 allow")))
	})
	// hermes 兜底审批把裁决改写成这三条纯文本再当普通消息投递。
	t.Run("hermes fallback approval text replies", func(t *testing.T) {
		for _, content := range []string{"/approve", "/approve always", "/deny"} {
			require.Truef(t, isOwnerAnswerEvent(base(content)), "content=%s", content)
		}
	})
	t.Run("deny lookalike is not an answer", func(t *testing.T) {
		require.False(t, isOwnerAnswerEvent(base("/deny 这条不是裁决回传")))
	})
	t.Run("ordinary task message", func(t *testing.T) {
		require.False(t, isOwnerAnswerEvent(base("帮我看一下昨天的日志")))
	})
	t.Run("question card from the agent is not an answer", func(t *testing.T) {
		require.False(t, isOwnerAnswerEvent(base(
			"[[Agent Question]](grix://card/agent_question?d=%7B%22request_id%22%3A%22req-7%22%7D)",
		)))
	})
	t.Run("reply sent by someone else in the session", func(t *testing.T) {
		evt := base(ownerQuestionReplyContent)
		evt.SenderID = 12
		require.False(t, isOwnerAnswerEvent(evt))
	})
}

// 回执事件即便被单独结算，也不得生成任何 chat_states 终态。
func TestTerminalChatStateSkipsOwnerAnswerEvent(t *testing.T) {
	state := terminalChatState(EventResultPayload{
		Status: protocol.AgentEventResultResponded,
	}, &durablePendingDelegateRecord{
		Event: DelegateEventPayload{
			EventID:   "evt-owner-answer",
			OwnerID:   101,
			AgentID:   202,
			SenderID:  101,
			SessionID: "sess-owner-answer",
			Content:   ownerQuestionReplyContent,
		},
	})
	require.Nil(t, state)
}
