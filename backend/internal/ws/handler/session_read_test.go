package handler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func makeSessionReadPacket(
	t *testing.T,
	sessionID string,
	lastReadMsgID int64,
) *protocol.Packet {
	t.Helper()
	raw, err := json.Marshal(protocol.SessionReadPayload{
		SessionID:     sessionID,
		LastReadMsgID: lastReadMsgID,
	})
	if err != nil {
		t.Fatalf("marshal session_read payload error: %v", err)
	}
	return &protocol.Packet{
		Cmd:     protocol.CmdSessionRead,
		Seq:     66,
		Payload: raw,
	}
}

func seedSessionReadMessage(
	t *testing.T,
	sessionID string,
	msgID int64,
	senderID int64,
) {
	t.Helper()
	if err := store.DB.Create(&model.Message{
		MsgID:      msgID,
		SessionID:  sessionID,
		SenderID:   senderID,
		SenderType: 1,
		MsgType:    1,
		Content:    "msg",
		CreatedAt:  time.UnixMilli(msgID).UTC(),
	}).Error; err != nil {
		t.Fatalf("create message error: %v", err)
	}
}

func TestHandleSessionReadClearsUnreadForMember(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-read-1"
	userID := int64(6001)
	peerID := int64(6002)
	lastMsgID := int64(99001)
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     userID,
		SessionType: 1,
		LastMsgID:   &lastMsgID,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{
		SessionID:   sessionID,
		MemberID:    userID,
		MemberType:  1,
		UnreadCount: 9,
	}).Error; err != nil {
		t.Fatalf("create session member error: %v", err)
	}
	if err := store.RDB.HSet(context.Background(), "im:unread:6001", sessionID, 9).Err(); err != nil {
		t.Fatalf("seed redis unread error: %v", err)
	}
	seedSessionReadMessage(t, sessionID, lastMsgID, peerID)

	conn := &sendMsgMockConn{userID: userID, deviceID: "dev-read"}
	HandleSessionRead(nil, conn, makeSessionReadPacket(t, sessionID, lastMsgID))
	if len(conn.sent) != 1 {
		t.Fatalf("expected one session_read_ack, got=%d", len(conn.sent))
	}
	if conn.sent[0].cmd != protocol.CmdSessionReadAck {
		t.Fatalf("expected cmd=%s, got=%s", protocol.CmdSessionReadAck, conn.sent[0].cmd)
	}
	ack, ok := conn.sent[0].payload.(protocol.SessionReadAckPayload)
	if !ok {
		t.Fatalf("expected SessionReadAckPayload, got=%T", conn.sent[0].payload)
	}
	if ack.Code != 0 {
		t.Fatalf("expected code=0, got=%d", ack.Code)
	}
	if ack.LastReadMsgID != lastMsgID {
		t.Fatalf("expected ack last_read_msg_id=%d, got=%d", lastMsgID, ack.LastReadMsgID)
	}

	var member model.SessionMember
	if err := store.DB.Where("session_id = ? AND member_id = ?", sessionID, userID).First(&member).Error; err != nil {
		t.Fatalf("query session member error: %v", err)
	}
	if member.UnreadCount != 0 {
		t.Fatalf("expected unread_count=0, got=%d", member.UnreadCount)
	}
	if member.LastReadMsgID != lastMsgID {
		t.Fatalf("expected last_read_msg_id=%d, got=%d", lastMsgID, member.LastReadMsgID)
	}

	exists, err := store.RDB.HExists(context.Background(), "im:unread:6001", sessionID).Result()
	if err != nil {
		t.Fatalf("HExists unread error: %v", err)
	}
	if exists {
		t.Fatalf("expected unread hash field removed")
	}
}

func TestHandleSessionReadBroadcastsGroupReadSync(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-read-group-sync"
	readerID := int64(6051)
	peerID := int64(6052)
	lastMsgID := int64(99123)
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     readerID,
		SessionType: 2,
		LastMsgID:   &lastMsgID,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: readerID, MemberType: 1, UnreadCount: 3},
		{SessionID: sessionID, MemberID: peerID, MemberType: 1},
	}
	for _, member := range members {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	seedSessionReadMessage(t, sessionID, lastMsgID, peerID)

	readerConn := &sendMsgMockConn{userID: readerID, deviceID: "dev-reader"}
	peerConn := &sendMsgMockConn{userID: peerID, deviceID: "dev-peer"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			readerID: {readerConn},
			peerID:   {peerConn},
		},
	}

	HandleSessionRead(hub, readerConn, makeSessionReadPacket(t, sessionID, lastMsgID))

	if len(readerConn.sent) != 2 {
		t.Fatalf("reader should receive session_read_sync and session_read_ack, got=%d", len(readerConn.sent))
	}
	if readerConn.sent[0].cmd != protocol.CmdSessionReadSync {
		t.Fatalf("reader first cmd=%s want=%s", readerConn.sent[0].cmd, protocol.CmdSessionReadSync)
	}
	readerSync, ok := readerConn.sent[0].payload.(protocol.SessionReadSyncPayload)
	if !ok {
		t.Fatalf("reader first payload should be SessionReadSyncPayload, got=%T", readerConn.sent[0].payload)
	}
	if readerSync.ReaderID != readerID || readerSync.LastReadMsgID != lastMsgID {
		t.Fatalf("reader sync mismatch: %+v", readerSync)
	}
	if readerConn.sent[1].cmd != protocol.CmdSessionReadAck {
		t.Fatalf("reader second cmd=%s want=%s", readerConn.sent[1].cmd, protocol.CmdSessionReadAck)
	}

	if len(peerConn.sent) != 1 {
		t.Fatalf("peer should receive one session_read_sync, got=%d", len(peerConn.sent))
	}
	if peerConn.sent[0].cmd != protocol.CmdSessionReadSync {
		t.Fatalf("peer cmd=%s want=%s", peerConn.sent[0].cmd, protocol.CmdSessionReadSync)
	}
	peerSync, ok := peerConn.sent[0].payload.(protocol.SessionReadSyncPayload)
	if !ok {
		t.Fatalf("peer payload should be SessionReadSyncPayload, got=%T", peerConn.sent[0].payload)
	}
	if peerSync.ReaderID != readerID || peerSync.LastReadMsgID != lastMsgID {
		t.Fatalf("peer sync mismatch: %+v", peerSync)
	}
}

func TestHandleSessionReadIgnoresNonMember(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-read-2"
	memberID := int64(6101)
	otherID := int64(6102)
	if err := store.DB.Create(&model.SessionMember{
		SessionID:   sessionID,
		MemberID:    memberID,
		MemberType:  1,
		UnreadCount: 7,
	}).Error; err != nil {
		t.Fatalf("create session member error: %v", err)
	}
	if err := store.RDB.HSet(context.Background(), "im:unread:6101", sessionID, 7).Err(); err != nil {
		t.Fatalf("seed redis unread error: %v", err)
	}

	conn := &sendMsgMockConn{userID: otherID, deviceID: "dev-other"}
	HandleSessionRead(nil, conn, makeSessionReadPacket(t, sessionID, 7))
	if len(conn.sent) != 1 {
		t.Fatalf("expected one session_read_ack, got=%d", len(conn.sent))
	}
	if conn.sent[0].cmd != protocol.CmdSessionReadAck {
		t.Fatalf("expected cmd=%s, got=%s", protocol.CmdSessionReadAck, conn.sent[0].cmd)
	}
	ack, ok := conn.sent[0].payload.(protocol.SessionReadAckPayload)
	if !ok {
		t.Fatalf("expected SessionReadAckPayload, got=%T", conn.sent[0].payload)
	}
	if ack.Code != 4003 {
		t.Fatalf("expected code=4003, got=%d", ack.Code)
	}

	var member model.SessionMember
	if err := store.DB.Where("session_id = ? AND member_id = ?", sessionID, memberID).First(&member).Error; err != nil {
		t.Fatalf("query session member error: %v", err)
	}
	if member.UnreadCount != 7 {
		t.Fatalf("non-member call should not change unread_count, got=%d", member.UnreadCount)
	}
	val, err := store.RDB.HGet(context.Background(), "im:unread:6101", sessionID).Result()
	if err != nil {
		t.Fatalf("HGet unread error: %v", err)
	}
	if val != "7" {
		t.Fatalf("non-member call should not clear redis unread, got=%s", val)
	}
}

func TestHandleSessionReadInvalidSessionIDReturnsAck(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	conn := &sendMsgMockConn{userID: 6201, deviceID: "dev-invalid"}
	HandleSessionRead(nil, conn, makeSessionReadPacket(t, "", 0))

	if len(conn.sent) != 1 {
		t.Fatalf("expected one session_read_ack, got=%d", len(conn.sent))
	}
	if conn.sent[0].cmd != protocol.CmdSessionReadAck {
		t.Fatalf("expected cmd=%s, got=%s", protocol.CmdSessionReadAck, conn.sent[0].cmd)
	}
	ack, ok := conn.sent[0].payload.(protocol.SessionReadAckPayload)
	if !ok {
		t.Fatalf("expected SessionReadAckPayload, got=%T", conn.sent[0].payload)
	}
	if ack.Code != 4001 {
		t.Fatalf("expected code=4001, got=%d", ack.Code)
	}
}

func TestHandleSessionReadRepeatedCallsStillAckSuccess(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-read-repeat"
	userID := int64(6301)
	peerID := int64(6302)
	lastMsgID := int64(10011)
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     userID,
		SessionType: 1,
		LastMsgID:   &lastMsgID,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{
		SessionID:   sessionID,
		MemberID:    userID,
		MemberType:  1,
		UnreadCount: 2,
	}).Error; err != nil {
		t.Fatalf("create session member error: %v", err)
	}
	seedSessionReadMessage(t, sessionID, lastMsgID, peerID)

	conn := &sendMsgMockConn{userID: userID, deviceID: "dev-repeat"}
	pkt := makeSessionReadPacket(t, sessionID, lastMsgID)
	HandleSessionRead(nil, conn, pkt)
	HandleSessionRead(nil, conn, pkt)

	if len(conn.sent) != 2 {
		t.Fatalf("expected two session_read_ack payloads, got=%d", len(conn.sent))
	}
	for i, sent := range conn.sent {
		if sent.cmd != protocol.CmdSessionReadAck {
			t.Fatalf("ack[%d] expected cmd=%s, got=%s", i, protocol.CmdSessionReadAck, sent.cmd)
		}
		ack, ok := sent.payload.(protocol.SessionReadAckPayload)
		if !ok {
			t.Fatalf("ack[%d] expected SessionReadAckPayload, got=%T", i, sent.payload)
		}
		if ack.Code != 0 {
			t.Fatalf("ack[%d] expected code=0, got=%d", i, ack.Code)
		}
		if ack.LastReadMsgID != lastMsgID {
			t.Fatalf("ack[%d] expected last_read_msg_id=%d, got=%d", i, lastMsgID, ack.LastReadMsgID)
		}
	}
}

func TestHandleSessionReadRepeatedAfterNewMessageUpdatesCursor(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-read-repeat-new-msg"
	userID := int64(6351)
	peerID := int64(6352)
	firstMsgID := int64(1001)
	secondMsgID := int64(1002)
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     userID,
		SessionType: 1,
		LastMsgID:   &firstMsgID,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{
		SessionID:   sessionID,
		MemberID:    userID,
		MemberType:  1,
		UnreadCount: 2,
	}).Error; err != nil {
		t.Fatalf("create session member error: %v", err)
	}
	seedSessionReadMessage(t, sessionID, firstMsgID, peerID)

	conn := &sendMsgMockConn{userID: userID, deviceID: "dev-repeat-new"}
	HandleSessionRead(nil, conn, makeSessionReadPacket(t, sessionID, firstMsgID))

	if err := store.DB.Model(&model.Session{}).
		Where("session_id = ?", sessionID).
		Update("last_msg_id", secondMsgID).Error; err != nil {
		t.Fatalf("update session last_msg_id error: %v", err)
	}
	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, userID).
		Update("unread_count", 1).Error; err != nil {
		t.Fatalf("update unread_count error: %v", err)
	}
	seedSessionReadMessage(t, sessionID, secondMsgID, peerID)

	HandleSessionRead(nil, conn, makeSessionReadPacket(t, sessionID, secondMsgID))

	var member model.SessionMember
	if err := store.DB.Where("session_id = ? AND member_id = ?", sessionID, userID).First(&member).Error; err != nil {
		t.Fatalf("query session member error: %v", err)
	}
	if member.LastReadMsgID != secondMsgID {
		t.Fatalf("expected last_read_msg_id=%d, got=%d", secondMsgID, member.LastReadMsgID)
	}
	if member.UnreadCount != 0 {
		t.Fatalf("expected unread_count=0, got=%d", member.UnreadCount)
	}
}

func TestHandleSessionReadKeepsUnreadBeyondRequestedBoundary(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-read-stale-boundary"
	userID := int64(6381)
	peerID := int64(6382)
	firstUnreadMsgID := int64(2001)
	secondUnreadMsgID := int64(2002)
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     userID,
		SessionType: 1,
		LastMsgID:   &secondUnreadMsgID,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{
		SessionID:     sessionID,
		MemberID:      userID,
		MemberType:    1,
		UnreadCount:   2,
		LastReadMsgID: 2000,
	}).Error; err != nil {
		t.Fatalf("create session member error: %v", err)
	}
	seedSessionReadMessage(t, sessionID, firstUnreadMsgID, peerID)
	seedSessionReadMessage(t, sessionID, secondUnreadMsgID, peerID)
	if err := store.RDB.HSet(context.Background(), "im:unread:6381", sessionID, 2).Err(); err != nil {
		t.Fatalf("seed redis unread error: %v", err)
	}

	conn := &sendMsgMockConn{userID: userID, deviceID: "dev-stale-boundary"}
	HandleSessionRead(nil, conn, makeSessionReadPacket(t, sessionID, firstUnreadMsgID))

	if len(conn.sent) != 1 {
		t.Fatalf("expected one session_read_ack, got=%d", len(conn.sent))
	}
	ack, ok := conn.sent[0].payload.(protocol.SessionReadAckPayload)
	if !ok {
		t.Fatalf("expected SessionReadAckPayload, got=%T", conn.sent[0].payload)
	}
	if ack.Code != 0 {
		t.Fatalf("expected code=0, got=%d", ack.Code)
	}
	if ack.LastReadMsgID != firstUnreadMsgID {
		t.Fatalf("expected ack last_read_msg_id=%d, got=%d", firstUnreadMsgID, ack.LastReadMsgID)
	}

	var member model.SessionMember
	if err := store.DB.Where("session_id = ? AND member_id = ?", sessionID, userID).First(&member).Error; err != nil {
		t.Fatalf("query session member error: %v", err)
	}
	if member.LastReadMsgID != firstUnreadMsgID {
		t.Fatalf("expected last_read_msg_id=%d, got=%d", firstUnreadMsgID, member.LastReadMsgID)
	}
	if member.UnreadCount != 1 {
		t.Fatalf("expected unread_count=1, got=%d", member.UnreadCount)
	}

	val, err := store.RDB.HGet(context.Background(), "im:unread:6381", sessionID).Result()
	if err != nil {
		t.Fatalf("HGet unread error: %v", err)
	}
	if val != "1" {
		t.Fatalf("expected redis unread=1, got=%s", val)
	}
}

func TestHandleSessionReadClampsFutureBoundaryToExistingMessage(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-read-clamp-existing-boundary"
	userID := int64(6391)
	peerID := int64(6392)
	existingMsgID := int64(3001)
	requestedFutureMsgID := existingMsgID + 999999
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     userID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{
		SessionID:   sessionID,
		MemberID:    userID,
		MemberType:  1,
		UnreadCount: 1,
	}).Error; err != nil {
		t.Fatalf("create session member error: %v", err)
	}
	seedSessionReadMessage(t, sessionID, existingMsgID, peerID)
	if err := store.RDB.HSet(context.Background(), "im:unread:6391", sessionID, 1).Err(); err != nil {
		t.Fatalf("seed redis unread error: %v", err)
	}

	conn := &sendMsgMockConn{userID: userID, deviceID: "dev-clamp-existing-boundary"}
	HandleSessionRead(nil, conn, makeSessionReadPacket(t, sessionID, requestedFutureMsgID))

	if len(conn.sent) != 1 {
		t.Fatalf("expected one session_read_ack, got=%d", len(conn.sent))
	}
	ack, ok := conn.sent[0].payload.(protocol.SessionReadAckPayload)
	if !ok {
		t.Fatalf("expected SessionReadAckPayload, got=%T", conn.sent[0].payload)
	}
	if ack.Code != 0 {
		t.Fatalf("expected code=0, got=%d", ack.Code)
	}
	if ack.LastReadMsgID != existingMsgID {
		t.Fatalf("expected ack last_read_msg_id=%d, got=%d", existingMsgID, ack.LastReadMsgID)
	}

	var member model.SessionMember
	if err := store.DB.Where("session_id = ? AND member_id = ?", sessionID, userID).First(&member).Error; err != nil {
		t.Fatalf("query session member error: %v", err)
	}
	if member.LastReadMsgID != existingMsgID {
		t.Fatalf("expected last_read_msg_id=%d, got=%d", existingMsgID, member.LastReadMsgID)
	}
	if member.UnreadCount != 0 {
		t.Fatalf("expected unread_count=0, got=%d", member.UnreadCount)
	}

	exists, err := store.RDB.HExists(context.Background(), "im:unread:6391", sessionID).Result()
	if err != nil {
		t.Fatalf("HExists unread error: %v", err)
	}
	if exists {
		t.Fatalf("expected unread hash field removed")
	}
}

func TestHandleSessionReadInvalidPayloadReturnsAck(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	conn := &sendMsgMockConn{userID: 6401, deviceID: "dev-invalid-payload"}
	pkt := &protocol.Packet{
		Cmd:     protocol.CmdSessionRead,
		Seq:     67,
		Payload: []byte(`{"session_id":`),
	}
	HandleSessionRead(nil, conn, pkt)

	if len(conn.sent) != 1 {
		t.Fatalf("expected one session_read_ack, got=%d", len(conn.sent))
	}
	if conn.sent[0].cmd != protocol.CmdSessionReadAck {
		t.Fatalf("expected cmd=%s, got=%s", protocol.CmdSessionReadAck, conn.sent[0].cmd)
	}
	ack, ok := conn.sent[0].payload.(protocol.SessionReadAckPayload)
	if !ok {
		t.Fatalf("expected SessionReadAckPayload, got=%T", conn.sent[0].payload)
	}
	if ack.Code != 4001 {
		t.Fatalf("expected code=4001, got=%d", ack.Code)
	}
}
