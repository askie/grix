//go:build pgverify

package service

// 真实 Postgres 回归验证：生产连接开启 PreferSimpleProtocol（兼容 PgBouncer），
// 简单协议下 pgx 会把 []byte 参数当 bytea 渲染，写 jsonb 列报 SQLSTATE 22P02；
// 必须传 string（或实现 Valuer 的类型）。SQLite 单测无法覆盖此差异。
// 运行: AIBOT_TEST_PG_DSN="host=127.0.0.1 port=5432 user=aibot password=... dbname=aibot sslmode=disable" \
//       go test -tags pgverify -run TestReachStatsJSONBWriteOnPostgres ./internal/api/service/ -v

import (
	"encoding/json"
	"os"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestReachStatsJSONBWriteOnPostgres(t *testing.T) {
	dsn := os.Getenv("AIBOT_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("AIBOT_TEST_PG_DSN not set")
	}
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), &gorm.Config{})
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	db.Exec(`DROP TABLE IF EXISTS jsonb_verify_tmp`)
	if err := db.Exec(`CREATE TABLE jsonb_verify_tmp (id bigint primary key, stats jsonb not null default '{}')`).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DROP TABLE IF EXISTS jsonb_verify_tmp`)
	db.Exec(`INSERT INTO jsonb_verify_tmp (id) VALUES (1)`)

	statsJSON, _ := json.Marshal(map[string]int{"sent": 0, "skipped": 1})

	// 坏写法：raw []byte 在简单协议下必须失败（这正是线上 22P02 的根因）
	bad := db.Table("jsonb_verify_tmp").Where("id = ?", 1).Updates(map[string]any{"stats": statsJSON})
	if bad.Error == nil {
		t.Log("warning: raw []byte unexpectedly accepted (driver behavior changed?)")
	} else {
		t.Logf("raw []byte rejected as expected: %v", bad.Error)
	}

	// 修复写法：string 必须成功
	good := db.Table("jsonb_verify_tmp").Where("id = ?", 1).Updates(map[string]any{"stats": string(statsJSON)})
	if good.Error != nil {
		t.Fatalf("string write must succeed: %v", good.Error)
	}
	var out string
	db.Raw(`SELECT stats::text FROM jsonb_verify_tmp WHERE id = 1`).Scan(&out)
	t.Logf("stored: %s", out)
}
