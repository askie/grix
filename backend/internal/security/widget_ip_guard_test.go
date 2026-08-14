package security

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 注意：widgetIPBanCache 是包级共享缓存，各用例使用不同的 ownerUserID 隔离，
// 避免跨用例缓存污染。

func setupWidgetIPGuardTest(t *testing.T) {
	t.Helper()
	logger.Init()
	config.C.JWT.Secret = "test-jwt-secret-for-widget-ip-guard-0123456789"
	require.NoError(t, snowflake.Init(1))
	testDB := testutil.NewTestDB()
	origDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = origDB
		testDB.Close()
	})
}

func countWidgetIPBans(t *testing.T, ownerUserID int64) int64 {
	t.Helper()
	var cnt int64
	require.NoError(t, store.DB.Model(&model.WidgetIPBan{}).Where("owner_user_id = ?", ownerUserID).Count(&cnt).Error)
	return cnt
}

func TestSignAndVerifyWidgetIPBan(t *testing.T) {
	setupWidgetIPGuardTest(t)
	expires := time.Now().UTC().Add(WidgetIPBanDefaultTTL).Truncate(time.Second)
	sig := SignWidgetIPBan(101, "203.0.113.7", &expires)
	assert.NotEmpty(t, sig)

	rule := &model.WidgetIPBan{OwnerUserID: 101, IPCIDR: "203.0.113.7", ExpiresAt: &expires, Signature: sig}
	assert.True(t, VerifyWidgetIPBan(rule))

	// 篡改 IP / 过期时间 / owner 后验签失败
	tampered := *rule
	tampered.IPCIDR = "203.0.113.8"
	assert.False(t, VerifyWidgetIPBan(&tampered))

	tampered = *rule
	later := expires.Add(time.Hour)
	tampered.ExpiresAt = &later
	assert.False(t, VerifyWidgetIPBan(&tampered))

	tampered = *rule
	tampered.OwnerUserID = 102
	assert.False(t, VerifyWidgetIPBan(&tampered))

	// nil 规则 / 永不过期规则
	assert.False(t, VerifyWidgetIPBan(nil))
	neverSig := SignWidgetIPBan(101, "203.0.113.7", nil)
	assert.True(t, VerifyWidgetIPBan(&model.WidgetIPBan{OwnerUserID: 101, IPCIDR: "203.0.113.7", Signature: neverSig}))
}

func TestIsWidgetIPBannedSingleIPAndCIDR(t *testing.T) {
	setupWidgetIPGuardTest(t)
	const owner = int64(7101)

	// 单 IP 规则（经 BanWidgetIP 写入）
	require.NoError(t, BanWidgetIP(owner, "203.0.113.7", "test", "ws_1", WidgetIPBanDefaultTTL))
	// CIDR 规则（手工插入并正确签名）
	expires := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	require.NoError(t, store.DB.Create(&model.WidgetIPBan{
		ID:          snowflake.GenID(),
		OwnerUserID: owner,
		IPCIDR:      "198.51.100.0/24",
		ExpiresAt:   &expires,
		Signature:   SignWidgetIPBan(owner, "198.51.100.0/24", &expires),
	}).Error)
	InvalidateWidgetIPBanCache(owner)

	assert.True(t, IsWidgetIPBanned(owner, "203.0.113.7"))
	assert.False(t, IsWidgetIPBanned(owner, "203.0.113.8"))
	assert.True(t, IsWidgetIPBanned(owner, "198.51.100.66"))
	assert.False(t, IsWidgetIPBanned(owner, "198.51.101.66"))
	// owner 全局但隔离：其他 owner 不受影响
	assert.False(t, IsWidgetIPBanned(owner+1, "203.0.113.7"))
	assert.False(t, IsWidgetIPBanned(owner+1, "198.51.100.66"))
	// 空 IP / 非法 owner 一律放行
	assert.False(t, IsWidgetIPBanned(owner, ""))
	assert.False(t, IsWidgetIPBanned(0, "203.0.113.7"))
}

func TestIsWidgetIPBannedExpiredRuleIgnored(t *testing.T) {
	setupWidgetIPGuardTest(t)
	const owner = int64(7102)

	// 已过期但签名正确的规则不生效
	expired := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	require.NoError(t, store.DB.Create(&model.WidgetIPBan{
		ID:          snowflake.GenID(),
		OwnerUserID: owner,
		IPCIDR:      "203.0.113.7",
		ExpiresAt:   &expired,
		Signature:   SignWidgetIPBan(owner, "203.0.113.7", &expired),
	}).Error)

	assert.False(t, IsWidgetIPBanned(owner, "203.0.113.7"))
}

func TestTamperedWidgetIPBanIgnored(t *testing.T) {
	setupWidgetIPGuardTest(t)
	const owner = int64(7103)

	// 伪造签名（模拟直改数据库）的规则不生效
	expires := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	require.NoError(t, store.DB.Create(&model.WidgetIPBan{
		ID:          snowflake.GenID(),
		OwnerUserID: owner,
		IPCIDR:      "203.0.113.7",
		ExpiresAt:   &expires,
		Signature:   "forged-signature",
	}).Error)

	assert.False(t, IsWidgetIPBanned(owner, "203.0.113.7"))
}

func TestBanWidgetIPUpsertRefreshesExpiry(t *testing.T) {
	setupWidgetIPGuardTest(t)
	const owner = int64(7104)

	require.NoError(t, BanWidgetIP(owner, "203.0.113.7", "r1", "ws_1", WidgetIPBanDefaultTTL))
	var first model.WidgetIPBan
	require.NoError(t, store.DB.Where("owner_user_id = ? AND ip_cidr = ?", owner, "203.0.113.7").First(&first).Error)
	require.NotNil(t, first.ExpiresAt)
	assert.Equal(t, "r1", first.Reason)
	assert.Equal(t, "ws_1", first.SourceSessionID)
	assert.True(t, VerifyWidgetIPBan(&first))
	// 默认 7 天过期
	assert.WithinDuration(t, time.Now().UTC().Add(WidgetIPBanDefaultTTL), *first.ExpiresAt, time.Minute)

	// 再次封禁同一 (owner, ip)：不新增行，刷新过期时间/reason/source，重新签名
	require.NoError(t, BanWidgetIP(owner, "203.0.113.7", "r2", "ws_2", WidgetIPBanDefaultTTL))
	assert.Equal(t, int64(1), countWidgetIPBans(t, owner))

	var second model.WidgetIPBan
	require.NoError(t, store.DB.Where("owner_user_id = ? AND ip_cidr = ?", owner, "203.0.113.7").First(&second).Error)
	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, "r2", second.Reason)
	assert.Equal(t, "ws_2", second.SourceSessionID)
	require.NotNil(t, second.ExpiresAt)
	assert.False(t, second.ExpiresAt.Before(*first.ExpiresAt))
	assert.True(t, VerifyWidgetIPBan(&second))
}

func TestBanWidgetIPSkipsEmptyOrInvalid(t *testing.T) {
	setupWidgetIPGuardTest(t)
	const owner = int64(7105)

	// 空 IP / 非法 IP / 非法 owner：静默跳过（返回 nil，不写库）
	assert.NoError(t, BanWidgetIP(owner, "", "r", "ws", WidgetIPBanDefaultTTL))
	assert.NoError(t, BanWidgetIP(owner, "   ", "r", "ws", WidgetIPBanDefaultTTL))
	assert.NoError(t, BanWidgetIP(owner, "not-an-ip", "r", "ws", WidgetIPBanDefaultTTL))
	assert.NoError(t, BanWidgetIP(0, "203.0.113.7", "r", "ws", WidgetIPBanDefaultTTL))
	assert.Equal(t, int64(0), countWidgetIPBans(t, owner))
	assert.Equal(t, int64(0), countWidgetIPBans(t, 0))
}

func TestWidgetIPBanCacheInvalidation(t *testing.T) {
	setupWidgetIPGuardTest(t)
	const owner = int64(7106)
	const ip = "203.0.113.7"

	// 空结果也会被缓存
	assert.False(t, IsWidgetIPBanned(owner, ip))

	// BanWidgetIP 主动失效缓存：封禁立刻生效，无需等 30s TTL
	require.NoError(t, BanWidgetIP(owner, ip, "r", "ws", WidgetIPBanDefaultTTL))
	assert.True(t, IsWidgetIPBanned(owner, ip))

	// 直删数据库（未失效缓存）：缓存 TTL 内仍判定封禁
	require.NoError(t, store.DB.Where("owner_user_id = ?", owner).Delete(&model.WidgetIPBan{}).Error)
	assert.True(t, IsWidgetIPBanned(owner, ip), "缓存 TTL 内应仍命中旧规则")

	// 失效缓存后立即放行
	InvalidateWidgetIPBanCache(owner)
	assert.False(t, IsWidgetIPBanned(owner, ip))
}
