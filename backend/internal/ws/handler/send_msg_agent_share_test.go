package handler

import (
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// send_msg 共享授权护栏(send_msg.go:202-215)端到端守卫:
//   - 被共享者 B 用 A 共享的 agent 发消息 → 通过 CanUseAgent → 不被 4003 拒
//   - 撤销共享后 → CanUseAgent 即时返 false → 4003
//   - B 被封号后 → hasActiveAgentShare 查 user.status 失败 → 4003
//
// 这三条共同守:撤销/封号「下一条消息即生效」,不依赖连接断开/重连。

const (
	shareSendOwnerID  = int64(7801)
	shareSendUserB    = int64(7802)
	shareSendAgentID  = int64(9981)
	shareSendSessID   = "session-share-send-1"
	shareSendPlainKey = "ak-share-send"
)

func seedShareSendFixture(t *testing.T) {
	t.Helper()
	users := []model.User{
		{ID: shareSendOwnerID, Username: "share-send-owner", Email: "share-send-owner@test.local", Status: model.UserStatusActive},
		{ID: shareSendUserB, Username: "share-send-b", Email: "share-send-b@test.local", Status: model.UserStatusActive},
	}
	for i := range users {
		if err := store.DB.Create(&users[i]).Error; err != nil {
			t.Fatalf("seed user %d: %v", users[i].ID, err)
		}
	}
	if err := store.DB.Create(&model.Agent{
		ID:           shareSendAgentID,
		AgentName:    "share-send-agent",
		OwnerID:      shareSendOwnerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
	}).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if err := store.DB.Create(&model.Session{
		SessionID:   shareSendSessID,
		OwnerID:     shareSendUserB,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: shareSendSessID, MemberID: shareSendUserB, MemberType: 1},
		{SessionID: shareSendSessID, MemberID: shareSendAgentID, MemberType: 2},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("seed session member %d: %v", m.MemberID, err)
		}
	}
}

func hasSendNack4003(c *sendMsgMockConn) bool {
	for _, item := range c.sent {
		if item.cmd != protocol.CmdSendNack {
			continue
		}
		if nack, ok := item.payload.(protocol.SendNackPayload); ok && nack.Code == 4003 {
			return true
		}
	}
	return false
}

// 被共享者 B 用 A 共享的 agent 发消息 → 通过授权护栏(无 4003)。
func TestHandleSendMsgAllowsActiveSharedUser(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()
	seedShareSendFixture(t)

	if err := store.DB.Create(&model.AgentShare{
		ID: 1, AgentID: shareSendAgentID, OwnerID: shareSendOwnerID,
		SharedTo: shareSendUserB, Status: model.AgentShareStatusActive,
	}).Error; err != nil {
		t.Fatalf("seed agent_share: %v", err)
	}

	sender := &sendMsgMockConn{userID: shareSendUserB, deviceID: "share-b-dev"}
	hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{shareSendUserB: {sender}}}
	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID: shareSendSessID, ClientMsgID: "share-b-1", MsgType: 1, Content: "hi from B",
	})

	HandleSendMsg(hub, sender, pkt)

	if hasSendNack4003(sender) {
		t.Fatalf("active shared user B must NOT be rejected by 4003, got sent=%#v", sender.sent)
	}
	// 走通的标志:落库了 B 的消息
	var msgCount int64
	store.DB.Model(&model.Message{}).Where("session_id = ? AND sender_id = ?", shareSendSessID, shareSendUserB).Count(&msgCount)
	if msgCount != 1 {
		t.Fatalf("B's send_msg must persist exactly one message, got %d", msgCount)
	}
}

// 共享撤销 → 下一条消息即被 4003 拒(运行时 CanUseAgent 校验,不等连接断开)。
func TestHandleSendMsgRejectsRevokedSharedUser(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()
	seedShareSendFixture(t)

	if err := store.DB.Create(&model.AgentShare{
		ID: 1, AgentID: shareSendAgentID, OwnerID: shareSendOwnerID,
		SharedTo: shareSendUserB, Status: model.AgentShareStatusRevoked,
	}).Error; err != nil {
		t.Fatalf("seed revoked share: %v", err)
	}

	sender := &sendMsgMockConn{userID: shareSendUserB, deviceID: "share-b-dev"}
	hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{shareSendUserB: {sender}}}
	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID: shareSendSessID, ClientMsgID: "share-b-revoked", MsgType: 1, Content: "should be blocked",
	})

	HandleSendMsg(hub, sender, pkt)

	if !hasSendNack4003(sender) {
		t.Fatalf("revoked share user B must be rejected by 4003, got sent=%#v", sender.sent)
	}
	var msgCount int64
	store.DB.Model(&model.Message{}).Where("session_id = ?", shareSendSessID).Count(&msgCount)
	if msgCount != 0 {
		t.Fatalf("revoked share user must not persist any message, got %d", msgCount)
	}
}

// 被共享者封号 → 下一条消息即被 4003 拒(hasActiveAgentShare 查 user.status 失败)。
// 同时守 enforceAuthorizedShareConns/agentShareActive 与发送护栏三处口径一致。
func TestHandleSendMsgRejectsBannedSharedUser(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()
	seedShareSendFixture(t)

	// 共享行仍是 status=1 不动,只把 B 用户封号 → 共享视为自动失效
	if err := store.DB.Create(&model.AgentShare{
		ID: 1, AgentID: shareSendAgentID, OwnerID: shareSendOwnerID,
		SharedTo: shareSendUserB, Status: model.AgentShareStatusActive,
	}).Error; err != nil {
		t.Fatalf("seed active share: %v", err)
	}
	if err := store.DB.Model(&model.User{}).
		Where("id = ?", shareSendUserB).
		Update("status", model.UserStatusBanned).Error; err != nil {
		t.Fatalf("ban B: %v", err)
	}

	sender := &sendMsgMockConn{userID: shareSendUserB, deviceID: "share-b-dev"}
	hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{shareSendUserB: {sender}}}
	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID: shareSendSessID, ClientMsgID: "share-b-banned", MsgType: 1, Content: "should be blocked too",
	})

	HandleSendMsg(hub, sender, pkt)

	if !hasSendNack4003(sender) {
		t.Fatalf("banned shared user B must be rejected by 4003 (cascading), got sent=%#v", sender.sent)
	}
	var msgCount int64
	store.DB.Model(&model.Message{}).Where("session_id = ?", shareSendSessID).Count(&msgCount)
	if msgCount != 0 {
		t.Fatalf("banned shared user must not persist any message, got %d", msgCount)
	}
}
