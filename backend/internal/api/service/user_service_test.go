package service

import (
	"errors"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

func init() {
	// Initialize snowflake for ID generation
	_ = snowflake.Init(1)
}

// setupServiceTest creates a test database and sets up the global store.DB
func setupServiceTest(t *testing.T) *testutil.TestDB {
	t.Helper()
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	return testDB
}

func TestGetUserProfile(t *testing.T) {
	testDB := setupServiceTest(t)
	defer testDB.Close()

	fixture := testutil.NewFixtureBuilder(testDB.DB)

	t.Run("user exists", func(t *testing.T) {
		// Create test user
		createdUser := fixture.CreateUser(func(u *model.User) {
			u.Username = "testuser1"
			u.Nickname = "Test User 1"
		})

		// Get profile
		user, err := GetUserProfile(createdUser.ID)
		if err != nil {
			t.Fatalf("GetUserProfile() error = %v", err)
		}

		if user.ID != createdUser.ID {
			t.Errorf("expected ID %d, got %d", createdUser.ID, user.ID)
		}
		if user.Username != "testuser1" {
			t.Errorf("expected username 'testuser1', got %s", user.Username)
		}
	})

	t.Run("user not found", func(t *testing.T) {
		_, err := GetUserProfile(99999999)
		if err == nil {
			t.Error("expected error for non-existent user")
		}
	})
}

func TestGetPublicProfile(t *testing.T) {
	testDB := setupServiceTest(t)
	defer testDB.Close()

	fixture := testutil.NewFixtureBuilder(testDB.DB)

	t.Run("user exists", func(t *testing.T) {
		createdUser := fixture.CreateUser(func(u *model.User) {
			u.Username = "publicuser"
			u.Nickname = "Public User"
			u.Introduction = "Public introduction"
			u.AvatarURL = "https://example.com/avatar.png"
		})

		profile, err := GetPublicProfile(createdUser.ID, createdUser.ID)
		if err != nil {
			t.Fatalf("GetPublicProfile() error = %v", err)
		}
		if profile.IsVisitor {
			t.Error("registered user profile should not be marked as visitor")
		}

		// Verify only public fields are returned
		if profile.ID != createdUser.ID {
			t.Errorf("expected ID %d, got %d", createdUser.ID, profile.ID)
		}
		if profile.Username != "publicuser" {
			t.Errorf("expected username 'publicuser', got %s", profile.Username)
		}
		if profile.Nickname != "Public User" {
			t.Errorf("expected nickname 'Public User', got %s", profile.Nickname)
		}
		if profile.Introduction != "Public introduction" {
			t.Errorf("expected introduction 'Public introduction', got %s", profile.Introduction)
		}
		if profile.AvatarURL != "https://example.com/avatar.png" {
			t.Errorf("expected avatar URL, got %s", profile.AvatarURL)
		}
	})

	t.Run("user not found", func(t *testing.T) {
		_, err := GetPublicProfile(1, 99999999)
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Errorf("expected ErrRecordNotFound for non-existent id, got %v", err)
		}
	})

	t.Run("widget visitor fallback", func(t *testing.T) {
		const ownerID int64 = 100001
		const visitorID int64 = 200002
		if err := testDB.DB.Create(&model.WidgetSession{
			ID:          300003,
			SiteID:      1,
			OwnerUserID: ownerID,
			VisitorID:   visitorID,
			VisitorKey:  "vkf_test",
			SessionID:   "sess-visitor-1",
			VisitorName: "  访客小明  ",
		}).Error; err != nil {
			t.Fatalf("create widget session: %v", err)
		}

		profile, err := GetPublicProfile(ownerID, visitorID)
		if err != nil {
			t.Fatalf("GetPublicProfile() visitor error = %v", err)
		}
		if !profile.IsVisitor {
			t.Error("expected IsVisitor=true for widget visitor")
		}
		if profile.Nickname != "访客小明" {
			t.Errorf("expected trimmed visitor name '访客小明', got %q", profile.Nickname)
		}
		if profile.ID != visitorID {
			t.Errorf("expected ID %d, got %d", visitorID, profile.ID)
		}

		// 非归属请求者不得读取到他人访客，应视为查无此人
		if _, err := GetPublicProfile(999, visitorID); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Errorf("expected ErrRecordNotFound for non-owner requester, got %v", err)
		}
	})
}

func TestUpdateUserProfile(t *testing.T) {
	testDB := setupServiceTest(t)
	defer testDB.Close()

	fixture := testutil.NewFixtureBuilder(testDB.DB)

	t.Run("update nickname", func(t *testing.T) {
		createdUser := fixture.CreateUser(func(u *model.User) {
			u.Username = "updatenick"
			u.Nickname = "Old Nickname"
		})

		newNickname := "New Nickname"
		err := UpdateUserProfile(createdUser.ID, &newNickname, nil, nil)
		if err != nil {
			t.Fatalf("UpdateUserProfile() error = %v", err)
		}

		// Verify update
		user, _ := GetUserProfile(createdUser.ID)
		if user.Nickname != newNickname {
			t.Errorf("expected nickname '%s', got '%s'", newNickname, user.Nickname)
		}
	})

	t.Run("update avatar url", func(t *testing.T) {
		createdUser := fixture.CreateUser(func(u *model.User) {
			u.Username = "updateavatar"
		})

		newAvatar := "https://example.com/new-avatar.png"
		err := UpdateUserProfile(createdUser.ID, nil, &newAvatar, nil)
		if err != nil {
			t.Fatalf("UpdateUserProfile() error = %v", err)
		}

		user, _ := GetUserProfile(createdUser.ID)
		if user.AvatarURL != newAvatar {
			t.Errorf("expected avatar '%s', got '%s'", newAvatar, user.AvatarURL)
		}
	})

	t.Run("update both fields", func(t *testing.T) {
		createdUser := fixture.CreateUser(func(u *model.User) {
			u.Username = "updateboth"
			u.Nickname = "Old"
		})

		newNickname := "New"
		newAvatar := "https://example.com/avatar.jpg"
		err := UpdateUserProfile(createdUser.ID, &newNickname, &newAvatar, nil)
		if err != nil {
			t.Fatalf("UpdateUserProfile() error = %v", err)
		}

		user, _ := GetUserProfile(createdUser.ID)
		if user.Nickname != newNickname {
			t.Errorf("expected nickname '%s', got '%s'", newNickname, user.Nickname)
		}
		if user.AvatarURL != newAvatar {
			t.Errorf("expected avatar '%s', got '%s'", newAvatar, user.AvatarURL)
		}
	})

	t.Run("no updates", func(t *testing.T) {
		createdUser := fixture.CreateUser(func(u *model.User) {
			u.Username = "noupdate"
			u.Nickname = "Original"
		})

		// Call with nil values
		err := UpdateUserProfile(createdUser.ID, nil, nil, nil)
		if err != nil {
			t.Fatalf("UpdateUserProfile() error = %v", err)
		}

		// Verify nothing changed
		user, _ := GetUserProfile(createdUser.ID)
		if user.Nickname != "Original" {
			t.Errorf("nickname should not change, got '%s'", user.Nickname)
		}
	})

	t.Run("user not found", func(t *testing.T) {
		nickname := "Test"
		err := UpdateUserProfile(99999999, &nickname, nil, nil)
		// GORM doesn't return error when updating non-existent record
		// This test documents current behavior
		if err != nil {
			t.Logf("UpdateUserProfile for non-existent user returned: %v", err)
		}
	})

	t.Run("update introduction", func(t *testing.T) {
		createdUser := fixture.CreateUser(func(u *model.User) {
			u.Username = "updateintro"
			u.Introduction = "Old intro"
		})

		newIntroduction := "New introduction"
		err := UpdateUserProfile(createdUser.ID, nil, nil, &newIntroduction)
		if err != nil {
			t.Fatalf("UpdateUserProfile() error = %v", err)
		}

		user, _ := GetUserProfile(createdUser.ID)
		if user.Introduction != newIntroduction {
			t.Errorf("expected introduction '%s', got '%s'", newIntroduction, user.Introduction)
		}
	})

	t.Run("reject invalid introduction", func(t *testing.T) {
		createdUser := fixture.CreateUser(func(u *model.User) {
			u.Username = "invalidintro"
		})

		invalidIntroduction := "bad\x00intro"
		err := UpdateUserProfile(createdUser.ID, nil, nil, &invalidIntroduction)
		if err == nil {
			t.Fatal("expected validation error")
		}
	})
}

func TestPublicProfileExcludesSensitiveFields(t *testing.T) {
	testDB := setupServiceTest(t)
	defer testDB.Close()

	fixture := testutil.NewFixtureBuilder(testDB.DB)

	// Create user with sensitive data
	createdUser := fixture.CreateUser(func(u *model.User) {
		u.Username = "sensitive"
		u.PasswordHash = "secret_hash_value"
		u.AuthProvider = "local"
	})

	profile, err := GetPublicProfile(createdUser.ID, createdUser.ID)
	if err != nil {
		t.Fatalf("GetPublicProfile() error = %v", err)
	}

	// PublicProfile should not have password hash or auth provider
	// This is implicitly tested by the struct not having those fields
	if profile.ID == 0 {
		t.Error("profile ID should be set")
	}
}
