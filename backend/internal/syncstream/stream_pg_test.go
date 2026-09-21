//go:build pgverify

package syncstream

import (
	"os"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// TestAppendTxPostgresSerializesAndPublishesOnlyCommittedCursors verifies the
// PostgreSQL row-lock behavior that SQLite cannot model. The second writer for
// one user must wait for the first transaction, and a rolled-back first writer
// must leave neither an event nor a published head behind.
func TestAppendTxPostgresSerializesAndPublishesOnlyCommittedCursors(t *testing.T) {
	dsn := os.Getenv("AIBOT_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("AIBOT_TEST_PG_DSN not set")
	}
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), &gorm.Config{})
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	if err := db.AutoMigrate(&model.UserSyncHead{}, &model.UserSyncEvent{}); err != nil {
		t.Fatal(err)
	}

	userID := time.Now().UnixNano() & 0x3fffffffffffffff
	t.Cleanup(func() {
		_ = db.Where("user_id = ?", userID).Delete(&model.UserSyncEvent{}).Error
		_ = db.Where("user_id = ?", userID).Delete(&model.UserSyncHead{}).Error
	})

	tx1 := db.Begin()
	if tx1.Error != nil {
		t.Fatal(tx1.Error)
	}
	if _, err := AppendTx(tx1, []Event{{
		UserID: userID, Kind: "message.upsert", EntityType: "message",
		EntityID: "rolled-back", EntityVersion: 1, Payload: map[string]any{"value": "first"},
	}}); err != nil {
		_ = tx1.Rollback().Error
		t.Fatal(err)
	}

	type appendResult struct {
		cursor int64
		err    error
	}
	done := make(chan appendResult, 1)
	go func() {
		tx2 := db.Begin()
		if tx2.Error != nil {
			done <- appendResult{err: tx2.Error}
			return
		}
		rows, appendErr := AppendTx(tx2, []Event{{
			UserID: userID, Kind: "message.upsert", EntityType: "message",
			EntityID: "committed", EntityVersion: 1, Payload: map[string]any{"value": "second"},
		}})
		if appendErr != nil {
			_ = tx2.Rollback().Error
			done <- appendResult{err: appendErr}
			return
		}
		if err := tx2.Commit().Error; err != nil {
			done <- appendResult{err: err}
			return
		}
		done <- appendResult{cursor: rows[0].StreamCursor}
	}()

	select {
	case result := <-done:
		_ = tx1.Rollback().Error
		t.Fatalf("second writer escaped before first transaction ended: %+v", result)
	case <-time.After(200 * time.Millisecond):
	}
	if err := tx1.Rollback().Error; err != nil {
		t.Fatal(err)
	}

	select {
	case result := <-done:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.cursor != 1 {
			t.Fatalf("cursor after rollback=%d want=1", result.cursor)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second writer remained blocked after rollback")
	}

	var head model.UserSyncHead
	if err := db.First(&head, "user_id = ?", userID).Error; err != nil {
		t.Fatal(err)
	}
	if head.HeadCursor != 1 {
		t.Fatalf("published head=%d want=1", head.HeadCursor)
	}
	var events []model.UserSyncEvent
	if err := db.Where("user_id = ?", userID).Order("stream_cursor").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].EntityID != "committed" {
		t.Fatalf("events after rollback=%+v", events)
	}
}
