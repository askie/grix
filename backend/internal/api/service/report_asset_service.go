package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
)

const (
	reportAssetMaxCount       = 3
	reportAssetMaxUploadBytes = 10 * 1024 * 1024
	reportAssetPresignTTL     = 10 * time.Minute
)

var (
	ErrReportAssetFilenameRequired = errors.New("report asset filename required")
	ErrReportAssetContentTypeMiss  = errors.New("report asset content type required")
	ErrReportAssetContentTypeBad   = errors.New("report asset content type invalid")
	ErrReportAssetCountInvalid     = errors.New("report asset count invalid")
	ErrReportAssetOwnershipDenied  = errors.New("report asset not owned by current user")
	ErrReportAssetNotFound         = errors.New("report asset not found")
	ErrReportAssetTooLarge         = errors.New("report asset too large")
)

var reportAssetAllowedContentTypes = map[string]struct{}{
	"image/jpeg": {},
	"image/png":  {},
	"image/webp": {},
}

var (
	reportAssetEnsureOSSReady = ensureReportOSSReady
	reportAssetNow            = func() time.Time { return time.Now().UTC() }
	reportAssetNewUUID        = uuid.NewString
	reportAssetStatObject     = func(
		ctx context.Context,
		bucket string,
		objectKey string,
	) (minio.ObjectInfo, error) {
		return getOSSClient(ossStorageReport).StatObject(
			ctx,
			bucket,
			objectKey,
			minio.StatObjectOptions{},
		)
	}
	reportAssetPresignPutObject = func(
		ctx context.Context,
		bucket string,
		objectKey string,
		expiry time.Duration,
	) (*PresignResp, error) {
		presignedURL, err := getOSSClient(ossStorageReport).PresignedPutObject(
			ctx,
			bucket,
			objectKey,
			expiry,
		)
		if err != nil {
			return nil, err
		}
		return &PresignResp{
			UploadURL: presignedURL.String(),
			ObjectKey: objectKey,
		}, nil
	}
	reportAssetPresignGetObject = func(
		ctx context.Context,
		bucket string,
		objectKey string,
		expiry time.Duration,
	) (string, error) {
		presignedURL, err := getOSSClient(ossStorageReport).PresignedGetObject(
			ctx,
			bucket,
			objectKey,
			expiry,
			nil,
		)
		if err != nil {
			return "", err
		}
		return presignedURL.String(), nil
	}
)

type ReportAssetPresignResp struct {
	AssetKey         string `json:"asset_key"`
	UploadURL        string `json:"upload_url"`
	ExpiresInSeconds int64  `json:"expires_in_seconds"`
}

type ReportAssetInfo struct {
	ObjectKey string
	MimeType  string
	SizeBytes int64
}

func PresignReportAsset(userID int64, filename, contentType string) (*ReportAssetPresignResp, error) {
	if userID <= 0 {
		return nil, ErrReportAssetOwnershipDenied
	}
	if err := reportAssetEnsureOSSReady(); err != nil {
		return nil, err
	}

	normalizedFilename := normalizeReportAssetFilename(filename)
	if normalizedFilename == "" {
		return nil, ErrReportAssetFilenameRequired
	}

	normalizedContentType := strings.ToLower(strings.TrimSpace(contentType))
	if normalizedContentType == "" {
		return nil, ErrReportAssetContentTypeMiss
	}
	if _, ok := reportAssetAllowedContentTypes[normalizedContentType]; !ok {
		return nil, ErrReportAssetContentTypeBad
	}

	objectKey := buildReportAssetObjectKey(userID, normalizedFilename)
	presignResp, err := reportAssetPresignPutObject(
		context.Background(),
		config.C.OSS.Report.Bucket,
		objectKey,
		reportAssetPresignTTL,
	)
	if err != nil {
		return nil, err
	}

	return &ReportAssetPresignResp{
		AssetKey:         presignResp.ObjectKey,
		UploadURL:        presignResp.UploadURL,
		ExpiresInSeconds: int64(reportAssetPresignTTL / time.Second),
	}, nil
}

func InspectReportAssets(userID int64, assetKeys []string) ([]ReportAssetInfo, error) {
	if userID <= 0 {
		return nil, ErrReportAssetOwnershipDenied
	}
	if err := reportAssetEnsureOSSReady(); err != nil {
		return nil, err
	}
	if len(assetKeys) == 0 || len(assetKeys) > reportAssetMaxCount {
		return nil, ErrReportAssetCountInvalid
	}

	seen := make(map[string]struct{}, len(assetKeys))
	items := make([]ReportAssetInfo, 0, len(assetKeys))
	for _, rawKey := range assetKeys {
		key := strings.TrimSpace(rawKey)
		if key == "" {
			return nil, ErrReportAssetNotFound
		}
		if _, ok := seen[key]; ok {
			return nil, ErrReportAssetCountInvalid
		}
		seen[key] = struct{}{}
		if !strings.HasPrefix(key, reportAssetObjectPrefix(userID)) {
			return nil, ErrReportAssetOwnershipDenied
		}

		info, err := reportAssetStatObject(
			context.Background(),
			config.C.OSS.Report.Bucket,
			key,
		)
		if err != nil {
			return nil, ErrReportAssetNotFound
		}
		if info.Size <= 0 {
			return nil, ErrReportAssetNotFound
		}
		if info.Size > reportAssetMaxUploadBytes {
			return nil, ErrReportAssetTooLarge
		}
		contentType := strings.ToLower(strings.TrimSpace(info.ContentType))
		if _, ok := reportAssetAllowedContentTypes[contentType]; !ok {
			return nil, ErrReportAssetContentTypeBad
		}

		items = append(items, ReportAssetInfo{
			ObjectKey: key,
			MimeType:  contentType,
			SizeBytes: info.Size,
		})
	}

	return items, nil
}

func normalizeReportAssetFilename(raw string) string {
	normalized := filepath.Base(strings.TrimSpace(raw))
	normalized = strings.ReplaceAll(normalized, "\\", "_")
	normalized = strings.ReplaceAll(normalized, "/", "_")
	if normalized == "." || normalized == "" {
		return ""
	}
	return normalized
}

func buildReportAssetObjectKey(userID int64, filename string) string {
	objectKey := fmt.Sprintf(
		"report-assets/%d/%s/%d_%s",
		userID,
		reportAssetNewUUID(),
		reportAssetNow().UnixMilli(),
		filename,
	)
	return buildStorageObjectKey(config.C.OSS.Report, objectKey)
}

func reportAssetObjectPrefix(userID int64) string {
	prefix := fmt.Sprintf("report-assets/%d/", userID)
	if dir := strings.Trim(config.C.OSS.Report.StorageDir, "/"); dir != "" {
		return fmt.Sprintf("%s/%s", dir, prefix)
	}
	return prefix
}

func PresignReportAssetViewURL(objectKey string, expiry time.Duration) (string, error) {
	if err := reportAssetEnsureOSSReady(); err != nil {
		return "", err
	}

	key := strings.TrimSpace(objectKey)
	if key == "" {
		return "", ErrReportAssetNotFound
	}

	return reportAssetPresignGetObject(
		context.Background(),
		config.C.OSS.Report.Bucket,
		key,
		expiry,
	)
}
