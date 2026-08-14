package e2e

import (
	"net/http"
	"testing"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReachSubscriptionAPI_GetCreatesDefault(t *testing.T) {
	ctx := setupE2E(t)
	token, _ := ctx.loginHelper(t, "sub-user@test.com", "Pass123456")

	w := ctx.doReq(t, "GET", "/v1/reach/subscription", token, nil)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	resp := parseResp(t, w)
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, true, data["subscribed"])
}

func TestReachSubscriptionAPI_Update(t *testing.T) {
	ctx := setupE2E(t)
	token, _ := ctx.loginHelper(t, "sub-update@test.com", "Pass123456")

	ctx.doReq(t, "GET", "/v1/reach/subscription", token, nil)

	w := ctx.doReq(t, "PUT", "/v1/reach/subscription", token, map[string]interface{}{
		"subscribed": false,
	})
	require.Equal(t, http.StatusOK, w.Code)

	w = ctx.doReq(t, "GET", "/v1/reach/subscription", token, nil)
	require.Equal(t, http.StatusOK, w.Code)
	resp := parseResp(t, w)
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, false, data["subscribed"])
}

func TestReachUnsubscribeAPI_TokenBased(t *testing.T) {
	ctx := setupE2E(t)
	_, userID := ctx.loginHelper(t, "unsub-token@test.com", "Pass123456")

	sub, err := service.EnsureReachSubscription(userID, "cn")
	require.NoError(t, err)
	require.True(t, sub.Subscribed)

	w := ctx.doReq(t, "GET", "/v1/reach/unsubscribe?token="+sub.UnsubToken, "", nil)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	assert.False(t, service.IsUserSubscribedForReach(userID))
}

func TestReachUnsubscribeAPI_InvalidToken(t *testing.T) {
	ctx := setupE2E(t)
	w := ctx.doReq(t, "GET", "/v1/reach/unsubscribe?token=bogus", "", nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestReachUnsubscribeAPI_MissingToken(t *testing.T) {
	ctx := setupE2E(t)
	w := ctx.doReq(t, "GET", "/v1/reach/unsubscribe", "", nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestReachSubscriptionAPI_Unauthenticated(t *testing.T) {
	ctx := setupE2E(t)
	w := ctx.doReq(t, "GET", "/v1/reach/subscription", "", nil)
	assert.NotEqual(t, http.StatusOK, w.Code)
}
