package main

import (
	"context"
	"os"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/version"
)

func main() {
	configPath := "config.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	logger.Init()
	v := version.Get()
	logger.L.Infof("migrate starting: version=%s commit=%s build_time=%s", v.Version, v.Commit, v.BuildTime)
	config.Load(configPath)

	store.InitPostgres(config.C.Postgres)
	if err := store.ApplyMigrations(store.DB); err != nil {
		logger.L.Fatalf("migration failed: %v", err)
	}
	if err := service.RunToolMessageCompactionMigration(context.Background()); err != nil {
		logger.L.Fatalf("tool message compaction migration failed: %v", err)
	}
	if err := service.InitOSS(); err != nil {
		logger.L.Fatalf("oss init failed: %v", err)
	}
	if err := service.RunAvatarStorageMigration(context.Background()); err != nil {
		logger.L.Fatalf("avatar storage migration failed: %v", err)
	}
	if err := service.RunAvatarBucketMigration(context.Background()); err != nil {
		logger.L.Fatalf("avatar bucket migration failed: %v", err)
	}
	if err := service.RunReportAssetMigration(context.Background()); err != nil {
		logger.L.Fatalf("report asset migration failed: %v", err)
	}
	if err := service.RunPhoneEncryptionMigration(context.Background()); err != nil {
		logger.L.Fatalf("phone encryption migration failed: %v", err)
	}

	logger.L.Info("migration completed")
}
