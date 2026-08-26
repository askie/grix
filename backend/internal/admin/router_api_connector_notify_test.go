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

// 新增的 problem-users / notify 路由必须能和已有的 /connector/reports 共存，
// 且不与 :id 通配段冲突——冲突会让整个进程在启动注册路由时 panic。
func TestRegisterConnectorAPIRoutes_IncludesProblemUserRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerConnectorAPIRoutes(r.Group(""))

	registered := map[string]bool{}
	for _, route := range r.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	assert.True(t, registered["GET /connector/reports/problem-users"], "problem-users route missing")
	assert.True(t, registered["POST /connector/reports/notify"], "notify route missing")
	assert.True(t, registered["POST /connector/reports/notify/preview"], "notify preview route missing")
	assert.True(t, registered["GET /connector/reports"], "existing reports route must stay")
}

// 后台传的 user_ids 是雪花 ID 字符串数组；绑定失败会让整页发送直接报"参数错误"。
func TestApiNotifyConnectorProblemUsers_RejectsInvalidUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	_ = snowflake.Init(1)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"version":"4.3.5","user_ids":["not-a-number"],"channel":"email","title":"t","body":"b"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/connector/reports/notify", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	apiNotifyConnectorProblemUsers(c)
	require.Equal(t, http.StatusBadRequest, w.Code, "非法用户 ID 应被拒绝: %s", w.Body.String())
}

func TestApiListConnectorProblemUsers_RequiresVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	_ = snowflake.Init(1)
	require.NoError(t, store.DB.Create(&model.User{ID: 1, Username: "u", Status: model.UserStatusActive}).Error)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/connector/reports/problem-users", nil)

	apiListConnectorProblemUsers(c)
	require.Equal(t, http.StatusBadRequest, w.Code, "缺少 version 应报参数错误: %s", w.Body.String())
}
