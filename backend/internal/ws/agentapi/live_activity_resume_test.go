package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/grixactions"
	"github.com/askie/grix/backend/internal/liveactivity"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/nats-io/nats.go"
)

// fakeJetStream 只实现 Publish，其余方法由内嵌接口占位（本用例不会走到）。
// 实时活动的发布口在 liveactivity 包里，从这里看得见的唯一出口就是 NATS。
type fakeJetStream struct {
	nats.JetStreamContext
	mu   sync.Mutex
	msgs [][]byte
}

func (f *fakeJetStream) Publish(_ string, data []byte, _ ...nats.PubOpt) (*nats.PubAck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cloned := make([]byte, len(data))
	copy(cloned, data)
	f.msgs = append(f.msgs, cloned)
	return &nats.PubAck{}, nil
}

// runningUpdates 挑出这个会话上 phase=running 的 update 帧。
func (f *fakeJetStream) runningUpdates(t *testing.T, sessionID string) []protocol.LiveActivityPayload {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []protocol.LiveActivityPayload
	for _, raw := range f.msgs {
		var task struct {
			Cmd     string                       `json:"cmd"`
			Payload protocol.LiveActivityPayload `json:"payload"`
		}
		if err := json.Unmarshal(raw, &task); err != nil {
			continue
		}
		if task.Cmd != protocol.CmdLiveActivity ||
			task.Payload.SessionID != sessionID ||
			task.Payload.Event != protocol.LiveActivityEventUpdate ||
			task.Payload.ContentState.Phase != protocol.LiveActivityPhaseRunning {
			continue
		}
		out = append(out, task.Payload)
	}
	return out
}

// liveActivityDummySendFn 只是让 Manager 认为"能发消息"，本用例不校验消息本身。
var liveActivityDummySendFn SendMessageHandler = func(_ context.Context, _ SendMessageReq) (*SendMessageResult, error) {
	return &SendMessageResult{MsgID: 1}, nil
}

// setupLiveActivityResumeTest 备一张已经在锁屏上、停在"等你"阶段的卡。
func setupLiveActivityResumeTest(t *testing.T, ownerID, agentID int64, sessionID string) *fakeJetStream {
	t.Helper()
	logger.Init()

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	js := &fakeJetStream{}
	store.JS = js
	t.Cleanup(func() {
		testDB.Close()
		store.DB = nil
		store.RDB = nil
		store.JS = nil
	})

	now := time.Now().UTC()
	row := model.SessionAgentState{
		SessionID: sessionID,
		OwnerID:   ownerID,
		AgentID:   agentID,
		State:     model.SessionAgentStateWaitingApproval,
		TaskTitle: "等主人",
		StartedAt: &now,
		UpdatedAt: now,
	}
	if err := store.DB.Save(&row).Error; err != nil {
		t.Fatalf("seed chat_states: %v", err)
	}
	// 走公开入口开卡，避免测试自己拼 Redis 索引的 key 形状。
	liveactivity.OnWaiting(
		liveactivity.Run{UserID: ownerID, AgentID: agentID, SessionID: sessionID},
		protocol.LiveActivityPhaseWaitingApproval,
		"要删除生产数据库",
	)
	return js
}

// waitForRunningUpdates 等后台协程把这一帧发出去（钩子挂在 goBackground 上）。
func waitForRunningUpdates(t *testing.T, js *fakeJetStream, sessionID string, want int) []protocol.LiveActivityPayload {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var got []protocol.LiveActivityPayload
	for time.Now().Before(deadline) {
		got = js.runningUpdates(t, sessionID)
		if len(got) >= want {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// 再给一小段时间，好抓住"多发了一帧"的情况。
	time.Sleep(150 * time.Millisecond)
	return js.runningUpdates(t, sessionID)
}

// 主人在 App 聊天里点了审批卡：走的是 WS 这条路，不经过通知回调，
// 卡片同样要从"等你"翻回"在跑"。
func TestExecApprovalCommandResumesLiveActivity(t *testing.T) {
	const (
		ownerID   = int64(15001)
		agentID   = int64(88040)
		sessionID = "sess-live-activity-approve"
	)
	js := setupLiveActivityResumeTest(t, ownerID, agentID, sessionID)

	mgr := NewManager("", 30*time.Second, liveActivityDummySendFn, nil, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID:      agentID,
		ownerID:      ownerID,
		capabilities: []string{"local_action_v1"},
		localActions: []string{"exec_approve", "exec_reject"},
		send:         make(chan []byte, 8),
	}
	mgr.putConnForTest(conn)

	handled := mgr.tryHandleExecApprovalCommand(DelegateEventPayload{
		EventID:   "evt-approve-live-activity",
		AgentID:   agentID,
		OwnerID:   ownerID,
		SessionID: sessionID,
		MsgID:     88101,
		SenderID:  ownerID,
		Content:   "/approve req_live_activity allow-once",
	})
	if !handled {
		t.Fatal("owner approval should be handled")
	}

	updates := waitForRunningUpdates(t, js, sessionID, 1)
	if len(updates) != 1 {
		t.Fatalf("expected exactly 1 running update after an in-app approval, got %d", len(updates))
	}
	if updates[0].Alert != nil {
		t.Fatal("a resumed update must not alert")
	}
}

// 主人在 App 里答完提问：同样只走 WS，卡片要回到"在跑"。
func TestClaudeQuestionReplyResumesLiveActivity(t *testing.T) {
	const (
		ownerID   = int64(15002)
		agentID   = int64(88041)
		sessionID = "sess-live-activity-answer"
	)
	js := setupLiveActivityResumeTest(t, ownerID, agentID, sessionID)

	mgr := NewManager("", 30*time.Second, liveActivityDummySendFn, nil, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID:      agentID,
		ownerID:      ownerID,
		capabilities: []string{"local_action_v1"},
		localActions: []string{"claude_interaction_reply"},
		send:         make(chan []byte, 8),
	}
	mgr.putConnForTest(conn)

	handled := mgr.tryHandleClaudeQuestionCommand(DelegateEventPayload{
		EventID:   "evt-answer-live-activity",
		AgentID:   agentID,
		OwnerID:   ownerID,
		SessionID: sessionID,
		MsgID:     88102,
		SenderID:  ownerID,
		Content: grixactions.BuildQuestionReplyURI(grixactions.QuestionReply{
			RequestID: "req_live_activity",
			Action:    "accept",
		}),
	})
	if !handled {
		t.Fatal("owner question reply should be handled")
	}

	updates := waitForRunningUpdates(t, js, sessionID, 1)
	if len(updates) != 1 {
		t.Fatalf("expected exactly 1 running update after an in-app answer, got %d", len(updates))
	}
}

// 没有活卡的会话，主人的动作不该凭空推出一帧。
func TestResumeWithoutLiveCardPublishesNothing(t *testing.T) {
	const (
		ownerID   = int64(15003)
		agentID   = int64(88042)
		sessionID = "sess-live-activity-no-card"
	)
	js := setupLiveActivityResumeTest(t, ownerID, agentID, sessionID)
	// 收掉刚开的那张卡，模拟"主人关了推送 / 卡已经结束"。
	liveactivity.OnTerminal(
		liveactivity.Run{UserID: ownerID, AgentID: agentID, SessionID: sessionID},
		protocol.LiveActivityPhaseStopped,
		"",
	)

	mgr := NewManager("", 30*time.Second, liveActivityDummySendFn, nil, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID:      agentID,
		ownerID:      ownerID,
		capabilities: []string{"local_action_v1"},
		localActions: []string{"exec_approve", "exec_reject"},
		send:         make(chan []byte, 8),
	}
	mgr.putConnForTest(conn)

	mgr.tryHandleExecApprovalCommand(DelegateEventPayload{
		EventID:   "evt-approve-no-card",
		AgentID:   agentID,
		OwnerID:   ownerID,
		SessionID: sessionID,
		MsgID:     88103,
		SenderID:  ownerID,
		Content:   fmt.Sprintf("/approve %s allow-once", "req_no_card"),
	})

	if updates := waitForRunningUpdates(t, js, sessionID, 0); len(updates) != 0 {
		t.Fatalf("expected no running update without a live card, got %d", len(updates))
	}
}
