package store

import (
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/metrics"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var DB *gorm.DB

// ReadDB 指向只读副本连接池(配置 read_host 时);否则复用主库。
var ReadDB *gorm.DB

// Read 返回只读查询应使用的连接:有副本走副本,否则回主库(测试/未配时安全)。
func Read() *gorm.DB {
	if ReadDB != nil {
		return ReadDB
	}
	return DB
}

// IsPostgres returns true when the global DB is backed by PostgreSQL.
func IsPostgres() bool {
	if DB == nil {
		return false
	}
	_, ok := DB.Config.Dialector.(*postgres.Dialector)
	return ok
}

func InitPostgres(cfg config.PostgresConfig) {
	sslmode := strings.TrimSpace(cfg.SSLMode)
	if sslmode == "" {
		sslmode = "disable"
	}
	// Force UTC session timezone so TIMESTAMP (without timezone) values are
	// interpreted consistently across environments.
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s TimeZone=UTC",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DBName, sslmode)
	var err error
	// PreferSimpleProtocol 关闭服务端预编译语句，以兼容 PgBouncer transaction 池化模式。
	DB, err = gorm.Open(postgres.New(postgres.Config{
		DSN:                  dsn,
		PreferSimpleProtocol: true,
	}), &gorm.Config{})
	if err != nil {
		logger.L.Fatalf("failed to connect postgres: %v", err)
	}
	sqlDB, _ := DB.DB()
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)
	sqlDB.SetConnMaxIdleTime(10 * time.Minute)
	metrics.RegisterDBPool("primary", sqlDB.Stats)
	logger.L.Info("postgres connected")

	// 只读副本(可选):配 read_host 后,读密集查询(pull_sync 历史/未读等)经 Read() 走副本,
	// 卸载主库读 CPU;未配或连接失败则回退主库,行为不变。
	if cfg.ReadHost != "" && cfg.ReadHost != cfg.Host {
		readDSN := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s TimeZone=UTC",
			cfg.ReadHost, cfg.Port, cfg.User, cfg.Password, cfg.DBName, sslmode)
		rdb, rerr := gorm.Open(postgres.New(postgres.Config{DSN: readDSN, PreferSimpleProtocol: true}), &gorm.Config{})
		if rerr != nil {
			logger.L.Warnf("connect read replica %s failed, fallback to primary: %v", cfg.ReadHost, rerr)
			ReadDB = DB
		} else {
			if sqlRDB, e := rdb.DB(); e == nil {
				sqlRDB.SetMaxOpenConns(cfg.MaxOpenConns)
				sqlRDB.SetMaxIdleConns(cfg.MaxIdleConns)
				sqlRDB.SetConnMaxLifetime(5 * time.Minute)
				sqlRDB.SetConnMaxIdleTime(10 * time.Minute)
				metrics.RegisterDBPool("read", sqlRDB.Stats)
			}
			ReadDB = rdb
			logger.L.Infof("postgres read replica connected: %s", cfg.ReadHost)
		}
	} else {
		ReadDB = DB
	}
}
