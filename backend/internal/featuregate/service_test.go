package featuregate

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupServiceTestDB(t *testing.T) {
	t.Helper()
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	t.Cleanup(func() {
		testDB.Close()
		InvalidateCache()
	})
}

func resetServiceTime(t *testing.T, fn func() time.Time) {
	t.Helper()
	orig := now
	now = fn
	t.Cleanup(func() { now = orig })
}

func TestServiceGetUserFeatures_CacheHit(t *testing.T) {
	setupServiceTestDB(t)

	current := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)
	resetServiceTime(t, func() time.Time { return current })

	_, err := CreateGate("voice_call", "语音通话", model.FeatureStatusEnabled)
	require.NoError(t, err)

	// First call loads from DB and caches
	features, err := GetUserFeatures(100)
	require.NoError(t, err)
	assert.Contains(t, features, "voice_call")

	// Now change gate status directly in DB (bypass cache)
	store.DB.Model(&model.FeatureGate{}).Where("key = ?", "voice_call").
		Update("status", model.FeatureStatusDisabled)

	// Second call within cache TTL should still return cached result
	features, err = GetUserFeatures(100)
	require.NoError(t, err)
	assert.Contains(t, features, "voice_call") // still cached
}

func TestServiceGetUserFeatures_CacheExpiry(t *testing.T) {
	setupServiceTestDB(t)

	current := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)
	resetServiceTime(t, func() time.Time { return current })

	_, err := CreateGate("voice_call", "语音通话", model.FeatureStatusEnabled)
	require.NoError(t, err)

	features, err := GetUserFeatures(100)
	require.NoError(t, err)
	assert.Contains(t, features, "voice_call")

	// Change status in DB
	store.DB.Model(&model.FeatureGate{}).Where("key = ?", "voice_call").
		Update("status", model.FeatureStatusDisabled)

	// Advance time past cache TTL
	current = current.Add(cacheTTL + time.Second)

	// Cache expired, should read fresh data
	features, err = GetUserFeatures(100)
	require.NoError(t, err)
	assert.NotContains(t, features, "voice_call")
}

func TestServiceGetUserFeatures_WhitelistUser(t *testing.T) {
	setupServiceTestDB(t)

	_, err := CreateGate("voice_call", "语音通话", model.FeatureStatusWhitelist)
	require.NoError(t, err)

	err = AddUsersToWhitelist("voice_call", []int64{100})
	require.NoError(t, err)

	features, err := GetUserFeatures(100)
	require.NoError(t, err)
	assert.Contains(t, features, "voice_call")
}

func TestServiceGetUserFeatures_WhitelistOtherUser(t *testing.T) {
	setupServiceTestDB(t)

	_, err := CreateGate("voice_call", "语音通话", model.FeatureStatusWhitelist)
	require.NoError(t, err)

	err = AddUsersToWhitelist("voice_call", []int64{100})
	require.NoError(t, err)

	features, err := GetUserFeatures(200)
	require.NoError(t, err)
	assert.NotContains(t, features, "voice_call")
}

func TestServiceInvalidateCache(t *testing.T) {
	setupServiceTestDB(t)

	current := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)
	resetServiceTime(t, func() time.Time { return current })

	_, err := CreateGate("voice_call", "语音通话", model.FeatureStatusEnabled)
	require.NoError(t, err)

	features, err := GetUserFeatures(100)
	require.NoError(t, err)
	assert.Contains(t, features, "voice_call")

	// Change status
	store.DB.Model(&model.FeatureGate{}).Where("key = ?", "voice_call").
		Update("status", model.FeatureStatusDisabled)

	// Invalidate cache
	InvalidateCache()

	// Should read fresh data
	features, err = GetUserFeatures(100)
	require.NoError(t, err)
	assert.NotContains(t, features, "voice_call")
}

func TestServiceGetAllGates(t *testing.T) {
	setupServiceTestDB(t)

	_, err := CreateGate("voice_call", "语音通话", model.FeatureStatusDisabled)
	require.NoError(t, err)
	_, err = CreateGate("voice_delegate", "语音托管", model.FeatureStatusWhitelist)
	require.NoError(t, err)

	gates, err := GetAllGates()
	require.NoError(t, err)
	assert.Len(t, gates, 2)
}

func TestServiceSaveGate_UpdatesCache(t *testing.T) {
	setupServiceTestDB(t)

	_, err := CreateGate("voice_call", "语音通话", model.FeatureStatusDisabled)
	require.NoError(t, err)

	// Save (update) should update status and invalidate cache
	err = SaveGate("voice_call", "语音通话", model.FeatureStatusEnabled)
	require.NoError(t, err)

	// Cache should be invalidated, so next read gets fresh data
	features, err := GetUserFeatures(100)
	require.NoError(t, err)
	assert.Contains(t, features, "voice_call")
}
