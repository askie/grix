package agentapi

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/require"
)

func terminalChatStateTestRecord() *durablePendingDelegateRecord {
	return &durablePendingDelegateRecord{
		Event: DelegateEventPayload{
			EventID:   "evt-stop-reason-truncate",
			OwnerID:   101,
			AgentID:   202,
			SenderID:  101,
			SessionID: "sess-stop-reason-truncate",
		},
	}
}

// A connector may report a long adapter error (e.g. Codex 401, Pi missing API
// key) as code/msg. The chat_states.stop_reason column is VARCHAR(255), so the
// reason must be bounded by runes or the settlement transaction aborts with
// pg 22001 and the event_result is NACKed into an endless retry storm.
func TestTerminalChatStateTruncatesLongStopReason(t *testing.T) {
	longCode := strings.Repeat("adapter-401-", 30) // 360 runes, pure ASCII
	state := terminalChatState(EventResultPayload{
		Status: "failed",
		Code:   longCode,
	}, terminalChatStateTestRecord())
	require.NotNil(t, state)
	require.Equal(t, model.SessionAgentStateFailed, state.State)
	require.Len(t, []rune(state.StopReason), model.StopReasonMaxRunes)
	require.True(t, utf8.ValidString(state.StopReason))
	require.True(t, strings.HasPrefix(longCode, state.StopReason))
}

func TestTerminalChatStateTruncatesMultibyteStopReason(t *testing.T) {
	longMsg := strings.Repeat("鉴权失败", 100) // 400 runes, multi-byte UTF-8
	state := terminalChatState(EventResultPayload{
		Status: "failed",
		Msg:    longMsg,
	}, terminalChatStateTestRecord())
	require.NotNil(t, state)
	require.Len(t, []rune(state.StopReason), model.StopReasonMaxRunes)
	require.True(t, utf8.ValidString(state.StopReason))
	require.True(t, strings.HasPrefix(longMsg, state.StopReason))
}

func TestTerminalChatStateFallbackReasonUnchanged(t *testing.T) {
	state := terminalChatState(EventResultPayload{
		Status: "failed",
	}, terminalChatStateTestRecord())
	require.NotNil(t, state)
	require.Equal(t, protocol.AgentDeliveryCodeProcessingFailed, state.StopReason)
}

func TestTerminalChatStateRespondedKeepsEmptyStopReason(t *testing.T) {
	state := terminalChatState(EventResultPayload{
		Status: protocol.AgentEventResultResponded,
		Code:   strings.Repeat("x", 500),
	}, terminalChatStateTestRecord())
	require.NotNil(t, state)
	require.Equal(t, model.SessionAgentStateCompleted, state.State)
	require.Empty(t, state.StopReason)
}

func TestTerminalChatStateCanceledTruncatesLongStopReason(t *testing.T) {
	longMsg := strings.Repeat("已取消-", 100) // 400 runes
	state := terminalChatState(EventResultPayload{
		Status: protocol.AgentEventResultCanceled,
		Msg:    longMsg,
	}, terminalChatStateTestRecord())
	require.NotNil(t, state)
	require.Equal(t, model.SessionAgentStateIdle, state.State)
	require.Len(t, []rune(state.StopReason), model.StopReasonMaxRunes)
	require.True(t, utf8.ValidString(state.StopReason))
	require.True(t, strings.HasPrefix(longMsg, state.StopReason))
}

func TestTerminalChatStateShortReasonUntouched(t *testing.T) {
	state := terminalChatState(EventResultPayload{
		Status: "failed",
		Code:   "unauthorized",
	}, terminalChatStateTestRecord())
	require.NotNil(t, state)
	require.Equal(t, "unauthorized", state.StopReason)
}
