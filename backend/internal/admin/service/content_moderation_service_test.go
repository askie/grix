package service

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/systemsetting"
	"gorm.io/datatypes"
)

func TestUpdateContentModerationSettings(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	admin := createAdminFixture(t, testDB, 4601, "root", "Root", "RootPassword123A", model.AdminStatusActive)

	if err := systemsetting.SaveContentModerationSettings(systemsetting.ContentModerationSettings{
		Enabled:            false,
		Keywords:           []string{"legacy"},
		HumanMuteThreshold: 1,
	}, nil); err != nil {
		t.Fatalf("SaveContentModerationSettings(origin) error = %v", err)
	}
	if _, err := GetContentModerationSettings(); err != nil {
		t.Fatalf("GetContentModerationSettings() warm cache error = %v", err)
	}

	settings := systemsetting.ContentModerationSettings{
		Enabled:            true,
		Keywords:           []string{" Forbidden ", "敏感词", "forbidden"},
		HumanMuteThreshold: 5,
	}
	if err := UpdateContentModerationSettings(admin.ID, settings, "127.0.0.1", "test-agent"); err != nil {
		t.Fatalf("UpdateContentModerationSettings() error = %v", err)
	}

	loaded, err := GetContentModerationSettings()
	if err != nil {
		t.Fatalf("GetContentModerationSettings() error = %v", err)
	}
	if !loaded.Enabled {
		t.Fatal("expected content moderation enabled")
	}
	if loaded.HumanMuteThreshold != 5 {
		t.Fatalf("expected human_mute_threshold=5, got %d", loaded.HumanMuteThreshold)
	}

	wantKeywords := []string{"forbidden", "敏感词"}
	if len(loaded.Keywords) != len(wantKeywords) {
		t.Fatalf("expected %d keywords, got %d", len(wantKeywords), len(loaded.Keywords))
	}
	for index, keyword := range wantKeywords {
		if loaded.Keywords[index] != keyword {
			t.Fatalf("expected keyword[%d]=%q, got %q", index, keyword, loaded.Keywords[index])
		}
	}

	var log model.AdminOperationLog
	if err := testDB.DB.
		Where("action = ? AND target_id = ?", "content_moderation_settings_update", "content_moderation").
		Order("id DESC").
		First(&log).Error; err != nil {
		t.Fatalf("load operation log: %v", err)
	}
	if log.AdminID != admin.ID {
		t.Fatalf("expected admin id %d, got %d", admin.ID, log.AdminID)
	}

	detail := systemsetting.ContentModerationSettings{}
	if err := json.Unmarshal(log.Detail, &detail); err != nil {
		t.Fatalf("unmarshal operation detail: %v", err)
	}
	if detail.HumanMuteThreshold != 5 {
		t.Fatalf("expected log threshold 5, got %d", detail.HumanMuteThreshold)
	}
}

func TestUpdateContentModerationSettingsRejectsNonPositiveThreshold(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	admin := createAdminFixture(t, testDB, 4602, "root", "Root", "RootPassword123A", model.AdminStatusActive)

	err := UpdateContentModerationSettings(admin.ID, systemsetting.ContentModerationSettings{
		Enabled:            true,
		Keywords:           []string{"forbidden"},
		HumanMuteThreshold: 0,
	}, "127.0.0.1", "test-agent")
	if err == nil {
		t.Fatal("expected UpdateContentModerationSettings() to fail")
	}
	if err.Error() != "累计命中禁言阈值必须为正整数" {
		t.Fatalf("expected threshold validation error, got %v", err)
	}
}

func TestUpdateContentModerationSettingsWritesSystemSettingRow(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	admin := createAdminFixture(t, testDB, 4603, "root", "Root", "RootPassword123A", model.AdminStatusActive)

	if err := UpdateContentModerationSettings(admin.ID, systemsetting.ContentModerationSettings{
		Enabled:            true,
		Keywords:           []string{"review"},
		HumanMuteThreshold: 2,
	}, "127.0.0.1", "test-agent"); err != nil {
		t.Fatalf("UpdateContentModerationSettings() error = %v", err)
	}

	var row model.SystemSetting
	if err := testDB.DB.First(&row, "key = ?", "content_moderation").Error; err != nil {
		t.Fatalf("load system setting row: %v", err)
	}
	if row.UpdatedBy == nil || *row.UpdatedBy != admin.ID {
		t.Fatalf("expected updated_by=%d, got %#v", admin.ID, row.UpdatedBy)
	}

	value := systemsetting.ContentModerationSettings{}
	if err := json.Unmarshal(row.Value, &value); err != nil {
		t.Fatalf("unmarshal setting value: %v", err)
	}
	if !value.Enabled {
		t.Fatal("expected stored settings enabled")
	}
	if value.HumanMuteThreshold != 2 {
		t.Fatalf("expected stored threshold=2, got %d", value.HumanMuteThreshold)
	}
	if len(value.Keywords) != 1 || value.Keywords[0] != "review" {
		t.Fatalf("unexpected stored keywords: %#v", value.Keywords)
	}
}

func TestListContentModerationEventsIncludesCurrentMuteState(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	user := createUserFixture(t, testDB, 4801, "moderation-user", "moderation-user@example.com")
	createModerationSessionFixture(t, testDB, "moderation-session-1", user.ID)
	createModerationSessionMemberFixture(t, testDB, "moderation-session-1", user.ID, true)
	createContentModerationEventFixture(t, testDB, "moderation-session-1", 6801, user.ID, []string{"spam", "scam"}, 3, true, "revoked")

	result, err := ListContentModerationEvents(ContentModerationEventListParams{
		MutedOnly: true,
		Page:      1,
		PageSize:  20,
	})
	if err != nil {
		t.Fatalf("ListContentModerationEvents() error = %v", err)
	}
	if result.Total != 1 {
		t.Fatalf("expected total=1, got %d", result.Total)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result.Items))
	}

	item := result.Items[0]
	if !item.CurrentlyMuted {
		t.Fatal("expected CurrentlyMuted=true")
	}
	if item.MatchedKeywordsText != "spam、scam" {
		t.Fatalf("expected MatchedKeywordsText to be populated, got %q", item.MatchedKeywordsText)
	}
	if item.RecallStatusText != "已撤回" {
		t.Fatalf("expected RecallStatusText=已撤回, got %q", item.RecallStatusText)
	}
}

func TestUnmuteModeratedSessionMember(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	admin := createAdminFixture(t, testDB, 4802, "root", "Root", "RootPassword123A", model.AdminStatusActive)
	user := createUserFixture(t, testDB, 4803, "mute-target", "mute-target@example.com")
	createModerationSessionFixture(t, testDB, "moderation-session-2", user.ID)
	createModerationSessionMemberFixture(t, testDB, "moderation-session-2", user.ID, true)
	createContentModerationEventFixture(t, testDB, "moderation-session-2", 6802, user.ID, []string{"spam"}, 3, true, "revoked")

	if err := UnmuteModeratedSessionMember(admin.ID, "moderation-session-2", user.ID, "127.0.0.1", "test-agent"); err != nil {
		t.Fatalf("UnmuteModeratedSessionMember() error = %v", err)
	}

	var member model.SessionMember
	if err := testDB.DB.First(&member, "session_id = ? AND member_id = ? AND member_type = 1", "moderation-session-2", user.ID).Error; err != nil {
		t.Fatalf("load session member: %v", err)
	}
	if member.IsSpeakMuted {
		t.Fatal("expected session member to be unmuted")
	}

	var log model.AdminOperationLog
	if err := testDB.DB.Where("action = ? AND target_id = ?", "content_moderation_unmute", "moderation-session-2:"+jsonInt64(user.ID)).
		Order("id DESC").
		First(&log).Error; err != nil {
		t.Fatalf("load unmute log: %v", err)
	}
}

func TestUnmuteModeratedSessionMemberReturnsErrorWhenNoChange(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	admin := createAdminFixture(t, testDB, 4806, "root", "Root", "RootPassword123A", model.AdminStatusActive)
	user := createUserFixture(t, testDB, 4807, "already-unmuted", "already-unmuted@example.com")
	createModerationSessionFixture(t, testDB, "moderation-session-noop", user.ID)
	createModerationSessionMemberFixture(t, testDB, "moderation-session-noop", user.ID, false)
	createContentModerationEventFixture(t, testDB, "moderation-session-noop", 6805, user.ID, []string{"spam"}, 3, true, "revoked")

	err := UnmuteModeratedSessionMember(admin.ID, "moderation-session-noop", user.ID, "127.0.0.1", "test-agent")
	if err == nil {
		t.Fatal("expected UnmuteModeratedSessionMember() to fail when no state changed")
	}
	if err != ErrContentModerationMuteNotActive {
		t.Fatalf("expected ErrContentModerationMuteNotActive, got %v", err)
	}
}

func TestUnmuteUserContentModerationSessions(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	admin := createAdminFixture(t, testDB, 4804, "root", "Root", "RootPassword123A", model.AdminStatusActive)
	user := createUserFixture(t, testDB, 4805, "mute-all-target", "mute-all-target@example.com")

	createModerationSessionFixture(t, testDB, "moderation-session-3", user.ID)
	createModerationSessionFixture(t, testDB, "moderation-session-4", user.ID)
	createModerationSessionMemberFixture(t, testDB, "moderation-session-3", user.ID, true)
	createModerationSessionMemberFixture(t, testDB, "moderation-session-4", user.ID, true)
	createContentModerationEventFixture(t, testDB, "moderation-session-3", 6803, user.ID, []string{"spam"}, 3, true, "revoked")
	createContentModerationEventFixture(t, testDB, "moderation-session-4", 6804, user.ID, []string{"scam"}, 4, true, "revoked")

	if err := UnmuteUserContentModerationSessions(admin.ID, user.ID, "127.0.0.1", "test-agent"); err != nil {
		t.Fatalf("UnmuteUserContentModerationSessions() error = %v", err)
	}

	for _, sessionID := range []string{"moderation-session-3", "moderation-session-4"} {
		var member model.SessionMember
		if err := testDB.DB.First(&member, "session_id = ? AND member_id = ? AND member_type = 1", sessionID, user.ID).Error; err != nil {
			t.Fatalf("load session member %s: %v", sessionID, err)
		}
		if member.IsSpeakMuted {
			t.Fatalf("expected %s to be unmuted", sessionID)
		}
	}

	var log model.AdminOperationLog
	if err := testDB.DB.Where("action = ? AND target_id = ?", "content_moderation_user_unmute", jsonInt64(user.ID)).
		Order("id DESC").
		First(&log).Error; err != nil {
		t.Fatalf("load user unmute log: %v", err)
	}

	detail := map[string]any{}
	if err := json.Unmarshal(log.Detail, &detail); err != nil {
		t.Fatalf("unmarshal log detail: %v", err)
	}
	if detail["session_count"] != float64(2) {
		t.Fatalf("expected session_count=2, got %#v", detail["session_count"])
	}
}

func TestUnmuteUserContentModerationSessionsReturnsErrorWhenNoChange(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	admin := createAdminFixture(t, testDB, 4808, "root", "Root", "RootPassword123A", model.AdminStatusActive)
	user := createUserFixture(t, testDB, 4809, "user-noop", "user-noop@example.com")

	err := UnmuteUserContentModerationSessions(admin.ID, user.ID, "127.0.0.1", "test-agent")
	if err == nil {
		t.Fatal("expected UnmuteUserContentModerationSessions() to fail when no state changed")
	}
	if err != ErrContentModerationMuteNotActive {
		t.Fatalf("expected ErrContentModerationMuteNotActive, got %v", err)
	}
}

func createModerationSessionFixture(t *testing.T, db *testutil.TestDB, sessionID string, ownerID int64) {
	t.Helper()

	session := &model.Session{
		SessionID:        sessionID,
		OwnerID:          ownerID,
		SessionType:      model.SessionTypeGroup,
		ModerationStatus: model.SessionModerationStatusActive,
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	}
	if err := db.DB.Create(session).Error; err != nil {
		t.Fatalf("create moderation session fixture: %v", err)
	}
}

func createModerationSessionMemberFixture(t *testing.T, db *testutil.TestDB, sessionID string, userID int64, muted bool) {
	t.Helper()

	member := &model.SessionMember{
		SessionID:    sessionID,
		MemberID:     userID,
		MemberType:   1,
		IsSpeakMuted: muted,
		Role:         1,
		JoinedAt:     time.Now().UTC(),
		LastActiveAt: time.Now().UTC(),
	}
	if err := db.DB.Create(member).Error; err != nil {
		t.Fatalf("create moderation session member fixture: %v", err)
	}
}

func createContentModerationEventFixture(
	t *testing.T,
	db *testutil.TestDB,
	sessionID string,
	msgID int64,
	userID int64,
	keywords []string,
	hitCount int,
	muteApplied bool,
	recallStatus string,
) {
	t.Helper()

	raw, err := json.Marshal(keywords)
	if err != nil {
		t.Fatalf("marshal keywords: %v", err)
	}

	event := &model.ContentModerationEvent{
		SessionID:       sessionID,
		MsgID:           msgID,
		SenderID:        userID,
		SenderType:      1,
		MatchedKeywords: datatypes.JSON(raw),
		RecallStatus:    recallStatus,
		HitCount:        hitCount,
		MuteApplied:     muteApplied,
		CreatedAt:       time.Now().UTC(),
	}
	if err := db.DB.Create(event).Error; err != nil {
		t.Fatalf("create content moderation event fixture: %v", err)
	}
}

func jsonInt64(value int64) string {
	return strconv.FormatInt(value, 10)
}
