//go:build pgverify

package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestWidgetSiteFirstOriginBackfillOrder 在真实 PostgreSQL 上验证 131 迁移的回填顺序：
// 先用该站点最早一条 widget_sessions.last_page_url 的 host，其次 allowed_origins 第一项，
// 都没有则留空；重复执行不会覆盖已写入的值。
//
// 运行方式：
//
//	AIBOT_TEST_PG_MIGRATION_DSN="host=/tmp user=<user> dbname=<disposable> sslmode=disable" \
//	  go test -tags pgverify ./internal/store -run TestWidgetSiteFirstOriginBackfillOrder -v
func TestWidgetSiteFirstOriginBackfillOrder(t *testing.T) {
	dsn := os.Getenv("AIBOT_TEST_PG_MIGRATION_DSN")
	if dsn == "" {
		t.Skip("AIBOT_TEST_PG_MIGRATION_DSN not set")
	}

	schema := fmt.Sprintf("m131_first_origin_%d", time.Now().UnixNano())
	base, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	if err := base.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatalf("create scratch schema: %v", err)
	}
	t.Cleanup(func() {
		if err := base.Exec("DROP SCHEMA " + schema + " CASCADE").Error; err != nil {
			t.Logf("drop scratch schema: %v", err)
		}
		sqlDB, _ := base.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})

	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open scratch schema: %v", err)
	}
	sqlDB, _ := db.DB()
	if sqlDB != nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}

	execMigrationFile := func(name string) {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join("..", "..", "migration", name))
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		if err := db.Exec(string(raw)).Error; err != nil {
			t.Fatalf("apply migration %s: %v", name, err)
		}
	}

	execMigrationFile("045_widget_sites.sql")
	execMigrationFile("046_widget_sessions.sql")

	seedSite := func(id int64, key, name, origins string) {
		t.Helper()
		if err := db.Exec(`INSERT INTO widget_sites (id, owner_user_id, site_key, site_secret_hash, site_name, allowed_origins)
			VALUES (?, 1, ?, 'hash', ?, ?::jsonb)`, id, key, name, origins).Error; err != nil {
			t.Fatalf("seed site %d: %v", id, err)
		}
	}
	seedSession := func(id, siteID int64, sessionID, pageURL string, createdAt time.Time) {
		t.Helper()
		if err := db.Exec(`INSERT INTO widget_sessions (id, site_id, owner_user_id, visitor_id, visitor_key, session_id, last_page_url, created_at, updated_at, last_active_at)
			VALUES (?, ?, 1, 2, ?, ?, ?, ?, ?, ?)`, id, siteID, "vk_"+sessionID, sessionID, pageURL, createdAt, createdAt, createdAt).Error; err != nil {
			t.Fatalf("seed session %s: %v", sessionID, err)
		}
	}

	baseTime := time.Now().UTC().Add(-24 * time.Hour)
	// 站点 1：最早会话的 last_page_url 优先，且要规范化（大小写、端口、路径、query）。
	seedSite(1, "wk_pages", "Pages Site", `[]`)
	seedSession(11, 1, "s_oldest", "https://Shop.Example.com:8443/a/b?q=1", baseTime)
	seedSession(12, 1, "s_newer", "https://later.example.com/x", baseTime.Add(time.Hour))
	// 站点 2：没有可用会话 URL，退回 allowed_origins 第一项。
	seedSite(2, "wk_origins", "Origins Site", `["https://First.Example.com:3000/page/1","https://second.example.com"]`)
	seedSession(21, 2, "s_relative", "/relative/path", baseTime)
	// 站点 3：都没有，留空。
	seedSite(3, "wk_empty", "Empty Site", `[]`)

	execMigrationFile("131_widget_sites_first_origin.sql")

	firstOriginOf := func(id int64) string {
		t.Helper()
		var got string
		if err := db.Raw(`SELECT first_origin FROM widget_sites WHERE id = ?`, id).Scan(&got).Error; err != nil {
			t.Fatalf("read first_origin of site %d: %v", id, err)
		}
		return got
	}

	if got := firstOriginOf(1); got != "shop.example.com" {
		t.Fatalf("site 1 first_origin = %q, want earliest session host %q", got, "shop.example.com")
	}
	if got := firstOriginOf(2); got != "first.example.com" {
		t.Fatalf("site 2 first_origin = %q, want first allowed origin host %q", got, "first.example.com")
	}
	if got := firstOriginOf(3); got != "" {
		t.Fatalf("site 3 first_origin = %q, want empty", got)
	}

	// 幂等：再跑一次回填不改变结果（只写 first_origin = '' 的行）。
	execMigrationFile("131_widget_sites_first_origin.sql")
	if got := firstOriginOf(1); got != "shop.example.com" {
		t.Fatalf("site 1 first_origin changed on re-run: %q", got)
	}
	if got := firstOriginOf(2); got != "first.example.com" {
		t.Fatalf("site 2 first_origin changed on re-run: %q", got)
	}
}
