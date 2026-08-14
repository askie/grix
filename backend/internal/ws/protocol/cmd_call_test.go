package protocol_test

import (
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/assert"
)

// callCmds 列出所有 call:* 常量，便于统一校验
var callCmds = []string{
	protocol.CmdCallInvite,
	protocol.CmdCallInviteAck,
	protocol.CmdCallRing,
	protocol.CmdCallAnswer,
	protocol.CmdCallReject,
	protocol.CmdCallHangup,
	protocol.CmdCallPeerAnswered,
	protocol.CmdCallState,
	protocol.CmdCallTimeout,
	protocol.CmdCallBusy,
	// Phase 2
	protocol.CmdCallAnswerWithAI,
	protocol.CmdCallTakeover,
	protocol.CmdCallHandBack,
	// 多访客客服
	protocol.CmdCallListen,
	protocol.CmdCallListenAck,
	protocol.CmdCallLeave,
	protocol.CmdCallAiDelegated,
	protocol.CmdCallVoiceStatusEnd,
}

func TestCallCmds_HaveCallPrefix(t *testing.T) {
	for _, cmd := range callCmds {
		assert.True(t, strings.HasPrefix(cmd, "call:"), "cmd %q should have call: prefix", cmd)
	}
}

func TestCallCmds_NoConflictWithExisting(t *testing.T) {
	// 现有协议常量（从 packet.go 中取代表性样本）
	existing := []string{
		protocol.CmdAuth, protocol.CmdPing, protocol.CmdSendMsg,
		protocol.CmdPullSync, protocol.CmdDelegateStart, protocol.CmdStreamStop,
	}
	existingSet := make(map[string]bool, len(existing))
	for _, c := range existing {
		existingSet[c] = true
	}
	for _, cmd := range callCmds {
		assert.False(t, existingSet[cmd], "call cmd %q conflicts with existing cmd", cmd)
	}
}

func TestCallCmds_Unique(t *testing.T) {
	seen := make(map[string]bool)
	for _, cmd := range callCmds {
		assert.False(t, seen[cmd], "duplicate call cmd: %q", cmd)
		seen[cmd] = true
	}
}
