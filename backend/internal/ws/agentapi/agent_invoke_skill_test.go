package agentapi

import (
	"testing"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupSkillInvokeTest(t *testing.T) func() {
	t.Helper()
	previousDB := store.DB
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	require.NoError(t, store.DB.AutoMigrate(&model.UserSkill{}))
	return func() {
		testDB.Close()
		store.DB = previousDB
	}
}

func TestDispatchSkillSet_MissingName(t *testing.T) {
	_, code, _ := dispatchSkillSet(1, map[string]interface{}{"content": "x"})
	assert.Equal(t, 4001, code)
}

func TestDispatchSkillSet_UpsertThenDelete(t *testing.T) {
	defer setupSkillInvokeTest(t)()
	const owner = int64(2001)

	// 新建。
	_, code, msg := dispatchSkillSet(owner, map[string]interface{}{"name": "报告规范", "content": "# 规范"})
	assert.Equal(t, 0, code, msg)

	// skill_get 带名读回。
	got, code, _ := dispatchSkillGet(owner, map[string]interface{}{"name": "报告规范"})
	require.Equal(t, 0, code)
	skill, ok := got.(*model.UserSkill)
	require.True(t, ok)
	assert.Equal(t, "# 规范", skill.Content)

	// content 显式为空 = 删除。
	res, code, _ := dispatchSkillSet(owner, map[string]interface{}{"name": "报告规范", "content": ""})
	require.Equal(t, 0, code)
	m, ok := res.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, m["deleted"])

	// 删除后读不到。
	_, code, _ = dispatchSkillGet(owner, map[string]interface{}{"name": "报告规范"})
	assert.NotEqual(t, 0, code)
}

func TestDispatchSkillSet_MissingContentKeyRejected(t *testing.T) {
	defer setupSkillInvokeTest(t)()
	// 完全不传 content 键 = 参数缺失（区别于显式空串的删除语义）。
	_, code, _ := dispatchSkillSet(2002, map[string]interface{}{"name": "n"})
	assert.Equal(t, 4001, code)
}

func TestDispatchSkillGet_ListWhenNoName(t *testing.T) {
	defer setupSkillInvokeTest(t)()
	const owner = int64(2003)
	dispatchSkillSet(owner, map[string]interface{}{"name": "a", "content": "ca"})
	dispatchSkillSet(owner, map[string]interface{}{"name": "b", "content": "cb"})

	got, code, _ := dispatchSkillGet(owner, map[string]interface{}{})
	require.Equal(t, 0, code)
	m, ok := got.(map[string]interface{})
	require.True(t, ok)
	items, ok := m["items"].([]service.SkillSummary)
	require.True(t, ok)
	assert.Len(t, items, 2)
}
