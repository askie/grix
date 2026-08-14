package callbridge

import (
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/internal/call"
)

// start 消息体必须携带 per-agent 的 BYOK 配置（provider/model/endpoint/api_key 等）。
func TestBuildStartPayload_CarriesBYOKFields(t *testing.T) {
	spec := call.VoiceBridgeSpec{
		AgentID:      42,
		Provider:     "openai_realtime",
		Model:        "gpt-4o-realtime-preview",
		Endpoint:     "wss://example.com/realtime",
		Voice:        "alloy",
		SystemPrompt: "你是助理",
		APIKey:       "sk-user-key",
		Language:     "zh-CN",
	}
	data, err := buildStartPayload(7, spec)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	checks := map[string]any{
		"voice_provider": "openai_realtime",
		"model":          "gpt-4o-realtime-preview",
		"endpoint":       "wss://example.com/realtime",
		"api_key":        "sk-user-key",
		"system_prompt":  "你是助理",
		"voice":          "alloy",
		"language":       "zh-CN",
	}
	for k, want := range checks {
		if got[k] != want {
			t.Fatalf("payload[%q] = %v, want %v", k, got[k], want)
		}
	}
	// 默认非传声筒：relay_mode 必须为 false（客服/普通语音通话不受影响）。
	if got["relay_mode"] != false {
		t.Fatalf("payload[relay_mode] = %v, want false by default", got["relay_mode"])
	}
}

// 传声筒模式（语音大脑）必须把 relay_mode=true 透传给语音桥，
// 语音桥据此走事件500逐字念回而非事件502参考资料。
func TestBuildStartPayload_RelayModeFlag(t *testing.T) {
	spec := call.VoiceBridgeSpec{
		AgentID:   99,
		Provider:  "doubao_realtime",
		Model:     "doubao-realtime",
		APIKey:    "sk-user-key",
		RelayMode: true,
	}
	data, err := buildStartPayload(7, spec)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["relay_mode"] != true {
		t.Fatalf("payload[relay_mode] = %v, want true", got["relay_mode"])
	}
}
