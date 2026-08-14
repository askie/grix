package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupRolloutHandlerTest(t *testing.T) func() {
	t.Helper()
	gin.SetMode(gin.TestMode)
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	_ = snowflake.Init(1)
	return func() { testDB.Close() }
}

// 模拟塘主管理页真实发送的灰度规则请求体：release_id 是雪花 ID 字符串，
// rule_value 是 JSON 对象。锁住两个回归点：
//  1. 请求体 ReleaseID 必须带 ,string，否则字符串 ID 无法绑定到 int64，
//     接口会直接返回"参数错误"（线上反馈的现象）。
//  2. rule_value 必须按对象原样落库，不能被二次编码成带引号的字符串，
//     否则后续灰度匹配解析不出来、规则永远命中不了。
func TestApiCreateConnectorRolloutRule_FrontendPayload(t *testing.T) {
	cleanup := setupRolloutHandlerTest(t)
	defer cleanup()

	require.NoError(t, store.DB.Create(&model.ConnectorRelease{
		ID: 9001, Version: "0.3.0", Channel: "stable", Status: model.ReleaseStatusPublished,
	}).Error)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"release_id":"9001","rule_type":"percentage","rule_value":{"percent":10},"priority":5}`
	c.Request = httptest.NewRequest(http.MethodPost, "/connector/rollout-rules", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	apiCreateConnectorRolloutRule(c)

	require.Equal(t, http.StatusOK, w.Code, "前端真实请求体应被接受，而不是参数错误: %s", w.Body.String())

	var rule model.ConnectorRolloutRule
	require.NoError(t, store.DB.Where("release_id = ?", int64(9001)).First(&rule).Error)
	assert.Equal(t, "percentage", rule.RuleType)
	assert.Equal(t, 5, rule.Priority)
	assert.JSONEq(t, `{"percent":10}`, string(rule.RuleValue))
}

func TestApiCreateAppRolloutRule_FrontendPayload(t *testing.T) {
	cleanup := setupRolloutHandlerTest(t)
	defer cleanup()

	require.NoError(t, store.DB.Create(&model.AppRelease{
		ID: 7001, Version: "2.10.0", Platform: "ios", Channel: "stable", Status: 1,
	}).Error)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"release_id":"7001","rule_type":"user_list","rule_value":{"user_ids":[123,456]},"priority":3}`
	c.Request = httptest.NewRequest(http.MethodPost, "/app/rollout-rules", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	apiCreateAppRolloutRule(c)

	require.Equal(t, http.StatusOK, w.Code, "前端真实请求体应被接受，而不是参数错误: %s", w.Body.String())

	var rule model.AppRolloutRule
	require.NoError(t, store.DB.Where("release_id = ?", int64(7001)).First(&rule).Error)
	assert.Equal(t, "user_list", rule.RuleType)
	assert.JSONEq(t, `{"user_ids":[123,456]}`, string(rule.RuleValue))
}
