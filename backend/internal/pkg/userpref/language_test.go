package userpref

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/require"
)

func setupUserPrefTestDB(t *testing.T) {
	t.Helper()
	testDB := testutil.NewTestDB()
	origDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = origDB
	})
	languageCacheMu.Lock()
	languageCache = make(map[int64]languageCacheEntry)
	languageCacheMu.Unlock()
}

func TestPreferredLanguage_ReadsFromDB(t *testing.T) {
	setupUserPrefTestDB(t)
	require.NoError(t, store.DB.Create(&model.UserSetting{UserID: 1001, PreferredLanguage: "en"}).Error)

	got := PreferredLanguage(context.Background(), 1001)
	require.Equal(t, "en", got)
}

func TestPreferredLanguage_DefaultsWhenMissing(t *testing.T) {
	setupUserPrefTestDB(t)

	got := PreferredLanguage(context.Background(), 9999)
	require.Equal(t, DefaultLanguage, got)
}

func TestPreferredLanguage_ZeroOrNegativeUserIDDefaultsWithoutQuery(t *testing.T) {
	setupUserPrefTestDB(t)

	require.Equal(t, DefaultLanguage, PreferredLanguage(context.Background(), 0))
	require.Equal(t, DefaultLanguage, PreferredLanguage(context.Background(), -1))
}

func TestPreferredLanguage_CachesAcrossCalls(t *testing.T) {
	setupUserPrefTestDB(t)
	require.NoError(t, store.DB.Create(&model.UserSetting{UserID: 1002, PreferredLanguage: "en"}).Error)

	require.Equal(t, "en", PreferredLanguage(context.Background(), 1002))

	// 缓存命中后即使库里的值变了，短期内读到的还是缓存值。
	require.NoError(t, store.DB.Model(&model.UserSetting{}).
		Where("user_id = ?", 1002).
		Update("preferred_language", "ja").Error)
	require.Equal(t, "en", PreferredLanguage(context.Background(), 1002))
}

func TestInvalidatePreferredLanguage_ForcesReload(t *testing.T) {
	setupUserPrefTestDB(t)
	require.NoError(t, store.DB.Create(&model.UserSetting{UserID: 1003, PreferredLanguage: "en"}).Error)
	require.Equal(t, "en", PreferredLanguage(context.Background(), 1003))

	require.NoError(t, store.DB.Model(&model.UserSetting{}).
		Where("user_id = ?", 1003).
		Update("preferred_language", "ja").Error)
	InvalidatePreferredLanguage(1003)

	require.Equal(t, "ja", PreferredLanguage(context.Background(), 1003))
}
