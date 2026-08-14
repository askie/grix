package service

import (
	"net/url"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
)

func TestReconcileEggInstallChatStatusMarksExistingTargetSuccessOnce(t *testing.T) {
	testDB, cleanup := setupEggInstallTest(t)
	defer cleanup()

	const (
		userID          int64 = 7301
		executorAgentID int64 = 94001
		targetAgentID   int64 = 94002
	)

	seedEggInstallUser(t, testDB, userID)
	seedEggInstallAgent(t, testDB, model.Agent{
		ID:              executorAgentID,
		OwnerID:         userID,
		AgentName:       "main-openclaw",
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: model.AgentClientTypeOpenClaw,
		Status:          model.AgentStatusActive,
	})
	seedEggInstallAgent(t, testDB, model.Agent{
		ID:              targetAgentID,
		OwnerID:         userID,
		AgentName:       "travel-claw",
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: model.AgentClientTypeOpenClaw,
		Status:          model.AgentStatusActive,
	})
	seedEggInstallCatalog(t, testDB, "lobster.travel_assistant", model.EggPackageTypePersonaZip, model.EggTargetClientTypeOpenClaw)

	install := model.EggInstall{
		InstallID:       "eggins_success_existing",
		UserID:          userID,
		EggID:           "lobster.travel_assistant",
		Version:         1,
		Status:          model.EggInstallStatusRunning,
		Step:            eggInstallStepChatReady,
		ExecutorAgentID: int64Ptr(executorAgentID),
		SessionID:       "session_install_existing",
		TargetAgentID:   int64Ptr(targetAgentID),
		IdempotencyKey:  "eggins-success-existing",
	}
	if err := testDB.DB.Create(&install).Error; err != nil {
		t.Fatalf("seed install error: %v", err)
	}

	content := buildEggInstallChatContent(t, eggInstallChatStatusSignal{
		InstallID:     install.InstallID,
		Status:        eggInstallChatStatusSuccess,
		Step:          eggInstallStepCompleted,
		TargetAgentID: int64Ptr(targetAgentID),
		Summary:       "已完成安装",
	})
	if err := ReconcileEggInstallChatStatus(install.SessionID, executorAgentID, 2, content); err != nil {
		t.Fatalf("ReconcileEggInstallChatStatus error: %v", err)
	}
	if err := ReconcileEggInstallChatStatus(install.SessionID, executorAgentID, 2, content); err != nil {
		t.Fatalf("ReconcileEggInstallChatStatus second error: %v", err)
	}

	var updated model.EggInstall
	if err := testDB.DB.First(&updated, "install_id = ?", install.InstallID).Error; err != nil {
		t.Fatalf("load install error: %v", err)
	}
	if updated.Status != model.EggInstallStatusSuccess {
		t.Fatalf("status=%q want=%q", updated.Status, model.EggInstallStatusSuccess)
	}
	if updated.Step != eggInstallStepCompleted {
		t.Fatalf("step=%q want=%q", updated.Step, eggInstallStepCompleted)
	}
	if updated.TargetAgentID == nil || *updated.TargetAgentID != targetAgentID {
		t.Fatalf("target_agent_id=%v want=%d", updated.TargetAgentID, targetAgentID)
	}
	if !updated.CounterApplied {
		t.Fatal("expected counter_applied=true")
	}

	var egg model.Egg
	if err := testDB.DB.First(&egg, "id = ?", install.EggID).Error; err != nil {
		t.Fatalf("load egg error: %v", err)
	}
	if egg.InstallCount != 1 {
		t.Fatalf("install_count=%d want=1", egg.InstallCount)
	}
}

func TestReconcileEggInstallChatStatusBackfillsCreateNewTargetOnSuccess(t *testing.T) {
	testDB, cleanup := setupEggInstallTest(t)
	defer cleanup()

	const (
		userID          int64 = 7302
		executorAgentID int64 = 94101
		targetAgentID   int64 = 94102
	)

	seedEggInstallUser(t, testDB, userID)
	seedEggInstallAgent(t, testDB, model.Agent{
		ID:              executorAgentID,
		OwnerID:         userID,
		AgentName:       "main-openclaw",
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: model.AgentClientTypeOpenClaw,
		Status:          model.AgentStatusActive,
	})
	seedEggInstallAgent(t, testDB, model.Agent{
		ID:              targetAgentID,
		OwnerID:         userID,
		AgentName:       "brand-new-openclaw",
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: model.AgentClientTypeOpenClaw,
		Status:          model.AgentStatusActive,
	})
	seedEggInstallCatalog(t, testDB, "lobster.writer_persona", model.EggPackageTypePersonaZip, model.EggTargetClientTypeOpenClaw)

	install := model.EggInstall{
		InstallID:       "eggins_success_create_new",
		UserID:          userID,
		EggID:           "lobster.writer_persona",
		Version:         1,
		Status:          model.EggInstallStatusRunning,
		Step:            eggInstallStepChatReady,
		ExecutorAgentID: int64Ptr(executorAgentID),
		SessionID:       "session_install_create_new",
		IdempotencyKey:  "eggins-success-create-new",
	}
	if err := testDB.DB.Create(&install).Error; err != nil {
		t.Fatalf("seed install error: %v", err)
	}

	content := buildEggInstallChatContent(t, eggInstallChatStatusSignal{
		InstallID:     install.InstallID,
		Status:        eggInstallChatStatusSuccess,
		Step:          eggInstallStepCompleted,
		TargetAgentID: int64Ptr(targetAgentID),
		Summary:       "已新建并完成安装",
	})
	if err := ReconcileEggInstallChatStatus(install.SessionID, executorAgentID, 2, content); err != nil {
		t.Fatalf("ReconcileEggInstallChatStatus error: %v", err)
	}

	var updated model.EggInstall
	if err := testDB.DB.First(&updated, "install_id = ?", install.InstallID).Error; err != nil {
		t.Fatalf("load install error: %v", err)
	}
	if updated.TargetAgentID == nil || *updated.TargetAgentID != targetAgentID {
		t.Fatalf("target_agent_id=%v want=%d", updated.TargetAgentID, targetAgentID)
	}
	if updated.Status != model.EggInstallStatusSuccess {
		t.Fatalf("status=%q want=%q", updated.Status, model.EggInstallStatusSuccess)
	}
}

func TestReconcileEggInstallChatStatusMarksFailure(t *testing.T) {
	testDB, cleanup := setupEggInstallTest(t)
	defer cleanup()

	const (
		userID          int64 = 7303
		executorAgentID int64 = 94201
		targetAgentID   int64 = 94202
	)

	seedEggInstallUser(t, testDB, userID)
	seedEggInstallAgent(t, testDB, model.Agent{
		ID:              executorAgentID,
		OwnerID:         userID,
		AgentName:       "main-openclaw",
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: model.AgentClientTypeOpenClaw,
		Status:          model.AgentStatusActive,
	})
	seedEggInstallAgent(t, testDB, model.Agent{
		ID:              targetAgentID,
		OwnerID:         userID,
		AgentName:       "travel-claw",
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: model.AgentClientTypeOpenClaw,
		Status:          model.AgentStatusActive,
	})
	seedEggInstallCatalog(t, testDB, "lobster.fail_case", model.EggPackageTypePersonaZip, model.EggTargetClientTypeOpenClaw)

	install := model.EggInstall{
		InstallID:       "eggins_failed",
		UserID:          userID,
		EggID:           "lobster.fail_case",
		Version:         1,
		Status:          model.EggInstallStatusRunning,
		Step:            eggInstallStepChatReady,
		ExecutorAgentID: int64Ptr(executorAgentID),
		SessionID:       "session_install_failed",
		TargetAgentID:   int64Ptr(targetAgentID),
		IdempotencyKey:  "eggins-failed",
	}
	if err := testDB.DB.Create(&install).Error; err != nil {
		t.Fatalf("seed install error: %v", err)
	}

	content := buildEggInstallChatContent(t, eggInstallChatStatusSignal{
		InstallID: install.InstallID,
		Status:    eggInstallChatStatusFailed,
		Step:      "download_failed",
		ErrorCode: "download_failed",
		ErrorMsg:  "下载包失败",
	})
	if err := ReconcileEggInstallChatStatus(install.SessionID, executorAgentID, 2, content); err != nil {
		t.Fatalf("ReconcileEggInstallChatStatus error: %v", err)
	}

	var updated model.EggInstall
	if err := testDB.DB.First(&updated, "install_id = ?", install.InstallID).Error; err != nil {
		t.Fatalf("load install error: %v", err)
	}
	if updated.Status != model.EggInstallStatusFailed {
		t.Fatalf("status=%q want=%q", updated.Status, model.EggInstallStatusFailed)
	}
	if updated.ErrorCode != "download_failed" {
		t.Fatalf("error_code=%q want=download_failed", updated.ErrorCode)
	}
	if updated.ErrorMsg != "下载包失败" {
		t.Fatalf("error_msg=%q want=下载包失败", updated.ErrorMsg)
	}

	var egg model.Egg
	if err := testDB.DB.First(&egg, "id = ?", install.EggID).Error; err != nil {
		t.Fatalf("load egg error: %v", err)
	}
	if egg.InstallCount != 0 {
		t.Fatalf("install_count=%d want=0", egg.InstallCount)
	}
}

func TestReconcileEggInstallChatStatusRejectsForeignSuccessTarget(t *testing.T) {
	testDB, cleanup := setupEggInstallTest(t)
	defer cleanup()

	const (
		userID          int64 = 7304
		otherUserID     int64 = 7305
		executorAgentID int64 = 94301
		targetAgentID   int64 = 94302
	)

	seedEggInstallUser(t, testDB, userID)
	seedEggInstallUser(t, testDB, otherUserID)
	seedEggInstallAgent(t, testDB, model.Agent{
		ID:              executorAgentID,
		OwnerID:         userID,
		AgentName:       "main-openclaw",
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: model.AgentClientTypeOpenClaw,
		Status:          model.AgentStatusActive,
	})
	seedEggInstallAgent(t, testDB, model.Agent{
		ID:              targetAgentID,
		OwnerID:         otherUserID,
		AgentName:       "foreign-openclaw",
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: model.AgentClientTypeOpenClaw,
		Status:          model.AgentStatusActive,
	})
	seedEggInstallCatalog(t, testDB, "lobster.foreign_target", model.EggPackageTypePersonaZip, model.EggTargetClientTypeOpenClaw)

	install := model.EggInstall{
		InstallID:       "eggins_foreign_target",
		UserID:          userID,
		EggID:           "lobster.foreign_target",
		Version:         1,
		Status:          model.EggInstallStatusRunning,
		Step:            eggInstallStepChatReady,
		ExecutorAgentID: int64Ptr(executorAgentID),
		SessionID:       "session_install_foreign_target",
		IdempotencyKey:  "eggins-foreign-target",
	}
	if err := testDB.DB.Create(&install).Error; err != nil {
		t.Fatalf("seed install error: %v", err)
	}

	content := buildEggInstallChatContent(t, eggInstallChatStatusSignal{
		InstallID:     install.InstallID,
		Status:        eggInstallChatStatusSuccess,
		Step:          eggInstallStepCompleted,
		TargetAgentID: int64Ptr(targetAgentID),
		Summary:       "已完成安装",
	})
	if err := ReconcileEggInstallChatStatus(install.SessionID, executorAgentID, 2, content); err != nil {
		t.Fatalf("ReconcileEggInstallChatStatus error: %v", err)
	}

	var updated model.EggInstall
	if err := testDB.DB.First(&updated, "install_id = ?", install.InstallID).Error; err != nil {
		t.Fatalf("load install error: %v", err)
	}
	if updated.Status != model.EggInstallStatusFailed {
		t.Fatalf("status=%q want=%q", updated.Status, model.EggInstallStatusFailed)
	}
	if updated.ErrorCode != eggInstallErrorTargetOwnerMismatch {
		t.Fatalf("error_code=%q want=%q", updated.ErrorCode, eggInstallErrorTargetOwnerMismatch)
	}
}

func seedEggInstallCatalog(
	t *testing.T,
	db *testutil.TestDB,
	eggID string,
	packageType string,
	targetClientType string,
) {
	t.Helper()
	if err := db.DB.Create(&model.EggCategory{
		ID:     "assistant",
		Code:   "assistant",
		Status: model.EggCategoryStatusActive,
	}).Error; err != nil {
		t.Fatalf("seed egg category error: %v", err)
	}
	if err := db.DB.Create(&model.Egg{
		ID:           eggID,
		CategoryID:   "assistant",
		DefaultColor: "#D97706",
		DefaultEmoji: "🦞",
		Status:       model.EggStatusPublished,
	}).Error; err != nil {
		t.Fatalf("seed egg error: %v", err)
	}
	if err := db.DB.Create(&model.EggI18n{
		EggID:       eggID,
		Locale:      "en-US",
		Name:        humanizeEggInstallIdentifier(eggID),
		Description: "seed test egg",
		Vibe:        "seed",
	}).Error; err != nil {
		t.Fatalf("seed egg i18n error: %v", err)
	}

	version := model.EggVersion{
		EggID:     eggID,
		Version:   1,
		ZipURL:    "https://example.com/" + eggID + ".zip",
		ZipSHA256: "sha256-demo",
		ZipSize:   1024,
	}
	switch packageType {
	case model.EggPackageTypeSkillZip:
		version.SkillZipURL = "https://example.com/" + eggID + "-skill.zip"
		version.SkillZipSHA256 = "sha256-skill"
		version.SkillZipSize = 1024
	default:
		version.PersonaZipURL = "https://example.com/" + eggID + "-persona.zip"
		version.PersonaZipSHA256 = "sha256-persona"
		version.PersonaZipSize = 1024
	}
	if err := db.DB.Create(&version).Error; err != nil {
		t.Fatalf("seed egg version error: %v", err)
	}

	_ = targetClientType
}

func buildEggInstallChatContent(t *testing.T, signal eggInstallChatStatusSignal) string {
	t.Helper()

	summary := strings.TrimSpace(signal.Summary)
	if summary == "" {
		summary = "Egg install status"
	}

	params := url.Values{}
	params.Set("install_id", signal.InstallID)
	params.Set("status", signal.Status)
	if strings.TrimSpace(signal.Step) != "" {
		params.Set("step", strings.TrimSpace(signal.Step))
	}
	params.Set("summary", summary)
	if strings.TrimSpace(signal.DetailText) != "" {
		params.Set("detail_text", strings.TrimSpace(signal.DetailText))
	}
	if signal.TargetAgentID != nil && *signal.TargetAgentID > 0 {
		params.Set("target_agent_id", stringValue(*signal.TargetAgentID))
	}
	if strings.TrimSpace(signal.ErrorCode) != "" {
		params.Set("error_code", strings.TrimSpace(signal.ErrorCode))
	}
	if strings.TrimSpace(signal.ErrorMsg) != "" {
		params.Set("error_msg", strings.TrimSpace(signal.ErrorMsg))
	}

	return "[" + summary + "](grix://card/egg_install_status?" + params.Encode() + ")"
}

func TestParseEggInstallChatStatusSignalAcceptsStandaloneGrixCard(t *testing.T) {
	content := buildEggInstallChatContent(t, eggInstallChatStatusSignal{
		InstallID: "install-card",
		Status:    eggInstallChatStatusRunning,
		Step:      eggInstallStepChatReady,
		Summary:   "安装进行中",
		ErrorMsg:  "失败原因会编码",
	})

	signal, ok := parseEggInstallChatStatusSignal(content)
	if !ok {
		t.Fatal("parseEggInstallChatStatusSignal should accept standalone grix card")
	}
	if signal.InstallID != "install-card" {
		t.Fatalf("install_id=%q want install-card", signal.InstallID)
	}
	if signal.Status != eggInstallChatStatusRunning {
		t.Fatalf("status=%q want %q", signal.Status, eggInstallChatStatusRunning)
	}
	if signal.Step != eggInstallStepChatReady {
		t.Fatalf("step=%q want %q", signal.Step, eggInstallStepChatReady)
	}
	if signal.Summary != "安装进行中" {
		t.Fatalf("summary=%q want 安装进行中", signal.Summary)
	}
	if signal.ErrorMsg != "失败原因会编码" {
		t.Fatalf("error_msg=%q want 失败原因会编码", signal.ErrorMsg)
	}
}
