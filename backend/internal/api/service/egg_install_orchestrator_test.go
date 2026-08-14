package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/datatypes"
)

func init() {
	_ = snowflake.Init(1)
}

func setupEggInstallTest(t *testing.T) (*testutil.TestDB, func()) {
	t.Helper()
	logger.Init()
	testDB := testutil.NewTestDB()
	prevDB := store.DB
	prevRDB := store.RDB
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	return testDB, func() {
		if store.RDB != nil {
			_ = store.RDB.Close()
		}
		store.DB = prevDB
		store.RDB = prevRDB
		testDB.Close()
	}
}

func seedEggInstallUser(t *testing.T, db *testutil.TestDB, userID int64) {
	t.Helper()
	if err := db.DB.Create(&model.User{
		ID:           userID,
		Username:     fmt.Sprintf("user_%d", userID),
		Email:        fmt.Sprintf("user_%d@example.com", userID),
		PasswordHash: "x",
		AuthProvider: "local",
		Nickname:     fmt.Sprintf("User%d", userID),
	}).Error; err != nil {
		t.Fatalf("seed user error: %v", err)
	}
}

func seedEggInstallAgent(t *testing.T, db *testutil.TestDB, agent model.Agent) {
	t.Helper()
	if err := db.DB.Create(&agent).Error; err != nil {
		t.Fatalf("seed agent error: %v", err)
	}
}

func seedEggInstallAgentScope(t *testing.T, db *testutil.TestDB, agentID int64, scope string) {
	t.Helper()
	if err := db.DB.Create(&model.AgentAPIScope{
		AgentID:   agentID,
		Scope:     scope,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed agent scope error: %v", err)
	}
}

func TestEggInstallLaunchesMainAgentChatAndIsIdempotent(t *testing.T) {
	testDB, cleanup := setupEggInstallTest(t)
	defer cleanup()

	const (
		userID          int64 = 7101
		executorAgentID int64 = 93001
		targetAgentID   int64 = 93002
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
	seedEggInstallAgentScope(t, testDB, executorAgentID, "agent.api.create")
	seedEggInstallAgent(t, testDB, model.Agent{
		ID:              targetAgentID,
		OwnerID:         userID,
		AgentName:       "travel-claw",
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: model.AgentClientTypeOpenClaw,
		Status:          model.AgentStatusActive,
	})
	if err := testDB.DB.Create(&model.UserSetting{
		UserID:              userID,
		AutoDelegateAgentID: int64Ptr(executorAgentID),
		PreferredLanguage:   preferredLanguageEN,
	}).Error; err != nil {
		t.Fatalf("seed user setting error: %v", err)
	}
	ctx := context.Background()
	if err := store.RDB.Set(ctx, fmt.Sprintf("im:agent_api:route:%d", executorAgentID), "node-test", time.Minute).Err(); err != nil {
		t.Fatalf("seed agent route error: %v", err)
	}
	if err := store.RDB.HSet(ctx, fmt.Sprintf("im:ws:route:%d", userID), "device-seed", "node-seed").Err(); err != nil {
		t.Fatalf("seed user ws route error: %v", err)
	}
	pubsub := store.RDB.Subscribe(ctx, "chan:node-seed")
	defer pubsub.Close()

	if err := testDB.DB.Create(&model.EggCategory{ID: "assistant", Code: "assistant", Status: model.EggCategoryStatusActive}).Error; err != nil {
		t.Fatalf("seed egg category error: %v", err)
	}
	if err := testDB.DB.Create(&model.Egg{
		ID:           "lobster.travel_assistant",
		CategoryID:   "assistant",
		DefaultColor: "#D97706",
		DefaultEmoji: "🦞",
		Status:       model.EggStatusPublished,
	}).Error; err != nil {
		t.Fatalf("seed egg error: %v", err)
	}
	if err := testDB.DB.Create(&model.EggI18n{
		EggID:       "lobster.travel_assistant",
		Locale:      "en-US",
		Name:        "Travel Assistant",
		Description: "Travel helper",
		Vibe:        "helpful",
	}).Error; err != nil {
		t.Fatalf("seed egg i18n error: %v", err)
	}
	if err := testDB.DB.Create(&model.EggVersion{
		EggID:                "lobster.travel_assistant",
		Version:              2,
		ZipURL:               "https://example.com/lobster.travel_assistant-v2.zip",
		ZipSHA256:            "abc123",
		ZipSize:              1024,
		PersonaZipURL:        "https://example.com/lobster.travel_assistant-v2.zip",
		PersonaZipSHA256:     "abc123",
		PersonaZipSize:       1024,
		ArtifactManifestJSON: datatypes.JSON([]byte(`{"files":["IDENTITY.md"]}`)),
	}).Error; err != nil {
		t.Fatalf("seed egg version error: %v", err)
	}

	targetID := targetAgentID

	// Phase 1: without ExecutorAgentID → should return choose_executor with 1 candidate.
	chooseResp, chooseEc := EggInstall(userID, EggInstallReq{
		EggID:          "lobster.travel_assistant",
		Version:        2,
		IdempotencyKey: "egg-install-1",
		InstallMode:    eggInstallModeExistingAgent,
		TargetAgentID:  &targetID,
		Locale:         "en-US",
	})
	if chooseEc != nil {
		t.Fatalf("EggInstall choose_executor error: %#v", chooseEc)
	}
	if chooseResp.Status != "choose_executor" {
		t.Fatalf("status=%q want=choose_executor", chooseResp.Status)
	}
	if len(chooseResp.Candidates) != 1 {
		t.Fatalf("candidates=%d want=1", len(chooseResp.Candidates))
	}
	if chooseResp.Candidates[0].AgentID != fmt.Sprintf("%d", executorAgentID) {
		t.Fatalf("candidate agent_id=%q want=%d", chooseResp.Candidates[0].AgentID, executorAgentID)
	}
	if chooseResp.Candidates[0].AgentClientType != model.AgentClientTypeOpenClaw {
		t.Fatalf("candidate agent_client_type=%q want=%q", chooseResp.Candidates[0].AgentClientType, model.AgentClientTypeOpenClaw)
	}

	// Phase 2: with ExecutorAgentID → should proceed normally.
	resp, ec := EggInstall(userID, EggInstallReq{
		EggID:           "lobster.travel_assistant",
		Version:         2,
		IdempotencyKey:  "egg-install-1",
		InstallMode:     eggInstallModeExistingAgent,
		TargetAgentID:   &targetID,
		ExecutorAgentID: int64Ptr(executorAgentID),
		Locale:          "en-US",
	})
	if ec != nil {
		t.Fatalf("EggInstall error: %#v", ec)
	}
	if resp.InstallID == "" {
		t.Fatal("expected install_id")
	}
	if resp.Status != model.EggInstallStatusRunning {
		t.Fatalf("status=%q want=%q", resp.Status, model.EggInstallStatusRunning)
	}
	if resp.SessionID == "" {
		t.Fatal("expected session_id")
	}
	if resp.ExecutorAgentID != fmt.Sprintf("%d", executorAgentID) {
		t.Fatalf("executor_agent_id=%q want=%d", resp.ExecutorAgentID, executorAgentID)
	}

	var install model.EggInstall
	if err := testDB.DB.First(&install, "install_id = ?", resp.InstallID).Error; err != nil {
		t.Fatalf("load install error: %v", err)
	}
	if install.SessionID != resp.SessionID {
		t.Fatalf("install session_id=%q want=%q", install.SessionID, resp.SessionID)
	}
	if install.ExecutorAgentID == nil || *install.ExecutorAgentID != executorAgentID {
		t.Fatalf("executor_agent_id=%v want=%d", install.ExecutorAgentID, executorAgentID)
	}
	if install.TargetAgentID == nil || *install.TargetAgentID != targetAgentID {
		t.Fatalf("target_agent_id=%v want=%d", install.TargetAgentID, targetAgentID)
	}
	if install.Step != eggInstallStepChatReady {
		t.Fatalf("step=%q want=%q", install.Step, eggInstallStepChatReady)
	}

	var session model.Session
	if err := testDB.DB.First(&session, "session_id = ?", resp.SessionID).Error; err != nil {
		t.Fatalf("load session error: %v", err)
	}
	if session.SessionType != model.SessionTypeDirect {
		t.Fatalf("session_type=%d want=%d", session.SessionType, model.SessionTypeDirect)
	}

	var members []model.SessionMember
	if err := testDB.DB.Where("session_id = ?", resp.SessionID).Order("member_type ASC, member_id ASC").Find(&members).Error; err != nil {
		t.Fatalf("load members error: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("members=%d want=2", len(members))
	}

	var messages []model.Message
	if err := testDB.DB.Where("session_id = ?", resp.SessionID).Order("msg_id ASC").Find(&messages).Error; err != nil {
		t.Fatalf("load messages error: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages=%d want=1", len(messages))
	}
	if messages[0].SenderID != userID || messages[0].SenderType != 1 {
		t.Fatalf("unexpected sender=%d sender_type=%d", messages[0].SenderID, messages[0].SenderType)
	}
	expectedVisible := `Request to install Grix egg "Travel Assistant" to agent "travel-claw".`
	if messages[0].Content != expectedVisible {
		t.Fatalf("visible message=%q want=%q", messages[0].Content, expectedVisible)
	}

	var inboxRows []model.UserInbox
	if err := testDB.DB.Where("user_id = ? AND session_id = ?", userID, resp.SessionID).Find(&inboxRows).Error; err != nil {
		t.Fatalf("load inbox rows error: %v", err)
	}
	if len(inboxRows) != 1 || inboxRows[0].MsgID != messages[0].MsgID {
		t.Fatalf("unexpected inbox rows: %#v", inboxRows)
	}

	select {
	case envelope := <-pubsub.Channel():
		var payload struct {
			UserID  int64  `json:"user_id"`
			Cmd     string `json:"cmd"`
			Payload struct {
				InboxSeq    string `json:"inbox_seq"`
				MsgID       string `json:"msg_id"`
				SessionID   string `json:"session_id"`
				SessionType int16  `json:"session_type"`
				SenderID    string `json:"sender_id"`
				SenderType  int16  `json:"sender_type"`
				MsgType     int16  `json:"msg_type"`
				Content     string `json:"content"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(envelope.Payload), &payload); err != nil {
			t.Fatalf("unmarshal seed push message error: %v", err)
		}
		if payload.UserID != userID {
			t.Fatalf("push user_id=%d want=%d", payload.UserID, userID)
		}
		if payload.Cmd != "push_msg" {
			t.Fatalf("push cmd=%s want=push_msg", payload.Cmd)
		}
		if payload.Payload.InboxSeq != fmt.Sprintf("%d", inboxRows[0].InboxSeq) {
			t.Fatalf("push inbox_seq=%s want=%d", payload.Payload.InboxSeq, inboxRows[0].InboxSeq)
		}
		if payload.Payload.MsgID != fmt.Sprintf("%d", messages[0].MsgID) {
			t.Fatalf("push msg_id=%s want=%d", payload.Payload.MsgID, messages[0].MsgID)
		}
		if payload.Payload.SessionID != resp.SessionID {
			t.Fatalf("push session_id=%s want=%s", payload.Payload.SessionID, resp.SessionID)
		}
		if payload.Payload.SessionType != model.SessionTypeDirect {
			t.Fatalf("push session_type=%d want=%d", payload.Payload.SessionType, model.SessionTypeDirect)
		}
		if payload.Payload.SenderID != fmt.Sprintf("%d", userID) {
			t.Fatalf("push sender_id=%s want=%d", payload.Payload.SenderID, userID)
		}
		if payload.Payload.SenderType != 1 {
			t.Fatalf("push sender_type=%d want=1", payload.Payload.SenderType)
		}
		if payload.Payload.MsgType != 1 {
			t.Fatalf("push msg_type=%d want=1", payload.Payload.MsgType)
		}
		if payload.Payload.Content != messages[0].Content {
			t.Fatalf("push content mismatch")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for egg install seed push_msg")
	}

	queued, err := store.RDB.LRange(context.Background(), fmt.Sprintf("im:agent_api:queued_events:%d", executorAgentID), 0, -1).Result()
	if err != nil {
		t.Fatalf("load queued events error: %v", err)
	}
	if len(queued) != 1 {
		t.Fatalf("queued events=%d want=1", len(queued))
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(queued[0]), &event); err != nil {
		t.Fatalf("decode queued event error: %v", err)
	}
	if event["session_id"] != resp.SessionID {
		t.Fatalf("queued session_id=%v want=%s", event["session_id"], resp.SessionID)
	}
	if event["agent_id"] != fmt.Sprintf("%d", executorAgentID) && event["agent_id"] != float64(executorAgentID) {
		t.Fatalf("queued agent_id=%v want=%d", event["agent_id"], executorAgentID)
	}
	eventContent := strings.TrimSpace(fmt.Sprintf("%v", event["content"]))
	if eventContent == messages[0].Content {
		t.Fatal("queued event content should stay internal and differ from visible message")
	}
	if !containsAll(
		eventContent,
		"grix-egg",
		"人格包:",
		"https://example.com/lobster.travel_assistant-v2.zip",
		"install_id:",
		resp.InstallID,
		"grix agent id:",
		"grix wsurl or endpoint:",
		"grix api key:",
		"grix://card/egg_install_status",
		"target_agent_id=<grix agent id>",
	) {
		t.Fatalf("queued event missing required internal context: %s", eventContent)
	}

	resp2, ec := EggInstall(userID, EggInstallReq{
		EggID:          "lobster.travel_assistant",
		Version:        2,
		IdempotencyKey: "egg-install-1",
		InstallMode:    eggInstallModeExistingAgent,
		TargetAgentID:  &targetID,
	})
	if ec != nil {
		t.Fatalf("EggInstall second call error: %#v", ec)
	}
	if resp2.InstallID != resp.InstallID {
		t.Fatalf("install_id second=%q want=%q", resp2.InstallID, resp.InstallID)
	}
	if resp2.SessionID != resp.SessionID {
		t.Fatalf("session_id second=%q want=%q", resp2.SessionID, resp.SessionID)
	}

	var messageCount int64
	if err := testDB.DB.Model(&model.Message{}).Where("session_id = ?", resp.SessionID).Count(&messageCount).Error; err != nil {
		t.Fatalf("count messages error: %v", err)
	}
	if messageCount != 1 {
		t.Fatalf("message_count=%d want=1", messageCount)
	}
}

func TestEggInstallAlwaysCreatesFreshMainAgentChat(t *testing.T) {
	testDB, cleanup := setupEggInstallTest(t)
	defer cleanup()

	const (
		userID          int64 = 7104
		executorAgentID int64 = 93301
		targetAgentID   int64 = 93302
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
	seedEggInstallAgentScope(t, testDB, executorAgentID, "agent.api.create")
	seedEggInstallAgent(t, testDB, model.Agent{
		ID:              targetAgentID,
		OwnerID:         userID,
		AgentName:       "travel-claw",
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: model.AgentClientTypeOpenClaw,
		Status:          model.AgentStatusActive,
	})
	if err := testDB.DB.Create(&model.UserSetting{
		UserID:              userID,
		AutoDelegateAgentID: int64Ptr(executorAgentID),
		PreferredLanguage:   preferredLanguageEN,
	}).Error; err != nil {
		t.Fatalf("seed user setting error: %v", err)
	}
	if err := store.RDB.Set(context.Background(), fmt.Sprintf("im:agent_api:route:%d", executorAgentID), "node-test", time.Minute).Err(); err != nil {
		t.Fatalf("seed agent route error: %v", err)
	}

	seedEggInstallCatalog(t, testDB, "lobster.writer_persona", model.EggPackageTypePersonaZip, model.EggTargetClientTypeOpenClaw)

	existingSession, err := SessionCreate(userID, executorAgentID, 2)
	if err != nil {
		t.Fatalf("create existing session error: %v", err)
	}

	targetID := targetAgentID
	execID := executorAgentID
	resp, ec := EggInstall(userID, EggInstallReq{
		EggID:           "lobster.writer_persona",
		Version:         1,
		IdempotencyKey:  "egg-install-fresh-session-1",
		InstallMode:     eggInstallModeExistingAgent,
		TargetAgentID:   &targetID,
		ExecutorAgentID: &execID,
	})
	if ec != nil {
		t.Fatalf("EggInstall error: %#v", ec)
	}
	if resp.SessionID == existingSession.SessionID {
		t.Fatalf("install session_id=%q should not reuse existing session", resp.SessionID)
	}

	var sameDirectKeyCount int64
	directKey := buildDirectKey(userID, executorAgentID, 2)
	if err := testDB.DB.Model(&model.Session{}).
		Where("direct_key = ? AND session_type = ? AND is_deleted = ?", directKey, model.SessionTypeDirect, false).
		Count(&sameDirectKeyCount).Error; err != nil {
		t.Fatalf("count sessions by direct_key error: %v", err)
	}
	if sameDirectKeyCount < 2 {
		t.Fatalf("expected at least 2 sessions with same direct_key, got %d", sameDirectKeyCount)
	}
}

func TestEggInstallReturnsChooseExecutorWhenMultipleExecutorsEligible(t *testing.T) {
	testDB, cleanup := setupEggInstallTest(t)
	defer cleanup()

	const (
		userID           int64 = 7105
		firstExecutorID  int64 = 93401
		secondExecutorID int64 = 93402
	)

	seedEggInstallUser(t, testDB, userID)
	seedEggInstallAgent(t, testDB, model.Agent{
		ID:              firstExecutorID,
		OwnerID:         userID,
		AgentName:       "main-openclaw-a",
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: model.AgentClientTypeOpenClaw,
		Status:          model.AgentStatusActive,
	})
	seedEggInstallAgentScope(t, testDB, firstExecutorID, "agent.api.create")
	seedEggInstallAgent(t, testDB, model.Agent{
		ID:              secondExecutorID,
		OwnerID:         userID,
		AgentName:       "main-openclaw-b",
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: model.AgentClientTypeOpenClaw,
		Status:          model.AgentStatusActive,
	})
	seedEggInstallAgentScope(t, testDB, secondExecutorID, "agent.api.create")

	for _, agentID := range []int64{firstExecutorID, secondExecutorID} {
		if err := store.RDB.Set(
			context.Background(),
			fmt.Sprintf("im:agent_api:route:%d", agentID),
			"node-test",
			time.Minute,
		).Err(); err != nil {
			t.Fatalf("seed agent route error: %v", err)
		}
	}

	seedEggInstallCatalog(
		t,
		testDB,
		"lobster.multi_executor",
		model.EggPackageTypePersonaZip,
		model.EggTargetClientTypeOpenClaw,
	)

	resp, ec := EggInstall(userID, EggInstallReq{
		EggID:          "lobster.multi_executor",
		Version:        1,
		IdempotencyKey: "egg-install-choose-executor-1",
		InstallMode:    eggInstallModeCreateNew,
	})
	if ec != nil {
		t.Fatalf("EggInstall error: %#v", ec)
	}
	if resp.Status != "choose_executor" {
		t.Fatalf("status=%q want=choose_executor", resp.Status)
	}
	if resp.InstallID != "" {
		t.Fatalf("install_id=%q want empty", resp.InstallID)
	}
	if resp.SessionID != "" {
		t.Fatalf("session_id=%q want empty", resp.SessionID)
	}
	if resp.ExecutorAgentID != "" {
		t.Fatalf("executor_agent_id=%q want empty", resp.ExecutorAgentID)
	}
	if len(resp.Candidates) != 2 {
		t.Fatalf("candidates=%d want=2", len(resp.Candidates))
	}

	candidateNames := map[string]string{}
	candidateTypes := map[string]string{}
	for _, candidate := range resp.Candidates {
		candidateNames[candidate.AgentID] = candidate.AgentName
		candidateTypes[candidate.AgentID] = candidate.AgentClientType
	}
	if candidateNames[fmt.Sprintf("%d", firstExecutorID)] != "main-openclaw-a" {
		t.Fatalf("missing first candidate in response: %#v", resp.Candidates)
	}
	if candidateNames[fmt.Sprintf("%d", secondExecutorID)] != "main-openclaw-b" {
		t.Fatalf("missing second candidate in response: %#v", resp.Candidates)
	}
	if candidateTypes[fmt.Sprintf("%d", firstExecutorID)] != model.AgentClientTypeOpenClaw {
		t.Fatalf("first candidate agent_client_type=%q want=%q", candidateTypes[fmt.Sprintf("%d", firstExecutorID)], model.AgentClientTypeOpenClaw)
	}
	if candidateTypes[fmt.Sprintf("%d", secondExecutorID)] != model.AgentClientTypeOpenClaw {
		t.Fatalf("second candidate agent_client_type=%q want=%q", candidateTypes[fmt.Sprintf("%d", secondExecutorID)], model.AgentClientTypeOpenClaw)
	}

	var installCount int64
	if err := testDB.DB.Model(&model.EggInstall{}).Count(&installCount).Error; err != nil {
		t.Fatalf("count installs error: %v", err)
	}
	if installCount != 0 {
		t.Fatalf("install_count=%d want=0", installCount)
	}

	var sessionCount int64
	if err := testDB.DB.Model(&model.Session{}).Count(&sessionCount).Error; err != nil {
		t.Fatalf("count sessions error: %v", err)
	}
	if sessionCount != 0 {
		t.Fatalf("session_count=%d want=0", sessionCount)
	}

	var messageCount int64
	if err := testDB.DB.Model(&model.Message{}).Count(&messageCount).Error; err != nil {
		t.Fatalf("count messages error: %v", err)
	}
	if messageCount != 0 {
		t.Fatalf("message_count=%d want=0", messageCount)
	}
}

func TestEggInstallUsesRequestedExecutorAgent(t *testing.T) {
	testDB, cleanup := setupEggInstallTest(t)
	defer cleanup()

	const (
		userID              int64 = 7106
		preferredExecutorID int64 = 93501
		otherExecutorID     int64 = 93502
	)

	seedEggInstallUser(t, testDB, userID)
	seedEggInstallAgent(t, testDB, model.Agent{
		ID:              preferredExecutorID,
		OwnerID:         userID,
		AgentName:       "preferred-openclaw",
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: model.AgentClientTypeOpenClaw,
		Status:          model.AgentStatusActive,
	})
	seedEggInstallAgentScope(t, testDB, preferredExecutorID, "agent.api.create")
	seedEggInstallAgent(t, testDB, model.Agent{
		ID:              otherExecutorID,
		OwnerID:         userID,
		AgentName:       "other-openclaw",
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: model.AgentClientTypeOpenClaw,
		Status:          model.AgentStatusActive,
	})
	seedEggInstallAgentScope(t, testDB, otherExecutorID, "agent.api.create")

	for _, agentID := range []int64{preferredExecutorID, otherExecutorID} {
		if err := store.RDB.Set(
			context.Background(),
			fmt.Sprintf("im:agent_api:route:%d", agentID),
			"node-test",
			time.Minute,
		).Err(); err != nil {
			t.Fatalf("seed agent route error: %v", err)
		}
	}

	seedEggInstallCatalog(
		t,
		testDB,
		"lobster.preferred_executor",
		model.EggPackageTypePersonaZip,
		model.EggTargetClientTypeOpenClaw,
	)

	selectedExecutorID := preferredExecutorID
	resp, ec := EggInstall(userID, EggInstallReq{
		EggID:           "lobster.preferred_executor",
		Version:         1,
		IdempotencyKey:  "egg-install-preferred-executor-1",
		InstallMode:     eggInstallModeCreateNew,
		ExecutorAgentID: &selectedExecutorID,
	})
	if ec != nil {
		t.Fatalf("EggInstall error: %#v", ec)
	}
	if resp.Status != model.EggInstallStatusRunning {
		t.Fatalf("status=%q want=%q", resp.Status, model.EggInstallStatusRunning)
	}
	if resp.ExecutorAgentID != fmt.Sprintf("%d", preferredExecutorID) {
		t.Fatalf("executor_agent_id=%q want=%d", resp.ExecutorAgentID, preferredExecutorID)
	}
	if resp.SessionID == "" {
		t.Fatal("expected session_id")
	}

	var install model.EggInstall
	if err := testDB.DB.First(&install, "install_id = ?", resp.InstallID).Error; err != nil {
		t.Fatalf("load install error: %v", err)
	}
	if install.ExecutorAgentID == nil || *install.ExecutorAgentID != preferredExecutorID {
		t.Fatalf("executor_agent_id=%v want=%d", install.ExecutorAgentID, preferredExecutorID)
	}

	preferredQueued, err := store.RDB.LRange(
		context.Background(),
		fmt.Sprintf("im:agent_api:queued_events:%d", preferredExecutorID),
		0,
		-1,
	).Result()
	if err != nil {
		t.Fatalf("load preferred queued events error: %v", err)
	}
	if len(preferredQueued) != 1 {
		t.Fatalf("preferred queued events=%d want=1", len(preferredQueued))
	}

	otherQueued, err := store.RDB.LRange(
		context.Background(),
		fmt.Sprintf("im:agent_api:queued_events:%d", otherExecutorID),
		0,
		-1,
	).Result()
	if err != nil {
		t.Fatalf("load other queued events error: %v", err)
	}
	if len(otherQueued) != 0 {
		t.Fatalf("other queued events=%d want=0", len(otherQueued))
	}
}

func TestEggInstallRejectsRequestedExecutorWithoutCreateScope(t *testing.T) {
	testDB, cleanup := setupEggInstallTest(t)
	defer cleanup()

	const (
		userID     int64 = 7107
		executorID int64 = 93601
	)

	seedEggInstallUser(t, testDB, userID)
	seedEggInstallAgent(t, testDB, model.Agent{
		ID:              executorID,
		OwnerID:         userID,
		AgentName:       "scope-missing-openclaw",
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: model.AgentClientTypeOpenClaw,
		Status:          model.AgentStatusActive,
	})
	if err := store.RDB.Set(
		context.Background(),
		fmt.Sprintf("im:agent_api:route:%d", executorID),
		"node-test",
		time.Minute,
	).Err(); err != nil {
		t.Fatalf("seed agent route error: %v", err)
	}

	seedEggInstallCatalog(
		t,
		testDB,
		"lobster.scope_missing",
		model.EggPackageTypePersonaZip,
		model.EggTargetClientTypeOpenClaw,
	)

	selectedExecutorID := executorID
	_, ec := EggInstall(userID, EggInstallReq{
		EggID:           "lobster.scope_missing",
		Version:         1,
		IdempotencyKey:  "egg-install-scope-missing-1",
		InstallMode:     eggInstallModeCreateNew,
		ExecutorAgentID: &selectedExecutorID,
	})
	if ec == nil {
		t.Fatal("expected error")
	}
	if ec.HTTPStatus != 403 {
		t.Fatalf("http_status=%d want=403", ec.HTTPStatus)
	}
	if ec.BizCode != 10002 {
		t.Fatalf("biz_code=%d want=10002", ec.BizCode)
	}

	var installCount int64
	if err := testDB.DB.Model(&model.EggInstall{}).Count(&installCount).Error; err != nil {
		t.Fatalf("count installs error: %v", err)
	}
	if installCount != 0 {
		t.Fatalf("install_count=%d want=0", installCount)
	}
}

func TestEggInstallCreateNewFlowReachesSuccessAfterMainAgentStatus(t *testing.T) {
	testDB, cleanup := setupEggInstallTest(t)
	defer cleanup()

	const (
		userID          int64 = 7102
		executorAgentID int64 = 93101
		targetAgentID   int64 = 93102
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
	seedEggInstallAgentScope(t, testDB, executorAgentID, "agent.api.create")
	if err := testDB.DB.Create(&model.UserSetting{
		UserID:              userID,
		AutoDelegateAgentID: int64Ptr(executorAgentID),
		PreferredLanguage:   preferredLanguageEN,
	}).Error; err != nil {
		t.Fatalf("seed user setting error: %v", err)
	}
	if err := store.RDB.Set(context.Background(), fmt.Sprintf("im:agent_api:route:%d", executorAgentID), "node-test", time.Minute).Err(); err != nil {
		t.Fatalf("seed agent route error: %v", err)
	}

	seedEggInstallCatalog(t, testDB, "lobster.writer_persona", model.EggPackageTypePersonaZip, model.EggTargetClientTypeOpenClaw)

	execID := executorAgentID
	resp, ec := EggInstall(userID, EggInstallReq{
		EggID:           "lobster.writer_persona",
		Version:         1,
		IdempotencyKey:  "egg-install-create-new-1",
		InstallMode:     eggInstallModeCreateNew,
		ExecutorAgentID: &execID,
	})
	if ec != nil {
		t.Fatalf("EggInstall error: %#v", ec)
	}
	if resp.Status != model.EggInstallStatusRunning {
		t.Fatalf("status=%q want=%q", resp.Status, model.EggInstallStatusRunning)
	}
	if resp.ExecutorAgentID != fmt.Sprintf("%d", executorAgentID) {
		t.Fatalf("executor_agent_id=%q want=%d", resp.ExecutorAgentID, executorAgentID)
	}

	var messages []model.Message
	if err := testDB.DB.Where("session_id = ?", resp.SessionID).Order("msg_id ASC").Find(&messages).Error; err != nil {
		t.Fatalf("load messages error: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages=%d want=1", len(messages))
	}
	expectedVisible := fmt.Sprintf(
		"Request to incubate Grix egg \"%s\".",
		humanizeEggInstallIdentifier("lobster.writer_persona"),
	)
	if messages[0].Content != expectedVisible {
		t.Fatalf("visible create-new message=%q want=%q", messages[0].Content, expectedVisible)
	}

	queued, err := store.RDB.LRange(context.Background(), fmt.Sprintf("im:agent_api:queued_events:%d", executorAgentID), 0, -1).Result()
	if err != nil {
		t.Fatalf("load queued events error: %v", err)
	}
	if len(queued) != 1 {
		t.Fatalf("queued events=%d want=1", len(queued))
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(queued[0]), &event); err != nil {
		t.Fatalf("decode queued event error: %v", err)
	}
	eventContent := strings.TrimSpace(fmt.Sprintf("%v", event["content"]))
	if !containsAll(
		eventContent,
		"grix-egg",
		"人格包:",
		"https://example.com/lobster.writer_persona-persona.zip",
		"install_id:",
		resp.InstallID,
		"agent名字:",
		"新agent名字:",
		humanizeEggInstallIdentifier("lobster.writer_persona"),
		"grix://card/egg_install_status",
		"target_agent_id=<新建agent id>",
	) {
		t.Fatalf("queued create-new event missing required internal context: %s", eventContent)
	}

	seedEggInstallAgent(t, testDB, model.Agent{
		ID:              targetAgentID,
		OwnerID:         userID,
		AgentName:       "writer-openclaw",
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: model.AgentClientTypeOpenClaw,
		Status:          model.AgentStatusActive,
	})

	content := buildEggInstallChatContent(t, eggInstallChatStatusSignal{
		InstallID:     resp.InstallID,
		Status:        eggInstallChatStatusSuccess,
		Step:          eggInstallStepCompleted,
		TargetAgentID: int64Ptr(targetAgentID),
		Summary:       "已新建并完成安装",
	})
	if err := ReconcileEggInstallChatStatus(resp.SessionID, executorAgentID, 2, content); err != nil {
		t.Fatalf("ReconcileEggInstallChatStatus error: %v", err)
	}

	var install model.EggInstall
	if err := testDB.DB.First(&install, "install_id = ?", resp.InstallID).Error; err != nil {
		t.Fatalf("load install error: %v", err)
	}
	if install.Status != model.EggInstallStatusSuccess {
		t.Fatalf("status=%q want=%q", install.Status, model.EggInstallStatusSuccess)
	}
	if install.Step != eggInstallStepCompleted {
		t.Fatalf("step=%q want=%q", install.Step, eggInstallStepCompleted)
	}
	if install.TargetAgentID == nil || *install.TargetAgentID != targetAgentID {
		t.Fatalf("target_agent_id=%v want=%d", install.TargetAgentID, targetAgentID)
	}
	if !install.CounterApplied {
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

func TestEggInstallExistingClaudeRouteSeedsSkillPackageContext(t *testing.T) {
	testDB, cleanup := setupEggInstallTest(t)
	defer cleanup()

	const (
		userID          int64 = 7103
		executorAgentID int64 = 93201
		targetAgentID   int64 = 93202
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
	seedEggInstallAgentScope(t, testDB, executorAgentID, "agent.api.create")
	seedEggInstallAgent(t, testDB, model.Agent{
		ID:              targetAgentID,
		OwnerID:         userID,
		AgentName:       "claude-target",
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: model.AgentClientTypeClaude,
		Status:          model.AgentStatusActive,
	})
	if err := testDB.DB.Create(&model.UserSetting{UserID: userID, AutoDelegateAgentID: int64Ptr(executorAgentID)}).Error; err != nil {
		t.Fatalf("seed user setting error: %v", err)
	}
	if err := store.RDB.Set(context.Background(), fmt.Sprintf("im:agent_api:route:%d", executorAgentID), "node-test", time.Minute).Err(); err != nil {
		t.Fatalf("seed agent route error: %v", err)
	}
	// 技能包路由目标 agent 自身执行安装，目标必须在线。
	if err := store.RDB.Set(context.Background(), fmt.Sprintf("im:agent_api:route:%d", targetAgentID), "node-test", time.Minute).Err(); err != nil {
		t.Fatalf("seed target agent route error: %v", err)
	}

	if err := testDB.DB.Create(&model.EggCategory{ID: "assistant", Code: "assistant", Status: model.EggCategoryStatusActive}).Error; err != nil {
		t.Fatalf("seed egg category error: %v", err)
	}
	if err := testDB.DB.Create(&model.Egg{
		ID:           "lobster.hybrid_agent",
		CategoryID:   "assistant",
		DefaultColor: "#D97706",
		DefaultEmoji: "🦞",
		Status:       model.EggStatusPublished,
	}).Error; err != nil {
		t.Fatalf("seed egg error: %v", err)
	}
	if err := testDB.DB.Create(&model.EggVersion{
		EggID:                "lobster.hybrid_agent",
		Version:              1,
		ZipURL:               "https://example.com/lobster.hybrid_agent-v1-persona.zip",
		ZipSHA256:            "persona123",
		ZipSize:              1024,
		PersonaZipURL:        "https://example.com/lobster.hybrid_agent-v1-persona.zip",
		PersonaZipSHA256:     "persona123",
		PersonaZipSize:       1024,
		SkillZipURL:          "https://example.com/lobster.hybrid_agent-v1-skill.zip",
		SkillZipSHA256:       "skill123",
		SkillZipSize:         2048,
		ArtifactManifestJSON: datatypes.JSON([]byte(`{"files":["IDENTITY.md","skill.json"]}`)),
	}).Error; err != nil {
		t.Fatalf("seed egg version error: %v", err)
	}

	targetID := targetAgentID
	execID := executorAgentID
	resp, ec := EggInstall(userID, EggInstallReq{
		EggID:           "lobster.hybrid_agent",
		Version:         1,
		IdempotencyKey:  "egg-install-claude-route-1",
		InstallMode:     eggInstallModeExistingAgent,
		TargetAgentID:   &targetID,
		ExecutorAgentID: &execID,
	})
	if ec != nil {
		t.Fatalf("EggInstall error: %#v", ec)
	}
	if resp.Status != model.EggInstallStatusRunning {
		t.Fatalf("status=%q want=%q", resp.Status, model.EggInstallStatusRunning)
	}

	var messages []model.Message
	if err := testDB.DB.Where("session_id = ?", resp.SessionID).Order("msg_id ASC").Find(&messages).Error; err != nil {
		t.Fatalf("load messages error: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages=%d want=1", len(messages))
	}
	expectedVisible := fmt.Sprintf(
		"请求安装Grix虾蛋《%s》到 Agent「claude-target」。",
		humanizeEggInstallIdentifier("lobster.hybrid_agent"),
	)
	if messages[0].Content != expectedVisible {
		t.Fatalf("visible claude-route message=%q want=%q", messages[0].Content, expectedVisible)
	}

	// 技能包路由目标 agent 自身执行，安装事件应派发给目标，而非主 OpenClaw 执行者。
	if execQueued, err := store.RDB.LRange(context.Background(), fmt.Sprintf("im:agent_api:queued_events:%d", executorAgentID), 0, -1).Result(); err != nil {
		t.Fatalf("load executor queued events error: %v", err)
	} else if len(execQueued) != 0 {
		t.Fatalf("executor queued events=%d want=0 (skill route should target self)", len(execQueued))
	}
	queued, err := store.RDB.LRange(context.Background(), fmt.Sprintf("im:agent_api:queued_events:%d", targetAgentID), 0, -1).Result()
	if err != nil {
		t.Fatalf("load queued events error: %v", err)
	}
	if len(queued) != 1 {
		t.Fatalf("queued events=%d want=1", len(queued))
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(queued[0]), &event); err != nil {
		t.Fatalf("decode queued event error: %v", err)
	}
	eventContent := strings.TrimSpace(fmt.Sprintf("%v", event["content"]))
	if !containsAll(
		eventContent,
		"项目级技能",
		"install_scope: project",
		"skill_root_hint: <project>/.claude/skills/",
		"skill_layout: <skill_root>/<skill_directory>/SKILL.md",
		"skill_directory_rule: equals SKILL.md frontmatter name",
		"文件名必须是大写 SKILL.md",
		"<skill_directory> 必须等于 SKILL.md frontmatter 里的技能 name",
		"技能名里有空格是允许的",
		"download_url:",
		"https://example.com/lobster.hybrid_agent-v1-skill.zip",
		"install_id:",
		"egg_id: lobster.hybrid_agent",
		fmt.Sprintf("grix_agent_id: %d", targetAgentID),
	) {
		t.Fatalf("queued claude-route event missing self-install context: %s", eventContent)
	}
	if strings.Contains(eventContent, "~/.claude/skills/") {
		t.Fatalf("queued claude-route event should prefer project-level skill root, got: %s", eventContent)
	}
}

func TestResolveSkillDirHintAndScopeMatchConnectorRoots(t *testing.T) {
	cases := []struct {
		clientType string
		want       string
		wantScope  string
	}{
		{clientType: model.AgentClientTypeClaude, want: "<project>/.claude/skills/", wantScope: "project"},
		{clientType: model.AgentClientTypeCodex, want: "<project>/.codex/skills/", wantScope: "project"},
		{clientType: model.AgentClientTypeGemini, want: "<project>/.gemini/skills/", wantScope: "project"},
		{clientType: model.AgentClientTypeQwen, want: "<project>/.qwen/skills/", wantScope: "project"},
		{clientType: model.AgentClientTypeKiro, want: "<project>/.kiro/skills/", wantScope: "project"},
		{clientType: model.AgentClientTypeKimi, want: "~/.kimi-code/skills/", wantScope: "user"},
	}

	for _, tt := range cases {
		t.Run(tt.clientType, func(t *testing.T) {
			target := &eggInstallTargetSnapshot{AgentClientType: tt.clientType}
			got := resolveSkillDirHint(target)
			if got != tt.want {
				t.Fatalf("skill dir hint=%q want=%q", got, tt.want)
			}
			if gotScope := resolveSkillInstallScope(target); gotScope != tt.wantScope {
				t.Fatalf("skill install scope=%q want=%q", gotScope, tt.wantScope)
			}
		})
	}
}

func TestBuildEggInstallVisibleRequestMessageLocalized(t *testing.T) {
	cases := []struct {
		name        string
		eggName     string
		installMode string
		target      *eggInstallTargetSnapshot
		lang        string
		expected    string
	}{
		{
			name:        "zh create new",
			eggName:     "写作助手",
			installMode: eggInstallModeCreateNew,
			lang:        "zh-CN",
			expected:    "请求孵化Grix虾蛋《写作助手》",
		},
		{
			name:        "en create new",
			eggName:     "Writer Persona",
			installMode: eggInstallModeCreateNew,
			lang:        "en-US",
			expected:    `Request to incubate Grix egg "Writer Persona".`,
		},
		{
			name:        "zh claude existing",
			eggName:     "混合助手",
			installMode: eggInstallModeExistingAgent,
			target: &eggInstallTargetSnapshot{
				AgentName:       "claude-target",
				AgentClientType: model.AgentClientTypeClaude,
			},
			lang:     "zh-CN",
			expected: "请求安装Grix虾蛋《混合助手》到 Agent「claude-target」。",
		},
		{
			name:        "en claude existing",
			eggName:     "Hybrid Agent",
			installMode: eggInstallModeExistingAgent,
			target: &eggInstallTargetSnapshot{
				AgentName:       "claude-target",
				AgentClientType: model.AgentClientTypeClaude,
			},
			lang:     "en-US",
			expected: `Request to install Grix egg "Hybrid Agent" to agent "claude-target".`,
		},
		{
			name:        "en empty egg name fallback",
			eggName:     "  ",
			installMode: eggInstallModeCreateNew,
			lang:        "en-US",
			expected:    `Request to incubate Grix egg "Unnamed Egg".`,
		},
		{
			name:        "zh empty egg name fallback",
			eggName:     "  ",
			installMode: eggInstallModeCreateNew,
			lang:        "zh-CN",
			expected:    "请求孵化Grix虾蛋《未命名虾蛋》",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := buildEggInstallVisibleRequestMessage(tt.eggName, tt.installMode, tt.target, tt.lang)
			if got != tt.expected {
				t.Fatalf("message=%q want=%q", got, tt.expected)
			}
		})
	}
}

func int64Ptr(value int64) *int64 {
	return &value
}

func containsAll(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if needle == "" {
			continue
		}
		if !strings.Contains(haystack, needle) {
			return false
		}
	}
	return true
}
