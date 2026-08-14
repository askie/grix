package inboxseq

import (
	"context"
	"fmt"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

func TestAllocateNextBatchTxWithoutRedis(t *testing.T) {
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	previousRedis := store.RDB
	store.RDB = nil
	defer func() {
		store.RDB = previousRedis
		testDB.Close()
	}()

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	fixture.CreateUserWithDefaults(2001, "user2001")
	fixture.CreateUserWithDefaults(2002, "user2002")

	for _, row := range []model.UserInbox{
		{UserID: 2001, InboxSeq: 4, MsgID: 9001, SessionID: "seq-session-1"},
		{UserID: 2002, InboxSeq: 9, MsgID: 9002, SessionID: "seq-session-2"},
	} {
		inboxRow := row
		if err := testDB.DB.Create(&inboxRow).Error; err != nil {
			t.Fatalf("seed user_inbox error: %v", err)
		}
	}

	err := testDB.DB.Transaction(func(tx *gorm.DB) error {
		nextSeqByUser, err := AllocateNextBatchTx(
			context.Background(),
			tx,
			[]int64{2002, 2001, 2002},
		)
		if err != nil {
			return err
		}
		if nextSeqByUser[2001] != 5 {
			return fmt.Errorf("user 2001 next_seq=%d want=5", nextSeqByUser[2001])
		}
		if nextSeqByUser[2002] != 10 {
			return fmt.Errorf("user 2002 next_seq=%d want=10", nextSeqByUser[2002])
		}
		if len(nextSeqByUser) != 2 {
			return fmt.Errorf("next_seq map len=%d want=2", len(nextSeqByUser))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("transaction error: %v", err)
	}
}

func TestAllocateNextBatchTxFallsBackWhenRedisCommandFails(t *testing.T) {
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	previousRedis := store.RDB
	brokenRedis := testutil.NewMockRedis()
	if err := brokenRedis.Close(); err != nil {
		t.Fatalf("close redis client error: %v", err)
	}
	store.RDB = brokenRedis
	defer func() {
		store.RDB = previousRedis
		testDB.Close()
	}()

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	fixture.CreateUserWithDefaults(2101, "user2101")

	if err := testDB.DB.Create(&model.UserInbox{
		UserID: 2101, InboxSeq: 7, MsgID: 9101, SessionID: "seq-session-fallback",
	}).Error; err != nil {
		t.Fatalf("seed user_inbox error: %v", err)
	}

	err := testDB.DB.Transaction(func(tx *gorm.DB) error {
		nextSeqByUser, err := AllocateNextBatchTx(
			context.Background(),
			tx,
			[]int64{2101},
		)
		if err != nil {
			return err
		}
		if nextSeqByUser[2101] != 8 {
			return fmt.Errorf("user 2101 next_seq=%d want=8", nextSeqByUser[2101])
		}
		return nil
	})
	if err != nil {
		t.Fatalf("transaction error: %v", err)
	}
}

// 缺陷三回归：发消息路径（Redis 发号）与撤回已送达消息路径（改造后同样走 Redis）
// 对同一用户先后发号时，必须拿到互不相同的序号，即使两次分配之间 DB 当前最大值
// 尚未推进（模拟前一笔事务未提交）。改造前撤回走 DB MAX+1，会与发消息在途序号撞号。
func TestInboxSeqSingleSourceAvoidsCrossPathCollision(t *testing.T) {
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	previousRedis := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = previousRedis
		testDB.Close()
	}()

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	fixture.CreateUserWithDefaults(3001, "user3001")

	// 原投递行：inbox_seq=100。
	if err := testDB.DB.Create(&model.UserInbox{
		UserID: 3001, InboxSeq: 100, MsgID: 7001, SessionID: "collision-session",
	}).Error; err != nil {
		t.Fatalf("seed user_inbox error: %v", err)
	}

	err := testDB.DB.Transaction(func(tx *gorm.DB) error {
		// 发消息路径发号（走 Redis），此时尚未写入新的 user_inbox 行，DB MAX 仍是 100。
		sendSeqByUser, err := AllocateNextBatchTx(context.Background(), tx, []int64{3001})
		if err != nil {
			return err
		}
		// 撤回已送达消息路径发号：floor=原投递序号 100，改造后同样走 Redis。
		revokeSeqByUser, err := AllocateNextBatchWithFloorTx(
			context.Background(),
			tx,
			[]int64{3001},
			map[int64]int64{3001: 100},
		)
		if err != nil {
			return err
		}

		sendSeq := sendSeqByUser[3001]
		revokeSeq := revokeSeqByUser[3001]
		if sendSeq == revokeSeq {
			return fmt.Errorf("cross-path collision: both allocations got %d", sendSeq)
		}
		if sendSeq <= 100 || revokeSeq <= 100 {
			return fmt.Errorf("allocations must exceed db max=100, got send=%d revoke=%d", sendSeq, revokeSeq)
		}
		// 单源原子递增：第一笔 101，第二笔 102。
		if sendSeq != 101 || revokeSeq != 102 {
			return fmt.Errorf("expected send=101 revoke=102, got send=%d revoke=%d", sendSeq, revokeSeq)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("transaction error: %v", err)
	}
}

// floor 语义：分配序号必须严格大于 max(DB当前最大, extraFloor)。覆盖 floor>MAX 与
// floor<MAX 两种情况（Redis 路径）。
func TestAllocateNextBatchWithFloorTxAppliesFloorRedis(t *testing.T) {
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	previousRedis := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = previousRedis
		testDB.Close()
	}()

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	fixture.CreateUserWithDefaults(3101, "user3101")
	fixture.CreateUserWithDefaults(3102, "user3102")

	for _, row := range []model.UserInbox{
		{UserID: 3101, InboxSeq: 5, MsgID: 7101, SessionID: "floor-session-1"},
		{UserID: 3102, InboxSeq: 50, MsgID: 7102, SessionID: "floor-session-2"},
	} {
		inboxRow := row
		if err := testDB.DB.Create(&inboxRow).Error; err != nil {
			t.Fatalf("seed user_inbox error: %v", err)
		}
	}

	err := testDB.DB.Transaction(func(tx *gorm.DB) error {
		nextSeqByUser, err := AllocateNextBatchWithFloorTx(
			context.Background(),
			tx,
			[]int64{3101, 3102},
			// 3101: floor=20 > DBmax=5 → 21；3102: floor=10 < DBmax=50 → 51。
			map[int64]int64{3101: 20, 3102: 10},
		)
		if err != nil {
			return err
		}
		if nextSeqByUser[3101] != 21 {
			return fmt.Errorf("user 3101 next_seq=%d want=21 (floor>max)", nextSeqByUser[3101])
		}
		if nextSeqByUser[3102] != 51 {
			return fmt.Errorf("user 3102 next_seq=%d want=51 (max>floor)", nextSeqByUser[3102])
		}
		return nil
	})
	if err != nil {
		t.Fatalf("transaction error: %v", err)
	}
}

// floor 语义在 Redis 不可用降级到 DB 路径时同样成立。
func TestAllocateNextBatchWithFloorTxAppliesFloorWithoutRedis(t *testing.T) {
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	previousRedis := store.RDB
	store.RDB = nil
	defer func() {
		store.RDB = previousRedis
		testDB.Close()
	}()

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	fixture.CreateUserWithDefaults(3201, "user3201")
	fixture.CreateUserWithDefaults(3202, "user3202")

	for _, row := range []model.UserInbox{
		{UserID: 3201, InboxSeq: 5, MsgID: 7201, SessionID: "floor-db-session-1"},
		{UserID: 3202, InboxSeq: 50, MsgID: 7202, SessionID: "floor-db-session-2"},
	} {
		inboxRow := row
		if err := testDB.DB.Create(&inboxRow).Error; err != nil {
			t.Fatalf("seed user_inbox error: %v", err)
		}
	}

	err := testDB.DB.Transaction(func(tx *gorm.DB) error {
		nextSeqByUser, err := AllocateNextBatchWithFloorTx(
			context.Background(),
			tx,
			[]int64{3201, 3202},
			map[int64]int64{3201: 20, 3202: 10},
		)
		if err != nil {
			return err
		}
		if nextSeqByUser[3201] != 21 {
			return fmt.Errorf("user 3201 next_seq=%d want=21 (floor>max)", nextSeqByUser[3201])
		}
		if nextSeqByUser[3202] != 51 {
			return fmt.Errorf("user 3202 next_seq=%d want=51 (max>floor)", nextSeqByUser[3202])
		}
		return nil
	})
	if err != nil {
		t.Fatalf("transaction error: %v", err)
	}
}
