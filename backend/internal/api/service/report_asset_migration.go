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

const reportAssetMigrationName = "20260320_report_dedicated_oss"

var reportAssetMigrationCopyObject = func(
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

type reportAssetMigrationRecord struct {
	ObjectKey string `gorm:"column:object_key"`
}

type reportAssetMigrationStats struct {
	copied  int
	skipped int
}

func RunReportAssetMigration(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if store.DB == nil {
		return fmt.Errorf("db is nil")
	}

	sourceCfg, ok := getLegacyMigrationOSSConfig()
	if !ok {
		logReportAssetMigrationf("report asset migration skipped: migration.legacy_oss not configured")
		return nil
	}
	if err := validateOSSRuntimeConfig(sourceCfg); err != nil {
		return fmt.Errorf("invalid migration legacy oss config: %w", err)
	}
	if err := ensureReportOSSReady(); err != nil {
		return err
	}

	targetCfg := config.C.OSS.Report
	if sameOSSStorageLocation(sourceCfg, targetCfg) {
		logReportAssetMigrationf("report asset migration skipped: source and target oss are identical")
		return nil
	}

	if err := ensureDataMigrationTable(ctx, store.DB); err != nil {
		return err
	}
	applied, err := isDataMigrationApplied(ctx, store.DB, reportAssetMigrationName)
	if err != nil {
		return err
	}
	if applied {
		logReportAssetMigrationf(
			"report asset migration already applied: %s",
			reportAssetMigrationName,
		)
		return nil
	}

	sourceClient, err := newOSSClient(sourceCfg)
	if err != nil {
		return err
	}

	stats, err := migrateReportAssets(ctx, store.DB, sourceCfg, targetCfg, sourceClient)
	if err != nil {
		return err
	}
	if err := markDataMigrationApplied(ctx, store.DB, reportAssetMigrationName); err != nil {
		return err
	}

	logReportAssetMigrationf(
		"report asset migration applied: copied=%d skipped=%d",
		stats.copied,
		stats.skipped,
	)
	return nil
}

func migrateReportAssets(
	ctx context.Context,
	db *gorm.DB,
	sourceCfg config.OSSConfig,
	targetCfg config.OSSConfig,
	sourceClient *minio.Client,
) (reportAssetMigrationStats, error) {
	stats := reportAssetMigrationStats{}
	lastObjectKey := ""
	for {
		batch, err := listReportAssetMigrationBatch(ctx, db, lastObjectKey, 200)
		if err != nil {
			return stats, err
		}
		if len(batch) == 0 {
			return stats, nil
		}

		for _, record := range batch {
			result, err := migrateSingleReportAsset(ctx, sourceCfg, targetCfg, sourceClient, record.ObjectKey)
			if err != nil {
				return stats, err
			}
			stats.copied += result.copied
			stats.skipped += result.skipped
		}

		lastObjectKey = batch[len(batch)-1].ObjectKey
	}
}

func listReportAssetMigrationBatch(
	ctx context.Context,
	db *gorm.DB,
	lastObjectKey string,
	limit int,
) ([]reportAssetMigrationRecord, error) {
	query := db.WithContext(ctx).
		Model(&model.ReportAttachment{}).
		Select("object_key").
		Where("object_key <> ''")
	if lastObjectKey != "" {
		query = query.Where("object_key > ?", lastObjectKey)
	}

	records := make([]reportAssetMigrationRecord, 0, limit)
	err := query.
		Group("object_key").
		Order("object_key ASC").
		Limit(limit).
		Scan(&records).Error
	return records, err
}

func migrateSingleReportAsset(
	ctx context.Context,
	sourceCfg config.OSSConfig,
	targetCfg config.OSSConfig,
	sourceClient *minio.Client,
	objectKey string,
) (reportAssetMigrationStats, error) {
	key := strings.TrimSpace(objectKey)
	if key == "" {
		return reportAssetMigrationStats{skipped: 1}, nil
	}

	if err := reportAssetMigrationCopyObject(
		ctx,
		sourceClient,
		getOSSClient(ossStorageReport),
		sourceCfg.Bucket,
		key,
		targetCfg.Bucket,
		key,
	); err != nil {
		return reportAssetMigrationStats{}, fmt.Errorf(
			"copy report asset object %s failed: %w",
			key,
			err,
		)
	}
	return reportAssetMigrationStats{copied: 1}, nil
}

func logReportAssetMigrationf(format string, args ...any) {
	if logger.L != nil {
		logger.L.Infof(format, args...)
	}
}
