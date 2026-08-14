package featuregate

import (
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) {
	t.Helper()
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	t.Cleanup(func() { testDB.Close() })
}

func TestBuiltinFeaturesIncludeConversationAudit(t *testing.T) {
	for _, feature := range BuiltinFeatures {
		if feature.Key == "conversation_audit" {
			assert.Equal(t, "对话审计", feature.DisplayName)
			return
		}
	}
	t.Fatal("conversation_audit must be available in Admin Feature Gate")
}

// --- CreateGate / GetGate ---

func TestCreateGate(t *testing.T) {
	setupTestDB(t)

	gate, err := CreateGate("voice_call", "语音通话", model.FeatureStatusDisabled)
	require.NoError(t, err)
	assert.Equal(t, "voice_call", gate.Key)
	assert.Equal(t, "语音通话", gate.DisplayName)
	assert.Equal(t, model.FeatureStatusDisabled, gate.Status)
}

func TestCreateGateDuplicateKeyFails(t *testing.T) {
	setupTestDB(t)

	_, err := CreateGate("voice_call", "语音通话", model.FeatureStatusDisabled)
	require.NoError(t, err)

	_, err = CreateGate("voice_call", "语音通话2", model.FeatureStatusEnabled)
	assert.Error(t, err) // duplicate key
}

func TestGetGate(t *testing.T) {
	setupTestDB(t)

	_, err := CreateGate("voice_call", "语音通话", model.FeatureStatusWhitelist)
	require.NoError(t, err)

	gate, err := GetGate("voice_call")
	require.NoError(t, err)
	assert.Equal(t, "语音通话", gate.DisplayName)
	assert.Equal(t, model.FeatureStatusWhitelist, gate.Status)
}

func TestGetGateNotFound(t *testing.T) {
	setupTestDB(t)

	_, err := GetGate("nonexistent")
	assert.Error(t, err)
}

// --- UpdateGateStatus ---

func TestUpdateGateStatus(t *testing.T) {
	setupTestDB(t)

	_, err := CreateGate("voice_call", "语音通话", model.FeatureStatusDisabled)
	require.NoError(t, err)

	err = UpdateGateStatus("voice_call", model.FeatureStatusEnabled)
	require.NoError(t, err)

	gate, err := GetGate("voice_call")
	require.NoError(t, err)
	assert.Equal(t, model.FeatureStatusEnabled, gate.Status)
}

func TestUpdateGateStatusInvalidStatus(t *testing.T) {
	setupTestDB(t)

	_, err := CreateGate("voice_call", "语音通话", model.FeatureStatusDisabled)
	require.NoError(t, err)

	err = UpdateGateStatus("voice_call", "invalid_status")
	assert.Error(t, err)
}

// --- ListGates ---

func TestListGates(t *testing.T) {
	setupTestDB(t)

	_, err := CreateGate("voice_call", "语音通话", model.FeatureStatusDisabled)
	require.NoError(t, err)
	_, err = CreateGate("voice_delegate", "语音托管", model.FeatureStatusWhitelist)
	require.NoError(t, err)

	gates, err := ListGates()
	require.NoError(t, err)
	assert.Len(t, gates, 2)
}

func TestListGatesEmpty(t *testing.T) {
	setupTestDB(t)

	gates, err := ListGates()
	require.NoError(t, err)
	assert.Empty(t, gates)
}

// --- DeleteGate ---

func TestDeleteGate(t *testing.T) {
	setupTestDB(t)

	_, err := CreateGate("voice_call", "语音通话", model.FeatureStatusDisabled)
	require.NoError(t, err)

	err = DeleteGate("voice_call")
	require.NoError(t, err)

	_, err = GetGate("voice_call")
	assert.Error(t, err)
}

func TestDeleteGateCascadesWhitelist(t *testing.T) {
	setupTestDB(t)

	_, err := CreateGate("voice_call", "语音通话", model.FeatureStatusWhitelist)
	require.NoError(t, err)

	err = AddUsersToWhitelist("voice_call", []int64{100, 200})
	require.NoError(t, err)

	err = DeleteGate("voice_call")
	require.NoError(t, err)

	// whitelist should be gone too
	users, err := GetWhitelistUsers("voice_call")
	require.NoError(t, err)
	assert.Empty(t, users)
}

// --- Whitelist management ---

func TestAddUsersToWhitelist(t *testing.T) {
	setupTestDB(t)

	_, err := CreateGate("voice_call", "语音通话", model.FeatureStatusWhitelist)
	require.NoError(t, err)

	err = AddUsersToWhitelist("voice_call", []int64{100, 200, 300})
	require.NoError(t, err)

	users, err := GetWhitelistUsers("voice_call")
	require.NoError(t, err)
	assert.Len(t, users, 3)
}

func TestAddUsersToWhitelistIdempotent(t *testing.T) {
	setupTestDB(t)

	_, err := CreateGate("voice_call", "语音通话", model.FeatureStatusWhitelist)
	require.NoError(t, err)

	err = AddUsersToWhitelist("voice_call", []int64{100})
	require.NoError(t, err)

	// Adding same user again should not error (idempotent)
	err = AddUsersToWhitelist("voice_call", []int64{100})
	require.NoError(t, err)

	users, err := GetWhitelistUsers("voice_call")
	require.NoError(t, err)
	assert.Len(t, users, 1)
}

func TestRemoveUsersFromWhitelist(t *testing.T) {
	setupTestDB(t)

	_, err := CreateGate("voice_call", "语音通话", model.FeatureStatusWhitelist)
	require.NoError(t, err)

	err = AddUsersToWhitelist("voice_call", []int64{100, 200, 300})
	require.NoError(t, err)

	err = RemoveUsersFromWhitelist("voice_call", []int64{200})
	require.NoError(t, err)

	users, err := GetWhitelistUsers("voice_call")
	require.NoError(t, err)
	assert.Len(t, users, 2)
	// only 100 and 300 remain
	userIDs := make(map[int64]bool)
	for _, u := range users {
		userIDs[u.UserID] = true
	}
	assert.True(t, userIDs[100])
	assert.True(t, userIDs[300])
	assert.False(t, userIDs[200])
}

func TestRemoveUsersFromWhitelistNonexistentNoop(t *testing.T) {
	setupTestDB(t)

	_, err := CreateGate("voice_call", "语音通话", model.FeatureStatusWhitelist)
	require.NoError(t, err)

	err = AddUsersToWhitelist("voice_call", []int64{100})
	require.NoError(t, err)

	// Removing a user not in whitelist should not error
	err = RemoveUsersFromWhitelist("voice_call", []int64{999})
	require.NoError(t, err)

	users, err := GetWhitelistUsers("voice_call")
	require.NoError(t, err)
	assert.Len(t, users, 1)
}

// --- EvaluateUserFeatures ---

func TestEvaluateUserFeatures_DisabledGateNotVisible(t *testing.T) {
	setupTestDB(t)

	_, err := CreateGate("voice_call", "语音通话", model.FeatureStatusDisabled)
	require.NoError(t, err)

	features, err := EvaluateUserFeatures(100)
	require.NoError(t, err)
	assert.NotContains(t, features, "voice_call")
}

func TestEvaluateUserFeatures_EnabledGateVisibleToAll(t *testing.T) {
	setupTestDB(t)

	_, err := CreateGate("voice_call", "语音通话", model.FeatureStatusEnabled)
	require.NoError(t, err)

	features, err := EvaluateUserFeatures(999)
	require.NoError(t, err)
	assert.Contains(t, features, "voice_call")
}

func TestEvaluateUserFeatures_WhitelistGateVisibleOnlyToAllowed(t *testing.T) {
	setupTestDB(t)

	_, err := CreateGate("voice_call", "语音通话", model.FeatureStatusWhitelist)
	require.NoError(t, err)

	err = AddUsersToWhitelist("voice_call", []int64{100})
	require.NoError(t, err)

	// user 100 is whitelisted
	features, err := EvaluateUserFeatures(100)
	require.NoError(t, err)
	assert.Contains(t, features, "voice_call")

	// user 200 is NOT whitelisted
	features, err = EvaluateUserFeatures(200)
	require.NoError(t, err)
	assert.NotContains(t, features, "voice_call")
}

func TestEvaluateUserFeatures_MultipleGates(t *testing.T) {
	setupTestDB(t)

	_, err := CreateGate("voice_call", "语音通话", model.FeatureStatusEnabled)
	require.NoError(t, err)
	_, err = CreateGate("voice_delegate", "语音托管", model.FeatureStatusWhitelist)
	require.NoError(t, err)
	_, err = CreateGate("agent_voice_llm", "Agent语音大模型", model.FeatureStatusDisabled)
	require.NoError(t, err)

	err = AddUsersToWhitelist("voice_delegate", []int64{100})
	require.NoError(t, err)

	features, err := EvaluateUserFeatures(100)
	require.NoError(t, err)
	assert.Contains(t, features, "voice_call")       // enabled → all
	assert.Contains(t, features, "voice_delegate")   // whitelist + user 100
	assert.NotContains(t, features, "agent_voice_llm") // disabled
}

func TestEvaluateUserFeatures_EmptyGates(t *testing.T) {
	setupTestDB(t)

	features, err := EvaluateUserFeatures(100)
	require.NoError(t, err)
	assert.Empty(t, features)
}
