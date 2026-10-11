package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
)

func TestSessionBootstrapPageEnumeratesMoreThanTenThousandWithStableKeyset(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	const userID = int64(19701)
	const total = 10001
	seedUser(t, testDB, userID)
	now := time.Now()
	sessions := make([]model.Session, 0, total)
	members := make([]model.SessionMember, 0, total)
	for i := 0; i < total; i++ {
		sid := fmt.Sprintf("bootstrap-%05d", i)
		sessions = append(sessions, model.Session{
			SessionID:        sid,
			OwnerID:          userID,
			SessionType:      model.SessionTypeGroup,
			ModerationStatus: model.SessionModerationStatusActive,
			UpdatedAt:        now.Add(-time.Duration(i) * time.Second),
		})
		members = append(members, model.SessionMember{
			SessionID:    sid,
			MemberID:     userID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now.Add(-time.Duration(i) * time.Second),
			JoinedAt:     now,
		})
	}
	if err := testDB.DB.CreateInBatches(&sessions, 400).Error; err != nil {
		t.Fatalf("create sessions: %v", err)
	}
	if err := testDB.DB.CreateInBatches(&members, 400).Error; err != nil {
		t.Fatalf("create members: %v", err)
	}
	if err := testDB.DB.Exec(`
		UPDATE session_members
		SET bootstrap_id = CAST(SUBSTR(session_id, 11) AS INTEGER) + 1
		WHERE member_id = ? AND member_type = 1
	`, userID).Error; err != nil {
		t.Fatalf("assign bootstrap ids: %v", err)
	}
	if err := testDB.DB.Create(&model.UserSyncHead{UserID: userID, HeadCursor: 77}).Error; err != nil {
		t.Fatalf("create sync head: %v", err)
	}

	first, err := SessionBootstrapPage(userID, 10000, 0, nil)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.List) != syncBootstrapPageLimit || !first.HasMore || first.BootstrapCursor != 200 {
		t.Fatalf("unexpected first page len=%d has_more=%v cursor=%d", len(first.List), first.HasMore, first.BootstrapCursor)
	}
	if first.SyncHeadCursor != 77 {
		t.Fatalf("first head=%d want=77", first.SyncHeadCursor)
	}

	// Both already-returned and not-yet-returned memberships change the fields
	// used by the ordinary session list ordering. The bootstrap keyset must not
	// move either row across a page boundary.
	if err := testDB.DB.Model(&model.SessionMember{}).
		Where("member_id = ? AND member_type = 1 AND session_id IN ?", userID, []string{"bootstrap-00010", "bootstrap-09000"}).
		Updates(map[string]interface{}{"last_active_at": now.Add(24 * time.Hour), "is_pinned": true}).Error; err != nil {
		t.Fatalf("reorder memberships: %v", err)
	}
	// Its session ID sorts before the first page, but the immutable sequence ID
	// appends it to the tail so it is still enumerated. Its post-head event may
	// also be replayed after bootstrap; applying that event is idempotent.
	lateSession := model.Session{
		SessionID:        "bootstrap--late",
		OwnerID:          userID,
		SessionType:      model.SessionTypeGroup,
		ModerationStatus: model.SessionModerationStatusActive,
		UpdatedAt:        now.Add(time.Hour),
	}
	lateMember := model.SessionMember{
		SessionID:    lateSession.SessionID,
		MemberID:     userID,
		MemberType:   1,
		Role:         3,
		LastActiveAt: now.Add(time.Hour),
		JoinedAt:     now,
	}
	if err := testDB.DB.Create(&lateSession).Error; err != nil {
		t.Fatalf("create late session: %v", err)
	}
	if err := testDB.DB.Create(&lateMember).Error; err != nil {
		t.Fatalf("create late member: %v", err)
	}
	if err := testDB.DB.Table("session_members").
		Where("session_id = ? AND member_id = ? AND member_type = 1", lateSession.SessionID, userID).
		UpdateColumn("bootstrap_id", total+1).Error; err != nil {
		t.Fatalf("assign late bootstrap id: %v", err)
	}
	var lateBootstrapID int64
	if err := testDB.DB.Model(&model.SessionMember{}).
		Select("bootstrap_id").
		Where("session_id = ? AND member_id = ? AND member_type = 1", lateSession.SessionID, userID).
		Scan(&lateBootstrapID).Error; err != nil || lateBootstrapID != total+1 {
		t.Fatalf("late bootstrap id=%d err=%v", lateBootstrapID, err)
	}
	if err := testDB.DB.Model(&model.UserSyncHead{}).
		Where("user_id = ?", userID).
		Update("head_cursor", 78).Error; err != nil {
		t.Fatalf("advance sync head: %v", err)
	}

	seen := make(map[string]struct{}, total)
	for _, item := range first.List {
		seen[item.SessionID] = struct{}{}
	}
	cursor := first.BootstrapCursor
	fixedHead := first.SyncHeadCursor
	pages := 1
	for {
		page, pageErr := SessionBootstrapPage(userID, syncBootstrapPageLimit, cursor, &fixedHead)
		if pageErr != nil {
			t.Fatalf("page %d: %v", pages+1, pageErr)
		}
		pages++
		if page.SyncHeadCursor != 77 {
			t.Fatalf("page %d head=%d want fixed 77", pages, page.SyncHeadCursor)
		}
		for _, item := range page.List {
			if _, duplicate := seen[item.SessionID]; duplicate {
				t.Fatalf("duplicate session %q on page %d", item.SessionID, pages)
			}
			seen[item.SessionID] = struct{}{}
		}
		if !page.HasMore {
			if page.BootstrapCursor != 0 {
				t.Fatalf("final cursor=%d want zero", page.BootstrapCursor)
			}
			break
		}
		if page.BootstrapCursor <= cursor {
			t.Fatalf("page %d cursor=%d after=%d", pages, page.BootstrapCursor, cursor)
		}
		cursor = page.BootstrapCursor
	}

	if pages != 51 {
		t.Fatalf("pages=%d want=51", pages)
	}
	if len(seen) != total+1 {
		t.Fatalf("unique sessions=%d want=%d", len(seen), total+1)
	}
	if _, found := seen[lateSession.SessionID]; !found {
		t.Fatalf("post-head session with appended bootstrap id was not returned")
	}
	for i := 0; i < total; i++ {
		sid := fmt.Sprintf("bootstrap-%05d", i)
		if _, found := seen[sid]; !found {
			t.Fatalf("missing pre-head session %q", sid)
		}
	}
}
