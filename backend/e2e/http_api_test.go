package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestHTTP_AuthAndUserFlow(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()

	// 1. Login with a new user (Auto-register)
	username := "e2e_user_" + time.Now().Format("150405")
	password := "securepwd123"

	token, userID := ctx.loginHelper(t, username, password)
	assert.NotEmpty(t, token)
	assert.Greater(t, userID, int64(0))

	// 2. Get Profile
	w := ctx.doReq(t, "GET", "/v1/users/profile", token, nil)
	assert.Equal(t, http.StatusOK, w.Code)

	res := parseResp(t, w)
	data := res["data"].(map[string]interface{})
	assert.Equal(t, username, data["username"])

	// 3. Update Profile
	updatePayload := map[string]interface{}{
		"nickname":     "E2E Master",
		"introduction": "E2E user introduction",
	}
	w = ctx.doReq(t, "PUT", "/v1/users/profile", token, updatePayload)
	assert.Equal(t, http.StatusOK, w.Code)

	// Verify update
	w = ctx.doReq(t, "GET", "/v1/users/profile", token, nil)
	res = parseResp(t, w)
	data = res["data"].(map[string]interface{})
	assert.Equal(t, "E2E Master", data["nickname"])
	assert.Equal(t, "E2E user introduction", data["introduction"])

	// 4. Logout
	logoutPayload := map[string]interface{}{
		"device_id": "e2e_device_1",
	}
	w = ctx.doReq(t, "POST", "/v1/auth/logout", token, logoutPayload)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHTTP_FriendFlow(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()

	// Alice registers
	tokenA, idA := ctx.loginHelper(t, "alice", "pwd12345")
	// Bob registers
	tokenB, idB := ctx.loginHelper(t, "bob", "pwd12345")

	// 1. Alice searches for Bob
	w := ctx.doReq(t, "GET", "/v1/users/search?keyword=bob", tokenA, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	res := parseResp(t, w)
	data := res["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	assert.True(t, len(list) > 0, "should find bob")

	// 2. Alice sends friend request to Bob
	reqPayload := map[string]interface{}{
		"to_user_id": fmt.Sprintf("%d", idB),
		"message":    "Hi Bob, I am Alice.",
	}
	w = ctx.doReq(t, "POST", "/v1/friends/request", tokenA, reqPayload)
	assert.Equal(t, http.StatusOK, w.Code)

	// 3. Bob gets friend requests
	w = ctx.doReq(t, "GET", "/v1/friends/requests", tokenB, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	res = parseResp(t, w)
	data = res["data"].(map[string]interface{})
	reqList := data["list"].([]interface{})
	assert.Equal(t, 1, len(reqList))

	firstReq := reqList[0].(map[string]interface{})
	reqID, _ := parseID(firstReq["id"])
	fromUserID, _ := parseID(firstReq["from_user_id"])
	assert.Equal(t, idA, fromUserID)

	// 4. Bob accepts the request
	handlePayload := map[string]interface{}{
		"request_id": fmt.Sprintf("%d", reqID),
		"accept":     true,
	}
	w = ctx.doReq(t, "POST", "/v1/friends/handle", tokenB, handlePayload)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// 5. Alice checks friend list
	w = ctx.doReq(t, "GET", "/v1/friends/list", tokenA, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	res = parseResp(t, w)
	data = res["data"].(map[string]interface{})
	friendList := data["list"].([]interface{})
	assert.Equal(t, 1, len(friendList))
	firstFriend := friendList[0].(map[string]interface{})
	friendUserID, _ := parseID(firstFriend["user_id"])
	assert.Equal(t, idB, friendUserID)

	// 6. Bob checks friend list
	w = ctx.doReq(t, "GET", "/v1/friends/list", tokenB, nil)
	res = parseResp(t, w)
	data = res["data"].(map[string]interface{})
	friendList = data["list"].([]interface{})
	assert.Equal(t, 1, len(friendList))

	friendUserIDStr := firstFriend["user_id"].(string)
	w = ctx.doReq(t, "DELETE", "/v1/friends/"+friendUserIDStr, tokenA, nil)
}
