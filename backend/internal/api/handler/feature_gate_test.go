package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/askie/grix/backend/internal/featuregate"
	"github.com/askie/grix/backend/internal/model"
	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupFeatureGateTest(t *testing.T) (*gin.Engine, *testutil.TestDB, string) {
	t.Helper()
	tdb := testutil.NewTestDB()
	store.DB = tdb.DB
	// 切换到本用例的全新 DB 后立即失效全局门缓存，避免沿用上一个用例
	// 在旧 DB 上加载的快照（CreateGate 不会主动失效缓存）造成跨用例污染。
	featuregate.InvalidateCache()
	jwtpkg.Init("test-secret-key", 3600, 86400)

	user := model.User{ID: 4001, Username: "fguser", Email: "fg@example.com", PasswordHash: "x", Nickname: "fguser"}
	if err := tdb.DB.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	token, _, _ := jwtpkg.GenerateAccessToken(user.ID)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		a := c.GetHeader("Authorization")
		if a != "" {
			claims, _ := jwtpkg.ValidateAccessToken(a)
			if claims != nil {
				c.Set("user_id", claims.UserID)
			}
		}
		c.Next()
	})

	r.GET("/v1/users/features", UserGetFeatures)

	t.Cleanup(func() {
		tdb.Close()
		featuregate.InvalidateCache()
	})

	return r, tdb, token
}

// --- User endpoint ---

func TestUserGetFeatures_ReturnsEnabledGates(t *testing.T) {
	r, _, token := setupFeatureGateTest(t)

	_, err := featuregate.CreateGate("voice_call", "语音通话", model.FeatureStatusEnabled)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/v1/users/features", nil)
	req.Header.Set("Authorization", token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Code int                        `json:"code"`
		Data featureGateFeaturesResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Code)
	assert.Contains(t, resp.Data.Features, "voice_call")
}

func TestUserGetFeatures_ExcludesDisabled(t *testing.T) {
	r, _, token := setupFeatureGateTest(t)

	_, err := featuregate.CreateGate("voice_call", "语音通话", model.FeatureStatusDisabled)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/v1/users/features", nil)
	req.Header.Set("Authorization", token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Code int                        `json:"code"`
		Data featureGateFeaturesResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.Data.Features)
}

func TestUserGetFeatures_NoAuth(t *testing.T) {
	r, _, _ := setupFeatureGateTest(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/users/features", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEqual(t, 0, resp.Code)
}

