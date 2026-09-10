package main

import (
	"context"
	"flag"
	"os"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/version"
)

// backfillProviderKeysFlagUsage documents the deployment-order requirement
// callers must satisfy before setting this flag — see
// .agents/notes/implemented/2026-09-05-generic-acp-client-type.md.
const backfillProviderKeysFlagUsage = "run RunOpencodeDeepseekProviderKeyMigration (the opencode/deepseek/deveco " +
	"provider_key backfill) after the schema migrations; off by default. " +
	"Only enable this after BOTH the backend code fix and the grix-connector " +
	"providerKeyForAdapter fix are live (connector first or simultaneously, " +
	"never the backend alone first) — see " +
	".agents/notes/implemented/2026-09-05-generic-acp-client-type.md."

// parseArgs isolates flag parsing from main so it can be unit tested without
// touching config loading or the database.
func parseArgs(args []string) (configPath string, backfillProviderKeys bool) {
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	backfill := fs.Bool("backfill-provider-keys", false, backfillProviderKeysFlagUsage)
	_ = fs.Parse(args)

	configPath = "config.yaml"
	if fs.NArg() > 0 {
		configPath = fs.Arg(0)
	}
	return configPath, *backfill
}

func main() {
	configPath, backfillProviderKeys := parseArgs(os.Args[1:])

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

	// Explicit opt-in only — see backfillProviderKeysFlagUsage above for the
	// deployment-order requirement. Everything above this point runs
	// unconditionally on every deploy; this one does not.
	if backfillProviderKeys {
		logger.L.Info("backfill-provider-keys: running opencode/deepseek/deveco provider_key migration")
		if err := service.RunOpencodeDeepseekProviderKeyMigration(context.Background()); err != nil {
			logger.L.Fatalf("opencode/deepseek/deveco provider_key backfill failed: %v", err)
		}
	}

	logger.L.Info("migration completed")
}
