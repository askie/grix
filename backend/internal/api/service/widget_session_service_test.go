package service

import (
	"errors"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func setupWidgetSessionServiceTest(t *testing.T) *testutil.TestDB {
	t.Helper()
	tdb := testutil.NewTestDB()
	store.DB = tdb.DB
	return tdb
}

func TestWidgetSessionListAndStatusUpdate(t *testing.T) {
	tdb := setupWidgetSessionServiceTest(t)
	defer tdb.Close()
	now := time.Now().UTC()
	seed := []model.WidgetSession{
		{ID: 1, SiteID: 11, OwnerUserID: 101, VisitorID: 201, VisitorKey: "vk1", SessionID: "ws1", Status: model.WidgetSessionStatusActive, CreatedAt: now, UpdatedAt: now, LastActiveAt: now},
		{ID: 2, SiteID: 11, OwnerUserID: 101, VisitorID: 202, VisitorKey: "vk2", SessionID: "ws2", Status: model.WidgetSessionStatusActive, CreatedAt: now, UpdatedAt: now, LastActiveAt: now},
	}
	if err := store.DB.Create(&seed).Error; err != nil {
		t.Fatalf("seed widget sessions error: %v", err)
	}

	list, err := WidgetSessionList(WidgetSessionListInput{OwnerUserID: 101, SiteID: 11, Status: model.WidgetSessionStatusActive, Limit: 20, Offset: 0})
	if err != nil {
		t.Fatalf("WidgetSessionList() error = %v", err)
	}
	if list.Total != 2 || len(list.Items) != 2 {
		t.Fatalf("unexpected list result: %+v", list)
	}

	closed, err := WidgetSessionClose(WidgetSessionStatusUpdateInput{OwnerUserID: 101, SessionID: "ws1"})
	if err != nil {
		t.Fatalf("WidgetSessionClose() error = %v", err)
	}
	if closed.Status != model.WidgetSessionStatusClosed {
		t.Fatalf("status should be closed, got=%d", closed.Status)
	}

	banned, err := WidgetSessionBan(WidgetSessionStatusUpdateInput{OwnerUserID: 101, SessionID: "ws2"})
	if err != nil {
		t.Fatalf("WidgetSessionBan() error = %v", err)
	}
	if banned.Status != model.WidgetSessionStatusBanned {
		t.Fatalf("status should be banned, got=%d", banned.Status)
	}
}

func TestWidgetSessionUpdateOwnership(t *testing.T) {
	tdb := setupWidgetSessionServiceTest(t)
	defer tdb.Close()
	now := time.Now().UTC()
	if err := store.DB.Create(&model.WidgetSession{ID: 3, SiteID: 22, OwnerUserID: 110, VisitorID: 210, VisitorKey: "vk3", SessionID: "ws3", Status: model.WidgetSessionStatusActive, CreatedAt: now, UpdatedAt: now, LastActiveAt: now}).Error; err != nil {
		t.Fatalf("seed widget session error: %v", err)
	}

	_, err := WidgetSessionBan(WidgetSessionStatusUpdateInput{OwnerUserID: 999, SessionID: "ws3"})
	if !errors.Is(err, ErrWidgetSessionNotOwned) {
		t.Fatalf("expected ErrWidgetSessionNotOwned, got %v", err)
	}
}
