package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	adminservice "github.com/askie/grix/backend/internal/admin/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// setupAdminLoginRouter 搭建真实 gin 路由 + sqlite + miniredis，返回路由与清理函数。
func setupAdminLoginRouter(t *testing.T) (*gin.Engine, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	require.NoError(t, snowflake.Init(1))

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	r := gin.New()
	registerAPIRoutes(r.Group("/admin"))

	return r, func() {
		if store.RDB != nil {
			_ = store.RDB.Close()
			store.RDB = nil
		}
		testDB.Close()
	}
}

func seedAdmin(t *testing.T, username, password string) {
	t.Helper()
	hash, err := adminservice.HashAdminPassword(password)
	require.NoError(t, err)
	require.NoError(t, store.DB.Create(&model.AdminUser{
		ID:           4101,
		Username:     username,
		PasswordHash: hash,
		Nickname:     "Login Admin",
		Role:         model.AdminRoleSuperAdmin,
		Status:       model.AdminStatusActive,
	}).Error)
}

func postLogin(t *testing.T, r *gin.Engine, body map[string]any) (int, int, string) {
	t.Helper()
	raw, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/api/login", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return w.Code, resp.Code, resp.Msg
}

// TestAdminLogin 验证去掉图形验证码后，仅凭账号密码即可登录，且错误凭据仍被拒。
func TestAdminLogin(t *testing.T) {
	const username = "loginadmin"
	const password = "LoginPass123A"

	t.Run("账号密码正确则登录成功", func(t *testing.T) {
		r, cleanup := setupAdminLoginRouter(t)
		defer cleanup()
		seedAdmin(t, username, password)

		httpCode, bizCode, _ := postLogin(t, r, map[string]any{
			"username": username,
			"password": password,
		})
		require.Equal(t, http.StatusOK, httpCode)
		require.Equal(t, 0, bizCode)
	})

	t.Run("密码错误被拒", func(t *testing.T) {
		r, cleanup := setupAdminLoginRouter(t)
		defer cleanup()
		seedAdmin(t, username, password)

		httpCode, bizCode, _ := postLogin(t, r, map[string]any{
			"username": username,
			"password": "wrong-password",
		})
		require.Equal(t, http.StatusUnauthorized, httpCode)
		require.Equal(t, 10001, bizCode)
	})
}
