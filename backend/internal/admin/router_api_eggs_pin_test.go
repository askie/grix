package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/require"
)

// 回归测试：塘主 APP(Bearer Token 鉴权、/admin/api 路由树) 之前漏挂了置顶接口，
// 导致 APP 里点"置顶"实际报 404。本测试固定住这条路径必须真实可用。
func TestApiEggPin_RealAppRouterPath(t *testing.T) {
	r, cleanup := setupAdminLoginRouter(t)
	defer cleanup()

	const username = "eggpinadmin"
	const password = "EggPinPass123A"
	seedAdmin(t, username, password)

	w := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/admin/api/login", strings.NewReader(`{"username":"`+username+`","password":"`+password+`"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, loginReq)
	require.Equal(t, http.StatusOK, w.Code)

	var loginResp struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &loginResp))
	require.NotEmpty(t, loginResp.Data.Token)

	require.NoError(t, store.DB.Create(&model.EggCategory{
		ID: "cat1", Code: "cat1", Status: model.EggCategoryStatusActive,
	}).Error)
	require.NoError(t, store.DB.Create(&model.Egg{
		ID: "egg-a", CategoryID: "cat1", PackageType: "skill_zip", Status: model.EggStatusPublished,
	}).Error)

	pinReq := httptest.NewRequest(http.MethodPost, "/admin/api/eggs/egg-a/pin", strings.NewReader(`{"pinned":true}`))
	pinReq.Header.Set("Content-Type", "application/json")
	pinReq.Header.Set("Authorization", "Bearer "+loginResp.Data.Token)
	pinW := httptest.NewRecorder()
	r.ServeHTTP(pinW, pinReq)
	require.Equal(t, http.StatusOK, pinW.Code, "塘主APP点置顶不应再 404: %s", pinW.Body.String())

	var egg model.Egg
	require.NoError(t, store.DB.First(&egg, "id = ?", "egg-a").Error)
	require.NotNil(t, egg.PinnedAt)

	getReq := httptest.NewRequest(http.MethodGet, "/admin/api/eggs/egg-a", nil)
	getReq.Header.Set("Authorization", "Bearer "+loginResp.Data.Token)
	getW := httptest.NewRecorder()
	r.ServeHTTP(getW, getReq)
	require.Equal(t, http.StatusOK, getW.Code)

	var getResp struct {
		Data struct {
			Egg struct {
				Pinned bool `json:"pinned"`
			} `json:"egg"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(getW.Body.Bytes(), &getResp))
	require.True(t, getResp.Data.Egg.Pinned)
}
