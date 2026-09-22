//go:build pgverify

package inboxseq

// Real PostgreSQL regression for the cursor-watermark commit-order race.
// Run with:
//
//   AIBOT_TEST_PG_DSN="..." go test -tags pgverify \
//     -run TestRedisAllocationWaitsForEarlierUserTransactionCommit \
//     ./internal/pkg/inboxseq -v

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestRedisAllocationWaitsForEarlierUserTransactionCommit(t *testing.T) {
	dsn := os.Getenv("AIBOT_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("AIBOT_TEST_PG_DSN not set")
	}
	db, err := gorm.Open(
		postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}),
		&gorm.Config{},
	)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	if err := db.AutoMigrate(&model.UserInbox{}); err != nil {
		t.Fatal(err)
	}

	previousDB, previousRedis := store.DB, store.RDB
	store.DB = db
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.DB, store.RDB = previousDB, previousRedis
	}()

	// A unique positive advisory-lock key avoids interacting with other test or
	// development transactions on the same PostgreSQL database.
	userID := time.Now().UnixNano() & 0x3fffffffffffffff
	tx1 := db.Begin()
	if tx1.Error != nil {
		t.Fatal(tx1.Error)
	}
	seq1, err := AllocateNextBatchTx(context.Background(), tx1, []int64{userID})
	if err != nil {
		_ = tx1.Rollback().Error
		t.Fatal(err)
	}

	type allocation struct {
		seq int64
		err error
	}
	done := make(chan allocation, 1)
	go func() {
		tx2 := db.Begin()
		if tx2.Error != nil {
			done <- allocation{err: tx2.Error}
			return
		}
		seqs, allocErr := AllocateNextBatchTx(
			context.Background(),
			tx2,
			[]int64{userID},
		)
		if allocErr != nil {
			_ = tx2.Rollback().Error
			done <- allocation{err: allocErr}
			return
		}
		commitErr := tx2.Commit().Error
		done <- allocation{seq: seqs[userID], err: commitErr}
	}()

	select {
	case result := <-done:
		_ = tx1.Rollback().Error
		t.Fatalf(
			"second allocation escaped before first commit: seq=%d err=%v",
			result.seq,
			result.err,
		)
	case <-time.After(200 * time.Millisecond):
		// Expected: tx2 is waiting on tx1's per-user transaction lock.
	}

	if err := tx1.Commit().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.seq <= seq1[userID] {
			t.Fatalf("second seq=%d must exceed first seq=%d", result.seq, seq1[userID])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second allocation remained blocked after first transaction commit")
	}
}
