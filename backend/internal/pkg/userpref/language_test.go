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

func TestNormalizeLanguage(t *testing.T) {
	cases := map[string]string{
		"zh":      "zh",
		"zh-CN":   "zh",
		"zh_Hans": "zh",
		"EN":      "en",
		"en-US":   "en",
		"ja_JP":   "ja",
		"pt-BR":   "pt",
		"ar":      "ar",
		"":        DefaultLanguage,
		"xx":      DefaultLanguage,
		"  ":      DefaultLanguage,
	}
	for raw, want := range cases {
		require.Equal(t, want, NormalizeLanguage(raw), "NormalizeLanguage(%q)", raw)
	}
}

func TestMatchLanguage(t *testing.T) {
	lang, ok := MatchLanguage("en-US")
	require.True(t, ok)
	require.Equal(t, "en", lang)

	lang, ok = MatchLanguage("xx")
	require.False(t, ok)
	require.Equal(t, DefaultLanguage, lang)

	_, ok = MatchLanguage("")
	require.False(t, ok)
}

func TestLanguage_ReadsNormalized(t *testing.T) {
	setupUserPrefTestDB(t)
	require.NoError(t, store.DB.Create(&model.UserSetting{UserID: 1004, PreferredLanguage: "en"}).Error)

	require.Equal(t, "en", Language(context.Background(), 1004))
	require.Equal(t, DefaultLanguage, Language(context.Background(), 9998))
	require.Equal(t, DefaultLanguage, Language(context.Background(), 0))
}
