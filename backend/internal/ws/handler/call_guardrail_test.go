package handler

import (
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 自动代接成功时通知 owner(callee) call:ai_delegated（不含 room token，token 在 call:listen 时按需签发）。
func TestHandleCallInvite_AutoDelegate_NotifiesOwner(t *testing.T) {
	bridge, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()
	resolveCalleeVoiceAgent = func(_ int64, _ string) (int64, bool) { return 42, true }

	hub := newCallHandlerMockHub()
	callerConn := &callHandlerMockConn{userID: 1}
	calleeConn := &callHandlerMockConn{userID: 2}
	hub.addConn(2, calleeConn)

	invitePayload, _ := json.Marshal(protocol.CallInvitePayload{PeerID: "2", PeerType: "user", CallMode: 1})
	HandleCallInvite(hub, callerConn, &protocol.Packet{Cmd: protocol.CmdCallInvite, Seq: 1, Payload: invitePayload})

	assert.NotEmpty(t, bridge.started)
	ad, ok := calleeConn.findCmd(protocol.CmdCallAiDelegated)
	require.True(t, ok, "owner should be notified for takeover")
	b, _ := json.Marshal(ad.payload)
	var got protocol.CallAiDelegatedPayload
	require.NoError(t, json.Unmarshal(b, &got))
	assert.NotEmpty(t, got.CallID)
	// token 不在此 payload 下发——通过 call:listen 按需签发
	assert.NotContains(t, string(b), "room_token", "room token must NOT be sent in ai_delegated")
	_, rang := calleeConn.findCmd(protocol.CmdCallRing)
	assert.False(t, rang)
}

// 每日上限达到时，自动代接回退到响铃（不起 bridge）。
func TestHandleCallInvite_AutoDelegate_DailyLimitFallsBackToRing(t *testing.T) {
	bridge, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()
	resolveCalleeVoiceAgent = func(_ int64, _ string) (int64, bool) { return 42, true }
	reserveVoiceDailyQuota = func(_ int64, _ int) bool { return false }

	hub := newCallHandlerMockHub()
	callerConn := &callHandlerMockConn{userID: 1}
	calleeConn := &callHandlerMockConn{userID: 2}
	hub.addConn(2, calleeConn)

	invitePayload, _ := json.Marshal(protocol.CallInvitePayload{PeerID: "2", PeerType: "user", CallMode: 1})
	HandleCallInvite(hub, callerConn, &protocol.Packet{Cmd: protocol.CmdCallInvite, Seq: 1, Payload: invitePayload})

	assert.Empty(t, bridge.started, "daily limit should not start bridge")
	_, rang := calleeConn.findCmd(protocol.CmdCallRing)
	assert.True(t, rang, "should fall back to ring")
}
