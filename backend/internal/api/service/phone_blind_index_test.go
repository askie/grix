package service

import (
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

// TestPhoneBlindUniqueIndexExcludesEmpty 复刻 migration 083 的 uq_users_phone_blind 谓词，
// 守住"空串 phone_blind 不进唯一索引"这条线：未绑手机的用户 GORM 会把 phone_blind 写成 ''，
// 若谓词只排除 NULL，多个无手机用户/解绑用户会撞唯一索引导致注册/解绑 500（审查发现的硬伤）。
// 单测/e2e 跑在 SQLite AutoMigrate 上不会建出 083 的索引，故这里手动建同款部分唯一索引补盲区。
func TestPhoneBlindUniqueIndexExcludesEmpty(t *testing.T) {
	store.DB = testutil.NewTestDB().DB
	if err := store.DB.Exec(`CREATE UNIQUE INDEX uq_users_phone_blind ON users (phone_blind)
		WHERE phone_blind IS NOT NULL AND phone_blind <> ''`).Error; err != nil {
		t.Fatalf("create partial unique index: %v", err)
	}

	// 两个无手机用户（phone_blind 零值 ''）必须都能注册，不撞唯一索引
	if err := store.DB.Create(&model.User{ID: 1, Username: "email1", Email: "e1@x.com"}).Error; err != nil {
		t.Fatalf("first empty-blind user should insert: %v", err)
	}
	if err := store.DB.Create(&model.User{ID: 2, Username: "email2", Email: "e2@x.com"}).Error; err != nil {
		t.Fatalf("second empty-blind user must NOT conflict (the bug): %v", err)
	}

	// 解绑场景：把已有手机用户的 blind 清回 ''，也不能撞索引
	if err := store.DB.Create(&model.User{ID: 3, Username: "phoneuser", Email: "e3@x.com", PhoneBlind: "hmac-aaa"}).Error; err != nil {
		t.Fatalf("phone user insert: %v", err)
	}
	if err := store.DB.Model(&model.User{}).Where("id = ?", 3).Update("phone_blind", "").Error; err != nil {
		t.Fatalf("unbind clearing blind to '' must not conflict: %v", err)
	}

	// 反向保证：相同的非空盲索引仍然唯一冲突（约束没被削弱）
	if err := store.DB.Create(&model.User{ID: 4, Username: "p4", Email: "e4@x.com", PhoneBlind: "hmac-dup"}).Error; err != nil {
		t.Fatalf("seed dup blind: %v", err)
	}
	if err := store.DB.Create(&model.User{ID: 5, Username: "p5", Email: "e5@x.com", PhoneBlind: "hmac-dup"}).Error; err == nil {
		t.Fatalf("duplicate non-empty phone_blind must conflict, but insert succeeded")
	}
}
