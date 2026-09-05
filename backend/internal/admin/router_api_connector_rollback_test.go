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

func postCreateRelease(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/connector/releases", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	apiCreateConnectorRelease(c)
	return w
}

func postPublishRelease(t *testing.T, id int64) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/connector/releases/"+strconv.FormatInt(id, 10)+"/publish", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatInt(id, 10)}}
	apiPublishConnectorRelease(c)
	return w
}

// 漏配 min_version 的 grix-connector 版本连 draft 都建不出来：admin 这一层
// 默认 client_type 为 grix-connector，省略字段同样要被挡住。
func TestApiCreateConnectorRelease_RequiresMinVersion(t *testing.T) {
	defer setupConnectorRollbackTest(t)()

	w := postCreateRelease(t, `{"client_type":"grix-connector","version":"4.9.0"}`)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "min_version")

	w = postCreateRelease(t, `{"version":"4.9.0"}`)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	var count int64
	require.NoError(t, store.DB.Model(&model.ConnectorRelease{}).Count(&count).Error)
	assert.EqualValues(t, 0, count, "被拒绝的发布不应留下 draft")

	w = postCreateRelease(t, `{"client_type":"grix-connector","version":"4.9.0","min_version":"4.3.6"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// grix-hermes 没有版本闸门，缺 min_version 照常建。
	w = postCreateRelease(t, `{"client_type":"grix-hermes","version":"1.16.8","npm_package":"grix-hermes"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// publish 是真正把版本推向全网的关口：min_version 被清空过也要拦在这里。
func TestApiPublishConnectorRelease_RequiresMinVersion(t *testing.T) {
	defer setupConnectorRollbackTest(t)()

	require.NoError(t, store.DB.Create(&model.ConnectorRelease{
		ID: 7101, ClientType: "grix-connector", Version: "4.9.0",
		Channel: "stable", Status: model.ReleaseStatusDraft,
	}).Error)

	w := postPublishRelease(t, 7101)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "min_version")

	var stored model.ConnectorRelease
	require.NoError(t, store.DB.First(&stored, 7101).Error)
	assert.Equal(t, model.ReleaseStatusDraft, stored.Status, "被拒绝的发布不能改状态")

	// grix-hermes 缺 min_version 也要能发。
	require.NoError(t, store.DB.Create(&model.ConnectorRelease{
		ID: 7102, ClientType: "grix-hermes", Version: "1.16.8",
		Channel: "stable", Status: model.ReleaseStatusDraft,
	}).Error)
	w = postPublishRelease(t, 7102)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}
