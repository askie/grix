package service

import (
	"errors"
	"testing"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

func setupWidgetIPBanServiceTest(t *testing.T) *testutil.TestDB {
	t.Helper()
	logger.Init()
	config.C.JWT.Secret = "widget-ip-ban-service-test-secret-0123456789"
	require.NoError(t, snowflake.Init(1))
	tdb := testutil.NewTestDB()
	store.DB = tdb.DB
	store.RDB = nil
	return tdb
}

func seedWidgetSessionWithIP(t *testing.T, id, ownerUserID int64, sessionID, lastInitIP string) {
	t.Helper()
	now := time.Now().UTC()
	require.NoError(t, store.DB.Create(&model.WidgetSession{
		ID:           id,
		SiteID:       id + 10000,
		OwnerUserID:  ownerUserID,
		VisitorID:    id + 20000,
		VisitorKey:   "vk_" + sessionID,
		SessionID:    sessionID,
		LastInitIP:   lastInitIP,
		Status:       model.WidgetSessionStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
		LastActiveAt: now,
	}).Error)
}

func listWidgetIPBanRows(t *testing.T, ownerUserID int64) []model.WidgetIPBan {
	t.Helper()
	var rules []model.WidgetIPBan
	require.NoError(t, store.DB.Where("owner_user_id = ?", ownerUserID).Find(&rules).Error)
	return rules
}

func TestWidgetSessionBanWritesIPBan(t *testing.T) {
	tdb := setupWidgetIPBanServiceTest(t)
	defer tdb.Close()
	const owner = int64(8101)
	seedWidgetSessionWithIP(t, 8101, owner, "ws_ip1", "203.0.113.7")

	dto, err := WidgetSessionBan(WidgetSessionStatusUpdateInput{OwnerUserID: owner, SessionID: "ws_ip1"})
	require.NoError(t, err)
	assert.Equal(t, model.WidgetSessionStatusBanned, dto.Status)

	rules := listWidgetIPBanRows(t, owner)
	require.Len(t, rules, 1)
	assert.Equal(t, "203.0.113.7", rules[0].IPCIDR)
	assert.Equal(t, "session_ban", rules[0].Reason)
	assert.Equal(t, "ws_ip1", rules[0].SourceSessionID)
	require.NotNil(t, rules[0].ExpiresAt)
	// 默认 7 天过期
	assert.WithinDuration(t, time.Now().UTC().Add(security.WidgetIPBanDefaultTTL), *rules[0].ExpiresAt, time.Minute)
	// 签名有效且判定立刻生效（BanWidgetIP 已失效缓存）
	assert.True(t, security.VerifyWidgetIPBan(&rules[0]))
	assert.True(t, security.IsWidgetIPBanned(owner, "203.0.113.7"))
	// visitor_key 封禁与 IP 封禁独立：其他 owner 不受影响
	assert.False(t, security.IsWidgetIPBanned(owner+1, "203.0.113.7"))
}

func TestWidgetSessionBanSkipsIPBanWhenLastInitIPEmpty(t *testing.T) {
	tdb := setupWidgetIPBanServiceTest(t)
	defer tdb.Close()
	const owner = int64(8102)
	seedWidgetSessionWithIP(t, 8102, owner, "ws_ip2", "")

	_, err := WidgetSessionBan(WidgetSessionStatusUpdateInput{OwnerUserID: owner, SessionID: "ws_ip2"})
	require.NoError(t, err)
	assert.Empty(t, listWidgetIPBanRows(t, owner))
}

func TestWidgetSessionBanRebanRefreshesExpiry(t *testing.T) {
	tdb := setupWidgetIPBanServiceTest(t)
	defer tdb.Close()
	const owner = int64(8103)
	seedWidgetSessionWithIP(t, 8103, owner, "ws_ip3", "203.0.113.9")

	_, err := WidgetSessionBan(WidgetSessionStatusUpdateInput{OwnerUserID: owner, SessionID: "ws_ip3"})
	require.NoError(t, err)
	first := listWidgetIPBanRows(t, owner)
	require.Len(t, first, 1)
	require.NotNil(t, first[0].ExpiresAt)

	// 跨过签名/截断的 1 秒粒度后再次 ban 同 IP：不新增行，刷新过期时间
	time.Sleep(1100 * time.Millisecond)
	_, err = WidgetSessionBan(WidgetSessionStatusUpdateInput{OwnerUserID: owner, SessionID: "ws_ip3"})
	require.NoError(t, err)

	second := listWidgetIPBanRows(t, owner)
	require.Len(t, second, 1)
	assert.Equal(t, first[0].ID, second[0].ID)
	require.NotNil(t, second[0].ExpiresAt)
	assert.True(t, second[0].ExpiresAt.After(*first[0].ExpiresAt),
		"再次 ban 应刷新 expires_at: first=%v second=%v", first[0].ExpiresAt, second[0].ExpiresAt)
	assert.True(t, security.VerifyWidgetIPBan(&second[0]))
}

func TestWidgetIPBanListAndDeleteOwnership(t *testing.T) {
	tdb := setupWidgetIPBanServiceTest(t)
	defer tdb.Close()
	const owner = int64(8104)
	const stranger = int64(8105)
	const ip = "203.0.113.10"

	require.NoError(t, security.BanWidgetIP(owner, ip, "session_ban", "ws_x", security.WidgetIPBanDefaultTTL))
	rules := listWidgetIPBanRows(t, owner)
	require.Len(t, rules, 1)
	ruleID := rules[0].ID

	// 属主可见列表
	list, err := WidgetIPBanList(owner)
	require.NoError(t, err)
	require.Equal(t, int64(1), list.Total)
	require.Len(t, list.Items, 1)
	assert.Equal(t, ruleID, list.Items[0].ID)
	assert.Equal(t, ip, list.Items[0].IPCIDR)
	assert.False(t, list.Items[0].Expired)
	assert.Greater(t, list.Items[0].ExpiresAt, time.Now().Unix())

	// 其他 owner 不可见、不可删
	otherList, err := WidgetIPBanList(stranger)
	require.NoError(t, err)
	assert.Equal(t, int64(0), otherList.Total)
	err = WidgetIPBanDelete(stranger, ruleID)
	assert.True(t, errors.Is(err, ErrWidgetIPBanNotOwned), "删别人的规则应返回 not found, got %v", err)

	// 非法入参
	assert.True(t, errors.Is(WidgetIPBanDelete(0, ruleID), ErrWidgetSiteInvalidInput))
	assert.True(t, errors.Is(WidgetIPBanDelete(owner, 0), ErrWidgetSiteInvalidInput))

	// 属主删除成功，判定缓存同步失效
	require.NoError(t, WidgetIPBanDelete(owner, ruleID))
	assert.Empty(t, listWidgetIPBanRows(t, owner))
	assert.False(t, security.IsWidgetIPBanned(owner, ip))
}

func TestWidgetVisitorInitIPBanned(t *testing.T) {
	tdb := setupWidgetIPBanServiceTest(t)
	defer tdb.Close()
	const owner = int64(8201)
	seedWidgetSite(t, model.WidgetSite{
		ID:             8201,
		OwnerUserID:    owner,
		SiteKey:        "wk_ipban",
		SiteSecretHash: "hash_ipban",
		SiteName:       "IPBan",
		AllowedOrigins: datatypes.JSON([]byte(`["https://ipban.example.com"]`)),
		Status:         model.WidgetSiteStatusActive,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})
	require.NoError(t, security.BanWidgetIP(owner, "203.0.113.12", "session_ban", "ws_seed", security.WidgetIPBanDefaultTTL))

	// 被封 IP：init 直接拒绝
	_, err := WidgetVisitorInit(WidgetVisitorInitInput{
		SiteKey:  "wk_ipban",
		PageURL:  "https://ipban.example.com/p/1",
		Origin:   "https://ipban.example.com",
		WSURL:    "wss://api.example.com/v1/widget/ws",
		ClientIP: "203.0.113.12",
	})
	assert.True(t, errors.Is(err, ErrWidgetIPBanned), "expected ErrWidgetIPBanned, got %v", err)

	// 未封 IP：正常 init，且写入 last_init_ip
	resp, err := WidgetVisitorInit(WidgetVisitorInitInput{
		SiteKey:    "wk_ipban",
		VisitorKey: "vk_ok_1",
		PageURL:    "https://ipban.example.com/p/1",
		Origin:     "https://ipban.example.com",
		WSURL:      "wss://api.example.com/v1/widget/ws",
		ClientIP:   "203.0.113.13",
	})
	require.NoError(t, err)
	var session model.WidgetSession
	require.NoError(t, store.DB.Where("session_id = ?", resp.SessionID).First(&session).Error)
	assert.Equal(t, "203.0.113.13", session.LastInitIP)

	// 空 ClientIP：不触发 IP 封禁，init 正常
	_, err = WidgetVisitorInit(WidgetVisitorInitInput{
		SiteKey:    "wk_ipban",
		VisitorKey: "vk_ok_2",
		PageURL:    "https://ipban.example.com/p/2",
		Origin:     "https://ipban.example.com",
		WSURL:      "wss://api.example.com/v1/widget/ws",
	})
	require.NoError(t, err)
}
