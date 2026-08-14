package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	adminservice "github.com/askie/grix/backend/internal/admin/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// loginToken 走真实登录接口拿 Bearer Token。
func loginToken(t *testing.T, r *gin.Engine, username, password string) string {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"username": username, "password": password})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/api/login", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Data.Token)
	return resp.Data.Token
}

func seedLookupUsers(t *testing.T) {
	t.Helper()
	require.NoError(t, store.DB.Create(&model.User{
		ID: 9001, Username: "alice", Nickname: "爱丽丝",
		Email: "alice@example.com", AvatarURL: "https://cdn/a.png",
		Status: model.UserStatusActive,
	}).Error)
	require.NoError(t, store.DB.Create(&model.User{
		ID: 9002, Username: "bob", Nickname: "",
		Email: "bob@example.com", Status: model.UserStatusBanned,
	}).Error)
}

func doLookup(t *testing.T, r *gin.Engine, token, ids string) (int, []map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/api/users/lookup?ids="+ids, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	var resp struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return w.Code, resp.Data.Items
}

// TestApiLookupUsers 锁住用户目录批量查询的三个关键点：
//  1. 路由 /users/lookup 与 /users/:id/* 共存且仅需登录鉴权即可访问；
//  2. 按 ids 批量返回昵称/账号/头像/状态，非法与重复 ID 被忽略；
//  3. 无 users 权限的管理员拿不到邮箱/手机号（降敏），超管拿全量。
func TestApiLookupUsers(t *testing.T) {
	const password = "LookupPass123A"

	t.Run("超管批量查询返回全量字段", func(t *testing.T) {
		r, cleanup := setupAdminLoginRouter(t)
		defer cleanup()
		seedAdmin(t, "superlookup", password)
		seedLookupUsers(t)

		token := loginToken(t, r, "superlookup", password)
		code, items := doLookup(t, r, token, "9001,abc,9001,9002,0,-3")
		require.Equal(t, http.StatusOK, code)
		require.Len(t, items, 2)

		byID := map[string]map[string]any{}
		for _, it := range items {
			byID[fmt.Sprint(it["id"])] = it
		}
		require.Equal(t, "爱丽丝", byID["9001"]["nickname"])
		require.Equal(t, "alice", byID["9001"]["username"])
		require.Equal(t, "https://cdn/a.png", byID["9001"]["avatar_url"])
		require.Equal(t, "alice@example.com", byID["9001"]["email"])
		require.Equal(t, float64(model.UserStatusBanned), byID["9002"]["status"])
	})

	t.Run("无users权限管理员降敏返回", func(t *testing.T) {
		r, cleanup := setupAdminLoginRouter(t)
		defer cleanup()
		seedLookupUsers(t)

		require.NoError(t, store.DB.AutoMigrate(&model.AdminRole{}))
		role := model.AdminRole{ID: 5201, Name: "仅举报", Permissions: `["reports"]`}
		require.NoError(t, store.DB.Create(&role).Error)
		hash, err := adminservice.HashAdminPassword(password)
		require.NoError(t, err)
		require.NoError(t, store.DB.Create(&model.AdminUser{
			ID: 5202, Username: "reportonly", PasswordHash: hash,
			Nickname: "举报员", Role: model.AdminRoleCustom, RoleID: &role.ID,
			Status: model.AdminStatusActive,
		}).Error)

		token := loginToken(t, r, "reportonly", password)
		code, items := doLookup(t, r, token, "9001")
		require.Equal(t, http.StatusOK, code)
		require.Len(t, items, 1)
		require.Equal(t, "爱丽丝", items[0]["nickname"])
		require.Empty(t, items[0]["email"])
		require.Empty(t, items[0]["phone_e164"])
	})

	t.Run("未登录被拒", func(t *testing.T) {
		r, cleanup := setupAdminLoginRouter(t)
		defer cleanup()

		code, _ := doLookup(t, r, "invalid-token", "9001")
		require.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("空ids返回空列表", func(t *testing.T) {
		r, cleanup := setupAdminLoginRouter(t)
		defer cleanup()
		seedAdmin(t, "superlookup2", password)

		token := loginToken(t, r, "superlookup2", password)
		code, items := doLookup(t, r, token, "")
		require.Equal(t, http.StatusOK, code)
		require.Empty(t, items)
	})

	t.Run("全非法ids返回空列表", func(t *testing.T) {
		r, cleanup := setupAdminLoginRouter(t)
		defer cleanup()
		seedAdmin(t, "superlookup3", password)
		seedLookupUsers(t) // 库里有 9001/9002，但请求里没这两个

		token := loginToken(t, r, "superlookup3", password)
		// 涵盖非数字、负数、0、空 token；空格无需在 URL 里出现，业务层 TrimSpace 已由其它用例覆盖。
		code, items := doLookup(t, r, token, "abc,xyz,-1,0,,not-a-number")
		require.Equal(t, http.StatusOK, code)
		require.Empty(t, items)
	})

	t.Run("ids超过100时截断到前100", func(t *testing.T) {
		r, cleanup := setupAdminLoginRouter(t)
		defer cleanup()
		seedAdmin(t, "superlookup4", password)

		// 种入 105 个用户 id: 20001..20105（email 需唯一避免 sqlite UNIQUE 冲突）
		for i := 0; i < 105; i++ {
			require.NoError(t, store.DB.Create(&model.User{
				ID:       int64(20001 + i),
				Username: fmt.Sprintf("bulk%d", 20001+i),
				Nickname: fmt.Sprintf("bulk-%d", 20001+i),
				Email:    fmt.Sprintf("bulk%d@example.com", 20001+i),
				Status:   model.UserStatusActive,
			}).Error)
		}

		// 请求所有 105 个 id，第 101..105 应被截断，不返回
		parts := make([]string, 0, 105)
		for i := 0; i < 105; i++ {
			parts = append(parts, strconv.FormatInt(int64(20001+i), 10))
		}
		token := loginToken(t, r, "superlookup4", password)
		code, items := doLookup(t, r, token, strings.Join(parts, ","))
		require.Equal(t, http.StatusOK, code)
		require.Len(t, items, 100, "应只返回前 100 个 id 对应的用户")

		// 确认最后 5 个 id (20101..20105) 都不在返回结果里
		got := map[string]bool{}
		for _, it := range items {
			got[fmt.Sprint(it["id"])] = true
		}
		for i := 100; i < 105; i++ {
			require.False(t, got[strconv.Itoa(20001+i)],
				"超过 100 的 id %d 不应出现在结果中", 20001+i)
		}
	})

	t.Run("封禁用户返回status与banned_reason", func(t *testing.T) {
		r, cleanup := setupAdminLoginRouter(t)
		defer cleanup()
		seedAdmin(t, "superlookup5", password)

		bannedAt := time.Now().UTC()
		require.NoError(t, store.DB.Create(&model.User{
			ID:           9101,
			Username:     "banned-guy",
			Nickname:     "被封的",
			Status:       model.UserStatusBanned,
			BannedReason: "违规内容传播",
			BannedAt:     &bannedAt,
		}).Error)

		token := loginToken(t, r, "superlookup5", password)
		code, items := doLookup(t, r, token, "9101")
		require.Equal(t, http.StatusOK, code)
		require.Len(t, items, 1)
		require.Equal(t, "被封的", items[0]["nickname"])
		require.Equal(t, float64(model.UserStatusBanned), items[0]["status"])
		require.Equal(t, "违规内容传播", items[0]["banned_reason"])
		require.NotEmpty(t, items[0]["banned_at"])
	})
}

func doGetCustomerCoachSnapshot(t *testing.T, r *gin.Engine, token string, userID int64) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/api/users/%d/customer-coach-snapshot", userID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	var resp struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return w.Code, resp.Data
}

func TestApiGetUserCustomerCoachSnapshot(t *testing.T) {
	const password = "SnapshotPass123A"

	t.Run("返回客服同源用户状态快照", func(t *testing.T) {
		r, cleanup := setupAdminLoginRouter(t)
		defer cleanup()
		seedAdmin(t, "snapshotadmin", password)
		user := &model.User{
			ID:       9201,
			Username: "snapshot-user",
			Nickname: "Snapshot User",
			Email:    "snapshot-user@example.com",
			Region:   "cn",
			Status:   model.UserStatusActive,
		}
		require.NoError(t, store.DB.Create(user).Error)
		agent := &model.Agent{
			ID:              9202,
			AgentName:       "主控 Agent",
			OwnerID:         user.ID,
			ProviderType:    model.AgentProviderAPI,
			AgentClientType: model.AgentClientTypeOpenClaw,
			Introduction:    "负责协调任务",
			Status:          model.AgentStatusActive,
			CreatedAt:       time.Now().UTC(),
			UpdatedAt:       time.Now().UTC(),
		}
		require.NoError(t, store.DB.Create(agent).Error)
		for _, scope := range agentscope.AllowedScopes() {
			require.NoError(t, store.DB.Create(&model.AgentAPIScope{
				AgentID: agent.ID,
				Scope:   scope,
			}).Error)
		}

		token := loginToken(t, r, "snapshotadmin", password)
		code, data := doGetCustomerCoachSnapshot(t, r, token, user.ID)
		require.Equal(t, http.StatusOK, code)

		snapshot := data["snapshot"].(map[string]any)
		userData := snapshot["user"].(map[string]any)
		require.Equal(t, "9201", userData["id"])
		require.Equal(t, "cn", userData["region"])

		event := snapshot["event"].(map[string]any)
		require.Equal(t, "admin_api", event["source"])
		require.Equal(t, "admin_view", event["scenario"])

		overview := snapshot["overview"].(map[string]any)
		require.Equal(t, float64(1), overview["agent_total"])
		mainAgent := snapshot["main_agent"].(map[string]any)
		require.Equal(t, "9202", mainAgent["id"])
		require.Equal(t, "主控 Agent", mainAgent["name"])

		markdown := data["markdown"].(string)
		require.Contains(t, markdown, "# Grix 用户状态快照")
		require.Contains(t, markdown, "当前主 Agent：主控 Agent")
	})

	t.Run("用户不存在返回404", func(t *testing.T) {
		r, cleanup := setupAdminLoginRouter(t)
		defer cleanup()
		seedAdmin(t, "snapshotadmin404", password)

		token := loginToken(t, r, "snapshotadmin404", password)
		code, _ := doGetCustomerCoachSnapshot(t, r, token, 999999)
		require.Equal(t, http.StatusNotFound, code)
	})
}
