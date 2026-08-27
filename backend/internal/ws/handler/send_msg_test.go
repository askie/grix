package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/askie/grix/backend/internal/agentreceive"
	apiservice "github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/datatypes"
)

type sentPayload struct {
	cmd     string
	seq     int64
	payload interface{}
}

type sendMsgMockConn struct {
	userID   int64
	deviceID string
	platform string
	seq      int64
	sent     []sentPayload
}

func countSentCmd(sent []sentPayload, cmd string) int {
	count := 0
	for _, item := range sent {
		if item.cmd == cmd {
			count++
		}
	}
	return count
}

func findSendAck(sent []sentPayload) (protocol.SendAckPayload, bool) {
	for _, item := range sent {
		if item.cmd != protocol.CmdSendAck {
			continue
		}
		ack, ok := item.payload.(protocol.SendAckPayload)
		if ok {
			return ack, true
		}
	}
	return protocol.SendAckPayload{}, false
}

func findAgentDeliveryStatus(sent []sentPayload) (protocol.AgentDeliveryStatusPayload, bool) {
	for _, item := range sent {
		if item.cmd != protocol.CmdAgentDeliveryStatus {
			continue
		}
		status, ok := item.payload.(protocol.AgentDeliveryStatusPayload)
		if ok {
			return status, true
		}
	}
	return protocol.AgentDeliveryStatusPayload{}, false
}

func findPushMsg(sent []sentPayload) (protocol.PushMsgPayload, bool) {
	for _, item := range sent {
		if item.cmd != protocol.CmdPushMsg {
			continue
		}
		push, ok := item.payload.(protocol.PushMsgPayload)
		if ok {
			return push, true
		}
	}
	return protocol.PushMsgPayload{}, false
}

func findRelayLocalStreamStartAck(sent []sentPayload) (protocol.RelayLocalStreamStartAckPayload, bool) {
	for _, item := range sent {
		if item.cmd != protocol.CmdRelayLocalStreamStartAck {
			continue
		}
		ack, ok := item.payload.(protocol.RelayLocalStreamStartAckPayload)
		if ok {
			return ack, true
		}
	}
	return protocol.RelayLocalStreamStartAckPayload{}, false
}

func (c *sendMsgMockConn) SendPayload(cmd string, seq int64, payload interface{}) {
	c.sent = append(c.sent, sentPayload{cmd: cmd, seq: seq, payload: payload})
}

func (c *sendMsgMockConn) SendPacket(pkt *protocol.Packet) {}

func (c *sendMsgMockConn) AckPush(msgID int64) {}

func (c *sendMsgMockConn) Close() {}

func (c *sendMsgMockConn) NextSeq() int64 {
	c.seq++
	return c.seq
}

func (c *sendMsgMockConn) GetUserID() int64 { return c.userID }

func (c *sendMsgMockConn) GetDeviceID() string { return c.deviceID }

func (c *sendMsgMockConn) GetPlatform() string { return c.platform }

func (c *sendMsgMockConn) SetAuth(userID int64, sessionID, deviceID, platform string) {}

func (c *sendMsgMockConn) IsAuthed() bool { return true }

type sendMsgMockHub struct {
	nodeID string
	conns  map[int64][]ConnInterface
}

func (h *sendMsgMockHub) Register(c ConnInterface) {}

func (h *sendMsgMockHub) Unregister(c ConnInterface) {}

func (h *sendMsgMockHub) RefreshAlive(c ConnInterface) {}

func (h *sendMsgMockHub) GetUserConns(userID int64) []ConnInterface {
	return h.conns[userID]
}

func (h *sendMsgMockHub) GetNodeID() string { return h.nodeID }

func setupSendMsgTest(t *testing.T) func() {
	t.Helper()
	logger.Init()
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	clearSyncMap(&localStreamRegistry)
	clearSyncMap(&localStreamRequestRegistry)
	originalScheduleContentModeration := scheduleContentModeration

	if err := store.DB.AutoMigrate(&model.UserInbox{}); err != nil {
		t.Fatalf("auto migrate user_inbox error: %v", err)
	}

	return func() {
		// 先停内容审核 worker（取消 ctx 并等它们退出），再关 Redis/DB。
		// 它们是跨包的全局常驻协程，不停就会活过本用例、去读下一个用例的全局状态。
		apiservice.StopContentModerationWorkers()
		scheduleContentModeration = originalScheduleContentModeration
		_ = store.RDB.Close()
		testDB.Close()
	}
}

func clearSyncMap(m *sync.Map) {
	if m == nil {
		return
	}
	m.Range(func(key, value any) bool {
		m.Delete(key)
		return true
	})
}

var friendRelationIDCounter atomic.Int64

func seedSendMsgFriendRelation(t *testing.T, userID int64, friendID int64) {
	t.Helper()
	// Coarse Windows clock granularity makes consecutive UnixNano() calls
	// return identical values; combined with symmetric userID+friendID sums
	// (e.g. 1101+1102 == 1102+1101) this collides on friends.id. A monotonic
	// counter guarantees uniqueness regardless of clock resolution.
	rel := model.Friend{
		ID:       time.Now().UnixNano() + friendRelationIDCounter.Add(1),
		UserID:   userID,
		FriendID: friendID,
	}
	if err := store.DB.Create(&rel).Error; err != nil {
		t.Fatalf("seed friend relation %d->%d error: %v", userID, friendID, err)
	}
}

func seedSendMsgIdleGroupHistory(
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

func makeSendMsgPacket(t *testing.T, payload protocol.SendMsgPayload) *protocol.Packet {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal send_msg payload error: %v", err)
	}
	return &protocol.Packet{
		Cmd:     protocol.CmdSendMsg,
		Seq:     100,
		Payload: raw,
	}
}

func TestHandleSendMsgPermanentDedupSurvivesRedisLoss(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID   = "session-permanent-dedup"
		senderID    = int64(8901)
		clientMsgID = "gemini_terminal_status_evt-permanent"
	)
	if err := store.DB.Create(&model.Session{
		SessionID: sessionID, OwnerID: senderID, SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{
		SessionID: sessionID, MemberID: senderID, MemberType: 2,
	}).Error; err != nil {
		t.Fatalf("create member: %v", err)
	}
	hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{}}
	payload := protocol.SendMsgPayload{
		SessionID: sessionID, ClientMsgID: clientMsgID, MsgType: 1, Content: "done",
	}
	first := &sendMsgMockConn{userID: senderID, deviceID: "agent_api_8901"}
	HandleSendMsg(hub, first, makeSendMsgPacket(t, payload))
	firstAck, ok := findSendAck(first.sent)
	if !ok {
		t.Fatalf("first send missing ACK: %#v", first.sent)
	}

	// Model a crash/reclaim after the message transaction committed and after
	// the 6-hour Redis fast-path key expired.
	dedupKey := fmt.Sprintf("msg:dedup:%d:%s:%s", senderID, sessionID, clientMsgID)
	if err := store.RDB.Del(context.Background(), dedupKey).Err(); err != nil {
		t.Fatalf("delete Redis dedup key: %v", err)
	}
	second := &sendMsgMockConn{userID: senderID, deviceID: "agent_api_8901"}
	HandleSendMsg(hub, second, makeSendMsgPacket(t, payload))
	secondAck, ok := findSendAck(second.sent)
	if !ok {
		t.Fatalf("replayed send missing ACK: %#v", second.sent)
	}
	if firstAck.MsgID != secondAck.MsgID ||
		firstAck.InboxSeq != secondAck.InboxSeq ||
		firstAck.CreatedAt != secondAck.CreatedAt {
		t.Fatalf("permanent receipt mismatch first=%+v second=%+v", firstAck, secondAck)
	}

	var messageCount int64
	if err := store.DB.Model(&model.Message{}).
		Where("session_id = ? AND sender_id = ?", sessionID, senderID).
		Count(&messageCount).Error; err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if messageCount != 1 {
		t.Fatalf("message count=%d want=1", messageCount)
	}
	var inboxCount int64
	if err := store.DB.Model(&model.UserInbox{}).
		Where("session_id = ? AND user_id = ?", sessionID, senderID).
		Count(&inboxCount).Error; err != nil {
		t.Fatalf("count sender inbox: %v", err)
	}
	if inboxCount != 1 {
		t.Fatalf("sender inbox count=%d want=1", inboxCount)
	}
}

func TestHandleSendMsgPermanentDedupIsScopedBySessionAndSender(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const clientMsgID = "shared-client-id"
	for index, fixture := range []struct {
		sessionID string
		senderID  int64
	}{
		{sessionID: "session-dedup-scope-a", senderID: 8911},
		{sessionID: "session-dedup-scope-b", senderID: 8911},
		{sessionID: "session-dedup-scope-c", senderID: 8912},
	} {
		if err := store.DB.Create(&model.Session{
			SessionID:   fixture.sessionID,
			OwnerID:     fixture.senderID,
			SessionType: 1,
		}).Error; err != nil {
			t.Fatalf("create session %d: %v", index, err)
		}
		if err := store.DB.Create(&model.SessionMember{
			SessionID:  fixture.sessionID,
			MemberID:   fixture.senderID,
			MemberType: 2,
		}).Error; err != nil {
			t.Fatalf("create member %d: %v", index, err)
		}
		conn := &sendMsgMockConn{
			userID:   fixture.senderID,
			deviceID: fmt.Sprintf("agent_api_%d", fixture.senderID),
		}
		HandleSendMsg(
			&sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{}},
			conn,
			makeSendMsgPacket(t, protocol.SendMsgPayload{
				SessionID:   fixture.sessionID,
				ClientMsgID: clientMsgID,
				MsgType:     1,
				Content:     "scoped",
			}),
		)
		if _, ok := findSendAck(conn.sent); !ok {
			t.Fatalf("send %d missing ACK: %#v", index, conn.sent)
		}
	}
	var count int64
	if err := store.DB.Model(&model.SendMsgIdempotencyReceipt{}).
		Where("client_msg_key = ?", sendMsgClientKey(clientMsgID)).
		Count(&count).Error; err != nil {
		t.Fatalf("count scoped receipts: %v", err)
	}
	if count != 3 {
		t.Fatalf("scoped receipt count=%d want=3", count)
	}
}

func TestHandleSendMsgRejectsNonMember(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	conn := &sendMsgMockConn{userID: 9001, deviceID: "dev-origin"}
	hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{
		9001: {conn},
	}}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   "session-no-member",
		ClientMsgID: "cmsg-1",
		MsgType:     1,
		Content:     "hello",
	})

	HandleSendMsg(hub, conn, pkt)

	var msgCount int64
	if err := store.DB.Model(&model.Message{}).Count(&msgCount).Error; err != nil {
		t.Fatalf("count messages error: %v", err)
	}
	if msgCount != 0 {
		t.Fatalf("non-member send should not create message, got=%d", msgCount)
	}
	if len(conn.sent) != 1 {
		t.Fatalf("non-member send should receive one send_nack, got=%d payload(s)", len(conn.sent))
	}
	if conn.sent[0].cmd != protocol.CmdSendNack {
		t.Fatalf("expected cmd=%s, got=%s", protocol.CmdSendNack, conn.sent[0].cmd)
	}
	nack, ok := conn.sent[0].payload.(protocol.SendNackPayload)
	if !ok {
		t.Fatalf("expected SendNackPayload, got=%T", conn.sent[0].payload)
	}
	if nack.Code != 4003 {
		t.Fatalf("expected send_nack code=4003, got=%d", nack.Code)
	}
}

func TestHandleSendMsgRejectsPrivateHumanPeerNotFriend(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-not-friend"
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     9001,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: 9001, MemberType: 1},
		{SessionID: sessionID, MemberID: 9002, MemberType: 1},
	}
	if err := store.DB.Create(&members).Error; err != nil {
		t.Fatalf("create session members error: %v", err)
	}

	sender := &sendMsgMockConn{userID: 9001, deviceID: "dev-origin"}
	recipient := &sendMsgMockConn{userID: 9002, deviceID: "dev-recipient"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			9001: {sender},
			9002: {recipient},
		},
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-not-friend",
		MsgType:     1,
		Content:     "blocked",
	})
	HandleSendMsg(hub, sender, pkt)

	if len(sender.sent) != 1 {
		t.Fatalf("sender should receive one response, got=%d", len(sender.sent))
	}
	if sender.sent[0].cmd != protocol.CmdSendNack {
		t.Fatalf("expected cmd=%s, got=%s", protocol.CmdSendNack, sender.sent[0].cmd)
	}
	nack, ok := sender.sent[0].payload.(protocol.SendNackPayload)
	if !ok {
		t.Fatalf("expected SendNackPayload, got=%T", sender.sent[0].payload)
	}
	if nack.Code != 4003 {
		t.Fatalf("expected send_nack code=4003, got=%d", nack.Code)
	}
	if nack.Msg != "member is not friend" {
		t.Fatalf("expected send_nack msg=member is not friend, got=%q", nack.Msg)
	}

	if len(recipient.sent) != 0 {
		t.Fatalf("recipient should not receive push, got=%d payload(s)", len(recipient.sent))
	}

	var msgCount int64
	if err := store.DB.Model(&model.Message{}).Where("session_id = ?", sessionID).Count(&msgCount).Error; err != nil {
		t.Fatalf("count message rows error: %v", err)
	}
	if msgCount != 0 {
		t.Fatalf("non-friend send should not persist messages, got=%d", msgCount)
	}
}

func TestHandleSendMsgRejectsPrivateHumanPeerBlocked(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-blocked"
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     9011,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: 9011, MemberType: 1},
		{SessionID: sessionID, MemberID: 9012, MemberType: 1},
	}
	if err := store.DB.Create(&members).Error; err != nil {
		t.Fatalf("create session members error: %v", err)
	}
	seedSendMsgFriendRelation(t, 9011, 9012)
	seedSendMsgFriendRelation(t, 9012, 9011)
	if err := store.DB.Create(&model.UserBlock{
		ID:            time.Now().UnixNano(),
		UserID:        9012,
		BlockedUserID: 9011,
	}).Error; err != nil {
		t.Fatalf("create user block error: %v", err)
	}

	sender := &sendMsgMockConn{userID: 9011, deviceID: "dev-origin"}
	recipient := &sendMsgMockConn{userID: 9012, deviceID: "dev-recipient"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			9011: {sender},
			9012: {recipient},
		},
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-blocked",
		MsgType:     1,
		Content:     "blocked",
	})
	HandleSendMsg(hub, sender, pkt)

	if len(sender.sent) != 1 {
		t.Fatalf("sender should receive one response, got=%d", len(sender.sent))
	}
	if sender.sent[0].cmd != protocol.CmdSendNack {
		t.Fatalf("expected cmd=%s, got=%s", protocol.CmdSendNack, sender.sent[0].cmd)
	}
	nack, ok := sender.sent[0].payload.(protocol.SendNackPayload)
	if !ok {
		t.Fatalf("expected SendNackPayload, got=%T", sender.sent[0].payload)
	}
	if nack.Code != 4003 {
		t.Fatalf("expected send_nack code=4003, got=%d", nack.Code)
	}
	if nack.Msg != "you have been blocked by this user" {
		t.Fatalf("expected blocked send_nack msg, got=%q", nack.Msg)
	}

	if len(recipient.sent) != 0 {
		t.Fatalf("recipient should not receive push, got=%d payload(s)", len(recipient.sent))
	}

	var msgCount int64
	if err := store.DB.Model(&model.Message{}).Where("session_id = ?", sessionID).Count(&msgCount).Error; err != nil {
		t.Fatalf("count message rows error: %v", err)
	}
	if msgCount != 0 {
		t.Fatalf("blocked send should not persist messages, got=%d", msgCount)
	}
}

func TestHandleSendMsgInvalidPayloadReturnsNack(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	conn := &sendMsgMockConn{userID: 9101, deviceID: "dev-origin"}
	hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{
		9101: {conn},
	}}

	pkt := &protocol.Packet{
		Cmd:     protocol.CmdSendMsg,
		Seq:     101,
		Payload: []byte(`{"session_id":`),
	}

	HandleSendMsg(hub, conn, pkt)

	if len(conn.sent) != 1 {
		t.Fatalf("invalid payload should receive one send_nack, got=%d", len(conn.sent))
	}
	if conn.sent[0].cmd != protocol.CmdSendNack {
		t.Fatalf("expected cmd=%s, got=%s", protocol.CmdSendNack, conn.sent[0].cmd)
	}
	nack, ok := conn.sent[0].payload.(protocol.SendNackPayload)
	if !ok {
		t.Fatalf("expected SendNackPayload, got=%T", conn.sent[0].payload)
	}
	if nack.Code != 4001 {
		t.Fatalf("expected send_nack code=4001, got=%d", nack.Code)
	}
}

func TestHandleSendMsgInvalidVisibleToTypeReturnsNackWithClientMsgID(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	conn := &sendMsgMockConn{userID: 9102, deviceID: "dev-origin"}
	hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{
		9102: {conn},
	}}

	// Phase 2.1 后 visible_to 接受 ["123","456"] 形式；这里改用真正的非法类型（对象数组）触发 4001。
	pkt := &protocol.Packet{
		Cmd: protocol.CmdSendMsg,
		Seq: 102,
		Payload: []byte(`{
			"session_id":"session-test",
			"client_msg_id":"cmsg-visible-to-str",
			"msg_type":1,
			"content":"hello",
			"visible_to":[{"id":"x"}]
		}`),
	}

	HandleSendMsg(hub, conn, pkt)

	if len(conn.sent) != 1 {
		t.Fatalf("invalid visible_to payload should receive one send_nack, got=%d", len(conn.sent))
	}
	if conn.sent[0].cmd != protocol.CmdSendNack {
		t.Fatalf("expected cmd=%s, got=%s", protocol.CmdSendNack, conn.sent[0].cmd)
	}
	nack, ok := conn.sent[0].payload.(protocol.SendNackPayload)
	if !ok {
		t.Fatalf("expected SendNackPayload, got=%T", conn.sent[0].payload)
	}
	if nack.Code != 4001 {
		t.Fatalf("expected send_nack code=4001, got=%d", nack.Code)
	}
	if nack.ClientMsgID != "cmsg-visible-to-str" {
		t.Fatalf("expected send_nack client_msg_id=cmsg-visible-to-str, got=%q", nack.ClientMsgID)
	}
}

func TestHandleSendMsgGroupVisibleToInvalidMembersReturnsNack(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-visible-to-invalid-members"
		senderID  = int64(9401)
		memberID  = int64(9402)
	)

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     senderID,
		SessionType: 2,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, member := range []model.SessionMember{
		{SessionID: sessionID, MemberID: senderID, MemberType: 1},
		{SessionID: sessionID, MemberID: memberID, MemberType: 1},
	} {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	origin := &sendMsgMockConn{userID: senderID, deviceID: "dev-origin"}
	peer := &sendMsgMockConn{userID: memberID, deviceID: "dev-peer"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID: {origin},
			memberID: {peer},
		},
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-invalid-visible-to-members",
		MsgType:     1,
		Content:     "owner-only",
		VisibleTo:   []int64{999999},
	})

	HandleSendMsg(hub, origin, pkt)

	if len(origin.sent) != 1 {
		t.Fatalf("origin sent count=%d want=1", len(origin.sent))
	}
	if origin.sent[0].cmd != protocol.CmdSendNack {
		t.Fatalf("origin first cmd=%s want=%s", origin.sent[0].cmd, protocol.CmdSendNack)
	}
	nack, ok := origin.sent[0].payload.(protocol.SendNackPayload)
	if !ok {
		t.Fatalf("origin first payload type=%T want=%T", origin.sent[0].payload, protocol.SendNackPayload{})
	}
	if nack.Code != 4001 {
		t.Fatalf("nack code=%d want=4001", nack.Code)
	}
	if nack.Msg != "invalid visible_to members" {
		t.Fatalf("nack msg=%q want=%q", nack.Msg, "invalid visible_to members")
	}
	if nack.ClientMsgID != "cmsg-invalid-visible-to-members" {
		t.Fatalf("nack client_msg_id=%q want=%q", nack.ClientMsgID, "cmsg-invalid-visible-to-members")
	}
	if len(peer.sent) != 0 {
		t.Fatalf("peer should not receive events, got=%d", len(peer.sent))
	}

	var count int64
	if err := store.DB.Model(&model.Message{}).Where("session_id = ?", sessionID).Count(&count).Error; err != nil {
		t.Fatalf("count messages error: %v", err)
	}
	if count != 0 {
		t.Fatalf("message count=%d want=0", count)
	}
}

func TestHandleSendMsgCreatesSenderInboxAndPushesOtherDevices(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-send-1"
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     1001,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: 1001, MemberType: 1},
		{SessionID: sessionID, MemberID: 1002, MemberType: 1},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	seedSendMsgFriendRelation(t, 1001, 1002)
	seedSendMsgFriendRelation(t, 1002, 1001)

	origin := &sendMsgMockConn{userID: 1001, deviceID: "dev-origin"}
	senderOther := &sendMsgMockConn{userID: 1001, deviceID: "dev-other"}
	recipient := &sendMsgMockConn{userID: 1002, deviceID: "dev-recipient"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			1001: {origin, senderOther},
			1002: {recipient},
		},
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-2",
		MsgType:     1,
		Content:     "from origin",
	})

	HandleSendMsg(hub, origin, pkt)

	if countSentCmd(origin.sent, protocol.CmdSendAck) != 1 ||
		countSentCmd(origin.sent, protocol.CmdPushMsg) != 1 {
		t.Fatalf("origin should receive one send_ack and one push_msg, got=%#v", origin.sent)
	}
	ack, ok := findSendAck(origin.sent)
	if !ok {
		t.Fatalf("origin should include SendAckPayload, got=%#v", origin.sent)
	}
	if ack.MsgID <= 0 {
		t.Fatalf("ack msg_id should be >0, got=%d", ack.MsgID)
	}
	if ack.InboxSeq <= 0 {
		t.Fatalf("ack inbox_seq should be >0, got=%d", ack.InboxSeq)
	}

	if len(senderOther.sent) != 1 {
		t.Fatalf("sender other device should receive one push_msg, got=%d", len(senderOther.sent))
	}
	senderPush, ok := senderOther.sent[0].payload.(protocol.PushMsgPayload)
	if !ok {
		t.Fatalf("sender other payload should be PushMsgPayload, got=%T", senderOther.sent[0].payload)
	}
	if senderPush.InboxSeq != ack.InboxSeq {
		t.Fatalf("sender push inbox_seq mismatch: push=%d ack=%d", senderPush.InboxSeq, ack.InboxSeq)
	}
	if senderPush.MsgID != ack.MsgID {
		t.Fatalf("sender push msg_id mismatch: push=%d ack=%d", senderPush.MsgID, ack.MsgID)
	}
	originPush, ok := findPushMsg(origin.sent)
	if !ok {
		t.Fatalf("origin should include push_msg, got=%#v", origin.sent)
	}
	if originPush.InboxSeq != ack.InboxSeq {
		t.Fatalf("origin push inbox_seq mismatch: push=%d ack=%d", originPush.InboxSeq, ack.InboxSeq)
	}
	if originPush.MsgID != ack.MsgID {
		t.Fatalf("origin push msg_id mismatch: push=%d ack=%d", originPush.MsgID, ack.MsgID)
	}

	if len(recipient.sent) != 1 {
		t.Fatalf("recipient should receive one push_msg, got=%d", len(recipient.sent))
	}
	recipientPush, ok := recipient.sent[0].payload.(protocol.PushMsgPayload)
	if !ok {
		t.Fatalf("recipient payload should be PushMsgPayload, got=%T", recipient.sent[0].payload)
	}
	if recipientPush.MsgID != ack.MsgID {
		t.Fatalf("recipient push msg_id mismatch: push=%d ack=%d", recipientPush.MsgID, ack.MsgID)
	}

	var senderInboxCount int64
	if err := store.DB.Model(&model.UserInbox{}).
		Where("user_id = ? AND msg_id = ? AND session_id = ?", int64(1001), ack.MsgID, sessionID).
		Count(&senderInboxCount).Error; err != nil {
		t.Fatalf("count sender inbox rows error: %v", err)
	}
	if senderInboxCount != 1 {
		t.Fatalf("sender inbox should have exactly one row, got=%d", senderInboxCount)
	}

	var senderMember model.SessionMember
	if err := store.DB.Where("session_id = ? AND member_id = ?", sessionID, int64(1001)).
		First(&senderMember).Error; err != nil {
		t.Fatalf("query sender member error: %v", err)
	}
	if senderMember.UnreadCount != 0 {
		t.Fatalf("sender unread_count should stay 0, got=%d", senderMember.UnreadCount)
	}

	var recipientMember model.SessionMember
	if err := store.DB.Where("session_id = ? AND member_id = ?", sessionID, int64(1002)).
		First(&recipientMember).Error; err != nil {
		t.Fatalf("query recipient member error: %v", err)
	}
	if recipientMember.UnreadCount != 1 {
		t.Fatalf("recipient unread_count should be 1, got=%d", recipientMember.UnreadCount)
	}
}

// TestHandleSendMsgFirstTextSetsTitleSkippingLeadingCard 复现并守护 dispatch_agent
// 会话标题为空的问题：会话第一条物理消息是「绑定目录状态卡片」（非文字），
// 真正的文字任务消息是第二条。标题应从第一条人类文字消息提取，而不是被前面的
// 卡片占位后整体跳过。
func TestHandleSendMsgFirstTextSetsTitleSkippingLeadingCard(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-title-card-first"
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     1001,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: 1001, MemberType: 1},
		{SessionID: sessionID, MemberID: 1002, MemberType: 1},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	seedSendMsgFriendRelation(t, 1001, 1002)
	seedSendMsgFriendRelation(t, 1002, 1001)

	// 第一条：非文字的绑定目录状态卡片（msg_type != 1），模拟 dispatch_agent 建会话时插入。
	if err := store.DB.Create(&model.Message{
		MsgID:      time.Now().UnixNano(),
		SessionID:  sessionID,
		SenderID:   1002,
		SenderType: 2,
		MsgType:    3,
		Content:    "Agent session opened",
		CreatedAt:  time.Now().UTC().Add(-time.Second),
	}).Error; err != nil {
		t.Fatalf("seed leading card message error: %v", err)
	}

	origin := &sendMsgMockConn{userID: 1001, deviceID: "dev-origin"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			1001: {origin},
		},
	}

	taskText := "帮我修复登录页样式"
	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-title-1",
		MsgType:     1,
		Content:     taskText,
	})

	HandleSendMsg(hub, origin, pkt)

	wantTitle := apiservice.BuildFallbackTitleFromMessage(taskText)
	if wantTitle == "" {
		t.Fatalf("expected non-empty fallback title for content %q", taskText)
	}
	var senderMember model.SessionMember
	if err := store.DB.Where("session_id = ? AND member_id = ?", sessionID, int64(1001)).
		First(&senderMember).Error; err != nil {
		t.Fatalf("query sender member error: %v", err)
	}
	if senderMember.CustomTitle != wantTitle {
		t.Fatalf("custom_title should come from first human text message %q, got %q",
			wantTitle, senderMember.CustomTitle)
	}
}

func TestHandleSendMsgFirstTextSetsTitleSkippingBindDirectiveMessage(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-title-bind-first"
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     1001,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: 1001, MemberType: 1},
		{SessionID: sessionID, MemberID: 1002, MemberType: 1},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	seedSendMsgFriendRelation(t, 1001, 1002)
	seedSendMsgFriendRelation(t, 1002, 1001)

	origin := &sendMsgMockConn{userID: 1001, deviceID: "dev-origin"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			1001: {origin},
		},
	}

	// 第一条：目录绑定指令消息（人类发的文字消息），不应参与自动起标题。
	bindContent := "grix://open/session?cwd=%2FVolumes%2Fdisk1%2Fgo%2Fsrc%2Faibot"
	HandleSendMsg(hub, origin, makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-bind-1",
		MsgType:     1,
		Content:     bindContent,
	}))

	var senderMember model.SessionMember
	if err := store.DB.Where("session_id = ? AND member_id = ?", sessionID, int64(1001)).
		First(&senderMember).Error; err != nil {
		t.Fatalf("query sender member error: %v", err)
	}
	if senderMember.CustomTitle != "" {
		t.Fatalf("bind directive message should not set custom_title, got %q", senderMember.CustomTitle)
	}

	// 第二条：真实文字消息，应成为标题来源。
	taskText := "帮我修复登录页样式"
	HandleSendMsg(hub, origin, makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-bind-2",
		MsgType:     1,
		Content:     taskText,
	}))

	wantTitle := apiservice.BuildFallbackTitleFromMessage(taskText)
	if wantTitle == "" {
		t.Fatalf("expected non-empty fallback title for content %q", taskText)
	}
	if err := store.DB.Where("session_id = ? AND member_id = ?", sessionID, int64(1001)).
		First(&senderMember).Error; err != nil {
		t.Fatalf("query sender member error: %v", err)
	}
	if senderMember.CustomTitle != wantTitle {
		t.Fatalf("custom_title should come from first real text message %q, got %q",
			wantTitle, senderMember.CustomTitle)
	}
}

func TestHandleSendMsgSchedulesContentModeration(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-moderation"
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     9001,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: 9001, MemberType: 1},
		{SessionID: sessionID, MemberID: 9002, MemberType: 1},
	}
	if err := store.DB.Create(&members).Error; err != nil {
		t.Fatalf("create session members error: %v", err)
	}
	seedSendMsgFriendRelation(t, 9001, 9002)
	seedSendMsgFriendRelation(t, 9002, 9001)

	origin := &sendMsgMockConn{userID: 9001, deviceID: "dev-origin"}
	recipient := &sendMsgMockConn{userID: 9002, deviceID: "dev-recipient"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			9001: {origin},
			9002: {recipient},
		},
	}

	var scheduled []apiservice.ContentModerationTask
	scheduleContentModeration = func(task apiservice.ContentModerationTask) {
		scheduled = append(scheduled, task)
	}

	HandleSendMsg(hub, origin, makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-moderation",
		MsgType:     1,
		Content:     "please review this",
	}))

	if len(scheduled) != 1 {
		t.Fatalf("scheduled moderation task count=%d want=1", len(scheduled))
	}
	if scheduled[0].SessionID != sessionID {
		t.Fatalf("scheduled session_id=%q want=%q", scheduled[0].SessionID, sessionID)
	}
	ack, ok := findSendAck(origin.sent)
	if !ok {
		t.Fatal("expected send_ack")
	}
	if scheduled[0].MsgID != ack.MsgID {
		t.Fatalf("scheduled msg_id=%d want=%d", scheduled[0].MsgID, ack.MsgID)
	}
}

func TestHandleSendMsgSkipsUnreadWhenRecipientIsViewing(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-send-viewing"
	senderID := int64(1301)
	recipientID := int64(1302)
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     senderID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: senderID, MemberType: 1},
		{SessionID: sessionID, MemberID: recipientID, MemberType: 1},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	seedSendMsgFriendRelation(t, senderID, recipientID)
	seedSendMsgFriendRelation(t, recipientID, senderID)

	origin := &sendMsgMockConn{userID: senderID, deviceID: "dev-origin"}
	recipient := &sendMsgMockConn{userID: recipientID, deviceID: "dev-recipient"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID:    {origin},
			recipientID: {recipient},
		},
	}

	if err := UpsertSessionActivity(context.Background(), hub, protocol.SessionActivityPayload{
		SessionID:    sessionID,
		Kind:         protocol.SessionActivityKindViewing,
		ActorID:      recipientID,
		ActorType:    protocol.SessionActivityActorTypeHuman,
		ExecutorID:   recipientID,
		ExecutorType: protocol.SessionActivityActorTypeHuman,
		Source:       protocol.SessionActivitySourceHumanInput,
	}); err != nil {
		t.Fatalf("upsert viewing activity error: %v", err)
	}
	recipient.sent = nil

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-viewing-1",
		MsgType:     1,
		Content:     "message while viewing",
	})
	HandleSendMsg(hub, origin, pkt)

	var ack protocol.SendAckPayload
	foundAck := false
	for _, item := range origin.sent {
		if item.cmd != protocol.CmdSendAck {
			continue
		}
		payload, ok := item.payload.(protocol.SendAckPayload)
		if !ok {
			t.Fatalf("origin send_ack payload type mismatch: %T", item.payload)
		}
		ack = payload
		foundAck = true
		break
	}
	if !foundAck || ack.MsgID <= 0 {
		t.Fatalf("origin should receive valid send_ack, sent=%#v", origin.sent)
	}
	if len(recipient.sent) != 1 || recipient.sent[0].cmd != protocol.CmdPushMsg {
		t.Fatalf("recipient should receive one push_msg, sent=%#v", recipient.sent)
	}

	var recipientMember model.SessionMember
	if err := store.DB.Where("session_id = ? AND member_id = ?", sessionID, recipientID).
		First(&recipientMember).Error; err != nil {
		t.Fatalf("query recipient member error: %v", err)
	}
	if recipientMember.UnreadCount != 0 {
		t.Fatalf("viewing recipient unread_count should stay 0, got=%d", recipientMember.UnreadCount)
	}
	if recipientMember.LastReadMsgID != ack.MsgID {
		t.Fatalf("viewing recipient last_read_msg_id mismatch: got=%d want=%d", recipientMember.LastReadMsgID, ack.MsgID)
	}

	exists, err := store.RDB.HExists(context.Background(), "im:unread:1302", sessionID).Result()
	if err != nil {
		t.Fatalf("query redis unread hash error: %v", err)
	}
	if exists {
		t.Fatalf("viewing recipient should not have redis unread entry")
	}
}

func TestHandleSendMsgBroadcastsRecipientAcrossNodes(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-send-cross-node"
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     1201,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: 1201, MemberType: 1},
		{SessionID: sessionID, MemberID: 1202, MemberType: 1},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	seedSendMsgFriendRelation(t, 1201, 1202)
	seedSendMsgFriendRelation(t, 1202, 1201)

	origin := &sendMsgMockConn{userID: 1201, deviceID: "dev-origin"}
	recipientLocal := &sendMsgMockConn{userID: 1202, deviceID: "dev-local"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			1201: {origin},
			1202: {recipientLocal},
		},
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:ws:route:1202", "dev-local", "node-a", "dev-remote", "node-b").Err(); err != nil {
		t.Fatalf("set recipient routes error: %v", err)
	}
	if err := store.RDB.Set(ctx, aliveRouteKey(1202, "dev-local"), "1", time.Minute).Err(); err != nil {
		t.Fatalf("set local alive route error: %v", err)
	}
	if err := store.RDB.Set(ctx, aliveRouteKey(1202, "dev-remote"), "1", time.Minute).Err(); err != nil {
		t.Fatalf("set remote alive route error: %v", err)
	}

	pubsub := store.RDB.Subscribe(ctx, "chan:node-b")
	defer pubsub.Close()
	_, _ = pubsub.Receive(ctx)

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-cross-node",
		MsgType:     1,
		Content:     "fan out",
	})

	HandleSendMsg(hub, origin, pkt)

	if len(recipientLocal.sent) != 1 {
		t.Fatalf("local recipient should receive one push_msg, got=%d", len(recipientLocal.sent))
	}

	select {
	case msg := <-pubsub.Channel():
		var envelope struct {
			UserID  int64                   `json:"user_id"`
			Cmd     string                  `json:"cmd"`
			Payload protocol.PushMsgPayload `json:"payload"`
		}
		if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
			t.Fatalf("unmarshal cross-node envelope error: %v", err)
		}
		if envelope.UserID != 1202 {
			t.Fatalf("expected user_id=1202, got=%d", envelope.UserID)
		}
		if envelope.Cmd != protocol.CmdPushMsg {
			t.Fatalf("expected cmd=%s, got=%s", protocol.CmdPushMsg, envelope.Cmd)
		}
		if envelope.Payload.SessionID != sessionID {
			t.Fatalf("expected session_id=%s, got=%s", sessionID, envelope.Payload.SessionID)
		}
	case <-time.After(time.Second):
		t.Fatal("expected cross-node push publish for recipient")
	}
}

func TestHandleSendMsgPrunesStaleRouteAndFallsBackToOfflinePush(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-send-offline-fallback"
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     1301,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: 1301, MemberType: 1},
		{SessionID: sessionID, MemberID: 1302, MemberType: 1},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	seedSendMsgFriendRelation(t, 1301, 1302)
	seedSendMsgFriendRelation(t, 1302, 1301)

	origin := &sendMsgMockConn{userID: 1301, deviceID: "dev-origin"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			1301: {origin},
		},
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:ws:route:1302", "dev-stale", "node-z").Err(); err != nil {
		t.Fatalf("set stale recipient route error: %v", err)
	}

	type offlinePushCall struct {
		userID  int64
		cmd     string
		payload any
	}
	var offlineCalls []offlinePushCall
	originalOfflinePush := enqueueOfflinePushTask
	enqueueOfflinePushTask = func(userID int64, cmd string, payload any) error {
		offlineCalls = append(offlineCalls, offlinePushCall{
			userID:  userID,
			cmd:     cmd,
			payload: payload,
		})
		return nil
	}
	defer func() {
		enqueueOfflinePushTask = originalOfflinePush
	}()

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-offline-fallback",
		MsgType:     1,
		Content:     "fallback",
	})

	HandleSendMsg(hub, origin, pkt)

	if len(offlineCalls) != 1 {
		t.Fatalf("expected exactly one offline push fallback, got=%d", len(offlineCalls))
	}
	if offlineCalls[0].userID != 1302 {
		t.Fatalf("expected offline push for recipient user=1302, got=%d", offlineCalls[0].userID)
	}
	if offlineCalls[0].cmd != protocol.CmdPushMsg {
		t.Fatalf("expected offline push cmd=%s, got=%s", protocol.CmdPushMsg, offlineCalls[0].cmd)
	}
	pushPayload, ok := offlineCalls[0].payload.(protocol.PushMsgPayload)
	if !ok {
		t.Fatalf("expected offline push payload type=%T, got=%T", protocol.PushMsgPayload{}, offlineCalls[0].payload)
	}
	if pushPayload.SessionID != sessionID {
		t.Fatalf("expected offline push session_id=%s, got=%s", sessionID, pushPayload.SessionID)
	}

	if exists, err := store.RDB.HExists(ctx, "im:ws:route:1302", "dev-stale").Result(); err != nil {
		t.Fatalf("check stale route removal error: %v", err)
	} else if exists {
		t.Fatal("stale route should be pruned before offline fallback")
	}
}

func TestDispatchCrossNodeDoesNotFallbackToOfflinePushForNonPushMsg(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	type offlinePushCall struct {
		userID  int64
		cmd     string
		payload any
	}
	var offlineCalls []offlinePushCall
	originalOfflinePush := enqueueOfflinePushTask
	enqueueOfflinePushTask = func(userID int64, cmd string, payload any) error {
		offlineCalls = append(offlineCalls, offlinePushCall{
			userID:  userID,
			cmd:     cmd,
			payload: payload,
		})
		return nil
	}
	defer func() {
		enqueueOfflinePushTask = originalOfflinePush
	}()

	dispatchCrossNode(context.Background(), 1402, protocol.CmdStreamFinish, protocol.StreamFinishPayload{
		MsgID:        123,
		SessionID:    "session-stream-finish",
		FinalContent: "done",
	})

	if len(offlineCalls) != 0 {
		t.Fatalf("non-push cmd should not fallback to offline push, got=%d", len(offlineCalls))
	}
}

func TestHandleSendMsgClearsSenderComposing(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-send-clear-composing"
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     1101,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: 1101, MemberType: 1},
		{SessionID: sessionID, MemberID: 1102, MemberType: 1},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	seedSendMsgFriendRelation(t, 1101, 1102)
	seedSendMsgFriendRelation(t, 1102, 1101)

	origin := &sendMsgMockConn{userID: 1101, deviceID: "dev-origin"}
	senderOther := &sendMsgMockConn{userID: 1101, deviceID: "dev-other"}
	recipient := &sendMsgMockConn{userID: 1102, deviceID: "dev-recipient"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			1101: {origin, senderOther},
			1102: {recipient},
		},
	}

	activity := protocol.SessionActivityPayload{
		SessionID:    sessionID,
		Kind:         protocol.SessionActivityKindComposing,
		ActorID:      1101,
		ActorType:    protocol.SessionActivityActorTypeHuman,
		ExecutorID:   1101,
		ExecutorType: protocol.SessionActivityActorTypeHuman,
		Source:       protocol.SessionActivitySourceHumanInput,
	}
	if err := UpsertSessionActivity(context.Background(), hub, activity); err != nil {
		t.Fatalf("UpsertSessionActivity error: %v", err)
	}
	origin.sent = nil
	senderOther.sent = nil
	recipient.sent = nil

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-clear-1",
		MsgType:     1,
		Content:     "clear composing",
	})
	HandleSendMsg(hub, origin, pkt)

	activities, err := ListSessionActivities(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ListSessionActivities error: %v", err)
	}
	if len(activities) != 0 {
		t.Fatalf("expected sender composing to be cleared, got %d activities", len(activities))
	}

	assertInactiveSync := func(name string, sent []sentPayload) {
		t.Helper()
		for _, item := range sent {
			if item.cmd != protocol.CmdSessionActivitySync {
				continue
			}
			payload, ok := item.payload.(protocol.SessionActivityPayload)
			if !ok {
				t.Fatalf("%s session activity payload type=%T", name, item.payload)
			}
			if payload.Active {
				t.Fatalf("%s composing sync should be inactive, got %+v", name, payload)
			}
			return
		}
		t.Fatalf("%s should receive session_activity_sync clear event, got %#v", name, sent)
	}

	assertInactiveSync("senderOther", senderOther.sent)
	assertInactiveSync("recipient", recipient.sent)
}

func TestHandleSendMsgAgentReplyDoesNotRetriggerSameAgentDelegate(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-agent-self-bounce"
		ownerID   = int64(7401)
		agentID   = int64(9944)
	)

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	ctx := context.Background()
	delegateKey := "im:delegate:" + sessionID + ":7401"
	if err := store.RDB.HSet(ctx, delegateKey,
		"agent_id", "9944",
		"max_consecutive_replies", "3",
	).Err(); err != nil {
		t.Fatalf("seed delegate key error: %v", err)
	}

	agentConn := &sendMsgMockConn{userID: agentID, deviceID: "agent-dev"}
	ownerConn := &sendMsgMockConn{userID: ownerID, deviceID: "owner-dev"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			ownerID: {ownerConn},
			agentID: {agentConn},
		},
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "agent-reply-1",
		MsgType:     1,
		Content:     "agent reply",
		Extra:       json.RawMessage(`{"delegate_origin":true,"agent_api_origin":true,"agent_id":"9944"}`),
	})

	HandleSendMsg(hub, agentConn, pkt)

	if countSentCmd(agentConn.sent, protocol.CmdSendAck) != 1 ||
		countSentCmd(agentConn.sent, protocol.CmdPushMsg) != 1 {
		t.Fatalf("agent sender should receive one send_ack and one push_msg, got=%#v", agentConn.sent)
	}
	if len(ownerConn.sent) != 1 || ownerConn.sent[0].cmd != protocol.CmdPushMsg {
		t.Fatalf("owner should receive exactly one push_msg, got=%#v", ownerConn.sent)
	}
	push, ok := ownerConn.sent[0].payload.(protocol.PushMsgPayload)
	if !ok {
		t.Fatalf("owner payload should be PushMsgPayload, got=%T", ownerConn.sent[0].payload)
	}
	if push.SenderID != agentID {
		t.Fatalf("push sender_id=%d want=%d", push.SenderID, agentID)
	}
	if push.SenderType != 2 {
		t.Fatalf("push sender_type=%d want=2", push.SenderType)
	}

	var msg model.Message
	if err := store.DB.Where("session_id = ? AND msg_type = 1", sessionID).
		Order("msg_id DESC").
		First(&msg).Error; err != nil {
		t.Fatalf("load message error: %v", err)
	}
	if msg.SenderID != agentID {
		t.Fatalf("saved sender_id=%d want=%d", msg.SenderID, agentID)
	}
	if msg.SenderType != 2 {
		t.Fatalf("saved sender_type=%d want=2", msg.SenderType)
	}

	if exists, _ := store.RDB.Exists(ctx, "im:delegate:rate:auto:"+sessionID+":7401").Result(); exists != 0 {
		t.Fatalf("same agent reply should not retrigger delegate self-bounce")
	}
}

func TestHandleSendMsgDelegateAgentDropQueuesEventSilently(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-delegate-agent-drop"
		ownerID   = int64(7601)
		senderID  = int64(7602)
		agentID   = int64(9961)
	)

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(nil)
	defer wsagentapi.SetGlobal(prevManager)

	if err := store.DB.Create(&model.Agent{
		ID:           agentID,
		AgentName:    "delegate-api-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       1,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1},
		{SessionID: sessionID, MemberID: senderID, MemberType: 1},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	seedSendMsgFriendRelation(t, ownerID, senderID)
	seedSendMsgFriendRelation(t, senderID, ownerID)

	ctx := context.Background()
	delegateKey := "im:delegate:" + sessionID + ":7601"
	if err := store.RDB.HSet(ctx, delegateKey,
		"agent_id", "9961",
		"max_consecutive_replies", "3",
	).Err(); err != nil {
		t.Fatalf("seed delegate key error: %v", err)
	}

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "sender-dev"}
	ownerConn := &sendMsgMockConn{userID: ownerID, deviceID: "owner-dev"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
			ownerID:  {ownerConn},
		},
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "delegate-drop-1",
		MsgType:     1,
		Content:     "hello delegated owner",
	})

	HandleSendMsg(hub, senderConn, pkt)

	if countSentCmd(senderConn.sent, protocol.CmdSendAck) != 1 ||
		countSentCmd(senderConn.sent, protocol.CmdPushMsg) != 1 {
		t.Fatalf("sender should receive one send_ack and one push_msg, got=%#v", senderConn.sent)
	}

	if len(ownerConn.sent) != 1 {
		t.Fatalf("owner should receive push_msg only, got=%#v", ownerConn.sent)
	}
	if ownerConn.sent[0].cmd != protocol.CmdPushMsg {
		t.Fatalf("owner first cmd=%s want=%s", ownerConn.sent[0].cmd, protocol.CmdPushMsg)
	}
}

func TestHandleSendMsgDirectAPIAgentDropQueuesEventWithOfflineNotice(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-direct-api-drop"
		ownerID   = int64(7701)
		agentID   = int64(9971)
		quoteID   = int64(18889990781)
	)

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(nil)
	defer wsagentapi.SetGlobal(prevManager)

	if err := store.DB.Create(&model.Agent{
		ID:           agentID,
		AgentName:    "direct-api-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       1,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	if err := store.DB.Create(&model.Message{
		MsgID:      quoteID,
		SessionID:  sessionID,
		SenderID:   agentID,
		SenderType: 2,
		MsgType:    1,
		Content:    "dispatcher anchor",
	}).Error; err != nil {
		t.Fatalf("create quoted message error: %v", err)
	}
	ownerConn := &sendMsgMockConn{userID: ownerID, deviceID: "agent_api_bridge"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			ownerID: {ownerConn},
		},
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:       sessionID,
		ClientMsgID:     "direct-drop-1",
		MsgType:         1,
		Content:         "hello direct api",
		QuotedMessageID: quoteID,
	})

	HandleSendMsg(hub, ownerConn, pkt)

	// agent channel 不可用但入队成功：不报 channel_unavailable(那是真失败才有的 code)。
	// 但既然发送前 agent 就不在线，用户至少要收到一条"已排队等待 agent 上线"的提示，
	// 而不是完全沉默——即 status=queued 的 agent_delivery_status + 一条提示消息。
	if countSentCmd(ownerConn.sent, protocol.CmdSendAck) != 1 ||
		countSentCmd(ownerConn.sent, protocol.CmdPushMsg) != 2 ||
		countSentCmd(ownerConn.sent, protocol.CmdAgentDeliveryStatus) != 1 {
		t.Fatalf(
			"owner should receive send_ack + original push_msg + queued-offline notice, got=%#v",
			ownerConn.sent,
		)
	}
	statusPayload, ok := findAgentDeliveryStatus(ownerConn.sent)
	if !ok {
		t.Fatalf("owner missing agent_delivery_status: %#v", ownerConn.sent)
	}
	if statusPayload.Status != protocol.AgentDeliveryStatusQueued || statusPayload.Code != protocol.AgentDeliveryCodeQueuedOffline {
		t.Fatalf("agent_delivery_status status=%s code=%s want status=%s code=%s", statusPayload.Status, statusPayload.Code, protocol.AgentDeliveryStatusQueued, protocol.AgentDeliveryCodeQueuedOffline)
	}

	ack, ok := findSendAck(ownerConn.sent)
	if !ok {
		t.Fatalf("owner send missing ACK: %#v", ownerConn.sent)
	}
	var saved model.Message
	if err := store.DB.Where("session_id = ? AND msg_id = ?", sessionID, ack.MsgID).First(&saved).Error; err != nil {
		t.Fatalf("load callback message error: %v", err)
	}
	if saved.QuotedMessageID != quoteID {
		t.Fatalf("saved quoted_message_id=%d want=%d", saved.QuotedMessageID, quoteID)
	}

	queuedRaw, err := store.RDB.LRange(context.Background(), "im:agent_api:queued_events:9971", 0, -1).Result()
	if err != nil {
		t.Fatalf("load queued direct event error: %v", err)
	}
	if len(queuedRaw) != 1 {
		t.Fatalf("queued direct events=%d want=1", len(queuedRaw))
	}
	var queuedEvent wsagentapi.DelegateEventPayload
	if err := json.Unmarshal([]byte(queuedRaw[0]), &queuedEvent); err != nil {
		t.Fatalf("unmarshal queued direct event error: %v", err)
	}
	if queuedEvent.QuotedMessageID != quoteID {
		t.Fatalf("queued quoted_message_id=%d want=%d", queuedEvent.QuotedMessageID, quoteID)
	}
}

// TestHandleSendMsgDirectAPIAgentAvailableSkipsOfflineNotice guards against the
// false-positive this feature could introduce: when the agent channel really
// is reachable (forwarded to another node, not queued), the owner must not
// receive a "your agent is offline" notice for a message that was in fact
// delivered.
func TestHandleSendMsgDirectAPIAgentAvailableSkipsOfflineNotice(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-direct-api-available"
		ownerID   = int64(7711)
		agentID   = int64(9972)
	)

	manager := wsagentapi.NewManager("", 30*time.Second, nil, nil, nil, nil)
	manager.SetNodeID("node-origin")
	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(manager)
	defer wsagentapi.SetGlobal(prevManager)

	ctx := context.Background()
	if err := store.RDB.Set(ctx, fmt.Sprintf("im:agent_api:route:%d", agentID), "node-target", time.Minute).Err(); err != nil {
		t.Fatalf("seed agent route error: %v", err)
	}
	pubsub := store.RDB.Subscribe(ctx, "chan:node-target")
	defer pubsub.Close()
	if _, err := pubsub.ReceiveTimeout(ctx, 200*time.Millisecond); err != nil {
		t.Fatalf("subscribe forwarded channel error: %v", err)
	}

	if err := store.DB.Create(&model.Agent{
		ID:           agentID,
		AgentName:    "direct-api-agent-available",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       1,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	ownerConn := &sendMsgMockConn{userID: ownerID, deviceID: "agent_api_bridge"}
	hub := &sendMsgMockHub{
		nodeID: "node-origin",
		conns: map[int64][]ConnInterface{
			ownerID: {ownerConn},
		},
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "direct-available-1",
		MsgType:     1,
		Content:     "hello reachable agent",
	})

	HandleSendMsg(hub, ownerConn, pkt)

	if countSentCmd(ownerConn.sent, protocol.CmdSendAck) != 1 ||
		countSentCmd(ownerConn.sent, protocol.CmdPushMsg) != 1 ||
		countSentCmd(ownerConn.sent, protocol.CmdAgentDeliveryStatus) != 0 {
		t.Fatalf("owner should receive send_ack + original push_msg only, no offline notice, got=%#v", ownerConn.sent)
	}

	queuedLen, err := store.RDB.LLen(ctx, fmt.Sprintf("im:agent_api:queued_events:%d", agentID)).Result()
	if err != nil {
		t.Fatalf("check queued direct events error: %v", err)
	}
	if queuedLen != 0 {
		t.Fatalf("event should have been forwarded, not queued, queued=%d", queuedLen)
	}
}

func TestHandleSendMsgGroupWithoutMentionMirrorsDelegatedGroupMessage(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-group-no-mention-delegate"
		ownerID   = int64(7801)
		senderID  = int64(7802)
		agentID   = int64(9981)
	)

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(nil)
	defer wsagentapi.SetGlobal(prevManager)

	if err := store.DB.Create(&model.Agent{
		ID:           agentID,
		AgentName:    "delegate-api-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       1,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1},
		{SessionID: sessionID, MemberID: senderID, MemberType: 1},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	ctx := context.Background()
	delegateKey := "im:delegate:" + sessionID + ":7801"
	if err := store.RDB.HSet(ctx, delegateKey,
		"agent_id", "9981",
		"max_consecutive_replies", "3",
	).Err(); err != nil {
		t.Fatalf("seed delegate key error: %v", err)
	}

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "sender-dev"}
	ownerConn := &sendMsgMockConn{userID: ownerID, deviceID: "owner-dev"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
			ownerID:  {ownerConn},
		},
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:       sessionID,
		ClientMsgID:     "group-delegate-no-mention-1",
		MsgType:         1,
		Content:         "普通群消息",
		QuotedMessageID: 18889990001,
	})

	HandleSendMsg(hub, senderConn, pkt)

	if countSentCmd(senderConn.sent, protocol.CmdSendAck) != 1 ||
		countSentCmd(senderConn.sent, protocol.CmdPushMsg) != 1 {
		t.Fatalf("sender should receive one send_ack and one push_msg, got=%#v", senderConn.sent)
	}
	if len(ownerConn.sent) != 1 {
		t.Fatalf("owner should receive only push_msg, got=%#v", ownerConn.sent)
	}
	if ownerConn.sent[0].cmd != protocol.CmdPushMsg {
		t.Fatalf("owner first cmd=%s want=%s", ownerConn.sent[0].cmd, protocol.CmdPushMsg)
	}

	queuedRaw, err := store.RDB.LRange(ctx, "im:agent_api:queued_events:9981", 0, -1).Result()
	if err != nil {
		t.Fatalf("load queued delegate events error: %v", err)
	}
	if len(queuedRaw) != 1 {
		t.Fatalf("queued delegate events count=%d want=1 payload=%#v", len(queuedRaw), queuedRaw)
	}

	var queuedEvent wsagentapi.DelegateEventPayload
	if err := json.Unmarshal([]byte(queuedRaw[0]), &queuedEvent); err != nil {
		t.Fatalf("unmarshal queued delegate event error: %v", err)
	}
	if queuedEvent.MirrorMode != wsagentapi.MirrorModeRecordOnly {
		t.Fatalf("queued mirror_mode=%q want=%q", queuedEvent.MirrorMode, wsagentapi.MirrorModeRecordOnly)
	}
	if queuedEvent.EventType != "group_message" {
		t.Fatalf("queued event_type=%s want=group_message", queuedEvent.EventType)
	}
	if queuedEvent.OwnerID != ownerID {
		t.Fatalf("queued owner_id=%d want=%d", queuedEvent.OwnerID, ownerID)
	}
	if queuedEvent.SenderID != senderID {
		t.Fatalf("queued sender_id=%d want=%d", queuedEvent.SenderID, senderID)
	}
	if queuedEvent.QuotedMessageID != 18889990001 {
		t.Fatalf("queued quoted_message_id=%d want=%d", queuedEvent.QuotedMessageID, int64(18889990001))
	}
	if len(queuedEvent.MentionUserIDs) != 0 {
		t.Fatalf("queued mention_user_ids=%v want=[]", queuedEvent.MentionUserIDs)
	}
}

func TestHandleSendMsgGroupWithoutMentionSkipsMentionOnlyDelegatedAgent(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-group-no-mention-mention-only-delegate"
		ownerID   = int64(7803)
		senderID  = int64(7804)
		agentID   = int64(99081)
	)

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(nil)
	defer wsagentapi.SetGlobal(prevManager)

	if err := store.DB.Create(&model.Agent{
		ID:           agentID,
		AgentName:    "delegate-mention-only-api-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       1,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&[]model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, AgentReceiveMode: agentreceive.ModeMentionOnly},
		{SessionID: sessionID, MemberID: senderID, MemberType: 1},
	}).Error; err != nil {
		t.Fatalf("create session members error: %v", err)
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":7803",
		"agent_id", "99081",
		"max_consecutive_replies", "3",
	).Err(); err != nil {
		t.Fatalf("seed delegate key error: %v", err)
	}

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "sender-dev"}
	ownerConn := &sendMsgMockConn{userID: ownerID, deviceID: "owner-dev"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
			ownerID:  {ownerConn},
		},
	}

	HandleSendMsg(hub, senderConn, makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "group-delegate-no-mention-mention-only",
		MsgType:     1,
		Content:     "普通群消息",
	}))

	if countSentCmd(senderConn.sent, protocol.CmdSendAck) != 1 ||
		countSentCmd(senderConn.sent, protocol.CmdPushMsg) != 1 {
		t.Fatalf("sender should receive one send_ack and one push_msg, got=%#v", senderConn.sent)
	}
	if len(ownerConn.sent) != 1 || ownerConn.sent[0].cmd != protocol.CmdPushMsg {
		t.Fatalf("owner should receive only push_msg, got=%#v", ownerConn.sent)
	}

	queuedRaw, err := store.RDB.LRange(ctx, "im:agent_api:queued_events:99081", 0, -1).Result()
	if err != nil {
		t.Fatalf("load queued delegate events error: %v", err)
	}
	if len(queuedRaw) != 0 {
		t.Fatalf("queued delegate events count=%d want=0 payload=%#v", len(queuedRaw), queuedRaw)
	}
}

func TestHandleSendMsgGroupWithMentionTriggersDelegatedAgent(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-group-with-mention-delegate"
		ownerID   = int64(7811)
		senderID  = int64(7812)
		agentID   = int64(9982)
	)

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(nil)
	defer wsagentapi.SetGlobal(prevManager)

	if err := store.DB.Create(&model.Agent{
		ID:           agentID,
		AgentName:    "delegate-api-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       1,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, AgentReceiveMode: agentreceive.ModeMentionOnly},
		{SessionID: sessionID, MemberID: senderID, MemberType: 1},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	ctx := context.Background()
	delegateKey := "im:delegate:" + sessionID + ":7811"
	if err := store.RDB.HSet(ctx, delegateKey,
		"agent_id", "9982",
		"max_consecutive_replies", "3",
	).Err(); err != nil {
		t.Fatalf("seed delegate key error: %v", err)
	}

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "sender-dev"}
	ownerConn := &sendMsgMockConn{userID: ownerID, deviceID: "owner-dev"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
			ownerID:  {ownerConn},
		},
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "group-delegate-with-mention-1",
		MsgType:     1,
		Content:     "请处理 @7811",
		Extra:       json.RawMessage(`{"mention_user_ids":["7811"]}`),
	})

	HandleSendMsg(hub, senderConn, pkt)

	if countSentCmd(senderConn.sent, protocol.CmdSendAck) != 1 ||
		countSentCmd(senderConn.sent, protocol.CmdPushMsg) != 1 {
		t.Fatalf("sender should receive one send_ack and one push_msg, got=%#v", senderConn.sent)
	}
	if len(ownerConn.sent) != 1 {
		t.Fatalf("owner should receive push_msg only, got=%#v", ownerConn.sent)
	}
	if ownerConn.sent[0].cmd != protocol.CmdPushMsg {
		t.Fatalf("owner first cmd=%s want=%s", ownerConn.sent[0].cmd, protocol.CmdPushMsg)
	}

	queuedRaw, err := store.RDB.LRange(ctx, "im:agent_api:queued_events:9982", 0, -1).Result()
	if err != nil {
		t.Fatalf("load queued delegate events error: %v", err)
	}
	if len(queuedRaw) != 1 {
		t.Fatalf("queued delegate events count=%d want=1 payload=%#v", len(queuedRaw), queuedRaw)
	}

	var queuedEvent wsagentapi.DelegateEventPayload
	if err := json.Unmarshal([]byte(queuedRaw[0]), &queuedEvent); err != nil {
		t.Fatalf("unmarshal queued delegate event error: %v", err)
	}
	if queuedEvent.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("queued mirror_mode=%q want=%q", queuedEvent.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
	}
	if queuedEvent.EventType != "group_mention" {
		t.Fatalf("queued event_type=%s want=group_mention", queuedEvent.EventType)
	}
}

func TestHandleSendMsgDelegateEventCarriesStructuredPayload(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-delegate-structured-payload"
		ownerID   = int64(7821)
		senderID  = int64(7822)
		agentID   = int64(9983)
	)

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(nil)
	defer wsagentapi.SetGlobal(prevManager)

	if err := store.DB.Create(&model.Agent{
		ID:           agentID,
		AgentName:    "delegate-structured-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       1,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}

	now := time.Now().UTC()
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: senderID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
	}
	for _, member := range members {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	seedSendMsgFriendRelation(t, senderID, ownerID)
	seedSendMsgFriendRelation(t, ownerID, senderID)

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":7821",
		"agent_id", "9983",
		"max_consecutive_replies", "3",
	).Err(); err != nil {
		t.Fatalf("seed delegate key error: %v", err)
	}

	extra := map[string]any{
		"attachments": []map[string]any{
			{
				"media_url":       "https://cdn.example.com/demo.png",
				"attachment_type": "image",
				"file_name":       "demo.png",
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
	}
	extraRaw, err := json.Marshal(extra)
	if err != nil {
		t.Fatalf("marshal extra error: %v", err)
	}

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "sender-device"}
	ownerConn := &sendMsgMockConn{userID: ownerID, deviceID: "owner-device"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
			ownerID:  {ownerConn},
		},
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:       sessionID,
		ClientMsgID:     "delegate-structured-payload-1",
		MsgType:         2,
		Content:         "请查看附件状态",
		Extra:           extraRaw,
		QuotedMessageID: 18889990091,
	})

	// 发送者发完消息即处于查看态：私聊有人在看 → agent 走流式，event.Extra 不被投递降级改写，
	// 从而隔离本用例对结构化 payload 透传的断言。
	if err := UpsertSessionActivity(ctx, hub, protocol.SessionActivityPayload{
		SessionID:    sessionID,
		Kind:         protocol.SessionActivityKindViewing,
		ActorID:      senderID,
		ActorType:    protocol.SessionActivityActorTypeHuman,
		ExecutorID:   senderID,
		ExecutorType: protocol.SessionActivityActorTypeHuman,
		Source:       protocol.SessionActivitySourceHumanInput,
	}); err != nil {
		t.Fatalf("upsert viewing activity error: %v", err)
	}

	HandleSendMsg(hub, senderConn, pkt)

	queuedRaw, err := store.RDB.LRange(ctx, "im:agent_api:queued_events:9983", 0, -1).Result()
	if err != nil {
		t.Fatalf("load queued delegate events error: %v", err)
	}
	if len(queuedRaw) != 1 {
		t.Fatalf("queued delegate events count=%d want=1 payload=%#v", len(queuedRaw), queuedRaw)
	}

	var queuedEvent wsagentapi.DelegateEventPayload
	if err := json.Unmarshal([]byte(queuedRaw[0]), &queuedEvent); err != nil {
		t.Fatalf("unmarshal queued delegate event error: %v", err)
	}
	if queuedEvent.MsgType != 2 {
		t.Fatalf("queued msg_type=%d want=2", queuedEvent.MsgType)
	}
	// 托管代答私聊（session_type=1）会附加 connector 抑制指令（tool/thinking/text drop），
	// 结构化 payload 其余字段原样透传。期望值 = 输入 extra 叠加该 connector 对象。
	wantExtraMap := map[string]any{}
	if err := json.Unmarshal(extraRaw, &wantExtraMap); err != nil {
		t.Fatalf("unmarshal want extra error: %v", err)
	}
	wantExtraMap["connector"] = map[string]any{
		"tool_events":     "drop",
		"thinking_events": "drop",
		"text_events":     "drop",
	}
	wantExtraRaw, err := json.Marshal(wantExtraMap)
	if err != nil {
		t.Fatalf("marshal want extra error: %v", err)
	}
	if string(queuedEvent.Extra) != string(wantExtraRaw) {
		t.Fatalf("queued extra=%s want=%s", string(queuedEvent.Extra), string(wantExtraRaw))
	}
	if len(queuedEvent.Attachments) != 1 {
		t.Fatalf("queued attachments=%#v want=1 item", queuedEvent.Attachments)
	}
	if queuedEvent.Attachments[0].MediaURL != "https://cdn.example.com/demo.png" {
		t.Fatalf("queued attachment media_url=%s", queuedEvent.Attachments[0].MediaURL)
	}
	var bizCard map[string]any
	if err := json.Unmarshal(queuedEvent.BizCard, &bizCard); err != nil {
		t.Fatalf("unmarshal queued biz_card error: %v", err)
	}
	if bizCard["type"] != "exec_status" {
		t.Fatalf("queued biz_card.type=%v want=exec_status", bizCard["type"])
	}
	var channelData map[string]any
	if err := json.Unmarshal(queuedEvent.ChannelData, &channelData); err != nil {
		t.Fatalf("unmarshal queued channel_data error: %v", err)
	}
	if _, ok := channelData["grix"]; !ok {
		t.Fatalf("queued channel_data=%#v should contain grix", channelData)
	}
}

func TestHandleSendMsgGroupMentionByDottedUsernameTriggersDelegatedAgent(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-group-mention-dotted-username-delegate"
		ownerID   = int64(7821)
		senderID  = int64(7822)
		agentID   = int64(9983)
	)

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(nil)
	defer wsagentapi.SetGlobal(prevManager)

	users := []model.User{
		{ID: ownerID, Username: "owner.user", Email: "owner.user@example.com", Nickname: "老王"},
		{ID: senderID, Username: "sender_user", Email: "sender_user@example.com", Nickname: "小李"},
	}
	for _, u := range users {
		if err := store.DB.Create(&u).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}

	if err := store.DB.Create(&model.Agent{
		ID:           agentID,
		AgentName:    "delegate-api-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       1,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1},
		{SessionID: sessionID, MemberID: senderID, MemberType: 1},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	ctx := context.Background()
	delegateKey := "im:delegate:" + sessionID + ":7821"
	if err := store.RDB.HSet(ctx, delegateKey,
		"agent_id", "9983",
		"max_consecutive_replies", "3",
	).Err(); err != nil {
		t.Fatalf("seed delegate key error: %v", err)
	}

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "sender-dev"}
	ownerConn := &sendMsgMockConn{userID: ownerID, deviceID: "owner-dev"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
			ownerID:  {ownerConn},
		},
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "group-delegate-dotted-username-1",
		MsgType:     1,
		Content:     "请处理 @owner.user",
	})

	HandleSendMsg(hub, senderConn, pkt)

	if countSentCmd(senderConn.sent, protocol.CmdSendAck) != 1 ||
		countSentCmd(senderConn.sent, protocol.CmdPushMsg) != 1 {
		t.Fatalf("sender should receive one send_ack and one push_msg, got=%#v", senderConn.sent)
	}
	if len(ownerConn.sent) != 1 {
		t.Fatalf("owner should receive push_msg only, got=%#v", ownerConn.sent)
	}

	var msg model.Message
	if err := store.DB.Where("session_id = ? AND msg_type = 1", sessionID).
		Order("msg_id DESC").
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
	gotOwner := parseIntStr(t, rawMentions[0].(string))
	if gotOwner != ownerID {
		t.Fatalf("mention_user_ids[0]=%d want=%d", gotOwner, ownerID)
	}
}

func TestHandleSendMsgNormalizesMentionByUsernameAndNickname(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-mention-username"
		ownerID   = int64(8101)
		senderID  = int64(8102)
	)

	users := []model.User{
		{ID: ownerID, Username: "owner_user", Email: "owner_user@example.com", Nickname: "老王"},
		{ID: senderID, Username: "sender_user", Email: "sender_user@example.com", Nickname: "小李"},
	}
	for _, u := range users {
		if err := store.DB.Create(&u).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1},
		{SessionID: sessionID, MemberID: senderID, MemberType: 1},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "dev-origin"}
	recipient := &sendMsgMockConn{userID: ownerID, deviceID: "dev-recipient"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
			ownerID:  {recipient},
		},
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-mention-1",
		MsgType:     1,
		Content:     "@owner_user 已处理，抄送 @老王",
	})

	HandleSendMsg(hub, senderConn, pkt)

	var msg model.Message
	if err := store.DB.Where("session_id = ? AND msg_type = 1", sessionID).
		Order("msg_id DESC").
		First(&msg).Error; err != nil {
		t.Fatalf("load message error: %v", err)
	}

	var extra map[string]any
	if err := json.Unmarshal(msg.Extra, &extra); err != nil {
		t.Fatalf("unmarshal message extra error: %v", err)
	}
	rawMentions, ok := extra["mention_user_ids"].([]any)
	if !ok {
		t.Fatalf("mention_user_ids missing or invalid in extra: %#v", extra["mention_user_ids"])
	}
	if len(rawMentions) != 1 {
		t.Fatalf("mention_user_ids should dedupe to one owner id, got=%#v", rawMentions)
	}
	gotOwner := parseIntStr(t, rawMentions[0].(string))
	if gotOwner != ownerID {
		t.Fatalf("mention_user_ids[0]=%d, want=%d", gotOwner, ownerID)
	}
}

func TestHandleSendMsgMentionAllExpandsToAllOtherHumanMembers(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-mention-all-humans"
		ownerID   = int64(8111)
		senderID  = int64(8112)
		peerID    = int64(8113)
	)

	users := []model.User{
		{ID: ownerID, Username: "owner_user", Email: "owner_user@example.com", Nickname: "老王"},
		{ID: senderID, Username: "sender_user", Email: "sender_user@example.com", Nickname: "小李"},
		{ID: peerID, Username: "peer_user", Email: "peer_user@example.com", Nickname: "小张"},
	}
	for _, u := range users {
		if err := store.DB.Create(&u).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1},
		{SessionID: sessionID, MemberID: senderID, MemberType: 1},
		{SessionID: sessionID, MemberID: peerID, MemberType: 1},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "dev-origin"}
	ownerConn := &sendMsgMockConn{userID: ownerID, deviceID: "dev-owner"}
	peerConn := &sendMsgMockConn{userID: peerID, deviceID: "dev-peer"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
			ownerID:  {ownerConn},
			peerID:   {peerConn},
		},
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-mention-all-1",
		MsgType:     1,
		Content:     "@所有人 请看一下",
	})

	HandleSendMsg(hub, senderConn, pkt)

	if countSentCmd(senderConn.sent, protocol.CmdSendAck) != 1 ||
		countSentCmd(senderConn.sent, protocol.CmdPushMsg) != 1 {
		t.Fatalf("sender should receive one send_ack and one push_msg, got=%#v", senderConn.sent)
	}
	if len(ownerConn.sent) != 1 || ownerConn.sent[0].cmd != protocol.CmdPushMsg {
		t.Fatalf("owner should receive only push_msg, got=%#v", ownerConn.sent)
	}
	if len(peerConn.sent) != 1 || peerConn.sent[0].cmd != protocol.CmdPushMsg {
		t.Fatalf("peer should receive only push_msg, got=%#v", peerConn.sent)
	}

	var msg model.Message
	if err := store.DB.Where("session_id = ? AND msg_type = 1", sessionID).
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
	gotMentionIDs := []int64{
		parseIntStr(t, rawMentions[0].(string)),
		parseIntStr(t, rawMentions[1].(string)),
	}
	if containsInt64(gotMentionIDs, senderID) {
		t.Fatalf("mention_user_ids should not contain sender, got=%v", gotMentionIDs)
	}
	if !containsInt64(gotMentionIDs, ownerID) || !containsInt64(gotMentionIDs, peerID) {
		t.Fatalf("mention_user_ids=%v should contain owner=%d and peer=%d", gotMentionIDs, ownerID, peerID)
	}
}

func TestHandleSendMsgQuotedMessageOwnerBecomesMention(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID       = "session-mention-quoted-owner"
		ownerID         = int64(8121)
		senderID        = int64(8122)
		quotedMessageID = int64(18889990441)
	)

	now := time.Now().UTC()
	users := []model.User{
		{ID: ownerID, Username: "owner_user", Email: "owner_user@example.com", Nickname: "老王"},
		{ID: senderID, Username: "sender_user", Email: "sender_user@example.com", Nickname: "小李"},
	}
	for _, u := range users {
		if err := store.DB.Create(&u).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, m := range []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: senderID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
	} {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	if err := store.DB.Create(&model.Message{
		MsgID:      quotedMessageID,
		SessionID:  sessionID,
		SenderID:   ownerID,
		SenderType: 1,
		MsgType:    1,
		Content:    "原始消息",
		CreatedAt:  now.Add(-time.Second),
	}).Error; err != nil {
		t.Fatalf("create quoted message error: %v", err)
	}

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "dev-origin"}
	recipient := &sendMsgMockConn{userID: ownerID, deviceID: "dev-recipient"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
			ownerID:  {recipient},
		},
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:       sessionID,
		ClientMsgID:     "cmsg-mention-quoted-owner",
		MsgType:         1,
		Content:         "我接着说一下",
		QuotedMessageID: quotedMessageID,
	})

	HandleSendMsg(hub, senderConn, pkt)

	ack, ok := findSendAck(senderConn.sent)
	if !ok {
		t.Fatalf("sender should include SendAckPayload, got=%#v", senderConn.sent)
	}

	var msg model.Message
	if err := store.DB.Where("session_id = ? AND msg_id = ?", sessionID, ack.MsgID).
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
	gotOwner := parseIntStr(t, rawMentions[0].(string))
	if gotOwner != ownerID {
		t.Fatalf("mention_user_ids[0]=%d want=%d", gotOwner, ownerID)
	}
}

func TestHandleSendMsgQuotedMessageOwnerSkippedWhenExplicitMentionExists(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID       = "session-mention-quoted-owner-explicit-target"
		ownerID         = int64(8125)
		senderID        = int64(8126)
		targetID        = int64(8127)
		quotedMessageID = int64(18889990442)
	)

	now := time.Now().UTC()
	users := []model.User{
		{ID: ownerID, Username: "owner_user", Email: "owner_user@example.com", Nickname: "老王"},
		{ID: senderID, Username: "sender_user", Email: "sender_user@example.com", Nickname: "小李"},
		{ID: targetID, Username: "target_user", Email: "target_user@example.com", Nickname: "小张"},
	}
	for _, u := range users {
		if err := store.DB.Create(&u).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, m := range []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: senderID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: targetID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
	} {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	if err := store.DB.Create(&model.Message{
		MsgID:      quotedMessageID,
		SessionID:  sessionID,
		SenderID:   ownerID,
		SenderType: 1,
		MsgType:    1,
		Content:    "原始消息",
		CreatedAt:  now.Add(-time.Second),
	}).Error; err != nil {
		t.Fatalf("create quoted message error: %v", err)
	}

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "dev-origin"}
	ownerConn := &sendMsgMockConn{userID: ownerID, deviceID: "dev-owner"}
	targetConn := &sendMsgMockConn{userID: targetID, deviceID: "dev-target"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
			ownerID:  {ownerConn},
			targetID: {targetConn},
		},
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:       sessionID,
		ClientMsgID:     "cmsg-mention-quoted-owner-explicit-target",
		MsgType:         1,
		Content:         "@target_user 我接着说一下",
		QuotedMessageID: quotedMessageID,
	})

	HandleSendMsg(hub, senderConn, pkt)

	ack, ok := findSendAck(senderConn.sent)
	if !ok {
		t.Fatalf("sender should include SendAckPayload, got=%#v", senderConn.sent)
	}

	var msg model.Message
	if err := store.DB.Where("session_id = ? AND msg_id = ?", sessionID, ack.MsgID).
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
	gotTarget := parseIntStr(t, rawMentions[0].(string))
	if gotTarget != targetID {
		t.Fatalf("mention_user_ids[0]=%d want=%d", gotTarget, targetID)
	}
}

func TestHandleSendMsgGroupMentionTargetsAPIAgentByAgentName(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID    = "session-mention-api-agent"
		senderID     = int64(8301)
		peerID       = int64(8302)
		localAgentID = int64(8303)
		apiAgentID   = int64(8304)
	)

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(nil)
	defer wsagentapi.SetGlobal(prevManager)

	users := []model.User{
		{ID: senderID, Username: "sender_user", Email: "sender_user@example.com", Nickname: "发起人"},
		{ID: peerID, Username: "peer_user", Email: "peer_user@example.com", Nickname: "旁观者"},
	}
	for _, u := range users {
		if err := store.DB.Create(&u).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     senderID,
		SessionType: 2,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}

	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: senderID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: peerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: localAgentID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: apiAgentID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	agents := []model.Agent{
		{
			ID:             localAgentID,
			OwnerID:        senderID,
			AgentName:      "本地小龙虾",
			ProviderType:   model.AgentProviderLocal,
			LocalEndpoint:  "http://127.0.0.1:11434",
			LocalModelName: "gemma3",
			Status:         1,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		{
			ID:           apiAgentID,
			OwnerID:      senderID,
			AgentName:    "OpenClaw",
			ProviderType: model.AgentProviderAPI,
			Status:       1,
			CreatedAt:    now,
			UpdatedAt:    now,
		},
	}
	for _, agent := range agents {
		if err := store.DB.Create(&agent).Error; err != nil {
			t.Fatalf("create agent error: %v", err)
		}
	}

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "dev-origin"}
	peerConn := &sendMsgMockConn{userID: peerID, deviceID: "dev-peer"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
			peerID:   {peerConn},
		},
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-mention-api-agent",
		MsgType:     1,
		Content:     "@OpenClaw 你是谁",
	})

	HandleSendMsg(hub, senderConn, pkt)

	// agent channel 不可用但入队成功：不报 channel_unavailable(那是真失败才有的 code)，
	// 但也不再完全沉默——多一条 status=queued 的 agent_delivery_status 和一条排队提示消息。
	if countSentCmd(senderConn.sent, protocol.CmdSendAck) != 1 ||
		countSentCmd(senderConn.sent, protocol.CmdPushMsg) != 2 ||
		countSentCmd(senderConn.sent, protocol.CmdAgentDeliveryStatus) != 1 {
		t.Fatalf("sender should receive send_ack + original push_msg + queued-offline notice, got=%#v", senderConn.sent)
	}
	ack, ok := findSendAck(senderConn.sent)
	if !ok {
		t.Fatalf("sender should include SendAckPayload, got=%#v", senderConn.sent)
	}
	if ack.LocalInference != nil {
		t.Fatalf("explicit api mention should not route local inference, got=%#v", ack.LocalInference)
	}

	var msg model.Message
	if err := store.DB.Where("session_id = ? AND msg_type = 1 AND sender_id = ?", sessionID, senderID).
		Order("msg_id DESC").
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
	gotMention := parseIntStr(t, rawMentions[0].(string))
	if gotMention != apiAgentID {
		t.Fatalf("mention_user_ids[0]=%d want=%d", gotMention, apiAgentID)
	}
}

func TestHandleSendMsgGroupMentionTargetsLocalAgentByAgentName(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID    = "session-mention-local-agent"
		senderID     = int64(8401)
		peerID       = int64(8402)
		apiAgentID   = int64(8403)
		localAgentID = int64(8404)
	)

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(nil)
	defer wsagentapi.SetGlobal(prevManager)

	users := []model.User{
		{ID: senderID, Username: "sender_user", Email: "sender_user@example.com", Nickname: "发起人"},
		{ID: peerID, Username: "peer_user", Email: "peer_user@example.com", Nickname: "旁观者"},
	}
	for _, u := range users {
		if err := store.DB.Create(&u).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     senderID,
		SessionType: 2,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}

	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: senderID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: peerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: apiAgentID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: localAgentID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	agents := []model.Agent{
		{
			ID:           apiAgentID,
			OwnerID:      senderID,
			AgentName:    "OpenClaw",
			ProviderType: model.AgentProviderAPI,
			Status:       1,
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		{
			ID:             localAgentID,
			OwnerID:        senderID,
			AgentName:      "本地小龙虾",
			ProviderType:   model.AgentProviderLocal,
			LocalEndpoint:  "http://127.0.0.1:11434",
			LocalModelName: "gemma3",
			SystemPrompt:   "你是本地测试 agent",
			Status:         1,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}
	for _, agent := range agents {
		if err := store.DB.Create(&agent).Error; err != nil {
			t.Fatalf("create agent error: %v", err)
		}
	}

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "dev-origin"}
	peerConn := &sendMsgMockConn{userID: peerID, deviceID: "dev-peer"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
			peerID:   {peerConn},
		},
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-mention-local-agent",
		MsgType:     1,
		Content:     "@本地小龙虾 你是谁",
	})

	HandleSendMsg(hub, senderConn, pkt)

	if countSentCmd(senderConn.sent, protocol.CmdSendAck) != 1 ||
		countSentCmd(senderConn.sent, protocol.CmdPushMsg) != 1 {
		t.Fatalf("sender should receive one send_ack and one push_msg, got=%#v", senderConn.sent)
	}
	ack, ok := findSendAck(senderConn.sent)
	if !ok {
		t.Fatalf("sender should include SendAckPayload, got=%#v", senderConn.sent)
	}
	if ack.LocalInference == nil {
		t.Fatalf("local agent mention should trigger local inference")
	}
	if ack.LocalInference.AgentID != localAgentID {
		t.Fatalf("local_inference.agent_id=%d want=%d", ack.LocalInference.AgentID, localAgentID)
	}
	if ack.LocalInference.Endpoint != "http://127.0.0.1:11434" {
		t.Fatalf("local_inference.endpoint=%q", ack.LocalInference.Endpoint)
	}

	var msg model.Message
	if err := store.DB.Where("session_id = ? AND msg_type = 1", sessionID).
		Order("msg_id DESC").
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
	gotMention := parseIntStr(t, rawMentions[0].(string))
	if gotMention != localAgentID {
		t.Fatalf("mention_user_ids[0]=%d want=%d", gotMention, localAgentID)
	}
}

func TestHandleSendMsgGroupWithoutMentionTriggersAPIAgent(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID  = "session-group-no-mention-api-agent"
		senderID   = int64(8451)
		peerID     = int64(8452)
		apiAgentID = int64(8453)
	)

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(nil)
	defer wsagentapi.SetGlobal(prevManager)

	users := []model.User{
		{ID: senderID, Username: "sender_user", Email: "sender_user@example.com", Nickname: "发起人"},
		{ID: peerID, Username: "peer_user", Email: "peer_user@example.com", Nickname: "旁观者"},
	}
	for _, u := range users {
		if err := store.DB.Create(&u).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     senderID,
		SessionType: 2,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}

	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: senderID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: peerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: apiAgentID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	if err := store.DB.Create(&model.Agent{
		ID:           apiAgentID,
		OwnerID:      senderID,
		AgentName:    "OpenClaw",
		ProviderType: model.AgentProviderAPI,
		Status:       1,
		CreatedAt:    now,
		UpdatedAt:    now,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "dev-origin"}
	peerConn := &sendMsgMockConn{userID: peerID, deviceID: "dev-peer"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
			peerID:   {peerConn},
		},
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-no-mention-api-agent",
		MsgType:     1,
		Content:     "大家随便聊聊",
	})

	HandleSendMsg(hub, senderConn, pkt)

	// 未上线的 API agent 仍被派发一次，因此除了原始 push_msg 还会收到一条排队提示。
	if countSentCmd(senderConn.sent, protocol.CmdSendAck) != 1 ||
		countSentCmd(senderConn.sent, protocol.CmdPushMsg) != 2 ||
		countSentCmd(senderConn.sent, protocol.CmdAgentDeliveryStatus) != 1 {
		t.Fatalf("sender should receive send_ack + original push_msg + queued-offline notice, got=%#v", senderConn.sent)
	}
	ack, ok := findSendAck(senderConn.sent)
	if !ok {
		t.Fatalf("sender should include SendAckPayload, got=%#v", senderConn.sent)
	}
	if ack.LocalInference != nil {
		t.Fatalf("api-only group route should not include local inference, got=%#v", ack.LocalInference)
	}
}

func TestHandleSendMsgGroupWithoutMentionSkipsLocalAgent(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID    = "session-group-no-mention-local-agent"
		senderID     = int64(8461)
		peerID       = int64(8462)
		localAgentID = int64(8463)
	)

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(nil)
	defer wsagentapi.SetGlobal(prevManager)

	users := []model.User{
		{ID: senderID, Username: "sender_user", Email: "sender_user@example.com", Nickname: "发起人"},
		{ID: peerID, Username: "peer_user", Email: "peer_user@example.com", Nickname: "旁观者"},
	}
	for _, u := range users {
		if err := store.DB.Create(&u).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     senderID,
		SessionType: 2,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}

	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: senderID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: peerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: localAgentID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	if err := store.DB.Create(&model.Agent{
		ID:             localAgentID,
		OwnerID:        senderID,
		AgentName:      "本地小龙虾",
		ProviderType:   model.AgentProviderLocal,
		LocalEndpoint:  "http://127.0.0.1:11434",
		LocalModelName: "gemma3",
		SystemPrompt:   "你是本地测试 agent",
		Status:         1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "dev-origin"}
	peerConn := &sendMsgMockConn{userID: peerID, deviceID: "dev-peer"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
			peerID:   {peerConn},
		},
	}
	seedSendMsgIdleGroupHistory(t, sessionID, senderID, time.Minute)

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-no-mention-local-agent",
		MsgType:     1,
		Content:     "大家随便聊聊",
	})

	HandleSendMsg(hub, senderConn, pkt)

	if countSentCmd(senderConn.sent, protocol.CmdSendAck) != 1 ||
		countSentCmd(senderConn.sent, protocol.CmdPushMsg) != 1 {
		t.Fatalf("sender should receive one send_ack and one push_msg, got=%#v", senderConn.sent)
	}
	ack, ok := findSendAck(senderConn.sent)
	if !ok {
		t.Fatalf("sender should include SendAckPayload, got=%#v", senderConn.sent)
	}
	if ack.LocalInference != nil {
		t.Fatalf("group message without target should not trigger local inference, got=%#v", ack.LocalInference)
	}
}

func TestHandleSendMsgGroupMentionWithContextTriggersLocalAgent(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID    = "session-group-mention-context-local-agent"
		senderID     = int64(8467)
		peerID       = int64(8468)
		localAgentID = int64(8469)
	)

	users := []model.User{
		{ID: senderID, Username: "sender_user", Email: "sender_user@example.com", Nickname: "发起人"},
		{ID: peerID, Username: "peer_user", Email: "peer_user@example.com", Nickname: "旁观者"},
	}
	for _, u := range users {
		if err := store.DB.Create(&u).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     senderID,
		SessionType: 2,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}

	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: senderID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: peerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{
			SessionID:                sessionID,
			MemberID:                 localAgentID,
			MemberType:               2,
			AgentReceiveMode:         agentreceive.ModeNormal,
			AgentReceiveBacklogCount: 2,
			JoinedAt:                 now,
			LastActiveAt:             now,
		},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	if err := store.DB.Create(&model.Agent{
		ID:             localAgentID,
		OwnerID:        senderID,
		AgentName:      "本地小龙虾",
		ProviderType:   model.AgentProviderLocal,
		LocalEndpoint:  "http://127.0.0.1:11434",
		LocalModelName: "gemma3",
		SystemPrompt:   "你是本地测试 agent",
		Status:         1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "dev-origin"}
	peerConn := &sendMsgMockConn{userID: peerID, deviceID: "dev-peer"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
			peerID:   {peerConn},
		},
	}
	seedSendMsgIdleGroupHistory(t, sessionID, senderID, time.Minute)

	HandleSendMsg(hub, senderConn, makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-context-buffer-local-agent",
		MsgType:     1,
		Content:     "先别处理这句",
	}))

	firstAck, ok := findSendAck(senderConn.sent)
	if !ok {
		t.Fatalf("first send should include send_ack")
	}
	if firstAck.LocalInference != nil {
		t.Fatalf("unmentioned mode=2 message should not trigger local inference")
	}

	senderConn.sent = nil
	peerConn.sent = nil

	HandleSendMsg(hub, senderConn, makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-context-mention-local-agent",
		MsgType:     1,
		Content:     "@本地小龙虾 现在处理",
	}))

	secondAck, ok := findSendAck(senderConn.sent)
	if !ok {
		t.Fatalf("second send should include send_ack")
	}
	if secondAck.LocalInference == nil {
		t.Fatalf("mentioned mode=2 message should trigger local inference")
	}
	if len(secondAck.LocalInference.ContextMessages) != 2 {
		t.Fatalf("context_messages=%#v want=2 items", secondAck.LocalInference.ContextMessages)
	}
	if secondAck.LocalInference.ContextMessages[0].Content != "先别处理这句" {
		t.Fatalf("first context content=%q", secondAck.LocalInference.ContextMessages[0].Content)
	}
	if secondAck.LocalInference.ContextMessages[1].Content != "@本地小龙虾 现在处理" {
		t.Fatalf("second context content=%q", secondAck.LocalInference.ContextMessages[1].Content)
	}
}

func TestHandleSendMsgGroupMentionOnlySkipsUnmentionedLocalAgent(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID    = "session-group-mention-only-local-agent"
		senderID     = int64(8471)
		peerID       = int64(8472)
		localAgentID = int64(8473)
	)

	users := []model.User{
		{ID: senderID, Username: "sender_user", Email: "sender_user@example.com", Nickname: "发起人"},
		{ID: peerID, Username: "peer_user", Email: "peer_user@example.com", Nickname: "旁观者"},
	}
	for _, u := range users {
		if err := store.DB.Create(&u).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     senderID,
		SessionType: 2,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}

	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: senderID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: peerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{
			SessionID:                sessionID,
			MemberID:                 localAgentID,
			MemberType:               2,
			AgentReceiveMode:         3,
			AgentReceiveBacklogCount: 2,
			JoinedAt:                 now,
			LastActiveAt:             now,
		},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	if err := store.DB.Create(&model.Agent{
		ID:             localAgentID,
		OwnerID:        senderID,
		AgentName:      "本地小龙虾",
		ProviderType:   model.AgentProviderLocal,
		LocalEndpoint:  "http://127.0.0.1:11434",
		LocalModelName: "gemma3",
		Status:         1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "dev-origin"}
	peerConn := &sendMsgMockConn{userID: peerID, deviceID: "dev-peer"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
			peerID:   {peerConn},
		},
	}

	HandleSendMsg(hub, senderConn, makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-mention-only-local-agent",
		MsgType:     1,
		Content:     "这句不带 @",
	}))

	ack, ok := findSendAck(senderConn.sent)
	if !ok {
		t.Fatalf("send should include send_ack")
	}
	if ack.LocalInference != nil {
		t.Fatalf("mode=3 should skip unmentioned local inference")
	}
}

func TestHandleSendMsgDuplicateReplaysPendingLocalInferenceHint(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID    = "session-group-duplicate-local-hint"
		senderID     = int64(8474)
		peerID       = int64(8475)
		localAgentID = int64(8476)
	)

	users := []model.User{
		{ID: senderID, Username: "sender_user_8474", Email: "sender_user_8474@example.com", Nickname: "发起人"},
		{ID: peerID, Username: "peer_user_8475", Email: "peer_user_8475@example.com", Nickname: "旁观者"},
	}
	for _, u := range users {
		if err := store.DB.Create(&u).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     senderID,
		SessionType: 2,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}

	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: senderID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: peerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{
			SessionID:                sessionID,
			MemberID:                 localAgentID,
			MemberType:               2,
			AgentReceiveMode:         agentreceive.ModeNormal,
			AgentReceiveBacklogCount: 3,
			JoinedAt:                 now,
			LastActiveAt:             now,
		},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	if err := store.DB.Create(&model.Agent{
		ID:             localAgentID,
		OwnerID:        senderID,
		AgentName:      "本地小龙虾",
		ProviderType:   model.AgentProviderLocal,
		LocalEndpoint:  "http://127.0.0.1:11434",
		LocalModelName: "gemma3",
		SystemPrompt:   "你是本地测试 agent",
		Status:         1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "dev-origin"}
	peerConn := &sendMsgMockConn{userID: peerID, deviceID: "dev-peer"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
			peerID:   {peerConn},
		},
	}

	packet := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-local-dedup-hint",
		MsgType:     1,
		Content:     "@本地小龙虾 重新补发本地指令",
	})

	HandleSendMsg(hub, senderConn, packet)

	firstAck, ok := findSendAck(senderConn.sent)
	if !ok || firstAck.LocalInference == nil {
		t.Fatalf("first send should include local inference, ack=%#v", firstAck)
	}

	senderConn.sent = nil
	peerConn.sent = nil

	HandleSendMsg(hub, senderConn, packet)

	secondAck, ok := findSendAck(senderConn.sent)
	if !ok || secondAck.LocalInference == nil {
		t.Fatalf("duplicate send should replay local inference, ack=%#v", secondAck)
	}
	if secondAck.MsgID != firstAck.MsgID {
		t.Fatalf("duplicate send ack msg_id=%d want=%d", secondAck.MsgID, firstAck.MsgID)
	}
	if secondAck.LocalInference.TriggerMsgID != firstAck.LocalInference.TriggerMsgID {
		t.Fatalf(
			"duplicate local trigger_msg_id=%d want=%d",
			secondAck.LocalInference.TriggerMsgID,
			firstAck.LocalInference.TriggerMsgID,
		)
	}
	if len(secondAck.LocalInference.ContextMessages) != len(firstAck.LocalInference.ContextMessages) {
		t.Fatalf(
			"duplicate context count=%d want=%d",
			len(secondAck.LocalInference.ContextMessages),
			len(firstAck.LocalInference.ContextMessages),
		)
	}
}

func TestHandleSendMsgGroupMentionWithContextClearsConsumedBacklogAfterLocalAccept(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID    = "session-group-mention-context-local-agent-retain"
		senderID     = int64(8481)
		peerID       = int64(8482)
		localAgentID = int64(8483)
	)

	users := []model.User{
		{ID: senderID, Username: "sender_user_8481", Email: "sender_user_8481@example.com", Nickname: "发起人"},
		{ID: peerID, Username: "peer_user_8482", Email: "peer_user_8482@example.com", Nickname: "旁观者"},
	}
	for _, u := range users {
		if err := store.DB.Create(&u).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     senderID,
		SessionType: 2,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}

	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: senderID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: peerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{
			SessionID:                sessionID,
			MemberID:                 localAgentID,
			MemberType:               2,
			AgentReceiveMode:         agentreceive.ModeNormal,
			AgentReceiveBacklogCount: 3,
			JoinedAt:                 now,
			LastActiveAt:             now,
		},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	if err := store.DB.Create(&model.Agent{
		ID:             localAgentID,
		OwnerID:        senderID,
		AgentName:      "本地小龙虾",
		ProviderType:   model.AgentProviderLocal,
		LocalEndpoint:  "http://127.0.0.1:11434",
		LocalModelName: "gemma3",
		SystemPrompt:   "你是本地测试 agent",
		Status:         1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "dev-origin"}
	peerConn := &sendMsgMockConn{userID: peerID, deviceID: "dev-peer"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
			peerID:   {peerConn},
		},
	}
	seedSendMsgIdleGroupHistory(t, sessionID, senderID, time.Minute)

	HandleSendMsg(hub, senderConn, makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-local-retain-1",
		MsgType:     1,
		Content:     "第一句先排队",
	}))

	firstAck, ok := findSendAck(senderConn.sent)
	if !ok {
		t.Fatalf("first send should include send_ack")
	}
	if firstAck.LocalInference != nil {
		t.Fatalf("unmentioned mode=2 message should not trigger local inference")
	}

	senderConn.sent = nil
	peerConn.sent = nil

	HandleSendMsg(hub, senderConn, makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-local-retain-2",
		MsgType:     1,
		Content:     "@本地小龙虾 第二句先别清",
	}))

	secondAck, ok := findSendAck(senderConn.sent)
	if !ok || secondAck.LocalInference == nil {
		t.Fatalf("second send should include local inference, ack=%#v", secondAck)
	}
	if len(secondAck.LocalInference.ContextMessages) != 2 {
		t.Fatalf("second context_messages=%#v want=2 items", secondAck.LocalInference.ContextMessages)
	}

	senderConn.sent = nil
	peerConn.sent = nil

	HandleSendMsg(hub, senderConn, makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-local-retain-3",
		MsgType:     1,
		Content:     "@本地小龙虾 第三句再次触发",
	}))

	thirdAck, ok := findSendAck(senderConn.sent)
	if !ok || thirdAck.LocalInference == nil {
		t.Fatalf("third send should include local inference, ack=%#v", thirdAck)
	}
	if len(thirdAck.LocalInference.ContextMessages) != 1 {
		t.Fatalf("third context_messages=%#v want=1 item", thirdAck.LocalInference.ContextMessages)
	}
	if thirdAck.LocalInference.ContextMessages[0].Content != "@本地小龙虾 第三句再次触发" {
		t.Fatalf("third retained context content=%q", thirdAck.LocalInference.ContextMessages[0].Content)
	}
}

func TestHandleRelayLocalStreamStartClearsBufferedMentionContext(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID    = "session-group-mention-context-local-agent-clear"
		senderID     = int64(8491)
		peerID       = int64(8492)
		localAgentID = int64(8493)
	)

	users := []model.User{
		{ID: senderID, Username: "sender_user_8491", Email: "sender_user_8491@example.com", Nickname: "发起人"},
		{ID: peerID, Username: "peer_user_8492", Email: "peer_user_8492@example.com", Nickname: "旁观者"},
	}
	for _, u := range users {
		if err := store.DB.Create(&u).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     senderID,
		SessionType: 2,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}

	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: senderID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: peerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{
			SessionID:                sessionID,
			MemberID:                 localAgentID,
			MemberType:               2,
			AgentReceiveMode:         agentreceive.ModeNormal,
			AgentReceiveBacklogCount: 3,
			JoinedAt:                 now,
			LastActiveAt:             now,
		},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	if err := store.DB.Create(&model.Agent{
		ID:             localAgentID,
		OwnerID:        senderID,
		AgentName:      "本地小龙虾",
		ProviderType:   model.AgentProviderLocal,
		LocalEndpoint:  "http://127.0.0.1:11434",
		LocalModelName: "gemma3",
		SystemPrompt:   "你是本地测试 agent",
		Status:         1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "dev-origin"}
	peerConn := &sendMsgMockConn{userID: peerID, deviceID: "dev-peer"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
			peerID:   {peerConn},
		},
	}
	seedSendMsgIdleGroupHistory(t, sessionID, senderID, time.Minute)

	HandleSendMsg(hub, senderConn, makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-local-clear-1",
		MsgType:     1,
		Content:     "启动前先排队",
	}))

	senderConn.sent = nil
	peerConn.sent = nil

	HandleSendMsg(hub, senderConn, makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-local-clear-2",
		MsgType:     1,
		Content:     "@本地小龙虾 现在正式处理",
	}))

	secondAck, ok := findSendAck(senderConn.sent)
	if !ok || secondAck.LocalInference == nil {
		t.Fatalf("second send should include local inference, ack=%#v", secondAck)
	}
	if len(secondAck.LocalInference.ContextMessages) != 2 {
		t.Fatalf("second context_messages=%#v want=2 items", secondAck.LocalInference.ContextMessages)
	}

	senderConn.sent = nil
	peerConn.sent = nil

	HandleRelayLocalStreamStart(
		hub,
		senderConn,
		makeRelayLocalStreamPacket(t, protocol.CmdRelayLocalStreamStart, protocol.RelayLocalStreamStartPayload{
			SessionID:    sessionID,
			AgentID:      localAgentID,
			TriggerMsgID: secondAck.LocalInference.TriggerMsgID,
		}),
	)

	startAck, ok := findRelayLocalStreamStartAck(senderConn.sent)
	if !ok {
		t.Fatalf("relay_local_stream_start should include start ack")
	}
	if startAck.Code != 200 || startAck.MsgID <= 0 {
		t.Fatalf("unexpected start ack=%#v", startAck)
	}

	HandleRelayLocalStreamFinish(
		hub,
		senderConn,
		makeRelayLocalStreamPacket(t, protocol.CmdRelayLocalStreamFinish, protocol.RelayLocalStreamFinishPayload{
			SessionID:    sessionID,
			MsgID:        startAck.MsgID,
			FinalContent: "这次先结束，方便下一次验证",
		}),
	)

	senderConn.sent = nil
	peerConn.sent = nil

	HandleSendMsg(hub, senderConn, makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-local-clear-3",
		MsgType:     1,
		Content:     "@本地小龙虾 新的一次不要旧前文",
	}))

	thirdAck, ok := findSendAck(senderConn.sent)
	if !ok || thirdAck.LocalInference == nil {
		t.Fatalf("third send should include local inference, ack=%#v", thirdAck)
	}
	if len(thirdAck.LocalInference.ContextMessages) != 1 {
		t.Fatalf("third context_messages=%#v want=1 item", thirdAck.LocalInference.ContextMessages)
	}
	if thirdAck.LocalInference.ContextMessages[0].Content != "@本地小龙虾 新的一次不要旧前文" {
		t.Fatalf("third context content=%q", thirdAck.LocalInference.ContextMessages[0].Content)
	}
}

func TestHandleSendMsgGroupMentionWithContextTriggersDelegatedAgent(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-group-mention-context-delegate"
		ownerID   = int64(8477)
		senderID  = int64(8478)
		otherID   = int64(8479)
		agentID   = int64(9991)
	)

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(nil)
	defer wsagentapi.SetGlobal(prevManager)

	if err := store.DB.Create(&model.Agent{
		ID:           agentID,
		AgentName:    "delegate-api-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       1,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{
			SessionID:                sessionID,
			MemberID:                 ownerID,
			MemberType:               1,
			AgentReceiveMode:         agentreceive.ModeNormal,
			AgentReceiveBacklogCount: 2,
		},
		{SessionID: sessionID, MemberID: senderID, MemberType: 1},
		{SessionID: sessionID, MemberID: otherID, MemberType: 1},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	ctx := context.Background()
	delegateKey := "im:delegate:" + sessionID + ":8477"
	if err := store.RDB.HSet(ctx, delegateKey,
		"agent_id", "9991",
		"max_consecutive_replies", "3",
	).Err(); err != nil {
		t.Fatalf("seed delegate key error: %v", err)
	}

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "sender-dev"}
	ownerConn := &sendMsgMockConn{userID: ownerID, deviceID: "owner-dev"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
			ownerID:  {ownerConn},
		},
	}

	HandleSendMsg(hub, senderConn, makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "group-delegate-context-buffer",
		MsgType:     1,
		Content:     "先让别人看这句",
		Extra:       json.RawMessage(`{"mention_user_ids":["8479"]}`),
	}))

	queuedRaw, err := store.RDB.LRange(ctx, "im:agent_api:queued_events:9991", 0, -1).Result()
	if err != nil {
		t.Fatalf("load queued delegate events error: %v", err)
	}
	if len(queuedRaw) != 0 {
		t.Fatalf("delegate should ignore messages already aimed at others, got=%#v", queuedRaw)
	}

	HandleSendMsg(hub, senderConn, makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "group-delegate-context-mention",
		MsgType:     1,
		Content:     "请处理 @8477",
		Extra:       json.RawMessage(`{"mention_user_ids":["8477"]}`),
	}))

	queuedRaw, err = store.RDB.LRange(ctx, "im:agent_api:queued_events:9991", 0, -1).Result()
	if err != nil {
		t.Fatalf("reload queued delegate events error: %v", err)
	}
	if len(queuedRaw) != 1 {
		t.Fatalf("queued delegate events count=%d want=1 payload=%#v", len(queuedRaw), queuedRaw)
	}

	var queuedEvent wsagentapi.DelegateEventPayload
	if err := json.Unmarshal([]byte(queuedRaw[0]), &queuedEvent); err != nil {
		t.Fatalf("unmarshal queued delegate event error: %v", err)
	}
	if queuedEvent.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("queued mirror_mode=%q want=%q", queuedEvent.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
	}
	if queuedEvent.EventType != "group_mention" {
		t.Fatalf("queued event_type=%s want=group_mention", queuedEvent.EventType)
	}
	if len(queuedEvent.ContextMessages) != 2 {
		t.Fatalf("context_messages=%#v want=2 items", queuedEvent.ContextMessages)
	}
	if queuedEvent.ContextMessages[0].Content != "先让别人看这句" {
		t.Fatalf("first context content=%q", queuedEvent.ContextMessages[0].Content)
	}
	if queuedEvent.ContextMessages[1].Content != "请处理 @8477" {
		t.Fatalf("second context content=%q", queuedEvent.ContextMessages[1].Content)
	}
}

func TestHandleSendMsgIdleGroupWithoutMentionDispatchesAllDelegatedAgents(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-group-idle-cold-start-delegate"
		senderID  = int64(8485)
		ownerAID  = int64(8486)
		ownerBID  = int64(8487)
		agentAID  = int64(9994)
		agentBID  = int64(9995)
	)

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(nil)
	defer wsagentapi.SetGlobal(prevManager)

	agents := []model.Agent{
		{ID: agentAID, AgentName: "delegate-idle-a", OwnerID: ownerAID, ProviderType: model.AgentProviderAPI, Status: 1},
		{ID: agentBID, AgentName: "delegate-idle-b", OwnerID: ownerBID, ProviderType: model.AgentProviderAPI, Status: 1},
	}
	for _, agent := range agents {
		if err := store.DB.Create(&agent).Error; err != nil {
			t.Fatalf("create agent error: %v", err)
		}
	}
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     senderID,
		SessionType: 2,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: senderID, MemberType: 1},
		{SessionID: sessionID, MemberID: ownerAID, MemberType: 1, AgentReceiveMode: agentreceive.ModeNormal},
		{SessionID: sessionID, MemberID: ownerBID, MemberType: 1, AgentReceiveMode: agentreceive.ModeNormal, AgentReceiveBacklogCount: 2},
	}
	for _, member := range members {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":8486",
		"agent_id", "9994",
		"max_consecutive_replies", "3",
	).Err(); err != nil {
		t.Fatalf("seed delegate key A error: %v", err)
	}
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":8487",
		"agent_id", "9995",
		"max_consecutive_replies", "3",
	).Err(); err != nil {
		t.Fatalf("seed delegate key B error: %v", err)
	}
	seedSendMsgIdleGroupHistory(t, sessionID, senderID, groupColdStartIdleThreshold+time.Minute)

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "sender-dev"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
		},
	}

	HandleSendMsg(hub, senderConn, makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "group-idle-cold-start-delegate",
		MsgType:     1,
		Content:     "这个群安静太久了，我来打个招呼",
	}))

	queueA, err := store.RDB.LRange(ctx, "im:agent_api:queued_events:9994", 0, -1).Result()
	if err != nil {
		t.Fatalf("load agent A queued events error: %v", err)
	}
	queueB, err := store.RDB.LRange(ctx, "im:agent_api:queued_events:9995", 0, -1).Result()
	if err != nil {
		t.Fatalf("load agent B queued events error: %v", err)
	}
	if len(queueA) != 1 || len(queueB) != 1 {
		t.Fatalf("cold-start delegated queues=%#v %#v want one event each", queueA, queueB)
	}

	for _, raw := range []string{queueA[0], queueB[0]} {
		var queuedEvent wsagentapi.DelegateEventPayload
		if err := json.Unmarshal([]byte(raw), &queuedEvent); err != nil {
			t.Fatalf("unmarshal queued delegate event error: %v", err)
		}
		if queuedEvent.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
			t.Fatalf("queued mirror_mode=%q want=%q", queuedEvent.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
		}
		if queuedEvent.EventType != "group_message" {
			t.Fatalf("queued event_type=%s want=group_message", queuedEvent.EventType)
		}
	}
}

func TestHandleSendMsgQuotedTargetWakesMentionOnlyDelegatedAgent(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-group-quoted-target-mention-only-delegate"
		senderID  = int64(8488)
		ownerID   = int64(8489)
		agentID   = int64(9996)
		msgID     = int64(18889990621)
	)

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(nil)
	defer wsagentapi.SetGlobal(prevManager)

	seedUser := func(id int64, name string) {
		if err := store.DB.Create(&model.User{
			ID:       id,
			Username: name,
			Email:    name + "@example.com",
			Nickname: name,
		}).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}
	seedUser(senderID, "sender_8488")
	seedUser(ownerID, "owner_8489")

	if err := store.DB.Create(&model.Agent{
		ID:           agentID,
		OwnerID:      ownerID,
		AgentName:    "delegate-quoted-agent",
		ProviderType: model.AgentProviderAPI,
		Status:       1,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&[]model.SessionMember{
		{SessionID: sessionID, MemberID: senderID, MemberType: 1},
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, AgentReceiveMode: agentreceive.ModeMentionOnly},
	}).Error; err != nil {
		t.Fatalf("create session members error: %v", err)
	}
	if err := store.DB.Create(&model.Message{
		MsgID:      msgID,
		SessionID:  sessionID,
		SenderID:   ownerID,
		SenderType: 1,
		MsgType:    1,
		Content:    "这句留着被引用",
		CreatedAt:  time.Now().UTC().Add(-time.Second),
	}).Error; err != nil {
		t.Fatalf("create quoted message error: %v", err)
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":8489",
		"agent_id", "9996",
		"max_consecutive_replies", "3",
	).Err(); err != nil {
		t.Fatalf("seed delegate key error: %v", err)
	}

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "sender-dev"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
		},
	}

	HandleSendMsg(hub, senderConn, makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:       sessionID,
		ClientMsgID:     "group-quoted-target-mention-only-delegate",
		MsgType:         1,
		Content:         "我只是在回复你，但这句没有点名",
		QuotedMessageID: msgID,
	}))

	queuedRaw, err := store.RDB.LRange(ctx, "im:agent_api:queued_events:9996", 0, -1).Result()
	if err != nil {
		t.Fatalf("load queued delegate events error: %v", err)
	}
	if len(queuedRaw) != 1 {
		t.Fatalf("queued delegate events count=%d want=1 payload=%#v", len(queuedRaw), queuedRaw)
	}

	var queuedEvent wsagentapi.DelegateEventPayload
	if err := json.Unmarshal([]byte(queuedRaw[0]), &queuedEvent); err != nil {
		t.Fatalf("unmarshal queued delegate event error: %v", err)
	}
	if queuedEvent.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("mirror_mode=%q want=%q", queuedEvent.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
	}
	if queuedEvent.EventType != "group_mention" {
		t.Fatalf("event_type=%s want=group_mention", queuedEvent.EventType)
	}
	if queuedEvent.Content != "我只是在回复你，但这句没有点名" {
		t.Fatalf("queued event content=%q", queuedEvent.Content)
	}
	if len(queuedEvent.ContextMessages) != 1 {
		t.Fatalf("context_messages=%#v want=1 item (quoted only, no backlog for ModeMentionOnly)", queuedEvent.ContextMessages)
	}
	if queuedEvent.ContextMessages[0].MsgID != msgID {
		t.Fatalf("quoted context msg_id=%d want=%d", queuedEvent.ContextMessages[0].MsgID, msgID)
	}
	if queuedEvent.ContextMessages[0].Content != "[引用消息]\n这句留着被引用" {
		t.Fatalf("quoted context content=%q", queuedEvent.ContextMessages[0].Content)
	}
}

func TestHandleSendMsgDelegatedMentionOnlyWithoutQuoteHasEmptyContext(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-group-mention-only-no-quote-delegate"
		senderID  = int64(8493)
		ownerID   = int64(8494)
		agentID   = int64(9999)
	)

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(nil)
	defer wsagentapi.SetGlobal(prevManager)

	seedUser := func(id int64, name string) {
		if err := store.DB.Create(&model.User{
			ID: id, Username: name, Email: name + "@example.com", Nickname: name,
		}).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}
	seedUser(senderID, "sender_8493")
	seedUser(ownerID, "owner_8494")

	if err := store.DB.Create(&model.Agent{
		ID: agentID, OwnerID: ownerID, AgentName: "delegate-no-quote-agent",
		ProviderType: model.AgentProviderAPI, Status: 1,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}
	if err := store.DB.Create(&model.Session{
		SessionID: sessionID, OwnerID: ownerID, SessionType: 2,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&[]model.SessionMember{
		{SessionID: sessionID, MemberID: senderID, MemberType: 1},
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, AgentReceiveMode: agentreceive.ModeMentionOnly},
	}).Error; err != nil {
		t.Fatalf("create session members error: %v", err)
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":8494",
		"agent_id", "9999",
		"max_consecutive_replies", "3",
	).Err(); err != nil {
		t.Fatalf("seed delegate key error: %v", err)
	}

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "sender-dev"}
	ownerConn := &sendMsgMockConn{userID: ownerID, deviceID: "owner-dev"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
			ownerID:  {ownerConn},
		},
	}

	HandleSendMsg(hub, senderConn, makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "group-mention-only-no-quote-delegate",
		MsgType:     1,
		Content:     fmt.Sprintf("@owner_8494 处理一下"),
		Extra:       json.RawMessage(fmt.Sprintf(`{"mention_user_ids":["%d"]}`, ownerID)),
	}))

	queuedRaw, err := store.RDB.LRange(ctx, "im:agent_api:queued_events:9999", 0, -1).Result()
	if err != nil {
		t.Fatalf("load queued delegate events error: %v", err)
	}
	if len(queuedRaw) != 1 {
		t.Fatalf("queued delegate events count=%d want=1 payload=%#v", len(queuedRaw), queuedRaw)
	}

	var queuedEvent wsagentapi.DelegateEventPayload
	if err := json.Unmarshal([]byte(queuedRaw[0]), &queuedEvent); err != nil {
		t.Fatalf("unmarshal queued delegate event error: %v", err)
	}
	if queuedEvent.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("mirror_mode=%q want=%q", queuedEvent.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
	}
	if queuedEvent.EventType != "group_mention" {
		t.Fatalf("event_type=%s want=group_mention", queuedEvent.EventType)
	}
	if len(queuedEvent.ContextMessages) != 0 {
		t.Fatalf("context_messages=%#v want=empty (ModeMentionOnly without quote)", queuedEvent.ContextMessages)
	}
}

func TestHandleSendMsgMentionAllContinuationWakesMentionOnlyDelegatedAgents(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-group-mention-all-mention-only-delegate"
		senderID  = int64(8490)
		ownerAID  = int64(8491)
		ownerBID  = int64(8492)
		agentAID  = int64(9997)
		agentBID  = int64(9998)
	)

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(nil)
	defer wsagentapi.SetGlobal(prevManager)

	seedUser := func(id int64, name string) {
		if err := store.DB.Create(&model.User{
			ID:       id,
			Username: name,
			Email:    name + "@example.com",
			Nickname: name,
		}).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}
	seedUser(senderID, "sender_8490")
	seedUser(ownerAID, "owner_8491")
	seedUser(ownerBID, "owner_8492")

	for _, agent := range []model.Agent{
		{ID: agentAID, OwnerID: ownerAID, AgentName: "delegate-all-a", ProviderType: model.AgentProviderAPI, Status: 1},
		{ID: agentBID, OwnerID: ownerBID, AgentName: "delegate-all-b", ProviderType: model.AgentProviderAPI, Status: 1},
	} {
		if err := store.DB.Create(&agent).Error; err != nil {
			t.Fatalf("create agent error: %v", err)
		}
	}
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerAID,
		SessionType: 2,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&[]model.SessionMember{
		{SessionID: sessionID, MemberID: senderID, MemberType: 1},
		{SessionID: sessionID, MemberID: ownerAID, MemberType: 1, AgentReceiveMode: agentreceive.ModeMentionOnly},
		{SessionID: sessionID, MemberID: ownerBID, MemberType: 1, AgentReceiveMode: agentreceive.ModeMentionOnly},
	}).Error; err != nil {
		t.Fatalf("create session members error: %v", err)
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":8491",
		"agent_id", "9997",
		"max_consecutive_replies", "3",
	).Err(); err != nil {
		t.Fatalf("seed delegate key A error: %v", err)
	}
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":8492",
		"agent_id", "9998",
		"max_consecutive_replies", "3",
	).Err(); err != nil {
		t.Fatalf("seed delegate key B error: %v", err)
	}

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "sender-dev"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
		},
	}

	HandleSendMsg(hub, senderConn, makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "group-mention-all-mention-only-delegate-first",
		MsgType:     1,
		Content:     "@所有人 都先来一下",
	}))

	for _, agentID := range []int64{agentAID, agentBID} {
		queueKey := fmt.Sprintf("im:agent_api:queued_events:%d", agentID)
		queuedRaw, err := store.RDB.LRange(ctx, queueKey, 0, -1).Result()
		if err != nil {
			t.Fatalf("load first queued delegate events error: %v", err)
		}
		if len(queuedRaw) != 1 {
			t.Fatalf("first queued delegate events count=%d want=1 payload=%#v", len(queuedRaw), queuedRaw)
		}
		var queuedEvent wsagentapi.DelegateEventPayload
		if err := json.Unmarshal([]byte(queuedRaw[0]), &queuedEvent); err != nil {
			t.Fatalf("unmarshal first queued delegate event error: %v", err)
		}
		if queuedEvent.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
			t.Fatalf("first mirror_mode=%q want=%q", queuedEvent.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
		}
		if queuedEvent.EventType != "group_mention" {
			t.Fatalf("first event_type=%s want=group_mention", queuedEvent.EventType)
		}
		if err := store.RDB.Del(ctx, queueKey).Err(); err != nil {
			t.Fatalf("clear first queued delegate events error: %v", err)
		}
	}

	HandleSendMsg(hub, senderConn, makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "group-mention-all-mention-only-delegate-second",
		MsgType:     1,
		Content:     "第二句继续一起回答",
	}))

	for _, agentID := range []int64{agentAID, agentBID} {
		queueKey := fmt.Sprintf("im:agent_api:queued_events:%d", agentID)
		queuedRaw, err := store.RDB.LRange(ctx, queueKey, 0, -1).Result()
		if err != nil {
			t.Fatalf("load second queued delegate events error: %v", err)
		}
		if len(queuedRaw) != 1 {
			t.Fatalf("second queued delegate events count=%d want=1 payload=%#v", len(queuedRaw), queuedRaw)
		}
		var queuedEvent wsagentapi.DelegateEventPayload
		if err := json.Unmarshal([]byte(queuedRaw[0]), &queuedEvent); err != nil {
			t.Fatalf("unmarshal second queued delegate event error: %v", err)
		}
		if queuedEvent.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
			t.Fatalf("second mirror_mode=%q want=%q", queuedEvent.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
		}
		if queuedEvent.EventType != "group_message" {
			t.Fatalf("second event_type=%s want=group_message", queuedEvent.EventType)
		}
		if len(queuedEvent.MentionUserIDs) != 0 {
			t.Fatalf("second mention_user_ids=%v want=[]", queuedEvent.MentionUserIDs)
		}
	}
}

func TestTriggerDelegatesForAgentReplySkipsMentionOnlyOtherDelegatedAgents(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-group-agent-reply-context-delegate"
		ownerAID  = int64(8481)
		ownerBID  = int64(8482)
		agentAID  = int64(9992)
		agentBID  = int64(9993)
		msgID     = int64(18889990561)
	)

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(nil)
	defer wsagentapi.SetGlobal(prevManager)

	agents := []model.Agent{
		{ID: agentAID, AgentName: "delegate-agent-a", OwnerID: ownerAID, ProviderType: model.AgentProviderAPI, Status: 1},
		{ID: agentBID, AgentName: "delegate-agent-b", OwnerID: ownerBID, ProviderType: model.AgentProviderAPI, Status: 1},
	}
	for _, agent := range agents {
		if err := store.DB.Create(&agent).Error; err != nil {
			t.Fatalf("create agent error: %v", err)
		}
	}
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerAID,
		SessionType: 2,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{
			SessionID:                sessionID,
			MemberID:                 ownerAID,
			MemberType:               1,
			AgentReceiveMode:         agentreceive.ModeMentionOnly,
			AgentReceiveBacklogCount: 2,
		},
		{
			SessionID:                sessionID,
			MemberID:                 ownerBID,
			MemberType:               1,
			AgentReceiveMode:         agentreceive.ModeMentionOnly,
			AgentReceiveBacklogCount: 2,
		},
	}
	for _, member := range members {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":8481",
		"agent_id", "9992",
		"max_consecutive_replies", "3",
	).Err(); err != nil {
		t.Fatalf("seed delegate key A error: %v", err)
	}
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":8482",
		"agent_id", "9993",
		"max_consecutive_replies", "3",
	).Err(); err != nil {
		t.Fatalf("seed delegate key B error: %v", err)
	}

	hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{}}
	TriggerDelegatesForMessage(
		hub,
		ctx,
		sessionID,
		agentAID,
		2,
		msgID,
		0,
		1,
		"我先回答一句",
		nil,
		nil,
	)

	queuedRaw, err := store.RDB.LRange(ctx, "im:agent_api:queued_events:9993", 0, -1).Result()
	if err != nil {
		t.Fatalf("load queued delegate events error: %v", err)
	}
	if len(queuedRaw) != 0 {
		t.Fatalf("queued delegate events count=%d want=0 payload=%#v", len(queuedRaw), queuedRaw)
	}

	ownQueue, err := store.RDB.LRange(ctx, "im:agent_api:queued_events:9992", 0, -1).Result()
	if err != nil {
		t.Fatalf("load own queued delegate events error: %v", err)
	}
	if len(ownQueue) != 0 {
		t.Fatalf("sender delegate agent should not mirror to itself, got=%#v", ownQueue)
	}
}

func TestTriggerDelegatesForAgentReplyExplicitNumericMentionDispatchesOwner(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-group-agent-reply-explicit-mention-delegate"
		ownerAID  = int64(8483)
		ownerBID  = int64(8484)
		agentAID  = int64(9994)
		agentBID  = int64(9995)
		msgID     = int64(18889990562)
	)

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(nil)
	defer wsagentapi.SetGlobal(prevManager)

	agents := []model.Agent{
		{ID: agentAID, AgentName: "delegate-agent-a", OwnerID: ownerAID, ProviderType: model.AgentProviderAPI, Status: 1},
		{ID: agentBID, AgentName: "delegate-agent-b", OwnerID: ownerBID, ProviderType: model.AgentProviderAPI, Status: 1},
	}
	for _, agent := range agents {
		if err := store.DB.Create(&agent).Error; err != nil {
			t.Fatalf("create agent error: %v", err)
		}
	}
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerAID,
		SessionType: 2,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{
			SessionID:                sessionID,
			MemberID:                 ownerAID,
			MemberType:               1,
			AgentReceiveMode:         agentreceive.ModeMentionOnly,
			AgentReceiveBacklogCount: 2,
		},
		{
			SessionID:                sessionID,
			MemberID:                 ownerBID,
			MemberType:               1,
			AgentReceiveMode:         agentreceive.ModeMentionOnly,
			AgentReceiveBacklogCount: 2,
		},
	}
	for _, member := range members {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":8483",
		"agent_id", "9994",
		"max_consecutive_replies", "3",
	).Err(); err != nil {
		t.Fatalf("seed delegate key A error: %v", err)
	}
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":8484",
		"agent_id", "9995",
		"max_consecutive_replies", "3",
	).Err(); err != nil {
		t.Fatalf("seed delegate key B error: %v", err)
	}

	hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{}}
	TriggerDelegatesForMessage(
		hub,
		ctx,
		sessionID,
		agentAID,
		2,
		msgID,
		0,
		1,
		fmt.Sprintf("@%d 你来处理这句", ownerBID),
		nil,
		nil,
	)

	queuedRaw, err := store.RDB.LRange(ctx, "im:agent_api:queued_events:9995", 0, -1).Result()
	if err != nil {
		t.Fatalf("load queued delegate events error: %v", err)
	}
	if len(queuedRaw) != 1 {
		t.Fatalf("queued delegate events count=%d want=1 payload=%#v", len(queuedRaw), queuedRaw)
	}

	var queuedEvent wsagentapi.DelegateEventPayload
	if err := json.Unmarshal([]byte(queuedRaw[0]), &queuedEvent); err != nil {
		t.Fatalf("unmarshal queued delegate event error: %v", err)
	}
	if queuedEvent.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("queued mirror_mode=%q want=%q", queuedEvent.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
	}
	if queuedEvent.EventType != "group_mention" {
		t.Fatalf("queued event_type=%s want=group_mention", queuedEvent.EventType)
	}
	if !containsInt64(queuedEvent.MentionUserIDs, ownerBID) {
		t.Fatalf("queued mention_user_ids=%v want to include %d", queuedEvent.MentionUserIDs, ownerBID)
	}
	if queuedEvent.OwnerID != ownerBID {
		t.Fatalf("queued owner_id=%d want=%d", queuedEvent.OwnerID, ownerBID)
	}
	if queuedEvent.SenderID != agentAID {
		t.Fatalf("queued sender_id=%d want=%d", queuedEvent.SenderID, agentAID)
	}

	ownQueue, err := store.RDB.LRange(ctx, "im:agent_api:queued_events:9994", 0, -1).Result()
	if err != nil {
		t.Fatalf("load own queued delegate events error: %v", err)
	}
	if len(ownQueue) != 0 {
		t.Fatalf("sender delegate agent should not mirror to itself, got=%#v", ownQueue)
	}
}

func TestHandleSendMsgPrivateSessionStripsMentionUserIDs(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-private-no-mention"
		ownerID   = int64(8201)
		senderID  = int64(8202)
	)

	users := []model.User{
		{ID: ownerID, Username: "owner_user", Email: "owner_user@example.com", Nickname: "老王"},
		{ID: senderID, Username: "sender_user", Email: "sender_user@example.com", Nickname: "小李"},
	}
	for _, u := range users {
		if err := store.DB.Create(&u).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1},
		{SessionID: sessionID, MemberID: senderID, MemberType: 1},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	seedSendMsgFriendRelation(t, ownerID, senderID)
	seedSendMsgFriendRelation(t, senderID, ownerID)

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "dev-origin"}
	recipient := &sendMsgMockConn{userID: ownerID, deviceID: "dev-recipient"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
			ownerID:  {recipient},
		},
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-private-mention-1",
		MsgType:     1,
		Content:     "@owner_user 请看一下 @12345",
		Extra:       json.RawMessage(`{"mention_user_ids":["8201","12345"],"reply_mode":"plain"}`),
	})

	HandleSendMsg(hub, senderConn, pkt)

	var msg model.Message
	if err := store.DB.Where("session_id = ?", sessionID).Order("msg_id DESC").First(&msg).Error; err != nil {
		t.Fatalf("load message error: %v", err)
	}

	var extra map[string]any
	if len(msg.Extra) > 0 {
		if err := json.Unmarshal(msg.Extra, &extra); err != nil {
			t.Fatalf("unmarshal message extra error: %v", err)
		}
	}
	if _, ok := extra["mention_user_ids"]; ok {
		t.Fatalf("private session should not persist mention_user_ids, got=%#v", extra["mention_user_ids"])
	}
	if extra["reply_mode"] != "plain" {
		t.Fatalf("other extra fields should stay intact, got=%#v", extra)
	}

	if len(recipient.sent) != 1 {
		t.Fatalf("recipient should receive one push_msg, got=%d", len(recipient.sent))
	}
	pushPayload, ok := recipient.sent[0].payload.(protocol.PushMsgPayload)
	if !ok {
		t.Fatalf("recipient payload should be PushMsgPayload, got=%T", recipient.sent[0].payload)
	}
	if len(pushPayload.Extra) > 0 {
		var pushExtra map[string]any
		if err := json.Unmarshal(pushPayload.Extra, &pushExtra); err != nil {
			t.Fatalf("unmarshal push extra error: %v", err)
		}
		if _, ok := pushExtra["mention_user_ids"]; ok {
			t.Fatalf("private push payload should not contain mention_user_ids, got=%#v", pushExtra["mention_user_ids"])
		}
	}
}

func TestHandleSendMsgSessionSummaryStaysValidUTF8(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-send-utf8-summary"
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     5001,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: 5001, MemberType: 1},
		{SessionID: sessionID, MemberID: 5002, MemberType: 1},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	seedSendMsgFriendRelation(t, 5001, 5002)
	seedSendMsgFriendRelation(t, 5002, 5001)

	origin := &sendMsgMockConn{userID: 5001, deviceID: "dev-origin"}
	recipient := &sendMsgMockConn{userID: 5002, deviceID: "dev-recipient"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			5001: {origin},
			5002: {recipient},
		},
	}

	// 60 runes total (59 ASCII + 1 Chinese rune), but 62 bytes.
	// Byte-based truncation at 60 would split the last rune and produce invalid UTF-8.
	content := strings.Repeat("a", 59) + "你"
	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-utf8-summary",
		MsgType:     1,
		Content:     content,
	})
	HandleSendMsg(hub, origin, pkt)

	if countSentCmd(origin.sent, protocol.CmdSendAck) != 1 ||
		countSentCmd(origin.sent, protocol.CmdPushMsg) != 1 {
		t.Fatalf("origin should receive one send_ack and one push_msg, got=%#v", origin.sent)
	}

	var sess model.Session
	if err := store.DB.Where("session_id = ?", sessionID).First(&sess).Error; err != nil {
		t.Fatalf("query session error: %v", err)
	}
	if !utf8.ValidString(sess.LastMsgSummary) {
		t.Fatalf("session summary must be valid UTF-8, got invalid bytes: %q", sess.LastMsgSummary)
	}
	if sess.LastMsgSummary != content {
		t.Fatalf("session summary mismatch, got=%q want=%q", sess.LastMsgSummary, content)
	}
}

func TestHandleSendMsgPersistsThreadIDAndPushesTopLevelField(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-send-thread"
		senderID  = int64(8301)
		peerID    = int64(8302)
	)

	now := time.Now().UTC()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     senderID,
		SessionType: 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, member := range []model.SessionMember{
		{SessionID: sessionID, MemberID: senderID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: peerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
	} {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	for _, rel := range []model.Friend{
		{ID: now.UnixNano() + senderID + peerID, UserID: senderID, FriendID: peerID},
		{ID: now.UnixNano() + senderID + peerID + 1, UserID: peerID, FriendID: senderID},
	} {
		if err := store.DB.Create(&rel).Error; err != nil {
			t.Fatalf("create friend relation error: %v", err)
		}
	}

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "dev-thread-sender"}
	peerConn := &sendMsgMockConn{userID: peerID, deviceID: "dev-thread-peer"}
	hub := &sendMsgMockHub{
		nodeID: "node-thread",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
			peerID:   {peerConn},
		},
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ThreadID:    "topic-send-a",
		ClientMsgID: "cmsg-thread-1",
		MsgType:     1,
		Content:     "threaded hello",
		Extra:       json.RawMessage(`{"reply_mode":"plain"}`),
	})

	HandleSendMsg(hub, senderConn, pkt)

	var msg model.Message
	if err := store.DB.Where("session_id = ?", sessionID).Order("msg_id DESC").First(&msg).Error; err != nil {
		t.Fatalf("load message error: %v", err)
	}
	if msg.ThreadID != "topic-send-a" {
		t.Fatalf("message thread_id=%q want=topic-send-a", msg.ThreadID)
	}

	var extra map[string]any
	if err := json.Unmarshal(msg.Extra, &extra); err != nil {
		t.Fatalf("unmarshal message extra error: %v", err)
	}
	if extra["thread_id"] != "topic-send-a" {
		t.Fatalf("message extra thread_id=%v want=topic-send-a", extra["thread_id"])
	}

	pushPayload, ok := findPushMsg(peerConn.sent)
	if !ok {
		t.Fatal("expected peer push_msg")
	}
	if pushPayload.ThreadID != "topic-send-a" {
		t.Fatalf("push thread_id=%q want=topic-send-a", pushPayload.ThreadID)
	}
}

func TestHandleSendMsgIdempotentAckIncludesSameInboxSeq(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-send-idempotent"
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     3001,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{
		SessionID:  sessionID,
		MemberID:   3001,
		MemberType: 1,
	}).Error; err != nil {
		t.Fatalf("create session member error: %v", err)
	}

	origin := &sendMsgMockConn{userID: 3001, deviceID: "dev-origin"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			3001: {origin},
		},
	}

	payload := protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "same-client-msg",
		MsgType:     1,
		Content:     "once only",
	}

	HandleSendMsg(hub, origin, makeSendMsgPacket(t, payload))
	HandleSendMsg(hub, origin, makeSendMsgPacket(t, payload))

	if countSentCmd(origin.sent, protocol.CmdSendAck) != 2 {
		t.Fatalf("origin should receive two send_ack responses, got=%#v", origin.sent)
	}
	if countSentCmd(origin.sent, protocol.CmdPushMsg) != 1 {
		t.Fatalf("origin should receive one push_msg for first successful send, got=%#v", origin.sent)
	}
	acks := make([]protocol.SendAckPayload, 0, 2)
	for _, item := range origin.sent {
		if item.cmd != protocol.CmdSendAck {
			continue
		}
		ack, ok := item.payload.(protocol.SendAckPayload)
		if !ok {
			t.Fatalf("send_ack payload should be SendAckPayload, got=%T", item.payload)
		}
		acks = append(acks, ack)
	}
	if len(acks) != 2 {
		t.Fatalf("expected two parsed send_ack payloads, got=%d", len(acks))
	}
	ack1, ack2 := acks[0], acks[1]
	if ack1.MsgID != ack2.MsgID {
		t.Fatalf("idempotent send should return same msg_id, got first=%d second=%d", ack1.MsgID, ack2.MsgID)
	}
	if ack1.InboxSeq != ack2.InboxSeq {
		t.Fatalf("idempotent send should return same inbox_seq, got first=%d second=%d", ack1.InboxSeq, ack2.InboxSeq)
	}

	var msgCount int64
	if err := store.DB.Model(&model.Message{}).Count(&msgCount).Error; err != nil {
		t.Fatalf("count messages error: %v", err)
	}
	if msgCount != 1 {
		t.Fatalf("idempotent send should create one message row, got=%d", msgCount)
	}

	var senderInboxCount int64
	if err := store.DB.Model(&model.UserInbox{}).
		Where("user_id = ? AND msg_id = ? AND session_id = ?", int64(3001), ack1.MsgID, sessionID).
		Count(&senderInboxCount).Error; err != nil {
		t.Fatalf("count sender inbox rows error: %v", err)
	}
	if senderInboxCount != 1 {
		t.Fatalf("idempotent send should create one sender inbox row, got=%d", senderInboxCount)
	}
}

func TestHandleSendMsgHealsRecipientInboxSeqAfterRedisRegression(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-send-rollback"
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     4001,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: 4001, MemberType: 1},
		{SessionID: sessionID, MemberID: 4002, MemberType: 1},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	seedSendMsgFriendRelation(t, 4001, 4002)
	seedSendMsgFriendRelation(t, 4002, 4001)

	// Seed recipient inbox with seq=1, then force Redis cache to lag behind.
	// Allocator should lift the next seq above the persisted max instead of colliding.
	if err := store.DB.Create(&model.UserInbox{
		UserID:    4002,
		InboxSeq:  1,
		MsgID:     888001,
		SessionID: sessionID,
	}).Error; err != nil {
		t.Fatalf("seed recipient inbox error: %v", err)
	}
	if err := store.RDB.Set(context.Background(), "im:inbox_seq:4002", 0, 0).Err(); err != nil {
		t.Fatalf("seed recipient inbox_seq redis key error: %v", err)
	}

	origin := &sendMsgMockConn{userID: 4001, deviceID: "dev-origin"}
	recipient := &sendMsgMockConn{userID: 4002, deviceID: "dev-recipient"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			4001: {origin},
			4002: {recipient},
		},
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-heal-inbox-seq",
		MsgType:     1,
		Content:     "should heal inbox seq",
	})
	HandleSendMsg(hub, origin, pkt)

	if countSentCmd(origin.sent, protocol.CmdSendAck) != 1 ||
		countSentCmd(origin.sent, protocol.CmdPushMsg) != 1 {
		t.Fatalf("origin should receive one send_ack and one push_msg, got=%#v", origin.sent)
	}
	if countSentCmd(recipient.sent, protocol.CmdPushMsg) != 1 {
		t.Fatalf("recipient should receive one push after healed send, got=%#v", recipient.sent)
	}

	var msgCount int64
	if err := store.DB.Model(&model.Message{}).
		Where("session_id = ? AND content = ?", sessionID, "should heal inbox seq").
		Count(&msgCount).Error; err != nil {
		t.Fatalf("count message rows error: %v", err)
	}
	if msgCount != 1 {
		t.Fatalf("message row should be persisted once, got=%d", msgCount)
	}

	var senderInboxCount int64
	if err := store.DB.Model(&model.UserInbox{}).
		Where("user_id = ? AND session_id = ?", int64(4001), sessionID).
		Count(&senderInboxCount).Error; err != nil {
		t.Fatalf("count sender inbox rows error: %v", err)
	}
	if senderInboxCount != 1 {
		t.Fatalf("sender inbox row should be created once, got=%d", senderInboxCount)
	}

	var recipientRows []model.UserInbox
	if err := store.DB.
		Where("user_id = ? AND session_id = ?", int64(4002), sessionID).
		Order("inbox_seq ASC").
		Find(&recipientRows).Error; err != nil {
		t.Fatalf("load recipient inbox rows error: %v", err)
	}
	if len(recipientRows) != 2 {
		t.Fatalf("recipient inbox rows=%d want=2", len(recipientRows))
	}
	if recipientRows[1].InboxSeq != 2 {
		t.Fatalf("recipient healed inbox_seq=%d want=2", recipientRows[1].InboxSeq)
	}
}

func TestDelegateMaxRepliesFromAgentConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  string
		want int
	}{
		{name: "empty", cfg: "", want: defaultDelegateMaxConsecutiveReplies},
		{name: "invalid", cfg: "{", want: defaultDelegateMaxConsecutiveReplies},
		{name: "normal", cfg: `{"delegate_max_consecutive_replies":3}`, want: 3},
		{name: "alias", cfg: `{"max_consecutive_replies":"4"}`, want: 4},
		{name: "too_small", cfg: `{"delegate_max_consecutive_replies":0}`, want: defaultDelegateMaxConsecutiveReplies},
		{name: "too_large", cfg: `{"delegate_max_consecutive_replies":999}`, want: maxDelegateMaxConsecutiveReplies},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := delegateMaxRepliesFromAgentConfig(datatypes.JSON([]byte(tt.cfg)))
			if got != tt.want {
				t.Fatalf("delegateMaxRepliesFromAgentConfig() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestParseDelegateTriggerMeta(t *testing.T) {
	meta := parseDelegateTriggerMeta(nil)
	if meta.IsDelegateOrigin {
		t.Fatalf("empty extra should not be delegate origin")
	}

	meta = parseDelegateTriggerMeta(json.RawMessage(`{"delegate_origin":true}`))
	if !meta.IsDelegateOrigin {
		t.Fatalf("delegate_origin=true should be parsed as true")
	}

	meta = parseDelegateTriggerMeta(json.RawMessage(`{"delegate_origin":"true"}`))
	if meta.IsDelegateOrigin {
		t.Fatalf("non-bool delegate_origin should not be treated as true")
	}
}

func TestCheckDelegatesRespectsOwnerStreakLimit(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-delegate-limit"
		senderID  = int64(7101)
		ownerID   = int64(7102)
	)

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     senderID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: senderID, MemberType: 1},
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	ctx := context.Background()
	hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{}}
	delegateKey := "im:delegate:" + sessionID + ":7102"
	if err := store.RDB.HSet(ctx, delegateKey,
		"agent_id", "9911",
		"max_consecutive_replies", "2",
	).Err(); err != nil {
		t.Fatalf("seed delegate key error: %v", err)
	}

	// owner streak already reaches the max, this trigger should be skipped.
	if err := store.RDB.Set(ctx, delegateStreakKey(sessionID, ownerID), 2, 0).Err(); err != nil {
		t.Fatalf("seed streak key error: %v", err)
	}
	checkDelegates(hub, ctx, sessionID, senderID, 1, 0, 1, 0, 1, "hello", nil, delegateTriggerMeta{IsDelegateOrigin: true}, nil, nil, false)

	if exists, _ := store.RDB.Exists(ctx, "im:delegate:rate:auto:"+sessionID+":7102").Result(); exists != 0 {
		t.Fatalf("rate key should not be created when streak reached max")
	}
	if streak, _ := store.RDB.Get(ctx, delegateStreakKey(sessionID, ownerID)).Int(); streak != 2 {
		t.Fatalf("streak should remain unchanged when skipped, got=%d", streak)
	}

	// Lower streak and verify this trigger is accepted.
	if err := store.RDB.Set(ctx, delegateStreakKey(sessionID, ownerID), 1, 0).Err(); err != nil {
		t.Fatalf("seed streak key error: %v", err)
	}
	checkDelegates(hub, ctx, sessionID, senderID, 1, 0, 2, 0, 1, "world", nil, delegateTriggerMeta{IsDelegateOrigin: true}, nil, nil, false)

	if exists, _ := store.RDB.Exists(ctx, "im:delegate:rate:auto:"+sessionID+":7102").Result(); exists != 1 {
		t.Fatalf("rate key should be created when trigger accepted")
	}
	if streak, _ := store.RDB.Get(ctx, delegateStreakKey(sessionID, ownerID)).Int(); streak != 1 {
		t.Fatalf("streak should remain unchanged in gateway (incremented by orchestrator), got=%d", streak)
	}
}

func TestCheckDelegatesHumanTriggerResetsOwnerStreak(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-delegate-human-reset"
		senderID  = int64(7201)
		ownerID   = int64(7202)
	)

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     senderID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: senderID, MemberType: 1},
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	ctx := context.Background()
	hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{}}
	delegateKey := "im:delegate:" + sessionID + ":7202"
	if err := store.RDB.HSet(ctx, delegateKey,
		"agent_id", "9922",
		"max_consecutive_replies", "2",
	).Err(); err != nil {
		t.Fatalf("seed delegate key error: %v", err)
	}
	if err := store.RDB.Set(ctx, delegateStreakKey(sessionID, ownerID), 2, 0).Err(); err != nil {
		t.Fatalf("seed streak key error: %v", err)
	}

	checkDelegates(hub, ctx, sessionID, senderID, 1, 0, 1, 0, 1, "human trigger", nil, delegateTriggerMeta{IsDelegateOrigin: false}, nil, nil, false)

	if exists, _ := store.RDB.Exists(ctx, "im:delegate:rate:human:"+sessionID+":7202").Result(); exists != 1 {
		t.Fatalf("human trigger should pass after resetting streak")
	}
	if exists, _ := store.RDB.Exists(ctx, delegateStreakKey(sessionID, ownerID)).Result(); exists != 0 {
		t.Fatalf("owner streak should be cleared by human trigger")
	}
}

func TestCheckDelegatesSessionScopeIsolation(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionA = "session-delegate-scope-check-a"
		sessionB = "session-delegate-scope-check-b"
		senderID = int64(7301)
		ownerID  = int64(7302)
	)

	for _, sid := range []string{sessionA, sessionB} {
		if err := store.DB.Create(&model.Session{
			SessionID:   sid,
			OwnerID:     senderID,
			SessionType: 1,
		}).Error; err != nil {
			t.Fatalf("create session %s error: %v", sid, err)
		}
		members := []model.SessionMember{
			{SessionID: sid, MemberID: senderID, MemberType: 1},
			{SessionID: sid, MemberID: ownerID, MemberType: 1},
		}
		for _, m := range members {
			if err := store.DB.Create(&m).Error; err != nil {
				t.Fatalf("create session member sid=%s mid=%d error: %v", sid, m.MemberID, err)
			}
		}
	}

	ctx := context.Background()
	hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{}}
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionA+":7302",
		"agent_id", "9933",
		"max_consecutive_replies", "2",
	).Err(); err != nil {
		t.Fatalf("seed delegate key for sessionA error: %v", err)
	}

	checkDelegates(hub, ctx, sessionB, senderID, 1, 0, 1, 0, 1, "from-session-b", nil, delegateTriggerMeta{IsDelegateOrigin: false}, nil, nil, false)
	if exists, _ := store.RDB.Exists(ctx, "im:delegate:rate:human:"+sessionB+":7302").Result(); exists != 0 {
		t.Fatalf("sessionB should not trigger delegate rate key when only sessionA is delegated")
	}

	checkDelegates(hub, ctx, sessionA, senderID, 1, 0, 2, 0, 1, "from-session-a", nil, delegateTriggerMeta{IsDelegateOrigin: false}, nil, nil, false)
	if exists, _ := store.RDB.Exists(ctx, "im:delegate:rate:human:"+sessionA+":7302").Result(); exists != 1 {
		t.Fatalf("sessionA should trigger delegate rate key")
	}
}

// --- Quote visibility inheritance tests ---

func TestHandleSendMsgQuoteVisibleToInheritsSenderVisibility(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID       = "session-quote-visible-inherit"
		senderX         = int64(9201) // original visible_to sender
		memberA         = int64(9202) // visible_to member who replies
		memberB         = int64(9203) // visible_to member
		memberC         = int64(9204) // NOT in visible_to
		quotedMessageID = int64(9200990001)
	)

	now := time.Now().UTC()
	users := []model.User{
		{ID: senderX, Username: "sender_x", Email: "x@test.com", Nickname: "X"},
		{ID: memberA, Username: "member_a", Email: "a@test.com", Nickname: "A"},
		{ID: memberB, Username: "member_b", Email: "b@test.com", Nickname: "B"},
		{ID: memberC, Username: "member_c", Email: "c@test.com", Nickname: "C"},
	}
	for _, u := range users {
		if err := store.DB.Create(&u).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     senderX,
		SessionType: 2,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, m := range []model.SessionMember{
		{SessionID: sessionID, MemberID: senderX, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: memberA, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: memberB, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: memberC, MemberType: 1, JoinedAt: now, LastActiveAt: now},
	} {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	// X sends a message with visible_to: [A, B]
	visibleToJSON, _ := json.Marshal([]int64{memberA, memberB})
	if err := store.DB.Create(&model.Message{
		MsgID:      quotedMessageID,
		SessionID:  sessionID,
		SenderID:   senderX,
		SenderType: 1,
		MsgType:    1,
		Content:    "仅AB可见的消息",
		VisibleTo:  datatypes.JSON(visibleToJSON),
		CreatedAt:  now.Add(-time.Second),
	}).Error; err != nil {
		t.Fatalf("create quoted message error: %v", err)
	}

	// A replies quoting that message (no explicit visible_to from client)
	replyConn := &sendMsgMockConn{userID: memberA, deviceID: "dev-a"}
	senderXConn := &sendMsgMockConn{userID: senderX, deviceID: "dev-x"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			memberA: {replyConn},
			senderX: {senderXConn},
		},
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:       sessionID,
		ClientMsgID:     "cmsg-quote-visible-inherit",
		MsgType:         1,
		Content:         "回复X的消息",
		QuotedMessageID: quotedMessageID,
	})

	HandleSendMsg(hub, replyConn, pkt)

	ack, ok := findSendAck(replyConn.sent)
	if !ok {
		t.Fatalf("sender should receive SendAck, got=%#v", replyConn.sent)
	}

	var msg model.Message
	if err := store.DB.Where("session_id = ? AND msg_id = ?", sessionID, ack.MsgID).
		First(&msg).Error; err != nil {
		t.Fatalf("load reply message error: %v", err)
	}

	// Reply's visible_to should be forced to [senderX]
	if msg.VisibleTo == nil {
		t.Fatal("reply message should have visible_to set, got nil")
	}
	var gotVisibleTo []int64
	if err := json.Unmarshal(msg.VisibleTo, &gotVisibleTo); err != nil {
		t.Fatalf("unmarshal visible_to error: %v", err)
	}
	if len(gotVisibleTo) != 1 || gotVisibleTo[0] != senderX {
		t.Fatalf("visible_to should be [%d], got=%v", senderX, gotVisibleTo)
	}

	// Verify: only X got a push_msg (not B, not C)
	push, ok := findPushMsg(senderXConn.sent)
	if !ok {
		t.Fatal("senderX should receive push_msg for the reply")
	}
	if push.MsgID != ack.MsgID {
		t.Fatalf("push msg_id mismatch: got=%d want=%d", push.MsgID, ack.MsgID)
	}
}

func TestHandleSendMsgQuoteNormalMessageDoesNotInheritVisibility(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID       = "session-quote-normal"
		senderX         = int64(9211)
		memberA         = int64(9212)
		quotedMessageID = int64(9211990001)
	)

	now := time.Now().UTC()
	users := []model.User{
		{ID: senderX, Username: "sender_x", Email: "x@test.com", Nickname: "X"},
		{ID: memberA, Username: "member_a", Email: "a@test.com", Nickname: "A"},
	}
	for _, u := range users {
		if err := store.DB.Create(&u).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     senderX,
		SessionType: 2,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, m := range []model.SessionMember{
		{SessionID: sessionID, MemberID: senderX, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: memberA, MemberType: 1, JoinedAt: now, LastActiveAt: now},
	} {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	// X sends a normal message (no visible_to)
	if err := store.DB.Create(&model.Message{
		MsgID:      quotedMessageID,
		SessionID:  sessionID,
		SenderID:   senderX,
		SenderType: 1,
		MsgType:    1,
		Content:    "普通消息",
		CreatedAt:  now.Add(-time.Second),
	}).Error; err != nil {
		t.Fatalf("create quoted message error: %v", err)
	}

	replyConn := &sendMsgMockConn{userID: memberA, deviceID: "dev-a"}
	senderXConn := &sendMsgMockConn{userID: senderX, deviceID: "dev-x"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			memberA: {replyConn},
			senderX: {senderXConn},
		},
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:       sessionID,
		ClientMsgID:     "cmsg-quote-normal",
		MsgType:         1,
		Content:         "回复普通消息",
		QuotedMessageID: quotedMessageID,
	})

	HandleSendMsg(hub, replyConn, pkt)

	ack, ok := findSendAck(replyConn.sent)
	if !ok {
		t.Fatalf("sender should receive SendAck, got=%#v", replyConn.sent)
	}

	var msg model.Message
	if err := store.DB.Where("session_id = ? AND msg_id = ?", sessionID, ack.MsgID).
		First(&msg).Error; err != nil {
		t.Fatalf("load reply message error: %v", err)
	}

	// Reply should NOT have visible_to set — visible to all
	if msg.VisibleTo != nil {
		t.Fatalf("reply to normal message should not have visible_to, got=%s", string(msg.VisibleTo))
	}
}

func TestHandleSendMsgQuoteVisibleToRespectsExplicitClientSetting(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID       = "session-quote-visible-override"
		senderX         = int64(9221)
		memberA         = int64(9222)
		memberB         = int64(9223)
		quotedMessageID = int64(9222990001)
	)

	now := time.Now().UTC()
	users := []model.User{
		{ID: senderX, Username: "sender_x", Email: "x@test.com", Nickname: "X"},
		{ID: memberA, Username: "member_a", Email: "a@test.com", Nickname: "A"},
		{ID: memberB, Username: "member_b", Email: "b@test.com", Nickname: "B"},
	}
	for _, u := range users {
		if err := store.DB.Create(&u).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     senderX,
		SessionType: 2,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, m := range []model.SessionMember{
		{SessionID: sessionID, MemberID: senderX, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: memberA, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: memberB, MemberType: 1, JoinedAt: now, LastActiveAt: now},
	} {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	// X sends a message with visible_to: [A, B]
	visibleToJSON, _ := json.Marshal([]int64{memberA, memberB})
	if err := store.DB.Create(&model.Message{
		MsgID:      quotedMessageID,
		SessionID:  sessionID,
		SenderID:   senderX,
		SenderType: 1,
		MsgType:    1,
		Content:    "仅AB可见",
		VisibleTo:  datatypes.JSON(visibleToJSON),
		CreatedAt:  now.Add(-time.Second),
	}).Error; err != nil {
		t.Fatalf("create quoted message error: %v", err)
	}

	// A replies quoting the hidden message AND explicitly hidden-sends to B.
	// An explicit visible_to designation has the highest priority: the server
	// must NOT force it back to [senderX]; the quoted owner X is neither
	// treated as @mentioned nor able to receive the reply.
	replyConn := &sendMsgMockConn{userID: memberA, deviceID: "dev-a"}
	senderXConn := &sendMsgMockConn{userID: senderX, deviceID: "dev-x"}
	memberBConn := &sendMsgMockConn{userID: memberB, deviceID: "dev-b"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			memberA: {replyConn},
			senderX: {senderXConn},
			memberB: {memberBConn},
		},
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:       sessionID,
		ClientMsgID:     "cmsg-quote-visible-override",
		MsgType:         1,
		Content:         "A的回复",
		QuotedMessageID: quotedMessageID,
		VisibleTo:       []int64{memberB}, // client explicitly hidden-sends to [B]
	})

	HandleSendMsg(hub, replyConn, pkt)

	ack, ok := findSendAck(replyConn.sent)
	if !ok {
		t.Fatalf("sender should receive SendAck, got=%#v", replyConn.sent)
	}

	var msg model.Message
	if err := store.DB.Where("session_id = ? AND msg_id = ?", sessionID, ack.MsgID).
		First(&msg).Error; err != nil {
		t.Fatalf("load reply message error: %v", err)
	}

	// visible_to must stay [memberB] as the client explicitly designated.
	if msg.VisibleTo == nil {
		t.Fatal("reply message should have visible_to set, got nil")
	}
	var gotVisibleTo []int64
	if err := json.Unmarshal(msg.VisibleTo, &gotVisibleTo); err != nil {
		t.Fatalf("unmarshal visible_to error: %v", err)
	}
	if len(gotVisibleTo) != 1 || gotVisibleTo[0] != memberB {
		t.Fatalf("visible_to should stay [%d], got=%v", memberB, gotVisibleTo)
	}

	// The quoted owner X must not be treated as @mentioned: mentions are
	// restricted to the explicit visible_to targets only.
	var extra map[string]any
	if err := json.Unmarshal(msg.Extra, &extra); err != nil {
		t.Fatalf("unmarshal message extra error: %v", err)
	}
	rawMentions, ok := extra["mention_user_ids"].([]any)
	if !ok || len(rawMentions) != 1 || mustParseInt64(rawMentions[0].(string)) != memberB {
		t.Fatalf("mention_user_ids should be [%d], got=%#v", memberB, extra["mention_user_ids"])
	}

	// X must not receive the reply; B must.
	if _, ok := findPushMsg(senderXConn.sent); ok {
		t.Fatal("quoted owner X should not receive push_msg for an explicit hidden send to B")
	}
	if _, ok := findPushMsg(memberBConn.sent); !ok {
		t.Fatal("memberB should receive push_msg for the hidden send")
	}
}

func TestHandleSendMsgQuoteHiddenMessageDeniedForInvisibleSender(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID       = "session-quote-visibility-denied"
		senderX         = int64(9231)
		memberA         = int64(9232)
		memberC         = int64(9233) // NOT in quoted message visible_to
		quotedMessageID = int64(9233990001)
	)

	now := time.Now().UTC()
	users := []model.User{
		{ID: senderX, Username: "sender_x", Email: "x@test.com", Nickname: "X"},
		{ID: memberA, Username: "member_a", Email: "a@test.com", Nickname: "A"},
		{ID: memberC, Username: "member_c", Email: "c@test.com", Nickname: "C"},
	}
	for _, u := range users {
		if err := store.DB.Create(&u).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     senderX,
		SessionType: 2,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, m := range []model.SessionMember{
		{SessionID: sessionID, MemberID: senderX, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: memberA, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: memberC, MemberType: 1, JoinedAt: now, LastActiveAt: now},
	} {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	// X sends a hidden message visible only to A.
	visibleToJSON, _ := json.Marshal([]int64{memberA})
	if err := store.DB.Create(&model.Message{
		MsgID:      quotedMessageID,
		SessionID:  sessionID,
		SenderID:   senderX,
		SenderType: 1,
		MsgType:    1,
		Content:    "仅A可见",
		VisibleTo:  datatypes.JSON(visibleToJSON),
		CreatedAt:  now.Add(-time.Second),
	}).Error; err != nil {
		t.Fatalf("create quoted message error: %v", err)
	}

	// C cannot see that message; quoting it must be rejected.
	cConn := &sendMsgMockConn{userID: memberC, deviceID: "dev-c"}
	senderXConn := &sendMsgMockConn{userID: senderX, deviceID: "dev-x"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			memberC: {cConn},
			senderX: {senderXConn},
		},
	}

	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:       sessionID,
		ClientMsgID:     "cmsg-quote-visibility-denied",
		MsgType:         1,
		Content:         "C引用不可见消息",
		QuotedMessageID: quotedMessageID,
	})

	HandleSendMsg(hub, cConn, pkt)

	if len(cConn.sent) != 1 {
		t.Fatalf("sender sent count=%d want=1", len(cConn.sent))
	}
	if cConn.sent[0].cmd != protocol.CmdSendNack {
		t.Fatalf("sender first cmd=%s want=%s", cConn.sent[0].cmd, protocol.CmdSendNack)
	}
	nack, ok := cConn.sent[0].payload.(protocol.SendNackPayload)
	if !ok {
		t.Fatalf("sender first payload type=%T want=%T", cConn.sent[0].payload, protocol.SendNackPayload{})
	}
	if nack.Code != 4003 {
		t.Fatalf("nack code=%d want=4003", nack.Code)
	}
	if len(senderXConn.sent) != 0 {
		t.Fatalf("quoted owner should not receive events, got=%d", len(senderXConn.sent))
	}

	var count int64
	if err := store.DB.Model(&model.Message{}).
		Where("session_id = ? AND msg_id != ?", sessionID, quotedMessageID).
		Count(&count).Error; err != nil {
		t.Fatalf("count messages error: %v", err)
	}
	if count != 0 {
		t.Fatalf("rejected quote should not persist a message, got=%d", count)
	}
}

func TestHandleSendMsgVisibleToUsersBecomeMentionTargets(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-visible-to-mention"
		senderX   = int64(9301)
		memberA   = int64(9302)
		memberB   = int64(9303)
		memberC   = int64(9304) // NOT in visible_to
	)

	now := time.Now().UTC()
	users := []model.User{
		{ID: senderX, Username: "sender_x", Email: "x@test.com", Nickname: "X"},
		{ID: memberA, Username: "member_a", Email: "a@test.com", Nickname: "A"},
		{ID: memberB, Username: "member_b", Email: "b@test.com", Nickname: "B"},
		{ID: memberC, Username: "member_c", Email: "c@test.com", Nickname: "C"},
	}
	for _, u := range users {
		if err := store.DB.Create(&u).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     senderX,
		SessionType: 2,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, m := range []model.SessionMember{
		{SessionID: sessionID, MemberID: senderX, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: memberA, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: memberB, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: memberC, MemberType: 1, JoinedAt: now, LastActiveAt: now},
	} {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	senderConn := &sendMsgMockConn{userID: senderX, deviceID: "dev-x"}
	memberAConn := &sendMsgMockConn{userID: memberA, deviceID: "dev-a"}
	memberBConn := &sendMsgMockConn{userID: memberB, deviceID: "dev-b"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderX: {senderConn},
			memberA: {memberAConn},
			memberB: {memberBConn},
		},
	}

	// X sends a message with visible_to: [A, B] and no explicit @mention
	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-visible-to-mention",
		MsgType:     1,
		Content:     "仅AB可见",
		VisibleTo:   []int64{memberA, memberB},
	})

	HandleSendMsg(hub, senderConn, pkt)

	ack, ok := findSendAck(senderConn.sent)
	if !ok {
		t.Fatalf("sender should receive SendAck, got=%#v", senderConn.sent)
	}

	var msg model.Message
	if err := store.DB.Where("session_id = ? AND msg_id = ?", sessionID, ack.MsgID).
		First(&msg).Error; err != nil {
		t.Fatalf("load message error: %v", err)
	}

	// mention_user_ids in extra should contain A and B
	var extra map[string]any
	if err := json.Unmarshal(msg.Extra, &extra); err != nil {
		t.Fatalf("unmarshal message extra error: %v", err)
	}
	rawMentions, ok := extra["mention_user_ids"].([]any)
	if !ok || len(rawMentions) != 2 {
		t.Fatalf("mention_user_ids should have 2 entries, got=%#v", extra["mention_user_ids"])
	}
	gotMentions := make(map[int64]bool)
	for _, v := range rawMentions {
		gotMentions[mustParseInt64(v.(string))] = true
	}
	if !gotMentions[memberA] || !gotMentions[memberB] {
		t.Fatalf("mention_user_ids should contain %d and %d, got=%v", memberA, memberB, gotMentions)
	}

	// explicit_mention_user_ids should also contain A and B, so mention-only
	// receive mode can be triggered by visible_to hidden sends.
	rawExplicitMentions, ok := extra["explicit_mention_user_ids"].([]any)
	if !ok || len(rawExplicitMentions) != 2 {
		t.Fatalf(
			"explicit_mention_user_ids should have 2 entries, got=%#v",
			extra["explicit_mention_user_ids"],
		)
	}
	gotExplicitMentions := make(map[int64]bool)
	for _, v := range rawExplicitMentions {
		gotExplicitMentions[mustParseInt64(v.(string))] = true
	}
	if !gotExplicitMentions[memberA] || !gotExplicitMentions[memberB] {
		t.Fatalf(
			"explicit_mention_user_ids should contain %d and %d, got=%v",
			memberA,
			memberB,
			gotExplicitMentions,
		)
	}
}

func mustParseInt64(s string) int64 {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		panic(fmt.Sprintf("parse int64 %q: %v", s, err))
	}
	return v
}

// TestHandleSendMsgRejectsPrivateAgentOwnershipMismatch 锁定归属 guard(改用预加载 directAgents 后)
// 的拒绝路径:私聊中 agent 的 owner 已不再是发送者 → 拒绝(4003)、不落库。
func TestHandleSendMsgRejectsPrivateAgentOwnershipMismatch(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID  = "session-agent-owner-mismatch"
		senderID   = int64(7001)
		agentID    = int64(7002)
		otherOwner = int64(7999)
	)
	if err := store.DB.Create(&model.Session{SessionID: sessionID, OwnerID: senderID, SessionType: 1}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{SessionID: sessionID, MemberID: senderID, MemberType: 1}).Error; err != nil {
		t.Fatalf("create human member: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{SessionID: sessionID, MemberID: agentID, MemberType: 2}).Error; err != nil {
		t.Fatalf("create agent member: %v", err)
	}
	if err := store.DB.Create(&model.Agent{ID: agentID, OwnerID: otherOwner, Status: 1, ProviderType: model.AgentProviderAPI, AgentName: "x"}).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	conn := &sendMsgMockConn{userID: senderID, deviceID: "dev-x"}
	hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{senderID: {conn}}}
	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{SessionID: sessionID, ClientMsgID: "cm-own", MsgType: 1, Content: "hello"})

	HandleSendMsg(hub, conn, pkt)

	var msgCount int64
	store.DB.Model(&model.Message{}).Count(&msgCount)
	if msgCount != 0 {
		t.Fatalf("ownership mismatch should not create message, got=%d", msgCount)
	}
	if len(conn.sent) != 1 || conn.sent[0].cmd != protocol.CmdSendNack {
		t.Fatalf("expected one send_nack, got=%#v", conn.sent)
	}
	if nack, ok := conn.sent[0].payload.(protocol.SendNackPayload); !ok || nack.Code != 4003 {
		t.Fatalf("expected send_nack code 4003, got=%#v", conn.sent[0].payload)
	}
}

// TestHandleSendMsgVisibleToRestrictsMentionList verifies that when a group
// message has visible_to=[A] and explicitly @mentions B (outside visible_to),
// B does NOT receive the push_msg and the stored mention_user_ids only contains A.
func TestHandleSendMsgVisibleToRestrictsMentionList(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-visible-mention-restrict"
		senderID  = int64(8801)
		memberA   = int64(8802) // in visible_to
		memberB   = int64(8803) // @mentioned but NOT in visible_to
	)

	now := time.Now().UTC()
	users := []model.User{
		{ID: senderID, Username: "sender_vtr", Email: "sender_vtr@test.com", Nickname: "Sender"},
		{ID: memberA, Username: "member_a_vtr", Email: "a_vtr@test.com", Nickname: "A"},
		{ID: memberB, Username: "member_b_vtr", Email: "b_vtr@test.com", Nickname: "B"},
	}
	for _, u := range users {
		if err := store.DB.Create(&u).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}
	if err := store.DB.Create(&model.Session{
		SessionID: sessionID, OwnerID: senderID, SessionType: 2,
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, m := range []model.SessionMember{
		{SessionID: sessionID, MemberID: senderID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: memberA, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: memberB, MemberType: 1, JoinedAt: now, LastActiveAt: now},
	} {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create member error: %v", err)
		}
	}

	senderConn := &sendMsgMockConn{userID: senderID, deviceID: "dev-sender-vtr"}
	connA := &sendMsgMockConn{userID: memberA, deviceID: "dev-a-vtr"}
	connB := &sendMsgMockConn{userID: memberB, deviceID: "dev-b-vtr"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			senderID: {senderConn},
			memberA:  {connA},
			memberB:  {connB},
		},
	}

	// Send with visible_to=[A] and @B in the mention list
	mentionExtra, _ := json.Marshal(map[string]any{
		"mention_user_ids": []string{fmt.Sprintf("%d", memberB)},
	})
	pkt := makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "cmsg-visible-mention-restrict",
		MsgType:     1,
		Content:     fmt.Sprintf("只给A看 @%d", memberB),
		VisibleTo:   []int64{memberA},
		Extra:       mentionExtra,
	})

	HandleSendMsg(hub, senderConn, pkt)

	// Sender gets send_ack
	if countSentCmd(senderConn.sent, protocol.CmdSendAck) != 1 {
		t.Fatalf("sender should get send_ack, got=%#v", senderConn.sent)
	}

	// A gets push_msg (in visible_to)
	if countSentCmd(connA.sent, protocol.CmdPushMsg) != 1 {
		t.Fatalf("member A should receive push_msg, got=%#v", connA.sent)
	}

	// B must NOT receive push_msg (outside visible_to, even though @mentioned)
	if countSentCmd(connB.sent, protocol.CmdPushMsg) != 0 {
		t.Fatalf("member B should NOT receive push_msg (outside visible_to), got=%#v", connB.sent)
	}

	// Stored message: mention_user_ids must only contain A, not B
	ack, ok := findSendAck(senderConn.sent)
	if !ok {
		t.Fatal("could not find send_ack from sender")
	}
	var msg model.Message
	if err := store.DB.Where("session_id = ? AND msg_id = ?", sessionID, ack.MsgID).First(&msg).Error; err != nil {
		t.Fatalf("load message error: %v", err)
	}
	var extra map[string]any
	if err := json.Unmarshal(msg.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra error: %v", err)
	}
	rawMentions, ok := extra["mention_user_ids"].([]any)
	if !ok || len(rawMentions) != 1 {
		t.Fatalf("mention_user_ids should have exactly 1 entry (only A), got=%#v", extra["mention_user_ids"])
	}
	gotID := parseIntStr(t, rawMentions[0].(string))
	if gotID != memberA {
		t.Fatalf("mention_user_ids[0]=%d want=%d (A only, B must be excluded)", gotID, memberA)
	}
}
