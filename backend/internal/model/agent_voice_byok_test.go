package model_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
)

// VoiceAPIKeyCipher 必须永不出现在 JSON 序列化结果中（json:"-"）。
func TestAgentVoiceCipherNotSerialized(t *testing.T) {
	ag := model.Agent{
		ID:                1,
		AgentName:         "voice-bot",
		ProviderType:      model.AgentProviderVoice,
		MediaCapability:   model.AgentMediaCapabilityVoice,
		VoiceProvider:     "openai_realtime",
		VoiceModel:        "gpt-4o-realtime-preview",
		VoiceEndpoint:     "wss://api.openai.com/v1/realtime",
		VoiceAPIKeyCipher: "opaque",
		VoiceAPIKeyHint:   "1234",
	}
	raw, err := json.Marshal(ag)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	if strings.Contains(s, "opaque") {
		t.Fatal("voice_api_key_cipher must not appear in JSON")
	}
	if !strings.Contains(s, "\"voice_api_key_hint\":\"1234\"") {
		t.Fatalf("voice_api_key_hint should be serialized, got %s", s)
	}
	if !strings.Contains(s, "\"voice_model\":\"gpt-4o-realtime-preview\"") {
		t.Fatalf("voice_model should be serialized, got %s", s)
	}
}

// type=4 + voice 字段可写入并读回。
func TestAgentVoiceFieldsRoundTrip(t *testing.T) {
	tdb := testutil.NewTestDB()
	defer tdb.Close()

	in := model.Agent{
		ID:                100,
		AgentName:         "voice-bot",
		OwnerID:           7,
		ProviderType:      model.AgentProviderVoice,
		MediaCapability:   model.AgentMediaCapabilityVoice,
		VoiceProvider:     "openai_realtime",
		VoiceModel:        "gpt-4o-realtime-preview",
		VoiceEndpoint:     "wss://example.com/realtime",
		VoiceAPIKeyCipher: "ZmFrZS1jaXBoZXI",
		VoiceAPIKeyHint:   "abcd",
	}
	if err := tdb.DB.Create(&in).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	var out model.Agent
	if err := tdb.DB.First(&out, 100).Error; err != nil {
		t.Fatalf("read: %v", err)
	}
	if out.ProviderType != model.AgentProviderVoice ||
		out.VoiceModel != in.VoiceModel ||
		out.VoiceEndpoint != in.VoiceEndpoint ||
		out.VoiceAPIKeyCipher != in.VoiceAPIKeyCipher ||
		out.VoiceAPIKeyHint != in.VoiceAPIKeyHint {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
}
