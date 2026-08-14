package service

import (
	"strings"

	"github.com/askie/grix/backend/config"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func newOSSClient(cfg config.OSSConfig) (*minio.Client, error) {
	// Use path-style lookup for localhost endpoints (for testing),
	// otherwise use DNS-style (virtual-hosted) for production S3/MinIO.
	lookup := minio.BucketLookupDNS
	if strings.HasPrefix(cfg.Endpoint, "localhost:") || strings.HasPrefix(cfg.Endpoint, "127.0.0.1:") {
		lookup = minio.BucketLookupPath
	}

	return minio.New(cfg.Endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure:       cfg.UseSSL,
		Region:       cfg.Region,
		BucketLookup: lookup,
	})
}

func hasAnyOSSConfigValue(cfg config.OSSConfig) bool {
	return strings.TrimSpace(cfg.Endpoint) != "" ||
		strings.TrimSpace(cfg.AccessKey) != "" ||
		strings.TrimSpace(cfg.SecretKey) != "" ||
		strings.TrimSpace(cfg.Bucket) != "" ||
		strings.TrimSpace(cfg.Region) != "" ||
		strings.TrimSpace(cfg.PublicURL) != "" ||
		strings.TrimSpace(cfg.StorageDir) != ""
}

func getLegacyMigrationOSSConfig() (config.OSSConfig, bool) {
	cfg := config.C.Migration.LegacyOSS
	return cfg, hasAnyOSSConfigValue(cfg)
}

func sameOSSStorageLocation(left, right config.OSSConfig) bool {
	return strings.TrimSpace(left.Endpoint) == strings.TrimSpace(right.Endpoint) &&
		strings.TrimSpace(left.Bucket) == strings.TrimSpace(right.Bucket) &&
		strings.TrimSpace(left.Region) == strings.TrimSpace(right.Region) &&
		left.UseSSL == right.UseSSL
}
