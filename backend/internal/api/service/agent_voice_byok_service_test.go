package service

import (
	"testing"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/secretcrypto"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func setupVoiceAgentTest(t *testing.T, ownerID int64) func() {
	t.Helper()
	config.C.Server.VoiceCryptoSecret = "voice-agent-svc-test-secret"
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("init snowflake: %v", err)
	}
	seedAgentCreateMainOwner(t, ownerID)
	return func() { testDB.Close() }
}

func TestAgentCreate_VoiceBYOK_EncryptsKeyAndHidesPlaintext(t *testing.T) {
	const ownerID = int64(94001)
	defer setupVoiceAgentTest(t, ownerID)()

	plainKey := "sk-user-voice-key-7788"
	resp, ec := AgentCreate(ownerID, AgentCreateReq{
		AgentName:     "my-voice-bot",
		ProviderType:  model.AgentProviderVoice,
		SystemPrompt:  "你是电话秘书",
		VoiceProvider: "openai_realtime",
		VoiceModel:    "gpt-4o-realtime-preview",
		VoiceEndpoint: "wss://api.openai.com/v1/realtime",
		VoiceID:       "alloy",
		VoiceAPIKey:   plainKey,
	})
	if ec != nil {
		t.Fatalf("create voice agent: %+v", ec)
	}
	if resp.ProviderType != model.AgentProviderVoice {
		t.Fatalf("provider_type = %d, want 4", resp.ProviderType)
	}
	if resp.MediaCapability != model.AgentMediaCapabilityVoice {
		t.Fatalf("media_capability = %q, want voice", resp.MediaCapability)
	}
	if resp.SystemPrompt != "你是电话秘书" {
		t.Fatalf("system_prompt should be kept, got %q", resp.SystemPrompt)
	}
	if resp.VoiceAPIKeyHint != "7788" {
		t.Fatalf("hint = %q, want 7788", resp.VoiceAPIKeyHint)
	}

	// 库内必须是密文，且能解密回明文
	var row model.Agent
	if err := store.DB.First(&row, resp.ID).Error; err != nil {
		t.Fatalf("read agent: %v", err)
	}
	if row.VoiceAPIKeyCipher == "" || row.VoiceAPIKeyCipher == plainKey {
		t.Fatalf("cipher must be non-empty and not plaintext, got %q", row.VoiceAPIKeyCipher)
	}
	got, err := secretcrypto.Decrypt(row.VoiceAPIKeyCipher)
	if err != nil || got != plainKey {
		t.Fatalf("decrypt mismatch: got %q err %v", got, err)
	}
	if row.APIKeyHash != "" {
		t.Fatalf("voice agent must not generate agent-api key, got hash %q", row.APIKeyHash)
	}
}

func TestAgentCreate_VoiceWelcomeI18n_NormalizedAndRoundTrips(t *testing.T) {
	const ownerID = int64(94010)
	defer setupVoiceAgentTest(t, ownerID)()

	resp, ec := AgentCreate(ownerID, AgentCreateReq{
		AgentName:     "voice-welcome",
		ProviderType:  model.AgentProviderVoice,
		VoiceProvider: "openai_realtime",
		VoiceModel:    "gpt-4o-realtime-preview",
		VoiceAPIKey:   "sk-user-voice-key-welcome",
		VoiceWelcomeI18n: map[string]string{
			"zh-CN": "您好，请问有什么可以帮您？",
			"en_US": "Hi, how can I help?",
			"xx":    "unknown locale must be dropped, never collapse onto en_US",
		},
	})
	if ec != nil {
		t.Fatalf("create voice agent: %+v", ec)
	}
	if resp.VoiceWelcomeI18n["zh_CN"] != "您好，请问有什么可以帮您？" {
		t.Fatalf("zh-CN should normalize key to zh_CN, got %+v", resp.VoiceWelcomeI18n)
	}
	// 未知语言 key（xx）被丢弃，绝不能归一到 en_US 覆盖真实 en_US 值。
	if resp.VoiceWelcomeI18n["en_US"] != "Hi, how can I help?" {
		t.Fatalf("en_US must keep its own value, unknown key must not overwrite it, got %+v", resp.VoiceWelcomeI18n)
	}
	if len(resp.VoiceWelcomeI18n) != 2 {
		t.Fatalf("expect exactly zh_CN + en_US, unknown key dropped, got %+v", resp.VoiceWelcomeI18n)
	}

	// 更新：追加一个语言，验证归一化 key 且旧值保留
	updated, ec := AgentUpdate(ownerID, resp.ID, AgentUpdateReq{
		VoiceWelcomeI18n: &map[string]string{
			"zh_CN": "您好，请问有什么可以帮您？",
			"ja-JP": "こんにちは、ご用件をどうぞ",
		},
	})
	if ec != nil {
		t.Fatalf("update voice agent: %+v", ec)
	}
	if updated.VoiceWelcomeI18n["ja_JP"] != "こんにちは、ご用件をどうぞ" {
		t.Fatalf("ja-JP should normalize key to ja_JP, got %+v", updated.VoiceWelcomeI18n)
	}
}

func TestAgentCreate_VoiceBYOK_RequiresProviderModelKey(t *testing.T) {
	const ownerID = int64(94002)
	defer setupVoiceAgentTest(t, ownerID)()

	cases := []AgentCreateReq{
		{AgentName: "v1", ProviderType: model.AgentProviderVoice, VoiceModel: "m", VoiceAPIKey: "k"}, // 缺 provider
		{AgentName: "v2", ProviderType: model.AgentProviderVoice, VoiceProvider: "openai_realtime", VoiceAPIKey: "k"}, // 缺 model
		{AgentName: "v3", ProviderType: model.AgentProviderVoice, VoiceProvider: "openai_realtime", VoiceModel: "m"}, // 缺 key
	}
	for i, c := range cases {
		if _, ec := AgentCreate(ownerID, c); ec == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
}

func TestAgentCreate_VoiceBYOK_RejectsUnsupportedProvider(t *testing.T) {
	const ownerID = int64(94009)
	defer setupVoiceAgentTest(t, ownerID)()

	_, ec := AgentCreate(ownerID, AgentCreateReq{
		AgentName:     "bad-provider",
		ProviderType:  model.AgentProviderVoice,
		VoiceProvider: "unsupported_realtime",
		VoiceModel:    "unsupported-model",
		VoiceAPIKey:   "k",
	})
	if ec == nil {
		t.Fatal("expected unsupported provider to be rejected")
	}
}

func TestAgentUpdate_VoiceBYOK_KeyEmptyKeepsOld(t *testing.T) {
	const ownerID = int64(94003)
	defer setupVoiceAgentTest(t, ownerID)()

	resp, ec := AgentCreate(ownerID, AgentCreateReq{
		AgentName:     "voice-upd",
		ProviderType:  model.AgentProviderVoice,
		VoiceProvider: "openai_realtime",
		VoiceModel:    "gpt-4o-realtime-preview",
		VoiceAPIKey:   "sk-original-0001",
	})
	if ec != nil {
		t.Fatalf("create: %+v", ec)
	}
	var before model.Agent
	store.DB.First(&before, resp.ID)

	// 改 model，key 留空（nil）应保持原密文
	newModel := "gpt-4o-realtime-2025"
	emptyKey := ""
	if _, ec := AgentUpdate(ownerID, resp.ID, AgentUpdateReq{VoiceModel: &newModel, VoiceAPIKey: &emptyKey}); ec != nil {
		t.Fatalf("update keep key: %+v", ec)
	}
	var afterKeep model.Agent
	store.DB.First(&afterKeep, resp.ID)
	if afterKeep.VoiceModel != newModel {
		t.Fatalf("model not updated: %q", afterKeep.VoiceModel)
	}
	if afterKeep.VoiceAPIKeyCipher != before.VoiceAPIKeyCipher {
		t.Fatal("empty key should keep original cipher")
	}

	// 提供新 key 应重新加密
	newKey := "sk-rotated-9999"
	if _, ec := AgentUpdate(ownerID, resp.ID, AgentUpdateReq{VoiceAPIKey: &newKey}); ec != nil {
		t.Fatalf("update rotate key: %+v", ec)
	}
	var afterRotate model.Agent
	store.DB.First(&afterRotate, resp.ID)
	if afterRotate.VoiceAPIKeyCipher == before.VoiceAPIKeyCipher {
		t.Fatal("new key should re-encrypt cipher")
	}
	got, _ := secretcrypto.Decrypt(afterRotate.VoiceAPIKeyCipher)
	if got != newKey || afterRotate.VoiceAPIKeyHint != "9999" {
		t.Fatalf("rotated key mismatch: got %q hint %q", got, afterRotate.VoiceAPIKeyHint)
	}
}
