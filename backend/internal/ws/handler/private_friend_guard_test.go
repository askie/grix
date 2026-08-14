package handler

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func TestValidatePrivateHumanSendPermission_WidgetSessionBypass(t *testing.T) {
	tdb := testutil.NewTestDB()
	defer tdb.Close()
	store.DB = tdb.DB

	now := time.Now().UTC()
	if err := store.DB.Create(&model.Session{SessionID: "widget-s1", SessionType: model.SessionTypeDirect, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed session error: %v", err)
	}
	if err := store.DB.Create(&model.WidgetSession{ID: 1, SiteID: 2, OwnerUserID: 3, VisitorID: 4, VisitorKey: "vk_1", SessionID: "widget-s1", Status: model.WidgetSessionStatusActive, CreatedAt: now, UpdatedAt: now, LastActiveAt: now}).Error; err != nil {
		t.Fatalf("seed widget session error: %v", err)
	}

	if err := validatePrivateHumanSendPermission("widget-s1", 4, 1, 0); err != nil {
		t.Fatalf("expected bypass for widget session, got err=%v", err)
	}
}
