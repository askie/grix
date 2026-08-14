package voiceadapter_test

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/voiceadapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubBridge 是测试用的最小 VoiceAgentBridge 实现。
type stubBridge struct{ family string }

func (s *stubBridge) AdapterID() string { return s.family + "_v1" }
func (s *stubBridge) Family() string    { return s.family }
func (s *stubBridge) Start(_ context.Context, _ voiceadapter.VoiceAgentConfig, _ <-chan voiceadapter.PCMFrame) (<-chan voiceadapter.PCMFrame, <-chan voiceadapter.Event, error) {
	return make(chan voiceadapter.PCMFrame), make(chan voiceadapter.Event), nil
}
func (s *stubBridge) Interrupt(_ context.Context) error { return nil }
func (s *stubBridge) Close(_ context.Context) error     { return nil }

func TestRegistry_RegisterAndNew(t *testing.T) {
	voiceadapter.ResetForTest()
	defer voiceadapter.ResetForTest()

	voiceadapter.Register("test_provider", func() voiceadapter.VoiceAgentBridge {
		return &stubBridge{family: "test_provider"}
	})

	bridge, err := voiceadapter.New("test_provider")
	require.NoError(t, err)
	assert.Equal(t, "test_provider", bridge.Family())
	assert.Equal(t, "test_provider_v1", bridge.AdapterID())
}

func TestRegistry_UnknownFamily(t *testing.T) {
	voiceadapter.ResetForTest()
	defer voiceadapter.ResetForTest()

	_, err := voiceadapter.New("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown family")
}

func TestRegistry_DuplicateRegisterPanics(t *testing.T) {
	voiceadapter.ResetForTest()
	defer voiceadapter.ResetForTest()

	voiceadapter.Register("dup", func() voiceadapter.VoiceAgentBridge {
		return &stubBridge{family: "dup"}
	})
	assert.Panics(t, func() {
		voiceadapter.Register("dup", func() voiceadapter.VoiceAgentBridge {
			return &stubBridge{family: "dup"}
		})
	})
}

func TestRegistry_NewReturnsNewInstance(t *testing.T) {
	voiceadapter.ResetForTest()
	defer voiceadapter.ResetForTest()

	voiceadapter.Register("multi", func() voiceadapter.VoiceAgentBridge {
		return &stubBridge{family: "multi"}
	})

	b1, _ := voiceadapter.New("multi")
	b2, _ := voiceadapter.New("multi")
	// 每次调用返回新实例（指针不同）
	assert.NotSame(t, b1, b2)
}

func TestRegistry_Families(t *testing.T) {
	voiceadapter.ResetForTest()
	defer voiceadapter.ResetForTest()

	voiceadapter.Register("a", func() voiceadapter.VoiceAgentBridge { return &stubBridge{family: "a"} })
	voiceadapter.Register("b", func() voiceadapter.VoiceAgentBridge { return &stubBridge{family: "b"} })

	families := voiceadapter.Families()
	assert.Len(t, families, 2)
	assert.ElementsMatch(t, []string{"a", "b"}, families)
}
