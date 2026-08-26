package service

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedInactiveTestUser(t *testing.T, id int64, region, nickname, email, last4 string) {
	t.Helper()
	require.NoError(t, store.DB.Create(&model.User{
		ID:         id,
		Username:   nickname + "_" + region,
		Nickname:   nickname,
		Email:      email,
		PhoneLast4: last4,
		Region:     region,
		Status:     model.UserStatusActive,
		CreatedAt:  time.Now().UTC().Add(-time.Hour),
	}).Error)
}

func seedAgentConnection(t *testing.T, ownerID int64, connectedAt time.Time) {
	t.Helper()
	require.NoError(t, store.DB.Create(&model.AgentConnectionLog{
		ID:          ownerID*1000 + connectedAt.Unix()%1000,
		AgentID:     ownerID + 1,
		OwnerID:     ownerID,
		ConnectedAt: connectedAt,
	}).Error)
}

func TestListInactiveAgentUsers_ExcludesRecentlyConnectedOwners(t *testing.T) {
	setupReachTestDB(t)

	seedInactiveTestUser(t, 9001, "cn", "silent", "silent@example.com", "8000")
	seedInactiveTestUser(t, 9002, "cn", "active", "active@example.com", "")
	seedInactiveTestUser(t, 9003, "global", "nevercreated", "", "")

	now := time.Now().UTC()
	seedAgentConnection(t, 9001, now.AddDate(0, 0, -40)) // 40 天前连过：仍算沉默
	seedAgentConnection(t, 9002, now.AddDate(0, 0, -1))  // 昨天连过：活跃

	result, ec := ListInactiveAgentUsers(ListInactiveAgentUsersReq{})
	require.Nil(t, ec)
	assert.Equal(t, inactiveAgentDefaultDays, result.NoAgentDays)
	assert.EqualValues(t, 2, result.Total)

	got := map[int64]InactiveAgentUser{}
	for _, u := range result.Users {
		got[u.UserID] = u
	}
	require.Contains(t, got, int64(9001))
	require.Contains(t, got, int64(9003), "从没建过 agent 的用户也要在人群里")
	assert.NotContains(t, got, int64(9002))

	// 手机号只下发脱敏串，没绑邮箱的人留空供后台标记。
	assert.Equal(t, "****8000", got[9001].PhoneMasked)
	assert.NotEmpty(t, got[9001].LastAgentConnectedAt)
	assert.Empty(t, got[9003].Email)
	assert.Empty(t, got[9003].LastAgentConnectedAt)
}

func TestListInactiveAgentUsers_DaysAndRegionFilters(t *testing.T) {
	setupReachTestDB(t)

	seedInactiveTestUser(t, 9101, "cn", "cnuser", "cn@example.com", "")
	seedInactiveTestUser(t, 9102, "global", "globaluser", "g@example.com", "")
	seedAgentConnection(t, 9101, time.Now().UTC().AddDate(0, 0, -10))

	// 默认 14 天：9101 十天前连过，算活跃。
	result, ec := ListInactiveAgentUsers(ListInactiveAgentUsersReq{})
	require.Nil(t, ec)
	assert.EqualValues(t, 1, result.Total)

	// 放宽到 7 天：9101 变成沉默用户。
	result, ec = ListInactiveAgentUsers(ListInactiveAgentUsersReq{NoAgentDays: 7})
	require.Nil(t, ec)
	assert.EqualValues(t, 2, result.Total)

	result, ec = ListInactiveAgentUsers(ListInactiveAgentUsersReq{NoAgentDays: 7, Region: "global"})
	require.Nil(t, ec)
	assert.EqualValues(t, 1, result.Total)
	require.Len(t, result.Users, 1)
	assert.Equal(t, int64(9102), result.Users[0].UserID)

	_, ec = ListInactiveAgentUsers(ListInactiveAgentUsersReq{Region: "mars"})
	assert.NotNil(t, ec, "未知 region 应当报参数错误而不是静默全量")
}

func TestListInactiveAgentUsers_CountsOnlyLiveAgents(t *testing.T) {
	setupReachTestDB(t)

	seedInactiveTestUser(t, 9201, "cn", "hasagents", "a@example.com", "")
	require.NoError(t, store.DB.Create(&model.Agent{ID: 1, OwnerID: 9201, AgentName: "live", Status: model.AgentStatusActive}).Error)
	require.NoError(t, store.DB.Create(&model.Agent{ID: 2, OwnerID: 9201, AgentName: "gone", Status: model.AgentStatusDeleted}).Error)

	result, ec := ListInactiveAgentUsers(ListInactiveAgentUsersReq{})
	require.Nil(t, ec)
	require.Len(t, result.Users, 1)
	assert.Equal(t, 1, result.Users[0].AgentTotal)
}

func TestDirectReachChannelOrder(t *testing.T) {
	assert.Equal(t, directReachDefaultChannels, directReachChannelOrder(nil))
	// 显式指定却全无效时返回空，由 SendDirectUserReach 判成参数错误，
	// 不能静默回落成含短信的默认顺序。
	assert.Empty(t, directReachChannelOrder([]string{"carrier_pigeon"}))
	assert.Equal(t, []string{"email"}, directReachChannelOrder([]string{" Email ", "email"}))
	assert.Equal(t, []string{"email", "in_app"}, directReachChannelOrder([]string{"email", "in_app", "bogus"}))
}

func TestIsUserSubscribedForMarketing_GlobalNeedsOptIn(t *testing.T) {
	setupReachTestDB(t)

	assert.False(t, IsUserSubscribedForMarketing(7101, "global"), "global 缺记录按未订阅跳过")
	assert.True(t, IsUserSubscribedForMarketing(7102, "cn"), "cn 缺记录沿用注册即订阅")
	// 旧口径仍然放行，确认没有把其他调用方一起收紧。
	assert.True(t, IsUserSubscribedForReach(7101))

	_, err := EnsureReachSubscription(7101, "cn")
	require.NoError(t, err)
	assert.True(t, IsUserSubscribedForMarketing(7101, "global"), "显式 opt-in 后可发")

	require.NoError(t, UpdateReachSubscription(7101, false))
	assert.False(t, IsUserSubscribedForMarketing(7101, "cn"))
}
