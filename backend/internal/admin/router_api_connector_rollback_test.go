package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupConnectorRollbackTest(t *testing.T) func() {
	t.Helper()
	gin.SetMode(gin.TestMode)
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	_ = snowflake.Init(1)
	return func() { testDB.Close() }
}

func postRollbackPush(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/connector/releases/rollback-push", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	apiPushConnectorRollback(c)
	return w
}

func rollbackPushData(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var resp struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp.Data
}

// 目标版本没发布就下发，客户端会在 npm install 阶段拿 ETARGET 失败，而它已经写了
// pending 并可能重启过——这个坑 dhf-s1 真踩过（3.34.0 从未发到 npm 却一直在推）。
// 所以未发布版本必须在服务端就被挡下。
func TestApiPushConnectorRollback_RejectsUnpublishedVersion(t *testing.T) {
	defer setupConnectorRollbackTest(t)()

	require.NoError(t, store.DB.Create(&model.ConnectorRelease{
		ID: 7001, ClientType: "grix-connector", Version: "9.9.9",
		Channel: "stable", Status: model.ReleaseStatusRevoked,
	}).Error)

	w := postRollbackPush(t, `{"agent_ids":["101"],"target_version":"9.9.9"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code, "已撤销版本不应被下发: %s", w.Body.String())

	w = postRollbackPush(t, `{"agent_ids":["101"],"target_version":"8.8.8"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code, "库里根本没有的版本不应被下发: %s", w.Body.String())
}

func TestApiPushConnectorRollback_RejectsEmptyAndOversizedAgentList(t *testing.T) {
	defer setupConnectorRollbackTest(t)()
	require.NoError(t, store.DB.Create(&model.ConnectorRelease{
		ID: 7002, ClientType: "grix-connector", Version: "4.3.5",
		Channel: "stable", Status: model.ReleaseStatusPublished,
	}).Error)

	w := postRollbackPush(t, `{"agent_ids":["0","abc",""],"target_version":"4.3.5"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code, "全是非法 ID 时应拒绝: %s", w.Body.String())

	ids := make([]string, 0, connectorRollbackPushMaxAgents+1)
	for i := 1; i <= connectorRollbackPushMaxAgents+1; i++ {
		ids = append(ids, `"`+strconv.Itoa(i)+`"`)
	}
	w = postRollbackPush(t, `{"agent_ids":[`+strings.Join(ids, ",")+`],"target_version":"4.3.5"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code, "超过单次上限时应拒绝: %s", w.Body.String())
}

// 没有 ws 节点回写送达集合时，全部算 missed，并且**不能**打冷却——
// 这些 agent 只是当时不在线，上线后必须还能立刻重推。
func TestApiPushConnectorRollback_UndeliveredStaysRetriable(t *testing.T) {
	defer setupConnectorRollbackTest(t)()
	require.NoError(t, store.DB.Create(&model.ConnectorRelease{
		ID: 7003, ClientType: "grix-connector", Version: "4.3.5",
		Channel: "stable", Status: model.ReleaseStatusPublished,
	}).Error)

	w := postRollbackPush(t, `{"agent_ids":["201","202","201"],"target_version":"4.3.5"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	data := rollbackPushData(t, w)

	assert.EqualValues(t, 2, data["requested"], "重复 ID 应被去重")
	assert.Empty(t, data["dispatched"])
	assert.ElementsMatch(t, []any{"201", "202"}, data["missed"])
	assert.Empty(t, data["skipped"])

	cooling := wsagentapi.ConnectorRollbackInCooldown(context.Background(), []int64{201, 202})
	assert.Empty(t, cooling, "未送达的 agent 不应占用冷却，否则上线后 15 分钟内推不动")
}

// 冷却期内的 agent 必须被跳过：重复下发会让同一台机器反复 npm install + 重启。
func TestApiPushConnectorRollback_SkipsAgentsInCooldown(t *testing.T) {
	defer setupConnectorRollbackTest(t)()
	require.NoError(t, store.DB.Create(&model.ConnectorRelease{
		ID: 7004, ClientType: "grix-connector", Version: "4.3.5",
		Channel: "stable", Status: model.ReleaseStatusPublished,
	}).Error)

	wsagentapi.MarkConnectorRollbackCooldown(context.Background(), []int64{301})

	w := postRollbackPush(t, `{"agent_ids":["301"],"target_version":"4.3.5"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	data := rollbackPushData(t, w)

	assert.ElementsMatch(t, []any{"301"}, data["skipped"])
	assert.Empty(t, data["missed"])
	assert.Nil(t, data["push_id"], "全部被冷却挡下时不应真的发广播")
}
