package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/agentreceive"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/redis/go-redis/v9"
)

type dualRoleGroupFixture struct {
	hub        *sendMsgMockHub
	senderConn *sendMsgMockConn
	ownerConn  *sendMsgMockConn
	channel    <-chan *redis.Message
	sessionID  string
	senderID   int64
	ownerID    int64
	agentID    int64
	cleanup    func()
}

type multiAgentGroupFixture struct {
	hub        *sendMsgMockHub
	senderConn *sendMsgMockConn
	channel    <-chan *redis.Message
	sessionID  string
	senderID   int64
	agentIDs   []int64
	cleanup    func()
}

func setupDualRoleGroupFixture(
	t *testing.T,
	sessionID string,
	senderID int64,
	ownerID int64,
	agentID int64,
) *dualRoleGroupFixture {
	t.Helper()

	baseCleanup := setupSendMsgTest(t)
	now := time.Now().UTC()

	users := []model.User{
		{ID: ownerID, Username: fmt.Sprintf("owner_%d", ownerID), Email: fmt.Sprintf("owner_%d@example.com", ownerID), Nickname: fmt.Sprintf("owner-nick-%d", ownerID)},
		{ID: senderID, Username: fmt.Sprintf("sender_%d", senderID), Email: fmt.Sprintf("sender_%d@example.com", senderID), Nickname: fmt.Sprintf("sender-nick-%d", senderID)},
	}
	for _, user := range users {
		if err := store.DB.Create(&user).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
		GroupName:   "dual-role-group",
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}

	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: senderID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
	}
	for _, member := range members {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	if err := store.DB.Create(&model.Agent{
		ID:           agentID,
		OwnerID:      ownerID,
		AgentName:    fmt.Sprintf("DelegateBot%d", agentID),
		ProviderType: model.AgentProviderAPI,
		Status:       1,
		CreatedAt:    now,
		UpdatedAt:    now,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}

	ctx := context.Background()
	delegateKey := fmt.Sprintf("im:delegate:%s:%d", sessionID, ownerID)
	if err := store.RDB.HSet(ctx, delegateKey,
		"agent_id", fmt.Sprintf("%d", agentID),
		"max_consecutive_replies", "3",
	).Err(); err != nil {
		t.Fatalf("seed delegate key error: %v", err)
	}
	if err := store.RDB.Set(ctx, fmt.Sprintf("im:agent_api:route:%d", agentID), "node-target", time.Minute).Err(); err != nil {
		t.Fatalf("seed agent route error: %v", err)
	}

	manager := wsagentapi.NewManager("", 30*time.Second, nil, nil, nil, nil)
	manager.SetNodeID("node-origin")
	previousManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(manager)

	pubsub := store.RDB.Subscribe(ctx, "chan:node-target")
	if _, err := pubsub.ReceiveTimeout(ctx, 200*time.Millisecond); err != nil {
		_ = pubsub.Close()
		wsagentapi.SetGlobal(previousManager)
		baseCleanup()
		t.Fatalf("subscribe forwarded channel error: %v", err)
	}

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "sender-dev"}
	ownerConn := &sendMsgMockConn{userID: ownerID, deviceID: "owner-dev"}
	hub := &sendMsgMockHub{
		nodeID: "node-origin",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
			ownerID:  {ownerConn},
		},
	}

	return &dualRoleGroupFixture{
		hub:        hub,
		senderConn: senderConn,
		ownerConn:  ownerConn,
		channel:    pubsub.Channel(),
		sessionID:  sessionID,
		senderID:   senderID,
		ownerID:    ownerID,
		agentID:    agentID,
		cleanup: func() {
			_ = pubsub.Close()
			wsagentapi.SetGlobal(previousManager)
			baseCleanup()
		},
	}
}

func setupMultiAgentGroupFixture(
	t *testing.T,
	sessionID string,
	senderID int64,
	agentIDs ...int64,
) *multiAgentGroupFixture {
	t.Helper()

	baseCleanup := setupSendMsgTest(t)
	now := time.Now().UTC()

	users := []model.User{
		{ID: senderID, Username: fmt.Sprintf("sender_%d", senderID), Email: fmt.Sprintf("sender_%d@example.com", senderID), Nickname: fmt.Sprintf("sender-nick-%d", senderID)},
	}
	for idx := range agentIDs {
		ownerID := senderID + int64(idx) + 1000
		users = append(users, model.User{
			ID:       ownerID,
			Username: fmt.Sprintf("owner_%d", ownerID),
			Email:    fmt.Sprintf("owner_%d@example.com", ownerID),
			Nickname: fmt.Sprintf("owner-nick-%d", ownerID),
		})
	}
	for _, user := range users {
		if err := store.DB.Create(&user).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     senderID,
		SessionType: 2,
		GroupName:   "multi-agent-group",
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}

	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: senderID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
	}
	for _, member := range members {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	for idx, agentID := range agentIDs {
		if err := store.DB.Create(&model.SessionMember{
			SessionID:    sessionID,
			MemberID:     agentID,
			MemberType:   2,
			JoinedAt:     now,
			LastActiveAt: now,
		}).Error; err != nil {
			t.Fatalf("create agent session member error: %v", err)
		}
		ownerID := senderID + int64(idx) + 1000
		if err := store.DB.Create(&model.Agent{
			ID:           agentID,
			OwnerID:      ownerID,
			AgentName:    fmt.Sprintf("MirrorBot%d", agentID),
			ProviderType: model.AgentProviderAPI,
			Status:       1,
			CreatedAt:    now,
			UpdatedAt:    now,
		}).Error; err != nil {
			t.Fatalf("create agent error: %v", err)
		}
	}

	ctx := context.Background()
	for _, agentID := range agentIDs {
		if err := store.RDB.Set(ctx, fmt.Sprintf("im:agent_api:route:%d", agentID), "node-target", time.Minute).Err(); err != nil {
			t.Fatalf("seed agent route error: %v", err)
		}
	}

	manager := wsagentapi.NewManager("", 30*time.Second, nil, nil, nil, nil)
	manager.SetNodeID("node-origin")
	previousManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(manager)

	pubsub := store.RDB.Subscribe(ctx, "chan:node-target")
	if _, err := pubsub.ReceiveTimeout(ctx, 200*time.Millisecond); err != nil {
		_ = pubsub.Close()
		wsagentapi.SetGlobal(previousManager)
		baseCleanup()
		t.Fatalf("subscribe forwarded channel error: %v", err)
	}

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "sender-dev"}
	hub := &sendMsgMockHub{
		nodeID: "node-origin",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
		},
	}

	return &multiAgentGroupFixture{
		hub:        hub,
		senderConn: senderConn,
		channel:    pubsub.Channel(),
		sessionID:  sessionID,
		senderID:   senderID,
		agentIDs:   append([]int64(nil), agentIDs...),
		cleanup: func() {
			_ = pubsub.Close()
			wsagentapi.SetGlobal(previousManager)
			baseCleanup()
		},
	}
}

func seedIdleGroupMessageHistory(
	t *testing.T,
	sessionID string,
	senderID int64,
	idleFor time.Duration,
) int64 {
	t.Helper()

	oldMsgID := time.Now().UnixNano()
	oldCreatedAt := time.Now().UTC().Add(-idleFor)
	if err := store.DB.Create(&model.Message{
		MsgID:      oldMsgID,
		SessionID:  sessionID,
		SenderID:   senderID,
		SenderType: 1,
		MsgType:    1,
		Content:    "旧消息",
		CreatedAt:  oldCreatedAt,
	}).Error; err != nil {
		t.Fatalf("create idle history message error: %v", err)
	}
	if err := store.DB.Model(&model.Session{}).
		Where("session_id = ?", sessionID).
		Updates(map[string]any{
			"last_msg_id":      oldMsgID,
			"last_msg_summary": "旧消息",
			"updated_at":       oldCreatedAt,
		}).Error; err != nil {
		t.Fatalf("update session idle history error: %v", err)
	}
	return oldMsgID
}

func seedGroupLastMessage(t *testing.T, sessionID string, msg model.Message) {
	t.Helper()

	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}
	if err := store.DB.Create(&msg).Error; err != nil {
		t.Fatalf("create last message error: %v", err)
	}
	if err := store.DB.Model(&model.Session{}).
		Where("session_id = ?", sessionID).
		Updates(map[string]any{
			"last_msg_id":      msg.MsgID,
			"last_msg_summary": msg.Content,
			"updated_at":       msg.CreatedAt,
		}).Error; err != nil {
		t.Fatalf("update session last message error: %v", err)
	}
}

func collectForwardedAgentEvents(
	t *testing.T,
	channel <-chan *redis.Message,
	want int,
) []wsagentapi.DelegateEventPayload {
	t.Helper()

	events := make([]wsagentapi.DelegateEventPayload, 0, want)
	timeout := time.After(2 * time.Second)
	for len(events) < want {
		select {
		case msg := <-channel:
			if msg == nil {
				continue
			}
			var envelope struct {
				Cmd     string          `json:"cmd"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
				t.Fatalf("unmarshal forwarded envelope error: %v", err)
			}
			if envelope.Cmd != "_agent_api_delegate_event" {
				continue
			}

			var event wsagentapi.DelegateEventPayload
			if err := json.Unmarshal(envelope.Payload, &event); err != nil {
				t.Fatalf("unmarshal forwarded event payload error: %v", err)
			}
			events = append(events, event)
		case <-timeout:
			t.Fatalf("timed out waiting for %d forwarded agent event(s), got=%d", want, len(events))
		}
	}
	return events
}

func collectForwardedLocalActions(
	t *testing.T,
	channel <-chan *redis.Message,
	want int,
) []struct {
	AgentID int64
	Action  protocol.LocalActionPayload
} {
	t.Helper()

	actions := make([]struct {
		AgentID int64
		Action  protocol.LocalActionPayload
	}, 0, want)
	timeout := time.After(2 * time.Second)
	for len(actions) < want {
		select {
		case msg := <-channel:
			if msg == nil {
				continue
			}
			var envelope struct {
				Cmd     string          `json:"cmd"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
				t.Fatalf("unmarshal forwarded envelope error: %v", err)
			}
			if envelope.Cmd != "_agent_api_local_action_forward" {
				continue
			}
			var payload struct {
				AgentID int64                       `json:"agent_id,string"`
				Action  protocol.LocalActionPayload `json:"action"`
			}
			if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
				t.Fatalf("unmarshal forwarded local_action payload error: %v", err)
			}
			actions = append(actions, struct {
				AgentID int64
				Action  protocol.LocalActionPayload
			}{AgentID: payload.AgentID, Action: payload.Action})
		case <-timeout:
			t.Fatalf("timed out waiting for %d forwarded local_action(s), got=%d", want, len(actions))
		}
	}
	return actions
}

func assertNoMoreForwardedAgentEvents(t *testing.T, channel <-chan *redis.Message) {
	t.Helper()

	select {
	case msg := <-channel:
		if msg == nil {
			return
		}
		var envelope struct {
			Cmd string `json:"cmd"`
		}
		if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
			t.Fatalf("unmarshal extra forwarded envelope error: %v", err)
		}
		if envelope.Cmd == "_agent_api_delegate_event" {
			t.Fatalf("unexpected extra forwarded agent event: %s", msg.Payload)
		}
	case <-time.After(150 * time.Millisecond):
	}
}

func assertNoMoreForwardedLocalActions(t *testing.T, channel <-chan *redis.Message) {
	t.Helper()

	select {
	case msg := <-channel:
		if msg == nil {
			return
		}
		var envelope struct {
			Cmd string `json:"cmd"`
		}
		if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
			t.Fatalf("unmarshal extra forwarded envelope error: %v", err)
		}
		if envelope.Cmd == "_agent_api_local_action_forward" {
			t.Fatalf("unexpected extra forwarded local_action: %s", msg.Payload)
		}
	case <-time.After(150 * time.Millisecond):
	}
}

func requireForwardedAgentEventByOwner(
	t *testing.T,
	events []wsagentapi.DelegateEventPayload,
	ownerID int64,
) wsagentapi.DelegateEventPayload {
	t.Helper()

	var matched *wsagentapi.DelegateEventPayload
	for i := range events {
		if events[i].OwnerID != ownerID {
			continue
		}
		if matched != nil {
			t.Fatalf("found multiple forwarded agent events for owner=%d in %#v", ownerID, events)
		}
		matched = &events[i]
	}
	if matched == nil {
		t.Fatalf("missing forwarded agent event for owner=%d in %#v", ownerID, events)
	}
	return *matched
}

func parseIntStr(t *testing.T, s string) int64 {
	t.Helper()
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatalf("parse mention id %q: %v", s, err)
	}
	return v
}

func assertMentionIDsEqual(t *testing.T, got []int64, want ...int64) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("mention_user_ids=%v want=%v", got, want)
	}
	for _, id := range want {
		if !containsInt64(got, id) {
			t.Fatalf("mention_user_ids=%v should contain %d", got, id)
		}
	}
}

func TestHandleSendMsgGroupMentionDelegatedOwnerSuppressesDirectRouteForSameAgent(t *testing.T) {
	fixture := setupDualRoleGroupFixture(t, "session-dual-role-owner-only", 8701, 8702, 9701)
	defer fixture.cleanup()

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "dual-role-owner-only",
		MsgType:     1,
		Content:     fmt.Sprintf("@owner_%d 请你看一下", fixture.ownerID),
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, pkt)

	if countSentCmd(fixture.senderConn.sent, protocol.CmdSendAck) != 1 ||
		countSentCmd(fixture.senderConn.sent, protocol.CmdPushMsg) != 1 {
		t.Fatalf("sender should receive one send_ack and one push_msg, got=%#v", fixture.senderConn.sent)
	}
	if len(fixture.ownerConn.sent) != 1 || fixture.ownerConn.sent[0].cmd != protocol.CmdPushMsg {
		t.Fatalf("owner should receive only push_msg, got=%#v", fixture.ownerConn.sent)
	}

	events := collectForwardedAgentEvents(t, fixture.channel, 1)
	event := events[0]
	if event.AgentID != fixture.agentID {
		t.Fatalf("agent_id=%d want=%d", event.AgentID, fixture.agentID)
	}
	if event.OwnerID != fixture.ownerID {
		t.Fatalf("owner_id=%d want=%d", event.OwnerID, fixture.ownerID)
	}
	if event.SenderID != fixture.senderID {
		t.Fatalf("sender_id=%d want=%d", event.SenderID, fixture.senderID)
	}
	if event.EventType != "group_mention" {
		t.Fatalf("event_type=%s want=group_mention", event.EventType)
	}
	assertMentionIDsEqual(t, event.MentionUserIDs, fixture.ownerID)
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestHandleSendMsgQuotedOwnerSuppressesDirectRouteForSameAgent(t *testing.T) {
	fixture := setupDualRoleGroupFixture(t, "session-dual-role-quoted-owner", 8706, 8707, 9706)
	defer fixture.cleanup()

	const quotedMessageID = int64(18889990481)
	if err := store.DB.Create(&model.Message{
		MsgID:      quotedMessageID,
		SessionID:  fixture.sessionID,
		SenderID:   fixture.ownerID,
		SenderType: 1,
		MsgType:    1,
		Content:    "先看这条旧消息",
		CreatedAt:  time.Now().UTC().Add(-time.Second),
	}).Error; err != nil {
		t.Fatalf("create quoted message error: %v", err)
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:       fixture.sessionID,
		ClientMsgID:     "dual-role-quoted-owner",
		MsgType:         1,
		Content:         "我接着这条引用说",
		QuotedMessageID: quotedMessageID,
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, pkt)

	if countSentCmd(fixture.senderConn.sent, protocol.CmdSendAck) != 1 ||
		countSentCmd(fixture.senderConn.sent, protocol.CmdPushMsg) != 1 {
		t.Fatalf("sender should receive one send_ack and one push_msg, got=%#v", fixture.senderConn.sent)
	}
	if len(fixture.ownerConn.sent) != 1 || fixture.ownerConn.sent[0].cmd != protocol.CmdPushMsg {
		t.Fatalf("owner should receive only push_msg, got=%#v", fixture.ownerConn.sent)
	}

	events := collectForwardedAgentEvents(t, fixture.channel, 1)
	event := events[0]
	if event.AgentID != fixture.agentID {
		t.Fatalf("agent_id=%d want=%d", event.AgentID, fixture.agentID)
	}
	if event.OwnerID != fixture.ownerID {
		t.Fatalf("owner_id=%d want=%d", event.OwnerID, fixture.ownerID)
	}
	if event.SenderID != fixture.senderID {
		t.Fatalf("sender_id=%d want=%d", event.SenderID, fixture.senderID)
	}
	if event.EventType != "group_mention" {
		t.Fatalf("event_type=%s want=group_mention", event.EventType)
	}
	assertMentionIDsEqual(t, event.MentionUserIDs, fixture.ownerID)
	assertNoMoreForwardedAgentEvents(t, fixture.channel)

	ack, ok := findSendAck(fixture.senderConn.sent)
	if !ok {
		t.Fatalf("sender should include SendAckPayload, got=%#v", fixture.senderConn.sent)
	}
	var msg model.Message
	if err := store.DB.Where("session_id = ? AND msg_id = ?", fixture.sessionID, ack.MsgID).
		First(&msg).Error; err != nil {
		t.Fatalf("load message error: %v", err)
	}
	var extra map[string]any
	if err := json.Unmarshal(msg.Extra, &extra); err != nil {
		t.Fatalf("unmarshal message extra error: %v", err)
	}
	rawMentions, ok := extra["mention_user_ids"].([]any)
	if !ok || len(rawMentions) != 1 {
		t.Fatalf("mention_user_ids invalid: %#v", extra["mention_user_ids"])
	}
	gotMentionIDs := []int64{parseIntStr(t, rawMentions[0].(string))}
	assertMentionIDsEqual(t, gotMentionIDs, fixture.ownerID)
}

func TestHandleSendMsgQuotedOwnerSkippedWhenAgentExplicitlyMentioned(t *testing.T) {
	fixture := setupDualRoleGroupFixture(t, "session-dual-role-quoted-owner-explicit-agent", 8708, 8709, 9708)
	defer fixture.cleanup()

	const quotedMessageID = int64(18889990482)
	if err := store.DB.Create(&model.Message{
		MsgID:      quotedMessageID,
		SessionID:  fixture.sessionID,
		SenderID:   fixture.ownerID,
		SenderType: 1,
		MsgType:    1,
		Content:    "先看这条旧消息",
		CreatedAt:  time.Now().UTC().Add(-time.Second),
	}).Error; err != nil {
		t.Fatalf("create quoted message error: %v", err)
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:       fixture.sessionID,
		ClientMsgID:     "dual-role-quoted-owner-explicit-agent",
		MsgType:         1,
		Content:         fmt.Sprintf("@DelegateBot%d 你来直接回答", fixture.agentID),
		QuotedMessageID: quotedMessageID,
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, pkt)

	if countSentCmd(fixture.senderConn.sent, protocol.CmdSendAck) != 1 ||
		countSentCmd(fixture.senderConn.sent, protocol.CmdPushMsg) != 1 {
		t.Fatalf("sender should receive one send_ack and one push_msg, got=%#v", fixture.senderConn.sent)
	}
	if len(fixture.ownerConn.sent) != 1 || fixture.ownerConn.sent[0].cmd != protocol.CmdPushMsg {
		t.Fatalf("owner should receive only push_msg, got=%#v", fixture.ownerConn.sent)
	}

	events := collectForwardedAgentEvents(t, fixture.channel, 1)
	directEvent := requireForwardedAgentEventByOwner(t, events, fixture.ownerID)
	if directEvent.AgentID != fixture.agentID {
		t.Fatalf("agent_id=%d want=%d", directEvent.AgentID, fixture.agentID)
	}
	if directEvent.SenderID != fixture.senderID {
		t.Fatalf("sender_id=%d want=%d", directEvent.SenderID, fixture.senderID)
	}
	if directEvent.QuotedMessageID != quotedMessageID {
		t.Fatalf("quoted_message_id=%d want=%d", directEvent.QuotedMessageID, quotedMessageID)
	}
	if directEvent.EventType != "group_mention" {
		t.Fatalf("event_type=%s want=group_mention", directEvent.EventType)
	}
	if directEvent.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("direct mirror_mode=%q want=%q", directEvent.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
	}
	assertMentionIDsEqual(t, directEvent.MentionUserIDs, fixture.agentID)

	assertNoMoreForwardedAgentEvents(t, fixture.channel)

	ack, ok := findSendAck(fixture.senderConn.sent)
	if !ok {
		t.Fatalf("sender should include SendAckPayload, got=%#v", fixture.senderConn.sent)
	}
	var msg model.Message
	if err := store.DB.Where("session_id = ? AND msg_id = ?", fixture.sessionID, ack.MsgID).
		First(&msg).Error; err != nil {
		t.Fatalf("load message error: %v", err)
	}
	var extra map[string]any
	if err := json.Unmarshal(msg.Extra, &extra); err != nil {
		t.Fatalf("unmarshal message extra error: %v", err)
	}
	rawMentions, ok := extra["mention_user_ids"].([]any)
	if !ok || len(rawMentions) != 1 {
		t.Fatalf("mention_user_ids invalid: %#v", extra["mention_user_ids"])
	}
	gotMentionIDs := []int64{parseIntStr(t, rawMentions[0].(string))}
	assertMentionIDsEqual(t, gotMentionIDs, fixture.agentID)
}

// 群事件 OwnerID 路由口径守卫(防回归):
// 群里非主人成员发消息触发 agent,direct event 的 OwnerID 必须是 agent.OwnerID,
// 不能是 senderID。理由:agent 在群里代主人响应,事件要路由到主连接;若设成
// senderID(发送者),会让 lookupConnByOwner 找不到连接,事件入死队列→agent 没反应。
// (历史故障 2026-06-23 hotfix bdebc788/47aad232 → 根因修复 见 commit msg)
// 反向验证:把 direct_session_route.go 群分支 OwnerID 改回 senderID,本测试必 FAIL。
func TestHandleSendMsgGroupRoute_OwnerIDMustBeAgentOwnerNotSender(t *testing.T) {
	// dualRoleGroupFixture: senderID=8881(消息发送者), ownerID=8882(agent 主人),
	// agentID=9881. agent 在群里被 @ 触发。
	fixture := setupDualRoleGroupFixture(t, "group-route-owner-guard", 8881, 8882, 9881)
	defer fixture.cleanup()

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "group-route-owner-guard",
		MsgType:     1,
		Content:     fmt.Sprintf("@DelegateBot%d 群里非主人触发", fixture.agentID),
	})
	HandleSendMsg(fixture.hub, fixture.senderConn, pkt)

	events := collectForwardedAgentEvents(t, fixture.channel, 1)
	directIDInfix := fmt.Sprintf(":%d:", fixture.agentID)
	var direct *wsagentapi.DelegateEventPayload
	for i := range events {
		if strings.Contains(events[i].EventID, directIDInfix) {
			direct = &events[i]
			break
		}
	}
	if direct == nil {
		t.Fatalf("expected a direct event in %#v", events)
	}
	// 核心断言:OwnerID 必须是 agent.OwnerID(fixture.ownerID),不能是 senderID(fixture.senderID)。
	if direct.OwnerID != fixture.ownerID {
		t.Fatalf("群 direct event OwnerID=%d, want %d (agent.OwnerID); 若 owner_id=%d(senderID) 说明 direct_session_route 把 OwnerID 设成 senderID,会复现「群里非主人发消息没反应」",
			direct.OwnerID, fixture.ownerID, fixture.senderID)
	}
	if direct.SenderID != fixture.senderID {
		t.Fatalf("SenderID 字段必须保留原 sender,got=%d want=%d", direct.SenderID, fixture.senderID)
	}
}

func TestHandleSendMsgGroupMentionAgentDispatchesDirectRoleOnly(t *testing.T) {
	fixture := setupDualRoleGroupFixture(t, "session-dual-role-agent-only", 8711, 8712, 9711)
	defer fixture.cleanup()

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:       fixture.sessionID,
		ClientMsgID:     "dual-role-agent-only",
		MsgType:         1,
		Content:         fmt.Sprintf("@DelegateBot%d 你来直接回答", fixture.agentID),
		QuotedMessageID: 18889990222,
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, pkt)

	if countSentCmd(fixture.senderConn.sent, protocol.CmdSendAck) != 1 ||
		countSentCmd(fixture.senderConn.sent, protocol.CmdPushMsg) != 1 {
		t.Fatalf("sender should receive one send_ack and one push_msg, got=%#v", fixture.senderConn.sent)
	}
	if len(fixture.ownerConn.sent) != 1 || fixture.ownerConn.sent[0].cmd != protocol.CmdPushMsg {
		t.Fatalf("owner should receive only push_msg, got=%#v", fixture.ownerConn.sent)
	}

	events := collectForwardedAgentEvents(t, fixture.channel, 1)
	directEvent := requireForwardedAgentEventByOwner(t, events, fixture.ownerID)
	if directEvent.AgentID != fixture.agentID {
		t.Fatalf("agent_id=%d want=%d", directEvent.AgentID, fixture.agentID)
	}
	if directEvent.SenderID != fixture.senderID {
		t.Fatalf("sender_id=%d want=%d", directEvent.SenderID, fixture.senderID)
	}
	if directEvent.QuotedMessageID != 18889990222 {
		t.Fatalf("quoted_message_id=%d want=%d", directEvent.QuotedMessageID, int64(18889990222))
	}
	if directEvent.EventType != "group_mention" {
		t.Fatalf("event_type=%s want=group_mention", directEvent.EventType)
	}
	if directEvent.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("direct mirror_mode=%q want=%q", directEvent.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
	}
	assertMentionIDsEqual(t, directEvent.MentionUserIDs, fixture.agentID)

	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestHandleSendMsgDirectAgentConsumesQueuedVisibleHistoryOnce(t *testing.T) {
	fixture := setupDualRoleGroupFixture(t, "session-direct-agent-visible-backlog", 8713, 8714, 9713)
	defer fixture.cleanup()

	const otherID = int64(8715)
	if err := store.DB.Create(&model.User{
		ID:       otherID,
		Username: fmt.Sprintf("other_%d", otherID),
		Email:    fmt.Sprintf("other_%d@example.com", otherID),
		Nickname: "other-nick",
	}).Error; err != nil {
		t.Fatalf("create other user error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{
		SessionID:    fixture.sessionID,
		MemberID:     otherID,
		MemberType:   1,
		JoinedAt:     time.Now().UTC(),
		LastActiveAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("create other session member error: %v", err)
	}

	firstPkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "direct-agent-visible-backlog-first",
		MsgType:     1,
		Content:     fmt.Sprintf("@other_%d 这句先交给你", otherID),
		Extra:       json.RawMessage(fmt.Sprintf(`{"mention_user_ids":["%d"]}`, otherID)),
	})
	HandleSendMsg(fixture.hub, fixture.senderConn, firstPkt)
	firstEvents := collectForwardedAgentEvents(t, fixture.channel, 1)
	firstDirectMirror := requireForwardedAgentEventByOwner(t, firstEvents, fixture.ownerID)
	if firstDirectMirror.MirrorMode != wsagentapi.MirrorModeRecordOnly {
		t.Fatalf("first direct mirror_mode=%q want=%q", firstDirectMirror.MirrorMode, wsagentapi.MirrorModeRecordOnly)
	}
	if firstDirectMirror.AgentID != fixture.agentID {
		t.Fatalf("first direct agent_id=%d want=%d", firstDirectMirror.AgentID, fixture.agentID)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)

	fixture.senderConn.sent = nil
	fixture.ownerConn.sent = nil

	secondPkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "direct-agent-visible-backlog-second",
		MsgType:     1,
		Content:     fmt.Sprintf("@DelegateBot%d 现在轮到你", fixture.agentID),
	})
	HandleSendMsg(fixture.hub, fixture.senderConn, secondPkt)

	secondEvents := collectForwardedAgentEvents(t, fixture.channel, 1)
	secondDirectEvent := requireForwardedAgentEventByOwner(t, secondEvents, fixture.ownerID)
	if secondDirectEvent.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("second direct mirror_mode=%q want=%q", secondDirectEvent.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
	}
	if len(secondDirectEvent.ContextMessages) != 2 {
		t.Fatalf("second direct context_messages=%#v want=2 items", secondDirectEvent.ContextMessages)
	}
	if secondDirectEvent.ContextMessages[0].Content != fmt.Sprintf("@other_%d 这句先交给你", otherID) {
		t.Fatalf("first context content=%q", secondDirectEvent.ContextMessages[0].Content)
	}
	if secondDirectEvent.ContextMessages[1].Content != fmt.Sprintf("@DelegateBot%d 现在轮到你", fixture.agentID) {
		t.Fatalf("second context content=%q", secondDirectEvent.ContextMessages[1].Content)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)

	fixture.senderConn.sent = nil
	fixture.ownerConn.sent = nil

	thirdPkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "direct-agent-visible-backlog-third",
		MsgType:     1,
		Content:     fmt.Sprintf("@DelegateBot%d 这次只带新的", fixture.agentID),
	})
	HandleSendMsg(fixture.hub, fixture.senderConn, thirdPkt)

	thirdEvents := collectForwardedAgentEvents(t, fixture.channel, 1)
	thirdDirectEvent := requireForwardedAgentEventByOwner(t, thirdEvents, fixture.ownerID)
	if thirdDirectEvent.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("third direct mirror_mode=%q want=%q", thirdDirectEvent.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
	}
	if len(thirdDirectEvent.ContextMessages) != 1 {
		t.Fatalf("third context_messages=%#v want=1 item", thirdDirectEvent.ContextMessages)
	}
	if thirdDirectEvent.ContextMessages[0].Content != fmt.Sprintf("@DelegateBot%d 这次只带新的", fixture.agentID) {
		t.Fatalf("third context content=%q", thirdDirectEvent.ContextMessages[0].Content)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestHandleSendMsgGroupContinuationKeepsDirectAgentTargetWithoutMention(t *testing.T) {
	fixture := setupDualRoleGroupFixture(t, "session-dual-role-agent-continuation", 8716, 8717, 9716)
	defer fixture.cleanup()

	firstPkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "dual-role-agent-continuation-first",
		MsgType:     1,
		Content:     fmt.Sprintf("@DelegateBot%d 先听我说", fixture.agentID),
	})
	HandleSendMsg(fixture.hub, fixture.senderConn, firstPkt)

	firstEvents := collectForwardedAgentEvents(t, fixture.channel, 1)
	firstDirectEvent := requireForwardedAgentEventByOwner(t, firstEvents, fixture.ownerID)
	if firstDirectEvent.EventType != "group_mention" {
		t.Fatalf("first direct event_type=%s want=group_mention", firstDirectEvent.EventType)
	}
	if firstDirectEvent.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("first direct mirror_mode=%q want=%q", firstDirectEvent.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
	}
	assertMentionIDsEqual(t, firstDirectEvent.MentionUserIDs, fixture.agentID)
	assertNoMoreForwardedAgentEvents(t, fixture.channel)

	fixture.senderConn.sent = nil
	fixture.ownerConn.sent = nil

	secondPkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "dual-role-agent-continuation-second",
		MsgType:     1,
		Content:     "第二句不再带@，还是继续问你",
	})
	HandleSendMsg(fixture.hub, fixture.senderConn, secondPkt)

	secondEvents := collectForwardedAgentEvents(t, fixture.channel, 1)
	directEvent := requireForwardedAgentEventByOwner(t, secondEvents, fixture.ownerID)
	if directEvent.AgentID != fixture.agentID {
		t.Fatalf("agent_id=%d want=%d", directEvent.AgentID, fixture.agentID)
	}
	if directEvent.EventType != "group_message" {
		t.Fatalf("event_type=%s want=group_message", directEvent.EventType)
	}
	if len(directEvent.MentionUserIDs) != 0 {
		t.Fatalf("mention_user_ids=%v want=[]", directEvent.MentionUserIDs)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestHandleSendMsgGroupContinuationKeepsDirectAgentTargetAcrossDifferentSenderAfterAgentReply(t *testing.T) {
	fixture := setupDualRoleGroupFixture(t, "session-dual-role-agent-cross-sender", 87126, 87127, 97126)
	defer fixture.cleanup()

	const agentReplyMsgID = int64(18889990492)
	seedGroupLastMessage(t, fixture.sessionID, model.Message{
		MsgID:      agentReplyMsgID,
		SessionID:  fixture.sessionID,
		SenderID:   fixture.agentID,
		SenderType: 2,
		MsgType:    1,
		Content:    "我先说完，大家接着聊",
		CreatedAt:  time.Now().UTC().Add(-time.Second),
	})

	fixture.senderConn.sent = nil
	fixture.ownerConn.sent = nil

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "dual-role-agent-cross-sender",
		MsgType:     1,
		Content:     "我补一句，不@也不引用",
	})
	HandleSendMsg(fixture.hub, fixture.ownerConn, pkt)

	events := collectForwardedAgentEvents(t, fixture.channel, 1)
	event := events[0]
	if event.AgentID != fixture.agentID {
		t.Fatalf("agent_id=%d want=%d", event.AgentID, fixture.agentID)
	}
	if event.OwnerID != fixture.ownerID {
		t.Fatalf("owner_id=%d want=%d", event.OwnerID, fixture.ownerID)
	}
	if event.EventType != "group_message" {
		t.Fatalf("event_type=%s want=group_message", event.EventType)
	}
	if event.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("mirror_mode=%q want=%q", event.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
	}
	if len(event.MentionUserIDs) != 0 {
		t.Fatalf("mention_user_ids=%v want=[]", event.MentionUserIDs)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestHandleSendMsgGroupProprietaryAgentRepliesToContinuationAfterAgentReply(t *testing.T) {
	fixture := setupDualRoleGroupFixture(t, "session-dual-role-proprietary-continuation", 87136, 87137, 97136)
	defer fixture.cleanup()

	// Mark the agent as a proprietary client (e.g. Claude): in groups these
	// agents only respond to explicit @mention, EXCEPT for a directed
	// continuation right after they spoke.
	if err := store.DB.Model(&model.Agent{}).
		Where("id = ?", fixture.agentID).
		Update("agent_client_type", model.AgentClientTypeCodex).Error; err != nil {
		t.Fatalf("set agent client type error: %v", err)
	}

	const agentReplyMsgID = int64(18889990493)
	seedGroupLastMessage(t, fixture.sessionID, model.Message{
		MsgID:      agentReplyMsgID,
		SessionID:  fixture.sessionID,
		SenderID:   fixture.agentID,
		SenderType: 2,
		MsgType:    1,
		Content:    "我先回一句，你接着说",
		CreatedAt:  time.Now().UTC().Add(-time.Second),
	})

	fixture.senderConn.sent = nil
	fixture.ownerConn.sent = nil

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "dual-role-proprietary-continuation",
		MsgType:     1,
		Content:     "我直接往下问，不@你",
	})
	HandleSendMsg(fixture.hub, fixture.senderConn, pkt)

	events := collectForwardedAgentEvents(t, fixture.channel, 1)
	event := requireForwardedAgentEventByOwner(t, events, fixture.ownerID)
	if event.AgentID != fixture.agentID {
		t.Fatalf("agent_id=%d want=%d", event.AgentID, fixture.agentID)
	}
	if event.EventType != "group_message" {
		t.Fatalf("event_type=%s want=group_message", event.EventType)
	}
	if event.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("mirror_mode=%q want=%q", event.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
	}
	if len(event.MentionUserIDs) != 0 {
		t.Fatalf("mention_user_ids=%v want=[]", event.MentionUserIDs)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestHandleSendMsgGroupProprietaryMentionOnlyAgentRepliesToContinuationAfterAgentReply(t *testing.T) {
	fixture := setupDualRoleGroupFixture(t, "session-dual-role-proprietary-mention-only-continuation", 87138, 87139, 97138)
	defer fixture.cleanup()

	// Same as TestHandleSendMsgGroupProprietaryAgentRepliesToContinuationAfterAgentReply,
	// but the agent's own receive mode is explicitly ModeMentionOnly. The
	// receive-policy check must recognize a directed continuation the same way
	// the proprietary @-only bypass above it does, or the message is silently
	// dropped even though the agent was correctly identified as the target.
	if err := store.DB.Model(&model.Agent{}).
		Where("id = ?", fixture.agentID).
		Update("agent_client_type", model.AgentClientTypeCodex).Error; err != nil {
		t.Fatalf("set agent client type error: %v", err)
	}
	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = 2", fixture.sessionID, fixture.agentID).
		Update("agent_receive_mode", agentreceive.ModeMentionOnly).Error; err != nil {
		t.Fatalf("update mention-only mode error: %v", err)
	}

	const agentReplyMsgID = int64(18889990495)
	seedGroupLastMessage(t, fixture.sessionID, model.Message{
		MsgID:      agentReplyMsgID,
		SessionID:  fixture.sessionID,
		SenderID:   fixture.agentID,
		SenderType: 2,
		MsgType:    1,
		Content:    "我先回一句，你接着说",
		CreatedAt:  time.Now().UTC().Add(-time.Second),
	})

	fixture.senderConn.sent = nil
	fixture.ownerConn.sent = nil

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "dual-role-proprietary-mention-only-continuation",
		MsgType:     1,
		Content:     "我直接往下问，不@你",
	})
	HandleSendMsg(fixture.hub, fixture.senderConn, pkt)

	events := collectForwardedAgentEvents(t, fixture.channel, 1)
	event := requireForwardedAgentEventByOwner(t, events, fixture.ownerID)
	if event.AgentID != fixture.agentID {
		t.Fatalf("agent_id=%d want=%d", event.AgentID, fixture.agentID)
	}
	if event.EventType != "group_message" {
		t.Fatalf("event_type=%s want=group_message", event.EventType)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestHandleSendMsgGroupGenericMentionOnlyAgentRepliesToContinuationAfterOwnReply(t *testing.T) {
	fixture := setupDualRoleGroupFixture(t, "session-dual-role-generic-mention-only-continuation", 87140, 87141, 97140)
	defer fixture.cleanup()

	// This agent is NOT a proprietary client type (agent_client_type left at
	// its default). A directed single-agent continuation — a human replying
	// right after this agent spoke — counts as addressing it regardless of
	// client type, so ModeMentionOnly must dispatch it the same way an explicit
	// @mention would. ModeMentionOnly still blocks unrelated group chatter
	// (covered by the no-continuation test below).
	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = 2", fixture.sessionID, fixture.agentID).
		Update("agent_receive_mode", agentreceive.ModeMentionOnly).Error; err != nil {
		t.Fatalf("update mention-only mode error: %v", err)
	}

	const agentReplyMsgID = int64(18889990496)
	seedGroupLastMessage(t, fixture.sessionID, model.Message{
		MsgID:      agentReplyMsgID,
		SessionID:  fixture.sessionID,
		SenderID:   fixture.agentID,
		SenderType: 2,
		MsgType:    1,
		Content:    "我先回一句，你接着说",
		CreatedAt:  time.Now().UTC().Add(-time.Second),
	})

	fixture.senderConn.sent = nil
	fixture.ownerConn.sent = nil

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "dual-role-generic-mention-only-continuation",
		MsgType:     1,
		Content:     "我直接往下问，不@你",
	})
	HandleSendMsg(fixture.hub, fixture.senderConn, pkt)

	events := collectForwardedAgentEvents(t, fixture.channel, 1)
	event := requireForwardedAgentEventByOwner(t, events, fixture.ownerID)
	if event.AgentID != fixture.agentID {
		t.Fatalf("agent_id=%d want=%d", event.AgentID, fixture.agentID)
	}
	if event.EventType != "group_message" {
		t.Fatalf("event_type=%s want=group_message", event.EventType)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestHandleSendMsgGroupGenericMentionOnlyAgentIgnoresPlainMessageWithoutContinuation(t *testing.T) {
	fixture := setupDualRoleGroupFixture(t, "session-dual-role-generic-mention-only-no-continuation", 87142, 87143, 97142)
	defer fixture.cleanup()

	// ModeMentionOnly on a generic agent still blocks plain group chatter when
	// there is no continuation signal: the last session message is from a human,
	// so an un-@'d message must not reach the agent.
	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = 2", fixture.sessionID, fixture.agentID).
		Update("agent_receive_mode", agentreceive.ModeMentionOnly).Error; err != nil {
		t.Fatalf("update mention-only mode error: %v", err)
	}

	const humanMsgID = int64(18889990497)
	seedGroupLastMessage(t, fixture.sessionID, model.Message{
		MsgID:      humanMsgID,
		SessionID:  fixture.sessionID,
		SenderID:   fixture.ownerID,
		SenderType: 1,
		MsgType:    1,
		Content:    "我们人类先随便聊聊",
		CreatedAt:  time.Now().UTC().Add(-time.Second),
	})

	fixture.senderConn.sent = nil
	fixture.ownerConn.sent = nil

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "dual-role-generic-mention-only-no-continuation",
		MsgType:     1,
		Content:     "继续聊，不@任何agent",
	})
	HandleSendMsg(fixture.hub, fixture.senderConn, pkt)

	// The agent may still receive a record-only mirror of the group message,
	// but nothing that would make it respond (record_and_process).
	events := collectForwardedAgentEvents(t, fixture.channel, 1)
	event := requireForwardedAgentEventByOwner(t, events, fixture.ownerID)
	if event.MirrorMode != wsagentapi.MirrorModeRecordOnly {
		t.Fatalf("mirror_mode=%q want=%q", event.MirrorMode, wsagentapi.MirrorModeRecordOnly)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestHandleSendMsgGroupProprietaryAgentIgnoresPlainMessageWithoutContinuation(t *testing.T) {
	fixture := setupDualRoleGroupFixture(t, "session-dual-role-proprietary-no-continuation", 87146, 87147, 97146)
	defer fixture.cleanup()

	if err := store.DB.Model(&model.Agent{}).
		Where("id = ?", fixture.agentID).
		Update("agent_client_type", model.AgentClientTypeCodex).Error; err != nil {
		t.Fatalf("set agent client type error: %v", err)
	}

	// Last speaker is a human, so there is no directed continuation toward the
	// proprietary agent. A plain @-less group message must NOT pull it in.
	const humanMsgID = int64(18889990494)
	seedGroupLastMessage(t, fixture.sessionID, model.Message{
		MsgID:      humanMsgID,
		SessionID:  fixture.sessionID,
		SenderID:   fixture.ownerID,
		SenderType: 1,
		MsgType:    1,
		Content:    "大家随便聊聊",
		CreatedAt:  time.Now().UTC().Add(-time.Second),
	})

	fixture.senderConn.sent = nil
	fixture.ownerConn.sent = nil

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "dual-role-proprietary-no-continuation",
		MsgType:     1,
		Content:     "我说点别的，不@任何人",
	})
	HandleSendMsg(fixture.hub, fixture.senderConn, pkt)

	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

// 审批回传必达：审批卡按钮发出的是一条普通群消息，既没有 @、也不构成接续，
// 之前会被群聊 @-only 过滤挡下，发卡的 agent 永远等不到结果，卡片卡在「提交中」。
// 注：审批指令进入 PushDelegateEvent 后由审批拦截器消费成 local_action 下发，
// 因此这里断言的边界是「路由是否把发卡 agent 选为直投目标」。
func TestResolveDirectRouteApprovalResolutionReachesIssuerWithoutMention(t *testing.T) {
	fixture := setupDualRoleGroupFixture(t, "session-dual-role-approval-resolution", 87148, 87149, 97148)
	defer fixture.cleanup()

	if err := store.DB.Model(&model.Agent{}).
		Where("id = ?", fixture.agentID).
		Update("agent_client_type", model.AgentClientTypeCodex).Error; err != nil {
		t.Fatalf("set agent client type error: %v", err)
	}

	// 最后发言的是人类：不存在指向 agent 的接续（对照上一个用例，普通消息进不来）。
	seedGroupLastMessage(t, fixture.sessionID, model.Message{
		MsgID:      18889990495,
		SessionID:  fixture.sessionID,
		SenderID:   fixture.ownerID,
		SenderType: 1,
		MsgType:    1,
		Content:    "先聊点别的",
		CreatedAt:  time.Now().UTC().Add(-time.Second),
	})

	const approvalID = "perm-1784010723812-zkxkxj"
	wsagentapi.SaveApprovalIssuer(context.Background(), fixture.agentID, fixture.sessionID, approvalID)

	route := requireDirectRoute(t, fixture, approvalResolutionContent(approvalID), 18889990500)
	if len(route.Targets) != 1 {
		t.Fatalf("targets=%d want=1", len(route.Targets))
	}
	target := route.Targets[0]
	if target.Agent.ID != fixture.agentID {
		t.Fatalf("target agent=%d want=%d", target.Agent.ID, fixture.agentID)
	}
	// 审批事件会被拦截器消费成 local_action，agent 看不到 context_messages。
	// 若在这里挂上下文并清缓冲，agent 攒的群聊未读就被静默丢掉了。
	if target.ClearBufferOnAccept {
		t.Fatalf("clear_buffer_on_accept=true want=false: approval must not consume the agent's unread backlog")
	}
	if len(target.ContextMessages) != 0 {
		t.Fatalf("context_messages=%d want=0: approval must not carry the agent's unread backlog", len(target.ContextMessages))
	}
	if target.Mentioned {
		t.Fatalf("mentioned=true want=false: approval resolution is not an @mention")
	}
}

// 普通群消息（非审批指令）在同样条件下仍被过滤：豁免只对审批回传生效。
func TestResolveDirectRoutePlainMessageStaysFilteredWithoutMention(t *testing.T) {
	fixture := setupDualRoleGroupFixture(t, "session-dual-role-approval-plain", 87152, 87153, 97152)
	defer fixture.cleanup()

	if err := store.DB.Model(&model.Agent{}).
		Where("id = ?", fixture.agentID).
		Update("agent_client_type", model.AgentClientTypeCodex).Error; err != nil {
		t.Fatalf("set agent client type error: %v", err)
	}

	seedGroupLastMessage(t, fixture.sessionID, model.Message{
		MsgID:      18889990501,
		SessionID:  fixture.sessionID,
		SenderID:   fixture.ownerID,
		SenderType: 1,
		MsgType:    1,
		Content:    "先聊点别的",
		CreatedAt:  time.Now().UTC().Add(-time.Second),
	})

	wsagentapi.SaveApprovalIssuer(context.Background(), fixture.agentID, fixture.sessionID, "perm-plain")

	route := requireDirectRoute(t, fixture, "我说点别的，不@任何人", 18889990502)
	if route != nil && len(route.Targets) != 0 {
		t.Fatalf("targets=%d want=0", len(route.Targets))
	}
}

// 没有发卡记录的审批指令不得成为绕过群聊门禁的后门：伪造文本不能把 agent 拉出来。
func TestResolveDirectRouteApprovalResolutionWithoutIssuerStaysFiltered(t *testing.T) {
	fixture := setupDualRoleGroupFixture(t, "session-dual-role-approval-no-issuer", 87150, 87151, 97150)
	defer fixture.cleanup()

	if err := store.DB.Model(&model.Agent{}).
		Where("id = ?", fixture.agentID).
		Update("agent_client_type", model.AgentClientTypeCodex).Error; err != nil {
		t.Fatalf("set agent client type error: %v", err)
	}

	seedGroupLastMessage(t, fixture.sessionID, model.Message{
		MsgID:      18889990496,
		SessionID:  fixture.sessionID,
		SenderID:   fixture.ownerID,
		SenderType: 1,
		MsgType:    1,
		Content:    "先聊点别的",
		CreatedAt:  time.Now().UTC().Add(-time.Second),
	})

	route := requireDirectRoute(t, fixture, approvalResolutionContent("perm-does-not-exist"), 18889990503)
	if route != nil && len(route.Targets) != 0 {
		t.Fatalf("targets=%d want=0", len(route.Targets))
	}
}

func approvalResolutionContent(approvalID string) string {
	return fmt.Sprintf(
		"[[exec-approval-resolution|approval_id=%s|approval_command_id=%s|decision=allow-once]]",
		approvalID,
		approvalID,
	)
}

func requireDirectRoute(
	t *testing.T,
	fixture *dualRoleGroupFixture,
	content string,
	triggerMsgID int64,
) *directSessionRoute {
	t.Helper()

	route, err := resolveDirectSessionRoute(
		fixture.sessionID,
		2,
		fixture.senderID,
		1,
		triggerMsgID,
		0,
		1,
		content,
		nil,
		nil,
		nil,
		nil,
		false,
	)
	if err != nil {
		t.Fatalf("resolve direct session route error: %v", err)
	}
	return route
}

func TestHandleSendMsgGroupContinuationKeepsQuotedOwnerTargetWithoutMention(t *testing.T) {
	fixture := setupDualRoleGroupFixture(t, "session-dual-role-owner-continuation", 8718, 8719, 9718)
	defer fixture.cleanup()

	const quotedMessageID = int64(18889990491)
	if err := store.DB.Create(&model.Message{
		MsgID:      quotedMessageID,
		SessionID:  fixture.sessionID,
		SenderID:   fixture.ownerID,
		SenderType: 1,
		MsgType:    1,
		Content:    "引用我来开头",
		CreatedAt:  time.Now().UTC().Add(-time.Second),
	}).Error; err != nil {
		t.Fatalf("create quoted message error: %v", err)
	}

	firstPkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:       fixture.sessionID,
		ClientMsgID:     "dual-role-owner-continuation-first",
		MsgType:         1,
		Content:         "先沿着引用来问",
		QuotedMessageID: quotedMessageID,
	})
	HandleSendMsg(fixture.hub, fixture.senderConn, firstPkt)

	firstEvents := collectForwardedAgentEvents(t, fixture.channel, 1)
	if firstEvents[0].OwnerID != fixture.ownerID {
		t.Fatalf("first owner_id=%d want=%d", firstEvents[0].OwnerID, fixture.ownerID)
	}
	if firstEvents[0].EventType != "group_mention" {
		t.Fatalf("first event_type=%s want=group_mention", firstEvents[0].EventType)
	}
	assertMentionIDsEqual(t, firstEvents[0].MentionUserIDs, fixture.ownerID)
	assertNoMoreForwardedAgentEvents(t, fixture.channel)

	fixture.senderConn.sent = nil
	fixture.ownerConn.sent = nil

	secondPkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "dual-role-owner-continuation-second",
		MsgType:     1,
		Content:     "第二句不引用也不@，但还是接着问你",
	})
	HandleSendMsg(fixture.hub, fixture.senderConn, secondPkt)

	secondEvents := collectForwardedAgentEvents(t, fixture.channel, 1)
	event := secondEvents[0]
	if event.AgentID != fixture.agentID {
		t.Fatalf("agent_id=%d want=%d", event.AgentID, fixture.agentID)
	}
	if event.OwnerID != fixture.ownerID {
		t.Fatalf("owner_id=%d want=%d", event.OwnerID, fixture.ownerID)
	}
	if event.EventType != "group_message" {
		t.Fatalf("event_type=%s want=group_message", event.EventType)
	}
	if len(event.MentionUserIDs) != 0 {
		t.Fatalf("mention_user_ids=%v want=[]", event.MentionUserIDs)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestHandleRetryMsgGroupContinuationUsesSavedTargetSnapshot(t *testing.T) {
	fixture := setupDualRoleGroupFixture(t, "session-dual-role-retry-continuation", 8720, 8721, 9720)
	defer fixture.cleanup()

	firstPkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "dual-role-retry-continuation-first",
		MsgType:     1,
		Content:     fmt.Sprintf("@DelegateBot%d 先开头", fixture.agentID),
	})
	HandleSendMsg(fixture.hub, fixture.senderConn, firstPkt)
	firstEvents := collectForwardedAgentEvents(t, fixture.channel, 1)
	firstDirectEvent := requireForwardedAgentEventByOwner(t, firstEvents, fixture.ownerID)
	if firstDirectEvent.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("first direct mirror_mode=%q want=%q", firstDirectEvent.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)

	fixture.senderConn.sent = nil
	fixture.ownerConn.sent = nil

	secondPkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "dual-role-retry-continuation-second",
		MsgType:     1,
		Content:     "第二句继续，但不再带@",
	})
	HandleSendMsg(fixture.hub, fixture.senderConn, secondPkt)

	secondEvents := collectForwardedAgentEvents(t, fixture.channel, 1)
	secondDirectEvent := requireForwardedAgentEventByOwner(t, secondEvents, fixture.ownerID)
	if secondDirectEvent.EventType != "group_message" {
		t.Fatalf("second event_type=%s want=group_message", secondDirectEvent.EventType)
	}
	if len(secondDirectEvent.MentionUserIDs) != 0 {
		t.Fatalf("second mention_user_ids=%v want=[]", secondDirectEvent.MentionUserIDs)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)

	ack, ok := findSendAck(fixture.senderConn.sent)
	if !ok {
		t.Fatalf("expected send_ack for second message, got=%#v", fixture.senderConn.sent)
	}
	if err := clearGroupContinuationTargetIDs(context.Background(), fixture.sessionID, fixture.senderID); err != nil {
		t.Fatalf("clear continuation target error: %v", err)
	}

	fixture.senderConn.sent = nil
	HandleRetryMsg(fixture.hub, fixture.senderConn, makeRetryMsgPacket(t, protocol.RetryMsgPayload{
		SessionID: fixture.sessionID,
		MsgID:     ack.MsgID,
	}))

	retryAck, ok := findRetryMsgAck(fixture.senderConn.sent)
	if !ok {
		t.Fatalf("expected retry_msg_ack, got=%#v", fixture.senderConn.sent)
	}
	if retryAck.Code != 0 {
		t.Fatalf("retry ack code=%d want=0", retryAck.Code)
	}

	retryEvents := collectForwardedAgentEvents(t, fixture.channel, 1)
	directRetryEvent := requireForwardedAgentEventByOwner(t, retryEvents, fixture.ownerID)
	if directRetryEvent.AgentID != fixture.agentID {
		t.Fatalf("agent_id=%d want=%d", directRetryEvent.AgentID, fixture.agentID)
	}
	if directRetryEvent.EventType != "group_message" {
		t.Fatalf("event_type=%s want=group_message", directRetryEvent.EventType)
	}
	if len(directRetryEvent.MentionUserIDs) != 0 {
		t.Fatalf("mention_user_ids=%v want=[]", directRetryEvent.MentionUserIDs)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestHandleSendMsgOpenGroupCueClearsPreviousContinuationTarget(t *testing.T) {
	fixture := setupDualRoleGroupFixture(t, "session-dual-role-open-group-cue", 8724, 8725, 9724)
	defer fixture.cleanup()

	firstPkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "dual-role-open-group-first",
		MsgType:     1,
		Content:     fmt.Sprintf("@DelegateBot%d 先听我说", fixture.agentID),
	})
	HandleSendMsg(fixture.hub, fixture.senderConn, firstPkt)
	firstEvents := collectForwardedAgentEvents(t, fixture.channel, 1)
	firstDirectEvent := requireForwardedAgentEventByOwner(t, firstEvents, fixture.ownerID)
	if firstDirectEvent.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("first direct mirror_mode=%q want=%q", firstDirectEvent.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)

	secondPkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "dual-role-open-group-second",
		MsgType:     1,
		Content:     "大家怎么看这件事",
	})
	HandleSendMsg(fixture.hub, fixture.senderConn, secondPkt)

	secondEvents := collectForwardedAgentEvents(t, fixture.channel, 2)
	secondDirectFound := false
	secondDelegateFound := false
	for _, event := range secondEvents {
		if event.AgentID != fixture.agentID {
			t.Fatalf("second agent_id=%d want=%d", event.AgentID, fixture.agentID)
		}
		if event.EventType != "group_message" {
			t.Fatalf("second event_type=%s want=group_message", event.EventType)
		}
		if len(event.MentionUserIDs) != 0 {
			t.Fatalf("second mention_user_ids=%v want=[]", event.MentionUserIDs)
		}
		// 群事件 OwnerID 已统一为 agent.OwnerID (=fixture.ownerID),区分两类事件靠 EventID:
		// - direct event(agent 在群里被直接触发):EventID 含 ":{agentID}:" 段
		// - delegate event(群成员 X 设了托管被触发):EventID 是 "{sessionID}:{X}:{msgID}",不含 agentID
		if strings.Contains(event.EventID, fmt.Sprintf(":%d:", fixture.agentID)) {
			secondDirectFound = true
		} else {
			secondDelegateFound = true
		}
	}
	if !secondDirectFound || !secondDelegateFound {
		t.Fatalf("expected open-group cue to fan back out, got=%#v", secondEvents)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)

	thirdPkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "dual-role-open-group-third",
		MsgType:     1,
		Content:     "我再补一句背景",
	})
	HandleSendMsg(fixture.hub, fixture.senderConn, thirdPkt)

	thirdEvents := collectForwardedAgentEvents(t, fixture.channel, 2)
	// 群事件 OwnerID 已统一为 agent.OwnerID,区分 direct 与 delegate-mirror 用 EventID
	// (direct: "{session}:{sender}:{agentID}:{msgID}..."; delegate: "{session}:{owner}:{msgID}")
	var thirdDirectEvent, thirdDelegateMirror *wsagentapi.DelegateEventPayload
	directIDInfix := fmt.Sprintf(":%d:", fixture.agentID)
	for i := range thirdEvents {
		if strings.Contains(thirdEvents[i].EventID, directIDInfix) {
			thirdDirectEvent = &thirdEvents[i]
		} else {
			thirdDelegateMirror = &thirdEvents[i]
		}
	}
	if thirdDirectEvent == nil {
		t.Fatalf("expected direct event in %#v", thirdEvents)
	}
	if thirdDelegateMirror == nil {
		t.Fatalf("expected delegate event in %#v", thirdEvents)
	}
	if thirdDirectEvent.AgentID != fixture.agentID {
		t.Fatalf("third agent_id=%d want=%d", thirdDirectEvent.AgentID, fixture.agentID)
	}
	if thirdDirectEvent.EventType != "group_message" {
		t.Fatalf("third event_type=%s want=group_message", thirdDirectEvent.EventType)
	}
	if len(thirdDirectEvent.MentionUserIDs) != 0 {
		t.Fatalf("third mention_user_ids=%v want=[]", thirdDirectEvent.MentionUserIDs)
	}
	if thirdDirectEvent.OwnerID != fixture.ownerID {
		t.Fatalf("group direct event owner_id=%d want=%d (agent.OwnerID, not senderID)", thirdDirectEvent.OwnerID, fixture.ownerID)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestHandleSendMsgGroupMentionOwnerAndAgentDispatchesBothRoles(t *testing.T) {
	fixture := setupDualRoleGroupFixture(t, "session-dual-role-both-mentioned", 8721, 8722, 9721)
	defer fixture.cleanup()

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "dual-role-both-mentioned",
		MsgType:     1,
		Content:     fmt.Sprintf("@owner_%d @DelegateBot%d 你们一起看", fixture.ownerID, fixture.agentID),
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, pkt)

	if countSentCmd(fixture.senderConn.sent, protocol.CmdSendAck) != 1 ||
		countSentCmd(fixture.senderConn.sent, protocol.CmdPushMsg) != 1 {
		t.Fatalf("sender should receive one send_ack and one push_msg, got=%#v", fixture.senderConn.sent)
	}
	if len(fixture.ownerConn.sent) != 1 || fixture.ownerConn.sent[0].cmd != protocol.CmdPushMsg {
		t.Fatalf("owner should receive only push_msg, got=%#v", fixture.ownerConn.sent)
	}

	events := collectForwardedAgentEvents(t, fixture.channel, 2)
	delegateFound := false
	directFound := false
	for _, event := range events {
		if event.AgentID != fixture.agentID {
			t.Fatalf("agent_id=%d want=%d", event.AgentID, fixture.agentID)
		}
		if event.EventType != "group_mention" {
			t.Fatalf("event_type=%s want=group_mention", event.EventType)
		}
		assertMentionIDsEqual(t, event.MentionUserIDs, fixture.ownerID, fixture.agentID)

		// 群事件 OwnerID 已统一为 agent.OwnerID,区分两类事件靠 EventID(direct 含 :agentID:)。
		if strings.Contains(event.EventID, fmt.Sprintf(":%d:", fixture.agentID)) {
			directFound = true
		} else {
			delegateFound = true
		}
	}
	if !delegateFound || !directFound {
		t.Fatalf("expected both delegate and direct events, got=%#v", events)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)

	var msg model.Message
	if err := store.DB.Where("session_id = ? AND msg_type = 1", fixture.sessionID).
		Order("msg_id DESC").
		First(&msg).Error; err != nil {
		t.Fatalf("load message error: %v", err)
	}
	var extra map[string]any
	if err := json.Unmarshal(msg.Extra, &extra); err != nil {
		t.Fatalf("unmarshal message extra error: %v", err)
	}
	rawMentions, ok := extra["mention_user_ids"].([]any)
	if !ok || len(rawMentions) != 2 {
		t.Fatalf("mention_user_ids invalid: %#v", extra["mention_user_ids"])
	}
	gotMentionIDs := []int64{parseIntStr(t, rawMentions[0].(string)), parseIntStr(t, rawMentions[1].(string))}
	assertMentionIDsEqual(t, gotMentionIDs, fixture.ownerID, fixture.agentID)
}

func TestHandleSendMsgGroupMentionOwnerAndAgentClearsPreviousContinuationTarget(t *testing.T) {
	fixture := setupDualRoleGroupFixture(t, "session-dual-role-both-mentioned-clear-continuation", 8723, 8823, 9823)
	defer fixture.cleanup()

	if err := storeGroupContinuationTargetIDs(
		context.Background(),
		fixture.sessionID,
		fixture.senderID,
		[]int64{fixture.agentID},
	); err != nil {
		t.Fatalf("seed continuation target error: %v", err)
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "dual-role-both-mentioned-clear-continuation",
		MsgType:     1,
		Content:     fmt.Sprintf("@owner_%d @DelegateBot%d 你们一起看", fixture.ownerID, fixture.agentID),
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, pkt)

	collectForwardedAgentEvents(t, fixture.channel, 2)
	assertNoMoreForwardedAgentEvents(t, fixture.channel)

	continuedTargetIDs, err := loadGroupContinuationTargetIDs(
		context.Background(),
		fixture.sessionID,
		fixture.senderID,
	)
	if err != nil {
		t.Fatalf("load continuation target ids error: %v", err)
	}
	if len(continuedTargetIDs) != 0 {
		t.Fatalf("multi-target mention should clear continuation cache, got=%v", continuedTargetIDs)
	}

	mentionAllContinuation, err := loadGroupMentionAllContinuation(
		context.Background(),
		fixture.sessionID,
		1,
		fixture.senderID,
	)
	if err != nil {
		t.Fatalf("load mention-all continuation error: %v", err)
	}
	if len(mentionAllContinuation.TargetUserIDs) != 0 {
		t.Fatalf("multi-target mention should not create mention-all continuation, got=%v", mentionAllContinuation.TargetUserIDs)
	}
}

func TestHandleSendMsgMentionAllDispatchesBothRoles(t *testing.T) {
	fixture := setupDualRoleGroupFixture(t, "session-dual-role-mention-all", 8726, 8727, 9726)
	defer fixture.cleanup()

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "dual-role-mention-all",
		MsgType:     1,
		Content:     "@所有人 一起看",
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, pkt)

	if countSentCmd(fixture.senderConn.sent, protocol.CmdSendAck) != 1 ||
		countSentCmd(fixture.senderConn.sent, protocol.CmdPushMsg) != 1 {
		t.Fatalf("sender should receive one send_ack and one push_msg, got=%#v", fixture.senderConn.sent)
	}
	if len(fixture.ownerConn.sent) != 1 || fixture.ownerConn.sent[0].cmd != protocol.CmdPushMsg {
		t.Fatalf("owner should receive only push_msg, got=%#v", fixture.ownerConn.sent)
	}

	events := collectForwardedAgentEvents(t, fixture.channel, 2)
	delegateFound := false
	directFound := false
	for _, event := range events {
		if event.AgentID != fixture.agentID {
			t.Fatalf("agent_id=%d want=%d", event.AgentID, fixture.agentID)
		}
		if event.EventType != "group_mention" {
			t.Fatalf("event_type=%s want=group_mention", event.EventType)
		}
		assertMentionIDsEqual(t, event.MentionUserIDs, fixture.ownerID, fixture.agentID)

		// 群事件 OwnerID 已统一为 agent.OwnerID,区分两类事件靠 EventID(direct 含 :agentID:)。
		if strings.Contains(event.EventID, fmt.Sprintf(":%d:", fixture.agentID)) {
			directFound = true
		} else {
			delegateFound = true
		}
	}
	if !delegateFound || !directFound {
		t.Fatalf("expected both delegate and direct events, got=%#v", events)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)

	var msg model.Message
	if err := store.DB.Where("session_id = ? AND msg_type = 1", fixture.sessionID).
		Order("msg_id DESC").
		First(&msg).Error; err != nil {
		t.Fatalf("load message error: %v", err)
	}
	var extra map[string]any
	if err := json.Unmarshal(msg.Extra, &extra); err != nil {
		t.Fatalf("unmarshal message extra error: %v", err)
	}
	if _, ok := extra["mention_all"]; ok {
		t.Fatalf("mention_all should not persist in extra: %#v", extra)
	}
	rawMentions, ok := extra["mention_user_ids"].([]any)
	if !ok || len(rawMentions) != 2 {
		t.Fatalf("mention_user_ids invalid: %#v", extra["mention_user_ids"])
	}
	gotMentionIDs := []int64{parseIntStr(t, rawMentions[0].(string)), parseIntStr(t, rawMentions[1].(string))}
	assertMentionIDsEqual(t, gotMentionIDs, fixture.ownerID, fixture.agentID)
}

func TestHandleSendMsgMentionAllContinuationDispatchesAllDirectAgentsWithoutExplicitMention(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-direct-mention-all-continuation", 8749, 9761, 9762)
	defer fixture.cleanup()

	firstPkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "direct-mention-all-continuation-first",
		MsgType:     1,
		Content:     "@所有人 先一起看",
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, firstPkt)

	firstEvents := collectForwardedAgentEvents(t, fixture.channel, 2)
	firstSeenAgents := make(map[int64]struct{}, len(fixture.agentIDs))
	for _, event := range firstEvents {
		if !containsInt64(fixture.agentIDs, event.AgentID) {
			t.Fatalf("unexpected first agent_id=%d", event.AgentID)
		}
		if event.EventType != "group_mention" {
			t.Fatalf("first event_type=%s want=group_mention", event.EventType)
		}
		if event.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
			t.Fatalf("first mirror_mode=%q want=%q", event.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
		}
		assertMentionIDsEqual(t, event.MentionUserIDs, fixture.agentIDs...)
		firstSeenAgents[event.AgentID] = struct{}{}
	}
	if len(firstSeenAgents) != len(fixture.agentIDs) {
		t.Fatalf("expected first mention_all to dispatch all direct agents, got=%#v", firstEvents)
	}
	continuedTargetIDs, err := loadGroupContinuationTargetIDs(
		context.Background(),
		fixture.sessionID,
		fixture.senderID,
	)
	if err != nil {
		t.Fatalf("load continuation target ids error: %v", err)
	}
	if len(continuedTargetIDs) != 0 {
		t.Fatalf("mention_all should not populate single-target continuation cache, got=%v", continuedTargetIDs)
	}
	mentionAllContinuation, err := loadGroupMentionAllContinuation(
		context.Background(),
		fixture.sessionID,
		1,
		fixture.senderID,
	)
	if err != nil {
		t.Fatalf("load mention-all continuation error: %v", err)
	}
	assertMentionIDsEqual(t, mentionAllContinuation.TargetUserIDs, fixture.agentIDs...)
	assertNoMoreForwardedAgentEvents(t, fixture.channel)

	fixture.senderConn.sent = nil

	secondPkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "direct-mention-all-continuation-second",
		MsgType:     1,
		Content:     "第二句继续补充细节",
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, secondPkt)

	secondEvents := collectForwardedAgentEvents(t, fixture.channel, 2)
	secondSeenAgents := make(map[int64]struct{}, len(fixture.agentIDs))
	for _, event := range secondEvents {
		if !containsInt64(fixture.agentIDs, event.AgentID) {
			t.Fatalf("unexpected second agent_id=%d", event.AgentID)
		}
		if event.EventType != "group_message" {
			t.Fatalf("second event_type=%s want=group_message", event.EventType)
		}
		if event.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
			t.Fatalf("second mirror_mode=%q want=%q", event.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
		}
		if len(event.MentionUserIDs) != 0 {
			t.Fatalf("second mention_user_ids=%v want=[]", event.MentionUserIDs)
		}
		secondSeenAgents[event.AgentID] = struct{}{}
	}
	if len(secondSeenAgents) != len(fixture.agentIDs) {
		t.Fatalf("expected mention_all continuation to dispatch all direct agents, got=%#v", secondEvents)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)

	mentionAllContinuation, err = loadGroupMentionAllContinuation(
		context.Background(),
		fixture.sessionID,
		1,
		fixture.senderID,
	)
	if err != nil {
		t.Fatalf("reload mention-all continuation error: %v", err)
	}
	if len(mentionAllContinuation.TargetUserIDs) != 0 {
		t.Fatalf("mention-all continuation should be one-shot, got=%v", mentionAllContinuation.TargetUserIDs)
	}

	fixture.senderConn.sent = nil

	thirdPkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "direct-mention-all-continuation-third",
		MsgType:     1,
		Content:     "第三句不该再自动续发了",
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, thirdPkt)

	thirdEvents := collectForwardedAgentEvents(t, fixture.channel, 2)
	for _, event := range thirdEvents {
		if !containsInt64(fixture.agentIDs, event.AgentID) {
			t.Fatalf("unexpected third agent_id=%d", event.AgentID)
		}
		if event.EventType != "group_message" {
			t.Fatalf("third event_type=%s want=group_message", event.EventType)
		}
		if event.MirrorMode != wsagentapi.MirrorModeRecordOnly {
			t.Fatalf("third mirror_mode=%q want=%q", event.MirrorMode, wsagentapi.MirrorModeRecordOnly)
		}
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestHandleSendMsgMentionAllContinuationWakesMentionOnlyDirectAgents(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-direct-mention-all-mention-only", 8766, 9776, 9777)
	defer fixture.cleanup()

	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id IN ? AND member_type = 2", fixture.sessionID, fixture.agentIDs).
		Update("agent_receive_mode", agentreceive.ModeMentionOnly).Error; err != nil {
		t.Fatalf("update mention-only mode error: %v", err)
	}

	firstPkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "direct-mention-all-mention-only-first",
		MsgType:     1,
		Content:     "@所有人 先都来一下",
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, firstPkt)
	collectForwardedAgentEvents(t, fixture.channel, 2)
	assertNoMoreForwardedAgentEvents(t, fixture.channel)

	secondPkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "direct-mention-all-mention-only-second",
		MsgType:     1,
		Content:     "第二句继续一起回答",
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, secondPkt)

	secondEvents := collectForwardedAgentEvents(t, fixture.channel, 2)
	secondSeenAgents := make(map[int64]struct{}, len(fixture.agentIDs))
	for _, event := range secondEvents {
		if !containsInt64(fixture.agentIDs, event.AgentID) {
			t.Fatalf("unexpected second agent_id=%d", event.AgentID)
		}
		if event.EventType != "group_message" {
			t.Fatalf("second event_type=%s want=group_message", event.EventType)
		}
		if event.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
			t.Fatalf("second mirror_mode=%q want=%q", event.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
		}
		if len(event.MentionUserIDs) != 0 {
			t.Fatalf("second mention_user_ids=%v want=[]", event.MentionUserIDs)
		}
		secondSeenAgents[event.AgentID] = struct{}{}
	}
	if len(secondSeenAgents) != len(fixture.agentIDs) {
		t.Fatalf("expected mention-all continuation to wake mention-only direct agents, got=%#v", secondEvents)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestHandleSendMsgMentionAllContinuationSurvivesInterveningReply(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-direct-mention-all-intervened", 8750, 9763, 9764)
	defer fixture.cleanup()

	firstPkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "direct-mention-all-intervened-first",
		MsgType:     1,
		Content:     "@所有人 先听一下",
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, firstPkt)
	collectForwardedAgentEvents(t, fixture.channel, 2)
	assertNoMoreForwardedAgentEvents(t, fixture.channel)

	const otherUserID = int64(11750)
	if err := store.DB.Create(&model.User{
		ID:       otherUserID,
		Username: fmt.Sprintf("other_%d", otherUserID),
		Email:    fmt.Sprintf("other_%d@example.com", otherUserID),
		Nickname: fmt.Sprintf("other-nick-%d", otherUserID),
	}).Error; err != nil {
		t.Fatalf("create other user error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{
		SessionID:    fixture.sessionID,
		MemberID:     otherUserID,
		MemberType:   1,
		JoinedAt:     time.Now().UTC(),
		LastActiveAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("create other session member error: %v", err)
	}

	seedGroupLastMessage(t, fixture.sessionID, model.Message{
		MsgID:      18889990777,
		SessionID:  fixture.sessionID,
		SenderID:   otherUserID,
		SenderType: 1,
		MsgType:    1,
		Content:    "我中间插一句",
		CreatedAt:  time.Now().UTC(),
	})

	fixture.senderConn.sent = nil

	secondPkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "direct-mention-all-intervened-second",
		MsgType:     1,
		Content:     "这句不该再自动带上所有人",
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, secondPkt)

	secondEvents := collectForwardedAgentEvents(t, fixture.channel, 2)
	for _, event := range secondEvents {
		if !containsInt64(fixture.agentIDs, event.AgentID) {
			t.Fatalf("unexpected second agent_id=%d", event.AgentID)
		}
		if event.EventType != "group_message" {
			t.Fatalf("second event_type=%s want=group_message", event.EventType)
		}
		if event.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
			t.Fatalf("second mirror_mode=%q want=%q", event.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
		}
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)

	mentionAllContinuation, err := loadGroupMentionAllContinuation(
		context.Background(),
		fixture.sessionID,
		1,
		fixture.senderID,
	)
	if err != nil {
		t.Fatalf("reload mention-all continuation error: %v", err)
	}
	if len(mentionAllContinuation.TargetUserIDs) != 0 {
		t.Fatalf("continuation should be consumed after sender next message, got=%v", mentionAllContinuation.TargetUserIDs)
	}
}

func TestHandleSendMsgMentionAllSingleTargetContinuationStaysOneShot(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-direct-mention-all-single-target", 8755, 9765)
	defer fixture.cleanup()

	firstPkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "direct-mention-all-single-target-first",
		MsgType:     1,
		Content:     "@所有人 单人群也先打一声招呼",
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, firstPkt)

	firstEvents := collectForwardedAgentEvents(t, fixture.channel, 1)
	firstEvent := firstEvents[0]
	if firstEvent.AgentID != fixture.agentIDs[0] {
		t.Fatalf("first agent_id=%d want=%d", firstEvent.AgentID, fixture.agentIDs[0])
	}
	if firstEvent.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("first mirror_mode=%q want=%q", firstEvent.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)

	continuedTargetIDs, err := loadGroupContinuationTargetIDs(
		context.Background(),
		fixture.sessionID,
		fixture.senderID,
	)
	if err != nil {
		t.Fatalf("load continuation target ids error: %v", err)
	}
	if len(continuedTargetIDs) != 0 {
		t.Fatalf("mention_all should not populate generic continuation cache, got=%v", continuedTargetIDs)
	}

	fixture.senderConn.sent = nil

	secondPkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "direct-mention-all-single-target-second",
		MsgType:     1,
		Content:     "第二句继续发给同一个对象",
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, secondPkt)

	secondEvents := collectForwardedAgentEvents(t, fixture.channel, 1)
	secondEvent := secondEvents[0]
	if secondEvent.AgentID != fixture.agentIDs[0] {
		t.Fatalf("second agent_id=%d want=%d", secondEvent.AgentID, fixture.agentIDs[0])
	}
	if secondEvent.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("second mirror_mode=%q want=%q", secondEvent.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)

	continuedTargetIDs, err = loadGroupContinuationTargetIDs(
		context.Background(),
		fixture.sessionID,
		fixture.senderID,
	)
	if err != nil {
		t.Fatalf("reload continuation target ids error: %v", err)
	}
	if len(continuedTargetIDs) != 0 {
		t.Fatalf("one-shot mention_all should not spill into generic continuation cache, got=%v", continuedTargetIDs)
	}

	fixture.senderConn.sent = nil

	thirdPkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "direct-mention-all-single-target-third",
		MsgType:     1,
		Content:     "第三句不该再自动续发",
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, thirdPkt)

	thirdEvents := collectForwardedAgentEvents(t, fixture.channel, 1)
	thirdEvent := thirdEvents[0]
	if thirdEvent.AgentID != fixture.agentIDs[0] {
		t.Fatalf("third agent_id=%d want=%d", thirdEvent.AgentID, fixture.agentIDs[0])
	}
	if thirdEvent.MirrorMode != wsagentapi.MirrorModeRecordOnly {
		t.Fatalf("third mirror_mode=%q want=%q", thirdEvent.MirrorMode, wsagentapi.MirrorModeRecordOnly)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestHandleSendMsgMentionAllMarkerClearedByExplicitMention(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-direct-mention-all-marker-persist", 8760, 9770, 9771)
	defer fixture.cleanup()

	firstPkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "direct-mention-all-marker-persist-first",
		MsgType:     1,
		Content:     "@所有人 先集合一下",
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, firstPkt)
	collectForwardedAgentEvents(t, fixture.channel, 2)
	assertNoMoreForwardedAgentEvents(t, fixture.channel)

	secondPkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "direct-mention-all-marker-persist-second",
		MsgType:     1,
		Content:     fmt.Sprintf("@MirrorBot%d 我先单独问你一句", fixture.agentIDs[0]),
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, secondPkt)
	collectForwardedAgentEvents(t, fixture.channel, 2)
	assertNoMoreForwardedAgentEvents(t, fixture.channel)

	mentionAllContinuation, err := loadGroupMentionAllContinuation(
		context.Background(),
		fixture.sessionID,
		1,
		fixture.senderID,
	)
	if err != nil {
		t.Fatalf("reload mention-all continuation error: %v", err)
	}
	if len(mentionAllContinuation.TargetUserIDs) != 0 {
		t.Fatalf("mention-all marker should be cleared by explicit single @mention, got=%v", mentionAllContinuation.TargetUserIDs)
	}

	fixture.senderConn.sent = nil

	thirdPkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "direct-mention-all-marker-persist-third",
		MsgType:     1,
		Content:     "现在都分别介绍一下自己",
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, thirdPkt)

	thirdEvents := collectForwardedAgentEvents(t, fixture.channel, 2)
	var processEvents []wsagentapi.DelegateEventPayload
	for _, event := range thirdEvents {
		if !containsInt64(fixture.agentIDs, event.AgentID) {
			t.Fatalf("unexpected third agent_id=%d", event.AgentID)
		}
		if event.MirrorMode == wsagentapi.MirrorModeRecordAndProcess {
			processEvents = append(processEvents, event)
		}
	}
	if len(processEvents) != 1 || processEvents[0].AgentID != fixture.agentIDs[0] {
		t.Fatalf("expected only explicitly @mentioned agent in followup processing, got process_events=%v", processEvents)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestHandleSendMsgMentionAllMarkerOverridesImplicitQuotedTarget(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-direct-mention-all-marker-quoted", 8762, 9774, 9775)
	defer fixture.cleanup()

	firstPkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "direct-mention-all-marker-quoted-first",
		MsgType:     1,
		Content:     "@所有人 先都过来看一下",
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, firstPkt)
	collectForwardedAgentEvents(t, fixture.channel, 2)
	assertNoMoreForwardedAgentEvents(t, fixture.channel)

	const quotedMessageID = int64(18889990911)
	if err := store.DB.Create(&model.Message{
		MsgID:      quotedMessageID,
		SessionID:  fixture.sessionID,
		SenderID:   fixture.agentIDs[0],
		SenderType: 2,
		MsgType:    1,
		Content:    "你等会儿可以引用我",
		CreatedAt:  time.Now().UTC().Add(-time.Second),
	}).Error; err != nil {
		t.Fatalf("create quoted message error: %v", err)
	}

	secondPkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:       fixture.sessionID,
		ClientMsgID:     "direct-mention-all-marker-quoted-second",
		MsgType:         1,
		Content:         "我引用一句，但没有明确点名",
		QuotedMessageID: quotedMessageID,
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, secondPkt)

	secondEvents := collectForwardedAgentEvents(t, fixture.channel, 2)
	secondSeenAgents := make(map[int64]struct{}, len(fixture.agentIDs))
	for _, event := range secondEvents {
		if !containsInt64(fixture.agentIDs, event.AgentID) {
			t.Fatalf("unexpected second agent_id=%d", event.AgentID)
		}
		if event.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
			t.Fatalf("second mirror_mode=%q want=%q", event.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
		}
		if event.EventType != "group_message" {
			t.Fatalf("second event_type=%s want=group_message event=%#v", event.EventType, event)
		}
		secondSeenAgents[event.AgentID] = struct{}{}
	}
	if len(secondSeenAgents) != len(fixture.agentIDs) {
		t.Fatalf("expected mention-all marker to override implicit quoted target, got=%#v", secondEvents)
	}

	mentionAllContinuation, err := loadGroupMentionAllContinuation(
		context.Background(),
		fixture.sessionID,
		1,
		fixture.senderID,
	)
	if err != nil {
		t.Fatalf("reload mention-all continuation error: %v", err)
	}
	if len(mentionAllContinuation.TargetUserIDs) != 0 {
		t.Fatalf("mention-all marker should be consumed after quoted plain followup, got=%v", mentionAllContinuation.TargetUserIDs)
	}
}

func TestResolveDirectSessionRouteContinuedMentionAllAllowsAgentSenderProcessing(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-agent-mention-all-route", 8761, 9772, 9773)
	defer fixture.cleanup()

	semantics := &groupDispatchSemantics{
		TargetUserIDs:       []int64{fixture.agentIDs[1]},
		ContinuedMentionAll: true,
		Continued:           true,
	}

	route, err := resolveDirectSessionRoute(
		fixture.sessionID,
		2,
		fixture.agentIDs[0],
		2,
		18889990999,
		0,
		1,
		"继续按全员消息处理",
		nil,
		semantics,
		nil,
		nil,
		false,
	)
	if err != nil {
		t.Fatalf("resolve direct session route error: %v", err)
	}
	if route == nil {
		t.Fatal("route should not be nil")
	}
	if len(route.Targets) != 1 {
		t.Fatalf("expected one processing target for agent sender continuation, got=%#v", route.Targets)
	}
	if route.Targets[0].Agent.ID != fixture.agentIDs[1] {
		t.Fatalf("route target agent_id=%d want=%d", route.Targets[0].Agent.ID, fixture.agentIDs[1])
	}
}

func TestHandleSendMsgDirectRouteCarriesStructuredPayload(t *testing.T) {
	fixture := setupDualRoleGroupFixture(t, "session-direct-structured-payload", 8731, 8732, 9731)
	defer fixture.cleanup()

	extra, err := json.Marshal(map[string]any{
		"attachments": []map[string]any{
			{
				"media_url":       "https://cdn.example.com/direct.png",
				"attachment_type": "image",
				"file_name":       "direct.png",
				"content_type":    "image/png",
			},
		},
		"biz_card": map[string]any{
			"version": 1,
			"type":    "exec_status",
			"payload": map[string]any{"status": "running"},
		},
		"channel_data": map[string]any{
			"grix": map[string]any{
				"execStatus": map[string]any{"status": "running"},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal extra error: %v", err)
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:       fixture.sessionID,
		ClientMsgID:     "direct-structured-payload",
		MsgType:         2,
		Content:         fmt.Sprintf("@DelegateBot%d 处理这个结构化消息", fixture.agentID),
		Extra:           extra,
		QuotedMessageID: 18889990331,
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, pkt)

	events := collectForwardedAgentEvents(t, fixture.channel, 1)
	for _, event := range events {
		if event.MsgType != 2 {
			t.Fatalf("event msg_type=%d want=2", event.MsgType)
		}
		if len(event.Attachments) != 1 || event.Attachments[0].MediaURL != "https://cdn.example.com/direct.png" {
			t.Fatalf("event attachments=%#v", event.Attachments)
		}
		if len(event.BizCard) == 0 {
			t.Fatalf("event biz_card should not be empty")
		}
		if len(event.ChannelData) == 0 {
			t.Fatalf("event channel_data should not be empty")
		}
	}
}

func TestHandleSendMsgGroupMentionMirrorsUntargetedDirectAgent(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-direct-mirror-mention", 8741, 9741, 9742)
	defer fixture.cleanup()

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "direct-mirror-mention",
		MsgType:     1,
		Content:     fmt.Sprintf("@MirrorBot%d 只问你", fixture.agentIDs[0]),
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, pkt)

	events := collectForwardedAgentEvents(t, fixture.channel, 2)
	processingFound := false
	mirrorFound := false
	for _, event := range events {
		switch event.AgentID {
		case fixture.agentIDs[0]:
			processingFound = true
			if event.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
				t.Fatalf("processing mirror_mode=%q want=%q", event.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
			}
			if event.EventType != "group_mention" {
				t.Fatalf("processing event_type=%s want=group_mention", event.EventType)
			}
		case fixture.agentIDs[1]:
			mirrorFound = true
			if event.MirrorMode != wsagentapi.MirrorModeRecordOnly {
				t.Fatalf("mirror mirror_mode=%q want=%q", event.MirrorMode, wsagentapi.MirrorModeRecordOnly)
			}
			if event.EventType != "group_message" {
				t.Fatalf("mirror event_type=%s want=group_message", event.EventType)
			}
		default:
			t.Fatalf("unexpected mirrored agent_id=%d", event.AgentID)
		}
	}
	if !processingFound || !mirrorFound {
		t.Fatalf("expected one processing event and one mirror event, got=%#v", events)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestHandleSendMsgGroupMentionSkipsMentionOnlyUntargetedDirectAgent(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-direct-mention-only-skip-mirror", 8767, 9781, 9782)
	defer fixture.cleanup()

	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id IN ? AND member_type = 2", fixture.sessionID, fixture.agentIDs).
		Update("agent_receive_mode", agentreceive.ModeMentionOnly).Error; err != nil {
		t.Fatalf("update mention-only mode error: %v", err)
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "direct-mention-only-skip-mirror",
		MsgType:     1,
		Content:     fmt.Sprintf("@MirrorBot%d 只问你", fixture.agentIDs[0]),
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, pkt)

	events := collectForwardedAgentEvents(t, fixture.channel, 1)
	event := events[0]
	if event.AgentID != fixture.agentIDs[0] {
		t.Fatalf("agent_id=%d want=%d", event.AgentID, fixture.agentIDs[0])
	}
	if event.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("mirror_mode=%q want=%q", event.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
	}
	if event.EventType != "group_mention" {
		t.Fatalf("event_type=%s want=group_mention", event.EventType)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestHandleSendMsgIdleGroupWithoutMentionDispatchesAllDirectAgents(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-direct-idle-cold-start", 8745, 9751, 9752)
	defer fixture.cleanup()

	seedIdleGroupMessageHistory(t, fixture.sessionID, fixture.senderID, groupColdStartIdleThreshold+time.Minute)

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "direct-idle-cold-start",
		MsgType:     1,
		Content:     "好久没人说话了，我来冒个泡",
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, pkt)

	events := collectForwardedAgentEvents(t, fixture.channel, 2)
	for _, event := range events {
		if event.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
			t.Fatalf("cold-start mirror_mode=%q want=%q", event.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
		}
		if event.EventType != "group_message" {
			t.Fatalf("cold-start event_type=%s want=group_message", event.EventType)
		}
		if len(event.MentionUserIDs) != 0 {
			t.Fatalf("cold-start mention_user_ids=%v want=[]", event.MentionUserIDs)
		}
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestResolveDirectSessionRouteNewGroupFirstMessageTargetsAllProprietaryAgents(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-direct-new-group-cold-start", 8744, 9749, 9750)
	defer fixture.cleanup()

	for idx, agentID := range fixture.agentIDs {
		clientType := model.AgentClientTypeCodex
		if idx == 1 {
			clientType = model.AgentClientTypeClaude
		}
		if err := store.DB.Model(&model.Agent{}).
			Where("id = ?", agentID).
			Update("agent_client_type", clientType).Error; err != nil {
			t.Fatalf("set agent client type error: %v", err)
		}
	}

	const content = "这是新群里的第一条消息"
	semantics, err := resolveLiveGroupDispatchSemantics(
		context.Background(), fixture.sessionID, fixture.senderID, 1, 0, content, nil, true,
	)
	if err != nil {
		t.Fatalf("resolve live group semantics error: %v", err)
	}
	if !semantics.ColdStart {
		t.Fatal("cold_start=false want=true for a group with no previous messages")
	}

	route, err := resolveDirectSessionRoute(
		fixture.sessionID, 2, fixture.senderID, 1, 18889990500, 0, 1,
		content, nil, &semantics, nil, nil, false,
	)
	if err != nil {
		t.Fatalf("resolve direct session route error: %v", err)
	}
	if route == nil {
		t.Fatal("route=nil want processing targets")
	}
	if len(route.Targets) != len(fixture.agentIDs) {
		t.Fatalf("processing targets=%d want=%d", len(route.Targets), len(fixture.agentIDs))
	}
	for idx, target := range route.Targets {
		if target.Agent.ID != fixture.agentIDs[idx] {
			t.Fatalf("target[%d].agent_id=%d want=%d", idx, target.Agent.ID, fixture.agentIDs[idx])
		}
	}
}

func TestHandleSendMsgGroupPlainMessageMirrorsDirectAgentsWithoutProcessing(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-direct-open-message-mirror-only", 8746, 9756, 9757)
	defer fixture.cleanup()
	seedIdleGroupMessageHistory(t, fixture.sessionID, fixture.senderID, time.Minute)

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "direct-open-message-mirror-only",
		MsgType:     1,
		Content:     "大家看看这句，但我没点任何人",
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, pkt)

	events := collectForwardedAgentEvents(t, fixture.channel, 2)
	for _, event := range events {
		if event.MirrorMode != wsagentapi.MirrorModeRecordOnly {
			t.Fatalf("mirror_mode=%q want=%q", event.MirrorMode, wsagentapi.MirrorModeRecordOnly)
		}
		if event.EventType != "group_message" {
			t.Fatalf("event_type=%s want=group_message", event.EventType)
		}
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestHandleSendMsgGroupOpenSessionSubmitTargetsIndexedCardAgent(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-direct-open-session-indexed-card", 8749, 9761, 9762)
	defer fixture.cleanup()

	now := time.Now().UTC()
	targetCardMsgID := int64(18889990701)
	if err := store.DB.Create(&model.Message{
		MsgID:      targetCardMsgID,
		SessionID:  fixture.sessionID,
		SenderID:   fixture.agentIDs[0],
		SenderType: 2,
		MsgType:    1,
		Content:    "[Open Workspace](grix://card/agent_open_session?summary_text=missing)",
		CreatedAt:  now.Add(-time.Minute),
	}).Error; err != nil {
		t.Fatalf("create target binding card message error: %v", err)
	}
	if err := store.DB.Create(&model.Message{
		MsgID:      18889990702,
		SessionID:  fixture.sessionID,
		SenderID:   fixture.agentIDs[1],
		SenderType: 2,
		MsgType:    1,
		Content:    "[Open Workspace](grix://card/agent_open_session?summary_text=other)",
		CreatedAt:  now,
	}).Error; err != nil {
		t.Fatalf("create non-target binding card message error: %v", err)
	}
	wsagentapi.SaveBindingCardMsgID(context.Background(), fixture.agentIDs[0], fixture.sessionID, targetCardMsgID)

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "direct-open-session-indexed-card",
		MsgType:     1,
		Content:     "grix://open/session?cwd=%2Fworkspace%2Fcard",
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, pkt)

	actions := collectForwardedLocalActions(t, fixture.channel, 1)
	action := actions[0]
	if action.AgentID != fixture.agentIDs[0] {
		t.Fatalf("agent_id=%d want=%d", action.AgentID, fixture.agentIDs[0])
	}
	if action.Action.ActionType != "session_control" {
		t.Fatalf("action_type=%q want=session_control", action.Action.ActionType)
	}
	if got, _ := action.Action.Params["verb"].(string); got != "open" {
		t.Fatalf("params.verb=%q want=open", got)
	}
	if got, _ := action.Action.Params["cwd"].(string); got != "/workspace/card" {
		t.Fatalf("params.cwd=%q want=/workspace/card", got)
	}
	if got, _ := action.Action.Params["session_id"].(string); got != fixture.sessionID {
		t.Fatalf("params.session_id=%q want=%s", got, fixture.sessionID)
	}
	assertNoMoreForwardedLocalActions(t, fixture.channel)
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestHandleSendMsgIdleGroupMentionOnlyDirectAgentSkipsMirror(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-direct-idle-cold-start-mode-gate", 8747, 9758, 9759)
	defer fixture.cleanup()

	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = 2", fixture.sessionID, fixture.agentIDs[0]).
		Update("agent_receive_mode", agentreceive.ModeMentionOnly).Error; err != nil {
		t.Fatalf("update mention-only mode error: %v", err)
	}
	seedIdleGroupMessageHistory(t, fixture.sessionID, fixture.senderID, groupColdStartIdleThreshold+time.Minute)

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "direct-idle-cold-start-mode-gate",
		MsgType:     1,
		Content:     "冷群第一句，看看谁该自己判断",
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, pkt)

	events := collectForwardedAgentEvents(t, fixture.channel, 1)
	event := events[0]
	if event.AgentID != fixture.agentIDs[1] {
		t.Fatalf("agent_id=%d want=%d", event.AgentID, fixture.agentIDs[1])
	}
	if event.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("normal cold-start mirror_mode=%q want=%q", event.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestHandleSendMsgQuotedTargetWakesMentionOnlyDirectAgent(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-direct-quoted-target-mode-gate", 8748, 9760)
	defer fixture.cleanup()

	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = 2", fixture.sessionID, fixture.agentIDs[0]).
		Update("agent_receive_mode", agentreceive.ModeMentionOnly).Error; err != nil {
		t.Fatalf("update mention-only mode error: %v", err)
	}

	const quotedMessageID = int64(18889990611)
	if err := store.DB.Create(&model.Message{
		MsgID:      quotedMessageID,
		SessionID:  fixture.sessionID,
		SenderID:   fixture.agentIDs[0],
		SenderType: 2,
		MsgType:    1,
		Content:    "我先说一句，等会儿你引用我",
		CreatedAt:  time.Now().UTC().Add(-time.Second),
	}).Error; err != nil {
		t.Fatalf("create quoted message error: %v", err)
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:       fixture.sessionID,
		ClientMsgID:     "direct-quoted-target-mode-gate",
		MsgType:         1,
		Content:         "我只是在回复你，但这句没有点名",
		QuotedMessageID: quotedMessageID,
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, pkt)

	events := collectForwardedAgentEvents(t, fixture.channel, 1)
	event := events[0]
	if event.AgentID != fixture.agentIDs[0] {
		t.Fatalf("agent_id=%d want=%d", event.AgentID, fixture.agentIDs[0])
	}
	if event.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("mirror_mode=%q want=%q", event.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
	}
	if event.EventType != "group_mention" {
		t.Fatalf("event_type=%s want=group_mention", event.EventType)
	}
	if event.Content != "我只是在回复你，但这句没有点名" {
		t.Fatalf("event content=%q", event.Content)
	}
	if len(event.ContextMessages) != 1 {
		t.Fatalf("context_messages=%#v want=1 item (quoted only, no backlog for ModeMentionOnly)", event.ContextMessages)
	}
	if event.ContextMessages[0].MsgID != quotedMessageID {
		t.Fatalf("quoted context msg_id=%d want=%d", event.ContextMessages[0].MsgID, quotedMessageID)
	}
	if event.ContextMessages[0].Content != "[引用消息]\n我先说一句，等会儿你引用我" {
		t.Fatalf("quoted context content=%q", event.ContextMessages[0].Content)
	}
	assertMentionIDsEqual(t, event.MentionUserIDs, fixture.agentIDs[0])
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestHandleSendMsgDirectMentionOnlyWithoutQuoteHasEmptyContext(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-direct-mention-only-no-quote", 8750, 9763)
	defer fixture.cleanup()

	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = 2", fixture.sessionID, fixture.agentIDs[0]).
		Update("agent_receive_mode", agentreceive.ModeMentionOnly).Error; err != nil {
		t.Fatalf("update mention-only mode error: %v", err)
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "direct-mention-only-no-quote",
		MsgType:     1,
		Content:     fmt.Sprintf("@MirrorBot%d 处理一下", fixture.agentIDs[0]),
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, pkt)

	events := collectForwardedAgentEvents(t, fixture.channel, 1)
	event := events[0]
	if event.AgentID != fixture.agentIDs[0] {
		t.Fatalf("agent_id=%d want=%d", event.AgentID, fixture.agentIDs[0])
	}
	if event.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("mirror_mode=%q want=%q", event.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
	}
	if event.EventType != "group_mention" {
		t.Fatalf("event_type=%s want=group_mention", event.EventType)
	}
	if len(event.ContextMessages) != 0 {
		t.Fatalf("context_messages=%#v want=empty (ModeMentionOnly without quote)", event.ContextMessages)
	}
	assertMentionIDsEqual(t, event.MentionUserIDs, fixture.agentIDs[0])
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestTriggerDirectRouteForAgentMessageMirrorsOtherDirectAgents(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-direct-mirror-agent-reply", 8742, 9743, 9744)
	defer fixture.cleanup()

	TriggerDirectRouteForMessage(
		fixture.hub,
		context.Background(),
		fixture.sessionID,
		fixture.agentIDs[0],
		2,
		18889990501,
		0,
		1,
		"我先回答一下",
		nil,
		nil,
		nil,
	)

	events := collectForwardedAgentEvents(t, fixture.channel, 1)
	event := events[0]
	if event.AgentID != fixture.agentIDs[1] {
		t.Fatalf("agent_id=%d want=%d", event.AgentID, fixture.agentIDs[1])
	}
	if event.MirrorMode != wsagentapi.MirrorModeRecordOnly {
		t.Fatalf("mirror_mode=%q want=%q", event.MirrorMode, wsagentapi.MirrorModeRecordOnly)
	}
	if event.EventType != "group_message" {
		t.Fatalf("event_type=%s want=group_message", event.EventType)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestTriggerDirectRouteForAgentMessageExplicitNumericMentionDispatchesTarget(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-direct-agent-numeric-mention", 8743, 9745, 9746)
	defer fixture.cleanup()

	TriggerDirectRouteForMessage(
		fixture.hub,
		context.Background(),
		fixture.sessionID,
		fixture.agentIDs[0],
		2,
		18889990502,
		0,
		1,
		fmt.Sprintf("@%d 你来处理这句", fixture.agentIDs[1]),
		nil,
		nil,
		nil,
	)

	events := collectForwardedAgentEvents(t, fixture.channel, 1)
	event := events[0]
	if event.AgentID != fixture.agentIDs[1] {
		t.Fatalf("agent_id=%d want=%d", event.AgentID, fixture.agentIDs[1])
	}
	if event.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("mirror_mode=%q want=%q", event.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
	}
	if event.EventType != "group_mention" {
		t.Fatalf("event_type=%s want=group_mention", event.EventType)
	}
	assertMentionIDsEqual(t, event.MentionUserIDs, fixture.agentIDs[1])
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestHandleSendMsgIdleGroupAvoidsDoubleProcessingForDualRoleAgent(t *testing.T) {
	fixture := setupDualRoleGroupFixture(t, "session-dual-role-idle-cold-start", 8751, 8752, 9753)
	defer fixture.cleanup()

	seedIdleGroupMessageHistory(t, fixture.sessionID, fixture.senderID, groupColdStartIdleThreshold+time.Minute)

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "dual-role-idle-cold-start",
		MsgType:     1,
		Content:     "冷群第一句，大家自己判断",
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, pkt)

	events := collectForwardedAgentEvents(t, fixture.channel, 2)
	// 群事件 OwnerID 已统一为 agent.OwnerID,区分 direct(Process) 与 delegate-mirror(RecordOnly) 用 MirrorMode。
	var directEvent, delegateMirror *wsagentapi.DelegateEventPayload
	for i := range events {
		switch events[i].MirrorMode {
		case wsagentapi.MirrorModeRecordAndProcess:
			directEvent = &events[i]
		case wsagentapi.MirrorModeRecordOnly:
			delegateMirror = &events[i]
		}
	}
	if directEvent == nil {
		t.Fatalf("expected direct event (MirrorMode=record_and_process) in %#v", events)
	}
	if delegateMirror == nil {
		t.Fatalf("expected delegate mirror (MirrorMode=record_only) in %#v", events)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestHandleRetryMsgIdleGroupUsesSavedColdStartSnapshot(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-direct-idle-cold-start-retry", 8753, 9754, 9755)
	defer fixture.cleanup()

	seedIdleGroupMessageHistory(t, fixture.sessionID, fixture.senderID, groupColdStartIdleThreshold+time.Minute)

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "direct-idle-cold-start-retry",
		MsgType:     1,
		Content:     "重试也要按冷启动分发",
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, pkt)
	events := collectForwardedAgentEvents(t, fixture.channel, 2)
	for _, event := range events {
		if event.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
			t.Fatalf("initial cold-start mirror_mode=%q want=%q", event.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
		}
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)

	ack, ok := findSendAck(fixture.senderConn.sent)
	if !ok {
		t.Fatalf("expected send_ack, got=%#v", fixture.senderConn.sent)
	}

	fixture.senderConn.sent = nil
	HandleRetryMsg(fixture.hub, fixture.senderConn, makeRetryMsgPacket(t, protocol.RetryMsgPayload{
		SessionID: fixture.sessionID,
		MsgID:     ack.MsgID,
	}))

	retryAck, ok := findRetryMsgAck(fixture.senderConn.sent)
	if !ok {
		t.Fatalf("expected retry_msg_ack, got=%#v", fixture.senderConn.sent)
	}
	if retryAck.Code != 0 {
		t.Fatalf("retry ack code=%d want=0", retryAck.Code)
	}

	retryEvents := collectForwardedAgentEvents(t, fixture.channel, 2)
	for _, event := range retryEvents {
		if event.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
			t.Fatalf("retry cold-start mirror_mode=%q want=%q", event.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
		}
		if event.EventType != "group_message" {
			t.Fatalf("retry cold-start event_type=%s want=group_message", event.EventType)
		}
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestAgentQuoteAgentMessageProducesImplicitMentionUnderLoopCap(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-agent-quote-agent-under-cap", 8760, 9761, 9762)
	defer fixture.cleanup()

	const quotedMessageID = int64(18889990701)
	if err := store.DB.Create(&model.Message{
		MsgID:      quotedMessageID,
		SessionID:  fixture.sessionID,
		SenderID:   fixture.agentIDs[1],
		SenderType: 2,
		MsgType:    1,
		Content:    "agent B 的消息",
		CreatedAt:  time.Now().UTC().Add(-time.Second),
	}).Error; err != nil {
		t.Fatalf("create quoted message error: %v", err)
	}

	TriggerDirectRouteForMessage(
		fixture.hub,
		context.Background(),
		fixture.sessionID,
		fixture.agentIDs[0],
		2,
		18889990702,
		quotedMessageID,
		1,
		"回复你的话",
		nil,
		nil,
		nil,
	)

	events := collectForwardedAgentEvents(t, fixture.channel, 1)
	event := events[0]
	if event.AgentID != fixture.agentIDs[1] {
		t.Fatalf("agent_id=%d want=%d", event.AgentID, fixture.agentIDs[1])
	}
	if event.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("agent quote agent under cap: mirror_mode=%q want=%q (quoting should imply @mention, same as a human quoting an agent)", event.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
	}
	if event.EventType != "group_mention" {
		t.Fatalf("event_type=%s want=group_mention", event.EventType)
	}
	assertMentionIDsEqual(t, event.MentionUserIDs, fixture.agentIDs[1])
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

// TestAgentQuoteAgentMessageLoopCapSuppressesImplicitMentionAfterThreshold
// simulates a sustained agent-to-agent quote ping-pong with no human message
// in between. Each round quotes the other agent's previous reply, so the
// session-wide agentAutoLoopChainCap counter climbs by one per round. Once it
// exceeds the cap, quoting stops implying @mention (mirror-only) to prevent
// unbounded automated back-and-forth; up to and including the cap, quoting
// keeps triggering the other agent's turn as normal collaboration.
func TestAgentQuoteAgentMessageLoopCapSuppressesImplicitMentionAfterThreshold(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-agent-quote-agent-loop-cap", 8763, 9766, 9767)
	defer fixture.cleanup()

	// Round 1 seeds a plain (unquoted) message so round 2 has something to
	// quote; it does not itself increment the loop-chain counter. Rounds 2..N
	// each quote the previous round's message, incrementing the counter by
	// one per round. The counter exceeds agentAutoLoopChainCap starting at
	// round agentAutoLoopChainCap+2, which is where suppression should kick in.
	const lastRound = agentAutoLoopChainCap + 2
	quotedMessageID := int64(0)
	for round := 1; round <= lastRound; round++ {
		triggerMsgID := int64(18889990800 + round)
		senderIdx := round % 2
		quotedSenderIdx := (senderIdx + 1) % 2

		if round > 1 {
			if err := store.DB.Create(&model.Message{
				MsgID:      quotedMessageID,
				SessionID:  fixture.sessionID,
				SenderID:   fixture.agentIDs[quotedSenderIdx],
				SenderType: 2,
				MsgType:    1,
				Content:    fmt.Sprintf("round %d 的消息", round-1),
				CreatedAt:  time.Now().UTC().Add(-time.Second),
			}).Error; err != nil {
				t.Fatalf("round %d: create quoted message error: %v", round, err)
			}
		}

		TriggerDirectRouteForMessage(
			fixture.hub,
			context.Background(),
			fixture.sessionID,
			fixture.agentIDs[senderIdx],
			2,
			triggerMsgID,
			quotedMessageID,
			1,
			fmt.Sprintf("round %d 回复", round),
			nil,
			nil,
			nil,
		)

		events := collectForwardedAgentEvents(t, fixture.channel, 1)
		event := events[0]
		if round > 1 {
			wantMirrorMode := wsagentapi.MirrorModeRecordAndProcess
			if round > agentAutoLoopChainCap+1 {
				wantMirrorMode = wsagentapi.MirrorModeRecordOnly
			}
			if event.MirrorMode != wantMirrorMode {
				t.Fatalf("round %d: mirror_mode=%q want=%q", round, event.MirrorMode, wantMirrorMode)
			}
		}
		assertNoMoreForwardedAgentEvents(t, fixture.channel)

		quotedMessageID = triggerMsgID
	}
}

// TestHumanMessageResetsAgentLoopChain confirms a human message in between
// agent-to-agent quote rounds clears the loop-chain counter, so the cap only
// bites during a sustained fully-automated back-and-forth, not across a
// session's normal lifetime of occasional agent hand-offs.
func TestHumanMessageResetsAgentLoopChain(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-agent-quote-agent-loop-cap-reset", 8764, 9768, 9769)
	defer fixture.cleanup()

	ctx := context.Background()
	for i := 0; i < agentAutoLoopChainCap; i++ {
		if _, err := incrAgentAutoLoopChain(ctx, fixture.sessionID); err != nil {
			t.Fatalf("seed loop chain counter error: %v", err)
		}
	}

	resolveGroupMentionDispatchNormalization(
		ctx,
		fixture.sessionID,
		fixture.senderID,
		1,
		0,
		"人类插了一句话",
		nil,
	)

	const quotedMessageID = int64(18889990900)
	if err := store.DB.Create(&model.Message{
		MsgID:      quotedMessageID,
		SessionID:  fixture.sessionID,
		SenderID:   fixture.agentIDs[1],
		SenderType: 2,
		MsgType:    1,
		Content:    "agent B 在人类插话之后的消息",
		CreatedAt:  time.Now().UTC().Add(-time.Second),
	}).Error; err != nil {
		t.Fatalf("create quoted message error: %v", err)
	}

	TriggerDirectRouteForMessage(
		fixture.hub,
		ctx,
		fixture.sessionID,
		fixture.agentIDs[0],
		2,
		18889990901,
		quotedMessageID,
		1,
		"人类插话后我接着回复",
		nil,
		nil,
		nil,
	)

	events := collectForwardedAgentEvents(t, fixture.channel, 1)
	event := events[0]
	if event.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("mirror_mode=%q want=%q (human message should have reset the loop-chain counter)", event.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

// TestHandleSendMsgAgentQuoteAgentLoopChainIncrementsOncePerRealMessage exercises
// the actual production entry point (HandleSendMsg, as a real agent connection
// hits it) rather than calling TriggerDirectRouteForMessage directly. An
// agent-origin, non-delegate send both records liveGroupSemantics for the
// message itself (send_msg.go) and dispatches direct routing via
// TriggerDirectRouteForMessage — if the latter re-resolves semantics instead of
// reusing liveGroupSemantics, the loop-chain counter gets incremented twice for
// one real message, silently halving the configured cap.
func TestHandleSendMsgAgentQuoteAgentLoopChainIncrementsOncePerRealMessage(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-agent-quote-real-path-once", 8765, 9770, 9771)
	defer fixture.cleanup()

	agentA := fixture.agentIDs[0]
	agentB := fixture.agentIDs[1]
	connA := &sendMsgMockConn{userID: agentA, deviceID: fmt.Sprintf("agent_api_%d", agentA)}
	connB := &sendMsgMockConn{userID: agentB, deviceID: fmt.Sprintf("agent_api_%d", agentB)}

	pktB := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "loop-once-b-open",
		MsgType:     1,
		Content:     "我先说一句",
	})
	HandleSendMsg(fixture.hub, connB, pktB)
	ackB, ok := findSendAck(connB.sent)
	if !ok {
		t.Fatalf("agent B send should ack, got=%#v", connB.sent)
	}
	collectForwardedAgentEvents(t, fixture.channel, 1)

	pktA := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:       fixture.sessionID,
		ClientMsgID:     "loop-once-a-reply",
		MsgType:         1,
		Content:         "我引用你回复",
		QuotedMessageID: ackB.MsgID,
	})
	HandleSendMsg(fixture.hub, connA, pktA)
	collectForwardedAgentEvents(t, fixture.channel, 1)

	count, err := store.RDB.Get(context.Background(), agentAutoLoopChainKey(fixture.sessionID)).Int64()
	if err != nil {
		t.Fatalf("read loop chain counter error: %v", err)
	}
	if count != 1 {
		t.Fatalf("loop chain counter=%d want=1 (HandleSendMsg's real dispatch path must not double-resolve semantics for the same message)", count)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestAgentExplicitMentionStillDispatchesProcessing(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-agent-explicit-mention-processing", 8761, 9763, 9764)
	defer fixture.cleanup()

	TriggerDirectRouteForMessage(
		fixture.hub,
		context.Background(),
		fixture.sessionID,
		fixture.agentIDs[0],
		2,
		18889990710,
		0,
		1,
		fmt.Sprintf("@%d 你来处理", fixture.agentIDs[1]),
		nil,
		nil,
		nil,
	)

	events := collectForwardedAgentEvents(t, fixture.channel, 1)
	event := events[0]
	if event.AgentID != fixture.agentIDs[1] {
		t.Fatalf("agent_id=%d want=%d", event.AgentID, fixture.agentIDs[1])
	}
	if event.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("agent explicit @mention: mirror_mode=%q want=%q", event.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
	}
	if event.EventType != "group_mention" {
		t.Fatalf("event_type=%s want=group_mention", event.EventType)
	}
	assertMentionIDsEqual(t, event.MentionUserIDs, fixture.agentIDs[1])
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestHumanQuoteAgentMessageStillDispatchesProcessing(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-human-quote-agent-processing", 8762, 9765)
	defer fixture.cleanup()

	const quotedMessageID = int64(18889990720)
	if err := store.DB.Create(&model.Message{
		MsgID:      quotedMessageID,
		SessionID:  fixture.sessionID,
		SenderID:   fixture.agentIDs[0],
		SenderType: 2,
		MsgType:    1,
		Content:    "agent 先说的话",
		CreatedAt:  time.Now().UTC().Add(-time.Second),
	}).Error; err != nil {
		t.Fatalf("create quoted message error: %v", err)
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:       fixture.sessionID,
		ClientMsgID:     "human-quote-agent-processing",
		MsgType:         1,
		Content:         "回复你说的",
		QuotedMessageID: quotedMessageID,
	})

	HandleSendMsg(fixture.hub, fixture.senderConn, pkt)

	events := collectForwardedAgentEvents(t, fixture.channel, 1)
	event := events[0]
	if event.AgentID != fixture.agentIDs[0] {
		t.Fatalf("agent_id=%d want=%d", event.AgentID, fixture.agentIDs[0])
	}
	if event.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("human quote agent: mirror_mode=%q want=%q (should be processing)", event.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
	}
	if event.EventType != "group_mention" {
		t.Fatalf("event_type=%s want=group_mention", event.EventType)
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

// TestModeCallerBridgeQuoteAgentStillDispatchesProcessing covers session_send:
// bridge conn device_id is agent_api_* but sender_id is the human owner. Early
// mention normalization must treat them as human so quoting an agent message
// still wakes that agent (same as a real human quote).
func TestModeCallerBridgeQuoteAgentStillDispatchesProcessing(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-mode-caller-quote-agent", 8769, 9780)
	defer fixture.cleanup()

	ctx := context.Background()
	for i := 0; i < agentAutoLoopChainCap+1; i++ {
		if _, err := incrAgentAutoLoopChain(ctx, fixture.sessionID); err != nil {
			t.Fatalf("seed loop chain counter error: %v", err)
		}
	}

	const quotedMessageID = int64(18889990780)
	if err := store.DB.Create(&model.Message{
		MsgID:      quotedMessageID,
		SessionID:  fixture.sessionID,
		SenderID:   fixture.agentIDs[0],
		SenderType: 2,
		MsgType:    1,
		Content:    "dispatcher agent anchor",
		CreatedAt:  time.Now().UTC().Add(-time.Second),
	}).Error; err != nil {
		t.Fatalf("create quoted message error: %v", err)
	}

	callerConn := &sendMsgMockConn{
		userID:   fixture.senderID,
		deviceID: fmt.Sprintf("agent_api_%d", fixture.agentIDs[0]+100),
	}
	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:       fixture.sessionID,
		ClientMsgID:     "mode-caller-quote-agent",
		MsgType:         1,
		Content:         "[dispatch-result]\n**status**:\n```text\ncompleted\n```\n[/dispatch-result]",
		QuotedMessageID: quotedMessageID,
		Extra:           json.RawMessage(`{"agent_api_origin":true}`),
	})

	HandleSendMsg(fixture.hub, callerConn, pkt)

	events := collectForwardedAgentEvents(t, fixture.channel, 1)
	event := events[0]
	if event.AgentID != fixture.agentIDs[0] {
		t.Fatalf("agent_id=%d want=%d", event.AgentID, fixture.agentIDs[0])
	}
	if event.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("mode-caller quote agent: mirror_mode=%q want=%q", event.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
	}
	if event.EventType != "group_mention" {
		t.Fatalf("event_type=%s want=group_mention", event.EventType)
	}
	if exists, err := store.RDB.Exists(ctx, agentAutoLoopChainKey(fixture.sessionID)).Result(); err != nil {
		t.Fatalf("read loop chain counter error: %v", err)
	} else if exists != 0 {
		t.Fatalf("ModeCaller owner send must reset an over-limit agent loop chain")
	}
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

// TestSessionSendOwnerIdentityDoesNotWakeOriginAgent: agent 用 session_send 以主人身份
// 发消息（sender_type=1, extra.origin_agent_id=自己）时，群聊接续语义会把「刚说完话的
// agent」当目标——不能把它自己唤醒；引用自己上一条消息同样不能。
func TestSessionSendOwnerIdentityDoesNotWakeOriginAgent(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-owner-send-no-self-wake", 8771, 9782)
	defer fixture.cleanup()
	originAgentID := fixture.agentIDs[0]

	const lastMsgID = int64(18889990791)
	if err := store.DB.Create(&model.Message{
		MsgID: lastMsgID, SessionID: fixture.sessionID,
		SenderID: originAgentID, SenderType: 2, MsgType: 1,
		Content: "agent previous reply", CreatedAt: time.Now().UTC().Add(-time.Second),
	}).Error; err != nil {
		t.Fatalf("create last message error: %v", err)
	}
	if err := store.DB.Model(&model.Session{}).Where("session_id = ?", fixture.sessionID).
		Update("last_msg_id", lastMsgID).Error; err != nil {
		t.Fatalf("update last_msg_id error: %v", err)
	}

	send := func(clientMsgID string, quoted int64) {
		callerConn := &sendMsgMockConn{
			userID:   fixture.senderID,
			deviceID: fmt.Sprintf("agent_api_%d", originAgentID),
		}
		pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
			SessionID:       fixture.sessionID,
			ClientMsgID:     clientMsgID,
			MsgType:         1,
			Content:         "owner-identity message from agent",
			QuotedMessageID: quoted,
			Extra:           json.RawMessage(fmt.Sprintf(`{"agent_api_origin":true,"origin_agent_id":"%d"}`, originAgentID)),
		})
		HandleSendMsg(fixture.hub, callerConn, pkt)
	}

	send("owner-send-continuation", 0)
	assertNoMoreForwardedAgentEvents(t, fixture.channel)

	send("owner-send-quote-self", lastMsgID)
	assertNoMoreForwardedAgentEvents(t, fixture.channel)
}

func TestParseOriginAgentID(t *testing.T) {
	cases := map[string]int64{
		``:                                 0,
		`{}`:                               0,
		`{"origin_agent_id":"abc"}`:        0,
		`{"origin_agent_id":"0"}`:          0,
		`{"origin_agent_id":" 9782 "}`:     9782,
		`{"origin_agent_id":"9782","x":1}`: 9782,
	}
	for raw, want := range cases {
		if got := parseOriginAgentID(json.RawMessage(raw)); got != want {
			t.Fatalf("parseOriginAgentID(%s)=%d want %d", raw, got, want)
		}
	}
}

// 普通客户端连接透传的 origin_agent_id 必须被剥掉，不能借此抑制指定 agent 的唤醒。
func TestHumanClientForgedOriginAgentIDIsStripped(t *testing.T) {
	fixture := setupMultiAgentGroupFixture(t, "session-forged-origin-agent", 8772, 9783)
	defer fixture.cleanup()
	agentID := fixture.agentIDs[0]

	humanConn := &sendMsgMockConn{userID: fixture.senderID, deviceID: "ios-device"}
	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   fixture.sessionID,
		ClientMsgID: "forged-origin-agent",
		MsgType:     1,
		Content:     fmt.Sprintf("@agent_%d hi", agentID),
		Extra:       json.RawMessage(fmt.Sprintf(`{"mention_user_ids":["%d"],"origin_agent_id":"%d"}`, agentID, agentID)),
	})
	HandleSendMsg(fixture.hub, humanConn, pkt)

	events := collectForwardedAgentEvents(t, fixture.channel, 1)
	if events[0].AgentID != agentID || events[0].MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("forged origin_agent_id must not suppress wake: agent=%d mode=%s", events[0].AgentID, events[0].MirrorMode)
	}
	var stored model.Message
	if err := store.DB.Where("session_id = ? AND msg_id = ?", fixture.sessionID, events[0].MsgID).First(&stored).Error; err != nil {
		t.Fatalf("load stored message: %v", err)
	}
	if parseOriginAgentID(json.RawMessage(stored.Extra)) != 0 {
		t.Fatalf("origin_agent_id must be stripped from stored extra: %s", string(stored.Extra))
	}
}
