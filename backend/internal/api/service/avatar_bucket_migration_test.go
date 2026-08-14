package service

import (
	"context"
	"testing"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/minio/minio-go/v7"
)

func setupAvatarBucketMigrationTest(t *testing.T) (*testutil.TestDB, *testutil.FixtureBuilder, func()) {
	t.Helper()

	testDB := testutil.NewTestDB()
	fixture := testutil.NewFixtureBuilder(testDB.DB)

	originalDB := store.DB
	originalConfig := config.C
	originalCopy := avatarBucketCopyObject
	originalAvatarClient := getOSSClient(ossStorageAvatar)

	store.DB = testDB.DB
	config.C.OSS.Avatar.Endpoint = "avatar-oss.example.com"
	config.C.OSS.Avatar.AccessKey = "avatar-ak"
	config.C.OSS.Avatar.SecretKey = "avatar-sk"
	config.C.OSS.Avatar.Bucket = "avatar-bucket"
	config.C.OSS.Avatar.Region = "ap-shanghai"
	config.C.OSS.Avatar.UseSSL = true
	config.C.OSS.Avatar.PublicURL = "https://avatar.example.com"
	config.C.OSS.Avatar.StorageDir = "aibot/avatar"
	config.C.Migration.LegacyOSS.Endpoint = "legacy-oss.example.com"
	config.C.Migration.LegacyOSS.AccessKey = "legacy-ak"
	config.C.Migration.LegacyOSS.SecretKey = "legacy-sk"
	config.C.Migration.LegacyOSS.Bucket = "shared-bucket"
	config.C.Migration.LegacyOSS.Region = "ap-shanghai"
	config.C.Migration.LegacyOSS.UseSSL = true
	config.C.Migration.LegacyOSS.PublicURL = "https://shared.example.com"
	config.C.Migration.LegacyOSS.StorageDir = "aibot"
	setOSSClient(ossStorageAvatar, &minio.Client{})

	cleanup := func() {
		store.DB = originalDB
		config.C = originalConfig
		avatarBucketCopyObject = originalCopy
		setOSSClient(ossStorageAvatar, originalAvatarClient)
		testDB.Close()
	}
	return testDB, fixture, cleanup
}

func TestRunAvatarBucketMigration_CopiesAcrossBucketsAndUpdatesURLs(t *testing.T) {
	testDB, fixture, cleanup := setupAvatarBucketMigrationTest(t)
	defer cleanup()

	user := fixture.CreateUser(func(u *model.User) {
		u.ID = 8101
		u.Username = "avatar_bucket_user"
		u.AvatarURL = "https://shared.example.com/aibot/user/8101/avatar/original.jpg"
	})
	agent := &model.Agent{
		ID:        8201,
		AgentName: "avatar_bucket_agent",
		OwnerID:   user.ID,
		AvatarURL: "https://shared.example.com/aibot/agent/8101/8201/avatar/original.jpg",
		Status:    model.AgentStatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := testDB.DB.Create(agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	var copied [][4]string
	avatarBucketCopyObject = func(
		_ context.Context,
		_ *minio.Client,
		_ *minio.Client,
		sourceBucket string,
		sourceObjectKey string,
		targetBucket string,
		targetObjectKey string,
	) error {
		copied = append(copied, [4]string{
			sourceBucket,
			sourceObjectKey,
			targetBucket,
			targetObjectKey,
		})
		return nil
	}

	if err := RunAvatarBucketMigration(context.Background()); err != nil {
		t.Fatalf("RunAvatarBucketMigration() error = %v", err)
	}

	var migratedUser model.User
	if err := testDB.DB.First(&migratedUser, user.ID).Error; err != nil {
		t.Fatalf("reload user failed: %v", err)
	}
	if got, want := migratedUser.AvatarURL, "https://avatar.example.com/aibot/avatar/avatars/8101.jpg"; got != want {
		t.Fatalf("expected user avatar_url %q, got %q", want, got)
	}

	var migratedAgent model.Agent
	if err := testDB.DB.First(&migratedAgent, agent.ID).Error; err != nil {
		t.Fatalf("reload agent failed: %v", err)
	}
	if got, want := migratedAgent.AvatarURL, "https://avatar.example.com/aibot/avatar/avatars/agent_8201.jpg"; got != want {
		t.Fatalf("expected agent avatar_url %q, got %q", want, got)
	}

	if len(copied) != 2 {
		t.Fatalf("expected 2 cross-bucket copy operations, got %d", len(copied))
	}
	if copied[0] != [4]string{"shared-bucket", "aibot/user/8101/avatar/original.jpg", "avatar-bucket", "aibot/avatar/avatars/8101.jpg"} {
		t.Fatalf("unexpected first copy: %#v", copied[0])
	}
	if copied[1] != [4]string{"shared-bucket", "aibot/agent/8101/8201/avatar/original.jpg", "avatar-bucket", "aibot/avatar/avatars/agent_8201.jpg"} {
		t.Fatalf("unexpected second copy: %#v", copied[1])
	}
}
