//go:build pgverify

package store

import (
	"os"
	"testing"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// TestAllMigrationsOnPostgres executes the additive migration chain, including
// migration 127, against a disposable real PostgreSQL database. SQLite cannot
// validate PostgreSQL DDL, JSONB indexes, or locking-related column types.
func TestAllMigrationsOnPostgres(t *testing.T) {
	logger.Init()
	dsn := os.Getenv("AIBOT_TEST_PG_MIGRATION_DSN")
	if dsn == "" {
		t.Skip("AIBOT_TEST_PG_MIGRATION_DSN not set")
	}
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), &gorm.Config{})
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	if err := ApplyMigrationsFromDir(db, "../../migration"); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{"user_sync_heads", "user_sync_events", "device_sync_cursors", "sync_command_receipts"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("migration 127 table %s missing", table)
		}
	}
}
