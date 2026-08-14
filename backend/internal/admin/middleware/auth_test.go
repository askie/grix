package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	adminservice "github.com/askie/grix/backend/internal/admin/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
)

func TestRequireAPIAuth_AcceptsBearerSessionToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := store.DB
	store.DB = testDB.DB
	defer func() {
		store.DB = originalDB
	}()

	passwordHash, err := adminservice.HashAdminPassword("AdminPassword123")
	if err != nil {
		t.Fatalf("hash admin password: %v", err)
	}
	admin := model.AdminUser{
		ID:           9001,
		Username:     "admin",
		PasswordHash: passwordHash,
		Nickname:     "Admin",
		Role:         model.AdminRoleSuperAdmin,
		Status:       model.AdminStatusActive,
	}
	if err := testDB.DB.Create(&admin).Error; err != nil {
		t.Fatalf("create admin: %v", err)
	}

	sessionToken, _, err := adminservice.Login("admin", "AdminPassword123", "127.0.0.1", "unit-test")
	if err != nil {
		t.Fatalf("admin login: %v", err)
	}

	router := gin.New()
	router.Use(RequireAPIAuth())
	router.GET("/protected", func(c *gin.Context) {
		if CurrentAdmin(c) == nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}
}
