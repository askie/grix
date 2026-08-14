package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/minio/minio-go/v7"
	"gorm.io/gorm"
)

const avatarBucketMigrationName = "20260320_avatar_dedicated_oss"

var avatarBucketCopyObject = func(
	ctx context.Context,
	sourceClient *minio.Client,
	targetClient *minio.Client,
	sourceBucket string,
	sourceObjectKey string,
	targetBucket string,
	targetObjectKey string,
) error {
	sourceObject, err := sourceClient.GetObject(
		ctx,
		sourceBucket,
		sourceObjectKey,
		minio.GetObjectOptions{},
	)
	if err != nil {
		return err
	}
	defer sourceObject.Close()

	info, err := sourceObject.Stat()
	if err != nil {
		return err
	}

	_, err = targetClient.PutObject(
		ctx,
		targetBucket,
		targetObjectKey,
		sourceObject,
		info.Size,
		minio.PutObjectOptions{ContentType: info.ContentType},
	)
	return err
}

type avatarBucketMigrationStats struct {
	migrated   int
	normalized int
	skipped    int
}

func RunAvatarBucketMigration(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if store.DB == nil {
		return fmt.Errorf("db is nil")
	}

	sourceCfg, ok := getLegacyMigrationOSSConfig()
	if !ok {
		logAvatarBucketMigrationf("avatar bucket migration skipped: migration.legacy_oss not configured")
		return nil
	}
	if err := validateOSSRuntimeConfig(sourceCfg); err != nil {
		return fmt.Errorf("invalid migration legacy oss config: %w", err)
	}
	if err := ensureAvatarOSSReady(); err != nil {
		return err
	}
	targetCfg := config.C.OSS.Avatar
	if sameOSSStorageLocation(sourceCfg, targetCfg) {
		logAvatarBucketMigrationf("avatar bucket migration skipped: source and target oss are identical")
		return nil
	}

	if err := ensureDataMigrationTable(ctx, store.DB); err != nil {
		return err
	}
	applied, err := isDataMigrationApplied(ctx, store.DB, avatarBucketMigrationName)
	if err != nil {
		return err
	}
	if applied {
		logAvatarBucketMigrationf(
			"avatar bucket migration already applied: %s",
			avatarBucketMigrationName,
		)
		return nil
	}

	sourceClient, err := newOSSClient(sourceCfg)
	if err != nil {
		return err
	}

	userStats, err := migrateUserAvatarBucket(ctx, store.DB, sourceCfg, targetCfg, sourceClient)
	if err != nil {
		return err
	}
	agentStats, err := migrateAgentAvatarBucket(ctx, store.DB, sourceCfg, targetCfg, sourceClient)
	if err != nil {
		return err
	}

	if err := markDataMigrationApplied(ctx, store.DB, avatarBucketMigrationName); err != nil {
		return err
	}

	logAvatarBucketMigrationf(
		"avatar bucket migration applied: users_migrated=%d users_normalized=%d users_skipped=%d agents_migrated=%d agents_normalized=%d agents_skipped=%d",
		userStats.migrated,
		userStats.normalized,
		userStats.skipped,
		agentStats.migrated,
		agentStats.normalized,
		agentStats.skipped,
	)
	return nil
}

func migrateUserAvatarBucket(
	ctx context.Context,
	db *gorm.DB,
	sourceCfg config.OSSConfig,
	targetCfg config.OSSConfig,
	sourceClient *minio.Client,
) (avatarBucketMigrationStats, error) {
	stats := avatarBucketMigrationStats{}
	batch := make([]userAvatarMigrationRecord, 0, 100)
	err := db.WithContext(ctx).
		Model(&model.User{}).
		Select("id", "avatar_url").
		Where("avatar_url <> ''").
		FindInBatches(&batch, 100, func(_ *gorm.DB, _ int) error {
			for _, record := range batch {
				result, err := migrateUserAvatarBucketRecord(ctx, db, sourceCfg, targetCfg, sourceClient, record)
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

func migrateAgentAvatarBucket(
	ctx context.Context,
	db *gorm.DB,
	sourceCfg config.OSSConfig,
	targetCfg config.OSSConfig,
	sourceClient *minio.Client,
) (avatarBucketMigrationStats, error) {
	stats := avatarBucketMigrationStats{}
	batch := make([]agentAvatarMigrationRecord, 0, 100)
	err := db.WithContext(ctx).
		Model(&model.Agent{}).
		Select("id", "owner_id", "avatar_url").
		Where("avatar_url <> ''").
		FindInBatches(&batch, 100, func(_ *gorm.DB, _ int) error {
			for _, record := range batch {
				result, err := migrateAgentAvatarBucketRecord(ctx, db, sourceCfg, targetCfg, sourceClient, record)
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

func migrateUserAvatarBucketRecord(
	ctx context.Context,
	db *gorm.DB,
	sourceCfg config.OSSConfig,
	targetCfg config.OSSConfig,
	sourceClient *minio.Client,
	record userAvatarMigrationRecord,
) (avatarBucketMigrationStats, error) {
	sourceURL := strings.TrimSpace(record.AvatarURL)
	if sourceURL == "" {
		return avatarBucketMigrationStats{}, nil
	}

	sourceObjectKey := resolveObjectKeyFromURLWithConfig(sourceURL, sourceCfg)
	if sourceObjectKey == "" || !isUserAvatarObjectKeyForConfig(sourceCfg, record.ID, sourceObjectKey) {
		return avatarBucketMigrationStats{skipped: 1}, nil
	}

	targetObjectKey := buildUserAvatarObjectKeyForConfig(targetCfg, record.ID)
	targetURL := buildOSSAccessURL(ossStorageAvatar, targetObjectKey)
	if strings.TrimSpace(sourceURL) == targetURL {
		return avatarBucketMigrationStats{}, nil
	}

	if err := avatarBucketCopyObject(
		ctx,
		sourceClient,
		getOSSClient(ossStorageAvatar),
		sourceCfg.Bucket,
		sourceObjectKey,
		targetCfg.Bucket,
		targetObjectKey,
	); err != nil {
		return avatarBucketMigrationStats{}, fmt.Errorf(
			"copy user avatar object %s -> %s failed: %w",
			sourceObjectKey,
			targetObjectKey,
			err,
		)
	}

	if err := db.WithContext(ctx).
		Model(&model.User{}).
		Where("id = ?", record.ID).
		Updates(map[string]any{
			"avatar_url": targetURL,
			"updated_at": nowTime(),
		}).Error; err != nil {
		return avatarBucketMigrationStats{}, err
	}
	return avatarBucketMigrationStats{migrated: 1}, nil
}

func migrateAgentAvatarBucketRecord(
	ctx context.Context,
	db *gorm.DB,
	sourceCfg config.OSSConfig,
	targetCfg config.OSSConfig,
	sourceClient *minio.Client,
	record agentAvatarMigrationRecord,
) (avatarBucketMigrationStats, error) {
	sourceURL := strings.TrimSpace(record.AvatarURL)
	if sourceURL == "" {
		return avatarBucketMigrationStats{}, nil
	}

	sourceObjectKey := resolveObjectKeyFromURLWithConfig(sourceURL, sourceCfg)
	if sourceObjectKey == "" || !isAgentAvatarObjectKeyForConfig(sourceCfg, record.OwnerID, record.ID, sourceObjectKey) {
		return avatarBucketMigrationStats{skipped: 1}, nil
	}

	targetObjectKey := buildAgentAvatarObjectKeyForConfig(targetCfg, record.ID)
	targetURL := buildOSSAccessURL(ossStorageAvatar, targetObjectKey)
	if strings.TrimSpace(sourceURL) == targetURL {
		return avatarBucketMigrationStats{}, nil
	}

	if err := avatarBucketCopyObject(
		ctx,
		sourceClient,
		getOSSClient(ossStorageAvatar),
		sourceCfg.Bucket,
		sourceObjectKey,
		targetCfg.Bucket,
		targetObjectKey,
	); err != nil {
		return avatarBucketMigrationStats{}, fmt.Errorf(
			"copy agent avatar object %s -> %s failed: %w",
			sourceObjectKey,
			targetObjectKey,
			err,
		)
	}

	if err := db.WithContext(ctx).
		Model(&model.Agent{}).
		Where("id = ?", record.ID).
		Updates(map[string]any{
			"avatar_url": targetURL,
			"updated_at": nowTime(),
		}).Error; err != nil {
		return avatarBucketMigrationStats{}, err
	}
	return avatarBucketMigrationStats{migrated: 1}, nil
}

func logAvatarBucketMigrationf(format string, args ...any) {
	if logger.L != nil {
		logger.L.Infof(format, args...)
	}
}
