package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func setupAvatarStorageMigrationTest(t *testing.T) (*testutil.TestDB, *testutil.FixtureBuilder, func()) {
	t.Helper()

	testDB := testutil.NewTestDB()
	fixture := testutil.NewFixtureBuilder(testDB.DB)

	originalDB := store.DB
	originalConfig := config.C
	originalEnsure := avatarStorageEnsureOSSReady
	originalCopy := avatarStorageCopyObject
	originalRemove := avatarStorageRemoveObject

	store.DB = testDB.DB
	config.C.OSS.Avatar.Bucket = "aibot-avatar"
	config.C.OSS.Avatar.PublicURL = "https://cdn.example.com/avatar"
	config.C.OSS.Avatar.StorageDir = "prod"
	avatarStorageEnsureOSSReady = func() error { return nil }

	cleanup := func() {
		store.DB = originalDB
		config.C = originalConfig
		avatarStorageEnsureOSSReady = originalEnsure
		avatarStorageCopyObject = originalCopy
		avatarStorageRemoveObject = originalRemove
		testDB.Close()
	}

	return testDB, fixture, cleanup
}

func TestRunAvatarStorageMigration_MigratesLegacyUserAndAgentAvatars(t *testing.T) {
	testDB, fixture, cleanup := setupAvatarStorageMigrationTest(t)
	defer cleanup()

	user := fixture.CreateUser(func(u *model.User) {
		u.ID = 1001
		u.Username = "avatar_user"
		u.AvatarURL = "https://legacy.example.com/aibot-avatar/prod/user/1001/avatar/old-user.jpg"
	})
	agent := &model.Agent{
		ID:        2001,
		AgentName: "avatar_agent",
		OwnerID:   user.ID,
		AvatarURL: "https://legacy.example.com/aibot-avatar/prod/agent/1001/2001/avatar/old-agent.jpg",
		Status:    model.AgentStatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := testDB.DB.Create(agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	var copied [][2]string
	var removed []string
	avatarStorageCopyObject = func(_ context.Context, bucket, sourceObjectKey, targetObjectKey string) error {
		if bucket != "aibot-avatar" {
			t.Fatalf("expected bucket aibot-avatar, got %q", bucket)
		}
		copied = append(copied, [2]string{sourceObjectKey, targetObjectKey})
		return nil
	}
	avatarStorageRemoveObject = func(_ context.Context, bucket, objectKey string) error {
		if bucket != "aibot-avatar" {
			t.Fatalf("expected bucket aibot-avatar, got %q", bucket)
		}
		removed = append(removed, objectKey)
		return nil
	}

	if err := RunAvatarStorageMigration(context.Background()); err != nil {
		t.Fatalf("RunAvatarStorageMigration() error = %v", err)
	}

	var migratedUser model.User
	if err := testDB.DB.First(&migratedUser, user.ID).Error; err != nil {
		t.Fatalf("reload user failed: %v", err)
	}
	if got, want := migratedUser.AvatarURL, "https://cdn.example.com/avatar/prod/avatars/1001.jpg"; got != want {
		t.Fatalf("expected user avatar_url %q, got %q", want, got)
	}

	var migratedAgent model.Agent
	if err := testDB.DB.First(&migratedAgent, agent.ID).Error; err != nil {
		t.Fatalf("reload agent failed: %v", err)
	}
	if got, want := migratedAgent.AvatarURL, "https://cdn.example.com/avatar/prod/avatars/agent_2001.jpg"; got != want {
		t.Fatalf("expected agent avatar_url %q, got %q", want, got)
	}

	if len(copied) != 2 {
		t.Fatalf("expected 2 copy operations, got %d", len(copied))
	}
	if copied[0] != [2]string{"prod/user/1001/avatar/old-user.jpg", "prod/avatars/1001.jpg"} {
		t.Fatalf("unexpected first copy operation: %#v", copied[0])
	}
	if copied[1] != [2]string{"prod/agent/1001/2001/avatar/old-agent.jpg", "prod/avatars/agent_2001.jpg"} {
		t.Fatalf("unexpected second copy operation: %#v", copied[1])
	}

	if len(removed) != 2 {
		t.Fatalf("expected 2 remove operations, got %d", len(removed))
	}
	if removed[0] != "prod/user/1001/avatar/old-user.jpg" {
		t.Fatalf("unexpected first removed object: %q", removed[0])
	}
	if removed[1] != "prod/agent/1001/2001/avatar/old-agent.jpg" {
		t.Fatalf("unexpected second removed object: %q", removed[1])
	}

	avatarStorageCopyObject = func(_ context.Context, _, _, _ string) error {
		t.Fatal("copy should not run after migration has been marked applied")
		return nil
	}
	avatarStorageRemoveObject = func(_ context.Context, _, _ string) error {
		t.Fatal("remove should not run after migration has been marked applied")
		return nil
	}

	if err := RunAvatarStorageMigration(context.Background()); err != nil {
		t.Fatalf("second RunAvatarStorageMigration() error = %v", err)
	}
}

func TestRunAvatarStorageMigration_SkipsExternalAvatarURLs(t *testing.T) {
	testDB, fixture, cleanup := setupAvatarStorageMigrationTest(t)
	defer cleanup()

	user := fixture.CreateUser(func(u *model.User) {
		u.ID = 3001
		u.Username = "external_avatar_user"
		u.AvatarURL = "https://images.example.com/avatar/3001.png"
	})

	copyCalled := false
	removeCalled := false
	avatarStorageCopyObject = func(_ context.Context, _, _, _ string) error {
		copyCalled = true
		return nil
	}
	avatarStorageRemoveObject = func(_ context.Context, _, _ string) error {
		removeCalled = true
		return nil
	}

	if err := RunAvatarStorageMigration(context.Background()); err != nil {
		t.Fatalf("RunAvatarStorageMigration() error = %v", err)
	}

	var migratedUser model.User
	if err := testDB.DB.First(&migratedUser, user.ID).Error; err != nil {
		t.Fatalf("reload user failed: %v", err)
	}
	if migratedUser.AvatarURL != "https://images.example.com/avatar/3001.png" {
		t.Fatalf("expected external avatar URL unchanged, got %q", migratedUser.AvatarURL)
	}
	if copyCalled {
		t.Fatal("expected external avatar URL to skip copy")
	}
	if removeCalled {
		t.Fatal("expected external avatar URL to skip remove")
	}
}

func TestRunAvatarStorageMigration_RetriesLegacyCleanupAfterDeleteFailure(t *testing.T) {
	testDB, fixture, cleanup := setupAvatarStorageMigrationTest(t)
	defer cleanup()

	user := fixture.CreateUser(func(u *model.User) {
		u.ID = 7001
		u.Username = "cleanup_retry_user"
		u.AvatarURL = "https://legacy.example.com/aibot-avatar/prod/user/7001/avatar/old-user.jpg"
	})

	removeAttempts := 0
	avatarStorageCopyObject = func(_ context.Context, _, sourceObjectKey, targetObjectKey string) error {
		if sourceObjectKey != "prod/user/7001/avatar/old-user.jpg" {
			t.Fatalf("unexpected source object key: %q", sourceObjectKey)
		}
		if targetObjectKey != "prod/avatars/7001.jpg" {
			t.Fatalf("unexpected target object key: %q", targetObjectKey)
		}
		return nil
	}
	avatarStorageRemoveObject = func(_ context.Context, _, objectKey string) error {
		removeAttempts++
		if objectKey != "prod/user/7001/avatar/old-user.jpg" {
			t.Fatalf("unexpected removed object key: %q", objectKey)
		}
		if removeAttempts == 1 {
			return errors.New("temporary delete failure")
		}
		return nil
	}

	if err := RunAvatarStorageMigration(context.Background()); err == nil {
		t.Fatal("expected first migration run to fail on delete error")
	}

	var migratedUser model.User
	if err := testDB.DB.First(&migratedUser, user.ID).Error; err != nil {
		t.Fatalf("reload user failed: %v", err)
	}
	if got, want := migratedUser.AvatarURL, "https://cdn.example.com/avatar/prod/avatars/7001.jpg"; got != want {
		t.Fatalf("expected migrated avatar_url %q, got %q", want, got)
	}

	var cleanupCount int64
	if err := testDB.DB.Table("avatar_cleanup_tasks").Count(&cleanupCount).Error; err != nil {
		t.Fatalf("count cleanup tasks failed: %v", err)
	}
	if cleanupCount != 1 {
		t.Fatalf("expected 1 cleanup task, got %d", cleanupCount)
	}

	if err := RunAvatarStorageMigration(context.Background()); err != nil {
		t.Fatalf("second RunAvatarStorageMigration() error = %v", err)
	}
	if removeAttempts != 2 {
		t.Fatalf("expected cleanup delete retried once, got %d attempts", removeAttempts)
	}
	if err := testDB.DB.Table("avatar_cleanup_tasks").Count(&cleanupCount).Error; err != nil {
		t.Fatalf("count cleanup tasks failed: %v", err)
	}
	if cleanupCount != 0 {
		t.Fatalf("expected cleanup tasks cleared, got %d", cleanupCount)
	}
}
