package service

import (
	"errors"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func setupFavoriteServiceTest(t *testing.T) func() {
	t.Helper()
	_ = snowflake.Init(1)
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	// NOTE: UserSessionFavorite is missing from store.autoMigrateModels —
	// build the table by hand so the test can run while the gap is filed.
	if err := store.DB.AutoMigrate(&model.UserSessionFavorite{}); err != nil {
		t.Fatalf("automigrate UserSessionFavorite: %v", err)
	}
	return func() { testDB.Close() }
}

func seedFavoriteSession(t *testing.T, sessionID string, ownerID int64, members ...int64) {
	t.Helper()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
		GroupName:   "group-" + sessionID,
	}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	for _, m := range members {
		if err := store.DB.Create(&model.SessionMember{
			SessionID:    sessionID,
			MemberID:     m,
			MemberType:   1,
			JoinedAt:     time.Now(),
			LastActiveAt: time.Now(),
		}).Error; err != nil {
			t.Fatalf("seed member: %v", err)
		}
	}
}

func TestAddSessionFavorite_RejectsNonMember(t *testing.T) {
	cleanup := setupFavoriteServiceTest(t)
	defer cleanup()

	seedFavoriteSession(t, "s1", 1, 1)
	err := AddSessionFavorite(99, "s1")
	if !errors.Is(err, ErrSessionFavoriteNotMember) {
		t.Fatalf("want NotMember, got %v", err)
	}
}

func TestAddSessionFavorite_DuplicateReturnsAlreadyExists(t *testing.T) {
	cleanup := setupFavoriteServiceTest(t)
	defer cleanup()

	seedFavoriteSession(t, "s1", 1, 1)
	if err := AddSessionFavorite(1, "s1"); err != nil {
		t.Fatalf("first add: %v", err)
	}
	err := AddSessionFavorite(1, "s1")
	if !errors.Is(err, ErrSessionFavoriteAlreadyExists) {
		t.Fatalf("want AlreadyExists, got %v", err)
	}
}

func TestAddSessionFavorite_EmptySessionID(t *testing.T) {
	cleanup := setupFavoriteServiceTest(t)
	defer cleanup()

	if err := AddSessionFavorite(1, "  "); err == nil {
		t.Fatalf("want error on empty session_id")
	}
}

func TestRemoveSessionFavorite_IdempotencyAndNotFound(t *testing.T) {
	cleanup := setupFavoriteServiceTest(t)
	defer cleanup()

	seedFavoriteSession(t, "s1", 1, 1)
	if err := AddSessionFavorite(1, "s1"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := RemoveSessionFavorite(1, "s1"); err != nil {
		t.Fatalf("first remove: %v", err)
	}
	err := RemoveSessionFavorite(1, "s1")
	if !errors.Is(err, ErrSessionFavoriteNotFound) {
		t.Fatalf("want NotFound on second remove, got %v", err)
	}
}

func TestGetSessionFavoriteStatus(t *testing.T) {
	cleanup := setupFavoriteServiceTest(t)
	defer cleanup()

	seedFavoriteSession(t, "s1", 1, 1)
	st, err := GetSessionFavoriteStatus(1, "s1")
	if err != nil {
		t.Fatalf("status err: %v", err)
	}
	if st.IsFavorited {
		t.Fatalf("should be false initially")
	}
	if err := AddSessionFavorite(1, "s1"); err != nil {
		t.Fatalf("add: %v", err)
	}
	st, _ = GetSessionFavoriteStatus(1, "s1")
	if !st.IsFavorited {
		t.Fatalf("should be true after add")
	}
}

func TestListFavoriteSessions_OrderingAndContent(t *testing.T) {
	cleanup := setupFavoriteServiceTest(t)
	defer cleanup()

	seedFavoriteSession(t, "s1", 1, 1)
	seedFavoriteSession(t, "s2", 1, 1)
	seedFavoriteSession(t, "s3", 1, 1)
	// Add favorites in order s1, s2, s3 — list should return DESC (s3, s2, s1)
	for _, sid := range []string{"s1", "s2", "s3"} {
		if err := AddSessionFavorite(1, sid); err != nil {
			t.Fatalf("add %s: %v", sid, err)
		}
		time.Sleep(1100 * time.Millisecond)
	}
	resp, err := ListFavoriteSessions(1, 100, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(resp.List) != 3 {
		t.Fatalf("want 3 items, got %d", len(resp.List))
	}
	if resp.List[0].SessionID != "s3" || resp.List[1].SessionID != "s2" || resp.List[2].SessionID != "s1" {
		t.Fatalf("wrong order: %+v", []string{resp.List[0].SessionID, resp.List[1].SessionID, resp.List[2].SessionID})
	}
	if resp.List[0].FavoritedAt == 0 {
		t.Fatalf("favorited_at should be populated")
	}
	if resp.List[0].Title == "" {
		t.Fatalf("title should be resolved via JOIN")
	}
}

func TestListFavoriteSessions_Pagination(t *testing.T) {
	cleanup := setupFavoriteServiceTest(t)
	defer cleanup()

	for i := 1; i <= 5; i++ {
		sid := "s" + string(rune('0'+i))
		seedFavoriteSession(t, sid, 1, 1)
		if err := AddSessionFavorite(1, sid); err != nil {
			t.Fatalf("add: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
	resp, err := ListFavoriteSessions(1, 2, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !resp.HasMore || len(resp.List) != 2 {
		t.Fatalf("page1 expected has_more=true len=2, got has_more=%v len=%d", resp.HasMore, len(resp.List))
	}
	resp2, _ := ListFavoriteSessions(1, 2, 4)
	if resp2.HasMore || len(resp2.List) != 1 {
		t.Fatalf("page3 expected has_more=false len=1, got has_more=%v len=%d", resp2.HasMore, len(resp2.List))
	}
}

func TestGetFavoriteSessionIDs(t *testing.T) {
	cleanup := setupFavoriteServiceTest(t)
	defer cleanup()

	seedFavoriteSession(t, "s1", 1, 1)
	seedFavoriteSession(t, "s2", 1, 1)
	_ = AddSessionFavorite(1, "s1")
	_ = AddSessionFavorite(1, "s2")
	ids, err := GetFavoriteSessionIDs(1)
	if err != nil {
		t.Fatalf("ids: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("want 2 ids, got %d (%v)", len(ids), ids)
	}
}

func TestListFavoriteSessionsForAgent_KeywordFilter(t *testing.T) {
	cleanup := setupFavoriteServiceTest(t)
	defer cleanup()

	if err := store.DB.Create(&model.Session{SessionID: "s1", OwnerID: 1, SessionType: 2, GroupName: "alpha team"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := store.DB.Create(&model.Session{SessionID: "s2", OwnerID: 1, SessionType: 2, GroupName: "beta squad"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{SessionID: "s1", MemberID: 1, MemberType: 1, JoinedAt: time.Now(), LastActiveAt: time.Now()}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{SessionID: "s2", MemberID: 1, MemberType: 1, JoinedAt: time.Now(), LastActiveAt: time.Now()}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	_ = AddSessionFavorite(1, "s1")
	_ = AddSessionFavorite(1, "s2")

	resp, err := ListFavoriteSessionsForAgent(1, "alpha", 20, 0)
	if err != nil {
		t.Fatalf("agent list: %v", err)
	}
	if len(resp.List) != 1 || resp.List[0].SessionID != "s1" {
		t.Fatalf("expected only s1 matched, got %+v", resp.List)
	}
}
