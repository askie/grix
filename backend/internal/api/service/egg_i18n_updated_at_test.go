package service

import (
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

// Hand-written locales submitted together must share one updated_at, otherwise the
// translation job sees the non-source locale as stale and overwrites it.
func TestUpsertEggI18nSharesUpdatedAtAcrossLocales(t *testing.T) {
	testDB, _ := setupEggSearchTestDB(t)
	const eggID = "edu.marble_learning_path"

	inputs := []EggI18nInput{
		{
			Locale:      "zh-CN",
			Name:        "Marble 学习路径规划师",
			Description: "为孩子规划学习路径",
			Vibe:        "陪伴",
		},
		{
			Locale:      "en-US",
			Name:        "Marble Learning Path Planner",
			Description: "Plans a learning path for kids",
			Vibe:        "Companion",
		},
	}
	if err := upsertEggI18nTx(testDB.DB, eggID, inputs); err != nil {
		t.Fatalf("upsertEggI18nTx error: %v", err)
	}

	assertEggI18nUpdatedAtAligned(t, eggID)

	// The same submission replayed as an update must stay aligned too.
	if err := upsertEggI18nTx(testDB.DB, eggID, inputs); err != nil {
		t.Fatalf("upsertEggI18nTx replay error: %v", err)
	}
	assertEggI18nUpdatedAtAligned(t, eggID)
}

func assertEggI18nUpdatedAtAligned(t *testing.T, eggID string) {
	t.Helper()

	var zh, en model.EggI18n
	if err := store.DB.Where("egg_id = ? AND locale = ?", eggID, "zh-CN").Take(&zh).Error; err != nil {
		t.Fatalf("load zh-CN egg i18n error: %v", err)
	}
	if err := store.DB.Where("egg_id = ? AND locale = ?", eggID, "en-US").Take(&en).Error; err != nil {
		t.Fatalf("load en-US egg i18n error: %v", err)
	}
	if !zh.UpdatedAt.Equal(en.UpdatedAt) {
		t.Fatalf("egg i18n updated_at mismatch: zh-CN=%s en-US=%s", zh.UpdatedAt, en.UpdatedAt)
	}

	source, err := loadEggI18nTranslationSource(eggID)
	if err != nil {
		t.Fatalf("loadEggI18nTranslationSource error: %v", err)
	}
	if source.Locale != "en-US" {
		t.Fatalf("unexpected translation source locale: %s", source.Locale)
	}
	needsTranslation, err := eggI18nNeedsTranslation(eggID, "zh-CN", source.UpdatedAt)
	if err != nil {
		t.Fatalf("eggI18nNeedsTranslation error: %v", err)
	}
	if needsTranslation {
		t.Fatalf("hand-written zh-CN egg i18n was treated as stale against en-US")
	}
}

func TestUpsertEggVersionI18nSharesUpdatedAtAcrossLocales(t *testing.T) {
	testDB, _ := setupEggSearchTestDB(t)
	const eggID = "edu.marble_learning_path"
	const version = 1

	inputs := []EggVersionI18nInput{
		{Locale: "zh-CN", VersionDesc: "首个正式版本"},
		{Locale: "en-US", VersionDesc: "First public release"},
	}
	if err := upsertEggVersionI18nTx(testDB.DB, eggID, version, inputs); err != nil {
		t.Fatalf("upsertEggVersionI18nTx error: %v", err)
	}

	assertEggVersionI18nUpdatedAtAligned(t, eggID, version)

	if err := upsertEggVersionI18nTx(testDB.DB, eggID, version, inputs); err != nil {
		t.Fatalf("upsertEggVersionI18nTx replay error: %v", err)
	}
	assertEggVersionI18nUpdatedAtAligned(t, eggID, version)
}

func assertEggVersionI18nUpdatedAtAligned(t *testing.T, eggID string, version int) {
	t.Helper()

	var zh, en model.EggVersionI18n
	if err := store.DB.Where("egg_id = ? AND version = ? AND locale = ?", eggID, version, "zh-CN").Take(&zh).Error; err != nil {
		t.Fatalf("load zh-CN version i18n error: %v", err)
	}
	if err := store.DB.Where("egg_id = ? AND version = ? AND locale = ?", eggID, version, "en-US").Take(&en).Error; err != nil {
		t.Fatalf("load en-US version i18n error: %v", err)
	}
	if !zh.UpdatedAt.Equal(en.UpdatedAt) {
		t.Fatalf("version i18n updated_at mismatch: zh-CN=%s en-US=%s", zh.UpdatedAt, en.UpdatedAt)
	}

	source, err := loadEggVersionI18nTranslationSource(eggID, version)
	if err != nil {
		t.Fatalf("loadEggVersionI18nTranslationSource error: %v", err)
	}
	if source.Locale != "en-US" {
		t.Fatalf("unexpected version translation source locale: %s", source.Locale)
	}
	needsTranslation, err := eggVersionI18nNeedsTranslation(eggID, version, "zh-CN", source.UpdatedAt)
	if err != nil {
		t.Fatalf("eggVersionI18nNeedsTranslation error: %v", err)
	}
	if needsTranslation {
		t.Fatalf("hand-written zh-CN version i18n was treated as stale against en-US")
	}
}

func TestUpsertEggCategoryI18nSharesUpdatedAtAcrossLocales(t *testing.T) {
	testDB, _ := setupEggSearchTestDB(t)
	const categoryID = "edu"

	if err := upsertEggCategoryI18nTx(testDB.DB, categoryID, []EggCategoryI18nInput{
		{Locale: "zh-CN", Name: "教育", Description: "教育类"},
		{Locale: "en-US", Name: "Education", Description: "Education eggs"},
	}); err != nil {
		t.Fatalf("upsertEggCategoryI18nTx error: %v", err)
	}

	var zh, en model.EggCategoryI18n
	if err := testDB.DB.Where("category_id = ? AND locale = ?", categoryID, "zh-CN").Take(&zh).Error; err != nil {
		t.Fatalf("load zh-CN category i18n error: %v", err)
	}
	if err := testDB.DB.Where("category_id = ? AND locale = ?", categoryID, "en-US").Take(&en).Error; err != nil {
		t.Fatalf("load en-US category i18n error: %v", err)
	}
	if !zh.UpdatedAt.Equal(en.UpdatedAt) {
		t.Fatalf("category i18n updated_at mismatch: zh-CN=%s en-US=%s", zh.UpdatedAt, en.UpdatedAt)
	}
}
