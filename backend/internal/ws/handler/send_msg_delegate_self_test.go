package handler

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
)

// 直拨语音调度（架构文档 33）：owner 本人的转写须能触发 owner 自己挂的托管，
// 而普通消息路径的 skip-self 红线必须保持不变。
func TestSelfDelegateTrigger(t *testing.T) {
	const (
		sessionID = "session-voice-self-delegate"
		ownerID   = int64(8601)
		agentID   = int64(9601)
		msgID     = int64(18889990801)
	)

	setup := func(t *testing.T, sid string) func() {
		cleanup := setupSendMsgTest(t)
		prevManager := wsagentapi.GetGlobal()
		wsagentapi.SetGlobal(nil)

		if err := store.DB.Create(&model.Agent{
			ID: agentID, AgentName: "dispatch-brain", OwnerID: ownerID,
			ProviderType: model.AgentProviderAPI, Status: 1,
		}).Error; err != nil {
			t.Fatalf("create agent error: %v", err)
		}
		if err := store.DB.Create(&model.Session{
			SessionID: sid, OwnerID: ownerID, SessionType: 1,
		}).Error; err != nil {
			t.Fatalf("create session error: %v", err)
		}
		if err := store.DB.Create(&model.SessionMember{
			SessionID: sid, MemberID: ownerID, MemberType: 1,
		}).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
		if err := store.RDB.HSet(context.Background(), "im:delegate:"+sid+":8601",
			"agent_id", "9601",
			"max_consecutive_replies", "10",
		).Err(); err != nil {
			t.Fatalf("seed delegate key error: %v", err)
		}
		return func() {
			wsagentapi.SetGlobal(prevManager)
			cleanup()
		}
	}

	queuedLen := func(t *testing.T) int {
		raw, err := store.RDB.LRange(context.Background(), "im:agent_api:queued_events:9601", 0, -1).Result()
		if err != nil {
			t.Fatalf("load queued delegate events error: %v", err)
		}
		return len(raw)
	}

	t.Run("regular trigger keeps skip-self", func(t *testing.T) {
		defer setup(t, sessionID+"-a")()
		hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{}}
		dispatched := TriggerDelegatesForMessage(
			hub, context.Background(), sessionID+"-a", ownerID, 1,
			msgID, 0, 1, "自己打的字", nil, nil,
		)
		if dispatched {
			t.Fatal("普通路径 sender 本人的托管不得被触发（skip-self 红线）")
		}
		if n := queuedLen(t); n != 0 {
			t.Fatalf("普通路径不应入队事件 got=%d", n)
		}
	})

	t.Run("self trigger dispatches own delegate", func(t *testing.T) {
		defer setup(t, sessionID+"-b")()
		hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{}}
		dispatched := TriggerSelfDelegateForMessage(
			hub, context.Background(), sessionID+"-b", ownerID, 1,
			msgID, 0, int16(model.MsgTypeCallSegment), "看下重构进展", nil, nil,
		)
		if !dispatched {
			t.Fatal("直拨语音转写必须触发 owner 本人挂的托管")
		}
		if n := queuedLen(t); n != 1 {
			t.Fatalf("应入队 1 条托管事件 got=%d", n)
		}
	})

	t.Run("voice rate limit throttles immediate repeat", func(t *testing.T) {
		defer setup(t, sessionID+"-c")()
		hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{}}
		first := TriggerSelfDelegateForMessage(
			hub, context.Background(), sessionID+"-c", ownerID, 1,
			msgID, 0, int16(model.MsgTypeCallSegment), "第一段", nil, nil,
		)
		second := TriggerSelfDelegateForMessage(
			hub, context.Background(), sessionID+"-c", ownerID, 1,
			msgID+1, 0, int16(model.MsgTypeCallSegment), "第二段", nil, nil,
		)
		if !first {
			t.Fatal("第一段应触发")
		}
		if second {
			t.Fatal("同秒内第二段应被限流（delegateVoiceRateTTL 窗口内）")
		}
	})
}
