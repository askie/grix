package syncstream

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.UserSyncHead{}, &model.UserSyncEvent{}, &model.SyncCommandReceipt{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestClaimCommandTxIsDurableAndAtomic(t *testing.T) {
	db := openTestDB(t)
	claim := func() (bool, error) {
		var claimed bool
		err := db.Transaction(func(tx *gorm.DB) error {
			var err error
			claimed, err = ClaimCommandTx(tx, 7, "message.edit", "command-1", map[string]any{"ok": true})
			return err
		})
		return claimed, err
	}
	claimed, err := claim()
	if err != nil || !claimed {
		t.Fatalf("first claim=%v err=%v", claimed, err)
	}
	claimed, err = claim()
	if err != nil || claimed {
		t.Fatalf("duplicate claim=%v err=%v", claimed, err)
	}

	rollback := errors.New("rollback claim")
	if err := db.Transaction(func(tx *gorm.DB) error {
		claimed, err := ClaimCommandTx(tx, 7, "message.edit", "command-2", nil)
		if err != nil || !claimed {
			t.Fatalf("rollback claim=%v err=%v", claimed, err)
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatalf("rollback err=%v", err)
	}
	claimed, err = func() (bool, error) {
		var got bool
		err := db.Transaction(func(tx *gorm.DB) error {
			var err error
			got, err = ClaimCommandTx(tx, 7, "message.edit", "command-2", nil)
			return err
		})
		return got, err
	}()
	if err != nil || !claimed {
		t.Fatalf("claim after rollback=%v err=%v", claimed, err)
	}
}

func TestAppendTxPublishesOnlyOnCommitAndKeepsPerUserOrder(t *testing.T) {
	db := openTestDB(t)
	appendTwo := func(tx *gorm.DB) error {
		rows, err := AppendTx(tx, []Event{
			{UserID: 20, Kind: "message.upsert", EntityType: "message", EntityID: "m1", EntityVersion: 1, Payload: map[string]any{"value": "a"}},
			{UserID: 10, Kind: "session.upsert", EntityType: "session", EntityID: "s1", EntityVersion: 2, Payload: map[string]any{"value": "b"}},
			{UserID: 20, Kind: "message.revoke", EntityType: "message", EntityID: "m1", EntityVersion: 2, Tombstone: true, Payload: map[string]any{"value": "c"}},
		})
		if err != nil {
			return err
		}
		if rows[0].StreamCursor != 1 || rows[1].StreamCursor != 1 || rows[2].StreamCursor != 2 {
			t.Fatalf("unexpected cursors: %#v", rows)
		}
		return nil
	}
	if err := db.Transaction(appendTwo); err != nil {
		t.Fatal(err)
	}
	var head model.UserSyncHead
	if err := db.First(&head, "user_id = ?", 20).Error; err != nil {
		t.Fatal(err)
	}
	if head.HeadCursor != 2 {
		t.Fatalf("head=%d want=2", head.HeadCursor)
	}

	errRollback := errors.New("rollback")
	if err := db.Transaction(func(tx *gorm.DB) error {
		if _, err := AppendTx(tx, []Event{{UserID: 20, Kind: "message.upsert", EntityType: "message", EntityID: "m2", EntityVersion: 1, Payload: map[string]any{}}}); err != nil {
			return err
		}
		return errRollback
	}); !errors.Is(err, errRollback) {
		t.Fatalf("rollback error=%v", err)
	}
	if err := db.First(&head, "user_id = ?", 20).Error; err != nil {
		t.Fatal(err)
	}
	if head.HeadCursor != 2 {
		t.Fatalf("rollback published head=%d", head.HeadCursor)
	}
	var count int64
	if err := db.Model(&model.UserSyncEvent{}).Where("user_id = ?", 20).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("rollback persisted events=%d", count)
	}
}

func TestAppendTxRejectsMalformedEvent(t *testing.T) {
	db := openTestDB(t)
	err := db.Transaction(func(tx *gorm.DB) error {
		_, err := AppendTx(tx, []Event{{UserID: 1, Kind: "message.upsert"}})
		return err
	})
	if err == nil {
		t.Fatal("expected invalid event error")
	}
}

func TestNotifyDirtyHonorsFeatureGate(t *testing.T) {
	previous := store.RDB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		_ = store.RDB.Close()
		store.RDB = previous
	})

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:ws:route:42", "device", "node-sync-test").Err(); err != nil {
		t.Fatal(err)
	}
	pubsub := store.RDB.Subscribe(ctx, "chan:node-sync-test")
	t.Cleanup(func() { _ = pubsub.Close() })

	t.Setenv("AIBOT_SYNC_V2_ENABLED", "")
	NotifyDirty(42)
	select {
	case message := <-pubsub.Channel():
		t.Fatalf("disabled gate published dirty notification: %s", message.Payload)
	case <-time.After(20 * time.Millisecond):
	}

	t.Setenv("AIBOT_SYNC_V2_ENABLED", "1")
	NotifyDirty(42)
	select {
	case message := <-pubsub.Channel():
		var envelope struct {
			UserID int64  `json:"user_id"`
			Cmd    string `json:"cmd"`
		}
		if err := json.Unmarshal([]byte(message.Payload), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.UserID != 42 || envelope.Cmd != protocol.InternalCmdSyncV2Dirty {
			t.Fatalf("unexpected dirty envelope: %+v", envelope)
		}
	case <-time.After(time.Second):
		t.Fatal("enabled gate did not publish dirty notification")
	}
}
