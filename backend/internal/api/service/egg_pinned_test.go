package service

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/require"
)

func setupEggPinnedTest(t *testing.T) {
	t.Helper()
	logger.Init()
	config.C.JWT.Secret = "test-test-test-test-test-test-test-test"
	testDB := testutil.NewTestDB()
	origDB, origRDB := store.DB, store.RDB
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		store.DB = origDB
		store.RDB = origRDB
		testDB.Close()
	})

	require.NoError(t, store.DB.Create(&model.EggCategory{
		ID:     "cat1",
		Code:   "cat1",
		Status: model.EggCategoryStatusActive,
	}).Error)
	require.NoError(t, store.DB.Create(&model.EggCategoryI18n{
		CategoryID: "cat1",
		Locale:     "en-US",
		Name:       "Category One",
	}).Error)
}

func seedPublishedEgg(t *testing.T, id string, installCount int64, pinnedAt *time.Time) {
	t.Helper()
	require.NoError(t, store.DB.Create(&model.Egg{
		ID:           id,
		CategoryID:   "cat1",
		PackageType:  "skill_zip",
		Status:       model.EggStatusPublished,
		InstallCount: installCount,
		PinnedAt:     pinnedAt,
	}).Error)
	require.NoError(t, store.DB.Create(&model.EggI18n{
		EggID:  id,
		Locale: "en-US",
		Name:   id + " name",
	}).Error)
	require.NoError(t, store.DB.Create(&model.EggVersion{
		EggID:     id,
		Version:   1,
		ZipURL:    "https://example.com/" + id + ".zip",
		ZipSHA256: "sha",
		ZipSize:   1,
	}).Error)
	require.NoError(t, store.DB.Create(&model.EggVersionI18n{
		EggID:       id,
		Version:     1,
		Locale:      "en-US",
		VersionDesc: "v1",
	}).Error)
}

func TestEggSearchPinnedSortsFirst(t *testing.T) {
	setupEggPinnedTest(t)

	now := time.Now()
	seedPublishedEgg(t, "egg-popular", 1000, nil)
	seedPublishedEgg(t, "egg-pinned", 1, &now)

	resp, ec := EggSearch(1, EggSearchReq{Locale: "en-US", Page: 1, PageSize: 10})
	require.Nil(t, ec)
	require.Len(t, resp.List, 2)
	require.Equal(t, "egg-pinned", resp.List[0].ID, "pinned egg should rank first despite lower install_count")
	require.Equal(t, "egg-popular", resp.List[1].ID)
}

func TestAdminEggSetPinnedRoundTrip(t *testing.T) {
	setupEggPinnedTest(t)
	seedPublishedEgg(t, "egg-a", 5, nil)

	detail, ec := AdminEggGet("egg-a")
	require.Nil(t, ec)
	require.False(t, detail.Pinned)

	ec = AdminEggSetPinned("egg-a", AdminEggPinReq{Pinned: true})
	require.Nil(t, ec)

	detail, ec = AdminEggGet("egg-a")
	require.Nil(t, ec)
	require.True(t, detail.Pinned)
	require.NotZero(t, detail.PinnedAt)

	ec = AdminEggSetPinned("egg-a", AdminEggPinReq{Pinned: false})
	require.Nil(t, ec)

	detail, ec = AdminEggGet("egg-a")
	require.Nil(t, ec)
	require.False(t, detail.Pinned)
	require.Zero(t, detail.PinnedAt)
}

func TestAdminEggSetPinnedNotFound(t *testing.T) {
	setupEggPinnedTest(t)
	ec := AdminEggSetPinned("no-such-egg", AdminEggPinReq{Pinned: true})
	require.NotNil(t, ec)
	require.Equal(t, 404, ec.HTTPStatus)
}
