package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// loadMigrationFiles 必须只收正常的 .sql 迁移文件，跳过隐藏文件与 macOS
// AppleDouble 元数据（._ 前缀，文件名同样以 .sql 结尾），否则迁移引擎会把
// 二进制元数据当 SQL 执行并报 "invalid message format"。
func TestLoadMigrationFilesSkipsHiddenAndAppleDouble(t *testing.T) {
	dir := t.TempDir()

	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	write("0001_init.sql", "CREATE TABLE t (id INT);")
	write("0002_more.sql", "CREATE TABLE u (id INT);")
	write("._0001_init.sql", "\x00\x05\x16\x07binary-appledouble") // AppleDouble，含二进制
	write(".DS_Store", "junk")
	write("README.md", "not a migration")

	files, err := loadMigrationFiles(dir)
	if err != nil {
		t.Fatalf("loadMigrationFiles error: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("loaded %d files, want 2: %+v", len(files), files)
	}
	if files[0].Version != "0001_init.sql" || files[1].Version != "0002_more.sql" {
		t.Fatalf("unexpected versions: %s, %s", files[0].Version, files[1].Version)
	}
}

func TestAutoMigrateWithDBCreatesEggI18nLocaleIndex(t *testing.T) {
	dsn := fmt.Sprintf("file:store_index_test_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite db error: %v", err)
	}

	if err := AutoMigrateWithDB(db); err != nil {
		t.Fatalf("AutoMigrateWithDB error: %v", err)
	}

	type indexListRow struct {
		Name string `gorm:"column:name"`
	}
	var indexes []indexListRow
	if err := db.Raw("PRAGMA index_list('egg_i18n')").Scan(&indexes).Error; err != nil {
		t.Fatalf("load egg_i18n indexes error: %v", err)
	}

	found := false
	for _, item := range indexes {
		if item.Name == "idx_egg_i18n_locale_egg" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected idx_egg_i18n_locale_egg in indexes=%+v", indexes)
	}

	type indexInfoRow struct {
		Name string `gorm:"column:name"`
	}
	var columns []indexInfoRow
	if err := db.Raw("PRAGMA index_info('idx_egg_i18n_locale_egg')").Scan(&columns).Error; err != nil {
		t.Fatalf("load idx_egg_i18n_locale_egg columns error: %v", err)
	}
	if len(columns) != 2 {
		t.Fatalf("idx_egg_i18n_locale_egg column count=%d want=2 columns=%+v", len(columns), columns)
	}
	if columns[0].Name != "locale" || columns[1].Name != "egg_id" {
		t.Fatalf("idx_egg_i18n_locale_egg columns=%+v want=[locale egg_id]", columns)
	}
}
