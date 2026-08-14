package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/require"
)

// seedAdminSession 直接在测试 DB 里种一条 super admin + AdminSession。
// 返回的 rawToken 用于 Authorization: Bearer <token>。
// 服务端存的是 sha256(rawToken)。
func seedAdminSession(t *testing.T) string {
	t.Helper()

	admin := &model.AdminUser{
		ID:           time.Now().UnixNano(),
		Username:     fmt.Sprintf("e2e_admin_%d", time.Now().UnixNano()),
		PasswordHash: "unused",
		Nickname:     "E2E Admin",
		Role:         model.AdminRoleSuperAdmin,
		Status:       model.AdminStatusActive,
	}
	require.NoError(t, store.DB.Create(admin).Error)

	rawToken := fmt.Sprintf("e2e-admin-token-%d", time.Now().UnixNano())
	sum := sha256.Sum256([]byte(rawToken))
	hashed := hex.EncodeToString(sum[:])

	session := &model.AdminSession{
		SessionID:  hashed,
		AdminID:    admin.ID,
		ExpiresAt:  time.Now().UTC().Add(1 * time.Hour),
		LastSeenAt: time.Now().UTC(),
	}
	require.NoError(t, store.DB.Create(session).Error)
	return rawToken
}

// seedEggMarketFixture 建 1 个 active category + 若干条 published egg（含 i18n 与 version）。
type eggFixture struct {
	ID           string
	InstallCount int64
	PinnedAt     *time.Time
}

func seedEggMarketFixture(t *testing.T, categoryID string, eggs []eggFixture) {
	t.Helper()

	require.NoError(t, store.DB.Create(&model.EggCategory{
		ID:     categoryID,
		Code:   categoryID,
		Status: model.EggCategoryStatusActive,
	}).Error)
	require.NoError(t, store.DB.Create(&model.EggCategoryI18n{
		CategoryID: categoryID,
		Locale:     "en-US",
		Name:       "Category " + categoryID,
	}).Error)

	for _, e := range eggs {
		require.NoError(t, store.DB.Create(&model.Egg{
			ID:           e.ID,
			CategoryID:   categoryID,
			PackageType:  "skill_zip",
			Status:       model.EggStatusPublished,
			InstallCount: e.InstallCount,
			PinnedAt:     e.PinnedAt,
		}).Error)
		require.NoError(t, store.DB.Create(&model.EggI18n{
			EggID:  e.ID,
			Locale: "en-US",
			Name:   e.ID + " name",
		}).Error)
		require.NoError(t, store.DB.Create(&model.EggVersion{
			EggID:     e.ID,
			Version:   1,
			ZipURL:    "https://example.com/" + e.ID + ".zip",
			ZipSHA256: "sha",
			ZipSize:   1,
		}).Error)
		require.NoError(t, store.DB.Create(&model.EggVersionI18n{
			EggID:       e.ID,
			Version:     1,
			Locale:      "en-US",
			VersionDesc: "v1",
		}).Error)
	}
}

func searchEggIDs(t *testing.T, ctx *e2eContext, userToken string) []string {
	t.Helper()
	w := ctx.doReq(t, "GET", "/v1/eggs/search?locale=en-US&page=1&page_size=20", userToken, nil)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	res := parseResp(t, w)
	data, ok := res["data"].(map[string]interface{})
	require.True(t, ok, "missing data field: %s", w.Body.String())
	rawList, ok := data["list"].([]interface{})
	require.True(t, ok, "missing list field: %s", w.Body.String())
	ids := make([]string, 0, len(rawList))
	for _, raw := range rawList {
		item, ok := raw.(map[string]interface{})
		require.True(t, ok)
		id, _ := item["id"].(string)
		ids = append(ids, id)
	}
	return ids
}

// TestEggPinnedE2E 全流程：
// 1) 高装机量 A、置顶低装机量 B、更早置顶中装机量 C -> 用户 search 顺序 B, C, A
// 2) admin 置顶 A -> 顺序变成 A, B, C
// 3) admin 取消置顶 A -> 顺序恢复 B, C, A
// 4) admin 对不存在的 egg 置顶 -> 404
// 5) admin list + get 详情返回 pinned/pinned_at 字段
func TestEggPinnedE2E(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()

	userToken, _ := ctx.loginHelper(t, "e2e_pin_user_"+time.Now().Format("150405"), "pwd12345")
	adminToken := seedAdminSession(t)

	now := time.Now().UTC()
	earlier := now.Add(-time.Hour)
	seedEggMarketFixture(t, "cat-pin", []eggFixture{
		{ID: "egg-A-popular", InstallCount: 1000, PinnedAt: nil},
		{ID: "egg-B-pinned-now", InstallCount: 1, PinnedAt: &now},
		{ID: "egg-C-pinned-earlier", InstallCount: 100, PinnedAt: &earlier},
	})

	// 1) 初始排序：置顶时间 DESC NULLS LAST -> B (now), C (earlier), A (nil, 装机量最高但没置顶)
	ids := searchEggIDs(t, ctx, userToken)
	require.Equal(t, []string{"egg-B-pinned-now", "egg-C-pinned-earlier", "egg-A-popular"}, ids, "initial order wrong")

	// 2) 通过 admin 接口置顶 A（比 B 更新的 pinned_at）
	// 隔一点点，防止 SQLite time 精度导致 pinned_at 与 B 相等
	time.Sleep(20 * time.Millisecond)
	w := ctx.doReq(t, "POST", "/admin/api/eggs/egg-A-popular/pin", adminToken, map[string]interface{}{
		"pinned": true,
	})
	require.Equal(t, http.StatusOK, w.Code, "pin A body=%s", w.Body.String())

	ids = searchEggIDs(t, ctx, userToken)
	require.Equal(t, []string{"egg-A-popular", "egg-B-pinned-now", "egg-C-pinned-earlier"}, ids, "after pinning A")

	// 3) 取消置顶 A -> 恢复原顺序
	w = ctx.doReq(t, "POST", "/admin/api/eggs/egg-A-popular/pin", adminToken, map[string]interface{}{
		"pinned": false,
	})
	require.Equal(t, http.StatusOK, w.Code, "unpin A body=%s", w.Body.String())

	ids = searchEggIDs(t, ctx, userToken)
	require.Equal(t, []string{"egg-B-pinned-now", "egg-C-pinned-earlier", "egg-A-popular"}, ids, "after unpinning A")

	// 4) 对不存在的 egg id -> 404
	w = ctx.doReq(t, "POST", "/admin/api/eggs/no-such-egg-id/pin", adminToken, map[string]interface{}{
		"pinned": true,
	})
	require.Equal(t, http.StatusNotFound, w.Code, "expected 404 for nonexistent egg, body=%s", w.Body.String())

	// 4.1) 不存在的 egg 置顶失败，不应影响其他 egg 的置顶状态
	ids = searchEggIDs(t, ctx, userToken)
	require.Equal(t, []string{"egg-B-pinned-now", "egg-C-pinned-earlier", "egg-A-popular"}, ids, "sort corrupted after failed 404 pin")

	// 5.1) admin list：B/C pinned=true+pinned_at 有值；A pinned=false 且 pinned_at 字段应被 omitempty 掉
	w = ctx.doReq(t, "GET", "/admin/api/eggs?page=1&page_size=20", adminToken, nil)
	require.Equal(t, http.StatusOK, w.Code, "admin list body=%s", w.Body.String())
	adminList := parseResp(t, w)
	data, _ := adminList["data"].(map[string]interface{})
	require.NotNil(t, data, "admin list missing data")
	rawList, _ := data["list"].([]interface{})
	require.NotEmpty(t, rawList, "admin list empty")

	pinnedByID := make(map[string]map[string]interface{})
	for _, raw := range rawList {
		item := raw.(map[string]interface{})
		pinnedByID[item["id"].(string)] = item
	}
	require.Contains(t, pinnedByID, "egg-B-pinned-now")
	require.Contains(t, pinnedByID, "egg-C-pinned-earlier")
	require.Contains(t, pinnedByID, "egg-A-popular")

	require.Equal(t, true, pinnedByID["egg-B-pinned-now"]["pinned"], "B should be pinned")
	require.NotNil(t, pinnedByID["egg-B-pinned-now"]["pinned_at"], "B pinned_at should be present")

	require.Equal(t, true, pinnedByID["egg-C-pinned-earlier"]["pinned"], "C should be pinned")
	require.NotNil(t, pinnedByID["egg-C-pinned-earlier"]["pinned_at"], "C pinned_at should be present")

	require.Equal(t, false, pinnedByID["egg-A-popular"]["pinned"], "A should be unpinned")
	// omitempty: 对未置顶的 egg 不应下发 pinned_at 键（Go 侧 int64 零值触发 omitempty）
	_, hasPinnedAtA := pinnedByID["egg-A-popular"]["pinned_at"]
	require.False(t, hasPinnedAtA, "unpinned A should not carry pinned_at key: %v", pinnedByID["egg-A-popular"])

	// 5.2) admin get 详情：pinned+pinned_at
	w = ctx.doReq(t, "GET", "/admin/api/eggs/egg-B-pinned-now", adminToken, nil)
	require.Equal(t, http.StatusOK, w.Code, "admin get body=%s", w.Body.String())
	detail := parseResp(t, w)
	detailData, _ := detail["data"].(map[string]interface{})
	require.NotNil(t, detailData)
	dataB, _ := detailData["egg"].(map[string]interface{})
	require.NotNil(t, dataB)
	require.Equal(t, true, dataB["pinned"], "B detail pinned=true")
	pinnedAtRaw, ok := dataB["pinned_at"].(json.Number)
	require.True(t, ok, "B pinned_at should be a number, got %T", dataB["pinned_at"])
	pinnedAt, err := pinnedAtRaw.Int64()
	require.NoError(t, err)
	require.Greater(t, pinnedAt, int64(0))

	// 未置顶的 A 详情：pinned=false，pinned_at 键应被 omit
	w = ctx.doReq(t, "GET", "/admin/api/eggs/egg-A-popular", adminToken, nil)
	require.Equal(t, http.StatusOK, w.Code)
	detailA := parseResp(t, w)
	detailDataA, _ := detailA["data"].(map[string]interface{})
	dataA, _ := detailDataA["egg"].(map[string]interface{})
	require.Equal(t, false, dataA["pinned"])
	_, hasKey := dataA["pinned_at"]
	require.False(t, hasKey, "unpinned A detail should not carry pinned_at key: %v", dataA)
}

// TestEggPinnedE2E_UnpinIsIdempotent 反复取消置顶（本身就没置顶）也应正常返回 OK。
// 边界：pinned=false + pinned_at 已是 NULL，SQL update 依然会命中一行（写 NULL+updated_at），
// 因此不应触发 404，也不应把该 egg 从列表里弄没。
func TestEggPinnedE2E_UnpinIsIdempotent(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()

	userToken, _ := ctx.loginHelper(t, "e2e_pin_idem_user_"+time.Now().Format("150405"), "pwd12345")
	adminToken := seedAdminSession(t)

	seedEggMarketFixture(t, "cat-idem", []eggFixture{
		{ID: "egg-idem", InstallCount: 5, PinnedAt: nil},
	})

	w := ctx.doReq(t, "POST", "/admin/api/eggs/egg-idem/pin", adminToken, map[string]interface{}{
		"pinned": false,
	})
	require.Equal(t, http.StatusOK, w.Code, "unpin unpinned should still be 200, body=%s", w.Body.String())

	ids := searchEggIDs(t, ctx, userToken)
	require.Equal(t, []string{"egg-idem"}, ids)
}
