package service

import (
	"sync"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

// recordingBridge 捕获 PushDelegateEvent 调用，用于断言投递的 event payload。
type recordingBridge struct {
	mu     sync.Mutex
	events []AgentDelegateEvent
}

func (r *recordingBridge) PushAgentEvent(_, _ int64, _ string, _ interface{}) bool { return true }
func (r *recordingBridge) PushDelegateEvent(event AgentDelegateEvent) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
	return true
}
func (r *recordingBridge) IsAgentChannelAvailable(_ int64) bool { return true }
func (r *recordingBridge) GetAgentClientType(_ int64) string    { return "" }

func setupProfileSelfUpdateTest(t *testing.T) (*recordingBridge, func()) {
	t.Helper()
	logger.Init()
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("init snowflake: %v", err)
	}
	testDB := testutil.NewTestDB()
	prevDB := store.DB
	prevRDB := store.RDB
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	bridge := &recordingBridge{}
	SetAgentChannelBridge(bridge)
	return bridge, func() {
		SetAgentChannelBridge(nil)
		store.DB = prevDB
		store.RDB = prevRDB
		testDB.Close()
	}
}

// 给 SessionCreate 准备最小数据：owner + agent。SessionCreate 内部会查/建 sessions 行。
func seedProfileSelfUpdateActors(t *testing.T, ownerID, agentID int64, clientType string) {
	t.Helper()
	owner := model.User{ID: ownerID}
	if err := store.DB.Create(&owner).Error; err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	agent := model.Agent{
		ID:              agentID,
		OwnerID:         ownerID,
		AgentName:       "旧名字",
		Introduction:    "旧介绍",
		AgentClientType: clientType,
		ProviderType:    model.AgentProviderAPI,
		Status:          1,
	}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}
}

func TestIsFileBasedPersonaAgent(t *testing.T) {
	cases := map[string]bool{
		model.AgentClientTypeOpenClaw:  true,
		model.AgentClientTypeHermes:    true,
		model.AgentClientTypeClaude:    false,
		model.AgentClientTypeCodex:     false,
		model.AgentClientTypeGemini:    false,
		model.AgentClientTypeAgy:       false,
		"":                             false,
		"OPENCLAW":                     true,  // 大小写归一
		"  hermes  ":                   true,  // 前后空格归一
	}
	for input, want := range cases {
		if got := isFileBasedPersonaAgent(input); got != want {
			t.Errorf("isFileBasedPersonaAgent(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestBuildProfileUpdateInstruction_NameOnly(t *testing.T) {
	out := buildProfileUpdateInstruction("old", "new", "intro-same", "intro-same")
	if !contains(out, "[system-profile-update]") || !contains(out, "[/system-profile-update]") {
		t.Errorf("missing wrapping tags:\n%s", out)
	}
	if !contains(out, `名字：旧值="old" → 新值="new"`) {
		t.Errorf("missing name diff line:\n%s", out)
	}
	if contains(out, "介绍：旧值") {
		t.Errorf("intro diff should be omitted when unchanged:\n%s", out)
	}
	if !contains(out, "不要向用户发送任何文本回复") {
		t.Errorf("missing no-reply instruction:\n%s", out)
	}
}

func TestBuildProfileUpdateInstruction_IntroOnly(t *testing.T) {
	out := buildProfileUpdateInstruction("same", "same", "old-intro", "new-intro")
	if contains(out, "名字：旧值") {
		t.Errorf("name diff should be omitted when unchanged:\n%s", out)
	}
	if !contains(out, `介绍：旧值="old-intro" → 新值="new-intro"`) {
		t.Errorf("missing intro diff line:\n%s", out)
	}
}

func TestBuildProfileUpdateInstruction_Both(t *testing.T) {
	out := buildProfileUpdateInstruction("n1", "n2", "i1", "i2")
	if !contains(out, `名字：旧值="n1" → 新值="n2"`) || !contains(out, `介绍：旧值="i1" → 新值="i2"`) {
		t.Errorf("both lines expected:\n%s", out)
	}
}

func TestNotifyFileBasedAgentProfileChange_SkipsNonFileBasedTypes(t *testing.T) {
	bridge, cleanup := setupProfileSelfUpdateTest(t)
	defer cleanup()

	agent := &model.Agent{ID: 1, OwnerID: 100, AgentClientType: model.AgentClientTypeClaude}
	notifyFileBasedAgentProfileChange(agent, "old", "new", "old", "new")

	if len(bridge.events) != 0 {
		t.Errorf("expected no push for claude, got %d events", len(bridge.events))
	}
}

func TestNotifyFileBasedAgentProfileChange_SkipsWhenUnchanged(t *testing.T) {
	bridge, cleanup := setupProfileSelfUpdateTest(t)
	defer cleanup()

	agent := &model.Agent{ID: 1, OwnerID: 100, AgentClientType: model.AgentClientTypeOpenClaw}
	notifyFileBasedAgentProfileChange(agent, "same", "same", "same", "same")

	if len(bridge.events) != 0 {
		t.Errorf("expected no push when no diff, got %d events", len(bridge.events))
	}
}

func TestNotifyFileBasedAgentProfileChange_PushesForOpenClaw(t *testing.T) {
	bridge, cleanup := setupProfileSelfUpdateTest(t)
	defer cleanup()

	ownerID := int64(200001)
	agentID := int64(200002)
	seedProfileSelfUpdateActors(t, ownerID, agentID, model.AgentClientTypeOpenClaw)

	var agent model.Agent
	if err := store.DB.First(&agent, agentID).Error; err != nil {
		t.Fatalf("load agent: %v", err)
	}
	notifyFileBasedAgentProfileChange(&agent, "old-name", "new-name", "old-intro", "new-intro")

	if len(bridge.events) != 1 {
		t.Fatalf("expected 1 push, got %d", len(bridge.events))
	}
	evt := bridge.events[0]
	if evt.AgentID != agentID || evt.OwnerID != ownerID {
		t.Errorf("event addressing wrong: agentID=%d ownerID=%d", evt.AgentID, evt.OwnerID)
	}
	if evt.EventType != "user_chat" {
		t.Errorf("expected EventType=user_chat, got %q", evt.EventType)
	}
	if len(evt.EventID) < len("profile-update:") || evt.EventID[:len("profile-update:")] != "profile-update:" {
		t.Errorf("expected profile-update event ID, got %q", evt.EventID)
	}
	if evt.SenderID != ownerID {
		t.Errorf("expected SenderID=ownerID (so agent sees it as from its owner), got %d", evt.SenderID)
	}
	if !contains(evt.Content, "[system-profile-update]") {
		t.Errorf("content missing wrapping tag:\n%s", evt.Content)
	}
	if !contains(evt.Content, `名字：旧值="old-name" → 新值="new-name"`) {
		t.Errorf("content missing name diff:\n%s", evt.Content)
	}
	if !contains(evt.Content, `介绍：旧值="old-intro" → 新值="new-intro"`) {
		t.Errorf("content missing intro diff:\n%s", evt.Content)
	}
}

func TestNotifyFileBasedAgentProfileChange_PushesForHermes(t *testing.T) {
	bridge, cleanup := setupProfileSelfUpdateTest(t)
	defer cleanup()

	ownerID := int64(300001)
	agentID := int64(300002)
	seedProfileSelfUpdateActors(t, ownerID, agentID, model.AgentClientTypeHermes)

	var agent model.Agent
	if err := store.DB.First(&agent, agentID).Error; err != nil {
		t.Fatalf("load agent: %v", err)
	}
	notifyFileBasedAgentProfileChange(&agent, "n1", "n2", "i1", "i1")

	if len(bridge.events) != 1 {
		t.Fatalf("expected 1 push for hermes, got %d", len(bridge.events))
	}
	if contains(bridge.events[0].Content, "介绍：旧值") {
		t.Errorf("intro line should be omitted when intro unchanged:\n%s", bridge.events[0].Content)
	}
}

// 集成路径：从 AgentUpdate 入口改 name/introduction，验证文件型 agent 触发自更新指令；
// 非文件型 agent（claude）即使改了同样字段也不应触发（避免误伤）。
func TestAgentUpdate_TriggersSelfUpdateForOpenClawOnly(t *testing.T) {
	bridge, cleanup := setupProfileSelfUpdateTest(t)
	defer cleanup()

	ownerID := int64(400001)
	openclawID := int64(400002)
	claudeID := int64(400003)
	seedProfileSelfUpdateActors(t, ownerID, openclawID, model.AgentClientTypeOpenClaw)
	// 主动建 claude 同 owner 另一个 agent
	claudeAgent := model.Agent{
		ID:              claudeID,
		OwnerID:         ownerID,
		AgentName:       "claude-旧",
		Introduction:    "claude-旧介绍",
		AgentClientType: model.AgentClientTypeClaude,
		ProviderType:    model.AgentProviderAPI,
		Status:          1,
	}
	if err := store.DB.Create(&claudeAgent).Error; err != nil {
		t.Fatalf("seed claude agent: %v", err)
	}

	// 1) 更新 claude 的 name + introduction —— 不应触发 push
	newName1 := "claude-新"
	newIntro1 := "claude-新介绍"
	if _, ec := AgentUpdate(ownerID, claudeID, AgentUpdateReq{AgentName: &newName1, Introduction: &newIntro1}); ec != nil {
		t.Fatalf("AgentUpdate(claude) error: %+v", ec)
	}
	if len(bridge.events) != 0 {
		t.Errorf("claude update should NOT trigger self-update, got %d events", len(bridge.events))
	}

	// 2) 更新 openclaw 的 name + introduction —— 应触发 1 条 push
	newName2 := "openclaw-新名字"
	newIntro2 := "openclaw-新介绍"
	if _, ec := AgentUpdate(ownerID, openclawID, AgentUpdateReq{AgentName: &newName2, Introduction: &newIntro2}); ec != nil {
		t.Fatalf("AgentUpdate(openclaw) error: %+v", ec)
	}
	if len(bridge.events) != 1 {
		t.Fatalf("openclaw update should trigger 1 push, got %d", len(bridge.events))
	}
	evt := bridge.events[0]
	if evt.AgentID != openclawID {
		t.Errorf("event targets wrong agent: got %d, want %d", evt.AgentID, openclawID)
	}
	if !contains(evt.Content, `名字：旧值="旧名字" → 新值="openclaw-新名字"`) {
		t.Errorf("name diff missing/wrong in content:\n%s", evt.Content)
	}
	if !contains(evt.Content, `介绍：旧值="旧介绍" → 新值="openclaw-新介绍"`) {
		t.Errorf("intro diff missing/wrong in content:\n%s", evt.Content)
	}
}

// 集成路径：仅改 name（不改 introduction）—— 应只发一条，且 diff 只有名字行
func TestAgentUpdate_NameOnlyDiffForOpenClaw(t *testing.T) {
	bridge, cleanup := setupProfileSelfUpdateTest(t)
	defer cleanup()

	ownerID := int64(500001)
	agentID := int64(500002)
	seedProfileSelfUpdateActors(t, ownerID, agentID, model.AgentClientTypeOpenClaw)

	newName := "只改名字"
	if _, ec := AgentUpdate(ownerID, agentID, AgentUpdateReq{AgentName: &newName}); ec != nil {
		t.Fatalf("AgentUpdate error: %+v", ec)
	}
	if len(bridge.events) != 1 {
		t.Fatalf("expected 1 push, got %d", len(bridge.events))
	}
	evt := bridge.events[0]
	if !contains(evt.Content, `名字：旧值="旧名字" → 新值="只改名字"`) {
		t.Errorf("name diff expected:\n%s", evt.Content)
	}
	if contains(evt.Content, "介绍：旧值") {
		t.Errorf("intro line should be absent when intro unchanged:\n%s", evt.Content)
	}
}

// 集成路径：改 name/intro 之外的字段（例如 sort_order）—— 不应触发 push
func TestAgentUpdate_OtherFieldsDoNotTriggerSelfUpdate(t *testing.T) {
	bridge, cleanup := setupProfileSelfUpdateTest(t)
	defer cleanup()

	ownerID := int64(600001)
	agentID := int64(600002)
	seedProfileSelfUpdateActors(t, ownerID, agentID, model.AgentClientTypeOpenClaw)

	newSort := 42
	if _, ec := AgentUpdate(ownerID, agentID, AgentUpdateReq{SortOrder: &newSort}); ec != nil {
		t.Fatalf("AgentUpdate error: %+v", ec)
	}
	if len(bridge.events) != 0 {
		t.Errorf("non-profile field change should NOT trigger self-update, got %d events", len(bridge.events))
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
