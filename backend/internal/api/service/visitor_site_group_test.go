package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
)

// 列表接口（/sessions/list、sync 快照）下发的访客会话必须带所属站点 id 和该站点
// 的首个接入域名；站点没有记录域名时回退 site_name。
func TestBuildSessionItemsIncludesVisitorSite(t *testing.T) {
	tdb, cleanup := setupSessionTest(t)
	defer cleanup()

	now := time.Now().UTC()
	if err := tdb.DB.Create(&model.Session{
		SessionID: "s_vis_group_a", SessionType: model.SessionTypeDirect, LastMsgSummary: "hello", UpdatedAt: now, CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed session a error: %v", err)
	}
	if err := tdb.DB.Create(&model.Session{
		SessionID: "s_vis_group_b", SessionType: model.SessionTypeDirect, LastMsgSummary: "hello", UpdatedAt: now, CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed session b error: %v", err)
	}
	if err := tdb.DB.Create(&[]model.SessionMember{
		{SessionID: "s_vis_group_a", MemberID: 1111, MemberType: 1, LastActiveAt: now, JoinedAt: now},
		{SessionID: "s_vis_group_a", MemberID: 2111, MemberType: 1, LastActiveAt: now, JoinedAt: now},
		{SessionID: "s_vis_group_b", MemberID: 1111, MemberType: 1, LastActiveAt: now, JoinedAt: now},
		{SessionID: "s_vis_group_b", MemberID: 2112, MemberType: 1, LastActiveAt: now, JoinedAt: now},
	}).Error; err != nil {
		t.Fatalf("seed members error: %v", err)
	}
	if err := tdb.DB.Create(&[]model.WidgetSite{
		{ID: 7101, OwnerUserID: 1111, SiteKey: "wk_group_a", SiteSecretHash: "hash", SiteName: "Shop A", FirstOrigin: "shop.example.com", Status: model.WidgetSiteStatusActive, CreatedAt: now, UpdatedAt: now},
		{ID: 7102, OwnerUserID: 1111, SiteKey: "wk_group_b", SiteSecretHash: "hash", SiteName: "Fallback Site", Status: model.WidgetSiteStatusActive, CreatedAt: now, UpdatedAt: now},
	}).Error; err != nil {
		t.Fatalf("seed widget sites error: %v", err)
	}
	if err := tdb.DB.Create(&[]model.WidgetSession{
		{ID: 9101, SiteID: 7101, OwnerUserID: 1111, VisitorID: 2111, VisitorKey: "vk_a", SessionID: "s_vis_group_a", VisitorName: "Alice", Status: model.WidgetSessionStatusActive, CreatedAt: now, UpdatedAt: now, LastActiveAt: now},
		{ID: 9102, SiteID: 7102, OwnerUserID: 1111, VisitorID: 2112, VisitorKey: "vk_b", SessionID: "s_vis_group_b", VisitorName: "Bob", Status: model.WidgetSessionStatusActive, CreatedAt: now, UpdatedAt: now, LastActiveAt: now},
	}).Error; err != nil {
		t.Fatalf("seed widget sessions error: %v", err)
	}

	items, err := buildSessionItems(1111, []model.SessionMember{
		{SessionID: "s_vis_group_a", MemberID: 1111, MemberType: 1, LastActiveAt: now},
		{SessionID: "s_vis_group_b", MemberID: 1111, MemberType: 1, LastActiveAt: now},
	})
	if err != nil {
		t.Fatalf("buildSessionItems() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items len=%d", len(items))
	}
	for _, item := range items {
		switch item.SessionID {
		case "s_vis_group_a":
			if !item.IsVisitor {
				t.Fatalf("session a should be a visitor session")
			}
			if item.VisitorSiteID != "7101" {
				t.Fatalf("session a visitor_site_id=%q want %q", item.VisitorSiteID, "7101")
			}
			if item.VisitorSiteDomain != "shop.example.com" {
				t.Fatalf("session a visitor_site_domain=%q want %q", item.VisitorSiteDomain, "shop.example.com")
			}
		case "s_vis_group_b":
			if item.VisitorSiteID != "7102" {
				t.Fatalf("session b visitor_site_id=%q want %q", item.VisitorSiteID, "7102")
			}
			if item.VisitorSiteDomain != "Fallback Site" {
				t.Fatalf("session b visitor_site_domain=%q want fallback site_name %q", item.VisitorSiteDomain, "Fallback Site")
			}
		default:
			t.Fatalf("unexpected session %q", item.SessionID)
		}
	}
}

// 会话列表（/conversations）同样要带站点 id 和域名。
func TestSessionConversationsIncludesVisitorSite(t *testing.T) {
	tdb, cleanup := setupSessionTest(t)
	defer cleanup()

	now := time.Now().UTC()
	if err := tdb.DB.Create(&model.Session{
		SessionID: "s_conv_visitor", SessionType: model.SessionTypeDirect, LastMsgSummary: "hi", UpdatedAt: now, CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed session error: %v", err)
	}
	if err := tdb.DB.Create(&[]model.SessionMember{
		{SessionID: "s_conv_visitor", MemberID: 1121, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
		{SessionID: "s_conv_visitor", MemberID: 2121, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now},
	}).Error; err != nil {
		t.Fatalf("seed members error: %v", err)
	}
	if err := tdb.DB.Create(&model.WidgetSite{
		ID: 7201, OwnerUserID: 1121, SiteKey: "wk_conv", SiteSecretHash: "hash", SiteName: "Shop", FirstOrigin: "conv.example.com", Status: model.WidgetSiteStatusActive, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed widget site error: %v", err)
	}
	if err := tdb.DB.Create(&model.WidgetSession{
		ID: 9201, SiteID: 7201, OwnerUserID: 1121, VisitorID: 2121, VisitorKey: "vk_conv", SessionID: "s_conv_visitor", VisitorName: "Alice", Status: model.WidgetSessionStatusActive, CreatedAt: now, UpdatedAt: now, LastActiveAt: now,
	}).Error; err != nil {
		t.Fatalf("seed widget session error: %v", err)
	}

	resp, err := SessionConversations(1121, 20, "")
	if err != nil {
		t.Fatalf("SessionConversations() error = %v", err)
	}
	if len(resp.List) != 1 {
		t.Fatalf("conversation len=%d", len(resp.List))
	}
	item := resp.List[0]
	if !item.IsVisitor {
		t.Fatalf("conversation should be a visitor conversation")
	}
	if item.VisitorSiteID != "7201" {
		t.Fatalf("conversation visitor_site_id=%q want %q", item.VisitorSiteID, "7201")
	}
	if item.VisitorSiteDomain != "conv.example.com" {
		t.Fatalf("conversation visitor_site_domain=%q want %q", item.VisitorSiteDomain, "conv.example.com")
	}
}

// 非访客会话不能出现站点字段：JSON payload 与旧版保持一致（omitempty）。
func TestNonVisitorSessionItemsOmitVisitorSite(t *testing.T) {
	tdb, cleanup := setupSessionTest(t)
	defer cleanup()

	now := time.Now().UTC()
	if err := tdb.DB.Create(&model.Session{
		SessionID: "s_plain_direct", SessionType: model.SessionTypeDirect, LastMsgSummary: "hi", UpdatedAt: now, CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed session error: %v", err)
	}
	if err := tdb.DB.Create(&[]model.SessionMember{
		{SessionID: "s_plain_direct", MemberID: 1131, MemberType: 1, LastActiveAt: now, JoinedAt: now},
		{SessionID: "s_plain_direct", MemberID: 2131, MemberType: 1, LastActiveAt: now, JoinedAt: now},
	}).Error; err != nil {
		t.Fatalf("seed members error: %v", err)
	}

	items, err := buildSessionItems(1131, []model.SessionMember{
		{SessionID: "s_plain_direct", MemberID: 1131, MemberType: 1, LastActiveAt: now},
	})
	if err != nil {
		t.Fatalf("buildSessionItems() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items len=%d", len(items))
	}
	if items[0].IsVisitor || items[0].VisitorSiteID != "" || items[0].VisitorSiteDomain != "" {
		t.Fatalf("plain session must not carry visitor site fields, got %#v", items[0])
	}
	raw, err := json.Marshal(items[0])
	if err != nil {
		t.Fatalf("marshal session item: %v", err)
	}
	for _, key := range []string{"visitor_site_id", "visitor_site_domain"} {
		if strings.Contains(string(raw), key) {
			t.Fatalf("plain session payload must omit %q: %s", key, raw)
		}
	}
}
