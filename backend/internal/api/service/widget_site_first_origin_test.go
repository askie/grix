package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/datatypes"
)

// 访客初始化时，Origin 校验通过后写入站点首个接入域名（规范化 host）。
// 只写第一次：同一站点第二次换一个已授权域名不会覆盖。
func TestWidgetVisitorInitStoresFirstOriginOnce(t *testing.T) {
	tdb := setupWidgetVisitorServiceTest(t)
	defer tdb.Close()

	seedWidgetSite(t, model.WidgetSite{
		ID:             9301,
		OwnerUserID:    901,
		SiteKey:        "wk_first_origin",
		SiteSecretHash: "hash",
		SiteName:       "Origin Shop",
		AllowedOrigins: datatypes.JSON([]byte(`["https://Shop.Example.com:8443","https://www.example.com"]`)),
		Status:         model.WidgetSiteStatusActive,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})

	initVisitor := func(origin, pageURL string) {
		t.Helper()
		if _, err := WidgetVisitorInit(WidgetVisitorInitInput{
			SiteKey:      "wk_first_origin",
			VisitorKey:   "vk_first_origin",
			VisitorName:  "Alice",
			VisitorEmail: "alice@example.com",
			PageURL:      pageURL,
			Origin:       origin,
			WSURL:        "wss://api.example.com/v1/widget/ws",
		}); err != nil {
			t.Fatalf("WidgetVisitorInit(%q) error = %v", origin, err)
		}
	}

	initVisitor("https://Shop.Example.com:8443", "https://shop.example.com/p/1")

	var site model.WidgetSite
	if err := store.DB.Where("id = ?", 9301).First(&site).Error; err != nil {
		t.Fatalf("load widget site error: %v", err)
	}
	if site.FirstOrigin != "shop.example.com" {
		t.Fatalf("first_origin = %q, want normalized host %q", site.FirstOrigin, "shop.example.com")
	}

	initVisitor("https://www.example.com", "https://www.example.com/p/2")

	if err := store.DB.Where("id = ?", 9301).First(&site).Error; err != nil {
		t.Fatalf("reload widget site error: %v", err)
	}
	if site.FirstOrigin != "shop.example.com" {
		t.Fatalf("first_origin was overwritten by a second origin: %q", site.FirstOrigin)
	}
}

// 从未接入过访客的站点不写 first_origin，保持空串。
func TestWidgetVisitorConfigDoesNotWriteFirstOrigin(t *testing.T) {
	tdb := setupWidgetVisitorServiceTest(t)
	defer tdb.Close()

	seedWidgetSite(t, model.WidgetSite{
		ID:             9302,
		OwnerUserID:    902,
		SiteKey:        "wk_config_only",
		SiteSecretHash: "hash",
		SiteName:       "Config Only",
		AllowedOrigins: datatypes.JSON([]byte(`["https://config.example.com"]`)),
		Status:         model.WidgetSiteStatusActive,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})

	if _, err := WidgetVisitorConfig("wk_config_only", "https://config.example.com", ""); err != nil {
		t.Fatalf("WidgetVisitorConfig() error = %v", err)
	}

	var site model.WidgetSite
	if err := store.DB.Where("id = ?", 9302).First(&site).Error; err != nil {
		t.Fatalf("load widget site error: %v", err)
	}
	if site.FirstOrigin != "" {
		t.Fatalf("config-only request must not record first_origin, got %q", site.FirstOrigin)
	}
}

// 新建访客会话时写出的 v2 增量事件必须自带 is_visitor 和站点字段：
// v2 客户端就是靠这条 session.upsert 首次落地该会话的。
func TestWidgetVisitorInitSyncEventCarriesVisitorSite(t *testing.T) {
	t.Setenv("AIBOT_SYNC_V2_ENABLED", "1")
	tdb := setupWidgetVisitorServiceTest(t)
	defer tdb.Close()

	seedWidgetSite(t, model.WidgetSite{
		ID:             9303,
		OwnerUserID:    903,
		SiteKey:        "wk_sync_event",
		SiteSecretHash: "hash",
		SiteName:       "Sync Shop",
		AllowedOrigins: datatypes.JSON([]byte(`["https://sync.example.com"]`)),
		Status:         model.WidgetSiteStatusActive,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})

	resp, err := WidgetVisitorInit(WidgetVisitorInitInput{
		SiteKey:      "wk_sync_event",
		VisitorKey:   "vk_sync_event",
		VisitorName:  "Alice",
		VisitorEmail: "alice@example.com",
		PageURL:      "https://sync.example.com/p/1",
		Origin:       "https://sync.example.com",
		WSURL:        "wss://api.example.com/v1/widget/ws",
	})
	if err != nil {
		t.Fatalf("WidgetVisitorInit() error = %v", err)
	}

	var events []model.UserSyncEvent
	if err := store.DB.
		Where("user_id = ? AND event_kind = ? AND entity_id = ?", 903, "session.upsert", resp.SessionID).
		Find(&events).Error; err != nil {
		t.Fatalf("load sync events error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("session.upsert events=%d, want 1", len(events))
	}
	var payload map[string]any
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload["is_visitor"] != true {
		t.Fatalf("session.upsert is_visitor=%v want true (payload=%v)", payload["is_visitor"], payload)
	}
	if payload["visitor_site_id"] != "9303" {
		t.Fatalf("session.upsert visitor_site_id=%v want %q", payload["visitor_site_id"], "9303")
	}
	if payload["visitor_site_domain"] != "sync.example.com" {
		t.Fatalf("session.upsert visitor_site_domain=%v want %q", payload["visitor_site_domain"], "sync.example.com")
	}
}

// Origin 归一化必须和迁移 131 的 SQL 规则一致：去掉协议、userinfo、端口、路径、
// query、fragment，转小写，IPv6 去方括号；`*` 和解析不出的值一律无效。
func TestNormalizeOriginHost(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{name: "scheme port path query", input: "https://Shop.Example.com:8443/a/b?q=1", want: "shop.example.com", ok: true},
		{name: "userinfo", input: "https://user:pass@example.com/", want: "example.com", ok: true},
		{name: "ipv6 brackets", input: "https://[2001:DB8::1]:8443/x", want: "2001:db8::1", ok: true},
		{name: "uppercase", input: "HTTPS://SHOP.EXAMPLE.COM/", want: "shop.example.com", ok: true},
		{name: "fragment only path", input: "https://frag.example.com/#top", want: "frag.example.com", ok: true},
		{name: "no scheme", input: "shop.example.com:8443/path?q=1", want: "shop.example.com", ok: true},
		{name: "wildcard", input: "*", ok: false},
		{name: "wildcard origin", input: "https://*", ok: false},
		{name: "fragment only", input: "#top", ok: false},
		{name: "empty", input: "   ", ok: false},
		{name: "non http scheme", input: "ftp://example.com", ok: false},
		{name: "non numeric port", input: "https://example.com:abc", ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := normalizeOriginHost(tc.input)
			if ok != tc.ok {
				t.Fatalf("normalizeOriginHost(%q) ok = %v, want %v (got %q)", tc.input, ok, tc.ok, got)
			}
			if ok && got != tc.want {
				t.Fatalf("normalizeOriginHost(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
