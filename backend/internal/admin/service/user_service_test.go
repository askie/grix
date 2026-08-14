package service

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
)

func TestUnlockUserLogin(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	store.RDB = testutil.NewMockRedis()

	admin := createAdminFixture(t, testDB, 4001, "root", "Root", "RootPassword123A", model.AdminStatusActive)
	user := createUserFixture(t, testDB, 4101, "lock-user", "lock-user@example.com")

	lockUserGuard(t, security.NewUserLoginGuardByUserID(user.ID))
	lockUserGuard(t, security.NewUserLoginGuardByAccount(user.Username))
	lockUserGuard(t, security.NewUserLoginGuardByAccount(user.Email))

	if err := UnlockUserLogin(admin.ID, user.ID, "127.0.0.1", "test-agent"); err != nil {
		t.Fatalf("UnlockUserLogin() error = %v", err)
	}

	assertGuardUnlocked(t, security.NewUserLoginGuardByUserID(user.ID))
	assertGuardUnlocked(t, security.NewUserLoginGuardByAccount(user.Username))
	assertGuardUnlocked(t, security.NewUserLoginGuardByAccount(user.Email))

	var log model.AdminOperationLog
	if err := testDB.DB.Where("action = ? AND target_id = ?", "user_login_unlock", strconv.FormatInt(user.ID, 10)).
		Order("id DESC").
		First(&log).Error; err != nil {
		t.Fatalf("load unlock operation log: %v", err)
	}
	if log.AdminID != admin.ID {
		t.Fatalf("expected admin id %d, got %d", admin.ID, log.AdminID)
	}

	detail := map[string]any{}
	if err := json.Unmarshal(log.Detail, &detail); err != nil {
		t.Fatalf("unmarshal unlock detail: %v", err)
	}
	if detail["username"] != user.Username {
		t.Fatalf("expected username %q, got %#v", user.Username, detail["username"])
	}
	if detail["email"] != user.Email {
		t.Fatalf("expected email %q, got %#v", user.Email, detail["email"])
	}
}

func TestListUsersIncludesLoginLockStatus(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	store.RDB = testutil.NewMockRedis()

	lockedUser := createUserFixture(t, testDB, 4201, "locked-user", "locked-user@example.com")
	normalUser := createUserFixture(t, testDB, 4202, "normal-user", "normal-user@example.com")
	lockUserGuard(t, security.NewUserLoginGuardByUserID(lockedUser.ID))

	result, err := ListUsers(UserListParams{
		Page:     1,
		PageSize: 20,
	})
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}

	itemByID := make(map[int64]UserListItem, len(result.Items))
	for _, item := range result.Items {
		itemByID[item.ID] = item
	}

	lockedItem, ok := itemByID[lockedUser.ID]
	if !ok {
		t.Fatalf("missing locked user %d in list", lockedUser.ID)
	}
	if !lockedItem.LoginLocked {
		t.Fatal("expected locked user to have LoginLocked=true")
	}
	if lockedItem.LockRemaining == "" {
		t.Fatal("expected locked user lock remaining text")
	}

	normalItem, ok := itemByID[normalUser.ID]
	if !ok {
		t.Fatalf("missing normal user %d in list", normalUser.ID)
	}
	if normalItem.LoginLocked {
		t.Fatal("expected normal user to have LoginLocked=false")
	}
	if normalItem.LockRemaining != "" {
		t.Fatalf("expected normal user lock remaining empty, got %q", normalItem.LockRemaining)
	}
}

func TestListUsersIncludesModerationMuteStatus(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	mutedUser := createUserFixture(t, testDB, 4203, "muted-user", "muted-user@example.com")
	normalUser := createUserFixture(t, testDB, 4204, "other-user", "other-user@example.com")
	createModerationSessionFixture(t, testDB, "user-list-moderation-session", mutedUser.ID)
	createModerationSessionMemberFixture(t, testDB, "user-list-moderation-session", mutedUser.ID, true)
	createContentModerationEventFixture(t, testDB, "user-list-moderation-session", 6201, mutedUser.ID, []string{"spam"}, 3, true, "revoked")

	result, err := ListUsers(UserListParams{
		Page:     1,
		PageSize: 20,
	})
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}

	itemByID := make(map[int64]UserListItem, len(result.Items))
	for _, item := range result.Items {
		itemByID[item.ID] = item
	}

	if !itemByID[mutedUser.ID].ModerationMuted {
		t.Fatal("expected muted user to have ModerationMuted=true")
	}
	if itemByID[mutedUser.ID].ModerationMuteSessionCount != 1 {
		t.Fatalf("expected moderation mute session count=1, got %d", itemByID[mutedUser.ID].ModerationMuteSessionCount)
	}
	if itemByID[normalUser.ID].ModerationMuted {
		t.Fatal("expected normal user to have ModerationMuted=false")
	}
}

func TestUpdateAuthSettingsAutoAddCustomerUserID(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	admin := createAdminFixture(t, testDB, 4301, "root", "Root", "RootPassword123A", model.AdminStatusActive)
	customer := createUserFixture(t, testDB, 4302, "customer-user", "customer-user@example.com")

	origin := systemsetting.DefaultAuthSettings()
	if err := systemsetting.SaveAuthSettings(origin, nil); err != nil {
		t.Fatalf("SaveAuthSettings(origin) error = %v", err)
	}
	if _, err := GetAuthSettings(); err != nil {
		t.Fatalf("GetAuthSettings() warm cache error = %v", err)
	}

	settings := systemsetting.DefaultAuthSettings()
	settings.AutoAddCustomerUserID = customer.ID
	if err := UpdateAuthSettings(admin.ID, settings, "127.0.0.1", "test-agent"); err != nil {
		t.Fatalf("UpdateAuthSettings() error = %v", err)
	}

	loaded, err := GetAuthSettings()
	if err != nil {
		t.Fatalf("GetAuthSettings() error = %v", err)
	}
	if loaded.AutoAddCustomerUserID != customer.ID {
		t.Fatalf("expected auto_add_customer_user_id=%d, got %d", customer.ID, loaded.AutoAddCustomerUserID)
	}
}

func TestUpdateAuthSettingsRejectsMissingAutoAddCustomerUser(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	admin := createAdminFixture(t, testDB, 4401, "root", "Root", "RootPassword123A", model.AdminStatusActive)

	settings := systemsetting.DefaultAuthSettings()
	settings.AutoAddCustomerUserID = 999999
	err := UpdateAuthSettings(admin.ID, settings, "127.0.0.1", "test-agent")
	if err == nil {
		t.Fatal("expected UpdateAuthSettings() to fail")
	}
	if err.Error() != "系统客户账户不存在" {
		t.Fatalf("expected missing customer user error, got %v", err)
	}
}

func TestUpdateGroupSettings(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	admin := createAdminFixture(t, testDB, 4501, "root", "Root", "RootPassword123A", model.AdminStatusActive)

	settings := systemsetting.GroupSettings{
		MemberInviteThreshold: 36,
	}
	if err := UpdateGroupSettings(admin.ID, settings, "127.0.0.1", "test-agent"); err != nil {
		t.Fatalf("UpdateGroupSettings() error = %v", err)
	}

	loaded, err := GetGroupSettings()
	if err != nil {
		t.Fatalf("GetGroupSettings() error = %v", err)
	}
	if loaded.MemberInviteThreshold != 36 {
		t.Fatalf("expected member_invite_threshold=36, got %d", loaded.MemberInviteThreshold)
	}
}

func TestUpdateGroupSettingsRejectsNonPositiveThreshold(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	admin := createAdminFixture(t, testDB, 4502, "root", "Root", "RootPassword123A", model.AdminStatusActive)
	err := UpdateGroupSettings(admin.ID, systemsetting.GroupSettings{
		MemberInviteThreshold: 0,
	}, "127.0.0.1", "test-agent")
	if err == nil {
		t.Fatal("expected UpdateGroupSettings() to fail")
	}
	if err.Error() != "群成员邀请阈值必须为正整数" {
		t.Fatalf("expected threshold validation error, got %v", err)
	}
}

func TestListUsersClampsPageToLastPage(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	createUserFixture(t, testDB, 4701, "page-user-1", "page-user-1@example.com")
	createUserFixture(t, testDB, 4702, "page-user-2", "page-user-2@example.com")
	createUserFixture(t, testDB, 4703, "page-user-3", "page-user-3@example.com")

	result, err := ListUsers(UserListParams{
		Page:     9,
		PageSize: 2,
	})
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if result.Page != 2 {
		t.Fatalf("expected clamped page=2, got %d", result.Page)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected 1 item on last page, got %d", len(result.Items))
	}
}

func TestListUsersFiltersByIDs(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	a := createUserFixture(t, testDB, 4801, "ids-a", "ids-a@example.com")
	b := createUserFixture(t, testDB, 4802, "ids-b", "ids-b@example.com")
	createUserFixture(t, testDB, 4803, "ids-c", "ids-c@example.com")

	result, err := ListUsers(UserListParams{
		IDs:      []int64{a.ID, b.ID},
		Page:     1,
		PageSize: 20,
	})
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if result.Total != 2 {
		t.Fatalf("expected total=2, got %d", result.Total)
	}
	got := map[int64]bool{}
	for _, item := range result.Items {
		got[item.ID] = true
	}
	if !got[a.ID] || !got[b.ID] {
		t.Fatalf("expected items %d & %d, got %#v", a.ID, b.ID, got)
	}
}

func TestListUsersIDsEmptyReturnsEmpty(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	createUserFixture(t, testDB, 4901, "ids-empty-a", "ids-empty-a@example.com")
	createUserFixture(t, testDB, 4902, "ids-empty-b", "ids-empty-b@example.com")

	result, err := ListUsers(UserListParams{
		IDs:      []int64{},
		Page:     1,
		PageSize: 20,
	})
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if result.Total != 0 || len(result.Items) != 0 {
		t.Fatalf("expected empty result, got total=%d items=%d", result.Total, len(result.Items))
	}
}

func TestListUsersOnlineOnlyPaginates(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	previous := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = previous
	}()

	first := createUserFixture(t, testDB, 5001, "online-first", "online-first@example.com")
	second := createUserFixture(t, testDB, 5002, "online-second", "online-second@example.com")
	createUserFixture(t, testDB, 5003, "offline", "offline@example.com")

	ctx := context.Background()
	for _, key := range []string{
		"im:ws:alive:5001:web",
		"im:ws:alive:5001:ios",
		"im:ws:alive:5002:web",
	} {
		if err := store.RDB.Set(ctx, key, "1", time.Minute).Err(); err != nil {
			t.Fatalf("seed online user key %s: %v", key, err)
		}
	}

	firstPage, err := ListUsers(UserListParams{OnlineOnly: true, Page: 1, PageSize: 1})
	if err != nil {
		t.Fatalf("ListUsers(online only, page 1) error = %v", err)
	}
	if firstPage.Total != 2 || len(firstPage.Items) != 1 {
		t.Fatalf("expected total=2 and one item, got total=%d items=%d", firstPage.Total, len(firstPage.Items))
	}

	secondPage, err := ListUsers(UserListParams{OnlineOnly: true, Page: 2, PageSize: 1})
	if err != nil {
		t.Fatalf("ListUsers(online only, page 2) error = %v", err)
	}
	if len(secondPage.Items) != 1 {
		t.Fatalf("expected one item on second page, got %d", len(secondPage.Items))
	}
	got := map[int64]bool{firstPage.Items[0].ID: true, secondPage.Items[0].ID: true}
	if !got[first.ID] || !got[second.ID] {
		t.Fatalf("expected online users %d and %d, got %#v", first.ID, second.ID, got)
	}
}

func createUserFixture(t *testing.T, db *testutil.TestDB, id int64, username, email string) *model.User {
	t.Helper()

	user := &model.User{
		ID:           id,
		Username:     username,
		Email:        email,
		PasswordHash: "hashed-password",
		Nickname:     username,
		Status:       model.UserStatusActive,
	}
	if err := db.DB.Create(user).Error; err != nil {
		t.Fatalf("create user fixture: %v", err)
	}
	return user
}

func lockUserGuard(t *testing.T, guard *security.LoginGuard) {
	t.Helper()

	ctx := context.Background()
	for i := 0; i < security.MaxLoginAttempts; i++ {
		if _, _, _, err := guard.RecordFailure(ctx); err != nil {
			t.Fatalf("RecordFailure() error = %v", err)
		}
	}

	locked, _, err := guard.IsLocked(ctx)
	if err != nil {
		t.Fatalf("IsLocked() error = %v", err)
	}
	if !locked {
		t.Fatal("expected guard to be locked")
	}
}

func assertGuardUnlocked(t *testing.T, guard *security.LoginGuard) {
	t.Helper()

	locked, _, err := guard.IsLocked(context.Background())
	if err != nil {
		t.Fatalf("IsLocked() error = %v", err)
	}
	if locked {
		t.Fatal("expected guard to be unlocked")
	}
}
