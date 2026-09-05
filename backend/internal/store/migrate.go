package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"gorm.io/gorm"
)

const (
	defaultMigrationDir = "migration"
	schemaLockID        = 904502110113
)

type migrationFile struct {
	Version string
	Path    string
	SQL     string
}

var autoMigrateModels = []any{
	&model.User{},
	&model.UserIdentity{},
	&model.AdminUser{},
	&model.AdminSession{},
	&model.SystemSetting{},
	&model.AdminOperationLog{},
	&model.OAuthAccount{},
	&model.Agent{},
	&model.AgentCategory{},
	&model.Session{},
	&model.SessionMember{},
	&model.Message{},
	&model.SendMsgIdempotencyReceipt{},
	&model.ConversationAuditTurn{},
	&model.ConversationAuditPref{},
	&model.UserInbox{},
	&model.Device{},
	&model.FriendRequest{},
	&model.Friend{},
	&model.UserPeerPin{},
	&model.UserPeerMute{},
	&model.UserBlock{},
	&model.FriendQRCode{},
	&model.GroupQRCode{},
	&model.UserSetting{},
	&model.RegisterWelcomeCompensation{},
	&model.LLMUsageLog{},
	&model.AuditLog{},
	&model.ContentModerationEvent{},
	&model.LinkBlocklistRule{},
	&model.DelegationLog{},
	&model.MemoryEmbedding{},
	&model.KnowledgeDoc{},
	&model.MessageReaction{},
	&model.RefreshToken{},
	&model.LoginDeviceSession{},
	&model.AuthQRLoginSession{},
	&model.SessionHistoryReset{},
	&model.GeminiSessionContext{},
	&model.AgentSessionBinding{},
	&model.AgentSessionRouteMapping{},
	&model.AgentSessionSyncState{},
	&model.AgentNativeMessageImport{},
	&model.AgentAPIScope{},
	&model.AgentQueuedEvent{},
	&model.Report{},
	&model.ReportAttachment{},
	&model.ReportActionLog{},
	&model.EggCategory{},
	&model.EggCategoryI18n{},
	&model.Egg{},
	&model.EggI18n{},
	&model.EggVersion{},
	&model.EggVersionI18n{},
	&model.EggTranslationJob{},
	&model.EggInstall{},
	&model.UserFavoritePath{},
	&model.ConnectorRelease{},
	&model.ConnectorRolloutRule{},
	&model.ConnectorUpgradeReport{},
	&model.WebhookEndpoint{},
	&model.WidgetSite{},
	&model.WidgetSession{},
	&model.WidgetIPBan{},
	&model.CallRecord{},
	&model.FeatureGate{},
	&model.FeatureGateUser{},
	&model.AppRelease{},
	&model.AppRolloutRule{},
	&model.AppDownloadReport{},
	&model.NotificationPref{},
	&model.ReachTask{},
	&model.ReachTemplate{},
	&model.ReachSendLog{},
	&model.ReachSubscription{},
	&model.SessionAgentState{},
	&model.AgentEventTerminalLedger{},
	&model.AgentEventTerminalEffect{},
	&model.AgentNotificationReceipt{},
	&model.AgentRunSequence{},
	&model.SessionTombstone{},
	&model.AgentShare{},
	&model.AgentConnectionLog{},
	&model.AgentIPRule{},
	&model.AgentSlashCommand{},
	&model.UserSessionFavorite{},
	&model.GatewayWallet{},
	&model.GatewayVirtualKey{},
	&model.GatewayReconciliationReport{},
	&model.GatewayPricingRule{},
	&model.GatewayLedgerEntry{},
	&model.GatewayTopupRecord{},
	&model.GatewayTopupOrder{},
	&model.GatewayFxRate{},
	&model.GatewayUpstreamCredential{},
	&model.GatewayRelaySetting{},
	&model.GatewayAgentRelayState{},
	&model.UserSkill{},
}

// AutoMigrateWithDB migrates all backend models using the provided db.
func AutoMigrateWithDB(db *gorm.DB) error {
	if db == nil {
		return errors.New("db is nil")
	}
	return db.AutoMigrate(autoMigrateModels...)
}

// MustInitSchema applies SQL migrations from the default migration dir.
// MaybeInitSchema 在未显式跳过时执行 schema 初始化/迁移。
// 经 PgBouncer transaction 池连接的业务服务必须跳过：MustInitSchema 用会话级
// pg_advisory_lock，事务池下会泄漏/死锁；迁移由专用 migrate Job 直连 PG 负责。
// 设 AIBOT_SKIP_SCHEMA_INIT=1 跳过。
func MaybeInitSchema() {
	if os.Getenv("AIBOT_SKIP_SCHEMA_INIT") == "1" {
		logger.L.Info("schema init skipped (AIBOT_SKIP_SCHEMA_INIT=1); migrations owned by migrate job")
		return
	}
	MustInitSchema()
}

func MustInitSchema() {
	if err := ApplyMigrations(DB); err != nil {
		logger.L.Fatalf("init schema failed: %v", err)
	}
	logger.L.Infof("init schema completed")
}

// ApplyMigrations applies SQL migrations once per database.
func ApplyMigrations(db *gorm.DB) error {
	return ApplyMigrationsFromDir(db, defaultMigrationDir)
}

// ApplyMigrationsFromDir applies SQL migrations from the given directory.
func ApplyMigrationsFromDir(db *gorm.DB, dir string) error {
	if db == nil {
		return errors.New("db is nil")
	}

	ctx := context.Background()
	migrations, err := loadMigrationFiles(dir)
	if err != nil {
		return err
	}
	if len(migrations) == 0 {
		return fmt.Errorf("no migration files found in %s", dir)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get sql DB: %w", err)
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open db conn: %w", err)
	}
	defer conn.Close()

	if err := lockSchemaMigrate(conn); err != nil {
		return err
	}
	defer func() {
		if unlockErr := unlockSchemaMigrate(conn); unlockErr != nil {
			logger.L.Warnf("unlock schema migrate failed: %v", unlockErr)
		}
	}()

	if err := ensureMigrationTable(conn); err != nil {
		return err
	}
	applied, err := loadAppliedMigrations(conn)
	if err != nil {
		return err
	}

	for _, mf := range migrations {
		if _, ok := applied[mf.Version]; ok {
			continue
		}

		logger.L.Infof("applying migration %s", mf.Version)
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration tx: %w", err)
		}
		committed := false
		func() {
			defer func() {
				if !committed {
					_ = tx.Rollback()
				}
			}()

			if _, err = tx.ExecContext(ctx, mf.SQL); err != nil {
				err = fmt.Errorf("apply migration %s (%s): %w", mf.Version, mf.Path, err)
				return
			}
			if _, err = tx.ExecContext(
				ctx,
				`INSERT INTO schema_migrations(version) VALUES ($1)`,
				mf.Version,
			); err != nil {
				err = fmt.Errorf("record migration %s: %w", mf.Version, err)
				return
			}
			if err = tx.Commit(); err != nil {
				err = fmt.Errorf("commit migration %s: %w", mf.Version, err)
				return
			}
			committed = true
		}()
		if err != nil {
			return err
		}
		logger.L.Infof("migration applied %s", mf.Version)
	}

	return nil
}

func lockSchemaMigrate(conn *sql.Conn) error {
	if _, err := conn.ExecContext(
		context.Background(),
		`SELECT pg_advisory_lock($1)`,
		schemaLockID,
	); err != nil {
		return fmt.Errorf("acquire schema migrate lock: %w", err)
	}
	return nil
}

func unlockSchemaMigrate(conn *sql.Conn) error {
	if _, err := conn.ExecContext(
		context.Background(),
		`SELECT pg_advisory_unlock($1)`,
		schemaLockID,
	); err != nil {
		return fmt.Errorf("release schema migrate lock: %w", err)
	}
	return nil
}

func loadMigrationFiles(dir string) ([]migrationFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read migration dir %s: %w", dir, err)
	}

	files := make([]migrationFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		// 跳过隐藏文件与 macOS AppleDouble 元数据（如 ._0001_init.sql）：
		// 这些文件名也以 .sql 结尾，但内容是二进制元数据，被当 SQL 执行会报
		// "invalid message format"。统一以 "." 开头判定为非迁移文件。
		if strings.HasPrefix(name, ".") {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(name), ".sql") {
			continue
		}

		p := filepath.Join(dir, name)
		content, readErr := os.ReadFile(p)
		if readErr != nil {
			return nil, fmt.Errorf("read migration %s: %w", p, readErr)
		}
		files = append(files, migrationFile{
			Version: name,
			Path:    p,
			SQL:     string(content),
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Version < files[j].Version
	})
	return files, nil
}

func ensureMigrationTable(conn *sql.Conn) error {
	_, err := conn.ExecContext(context.Background(), `
CREATE TABLE IF NOT EXISTS schema_migrations (
	version VARCHAR(255) PRIMARY KEY,
	applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`)
	if err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}
	return nil
}

func loadAppliedMigrations(conn *sql.Conn) (map[string]struct{}, error) {
	rows, err := conn.QueryContext(context.Background(), `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer rows.Close()

	result := make(map[string]struct{})
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan schema_migrations: %w", err)
		}
		result[version] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema_migrations: %w", err)
	}
	return result, nil
}
