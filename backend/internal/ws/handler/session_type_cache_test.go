package handler

import (
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func TestLoadSessionTypeCachesImmutableValue(t *testing.T) {
	prev := store.DB
	tdb := testutil.NewTestDB()
	store.DB = tdb.DB
	defer func() { store.DB = prev; tdb.Close() }()

	sessionTypeCacheMu.Lock()
	sessionTypeCache = make(map[string]int16)
	sessionTypeCacheMu.Unlock()

	if err := store.DB.Create(&model.Session{SessionID: "s-grp", OwnerID: 1, SessionType: 2}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	// 未命中 → 回源 PG 得到 2 并回填
	if got := loadSessionType("s-grp"); got != 2 {
		t.Fatalf("first load=%d want=2", got)
	}
	// 命中缓存:即使 DB 改了(不可变假设),仍返回缓存值
	store.DB.Model(&model.Session{}).Where("session_id = ?", "s-grp").Update("session_type", 1)
	if got := loadSessionType("s-grp"); got != 2 {
		t.Fatalf("cached load=%d want=2 (immutable cache)", got)
	}
	// 不存在的会话 → 默认 1,且不缓存
	if got := loadSessionType("s-missing"); got != 1 {
		t.Fatalf("missing load=%d want=1", got)
	}
}
