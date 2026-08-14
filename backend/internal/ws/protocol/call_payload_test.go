package protocol_test

import (
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCallInvitePayload_RoundTrip(t *testing.T) {
	orig := protocol.CallInvitePayload{PeerID: "12345", PeerType: "user", CallMode: 1}
	b, err := json.Marshal(orig)
	require.NoError(t, err)
	var got protocol.CallInvitePayload
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, orig, got)
}

func TestCallRingPayload_RoundTrip(t *testing.T) {
	orig := protocol.CallRingPayload{
		CallID: "call-abc", CallerID: "99", CallerName: "Alice", CallMode: 1,
	}
	b, err := json.Marshal(orig)
	require.NoError(t, err)
	var got protocol.CallRingPayload
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, orig, got)
}

func TestCallPeerAnsweredPayload_RoundTrip(t *testing.T) {
	orig := protocol.CallPeerAnsweredPayload{
		CallID: "call-xyz", Mode: "human", RoomToken: "tok123", RoomURL: "wss://lk.example.com",
	}
	b, err := json.Marshal(orig)
	require.NoError(t, err)
	var got protocol.CallPeerAnsweredPayload
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, orig, got)
}

func TestCallStatePayload_RoundTrip(t *testing.T) {
	orig := protocol.CallStatePayload{CallID: "call-1", State: protocol.CallStateEnded, Ts: 1717000000}
	b, err := json.Marshal(orig)
	require.NoError(t, err)
	var got protocol.CallStatePayload
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, orig, got)
}

// --- Phase 2 payload 测试 ---

func TestCallAnswerWithAIPayload_RoundTrip(t *testing.T) {
	orig := protocol.CallAnswerWithAIPayload{CallID: "call-ai-1", AgentID: "42"}
	b, err := json.Marshal(orig)
	require.NoError(t, err)
	var got protocol.CallAnswerWithAIPayload
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, orig, got)
}

func TestCallTakeoverPayload_RoundTrip(t *testing.T) {
	orig := protocol.CallTakeoverPayload{CallID: "call-takeover-1"}
	b, err := json.Marshal(orig)
	require.NoError(t, err)
	var got protocol.CallTakeoverPayload
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, orig, got)
}

func TestCallHandBackPayload_RoundTrip(t *testing.T) {
	orig := protocol.CallHandBackPayload{CallID: "call-handback-1"}
	b, err := json.Marshal(orig)
	require.NoError(t, err)
	var got protocol.CallHandBackPayload
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, orig, got)
}

func TestCallAIStatePayload_RoundTrip(t *testing.T) {
	orig := protocol.CallAIStatePayload{CallID: "call-ai-state", Mode: "ai_delegated", Ts: 1717000001}
	b, err := json.Marshal(orig)
	require.NoError(t, err)
	var got protocol.CallAIStatePayload
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, orig, got)
}
