package e2e

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	pkgagentapi "github.com/askie/grix/backend/internal/pkg/agentapi"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWS_AuthAndMessage(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()

	// 1. Start WS Server on an isolated port/node to avoid cross-test collisions.
	server, wsPort := startTestWSServer(t, "node-auth-message")
	defer server.Shutdown()

	// 2. Prepare users and tokens
	tokenA, _ := ctx.loginHelper(t, "ws_user_a", "Password123", "device_a_1")

	// 3. Connect as User A
	dialer := websocket.DefaultDialer
	wsURL := fmt.Sprintf("ws://localhost:%d/ws", wsPort)

	reqHeader := http.Header{}
	connA, respA, err := dialer.Dial(wsURL, reqHeader)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, respA.StatusCode)
	defer connA.Close()

	// 4. Send Auth Packet
	authReq := map[string]interface{}{
		"cmd": "auth",
		"seq": int64(1),
		"payload": map[string]interface{}{
			"token":     tokenA,
			"device_id": "device_a_1",
			"platform":  "test",
		},
	}
	err = connA.WriteJSON(authReq)
	require.NoError(t, err)

	// Read Auth Ack
	connA.SetReadDeadline(time.Now().Add(2 * time.Second))
	var authAck map[string]interface{}
	err = connA.ReadJSON(&authAck)
	require.NoError(t, err)
	assert.Equal(t, "auth_ack", authAck["cmd"])

	payload, ok := authAck["payload"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(0), payload["code"]) // go json reads int as float64

	// 5. Send Ping
	pingReq := map[string]interface{}{
		"cmd":     "ping",
		"seq":     int64(2),
		"payload": map[string]interface{}{},
	}
	err = connA.WriteJSON(pingReq)
	require.NoError(t, err)

	// Read Pong
	var pongAck map[string]interface{}
	err = connA.ReadJSON(&pongAck)
	require.NoError(t, err)
	assert.Equal(t, "pong", pongAck["cmd"])

	// 6. Test Message Routing
	msgReq := map[string]interface{}{
		"cmd": "send_msg",
		"seq": int64(3),
		"payload": map[string]interface{}{
			"session_id":    "d95a4862-3f4c-49a8-b8b2-7992eb6f7e50", // fictional opaque session id
			"client_msg_id": "test_msg_uuid",
			"msg_type":      1,
			"content":       "Hello ws",
		},
	}
	err = connA.WriteJSON(msgReq)
	require.NoError(t, err)

	// Read send_ack or error (likely error if session not found, but it proves routing)
	var sendAck map[string]interface{}
	err = connA.ReadJSON(&sendAck)
	require.NoError(t, err)
	// Usually send_ack, but if validation fails, the handler might log and ignore, or send error.
	// Since there's no stream_error for generic message send failures in standard design,
	// let's just assert we got some response to seq: 3
	assert.Equal(t, float64(3), sendAck["seq"])
}

func TestWS_PushAckTimeoutRecoveredByPullSync(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()

	server, wsPort := startTestWSServer(t, "node-pullsync")
	defer server.Shutdown()

	tokenSender, senderID := ctx.loginHelper(t, "ws_pullsync_sender", "Password123", "device_pullsync_sender")
	tokenRecipient, recipientID := ctx.loginHelper(t, "ws_pullsync_recipient", "Password123", "device_pullsync_recipient")

	for _, friendship := range []model.Friend{
		{UserID: senderID, FriendID: recipientID},
		{UserID: recipientID, FriendID: senderID},
	} {
		err := store.DB.Create(&friendship).Error
		require.NoError(t, err)
	}

	w := ctx.doReq(t, "POST", "/v1/sessions/create", tokenSender, map[string]interface{}{
		"peer_id":   strconv.FormatInt(recipientID, 10),
		"peer_type": 1,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	createResp := parseResp(t, w)
	createData, ok := createResp["data"].(map[string]interface{})
	require.True(t, ok)
	sessionID, _ := createData["session_id"].(string)
	require.NotEmpty(t, sessionID)

	dialer := websocket.DefaultDialer
	wsURL := fmt.Sprintf("ws://localhost:%d/ws", wsPort)

	connSender, respSender, err := dialer.Dial(wsURL, http.Header{})
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, respSender.StatusCode)
	defer connSender.Close()

	connRecipient, respRecipient, err := dialer.Dial(wsURL, http.Header{})
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, respRecipient.StatusCode)
	defer connRecipient.Close()

	err = connSender.WriteJSON(map[string]interface{}{
		"cmd": "auth",
		"seq": int64(1),
		"payload": map[string]interface{}{
			"token":     tokenSender,
			"device_id": "device_pullsync_sender",
			"platform":  "test",
		},
	})
	require.NoError(t, err)
	authAckSender := readWSMessageByCmd(t, connSender, "auth_ack", 2*time.Second)
	authPayloadSender, ok := authAckSender["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, float64(0), authPayloadSender["code"])

	err = connRecipient.WriteJSON(map[string]interface{}{
		"cmd": "auth",
		"seq": int64(1),
		"payload": map[string]interface{}{
			"token":     tokenRecipient,
			"device_id": "device_pullsync_recipient",
			"platform":  "test",
		},
	})
	require.NoError(t, err)
	authAckRecipient := readWSMessageByCmd(t, connRecipient, "auth_ack", 2*time.Second)
	authPayloadRecipient, ok := authAckRecipient["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, float64(0), authPayloadRecipient["code"])

	err = connSender.WriteJSON(map[string]interface{}{
		"cmd": "send_msg",
		"seq": int64(2),
		"payload": map[string]interface{}{
			"session_id":    sessionID,
			"client_msg_id": "pullsync_timeout_msg",
			"msg_type":      1,
			"content":       "needs pull sync recovery",
		},
	})
	require.NoError(t, err)

	sendAck := readWSMessageByCmd(t, connSender, "send_ack", 2*time.Second)
	sendAckPayload, ok := sendAck["payload"].(map[string]interface{})
	require.True(t, ok)
	msgID, err := parseID(sendAckPayload["msg_id"])
	require.NoError(t, err)

	firstPush := readWSMessageByCmd(t, connRecipient, "push_msg", 2*time.Second)
	firstPushPayload, ok := firstPush["payload"].(map[string]interface{})
	require.True(t, ok)
	receivedMsgID, err := parseID(firstPushPayload["msg_id"])
	require.NoError(t, err)
	require.Equal(t, msgID, receivedMsgID)

	closed := false
	connRecipient.SetReadDeadline(time.Now().Add(24 * time.Second))
	for {
		var evt map[string]interface{}
		err := connRecipient.ReadJSON(&evt)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				break
			}
			closed = true
			break
		}
	}
	require.True(t, closed, "recipient connection should close after push_ack timeout retries")

	connRecipient.Close()

	connRecipient2, respRecipient2, err := dialer.Dial(wsURL, http.Header{})
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, respRecipient2.StatusCode)
	defer connRecipient2.Close()

	err = connRecipient2.WriteJSON(map[string]interface{}{
		"cmd": "auth",
		"seq": int64(1),
		"payload": map[string]interface{}{
			"token":     tokenRecipient,
			"device_id": "device_pullsync_recipient",
			"platform":  "test",
		},
	})
	require.NoError(t, err)
	authAckRecipient2 := readWSMessageByCmd(t, connRecipient2, "auth_ack", 2*time.Second)
	authPayloadRecipient2, ok := authAckRecipient2["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, float64(0), authPayloadRecipient2["code"])

	err = connRecipient2.WriteJSON(map[string]interface{}{
		"cmd": "pull_sync",
		"seq": int64(2),
		"payload": map[string]interface{}{
			"last_inbox_seq": "0",
		},
	})
	require.NoError(t, err)

	pullSyncResp := readWSMessageByCmd(t, connRecipient2, "pull_sync_resp", 2*time.Second)
	pullSyncPayload, ok := pullSyncResp["payload"].(map[string]interface{})
	require.True(t, ok)

	rawMessages, ok := pullSyncPayload["messages"].([]interface{})
	require.True(t, ok)
	require.Len(t, rawMessages, 1)

	row, ok := rawMessages[0].(map[string]interface{})
	require.True(t, ok)
	recoveredMsgID, err := parseID(row["msg_id"])
	require.NoError(t, err)
	require.Equal(t, msgID, recoveredMsgID)
	require.Equal(t, sessionID, row["session_id"])
	require.Equal(t, "needs pull sync recovery", row["content"])
}

func TestWS_MultiNodePushAndPullSyncRecovery(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()

	serverA, wsPortA := startTestWSServer(t, "node-multinode-a")
	serverB, wsPortB := startTestWSServer(t, "node-multinode-b")
	defer serverA.Shutdown()
	defer serverB.Shutdown()

	tokenSender, senderID := ctx.loginHelper(t, "ws_multinode_sender", "Password123", "device_multinode_sender")
	tokenRecipient, recipientID := ctx.loginHelper(t, "ws_multinode_recipient", "Password123", "device_multinode_recipient")

	for _, friendship := range []model.Friend{
		{UserID: senderID, FriendID: recipientID},
		{UserID: recipientID, FriendID: senderID},
	} {
		err := store.DB.Create(&friendship).Error
		require.NoError(t, err)
	}

	w := ctx.doReq(t, "POST", "/v1/sessions/create", tokenSender, map[string]interface{}{
		"peer_id":   strconv.FormatInt(recipientID, 10),
		"peer_type": 1,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	createResp := parseResp(t, w)
	createData, ok := createResp["data"].(map[string]interface{})
	require.True(t, ok)
	sessionID, _ := createData["session_id"].(string)
	require.NotEmpty(t, sessionID)

	dialer := websocket.DefaultDialer
	wsURLA := fmt.Sprintf("ws://localhost:%d/ws", wsPortA)
	wsURLB := fmt.Sprintf("ws://localhost:%d/ws", wsPortB)

	connSender, respSender, err := dialer.Dial(wsURLA, http.Header{})
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, respSender.StatusCode)
	defer connSender.Close()

	connRecipient, respRecipient, err := dialer.Dial(wsURLB, http.Header{})
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, respRecipient.StatusCode)
	defer connRecipient.Close()

	err = connSender.WriteJSON(map[string]interface{}{
		"cmd": "auth",
		"seq": int64(1),
		"payload": map[string]interface{}{
			"token":     tokenSender,
			"device_id": "device_multinode_sender",
			"platform":  "test",
		},
	})
	require.NoError(t, err)
	authAckSender := readWSMessageByCmd(t, connSender, "auth_ack", 2*time.Second)
	authPayloadSender, ok := authAckSender["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, float64(0), authPayloadSender["code"])

	err = connRecipient.WriteJSON(map[string]interface{}{
		"cmd": "auth",
		"seq": int64(1),
		"payload": map[string]interface{}{
			"token":     tokenRecipient,
			"device_id": "device_multinode_recipient",
			"platform":  "test",
		},
	})
	require.NoError(t, err)
	authAckRecipient := readWSMessageByCmd(t, connRecipient, "auth_ack", 2*time.Second)
	authPayloadRecipient, ok := authAckRecipient["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, float64(0), authPayloadRecipient["code"])

	err = connSender.WriteJSON(map[string]interface{}{
		"cmd": "send_msg",
		"seq": int64(2),
		"payload": map[string]interface{}{
			"session_id":    sessionID,
			"client_msg_id": "multinode_timeout_msg",
			"msg_type":      1,
			"content":       "cross node recovery",
		},
	})
	require.NoError(t, err)

	sendAck := readWSMessageByCmd(t, connSender, "send_ack", 2*time.Second)
	sendAckPayload, ok := sendAck["payload"].(map[string]interface{})
	require.True(t, ok)
	msgID, err := parseID(sendAckPayload["msg_id"])
	require.NoError(t, err)

	firstPush := readWSMessageByCmd(t, connRecipient, "push_msg", 2*time.Second)
	firstPushPayload, ok := firstPush["payload"].(map[string]interface{})
	require.True(t, ok)
	receivedMsgID, err := parseID(firstPushPayload["msg_id"])
	require.NoError(t, err)
	require.Equal(t, msgID, receivedMsgID)

	closed := false
	connRecipient.SetReadDeadline(time.Now().Add(24 * time.Second))
	for {
		var evt map[string]interface{}
		err := connRecipient.ReadJSON(&evt)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				break
			}
			closed = true
			break
		}
	}
	require.True(t, closed, "recipient connection on node-b should close after push_ack timeout retries")

	connRecipient.Close()

	connRecipient2, respRecipient2, err := dialer.Dial(wsURLA, http.Header{})
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, respRecipient2.StatusCode)
	defer connRecipient2.Close()

	err = connRecipient2.WriteJSON(map[string]interface{}{
		"cmd": "auth",
		"seq": int64(1),
		"payload": map[string]interface{}{
			"token":     tokenRecipient,
			"device_id": "device_multinode_recipient",
			"platform":  "test",
		},
	})
	require.NoError(t, err)
	authAckRecipient2 := readWSMessageByCmd(t, connRecipient2, "auth_ack", 2*time.Second)
	authPayloadRecipient2, ok := authAckRecipient2["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, float64(0), authPayloadRecipient2["code"])

	err = connRecipient2.WriteJSON(map[string]interface{}{
		"cmd": "pull_sync",
		"seq": int64(2),
		"payload": map[string]interface{}{
			"last_inbox_seq": "0",
		},
	})
	require.NoError(t, err)

	pullSyncResp := readWSMessageByCmd(t, connRecipient2, "pull_sync_resp", 2*time.Second)
	pullSyncPayload, ok := pullSyncResp["payload"].(map[string]interface{})
	require.True(t, ok)
	rawMessages, ok := pullSyncPayload["messages"].([]interface{})
	require.True(t, ok)
	require.Len(t, rawMessages, 1)

	row, ok := rawMessages[0].(map[string]interface{})
	require.True(t, ok)
	recoveredMsgID, err := parseID(row["msg_id"])
	require.NoError(t, err)
	require.Equal(t, msgID, recoveredMsgID)
	require.Equal(t, sessionID, row["session_id"])
	require.Equal(t, "cross node recovery", row["content"])
}

func TestWS_StaleSessionReadReplayDoesNotClearNewUnread(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()
	sqlDB, err := store.DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	server, wsPort := startTestWSServer(t, "node-read-boundary")
	defer server.Shutdown()

	tokenSender, senderID := ctx.loginHelper(t, "ws_read_boundary_sender", "Password123", "device_read_boundary_sender")
	tokenRecipient, recipientID := ctx.loginHelper(t, "ws_read_boundary_recipient", "Password123", "device_read_boundary_recipient")

	for _, friendship := range []model.Friend{
		{UserID: senderID, FriendID: recipientID},
		{UserID: recipientID, FriendID: senderID},
	} {
		err = store.DB.Create(&friendship).Error
		require.NoError(t, err)
	}

	w := ctx.doReq(t, "POST", "/v1/sessions/create", tokenSender, map[string]interface{}{
		"peer_id":   strconv.FormatInt(recipientID, 10),
		"peer_type": 1,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	createResp := parseResp(t, w)
	createData, ok := createResp["data"].(map[string]interface{})
	require.True(t, ok)
	sessionID, _ := createData["session_id"].(string)
	require.NotEmpty(t, sessionID)

	dialer := websocket.DefaultDialer
	wsURL := fmt.Sprintf("ws://localhost:%d/ws", wsPort)

	connSender, respSender, err := dialer.Dial(wsURL, http.Header{})
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, respSender.StatusCode)
	defer connSender.Close()

	connRecipient, respRecipient, err := dialer.Dial(wsURL, http.Header{})
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, respRecipient.StatusCode)

	err = connSender.WriteJSON(map[string]interface{}{
		"cmd": "auth",
		"seq": int64(1),
		"payload": map[string]interface{}{
			"token":     tokenSender,
			"device_id": "device_read_boundary_sender",
			"platform":  "test",
		},
	})
	require.NoError(t, err)
	authAckSender := readWSMessageByCmd(t, connSender, "auth_ack", 2*time.Second)
	authPayloadSender, ok := authAckSender["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, float64(0), authPayloadSender["code"])

	err = connRecipient.WriteJSON(map[string]interface{}{
		"cmd": "auth",
		"seq": int64(1),
		"payload": map[string]interface{}{
			"token":     tokenRecipient,
			"device_id": "device_read_boundary_recipient",
			"platform":  "test",
		},
	})
	require.NoError(t, err)
	authAckRecipient := readWSMessageByCmd(t, connRecipient, "auth_ack", 2*time.Second)
	authPayloadRecipient, ok := authAckRecipient["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, float64(0), authPayloadRecipient["code"])

	err = connSender.WriteJSON(map[string]interface{}{
		"cmd": "send_msg",
		"seq": int64(2),
		"payload": map[string]interface{}{
			"session_id":    sessionID,
			"client_msg_id": "read_boundary_msg_1",
			"msg_type":      1,
			"content":       "first message",
		},
	})
	require.NoError(t, err)

	sendAck1 := readWSMessageByCmd(t, connSender, "send_ack", 2*time.Second)
	sendAckPayload1, ok := sendAck1["payload"].(map[string]interface{})
	require.True(t, ok)
	firstMsgID, err := parseID(sendAckPayload1["msg_id"])
	require.NoError(t, err)

	firstPush := readWSMessageByCmd(t, connRecipient, "push_msg", 2*time.Second)
	firstPushPayload, ok := firstPush["payload"].(map[string]interface{})
	require.True(t, ok)
	firstPushMsgID, err := parseID(firstPushPayload["msg_id"])
	require.NoError(t, err)
	require.Equal(t, firstMsgID, firstPushMsgID)
	firstInboxSeq, err := parseID(firstPushPayload["inbox_seq"])
	require.NoError(t, err)

	err = connRecipient.WriteJSON(map[string]interface{}{
		"cmd": "push_ack",
		"seq": int64(2),
		"payload": map[string]interface{}{
			"msg_id": strconv.FormatInt(firstMsgID, 10),
		},
	})
	require.NoError(t, err)

	err = connRecipient.WriteJSON(map[string]interface{}{
		"cmd": "session_read",
		"seq": int64(3),
		"payload": map[string]interface{}{
			"session_id":       sessionID,
			"last_read_msg_id": strconv.FormatInt(firstMsgID, 10),
		},
	})
	require.NoError(t, err)

	readAck1 := readWSMessageByCmd(t, connRecipient, "session_read_ack", 2*time.Second)
	readAckPayload1, ok := readAck1["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, float64(0), readAckPayload1["code"])
	ackedFirstMsgID, err := parseID(readAckPayload1["last_read_msg_id"])
	require.NoError(t, err)
	require.Equal(t, firstMsgID, ackedFirstMsgID)

	connRecipient.Close()

	err = connSender.WriteJSON(map[string]interface{}{
		"cmd": "send_msg",
		"seq": int64(4),
		"payload": map[string]interface{}{
			"session_id":    sessionID,
			"client_msg_id": "read_boundary_msg_2",
			"msg_type":      1,
			"content":       "second message",
		},
	})
	require.NoError(t, err)

	sendAck2 := readWSMessageByCmd(t, connSender, "send_ack", 2*time.Second)
	sendAckPayload2, ok := sendAck2["payload"].(map[string]interface{})
	require.True(t, ok)
	secondMsgID, err := parseID(sendAckPayload2["msg_id"])
	require.NoError(t, err)
	require.Greater(t, secondMsgID, firstMsgID)

	connRecipient2, respRecipient2, err := dialer.Dial(wsURL, http.Header{})
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, respRecipient2.StatusCode)
	defer connRecipient2.Close()

	err = connRecipient2.WriteJSON(map[string]interface{}{
		"cmd": "auth",
		"seq": int64(1),
		"payload": map[string]interface{}{
			"token":     tokenRecipient,
			"device_id": "device_read_boundary_recipient",
			"platform":  "test",
		},
	})
	require.NoError(t, err)
	authAckRecipient2 := readWSMessageByCmd(t, connRecipient2, "auth_ack", 2*time.Second)
	authPayloadRecipient2, ok := authAckRecipient2["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, float64(0), authPayloadRecipient2["code"])

	err = connRecipient2.WriteJSON(map[string]interface{}{
		"cmd": "session_read",
		"seq": int64(2),
		"payload": map[string]interface{}{
			"session_id":       sessionID,
			"last_read_msg_id": strconv.FormatInt(firstMsgID, 10),
		},
	})
	require.NoError(t, err)

	readAck2 := readWSMessageByCmd(t, connRecipient2, "session_read_ack", 2*time.Second)
	readAckPayload2, ok := readAck2["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, float64(0), readAckPayload2["code"])
	ackedReplayMsgID, err := parseID(readAckPayload2["last_read_msg_id"])
	require.NoError(t, err)
	require.Equal(t, firstMsgID, ackedReplayMsgID)

	var member model.SessionMember
	err = store.DB.Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, recipientID).
		First(&member).Error
	require.NoError(t, err)
	require.Equal(t, firstMsgID, member.LastReadMsgID)
	require.EqualValues(t, 1, member.UnreadCount)

	err = connRecipient2.WriteJSON(map[string]interface{}{
		"cmd": "pull_sync",
		"seq": int64(3),
		"payload": map[string]interface{}{
			"last_inbox_seq": strconv.FormatInt(firstInboxSeq, 10),
		},
	})
	require.NoError(t, err)

	pullSyncResp := readWSMessageByCmd(t, connRecipient2, "pull_sync_resp", 2*time.Second)
	pullSyncPayload, ok := pullSyncResp["payload"].(map[string]interface{})
	require.True(t, ok)
	rawMessages, ok := pullSyncPayload["messages"].([]interface{})
	require.True(t, ok)
	require.Len(t, rawMessages, 1)

	row, ok := rawMessages[0].(map[string]interface{})
	require.True(t, ok)
	recoveredMsgID, err := parseID(row["msg_id"])
	require.NoError(t, err)
	require.Equal(t, secondMsgID, recoveredMsgID)
	require.Equal(t, "second message", row["content"])

	rawUnreadSnapshot, ok := pullSyncPayload["unread_snapshot"].(map[string]interface{})
	require.True(t, ok)
	unreadCount, err := parseID(rawUnreadSnapshot[sessionID])
	require.NoError(t, err)
	require.EqualValues(t, 1, unreadCount)
}

func TestWS_SessionReadClampsFutureBoundaryToExistingMessage(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()
	sqlDB, err := store.DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	server, wsPort := startTestWSServer(t, "node-read-clamp")
	defer server.Shutdown()

	tokenSender, senderID := ctx.loginHelper(t, "ws_read_clamp_sender", "Password123", "device_read_clamp_sender")
	tokenRecipient, recipientID := ctx.loginHelper(t, "ws_read_clamp_recipient", "Password123", "device_read_clamp_recipient")

	for _, friendship := range []model.Friend{
		{UserID: senderID, FriendID: recipientID},
		{UserID: recipientID, FriendID: senderID},
	} {
		err = store.DB.Create(&friendship).Error
		require.NoError(t, err)
	}

	w := ctx.doReq(t, "POST", "/v1/sessions/create", tokenSender, map[string]interface{}{
		"peer_id":   strconv.FormatInt(recipientID, 10),
		"peer_type": 1,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	createResp := parseResp(t, w)
	createData, ok := createResp["data"].(map[string]interface{})
	require.True(t, ok)
	sessionID, _ := createData["session_id"].(string)
	require.NotEmpty(t, sessionID)

	dialer := websocket.DefaultDialer
	wsURL := fmt.Sprintf("ws://localhost:%d/ws", wsPort)

	connSender, respSender, err := dialer.Dial(wsURL, http.Header{})
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, respSender.StatusCode)
	defer connSender.Close()

	connRecipient, respRecipient, err := dialer.Dial(wsURL, http.Header{})
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, respRecipient.StatusCode)

	err = connSender.WriteJSON(map[string]interface{}{
		"cmd": "auth",
		"seq": int64(1),
		"payload": map[string]interface{}{
			"token":     tokenSender,
			"device_id": "device_read_clamp_sender",
			"platform":  "test",
		},
	})
	require.NoError(t, err)
	authAckSender := readWSMessageByCmd(t, connSender, "auth_ack", 2*time.Second)
	authPayloadSender, ok := authAckSender["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, float64(0), authPayloadSender["code"])

	err = connRecipient.WriteJSON(map[string]interface{}{
		"cmd": "auth",
		"seq": int64(1),
		"payload": map[string]interface{}{
			"token":     tokenRecipient,
			"device_id": "device_read_clamp_recipient",
			"platform":  "test",
		},
	})
	require.NoError(t, err)
	authAckRecipient := readWSMessageByCmd(t, connRecipient, "auth_ack", 2*time.Second)
	authPayloadRecipient, ok := authAckRecipient["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, float64(0), authPayloadRecipient["code"])

	err = connSender.WriteJSON(map[string]interface{}{
		"cmd": "send_msg",
		"seq": int64(2),
		"payload": map[string]interface{}{
			"session_id":    sessionID,
			"client_msg_id": "read_clamp_msg_1",
			"msg_type":      1,
			"content":       "first message",
		},
	})
	require.NoError(t, err)

	sendAck1 := readWSMessageByCmd(t, connSender, "send_ack", 2*time.Second)
	sendAckPayload1, ok := sendAck1["payload"].(map[string]interface{})
	require.True(t, ok)
	firstMsgID, err := parseID(sendAckPayload1["msg_id"])
	require.NoError(t, err)

	firstPush := readWSMessageByCmd(t, connRecipient, "push_msg", 2*time.Second)
	firstPushPayload, ok := firstPush["payload"].(map[string]interface{})
	require.True(t, ok)
	firstPushMsgID, err := parseID(firstPushPayload["msg_id"])
	require.NoError(t, err)
	require.Equal(t, firstMsgID, firstPushMsgID)
	firstInboxSeq, err := parseID(firstPushPayload["inbox_seq"])
	require.NoError(t, err)

	err = connRecipient.WriteJSON(map[string]interface{}{
		"cmd": "push_ack",
		"seq": int64(2),
		"payload": map[string]interface{}{
			"msg_id": strconv.FormatInt(firstMsgID, 10),
		},
	})
	require.NoError(t, err)

	err = store.DB.Model(&model.Session{}).
		Where("session_id = ?", sessionID).
		Update("last_msg_id", nil).Error
	require.NoError(t, err)

	requestedFutureMsgID := firstMsgID + 999999999
	err = connRecipient.WriteJSON(map[string]interface{}{
		"cmd": "session_read",
		"seq": int64(3),
		"payload": map[string]interface{}{
			"session_id":       sessionID,
			"last_read_msg_id": strconv.FormatInt(requestedFutureMsgID, 10),
		},
	})
	require.NoError(t, err)

	readAck := readWSMessageByCmd(t, connRecipient, "session_read_ack", 2*time.Second)
	readAckPayload, ok := readAck["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, float64(0), readAckPayload["code"])
	ackedMsgID, err := parseID(readAckPayload["last_read_msg_id"])
	require.NoError(t, err)
	require.Equal(t, firstMsgID, ackedMsgID)

	var member model.SessionMember
	err = store.DB.Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, recipientID).
		First(&member).Error
	require.NoError(t, err)
	require.Equal(t, firstMsgID, member.LastReadMsgID)
	require.EqualValues(t, 0, member.UnreadCount)

	connRecipient.Close()

	err = connSender.WriteJSON(map[string]interface{}{
		"cmd": "send_msg",
		"seq": int64(4),
		"payload": map[string]interface{}{
			"session_id":    sessionID,
			"client_msg_id": "read_clamp_msg_2",
			"msg_type":      1,
			"content":       "second message",
		},
	})
	require.NoError(t, err)

	sendAck2 := readWSMessageByCmd(t, connSender, "send_ack", 2*time.Second)
	sendAckPayload2, ok := sendAck2["payload"].(map[string]interface{})
	require.True(t, ok)
	secondMsgID, err := parseID(sendAckPayload2["msg_id"])
	require.NoError(t, err)
	require.Greater(t, secondMsgID, firstMsgID)

	connRecipient2, respRecipient2, err := dialer.Dial(wsURL, http.Header{})
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, respRecipient2.StatusCode)
	defer connRecipient2.Close()

	err = connRecipient2.WriteJSON(map[string]interface{}{
		"cmd": "auth",
		"seq": int64(1),
		"payload": map[string]interface{}{
			"token":     tokenRecipient,
			"device_id": "device_read_clamp_recipient",
			"platform":  "test",
		},
	})
	require.NoError(t, err)
	authAckRecipient2 := readWSMessageByCmd(t, connRecipient2, "auth_ack", 2*time.Second)
	authPayloadRecipient2, ok := authAckRecipient2["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, float64(0), authPayloadRecipient2["code"])

	err = connRecipient2.WriteJSON(map[string]interface{}{
		"cmd": "pull_sync",
		"seq": int64(2),
		"payload": map[string]interface{}{
			"last_inbox_seq": strconv.FormatInt(firstInboxSeq, 10),
		},
	})
	require.NoError(t, err)

	pullSyncResp := readWSMessageByCmd(t, connRecipient2, "pull_sync_resp", 2*time.Second)
	pullSyncPayload, ok := pullSyncResp["payload"].(map[string]interface{})
	require.True(t, ok)
	rawMessages, ok := pullSyncPayload["messages"].([]interface{})
	require.True(t, ok)
	require.Len(t, rawMessages, 1)

	row, ok := rawMessages[0].(map[string]interface{})
	require.True(t, ok)
	recoveredMsgID, err := parseID(row["msg_id"])
	require.NoError(t, err)
	require.Equal(t, secondMsgID, recoveredMsgID)

	rawUnreadSnapshot, ok := pullSyncPayload["unread_snapshot"].(map[string]interface{})
	require.True(t, ok)
	unreadCount, err := parseID(rawUnreadSnapshot[sessionID])
	require.NoError(t, err)
	require.EqualValues(t, 1, unreadCount)
}

func TestWS_GroupDissolveBroadcast(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()

	server, wsPort := startTestWSServer(t, "node-group-dissolve")
	defer server.Shutdown()

	tokenOwner, ownerID := ctx.loginHelper(t, "ws_group_owner", "Password123", "device_owner")
	tokenMember, memberID := ctx.loginHelper(t, "ws_group_member", "Password123", "device_member")

	// Build friend relationship: owner -> member request, member accepts.
	reqPayload := map[string]interface{}{
		"to_user_id": fmt.Sprintf("%d", memberID),
		"message":    "join my group",
	}
	w := ctx.doReq(t, "POST", "/v1/friends/request", tokenOwner, reqPayload)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = ctx.doReq(t, "GET", "/v1/friends/requests", tokenMember, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	res := parseResp(t, w)
	data := res["data"].(map[string]interface{})
	reqList := data["list"].([]interface{})
	require.NotEmpty(t, reqList)
	firstReq := reqList[0].(map[string]interface{})
	reqID, err := parseID(firstReq["id"])
	require.NoError(t, err)

	handlePayload := map[string]interface{}{
		"request_id": fmt.Sprintf("%d", reqID),
		"accept":     true,
	}
	w = ctx.doReq(t, "POST", "/v1/friends/handle", tokenMember, handlePayload)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Create group with owner + member.
	createGroupPayload := map[string]interface{}{
		"name":         "ws_dissolve_group",
		"member_ids":   []string{fmt.Sprintf("%d", memberID)},
		"member_types": []int{1},
	}
	w = ctx.doReq(t, "POST", "/v1/sessions/create_group", tokenOwner, createGroupPayload)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	createResp := parseResp(t, w)
	createData := createResp["data"].(map[string]interface{})
	sessionID, _ := createData["session_id"].(string)
	require.NotEmpty(t, sessionID)

	dialer := websocket.DefaultDialer
	wsURL := fmt.Sprintf("ws://localhost:%d/ws", wsPort)
	reqHeader := http.Header{}

	connOwner, respOwner, err := dialer.Dial(wsURL, reqHeader)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, respOwner.StatusCode)
	defer connOwner.Close()

	connMember, respMember, err := dialer.Dial(wsURL, reqHeader)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, respMember.StatusCode)
	defer connMember.Close()

	authOwner := map[string]interface{}{
		"cmd": "auth",
		"seq": int64(1),
		"payload": map[string]interface{}{
			"token":     tokenOwner,
			"device_id": "device_owner",
			"platform":  "test",
		},
	}
	err = connOwner.WriteJSON(authOwner)
	require.NoError(t, err)
	connOwner.SetReadDeadline(time.Now().Add(2 * time.Second))
	var authAckOwner map[string]interface{}
	err = connOwner.ReadJSON(&authAckOwner)
	require.NoError(t, err)
	require.Equal(t, "auth_ack", authAckOwner["cmd"])

	authMember := map[string]interface{}{
		"cmd": "auth",
		"seq": int64(1),
		"payload": map[string]interface{}{
			"token":     tokenMember,
			"device_id": "device_member",
			"platform":  "test",
		},
	}
	err = connMember.WriteJSON(authMember)
	require.NoError(t, err)
	connMember.SetReadDeadline(time.Now().Add(2 * time.Second))
	var authAckMember map[string]interface{}
	err = connMember.ReadJSON(&authAckMember)
	require.NoError(t, err)
	require.Equal(t, "auth_ack", authAckMember["cmd"])

	// Dissolve group through HTTP and verify both WS clients receive event.
	dissolvePayload := map[string]interface{}{
		"session_id": sessionID,
	}
	w = ctx.doReq(t, "POST", "/v1/sessions/dissolve", tokenOwner, dissolvePayload)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assertSessionDissolveEvent(t, connOwner, sessionID, ownerID, ownerID, memberID)
	assertSessionDissolveEvent(t, connMember, sessionID, ownerID, ownerID, memberID)
}

func TestWS_DelegateStopStopsAgentAPIStream(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()

	server, wsPort := startTestWSServer(t, "node-delegate-stop")
	defer server.Shutdown()

	tokenOwner, ownerID := ctx.loginHelper(t, "ws_delegate_owner", "Password123", "device_owner_delegate")

	sessionID := fmt.Sprintf("e2e_delegate_stop_%d", time.Now().UnixNano())
	agentID := time.Now().UnixNano() + 1000
	apiKeyPlain, apiKeyHash, apiKeyHint, err := pkgagentapi.GenerateAPIKey(agentID)
	require.NoError(t, err)

	err = store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
	}).Error
	require.NoError(t, err)
	err = store.DB.Create(&model.SessionMember{
		SessionID:  sessionID,
		MemberID:   ownerID,
		MemberType: 1,
	}).Error
	require.NoError(t, err)
	err = store.DB.Create(&model.Agent{
		ID:           agentID,
		AgentName:    "e2e-api-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       1,
		APIKeyHash:   apiKeyHash,
		APIKeyHint:   apiKeyHint,
	}).Error
	require.NoError(t, err)

	dialer := websocket.DefaultDialer
	wsURL := fmt.Sprintf("ws://localhost:%d/ws", wsPort)

	connOwner, respOwner, err := dialer.Dial(wsURL, http.Header{})
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, respOwner.StatusCode)
	defer connOwner.Close()

	agentWSURL := fmt.Sprintf(
		"ws://localhost:%d/v1/agent-api/ws?agent_id=%d",
		wsPort,
		agentID,
	)
	connAgent, respAgent, err := dialer.Dial(agentWSURL, http.Header{})
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, respAgent.StatusCode)
	defer connAgent.Close()

	err = connOwner.WriteJSON(map[string]interface{}{
		"cmd": "auth",
		"seq": int64(1),
		"payload": map[string]interface{}{
			"token":     tokenOwner,
			"device_id": "device_owner_delegate",
			"platform":  "test",
		},
	})
	require.NoError(t, err)
	authAck := readWSMessageByCmd(t, connOwner, "auth_ack", 2*time.Second)
	authPayload, ok := authAck["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, float64(0), authPayload["code"])

	err = connAgent.WriteJSON(map[string]interface{}{
		"cmd": "auth",
		"seq": int64(1),
		"payload": map[string]interface{}{
			"agent_id": strconv.FormatInt(agentID, 10),
			"api_key":  apiKeyPlain,
			"client":   "e2e-agent",
		},
	})
	require.NoError(t, err)
	agentAuthAck := readWSMessageByCmd(t, connAgent, "auth_ack", 2*time.Second)
	agentAuthPayload, ok := agentAuthAck["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, float64(0), agentAuthPayload["code"])

	err = connOwner.WriteJSON(map[string]interface{}{
		"cmd": "delegate_start",
		"seq": int64(2),
		"payload": map[string]interface{}{
			"session_id": sessionID,
			"agent_id":   strconv.FormatInt(agentID, 10),
		},
	})
	require.NoError(t, err)
	delegateStartAck := readWSMessageByCmd(t, connOwner, "delegate_ack", 2*time.Second)
	startPayload, ok := delegateStartAck["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, true, startPayload["active"])

	// First chunk arrives before stop.
	err = connAgent.WriteJSON(map[string]interface{}{
		"cmd": "client_stream_chunk",
		"seq": int64(2),
		"payload": map[string]interface{}{
			"session_id":    sessionID,
			"client_msg_id": "e2e_stream_1",
			"chunk_seq":     1,
			"delta_content": "hello ",
			"is_finish":     false,
		},
	})
	require.NoError(t, err)
	firstChunk := readWSMessageByCmd(t, connOwner, "stream_chunk", 2*time.Second)
	firstChunkPayload, ok := firstChunk["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "hello ", firstChunkPayload["delta_content"])

	// Owner stops delegation.
	err = connOwner.WriteJSON(map[string]interface{}{
		"cmd": "delegate_stop",
		"seq": int64(3),
		"payload": map[string]interface{}{
			"session_id": sessionID,
		},
	})
	require.NoError(t, err)
	delegateStopAck := readWSMessageByCmd(t, connOwner, "delegate_ack", 2*time.Second)
	stopPayload, ok := delegateStopAck["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, false, stopPayload["active"])

	// Agent keeps pushing; backend should not forward new chunk content after stop.
	err = connAgent.WriteJSON(map[string]interface{}{
		"cmd": "client_stream_chunk",
		"seq": int64(3),
		"payload": map[string]interface{}{
			"session_id":    sessionID,
			"client_msg_id": "e2e_stream_1",
			"chunk_seq":     2,
			"delta_content": "after-stop",
			"is_finish":     false,
		},
	})
	require.NoError(t, err)

	var gotFinish bool
	var sawAfterStopChunk bool
	deadline := time.Now().Add(1500 * time.Millisecond)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		connOwner.SetReadDeadline(time.Now().Add(remaining))
		var evt map[string]interface{}
		err := connOwner.ReadJSON(&evt)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				break
			}
			t.Fatalf("read owner event after stop failed: %v", err)
		}

		cmd, _ := evt["cmd"].(string)
		switch cmd {
		case "stream_chunk":
			payload, _ := evt["payload"].(map[string]interface{})
			delta, _ := payload["delta_content"].(string)
			if strings.Contains(delta, "after-stop") {
				sawAfterStopChunk = true
			}
		case "stream_finish":
			payload, _ := evt["payload"].(map[string]interface{})
			finalContent, _ := payload["final_content"].(string)
			gotFinish = true
			require.Equal(t, "hello ", finalContent)
		}
	}

	require.False(t, sawAfterStopChunk, "received chunk generated after delegate_stop")
	require.True(t, gotFinish, "expected stream_finish from aborted stream finalization")
}

func TestWS_DelegatedAgentAPIRoundTrip(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()

	server, wsPort := startTestWSServer(t, "node-delegate-roundtrip")
	defer server.Shutdown()

	tokenA, userAID := ctx.loginHelper(t, "ws_delegate_user_a", "Password123", "device_delegate_a")
	tokenB, userBID := ctx.loginHelper(t, "ws_delegate_user_b", "Password123", "device_delegate_b")

	for _, friendship := range []model.Friend{
		{UserID: userAID, FriendID: userBID},
		{UserID: userBID, FriendID: userAID},
	} {
		err := store.DB.Create(&friendship).Error
		require.NoError(t, err)
	}

	sessionID := fmt.Sprintf("e2e_delegate_roundtrip_%d", time.Now().UnixNano())
	agentID := time.Now().UnixNano() + 2000
	apiKeyPlain, apiKeyHash, apiKeyHint, err := pkgagentapi.GenerateAPIKey(agentID)
	require.NoError(t, err)

	err = store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     userBID,
		SessionType: 1,
	}).Error
	require.NoError(t, err)
	for _, member := range []model.SessionMember{
		{SessionID: sessionID, MemberID: userAID, MemberType: 1},
		{SessionID: sessionID, MemberID: userBID, MemberType: 1},
	} {
		err = store.DB.Create(&member).Error
		require.NoError(t, err)
	}
	err = store.DB.Create(&model.Agent{
		ID:           agentID,
		AgentName:    "e2e-roundtrip-agent",
		OwnerID:      userBID,
		ProviderType: model.AgentProviderAPI,
		Status:       1,
		APIKeyHash:   apiKeyHash,
		APIKeyHint:   apiKeyHint,
	}).Error
	require.NoError(t, err)

	dialer := websocket.DefaultDialer
	wsURL := fmt.Sprintf("ws://localhost:%d/ws", wsPort)

	connA, respA, err := dialer.Dial(wsURL, http.Header{})
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, respA.StatusCode)
	defer connA.Close()

	connB, respB, err := dialer.Dial(wsURL, http.Header{})
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, respB.StatusCode)
	defer connB.Close()

	agentWSURL := fmt.Sprintf("ws://localhost:%d/v1/agent-api/ws?agent_id=%d", wsPort, agentID)
	connAgent, respAgent, err := dialer.Dial(agentWSURL, http.Header{})
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, respAgent.StatusCode)
	defer connAgent.Close()

	err = connA.WriteJSON(map[string]interface{}{
		"cmd": "auth",
		"seq": int64(1),
		"payload": map[string]interface{}{
			"token":     tokenA,
			"device_id": "device_delegate_a",
			"platform":  "test",
		},
	})
	require.NoError(t, err)
	authAckA := readWSMessageByCmd(t, connA, "auth_ack", 2*time.Second)
	authPayloadA, ok := authAckA["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, float64(0), authPayloadA["code"])

	err = connB.WriteJSON(map[string]interface{}{
		"cmd": "auth",
		"seq": int64(1),
		"payload": map[string]interface{}{
			"token":     tokenB,
			"device_id": "device_delegate_b",
			"platform":  "test",
		},
	})
	require.NoError(t, err)
	authAckB := readWSMessageByCmd(t, connB, "auth_ack", 2*time.Second)
	authPayloadB, ok := authAckB["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, float64(0), authPayloadB["code"])

	err = connAgent.WriteJSON(map[string]interface{}{
		"cmd": "auth",
		"seq": int64(1),
		"payload": map[string]interface{}{
			"agent_id": strconv.FormatInt(agentID, 10),
			"api_key":  apiKeyPlain,
			"client":   "e2e-roundtrip-agent",
		},
	})
	require.NoError(t, err)
	agentAuthAck := readWSMessageByCmd(t, connAgent, "auth_ack", 2*time.Second)
	agentAuthPayload, ok := agentAuthAck["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, float64(0), agentAuthPayload["code"])

	err = connB.WriteJSON(map[string]interface{}{
		"cmd": "delegate_start",
		"seq": int64(2),
		"payload": map[string]interface{}{
			"session_id": sessionID,
			"agent_id":   strconv.FormatInt(agentID, 10),
		},
	})
	require.NoError(t, err)
	delegateStartAck := readWSMessageByCmd(t, connB, "delegate_ack", 2*time.Second)
	startPayload, ok := delegateStartAck["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, true, startPayload["active"])

	err = connA.WriteJSON(map[string]interface{}{
		"cmd": "send_msg",
		"seq": int64(2),
		"payload": map[string]interface{}{
			"session_id":    sessionID,
			"client_msg_id": "e2e_delegate_a_to_b_1",
			"msg_type":      1,
			"content":       "hello from A",
		},
	})
	require.NoError(t, err)

	sendAckA := readWSMessageByCmd(t, connA, "send_ack", 2*time.Second)
	sendAckPayloadA, ok := sendAckA["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "e2e_delegate_a_to_b_1", sendAckPayloadA["client_msg_id"])

	incomingForB := readWSMessageByCmd(t, connB, "push_msg", 2*time.Second)
	incomingForBPayload, ok := incomingForB["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "hello from A", incomingForBPayload["content"])
	require.Equal(t, strconv.FormatInt(userAID, 10), fmt.Sprintf("%v", incomingForBPayload["sender_id"]))

	eventMsg := readWSMessageByCmd(t, connAgent, "event_msg", 2*time.Second)
	eventPayload, ok := eventMsg["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "user_chat", eventPayload["event_type"])
	require.Equal(t, sessionID, eventPayload["session_id"])
	require.Equal(t, "hello from A", eventPayload["content"])
	require.Equal(t, strconv.FormatInt(userBID, 10), fmt.Sprintf("%v", eventPayload["owner_id"]))
	require.Equal(t, strconv.FormatInt(userAID, 10), fmt.Sprintf("%v", eventPayload["sender_id"]))

	quotedMessageID := fmt.Sprintf("%v", eventPayload["msg_id"])
	err = connAgent.WriteJSON(map[string]interface{}{
		"cmd": "send_msg",
		"seq": int64(2),
		"payload": map[string]interface{}{
			"session_id":        sessionID,
			"client_msg_id":     "e2e_delegate_reply_1",
			"msg_type":          1,
			"content":           "hello from delegated agent",
			"quoted_message_id": quotedMessageID,
		},
	})
	require.NoError(t, err)

	agentSendAck := readWSMessageByCmd(t, connAgent, "send_ack", 2*time.Second)
	agentSendAckPayload, ok := agentSendAck["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "e2e_delegate_reply_1", agentSendAckPayload["client_msg_id"])

	replyForA := readWSPushMessageByContent(t, connA, "hello from delegated agent", 2*time.Second)
	replyForAPayload, ok := replyForA["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "hello from delegated agent", replyForAPayload["content"])
	require.Equal(t, strconv.FormatInt(userBID, 10), fmt.Sprintf("%v", replyForAPayload["sender_id"]))

	replyForB := readWSPushMessageByContent(t, connB, "hello from delegated agent", 2*time.Second)
	replyForBPayload, ok := replyForB["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "hello from delegated agent", replyForBPayload["content"])
	require.Equal(t, strconv.FormatInt(userBID, 10), fmt.Sprintf("%v", replyForBPayload["sender_id"]))
}

func readWSPushMessageByContent(
	t *testing.T,
	conn *websocket.Conn,
	wantContent string,
	timeout time.Duration,
) map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatalf("timed out waiting for push_msg content=%q", wantContent)
		}
		conn.SetReadDeadline(time.Now().Add(remaining))
		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("read websocket message failed while waiting push_msg content=%q: %v", wantContent, err)
		}
		if cmd, _ := msg["cmd"].(string); cmd != "push_msg" {
			continue
		}
		payload, _ := msg["payload"].(map[string]interface{})
		if payload["content"] == wantContent {
			return msg
		}
	}
}

func readWSMessageByCmd(
	t *testing.T,
	conn *websocket.Conn,
	wantCmd string,
	timeout time.Duration,
) map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatalf("timed out waiting for cmd=%s", wantCmd)
		}
		conn.SetReadDeadline(time.Now().Add(remaining))
		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("read websocket message failed while waiting cmd=%s: %v", wantCmd, err)
		}
		cmd, _ := msg["cmd"].(string)
		if cmd == wantCmd {
			return msg
		}
	}
}

func assertSessionDissolveEvent(
	t *testing.T,
	conn *websocket.Conn,
	sessionID string,
	operatorID int64,
	expectedUserIDs ...int64,
) {
	t.Helper()

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var event map[string]interface{}
	err := conn.ReadJSON(&event)
	require.NoError(t, err)
	require.Equal(t, "session_member_changed", event["cmd"])

	payload, ok := event["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, sessionID, payload["session_id"])
	require.Equal(t, "dissolve", payload["action"])

	gotOperatorID, err := parseID(payload["operator_id"])
	require.NoError(t, err)
	require.Equal(t, operatorID, gotOperatorID)

	removedRaw, ok := payload["removed_user_ids"].([]interface{})
	require.True(t, ok)
	for _, uid := range expectedUserIDs {
		assert.True(t, containsID(removedRaw, uid), "removed_user_ids missing user=%d payload=%v", uid, removedRaw)
	}
}

func containsID(items []interface{}, target int64) bool {
	for _, item := range items {
		id, err := parseID(item)
		if err != nil {
			continue
		}
		if id == target {
			return true
		}
	}
	return false
}
