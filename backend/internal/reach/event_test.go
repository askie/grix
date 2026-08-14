package reach

import (
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

func withTestDB(t *testing.T) {
	t.Helper()
	testDB := testutil.NewTestDB()
	original := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = original
		testDB.Close()
	})
}

var ruleIDSeq int64

func addRule(t *testing.T, releaseID int64, ruleType string, ruleValue string, status int16) {
	t.Helper()
	ruleIDSeq++
	require.NoError(t, store.DB.Create(&model.AppRolloutRule{
		ID:        ruleIDSeq,
		ReleaseID: releaseID,
		RuleType:  ruleType,
		RuleValue: datatypes.JSON([]byte(ruleValue)),
		Status:    status,
	}).Error)
}

func TestIsFullRelease_NoActiveRules(t *testing.T) {
	withTestDB(t)
	assert.True(t, isFullRelease(100))
}

func TestIsFullRelease_PercentageFull(t *testing.T) {
	withTestDB(t)
	addRule(t, 200, "percentage", `{"percent":100}`, model.RolloutRuleActive)
	assert.True(t, isFullRelease(200))
}

func TestIsFullRelease_PercentagePartialIsGray(t *testing.T) {
	withTestDB(t)
	addRule(t, 300, "percentage", `{"percent":30}`, model.RolloutRuleActive)
	assert.False(t, isFullRelease(300))
}

func TestIsFullRelease_UserListIsGray(t *testing.T) {
	withTestDB(t)
	addRule(t, 400, "user_list", `{"user_ids":[1,2,3]}`, model.RolloutRuleActive)
	assert.False(t, isFullRelease(400))
}

func TestIsFullRelease_PausedRuleIgnored(t *testing.T) {
	withTestDB(t)
	// A paused partial rule does not count as an active gray rule → full.
	addRule(t, 500, "percentage", `{"percent":10}`, model.RolloutRulePaused)
	assert.True(t, isFullRelease(500))
}

func TestIsFullRelease_MixedRulesFullWins(t *testing.T) {
	withTestDB(t)
	addRule(t, 600, "user_list", `{"user_ids":[1]}`, model.RolloutRuleActive)
	addRule(t, 600, "percentage", `{"percent":100}`, model.RolloutRuleActive)
	assert.True(t, isFullRelease(600))
}
