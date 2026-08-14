package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
)

func TestEggSearchSchedulesAndPersistsMissingLocaleTranslations(t *testing.T) {
	testDB, userID := setupEggSearchTestDB(t)
	seedSearchCategory(t, testDB, "persona")
	seedSearchEgg(
		t,
		testDB,
		seedSearchEggParams{
			ID:           "lobster.translate_test",
			CategoryID:   "persona",
			Locale:       "en-US",
			Name:         "Shrimp Helper",
			Description:  "OpenClaw persona helper",
			Vibe:         "Execution",
			SearchText:   buildEggSearchText("Shrimp Helper", "OpenClaw persona helper", "Execution"),
			InstallCount: 5,
		},
	)
	if err := testDB.DB.Model(&model.EggVersionI18n{}).
		Where("egg_id = ? AND version = ? AND locale = ?", "lobster.translate_test", 1, "en-US").
		Update("version_desc", "Initial release").Error; err != nil {
		t.Fatalf("update version desc error: %v", err)
	}

	prevEggTranslator := eggI18nLLMTranslator
	prevVersionTranslator := eggVersionI18nLLMTranslator
	eggI18nLLMTranslator = func(ctx context.Context, source model.EggI18n, targetLocale string) (EggI18nInput, error) {
		return EggI18nInput{
			Name:        "助手 crevette",
			Description: "助手说明",
			Vibe:        "执行",
		}, nil
	}
	eggVersionI18nLLMTranslator = func(ctx context.Context, source model.EggVersionI18n, targetLocale string) (EggVersionI18nInput, error) {
		return EggVersionI18nInput{VersionDesc: "首次发布"}, nil
	}
	t.Cleanup(func() {
		eggI18nLLMTranslator = prevEggTranslator
		eggVersionI18nLLMTranslator = prevVersionTranslator
	})

	resp, ec := EggSearch(userID, EggSearchReq{
		Keyword:  "shrimp",
		Locale:   "fr-FR",
		Page:     1,
		PageSize: 10,
	})
	if ec != nil {
		t.Fatalf("EggSearch error: %#v", ec)
	}
	if len(resp.List) != 1 {
		t.Fatalf("list len=%d want=1", len(resp.List))
	}
	if got := resp.List[0].Name; got != "Shrimp Helper" {
		t.Fatalf("first search name=%q want English fallback", got)
	}

	assertPendingEggTranslationJobs(t, testDB, 2)

	processed, err := processEggTranslationDueBatch(10)
	if err != nil {
		t.Fatalf("processEggTranslationDueBatch error: %v", err)
	}
	if processed != 2 {
		t.Fatalf("processed=%d want=2", processed)
	}

	var eggText model.EggI18n
	if err := testDB.DB.Where("egg_id = ? AND locale = ?", "lobster.translate_test", "fr-FR").Take(&eggText).Error; err != nil {
		t.Fatalf("load translated egg i18n error: %v", err)
	}
	if eggText.Name != "助手 crevette" {
		t.Fatalf("translated egg name=%q want=%q", eggText.Name, "助手 crevette")
	}
	if eggText.SearchTextNormalized == "" {
		t.Fatal("translated egg search_text_normalized should not be empty")
	}

	var versionText model.EggVersionI18n
	if err := testDB.DB.Where("egg_id = ? AND version = ? AND locale = ?", "lobster.translate_test", 1, "fr-FR").Take(&versionText).Error; err != nil {
		t.Fatalf("load translated version i18n error: %v", err)
	}
	if versionText.VersionDesc != "首次发布" {
		t.Fatalf("translated version desc=%q want=%q", versionText.VersionDesc, "首次发布")
	}

	resp, ec = EggSearch(userID, EggSearchReq{
		Keyword:  "shrimp",
		Locale:   "fr-FR",
		Page:     1,
		PageSize: 10,
	})
	if ec != nil {
		t.Fatalf("EggSearch second call error: %#v", ec)
	}
	if len(resp.List) != 1 {
		t.Fatalf("second search list len=%d want=1", len(resp.List))
	}
	if got := resp.List[0].Name; got != "助手 crevette" {
		t.Fatalf("second search name=%q want translated value", got)
	}
	if got := resp.List[0].VersionDesc; got != "首次发布" {
		t.Fatalf("second search version desc=%q want translated value", got)
	}

	resp, ec = EggSearch(userID, EggSearchReq{
		Keyword:  "crevette",
		Locale:   "fr-FR",
		Page:     1,
		PageSize: 10,
	})
	if ec != nil {
		t.Fatalf("EggSearch translated-name keyword error: %#v", ec)
	}
	if len(resp.List) != 1 {
		t.Fatalf("translated-name search list len=%d want=1", len(resp.List))
	}
	if got := resp.List[0].Name; got != "助手 crevette" {
		t.Fatalf("translated-name search result name=%q want translated value", got)
	}

	assertPendingEggTranslationJobs(t, testDB, 0)
}

func TestEggSearchRequeuesTranslationsWhenEnglishSourceChanges(t *testing.T) {
	testDB, userID := setupEggSearchTestDB(t)
	seedSearchCategory(t, testDB, "persona")
	seedSearchEgg(
		t,
		testDB,
		seedSearchEggParams{
			ID:           "lobster.translate_refresh",
			CategoryID:   "persona",
			Locale:       "en-US",
			Name:         "Shrimp Helper",
			Description:  "OpenClaw persona helper",
			Vibe:         "Execution",
			SearchText:   buildEggSearchText("Shrimp Helper", "OpenClaw persona helper", "Execution"),
			InstallCount: 5,
		},
	)
	if err := testDB.DB.Model(&model.EggVersionI18n{}).
		Where("egg_id = ? AND version = ? AND locale = ?", "lobster.translate_refresh", 1, "en-US").
		Update("version_desc", "Initial release").Error; err != nil {
		t.Fatalf("update version desc error: %v", err)
	}

	prevEggTranslator := eggI18nLLMTranslator
	prevVersionTranslator := eggVersionI18nLLMTranslator
	eggI18nLLMTranslator = func(ctx context.Context, source model.EggI18n, targetLocale string) (EggI18nInput, error) {
		return EggI18nInput{
			Name:        "FR:" + source.Name,
			Description: "FR:" + source.Description,
			Vibe:        "FR:" + source.Vibe,
		}, nil
	}
	eggVersionI18nLLMTranslator = func(ctx context.Context, source model.EggVersionI18n, targetLocale string) (EggVersionI18nInput, error) {
		return EggVersionI18nInput{VersionDesc: "FR:" + source.VersionDesc}, nil
	}
	t.Cleanup(func() {
		eggI18nLLMTranslator = prevEggTranslator
		eggVersionI18nLLMTranslator = prevVersionTranslator
	})

	if _, ec := EggSearch(userID, EggSearchReq{
		Keyword:  "shrimp",
		Locale:   "fr-FR",
		Page:     1,
		PageSize: 10,
	}); ec != nil {
		t.Fatalf("initial EggSearch error: %#v", ec)
	}
	if _, err := processEggTranslationDueBatch(10); err != nil {
		t.Fatalf("process initial translation batch error: %v", err)
	}

	sourceUpdatedAt := time.Now().UTC().Add(2 * time.Second)
	if err := testDB.DB.Model(&model.EggI18n{}).
		Where("egg_id = ? AND locale = ?", "lobster.translate_refresh", "en-US").
		Updates(map[string]any{
			"name":                   "Shrimp Helper 2",
			"description":            "Updated helper copy",
			"vibe":                   "Automation",
			"search_text_normalized": buildEggSearchText("Shrimp Helper 2", "Updated helper copy", "Automation"),
			"updated_at":             sourceUpdatedAt,
		}).Error; err != nil {
		t.Fatalf("update source egg i18n error: %v", err)
	}
	if err := testDB.DB.Model(&model.EggVersionI18n{}).
		Where("egg_id = ? AND version = ? AND locale = ?", "lobster.translate_refresh", 1, "en-US").
		Updates(map[string]any{
			"version_desc": "Second release",
			"updated_at":   sourceUpdatedAt,
		}).Error; err != nil {
		t.Fatalf("update source version i18n error: %v", err)
	}

	resp, ec := EggSearch(userID, EggSearchReq{
		Keyword:  "shrimp",
		Locale:   "fr-FR",
		Page:     1,
		PageSize: 10,
	})
	if ec != nil {
		t.Fatalf("refresh EggSearch error: %#v", ec)
	}
	if got := resp.List[0].Name; got != "FR:Shrimp Helper" {
		t.Fatalf("refresh search name=%q want old translated value before async refresh", got)
	}
	if got := resp.List[0].VersionDesc; got != "FR:Initial release" {
		t.Fatalf("refresh search version_desc=%q want old translated value before async refresh", got)
	}

	assertPendingEggTranslationJobs(t, testDB, 2)

	if _, err := processEggTranslationDueBatch(10); err != nil {
		t.Fatalf("process refresh translation batch error: %v", err)
	}

	resp, ec = EggSearch(userID, EggSearchReq{
		Keyword:  "shrimp",
		Locale:   "fr-FR",
		Page:     1,
		PageSize: 10,
	})
	if ec != nil {
		t.Fatalf("post-refresh EggSearch error: %#v", ec)
	}
	if got := resp.List[0].Name; got != "FR:Shrimp Helper 2" {
		t.Fatalf("post-refresh search name=%q want refreshed translated value", got)
	}
	if got := resp.List[0].VersionDesc; got != "FR:Second release" {
		t.Fatalf("post-refresh search version_desc=%q want refreshed translated value", got)
	}
}

func TestEggTranslationSkipsSupersededInFlightResult(t *testing.T) {
	testDB, _ := setupEggSearchTestDB(t)
	seedSearchCategory(t, testDB, "persona")
	seedSearchEgg(
		t,
		testDB,
		seedSearchEggParams{
			ID:           "lobster.translate_superseded",
			CategoryID:   "persona",
			Locale:       "en-US",
			Name:         "Shrimp Helper",
			Description:  "OpenClaw persona helper",
			Vibe:         "Execution",
			SearchText:   buildEggSearchText("Shrimp Helper", "OpenClaw persona helper", "Execution"),
			InstallCount: 5,
		},
	)

	prevEggTranslator := eggI18nLLMTranslator
	prevVersionTranslator := eggVersionI18nLLMTranslator
	eggVersionI18nLLMTranslator = func(ctx context.Context, source model.EggVersionI18n, targetLocale string) (EggVersionI18nInput, error) {
		return EggVersionI18nInput{VersionDesc: "unused"}, nil
	}
	eggI18nLLMTranslator = func(ctx context.Context, source model.EggI18n, targetLocale string) (EggI18nInput, error) {
		sourceUpdatedAt := time.Now().UTC().Add(2 * time.Second)
		if err := testDB.DB.Model(&model.EggI18n{}).
			Where("egg_id = ? AND locale = ?", source.EggID, source.Locale).
			Updates(map[string]any{
				"name":                   "Shrimp Helper Updated",
				"description":            "Updated helper copy",
				"vibe":                   "Automation",
				"search_text_normalized": buildEggSearchText("Shrimp Helper Updated", "Updated helper copy", "Automation"),
				"updated_at":             sourceUpdatedAt,
			}).Error; err != nil {
			t.Fatalf("update source during translation error: %v", err)
		}
		if err := enqueueEggI18nTranslationJob(source.EggID, targetLocale); err != nil {
			t.Fatalf("enqueue superseding translation job error: %v", err)
		}
		return EggI18nInput{
			Name:        "FR:" + source.Name,
			Description: "FR:" + source.Description,
			Vibe:        "FR:" + source.Vibe,
		}, nil
	}
	t.Cleanup(func() {
		eggI18nLLMTranslator = prevEggTranslator
		eggVersionI18nLLMTranslator = prevVersionTranslator
	})

	if err := enqueueEggI18nTranslationJob("lobster.translate_superseded", "fr-FR"); err != nil {
		t.Fatalf("enqueue initial translation job error: %v", err)
	}

	job, err := claimNextEggTranslationJob(time.Now().UTC())
	if err != nil {
		t.Fatalf("claimNextEggTranslationJob error: %v", err)
	}
	if job == nil {
		t.Fatal("expected claimed translation job")
	}
	if err := executeEggTranslationJob(context.Background(), job); err != nil {
		t.Fatalf("executeEggTranslationJob error: %v", err)
	}

	var staleResult model.EggI18n
	if err := testDB.DB.Where("egg_id = ? AND locale = ?", "lobster.translate_superseded", "fr-FR").Take(&staleResult).Error; err == nil {
		t.Fatalf("expected superseded translation result to be skipped, got row=%+v", staleResult)
	}

	currentJob := loadEggTranslationJobForTest(t, testDB, model.EggTranslationJobTypeEggI18n, "lobster.translate_superseded", 0, "fr-FR")
	if currentJob.Status != model.EggTranslationJobStatusPending {
		t.Fatalf("current job status=%d want pending", currentJob.Status)
	}

	eggI18nLLMTranslator = func(ctx context.Context, source model.EggI18n, targetLocale string) (EggI18nInput, error) {
		return EggI18nInput{
			Name:        "FR:" + source.Name,
			Description: "FR:" + source.Description,
			Vibe:        "FR:" + source.Vibe,
		}, nil
	}

	if _, err := processEggTranslationDueBatch(10); err != nil {
		t.Fatalf("process superseding translation batch error: %v", err)
	}

	var refreshed model.EggI18n
	if err := testDB.DB.Where("egg_id = ? AND locale = ?", "lobster.translate_superseded", "fr-FR").Take(&refreshed).Error; err != nil {
		t.Fatalf("load refreshed translation error: %v", err)
	}
	if refreshed.Name != "FR:Shrimp Helper Updated" {
		t.Fatalf("refreshed translation name=%q want updated source translation", refreshed.Name)
	}
}

func TestEggTranslationFailedJobDoesNotRequeueSameSource(t *testing.T) {
	testDB, _ := setupEggSearchTestDB(t)
	seedSearchCategory(t, testDB, "persona")
	seedSearchEgg(
		t,
		testDB,
		seedSearchEggParams{
			ID:           "lobster.translate_failed",
			CategoryID:   "persona",
			Locale:       "en-US",
			Name:         "Shrimp Helper",
			Description:  "OpenClaw persona helper",
			Vibe:         "Execution",
			SearchText:   buildEggSearchText("Shrimp Helper", "OpenClaw persona helper", "Execution"),
			InstallCount: 5,
		},
	)

	source, err := loadEggI18nTranslationSource("lobster.translate_failed")
	if err != nil {
		t.Fatalf("load source egg i18n error: %v", err)
	}
	job := model.EggTranslationJob{
		JobType:         model.EggTranslationJobTypeEggI18n,
		EggID:           "lobster.translate_failed",
		Version:         0,
		SourceLocale:    source.Locale,
		SourceUpdatedAt: normalizeEggTranslationTime(source.UpdatedAt),
		TargetLocale:    "fr-FR",
		Status:          model.EggTranslationJobStatusFailed,
		AttemptCount:    eggTranslationMaxAttempts,
		MaxAttempts:     eggTranslationMaxAttempts,
		NextRetryAt:     time.Now().UTC(),
		LastError:       "boom",
	}
	if err := testDB.DB.Create(&job).Error; err != nil {
		t.Fatalf("create failed translation job error: %v", err)
	}

	if err := enqueueEggI18nTranslationJob("lobster.translate_failed", "fr-FR"); err != nil {
		t.Fatalf("enqueue failed translation job error: %v", err)
	}

	refreshed := loadEggTranslationJobForTest(t, testDB, model.EggTranslationJobTypeEggI18n, "lobster.translate_failed", 0, "fr-FR")
	if refreshed.Status != model.EggTranslationJobStatusFailed {
		t.Fatalf("failed job status=%d want=%d", refreshed.Status, model.EggTranslationJobStatusFailed)
	}
	if refreshed.AttemptCount != eggTranslationMaxAttempts {
		t.Fatalf("failed job attempt_count=%d want=%d", refreshed.AttemptCount, eggTranslationMaxAttempts)
	}
	assertPendingEggTranslationJobs(t, testDB, 0)
}

func TestBuildEggI18nTranslationInstructionsTranslateDisplayName(t *testing.T) {
	instructions := buildEggI18nTranslationInstructions("en-US", "zh-CN")
	if !strings.Contains(instructions, "Translate the name field too") {
		t.Fatalf("instructions=%q want explicit name translation guidance", instructions)
	}
	if strings.Contains(instructions, "Keep product names") {
		t.Fatalf("instructions=%q should not force product names to stay unchanged", instructions)
	}
}

func assertPendingEggTranslationJobs(t *testing.T, testDB *testutil.TestDB, want int64) {
	t.Helper()
	var pendingCount int64
	if err := testDB.DB.Model(&model.EggTranslationJob{}).
		Where("status = ?", model.EggTranslationJobStatusPending).
		Count(&pendingCount).Error; err != nil {
		t.Fatalf("count pending translation jobs error: %v", err)
	}
	if pendingCount != want {
		var jobs []model.EggTranslationJob
		if err := testDB.DB.Order("id ASC").Find(&jobs).Error; err != nil {
			t.Fatalf("pending translation job count=%d want=%d (and load jobs error: %v)", pendingCount, want, err)
		}
		t.Fatalf("pending translation job count=%d want=%d jobs=%+v", pendingCount, want, jobs)
	}
}

func loadEggTranslationJobForTest(
	t *testing.T,
	testDB *testutil.TestDB,
	jobType string,
	eggID string,
	version int,
	targetLocale string,
) model.EggTranslationJob {
	t.Helper()
	var job model.EggTranslationJob
	if err := testDB.DB.
		Where("job_type = ? AND egg_id = ? AND version = ? AND target_locale = ?", jobType, eggID, version, targetLocale).
		Take(&job).Error; err != nil {
		t.Fatalf("load translation job error: %v", err)
	}
	return job
}
