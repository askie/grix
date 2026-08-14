package voicerefiner_test

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/voicerefiner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoopRefiner_ReturnsRaw(t *testing.T) {
	r := &voicerefiner.NoopRefiner{}
	refined, err := r.Refine(context.Background(), "嗯 您好 那个 请问您是张先生吗")
	require.NoError(t, err)
	assert.Equal(t, "嗯 您好 那个 请问您是张先生吗", refined)
}

func TestNoopRefiner_EmptyInput(t *testing.T) {
	r := &voicerefiner.NoopRefiner{}
	refined, err := r.Refine(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, "", refined)
}

func TestNoopRefiner_ImplementsInterface(t *testing.T) {
	var _ voicerefiner.TranscriptRefiner = &voicerefiner.NoopRefiner{}
}

func TestLLMRefiner_ImplementsInterface(t *testing.T) {
	// 只验证接口实现，不做真实 LLM 调用
	var _ voicerefiner.TranscriptRefiner = voicerefiner.NewLLMRefiner(nil, "")
}
