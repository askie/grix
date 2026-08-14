package service

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func TestBuildSessionItemsMarksVisitorSession(t *testing.T) {
	tdb := testutil.NewTestDB()
	defer tdb.Close()
	store.DB = tdb.DB

	now := time.Now().UTC()
	if err := store.DB.Create(&model.Session{SessionID: "s_widget_1", SessionType: model.SessionTypeDirect, LastMsgSummary: "hello", UpdatedAt: now, CreatedAt: now}).Error; err != nil {
		t.Fatalf("seed session error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{SessionID: "s_widget_1", MemberID: 1001, MemberType: 1, LastActiveAt: now, JoinedAt: now}).Error; err != nil {
		t.Fatalf("seed member self error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{SessionID: "s_widget_1", MemberID: 2001, MemberType: 1, LastActiveAt: now, JoinedAt: now}).Error; err != nil {
		t.Fatalf("seed member peer error: %v", err)
	}
	if err := store.DB.Create(&model.User{ID: 2001, Username: "visitor-user", Email: "visitor-user@example.com", Nickname: "Visitor User", PasswordHash: "x"}).Error; err != nil {
		t.Fatalf("seed user error: %v", err)
	}
	if err := store.DB.Create(&model.WidgetSession{ID: 9001, SiteID: 8001, OwnerUserID: 1001, VisitorID: 2001, VisitorKey: "vk_1", SessionID: "s_widget_1", VisitorName: "Alice", VisitorEmail: "alice@example.com", Status: model.WidgetSessionStatusActive, CreatedAt: now, UpdatedAt: now, LastActiveAt: now}).Error; err != nil {
		t.Fatalf("seed widget session error: %v", err)
	}

	items, err := buildSessionItems(1001, []model.SessionMember{{SessionID: "s_widget_1", MemberID: 1001, MemberType: 1, LastActiveAt: now}})
	if err != nil {
		t.Fatalf("buildSessionItems() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items len=%d", len(items))
	}
	if !items[0].IsVisitor {
		t.Fatalf("expected visitor session flag true, got false")
	}
	if items[0].Peer == nil {
		t.Fatalf("expected visitor peer not nil")
	}
	if items[0].Peer.Nickname != "Alice" {
		t.Fatalf("visitor peer nickname=%q", items[0].Peer.Nickname)
	}
}

func TestSessionDetailIncludesVisitorInfo(t *testing.T) {
	tdb := testutil.NewTestDB()
	defer tdb.Close()
	store.DB = tdb.DB

	now := time.Now().UTC()
	if err := store.DB.Create(&model.Session{SessionID: "s_widget_d1", SessionType: model.SessionTypeDirect, UpdatedAt: now, CreatedAt: now}).Error; err != nil {
		t.Fatalf("seed session error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{SessionID: "s_widget_d1", MemberID: 3001, MemberType: 1, LastActiveAt: now, JoinedAt: now}).Error; err != nil {
		t.Fatalf("seed owner member error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{SessionID: "s_widget_d1", MemberID: 3002, MemberType: 1, LastActiveAt: now, JoinedAt: now}).Error; err != nil {
		t.Fatalf("seed visitor member error: %v", err)
	}
	if err := store.DB.Create(&model.User{ID: 3001, Username: "owner", Email: "owner@example.com", Nickname: "Owner", PasswordHash: "x"}).Error; err != nil {
		t.Fatalf("seed owner user error: %v", err)
	}
	if err := store.DB.Create(&model.User{ID: 3002, Username: "visitor", Email: "visitor@example.com", Nickname: "Visitor", PasswordHash: "x"}).Error; err != nil {
		t.Fatalf("seed visitor user error: %v", err)
	}
	if err := store.DB.Create(&model.WidgetSite{ID: 7001, OwnerUserID: 3001, SiteKey: "wk_meta", SiteSecretHash: "hash", SiteName: "Demo Site", Status: model.WidgetSiteStatusActive, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed widget site error: %v", err)
	}
	if err := store.DB.Create(&model.WidgetSession{ID: 7002, SiteID: 7001, OwnerUserID: 3001, VisitorID: 3002, VisitorKey: "vk_meta", SessionID: "s_widget_d1", VisitorName: "Alice", VisitorEmail: "alice@example.com", LastPageURL: "https://demo.example.com/p/1", Status: model.WidgetSessionStatusActive, CreatedAt: now, UpdatedAt: now, LastActiveAt: now}).Error; err != nil {
		t.Fatalf("seed widget session error: %v", err)
	}

	resp, err := SessionDetail(3001, "s_widget_d1")
	if err != nil {
		t.Fatalf("SessionDetail() error = %v", err)
	}
	if !resp.IsVisitor {
		t.Fatalf("expected IsVisitor=true")
	}
	if resp.VisitorInfo == nil {
		t.Fatalf("expected visitor_info not nil")
	}
	if resp.VisitorInfo.SiteName != "Demo Site" {
		t.Fatalf("visitor site_name=%q", resp.VisitorInfo.SiteName)
	}
}
