package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
)

func setupWidgetSessionHandlerRouter(t *testing.T) (*gin.Engine, *testutil.TestDB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	tdb := testutil.NewTestDB()
	store.DB = tdb.DB
	r := gin.New()
	authed := r.Group("/")
	authed.Use(func(c *gin.Context) {
		c.Set("user_id", int64(101))
		c.Next()
	})
	authed.GET("/v1/widget/sessions/list", WidgetSessionList)
	authed.POST("/v1/widget/sessions/close", WidgetSessionClose)
	authed.POST("/v1/widget/sessions/ban", WidgetSessionBan)
	return r, tdb
}

func TestWidgetSessionHandlers(t *testing.T) {
	r, tdb := setupWidgetSessionHandlerRouter(t)
	defer tdb.Close()
	now := time.Now().UTC()
	if err := store.DB.Create(&model.WidgetSession{ID: 1, SiteID: 10, OwnerUserID: 101, VisitorID: 201, VisitorKey: "vk1", SessionID: "ws1", Status: model.WidgetSessionStatusActive, CreatedAt: now, UpdatedAt: now, LastActiveAt: now}).Error; err != nil {
		t.Fatalf("seed widget session error: %v", err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/v1/widget/sessions/list?site_id=10&status=1", nil)
	listRec := httptest.NewRecorder()
	r.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}

	closeBody, _ := json.Marshal(map[string]string{"session_id": "ws1"})
	closeReq := httptest.NewRequest(http.MethodPost, "/v1/widget/sessions/close", bytes.NewReader(closeBody))
	closeReq.Header.Set("Content-Type", "application/json")
	closeRec := httptest.NewRecorder()
	r.ServeHTTP(closeRec, closeReq)
	if closeRec.Code != http.StatusOK {
		t.Fatalf("close status=%d body=%s", closeRec.Code, closeRec.Body.String())
	}

	banBody, _ := json.Marshal(map[string]string{"session_id": "ws1"})
	banReq := httptest.NewRequest(http.MethodPost, "/v1/widget/sessions/ban", bytes.NewReader(banBody))
	banReq.Header.Set("Content-Type", "application/json")
	banRec := httptest.NewRecorder()
	r.ServeHTTP(banRec, banReq)
	if banRec.Code != http.StatusOK {
		t.Fatalf("ban status=%d body=%s", banRec.Code, banRec.Body.String())
	}
}
