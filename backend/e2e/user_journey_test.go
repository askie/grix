package e2e

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/ws"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const userJourneyPassword = "Password123A"

func TestUserJourney_FirstUseProfileAndReturn(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()

	account := fmt.Sprintf("journey_first_%d", time.Now().UnixNano())
	deviceID := "journey-first-device"

	token, userID := ctx.loginHelper(t, account, userJourneyPassword, deviceID)
	require.NotEmpty(t, token)
	require.Greater(t, userID, int64(0))

	w := ctx.doReq(t, http.MethodGet, "/v1/users/profile", token, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	profile := parseResp(t, w)["data"].(map[string]interface{})
	require.Equal(t, account, asString(profile["username"]))

	updatePayload := map[string]interface{}{
		"nickname":     "Journey First User",
		"introduction": "first-use profile saved",
	}
	w = ctx.doReq(t, http.MethodPut, "/v1/users/profile", token, updatePayload)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	tokenAgain, userIDAgain := ctx.loginHelper(t, account, userJourneyPassword, deviceID)
	require.Equal(t, userID, userIDAgain)
	require.NotEmpty(t, tokenAgain)

	w = ctx.doReq(t, http.MethodGet, "/v1/users/profile", tokenAgain, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	profile = parseResp(t, w)["data"].(map[string]interface{})
	assert.Equal(t, "Journey First User", asString(profile["nickname"]))
	assert.Equal(t, "first-use profile saved", asString(profile["introduction"]))

	w = ctx.doReq(t, http.MethodPost, "/v1/auth/logout", tokenAgain, map[string]interface{}{
		"device_id": deviceID,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = ctx.doReq(t, http.MethodGet, "/v1/users/profile", tokenAgain, nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUserJourney_ContactsToPrivateChat(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()

	wsPort, server := startJourneyWSServer(t, "journey-dm")
	defer server.Shutdown()

	suffix := time.Now().UnixNano()
	accountA := fmt.Sprintf("journey_dm_a_%d", suffix)
	accountB := fmt.Sprintf("journey_dm_b_%d", suffix)

	tokenA, idA := ctx.loginHelper(t, accountA, userJourneyPassword, "journey-dm-a")
	tokenB, idB := ctx.loginHelper(t, accountB, userJourneyPassword, "journey-dm-b")

	searchPath := "/v1/users/search?keyword=" + url.QueryEscape(accountB)
	w := ctx.doReq(t, http.MethodGet, searchPath, tokenA, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	searchData := parseResp(t, w)["data"].(map[string]interface{})
	searchList := mustListMaps(t, searchData["list"])
	require.NotEmpty(t, searchList)
	assert.True(t, containsUserID(searchList, idB), "search results should include the target user")

	establishFriendshipViaAPI(t, ctx, tokenA, idB, tokenB, idA)

	sessionID := createDirectSessionViaAPI(t, ctx, tokenA, idB)
	require.NotEmpty(t, sessionID)

	connA := connectJourneyUserWS(t, wsPort, tokenA, "journey-dm-a")
	defer connA.Close()
	connB := connectJourneyUserWS(t, wsPort, tokenB, "journey-dm-b")
	defer connB.Close()

	content := fmt.Sprintf("journey-private-message-%d", time.Now().UnixNano())
	require.NoError(t, connA.WriteJSON(map[string]interface{}{
		"cmd": "send_msg",
		"seq": int64(2),
		"payload": map[string]interface{}{
			"session_id":    sessionID,
			"client_msg_id": fmt.Sprintf("journey-dm-%d", time.Now().UnixNano()),
			"msg_type":      1,
			"content":       content,
		},
	}))

	sendAck := readWSMessageByCmd(t, connA, "send_ack", 2*time.Second)
	sendAckPayload, ok := sendAck["payload"].(map[string]interface{})
	require.True(t, ok)
	msgID, err := parseID(sendAckPayload["msg_id"])
	require.NoError(t, err)

	pushMsg := readWSMessageByCmd(t, connB, "push_msg", 2*time.Second)
	pushPayload, ok := pushMsg["payload"].(map[string]interface{})
	require.True(t, ok)
	pushMsgID, err := parseID(pushPayload["msg_id"])
	require.NoError(t, err)
	require.Equal(t, msgID, pushMsgID)
	assert.Equal(t, content, asString(pushPayload["content"]))

	require.NoError(t, connB.WriteJSON(map[string]interface{}{
		"cmd": "push_ack",
		"seq": int64(3),
		"payload": map[string]interface{}{
			"msg_id": strconv.FormatInt(msgID, 10),
		},
	}))

	sessionItemBeforeRead := requireSessionListItem(t, ctx, tokenB, sessionID)
	assert.Equal(t, content, asString(sessionItemBeforeRead["last_msg"]))
	unreadBeforeRead, err := parseID(sessionItemBeforeRead["unread"])
	require.NoError(t, err)
	assert.EqualValues(t, 1, unreadBeforeRead)

	messageRows := fetchMessageHistory(t, ctx, tokenB, sessionID)
	require.NotEmpty(t, messageRows)
	assert.Equal(t, content, asString(messageRows[0]["content"]))

	require.NoError(t, connB.WriteJSON(map[string]interface{}{
		"cmd": "session_read",
		"seq": int64(4),
		"payload": map[string]interface{}{
			"session_id":       sessionID,
			"last_read_msg_id": strconv.FormatInt(msgID, 10),
		},
	}))

	readAck := readWSMessageByCmd(t, connB, "session_read_ack", 2*time.Second)
	readAckPayload, ok := readAck["payload"].(map[string]interface{})
	require.True(t, ok)
	readAckMsgID, err := parseID(readAckPayload["last_read_msg_id"])
	require.NoError(t, err)
	assert.Equal(t, msgID, readAckMsgID)

	sessionItemAfterRead := requireSessionListItem(t, ctx, tokenB, sessionID)
	unreadAfterRead, err := parseID(sessionItemAfterRead["unread"])
	require.NoError(t, err)
	assert.Zero(t, unreadAfterRead)
}

func TestUserJourney_GroupCreateInviteAndChat(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()

	wsPort, server := startJourneyWSServer(t, "journey-group")
	defer server.Shutdown()

	suffix := time.Now().UnixNano()
	ownerAccount := fmt.Sprintf("journey_group_owner_%d", suffix)
	memberAAccount := fmt.Sprintf("journey_group_member_a_%d", suffix)
	memberBAccount := fmt.Sprintf("journey_group_member_b_%d", suffix)

	ownerToken, ownerID := ctx.loginHelper(t, ownerAccount, userJourneyPassword, "journey-group-owner")
	memberAToken, memberAID := ctx.loginHelper(t, memberAAccount, userJourneyPassword, "journey-group-a")
	memberBToken, memberBID := ctx.loginHelper(t, memberBAccount, userJourneyPassword, "journey-group-b")

	establishFriendshipViaAPI(t, ctx, ownerToken, memberAID, memberAToken, ownerID)
	establishFriendshipViaAPI(t, ctx, ownerToken, memberBID, memberBToken, ownerID)

	groupName := fmt.Sprintf("Journey Group %d", suffix)
	groupSessionID := createGroupSessionViaAPI(t, ctx, ownerToken, groupName, []int64{memberAID})
	require.NotEmpty(t, groupSessionID)

	groupDetail := fetchSessionDetail(t, ctx, ownerToken, groupSessionID)
	assert.Equal(t, groupName, asString(groupDetail["group_name"]))
	memberCount, err := parseID(groupDetail["member_count"])
	require.NoError(t, err)
	assert.EqualValues(t, 2, memberCount)
	assert.True(t, asBool(groupDetail["allow_member_invite"]))

	connOwner := connectJourneyUserWS(t, wsPort, ownerToken, "journey-group-owner")
	defer connOwner.Close()
	connMemberA := connectJourneyUserWS(t, wsPort, memberAToken, "journey-group-a")
	defer connMemberA.Close()

	content := fmt.Sprintf("journey-group-message-%d", time.Now().UnixNano())
	require.NoError(t, connOwner.WriteJSON(map[string]interface{}{
		"cmd": "send_msg",
		"seq": int64(2),
		"payload": map[string]interface{}{
			"session_id":    groupSessionID,
			"client_msg_id": fmt.Sprintf("journey-group-%d", time.Now().UnixNano()),
			"msg_type":      1,
			"content":       content,
		},
	}))

	sendAck := readWSMessageByCmd(t, connOwner, "send_ack", 2*time.Second)
	sendAckPayload, ok := sendAck["payload"].(map[string]interface{})
	require.True(t, ok)
	msgID, err := parseID(sendAckPayload["msg_id"])
	require.NoError(t, err)

	pushMsg := readWSMessageByCmd(t, connMemberA, "push_msg", 2*time.Second)
	pushPayload, ok := pushMsg["payload"].(map[string]interface{})
	require.True(t, ok)
	pushMsgID, err := parseID(pushPayload["msg_id"])
	require.NoError(t, err)
	require.Equal(t, msgID, pushMsgID)
	assert.Equal(t, content, asString(pushPayload["content"]))

	require.NoError(t, connMemberA.WriteJSON(map[string]interface{}{
		"cmd": "push_ack",
		"seq": int64(3),
		"payload": map[string]interface{}{
			"msg_id": strconv.FormatInt(msgID, 10),
		},
	}))

	w := ctx.doReq(t, http.MethodPost, "/v1/sessions/members/add", ownerToken, map[string]interface{}{
		"session_id":   groupSessionID,
		"member_ids":   []string{strconv.FormatInt(memberBID, 10)},
		"member_types": []int16{1},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	groupDetail = fetchSessionDetail(t, ctx, ownerToken, groupSessionID)
	memberCount, err = parseID(groupDetail["member_count"])
	require.NoError(t, err)
	assert.EqualValues(t, 3, memberCount)
	assert.True(t, sessionDetailHasMember(groupDetail, memberBID))

	memberBSession := requireSessionListItem(t, ctx, memberBToken, groupSessionID)
	assert.Equal(t, groupName, asString(memberBSession["title"]))

	messageRows := fetchMessageHistory(t, ctx, memberAToken, groupSessionID)
	require.NotEmpty(t, messageRows)
	assert.Equal(t, content, asString(messageRows[0]["content"]))
}

func startJourneyWSServer(t *testing.T, label string) (int, *ws.Server) {
	t.Helper()

	server, wsPort := startTestWSServer(t, label)
	return wsPort, server
}

func connectJourneyUserWS(t *testing.T, wsPort int, token, deviceID string) *websocket.Conn {
	t.Helper()

	dialer := websocket.DefaultDialer
	wsURL := fmt.Sprintf("ws://localhost:%d/ws", wsPort)

	conn, resp, err := dialer.Dial(wsURL, http.Header{})
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	err = conn.WriteJSON(map[string]interface{}{
		"cmd": "auth",
		"seq": int64(1),
		"payload": map[string]interface{}{
			"token":     token,
			"device_id": deviceID,
			"platform":  "test",
		},
	})
	require.NoError(t, err)

	authAck := readWSMessageByCmd(t, conn, "auth_ack", 2*time.Second)
	authPayload, ok := authAck["payload"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, float64(0), authPayload["code"])

	return conn
}

func establishFriendshipViaAPI(
	t *testing.T,
	ctx *e2eContext,
	requesterToken string,
	targetUserID int64,
	targetToken string,
	requesterUserID int64,
) {
	t.Helper()

	w := ctx.doReq(t, http.MethodPost, "/v1/friends/request", requesterToken, map[string]interface{}{
		"to_user_id": strconv.FormatInt(targetUserID, 10),
		"message":    "user-journey",
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = ctx.doReq(t, http.MethodGet, "/v1/friends/requests", targetToken, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	requestsData := parseResp(t, w)["data"].(map[string]interface{})
	requestRows := mustListMaps(t, requestsData["list"])

	var requestID int64
	for _, row := range requestRows {
		fromUserID, err := parseID(row["from_user_id"])
		require.NoError(t, err)
		if fromUserID != requesterUserID {
			continue
		}
		requestID, err = parseID(row["id"])
		require.NoError(t, err)
		break
	}
	require.Greater(t, requestID, int64(0), "pending friend request should be visible to the recipient")

	w = ctx.doReq(t, http.MethodPost, "/v1/friends/handle", targetToken, map[string]interface{}{
		"request_id": strconv.FormatInt(requestID, 10),
		"accept":     true,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func createDirectSessionViaAPI(t *testing.T, ctx *e2eContext, token string, peerID int64) string {
	t.Helper()

	w := ctx.doReq(t, http.MethodPost, "/v1/sessions/create", token, map[string]interface{}{
		"peer_id":   strconv.FormatInt(peerID, 10),
		"peer_type": 1,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	data := parseResp(t, w)["data"].(map[string]interface{})
	return asString(data["session_id"])
}

func createGroupSessionViaAPI(
	t *testing.T,
	ctx *e2eContext,
	token string,
	groupName string,
	memberIDs []int64,
) string {
	t.Helper()

	memberIDStrings := make([]string, 0, len(memberIDs))
	memberTypes := make([]int16, 0, len(memberIDs))
	for _, memberID := range memberIDs {
		memberIDStrings = append(memberIDStrings, strconv.FormatInt(memberID, 10))
		memberTypes = append(memberTypes, 1)
	}

	w := ctx.doReq(t, http.MethodPost, "/v1/sessions/create_group", token, map[string]interface{}{
		"name":         groupName,
		"member_ids":   memberIDStrings,
		"member_types": memberTypes,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	data := parseResp(t, w)["data"].(map[string]interface{})
	return asString(data["session_id"])
}

func requireSessionListItem(t *testing.T, ctx *e2eContext, token, sessionID string) map[string]interface{} {
	t.Helper()

	w := ctx.doReq(t, http.MethodGet, "/v1/sessions/list", token, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	data := parseResp(t, w)["data"].(map[string]interface{})
	rows := mustListMaps(t, data["list"])
	for _, row := range rows {
		if asString(row["session_id"]) == sessionID {
			return row
		}
	}

	t.Fatalf("session %s not found in session list", sessionID)
	return nil
}

func fetchMessageHistory(t *testing.T, ctx *e2eContext, token, sessionID string) []map[string]interface{} {
	t.Helper()

	path := "/v1/messages/history?session_id=" + url.QueryEscape(sessionID) + "&limit=20"
	w := ctx.doReq(t, http.MethodGet, path, token, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	data := parseResp(t, w)["data"].(map[string]interface{})
	return mustListMaps(t, data["messages"])
}

func fetchSessionDetail(t *testing.T, ctx *e2eContext, token, sessionID string) map[string]interface{} {
	t.Helper()

	path := "/v1/sessions/detail?session_id=" + url.QueryEscape(sessionID)
	w := ctx.doReq(t, http.MethodGet, path, token, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return parseResp(t, w)["data"].(map[string]interface{})
}

func mustListMaps(t *testing.T, raw interface{}) []map[string]interface{} {
	t.Helper()

	items, ok := raw.([]interface{})
	require.True(t, ok, "expected []interface{}, got %T", raw)

	rows := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]interface{})
		require.True(t, ok, "expected map row, got %T", item)
		rows = append(rows, row)
	}
	return rows
}

func containsUserID(rows []map[string]interface{}, target int64) bool {
	for _, row := range rows {
		got, err := parseID(row["id"])
		if err != nil {
			continue
		}
		if got == target {
			return true
		}
	}
	return false
}

func sessionDetailHasMember(detail map[string]interface{}, targetMemberID int64) bool {
	rawMembers, ok := detail["members"].([]interface{})
	if !ok {
		return false
	}
	for _, item := range rawMembers {
		row, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		memberID, err := parseID(row["member_id"])
		if err != nil {
			continue
		}
		if memberID == targetMemberID {
			return true
		}
	}
	return false
}
