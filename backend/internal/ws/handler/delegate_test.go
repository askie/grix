package handler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func makeDelegateStartPacket(t *testing.T, payload protocol.DelegateStartPayload) *protocol.Packet {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal delegate_start payload error: %v", err)
	}
	return &protocol.Packet{
		Cmd:     protocol.CmdDelegateStart,
		Seq:     101,
		Payload: raw,
	}
}

func makeDelegateStopPacket(t *testing.T, payload protocol.DelegateStopPayload) *protocol.Packet {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal delegate_stop payload error: %v", err)
	}
	return &protocol.Packet{
		Cmd:     protocol.CmdDelegateStop,
		Seq:     102,
		Payload: raw,
	}
}

func TestHandleDelegateStartDefaultMaxConsecutiveReplies(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-delegate-start-default"
		userID    = int64(8001)
		agentID   = int64(9001)
	)

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     userID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{
		SessionID:  sessionID,
		MemberID:   userID,
		MemberType: 1,
	}).Error; err != nil {
		t.Fatalf("create session member error: %v", err)
	}
	if err := store.DB.Create(&model.Agent{
		ID:        agentID,
		OwnerID:   userID,
		AgentName: "delegate-bot",
		Status:    1,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}

	conn := &sendMsgMockConn{userID: userID, deviceID: "dev-origin"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			userID: {conn},
		},
	}

	HandleDelegateStart(hub, conn, makeDelegateStartPacket(t, protocol.DelegateStartPayload{
		SessionID: sessionID,
		AgentID:   agentID,
	}))

	if len(conn.sent) == 0 {
		t.Fatalf("delegate_start should send delegate_ack")
	}
	ack, ok := conn.sent[len(conn.sent)-1].payload.(protocol.DelegateAckPayload)
	if !ok {
		t.Fatalf("expected DelegateAckPayload, got=%T", conn.sent[len(conn.sent)-1].payload)
	}
	if !ack.Active {
		t.Fatalf("delegate_start should set active=true")
	}
	if ack.MaxConsecutiveReplies != defaultDelegateMaxConsecutiveReplies {
		t.Fatalf("delegate_start default max mismatch, got=%d want=%d",
			ack.MaxConsecutiveReplies, defaultDelegateMaxConsecutiveReplies)
	}

	delegateKey := "im:delegate:" + sessionID + ":8001"
	maxRaw, err := store.RDB.HGet(context.Background(), delegateKey, "max_consecutive_replies").Result()
	if err != nil {
		t.Fatalf("read redis delegate max error: %v", err)
	}
	if maxRaw != "10" {
		t.Fatalf("redis max_consecutive_replies should default to 10, got=%s", maxRaw)
	}
}

func TestHandleDelegateStartRejectsAPIAgentWhenChannelUnavailable(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-delegate-start-api-offline"
		userID    = int64(8002)
		agentID   = int64(9002)
	)

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(nil)
	defer wsagentapi.SetGlobal(prevManager)

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     userID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{
		SessionID:  sessionID,
		MemberID:   userID,
		MemberType: 1,
	}).Error; err != nil {
		t.Fatalf("create session member error: %v", err)
	}
	if err := store.DB.Create(&model.Agent{
		ID:           agentID,
		OwnerID:      userID,
		AgentName:    "delegate-api-offline",
		ProviderType: model.AgentProviderAPI,
		Status:       1,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}

	conn := &sendMsgMockConn{userID: userID, deviceID: "dev-origin"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			userID: {conn},
		},
	}

	HandleDelegateStart(hub, conn, makeDelegateStartPacket(t, protocol.DelegateStartPayload{
		SessionID: sessionID,
		AgentID:   agentID,
	}))

	if len(conn.sent) != 2 {
		t.Fatalf("delegate_start api offline should send error and ack, got=%#v", conn.sent)
	}
	if conn.sent[0].cmd != protocol.CmdAgentDeliveryStatus {
		t.Fatalf("first cmd=%s want=%s", conn.sent[0].cmd, protocol.CmdAgentDeliveryStatus)
	}
	errPayload, ok := conn.sent[0].payload.(protocol.AgentDeliveryStatusPayload)
	if !ok {
		t.Fatalf("first payload should be AgentDeliveryStatusPayload, got=%T", conn.sent[0].payload)
	}
	if errPayload.Status != protocol.AgentDeliveryStatusFailed {
		t.Fatalf("status=%q want=%q", errPayload.Status, protocol.AgentDeliveryStatusFailed)
	}
	if errPayload.Scope != protocol.AgentDeliveryScopeDelegate {
		t.Fatalf("scope=%q want=%q", errPayload.Scope, protocol.AgentDeliveryScopeDelegate)
	}
	if errPayload.Code != protocol.AgentDeliveryCodeChannelUnavailable {
		t.Fatalf("code=%q want=%q", errPayload.Code, protocol.AgentDeliveryCodeChannelUnavailable)
	}

	if conn.sent[1].cmd != protocol.CmdDelegateAck {
		t.Fatalf("second cmd=%s want=%s", conn.sent[1].cmd, protocol.CmdDelegateAck)
	}
	ack, ok := conn.sent[1].payload.(protocol.DelegateAckPayload)
	if !ok {
		t.Fatalf("second payload should be DelegateAckPayload, got=%T", conn.sent[1].payload)
	}
	if ack.Active {
		t.Fatalf("delegate_start api offline must reject active delegation")
	}

	exists, err := store.RDB.Exists(context.Background(), "im:delegate:"+sessionID+":8002").Result()
	if err != nil {
		t.Fatalf("check delegate key exists error: %v", err)
	}
	if exists != 0 {
		t.Fatalf("delegate key should not exist after rejected start")
	}
}

func TestHandleDelegateStartUpdateKeepsStreak(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-delegate-start-update"
		userID    = int64(8101)
		agentID   = int64(9101)
	)

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     userID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{
		SessionID:  sessionID,
		MemberID:   userID,
		MemberType: 1,
	}).Error; err != nil {
		t.Fatalf("create session member error: %v", err)
	}
	if err := store.DB.Create(&model.Agent{
		ID:        agentID,
		OwnerID:   userID,
		AgentName: "delegate-bot-update",
		Status:    1,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}

	ctx := context.Background()
	delegateKey := "im:delegate:" + sessionID + ":8101"
	if err := store.RDB.HSet(ctx, delegateKey,
		"agent_id", "9101",
		"max_consecutive_replies", "10",
	).Err(); err != nil {
		t.Fatalf("seed delegate key error: %v", err)
	}
	streakKey := delegateStreakKey(sessionID, userID)
	if err := store.RDB.Set(ctx, streakKey, 4, 0).Err(); err != nil {
		t.Fatalf("seed streak key error: %v", err)
	}

	conn := &sendMsgMockConn{userID: userID, deviceID: "dev-origin"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			userID: {conn},
		},
	}

	HandleDelegateStart(hub, conn, makeDelegateStartPacket(t, protocol.DelegateStartPayload{
		SessionID:             sessionID,
		AgentID:               agentID,
		MaxConsecutiveReplies: 12,
	}))

	if len(conn.sent) == 0 {
		t.Fatalf("delegate_start update should send delegate_ack")
	}
	ack, ok := conn.sent[len(conn.sent)-1].payload.(protocol.DelegateAckPayload)
	if !ok {
		t.Fatalf("expected DelegateAckPayload, got=%T", conn.sent[len(conn.sent)-1].payload)
	}
	if !ack.Active {
		t.Fatalf("delegate update should keep active=true")
	}
	if ack.MaxConsecutiveReplies != 12 {
		t.Fatalf("delegate update max mismatch, got=%d want=12", ack.MaxConsecutiveReplies)
	}

	maxRaw, err := store.RDB.HGet(ctx, delegateKey, "max_consecutive_replies").Result()
	if err != nil {
		t.Fatalf("read updated delegate max error: %v", err)
	}
	if maxRaw != "12" {
		t.Fatalf("redis max_consecutive_replies should update to 12, got=%s", maxRaw)
	}

	streak, err := store.RDB.Get(ctx, streakKey).Int()
	if err != nil {
		t.Fatalf("read streak error: %v", err)
	}
	if streak != 4 {
		t.Fatalf("delegate update should not reset streak, got=%d want=4", streak)
	}
}

func TestHandleDelegateStartStopSessionScoped(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionA = "session-delegate-scope-a"
		sessionB = "session-delegate-scope-b"
		ownerID  = int64(8201)
		peerID   = int64(8202)
		agentID  = int64(9201)
	)

	now := time.Now()
	for _, sid := range []string{sessionA, sessionB} {
		if err := store.DB.Create(&model.Session{
			SessionID:   sid,
			OwnerID:     ownerID,
			SessionType: 1,
			CreatedAt:   now,
			UpdatedAt:   now,
		}).Error; err != nil {
			t.Fatalf("create session %s error: %v", sid, err)
		}
		members := []model.SessionMember{
			{SessionID: sid, MemberID: ownerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
			{SessionID: sid, MemberID: peerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		}
		for _, m := range members {
			if err := store.DB.Create(&m).Error; err != nil {
				t.Fatalf("create session member sid=%s mid=%d error: %v", sid, m.MemberID, err)
			}
		}
	}
	if err := store.DB.Create(&model.Agent{
		ID:        agentID,
		OwnerID:   ownerID,
		AgentName: "delegate-scope-agent",
		Status:    1,
		CreatedAt: now,
		UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}

	conn := &sendMsgMockConn{userID: ownerID, deviceID: "dev-origin"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			ownerID: {conn},
		},
	}

	ctx := context.Background()
	keyA := "im:delegate:" + sessionA + ":8201"
	keyB := "im:delegate:" + sessionB + ":8201"

	HandleDelegateStart(hub, conn, makeDelegateStartPacket(t, protocol.DelegateStartPayload{
		SessionID: sessionA,
		AgentID:   agentID,
	}))

	if exists, err := store.RDB.Exists(ctx, keyA).Result(); err != nil || exists != 1 {
		t.Fatalf("sessionA delegate key should exist after start, exists=%d err=%v", exists, err)
	}
	if exists, err := store.RDB.Exists(ctx, keyB).Result(); err != nil || exists != 0 {
		t.Fatalf("sessionB delegate key should remain absent, exists=%d err=%v", exists, err)
	}

	HandleDelegateStart(hub, conn, makeDelegateStartPacket(t, protocol.DelegateStartPayload{
		SessionID: sessionB,
		AgentID:   agentID,
	}))

	if exists, err := store.RDB.Exists(ctx, keyB).Result(); err != nil || exists != 1 {
		t.Fatalf("sessionB delegate key should exist after its own start, exists=%d err=%v", exists, err)
	}

	HandleDelegateStop(hub, conn, makeDelegateStopPacket(t, protocol.DelegateStopPayload{
		SessionID: sessionA,
	}))

	if exists, err := store.RDB.Exists(ctx, keyA).Result(); err != nil || exists != 0 {
		t.Fatalf("sessionA delegate key should be removed after stop, exists=%d err=%v", exists, err)
	}
	if exists, err := store.RDB.Exists(ctx, keyB).Result(); err != nil || exists != 1 {
		t.Fatalf("sessionB delegate key should stay active, exists=%d err=%v", exists, err)
	}

	if len(conn.sent) < 3 {
		t.Fatalf("expected at least 3 delegate acknowledgements, got=%d", len(conn.sent))
	}
	lastAck, ok := conn.sent[len(conn.sent)-1].payload.(protocol.DelegateAckPayload)
	if !ok {
		t.Fatalf("expected DelegateAckPayload, got=%T", conn.sent[len(conn.sent)-1].payload)
	}
	if lastAck.SessionID != sessionA || lastAck.Active {
		t.Fatalf("stop ack mismatch: session=%s active=%v", lastAck.SessionID, lastAck.Active)
	}
}
