package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/minio/minio-go/v7"
	"gorm.io/gorm"
)

const avatarStorageMigrationName = "20260319_avatar_storage_layout"

var (
	avatarStorageEnsureOSSReady = ensureAvatarOSSReady
	avatarStorageCopyObject     = func(ctx context.Context, bucket, sourceObjectKey, targetObjectKey string) error {
		_, err := getOSSClient(ossStorageAvatar).CopyObject(
			ctx,
			minio.CopyDestOptions{
				Bucket: bucket,
				Object: targetObjectKey,
			},
			minio.CopySrcOptions{
				Bucket: bucket,
				Object: sourceObjectKey,
			},
		)
		return err
	}
	avatarStorageRemoveObject = func(ctx context.Context, bucket, objectKey string) error {
		return getOSSClient(ossStorageAvatar).RemoveObject(
			ctx,
			bucket,
			objectKey,
			minio.RemoveObjectOptions{},
		)
	}
)

type avatarMigrationStats struct {
	migrated   int
	normalized int
	skipped    int
}

type userAvatarMigrationRecord struct {
	ID        int64  `gorm:"column:id"`
	AvatarURL string `gorm:"column:avatar_url"`
}

type agentAvatarMigrationRecord struct {
	ID        int64  `gorm:"column:id"`
	OwnerID   int64  `gorm:"column:owner_id"`
	AvatarURL string `gorm:"column:avatar_url"`
}

type avatarCleanupTask struct {
	ObjectKey string `gorm:"column:object_key"`
}

func RunAvatarStorageMigration(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if store.DB == nil {
		return fmt.Errorf("db is nil")
	}
	if err := avatarStorageEnsureOSSReady(); err != nil {
		return err
	}
	if err := ensureDataMigrationTable(ctx, store.DB); err != nil {
		return err
	}
	if err := ensureAvatarCleanupTaskTable(ctx, store.DB); err != nil {
		return err
	}
	if err := processPendingAvatarCleanupTasks(ctx, store.DB); err != nil {
		return err
	}

	applied, err := isDataMigrationApplied(ctx, store.DB, avatarStorageMigrationName)
	if err != nil {
		return err
	}
	if applied {
		logAvatarStorageMigrationf(
			"avatar storage migration already applied: %s",
			avatarStorageMigrationName,
		)
		return nil
	}

	userStats, err := migrateUserAvatarStorage(ctx, store.DB)
	if err != nil {
		return err
	}
	agentStats, err := migrateAgentAvatarStorage(ctx, store.DB)
	if err != nil {
		return err
	}

	if err := markDataMigrationApplied(ctx, store.DB, avatarStorageMigrationName); err != nil {
		return err
	}
	if err := processPendingAvatarCleanupTasks(ctx, store.DB); err != nil {
		return err
	}

	logAvatarStorageMigrationf(
		"avatar storage migration applied: users_migrated=%d users_normalized=%d users_skipped=%d agents_migrated=%d agents_normalized=%d agents_skipped=%d",
		userStats.migrated,
		userStats.normalized,
		userStats.skipped,
		agentStats.migrated,
		agentStats.normalized,
		agentStats.skipped,
	)
	return nil
}

func migrateUserAvatarStorage(ctx context.Context, db *gorm.DB) (avatarMigrationStats, error) {
	stats := avatarMigrationStats{}
	batch := make([]userAvatarMigrationRecord, 0, 100)
	err := db.WithContext(ctx).
		Model(&model.User{}).
		Select("id", "avatar_url").
		Where("avatar_url <> ''").
		FindInBatches(&batch, 100, func(_ *gorm.DB, _ int) error {
			for _, record := range batch {
				result, err := migrateUserAvatarRecord(ctx, db, record)
				if err != nil {
					return err
				}
				stats.migrated += result.migrated
				stats.normalized += result.normalized
				stats.skipped += result.skipped
			}
			return nil
		}).Error
	return stats, err
}

func migrateAgentAvatarStorage(ctx context.Context, db *gorm.DB) (avatarMigrationStats, error) {
	stats := avatarMigrationStats{}
	batch := make([]agentAvatarMigrationRecord, 0, 100)
	err := db.WithContext(ctx).
		Model(&model.Agent{}).
		Select("id", "owner_id", "avatar_url").
		Where("avatar_url <> ''").
		FindInBatches(&batch, 100, func(_ *gorm.DB, _ int) error {
			for _, record := range batch {
				result, err := migrateAgentAvatarRecord(ctx, db, record)
				if err != nil {
					return err
				}
				stats.migrated += result.migrated
				stats.normalized += result.normalized
				stats.skipped += result.skipped
			}
			return nil
		}).Error
	return stats, err
}

func migrateUserAvatarRecord(
	ctx context.Context,
	db *gorm.DB,
	record userAvatarMigrationRecord,
) (avatarMigrationStats, error) {
	sourceURL := strings.TrimSpace(record.AvatarURL)
	if sourceURL == "" {
		return avatarMigrationStats{}, nil
	}

	targetObjectKey := buildUserAvatarObjectKey(record.ID)
	targetURL := buildAvatarAccessURL(targetObjectKey)
	return migrateAvatarRecord(
		ctx,
		db,
		sourceURL,
		targetObjectKey,
		targetURL,
		func(objectKey string) bool {
			return isUserAvatarObjectKey(record.ID, objectKey)
		},
		func(tx *gorm.DB, now time.Time) error {
			return tx.WithContext(ctx).
				Model(&model.User{}).
				Where("id = ?", record.ID).
				Updates(map[string]any{
					"avatar_url": targetURL,
					"updated_at": now,
				}).Error
		},
	)
}

func migrateAgentAvatarRecord(
	ctx context.Context,
	db *gorm.DB,
	record agentAvatarMigrationRecord,
) (avatarMigrationStats, error) {
	sourceURL := strings.TrimSpace(record.AvatarURL)
	if sourceURL == "" {
		return avatarMigrationStats{}, nil
	}

	targetObjectKey := buildAgentAvatarObjectKey(record.ID)
	targetURL := buildAvatarAccessURL(targetObjectKey)
	return migrateAvatarRecord(
		ctx,
		db,
		sourceURL,
		targetObjectKey,
		targetURL,
		func(objectKey string) bool {
			return isAgentAvatarObjectKey(record.OwnerID, record.ID, objectKey)
		},
		func(tx *gorm.DB, now time.Time) error {
			return tx.WithContext(ctx).
				Model(&model.Agent{}).
				Where("id = ?", record.ID).
				Updates(map[string]any{
					"avatar_url": targetURL,
					"updated_at": now,
				}).Error
		},
	)
}

func migrateAvatarRecord(
	ctx context.Context,
	db *gorm.DB,
	sourceURL, targetObjectKey, targetURL string,
	isManagedObjectKey func(string) bool,
	updateAvatarURL func(*gorm.DB, time.Time) error,
) (avatarMigrationStats, error) {
	sourceObjectKey := resolveAvatarObjectKey(sourceURL)
	if sourceObjectKey == "" || !isManagedObjectKey(sourceObjectKey) {
		return avatarMigrationStats{skipped: 1}, nil
	}

	if sourceObjectKey != targetObjectKey {
		if err := avatarStorageCopyObject(
			ctx,
			config.C.OSS.Avatar.Bucket,
			sourceObjectKey,
			targetObjectKey,
		); err != nil {
			return avatarMigrationStats{}, fmt.Errorf(
				"copy avatar object %s -> %s failed: %w",
				sourceObjectKey,
				targetObjectKey,
				err,
			)
		}
		if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := upsertAvatarCleanupTask(ctx, tx, sourceObjectKey); err != nil {
				return err
			}
			return updateAvatarURL(tx, nowTime())
		}); err != nil {
			return avatarMigrationStats{}, err
		}
		if err := avatarStorageRemoveObject(ctx, config.C.OSS.Avatar.Bucket, sourceObjectKey); err != nil {
			return avatarMigrationStats{}, fmt.Errorf(
				"remove avatar object %s failed: %w",
				sourceObjectKey,
				err,
			)
		}
		if err := deleteAvatarCleanupTask(ctx, db, sourceObjectKey); err != nil {
			return avatarMigrationStats{}, err
		}
		return avatarMigrationStats{migrated: 1}, nil
	}

	if strings.TrimSpace(sourceURL) != targetURL {
		if err := updateAvatarURL(db, nowTime()); err != nil {
			return avatarMigrationStats{}, err
		}
	}

	if strings.TrimSpace(sourceURL) != targetURL {
		return avatarMigrationStats{normalized: 1}, nil
	}
	return avatarMigrationStats{}, nil
}

func ensureDataMigrationTable(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Exec(`
CREATE TABLE IF NOT EXISTS data_migrations (
	name VARCHAR(255) PRIMARY KEY,
	applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
)`).Error
}

func ensureAvatarCleanupTaskTable(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Exec(`
CREATE TABLE IF NOT EXISTS avatar_cleanup_tasks (
	object_key VARCHAR(1024) PRIMARY KEY,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
)`).Error
}

func processPendingAvatarCleanupTasks(ctx context.Context, db *gorm.DB) error {
	var tasks []avatarCleanupTask
	if err := db.WithContext(ctx).
		Table("avatar_cleanup_tasks").
		Select("object_key").
		Order("created_at ASC").
		Find(&tasks).Error; err != nil {
		return err
	}
	for _, task := range tasks {
		if err := avatarStorageRemoveObject(ctx, config.C.OSS.Avatar.Bucket, task.ObjectKey); err != nil {
			return fmt.Errorf("remove avatar cleanup task object %s failed: %w", task.ObjectKey, err)
		}
		if err := deleteAvatarCleanupTask(ctx, db, task.ObjectKey); err != nil {
			return err
		}
	}
	return nil
}

func upsertAvatarCleanupTask(ctx context.Context, db *gorm.DB, objectKey string) error {
	return db.WithContext(ctx).Exec(
		`INSERT INTO avatar_cleanup_tasks(object_key) VALUES (?) ON CONFLICT(object_key) DO NOTHING`,
		objectKey,
	).Error
}

func deleteAvatarCleanupTask(ctx context.Context, db *gorm.DB, objectKey string) error {
	return db.WithContext(ctx).
		Exec(`DELETE FROM avatar_cleanup_tasks WHERE object_key = ?`, objectKey).
		Error
}

func nowTime() time.Time {
	return time.Now()
}

func isDataMigrationApplied(ctx context.Context, db *gorm.DB, name string) (bool, error) {
	var count int64
	if err := db.WithContext(ctx).
		Table("data_migrations").
		Where("name = ?", name).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func markDataMigrationApplied(ctx context.Context, db *gorm.DB, name string) error {
	return db.WithContext(ctx).
		Exec(
			`INSERT INTO data_migrations(name, applied_at) VALUES (?, ?)`,
			name,
			time.Now(),
		).Error
}

func logAvatarStorageMigrationf(format string, args ...any) {
	if logger.L != nil {
		logger.L.Infof(format, args...)
		return
	}
}
