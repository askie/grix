package service

import (
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureReachSubscription_CreatesDefault(t *testing.T) {
	setupReachTestDB(t)

	sub, err := EnsureReachSubscription(1001, "cn")
	require.NoError(t, err)
	assert.Equal(t, int64(1001), sub.UserID)
	assert.True(t, sub.Subscribed, "cn region defaults to subscribed")
	assert.NotEmpty(t, sub.UnsubToken)
}

func TestEnsureReachSubscription_GlobalDefaultUnsubscribed(t *testing.T) {
	setupReachTestDB(t)

	sub, err := EnsureReachSubscription(2002, "global")
	require.NoError(t, err)
	assert.False(t, sub.Subscribed, "global region defaults to unsubscribed (GDPR)")
}

func TestEnsureReachSubscription_Idempotent(t *testing.T) {
	setupReachTestDB(t)

	first, err := EnsureReachSubscription(3003, "cn")
	require.NoError(t, err)
	second, err := EnsureReachSubscription(3003, "cn")
	require.NoError(t, err)
	assert.Equal(t, first.UserID, second.UserID)
	assert.Equal(t, first.UnsubToken, second.UnsubToken)
}

func TestUpdateReachSubscription(t *testing.T) {
	setupReachTestDB(t)

	_, err := EnsureReachSubscription(4004, "cn")
	require.NoError(t, err)

	require.NoError(t, UpdateReachSubscription(4004, false))
	sub, err := GetReachSubscription(4004)
	require.NoError(t, err)
	assert.False(t, sub.Subscribed)

	require.NoError(t, UpdateReachSubscription(4004, true))
	sub, err = GetReachSubscription(4004)
	require.NoError(t, err)
	assert.True(t, sub.Subscribed)
}

func TestUnsubscribeByToken(t *testing.T) {
	setupReachTestDB(t)

	sub, err := EnsureReachSubscription(5005, "cn")
	require.NoError(t, err)
	assert.True(t, sub.Subscribed)

	require.NoError(t, UnsubscribeByToken(sub.UnsubToken))

	var loaded model.ReachSubscription
	require.NoError(t, store.DB.Where("user_id = ?", 5005).First(&loaded).Error)
	assert.False(t, loaded.Subscribed)
}

func TestUnsubscribeByToken_AlreadyUnsubscribed(t *testing.T) {
	setupReachTestDB(t)

	sub, _ := EnsureReachSubscription(6006, "cn")
	UnsubscribeByToken(sub.UnsubToken)
	assert.Error(t, UnsubscribeByToken(sub.UnsubToken), "should fail on double unsub")
}

func TestUnsubscribeByToken_EmptyToken(t *testing.T) {
	setupReachTestDB(t)
	assert.Error(t, UnsubscribeByToken(""))
}

func TestIsUserSubscribedForReach(t *testing.T) {
	setupReachTestDB(t)

	assert.True(t, IsUserSubscribedForReach(7007), "no record = default subscribed")

	EnsureReachSubscription(7007, "cn")
	assert.True(t, IsUserSubscribedForReach(7007))

	UpdateReachSubscription(7007, false)
	assert.False(t, IsUserSubscribedForReach(7007))
}
