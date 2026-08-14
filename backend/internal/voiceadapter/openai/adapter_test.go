package openai_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/voiceadapter"
	"github.com/askie/grix/backend/internal/voiceadapter/openai"
)

// 缺 api_key 或 model 时必须返回 ErrNotConfigured（无全局兜底），且不发起网络连接。
func TestOpenAIAdapter_MissingBYOK_ReturnsErrNotConfigured(t *testing.T) {
	cases := []voiceadapter.VoiceAgentConfig{
		{AgentID: 1, Mode: voiceadapter.ModeDuplex},                                   // 全空
		{AgentID: 1, Mode: voiceadapter.ModeDuplex, Model: "gpt-4o-realtime-preview"}, // 缺 api_key
		{AgentID: 1, Mode: voiceadapter.ModeDuplex, APIKey: "sk-x"},                   // 缺 model
	}
	for i, cfg := range cases {
		a := openai.New()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		audio := make(chan voiceadapter.PCMFrame)
		close(audio)
		_, _, err := a.Start(ctx, cfg, audio)
		cancel()
		if !errors.Is(err, openai.ErrNotConfigured) {
			t.Fatalf("case %d: expected ErrNotConfigured, got %v", i, err)
		}
	}
}
