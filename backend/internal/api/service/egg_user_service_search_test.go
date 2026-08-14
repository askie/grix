package service

import (
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/datatypes"
)

func TestEggSearchKeywordReturnsRankedEggs(t *testing.T) {
	testDB, userID := setupEggSearchTestDB(t)
	seedSearchCategory(t, testDB, "persona")
	seedSearchEgg(
		t,
		testDB,
		seedSearchEggParams{
			ID:           "lobster.persona_test",
			CategoryID:   "persona",
			Locale:       "zh",
			Name:         "测试虾蛋",
			Description:  "用于验证关键词搜索。",
			Vibe:         "执行型",
			SearchText:   "测试虾蛋 shrimp egg openclaw persona",
			InstallCount: 9,
		},
	)

	resp, ec := EggSearch(userID, EggSearchReq{
		Keyword:  "虾蛋",
		Locale:   "zh",
		Page:     1,
		PageSize: 10,
	})
	if ec != nil {
		t.Fatalf("EggSearch error: %#v", ec)
	}
	if len(resp.List) != 1 {
		t.Fatalf("list len=%d want=1", len(resp.List))
	}
	if resp.List[0].ID != "lobster.persona_test" {
		t.Fatalf("egg id=%q want=%q", resp.List[0].ID, "lobster.persona_test")
	}
	if resp.List[0].CategoryID != "persona" {
		t.Fatalf("category_id=%q want=%q", resp.List[0].CategoryID, "persona")
	}
	if resp.List[0].Version != 1 {
		t.Fatalf("version=%d want=1", resp.List[0].Version)
	}
	if !resp.List[0].CanCreateAgent {
		t.Fatal("expected can_create_agent=true")
	}
	if len(resp.List[0].ExistingAgentClientTypes) != 2 || resp.List[0].ExistingAgentClientTypes[0] != model.AgentClientTypeOpenClaw || resp.List[0].ExistingAgentClientTypes[1] != model.AgentClientTypeHermes {
		t.Fatalf("existing_agent_client_types=%v want=[openclaw hermes]", resp.List[0].ExistingAgentClientTypes)
	}
}

func TestEggSearchIsCaseInsensitive(t *testing.T) {
	testDB, userID := setupEggSearchTestDB(t)
	seedSearchCategory(t, testDB, "persona")
	seedSearchEgg(
		t,
		testDB,
		seedSearchEggParams{
			ID:           "lobster.case_test",
			CategoryID:   "persona",
			Locale:       "en-US",
			Name:         "Shrimp Helper",
			Description:  "OpenClaw persona helper",
			Vibe:         "Execution",
			SearchText:   buildEggSearchText("Shrimp Helper", "OpenClaw persona helper", "Execution"),
			InstallCount: 3,
		},
	)

	resp, ec := EggSearch(userID, EggSearchReq{
		Keyword:  "sHRimP HELPER",
		Locale:   "en-US",
		Page:     1,
		PageSize: 10,
	})
	if ec != nil {
		t.Fatalf("EggSearch error: %#v", ec)
	}
	if len(resp.List) != 1 {
		t.Fatalf("list len=%d want=1", len(resp.List))
	}
	if resp.List[0].ID != "lobster.case_test" {
		t.Fatalf("egg id=%q want=%q", resp.List[0].ID, "lobster.case_test")
	}
}

func TestEggSearchMatchesMultipleTermsRegardlessOfOrderAndRanksExactPhraseFirst(t *testing.T) {
	testDB, userID := setupEggSearchTestDB(t)
	seedSearchCategory(t, testDB, "persona")
	seedSearchEgg(
		t,
		testDB,
		seedSearchEggParams{
			ID:           "lobster.exact_phrase",
			CategoryID:   "persona",
			Locale:       "en-US",
			Name:         "OpenClaw Shrimp",
			Description:  "Best exact phrase match",
			Vibe:         "Assistant",
			SearchText:   buildEggSearchText("OpenClaw Shrimp", "Best exact phrase match", "Assistant"),
			InstallCount: 2,
		},
	)
	seedSearchEgg(
		t,
		testDB,
		seedSearchEggParams{
			ID:           "lobster.term_match",
			CategoryID:   "persona",
			Locale:       "en-US",
			Name:         "Shrimp assistant",
			Description:  "Built for OpenClaw workflows",
			Vibe:         "Assistant",
			SearchText:   buildEggSearchText("Shrimp assistant", "Built for OpenClaw workflows", "Assistant"),
			InstallCount: 20,
		},
	)

	resp, ec := EggSearch(userID, EggSearchReq{
		Keyword:  "openclaw shrimp",
		Locale:   "en-US",
		Page:     1,
		PageSize: 10,
	})
	if ec != nil {
		t.Fatalf("EggSearch error: %#v", ec)
	}
	if len(resp.List) != 2 {
		t.Fatalf("list len=%d want=2", len(resp.List))
	}
	if resp.List[0].ID != "lobster.exact_phrase" {
		t.Fatalf("first egg id=%q want=%q", resp.List[0].ID, "lobster.exact_phrase")
	}
	if resp.List[1].ID != "lobster.term_match" {
		t.Fatalf("second egg id=%q want=%q", resp.List[1].ID, "lobster.term_match")
	}

	reversedResp, ec := EggSearch(userID, EggSearchReq{
		Keyword:  "shrimp openclaw",
		Locale:   "en-US",
		Page:     1,
		PageSize: 10,
	})
	if ec != nil {
		t.Fatalf("EggSearch reversed error: %#v", ec)
	}
	if len(reversedResp.List) != 2 {
		t.Fatalf("reversed list len=%d want=2", len(reversedResp.List))
	}
}

type seedSearchEggParams struct {
	ID           string
	CategoryID   string
	Locale       string
	Name         string
	Description  string
	Vibe         string
	SearchText   string
	InstallCount int64
}

func setupEggSearchTestDB(t *testing.T) (*testutil.TestDB, int64) {
	t.Helper()

	testDB := testutil.NewTestDB()
	prevDB := store.DB
	prevRDB := store.RDB
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		if store.RDB != nil {
			_ = store.RDB.Close()
		}
		store.DB = prevDB
		store.RDB = prevRDB
		testDB.Close()
	})

	const userID int64 = 8101
	seedEggInstallUser(t, testDB, userID)
	return testDB, userID
}

func seedSearchCategory(t *testing.T, testDB *testutil.TestDB, categoryID string) {
	t.Helper()
	if err := testDB.DB.Create(&model.EggCategory{
		ID:     categoryID,
		Code:   categoryID,
		Status: model.EggCategoryStatusActive,
	}).Error; err != nil {
		t.Fatalf("seed category error: %v", err)
	}
	if err := testDB.DB.Create(&model.EggCategoryI18n{
		CategoryID:  categoryID,
		Locale:      "zh",
		Name:        "人格",
		Description: "人格蛋",
	}).Error; err != nil {
		t.Fatalf("seed category i18n error: %v", err)
	}
	if err := testDB.DB.Create(&model.EggCategoryI18n{
		CategoryID:  categoryID,
		Locale:      "en-US",
		Name:        "Persona",
		Description: "Persona eggs",
	}).Error; err != nil {
		t.Fatalf("seed category i18n error: %v", err)
	}
}

func seedSearchEgg(t *testing.T, testDB *testutil.TestDB, params seedSearchEggParams) {
	t.Helper()

	if err := testDB.DB.Create(&model.Egg{
		ID:           params.ID,
		CategoryID:   params.CategoryID,
		DefaultColor: "#D97706",
		DefaultEmoji: "🦞",
		Status:       model.EggStatusPublished,
		InstallCount: params.InstallCount,
	}).Error; err != nil {
		t.Fatalf("seed egg error: %v", err)
	}
	if err := testDB.DB.Create(&model.EggI18n{
		EggID:                params.ID,
		Locale:               params.Locale,
		Name:                 params.Name,
		Description:          params.Description,
		Vibe:                 params.Vibe,
		SearchTextNormalized: params.SearchText,
	}).Error; err != nil {
		t.Fatalf("seed egg i18n error: %v", err)
	}
	if err := testDB.DB.Create(&model.EggVersion{
		EggID:                params.ID,
		Version:              1,
		ZipURL:               "https://example.com/" + params.ID + "-v1.zip",
		ZipSHA256:            "abc123",
		ZipSize:              1024,
		PersonaZipURL:        "https://example.com/" + params.ID + "-v1.zip",
		PersonaZipSHA256:     "abc123",
		PersonaZipSize:       1024,
		ArtifactManifestJSON: datatypes.JSON([]byte(`{"persona":{"entry":"persona.md"}}`)),
	}).Error; err != nil {
		t.Fatalf("seed egg version error: %v", err)
	}
	if err := testDB.DB.Create(&model.EggVersionI18n{
		EggID:       params.ID,
		Version:     1,
		Locale:      params.Locale,
		VersionDesc: "测试版本",
	}).Error; err != nil {
		t.Fatalf("seed egg version i18n error: %v", err)
	}
}
