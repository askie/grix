package service

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func setupAdminEggCreateWithFilesTest(t *testing.T) (*testutil.TestDB, func()) {
	t.Helper()

	testDB := testutil.NewTestDB()
	originalDB := store.DB
	originalUploader := adminEggArtifactUploader
	originalDeleter := adminEggArtifactDeleter

	store.DB = testDB.DB
	adminEggArtifactUploader = func(objectKey, localPath string, size int64) (string, error) {
		if _, err := os.ReadFile(localPath); err != nil {
			return "", err
		}
		return "https://media.example.com/" + objectKey, nil
	}
	adminEggArtifactDeleter = func(objectKey string) error {
		return nil
	}

	return testDB, func() {
		adminEggArtifactUploader = originalUploader
		adminEggArtifactDeleter = originalDeleter
		store.DB = originalDB
		testDB.Close()
	}
}

func seedAdminEggCategory(t *testing.T, testDB *testutil.TestDB, categoryID string, status string, name string, description string) {
	t.Helper()

	category := model.EggCategory{
		ID:        categoryID,
		Code:      categoryID,
		Status:    status,
		SortOrder: 1,
	}
	if err := testDB.DB.Create(&category).Error; err != nil {
		t.Fatalf("seed egg category: %v", err)
	}
	if err := testDB.DB.Create(&model.EggCategoryI18n{
		CategoryID:  categoryID,
		Locale:      "zh-CN",
		Name:        name,
		Description: description,
	}).Error; err != nil {
		t.Fatalf("seed egg category i18n: %v", err)
	}
}

func makeTestZipBytes(t *testing.T, fileName, content string) []byte {
	t.Helper()

	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)
	writer, err := zipWriter.Create(fileName)
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := writer.Write([]byte(content)); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	return buf.Bytes()
}

func makeMultipartFileHeader(t *testing.T, fieldName, fileName string, content []byte) *multipart.FileHeader {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(fieldName, fileName)
	if err != nil {
		t.Fatalf("create multipart form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write multipart form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest("POST", "/", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if err := req.ParseMultipartForm(64 << 20); err != nil {
		t.Fatalf("parse multipart form: %v", err)
	}

	files := req.MultipartForm.File[fieldName]
	if len(files) != 1 {
		t.Fatalf("expected 1 multipart file, got %d", len(files))
	}
	return files[0]
}

func TestAdminEggCreateWithFiles_CreatesAndPublishesEgg(t *testing.T) {
	testDB, cleanup := setupAdminEggCreateWithFilesTest(t)
	defer cleanup()

	seedAdminEggCategory(t, testDB, "productivity", model.EggCategoryStatusActive, "生产力", "效率工具")

	personaZip := makeMultipartFileHeader(t, "persona_zip", "openclaw.zip", makeTestZipBytes(t, "persona.md", "hello"))
	skillZip := makeMultipartFileHeader(t, "skill_zip", "skill.zip", makeTestZipBytes(t, "skill.md", "world"))

	resp, ec := AdminEggCreateWithFiles(AdminEggCreateWithFilesReq{
		Meta: AdminEggCreateWithFilesMeta{
			ID:         "translator-pro",
			CategoryID: "productivity",
			EggI18n: []EggI18nInput{
				{Locale: "zh-CN", Name: "翻译助手", Description: "支持翻译"},
				{Locale: "en-US", Name: "Translator", Description: "Translate text"},
			},
			VersionI18n: []EggVersionI18nInput{
				{Locale: "zh-CN", VersionDesc: "初始版本"},
				{Locale: "en-US", VersionDesc: "Initial release"},
			},
			ArtifactManifest: json.RawMessage(`{"source":"test"}`),
		},
		PersonaZipFile: personaZip,
		SkillZipFile:   skillZip,
	})
	if ec != nil {
		t.Fatalf("AdminEggCreateWithFiles() err = %+v", ec)
	}
	if resp.Version != 1 {
		t.Fatalf("expected version 1, got %d", resp.Version)
	}
	if resp.Status != model.EggStatusPublished {
		t.Fatalf("expected published status, got %s", resp.Status)
	}
	if !resp.Created {
		t.Fatal("expected created=true for new egg")
	}
	if resp.PersonaZipURL == "" || resp.SkillZipURL == "" {
		t.Fatalf("expected both zip urls, got persona=%q skill=%q", resp.PersonaZipURL, resp.SkillZipURL)
	}

	var egg model.Egg
	if err := testDB.DB.First(&egg, "id = ?", "translator-pro").Error; err != nil {
		t.Fatalf("load egg: %v", err)
	}
	if egg.Status != model.EggStatusPublished {
		t.Fatalf("expected egg published, got %s", egg.Status)
	}
	if !egg.HasPersonaZip || !egg.HasSkillZip {
		t.Fatalf("expected egg zip flags true, got persona=%v skill=%v", egg.HasPersonaZip, egg.HasSkillZip)
	}

	var version model.EggVersion
	if err := testDB.DB.First(&version, "egg_id = ? AND version = ?", "translator-pro", 1).Error; err != nil {
		t.Fatalf("load egg version: %v", err)
	}
	if version.PersonaZipURL == "" || version.SkillZipURL == "" {
		t.Fatalf("expected stored zip urls, got persona=%q skill=%q", version.PersonaZipURL, version.SkillZipURL)
	}
	if !strings.Contains(version.PersonaZipURL, "persona.zip") {
		t.Fatalf("expected persona zip path, got %q", version.PersonaZipURL)
	}
	if !strings.Contains(version.SkillZipURL, "skill.zip") {
		t.Fatalf("expected skill zip path, got %q", version.SkillZipURL)
	}
	if strings.Contains(version.PersonaZipURL, "/media/eggs/") {
		t.Fatalf("expected persona zip url without media prefix, got %q", version.PersonaZipURL)
	}
	if !strings.Contains(version.PersonaZipURL, "/eggs/translator-pro/1_persona.zip") {
		t.Fatalf("expected persona zip url under eggs root, got %q", version.PersonaZipURL)
	}
	if strings.Contains(version.SkillZipURL, "/media/eggs/") {
		t.Fatalf("expected skill zip url without media prefix, got %q", version.SkillZipURL)
	}
	if !strings.Contains(version.SkillZipURL, "/eggs/translator-pro/1_skill.zip") {
		t.Fatalf("expected skill zip url under eggs root, got %q", version.SkillZipURL)
	}
}

func TestAdminEggCreateWithFiles_AutoIncrementsVersionForExistingEgg(t *testing.T) {
	testDB, cleanup := setupAdminEggCreateWithFilesTest(t)
	defer cleanup()

	seedAdminEggCategory(t, testDB, "productivity", model.EggCategoryStatusActive, "生产力", "效率工具")

	firstZip := makeMultipartFileHeader(t, "persona_zip", "openclaw.zip", makeTestZipBytes(t, "persona.md", "v1"))
	firstResp, ec := AdminEggCreateWithFiles(AdminEggCreateWithFilesReq{
		Meta: AdminEggCreateWithFilesMeta{
			ID:         "translator-pro",
			CategoryID: "productivity",
			EggI18n: []EggI18nInput{
				{Locale: "zh-CN", Name: "翻译助手"},
			},
			VersionI18n: []EggVersionI18nInput{
				{Locale: "zh-CN", VersionDesc: "v1"},
			},
		},
		PersonaZipFile: firstZip,
	})
	if ec != nil {
		t.Fatalf("first AdminEggCreateWithFiles() err = %+v", ec)
	}
	if firstResp.Version != 1 {
		t.Fatalf("expected first version 1, got %d", firstResp.Version)
	}

	secondZip := makeMultipartFileHeader(t, "persona_zip", "openclaw.zip", makeTestZipBytes(t, "persona.md", "v2"))
	publishNow := true
	secondResp, ec := AdminEggCreateWithFiles(AdminEggCreateWithFilesReq{
		Meta: AdminEggCreateWithFilesMeta{
			ID:         "translator-pro",
			Color:      "#111111",
			PublishNow: &publishNow,
			EggI18n: []EggI18nInput{
				{Locale: "zh-CN", Name: "翻译助手 2"},
			},
			VersionI18n: []EggVersionI18nInput{
				{Locale: "zh-CN", VersionDesc: "v2"},
			},
		},
		PersonaZipFile: secondZip,
	})
	if ec != nil {
		t.Fatalf("second AdminEggCreateWithFiles() err = %+v", ec)
	}
	if secondResp.Version != 2 {
		t.Fatalf("expected second version 2, got %d", secondResp.Version)
	}
	if secondResp.Created {
		t.Fatal("expected created=false for existing egg")
	}

	var versionCount int64
	if err := testDB.DB.Model(&model.EggVersion{}).Where("egg_id = ?", "translator-pro").Count(&versionCount).Error; err != nil {
		t.Fatalf("count egg versions: %v", err)
	}
	if versionCount != 2 {
		t.Fatalf("expected 2 versions, got %d", versionCount)
	}

	var egg model.Egg
	if err := testDB.DB.First(&egg, "id = ?", "translator-pro").Error; err != nil {
		t.Fatalf("load egg: %v", err)
	}
	if egg.DefaultColor != "#111111" {
		t.Fatalf("expected updated color, got %q", egg.DefaultColor)
	}
	if egg.CategoryID != "productivity" {
		t.Fatalf("expected category to stay productivity, got %q", egg.CategoryID)
	}

	var secondVersion model.EggVersion
	if err := testDB.DB.First(&secondVersion, "egg_id = ? AND version = ?", "translator-pro", 2).Error; err != nil {
		t.Fatalf("load second egg version: %v", err)
	}
	if !strings.Contains(secondVersion.PersonaZipURL, "/eggs/translator-pro/2_persona.zip") {
		t.Fatalf("expected versioned persona zip url, got %q", secondVersion.PersonaZipURL)
	}
}

func TestAdminEggCreateWithFiles_AutoMatchesCategoryWhenCategoryIDMissing(t *testing.T) {
	testDB, cleanup := setupAdminEggCreateWithFilesTest(t)
	defer cleanup()

	seedAdminEggCategory(t, testDB, "translation", model.EggCategoryStatusActive, "翻译", "翻译与本地化")
	seedAdminEggCategory(t, testDB, "writing", model.EggCategoryStatusActive, "写作", "写作与创作")

	personaZip := makeMultipartFileHeader(t, "persona_zip", "openclaw.zip", makeTestZipBytes(t, "persona.md", "hello"))

	resp, ec := AdminEggCreateWithFiles(AdminEggCreateWithFilesReq{
		Meta: AdminEggCreateWithFilesMeta{
			ID: "translator-auto",
			EggI18n: []EggI18nInput{
				{Locale: "zh-CN", Name: "翻译助手", Description: "支持多语言翻译与本地化"},
			},
			VersionI18n: []EggVersionI18nInput{
				{Locale: "zh-CN", VersionDesc: "初始版本"},
			},
		},
		PersonaZipFile: personaZip,
	})
	if ec != nil {
		t.Fatalf("AdminEggCreateWithFiles() err = %+v", ec)
	}
	if resp.EggID != "translator-auto" {
		t.Fatalf("expected egg id translator-auto, got %q", resp.EggID)
	}

	var egg model.Egg
	if err := testDB.DB.First(&egg, "id = ?", "translator-auto").Error; err != nil {
		t.Fatalf("load egg: %v", err)
	}
	if egg.CategoryID != "translation" {
		t.Fatalf("expected auto matched category translation, got %q", egg.CategoryID)
	}
}

func TestAdminEggCreateWithFiles_UsesFallbackCategoryWhenCategoryIDMissing(t *testing.T) {
	testDB, cleanup := setupAdminEggCreateWithFilesTest(t)
	defer cleanup()

	personaZip := makeMultipartFileHeader(t, "persona_zip", "openclaw.zip", makeTestZipBytes(t, "persona.md", "hello"))

	_, ec := AdminEggCreateWithFiles(AdminEggCreateWithFilesReq{
		Meta: AdminEggCreateWithFilesMeta{
			ID: "mystery-agent",
			EggI18n: []EggI18nInput{
				{Locale: "zh-CN", Name: "星际编排器", Description: "处理未知领域任务"},
			},
			VersionI18n: []EggVersionI18nInput{
				{Locale: "zh-CN", VersionDesc: "初始版本"},
			},
		},
		PersonaZipFile: personaZip,
	})
	if ec != nil {
		t.Fatalf("AdminEggCreateWithFiles() err = %+v", ec)
	}

	var egg model.Egg
	if err := testDB.DB.First(&egg, "id = ?", "mystery-agent").Error; err != nil {
		t.Fatalf("load egg: %v", err)
	}
	if egg.CategoryID != adminEggFallbackCategoryID {
		t.Fatalf("expected fallback category %q, got %q", adminEggFallbackCategoryID, egg.CategoryID)
	}

	var category model.EggCategory
	if err := testDB.DB.First(&category, "id = ?", adminEggFallbackCategoryID).Error; err != nil {
		t.Fatalf("load fallback category: %v", err)
	}
	if category.Status != model.EggCategoryStatusActive {
		t.Fatalf("expected fallback category active, got %q", category.Status)
	}

	var zh model.EggCategoryI18n
	if err := testDB.DB.First(&zh, "category_id = ? AND locale = ?", adminEggFallbackCategoryID, "zh-CN").Error; err != nil {
		t.Fatalf("load fallback zh i18n: %v", err)
	}
	if zh.Name != "待分类" {
		t.Fatalf("expected fallback zh name 待分类, got %q", zh.Name)
	}
}

func TestAdminEggCreateWithFiles_RequiresAtLeastOneZip(t *testing.T) {
	_, cleanup := setupAdminEggCreateWithFilesTest(t)
	defer cleanup()

	_, ec := AdminEggCreateWithFiles(AdminEggCreateWithFilesReq{
		Meta: AdminEggCreateWithFilesMeta{
			ID: "translator-pro",
			EggI18n: []EggI18nInput{
				{Locale: "zh-CN", Name: "翻译助手"},
			},
			VersionI18n: []EggVersionI18nInput{
				{Locale: "zh-CN", VersionDesc: "v1"},
			},
		},
	})
	if ec == nil {
		t.Fatal("expected error for missing zip files")
	}
	if ec.BizCode != 10003 {
		t.Fatalf("expected biz code 10003, got %+v", ec)
	}
}

func TestAdminEggCreate_InitializesRequiredPackageFields(t *testing.T) {
	testDB, cleanup := setupAdminEggCreateWithFilesTest(t)
	defer cleanup()

	seedAdminEggCategory(t, testDB, "engineering", model.EggCategoryStatusActive, "工程", "工程工具")

	_, ec := AdminEggCreate(AdminEggCreateReq{
		ID:         "api-created-egg",
		CategoryID: "engineering",
		I18n: []EggI18nInput{
			{Locale: "zh-CN", Name: "API 创建虾蛋"},
			{Locale: "en-US", Name: "API Created Egg"},
		},
	})
	if ec != nil {
		t.Fatalf("AdminEggCreate() err = %+v", ec)
	}

	var egg model.Egg
	if err := testDB.DB.First(&egg, "id = ?", "api-created-egg").Error; err != nil {
		t.Fatalf("load egg: %v", err)
	}
	if egg.PackageType != model.EggPackageTypePersonaZip {
		t.Fatalf("expected package_type persona_zip, got %q", egg.PackageType)
	}
	if egg.TargetClientType != model.EggTargetClientTypeOpenClaw {
		t.Fatalf("expected target_client_type openclaw, got %q", egg.TargetClientType)
	}
	if egg.Status != model.EggStatusDraft {
		t.Fatalf("expected draft status, got %q", egg.Status)
	}
}

func TestAdminEggVersionCreate_UpdatesPackageCapabilities(t *testing.T) {
	testDB, cleanup := setupAdminEggCreateWithFilesTest(t)
	defer cleanup()

	seedAdminEggCategory(t, testDB, "engineering", model.EggCategoryStatusActive, "工程", "工程工具")
	_, ec := AdminEggCreate(AdminEggCreateReq{
		ID:         "dual-package-egg",
		CategoryID: "engineering",
		I18n: []EggI18nInput{
			{Locale: "zh-CN", Name: "双包虾蛋"},
		},
	})
	if ec != nil {
		t.Fatalf("AdminEggCreate() err = %+v", ec)
	}

	_, ec = AdminEggVersionCreate("dual-package-egg", AdminEggVersionCreateReq{
		Version:          1,
		PersonaZipURL:    "https://media.example.com/openclaw.zip",
		PersonaZipSHA256: "persona-sha",
		PersonaZipSize:   10,
		SkillZipURL:      "https://media.example.com/skill.zip",
		SkillZipSHA256:   "skill-sha",
		SkillZipSize:     20,
		I18n: []EggVersionI18nInput{
			{Locale: "zh-CN", VersionDesc: "v1"},
		},
	})
	if ec != nil {
		t.Fatalf("AdminEggVersionCreate() err = %+v", ec)
	}

	var egg model.Egg
	if err := testDB.DB.First(&egg, "id = ?", "dual-package-egg").Error; err != nil {
		t.Fatalf("load egg: %v", err)
	}
	if !egg.HasPersonaZip || !egg.HasSkillZip {
		t.Fatalf("expected both package flags true, got persona=%v skill=%v", egg.HasPersonaZip, egg.HasSkillZip)
	}
	if egg.PackageType != model.EggPackageTypePersonaZip {
		t.Fatalf("expected legacy package_type persona_zip, got %q", egg.PackageType)
	}
	if egg.TargetClientType != model.EggTargetClientTypeOpenClaw {
		t.Fatalf("expected legacy target_client_type openclaw, got %q", egg.TargetClientType)
	}
	if egg.SkillTargetType != model.EggTargetClientTypeClaude {
		t.Fatalf("expected skill_target_type claude, got %q", egg.SkillTargetType)
	}
}
