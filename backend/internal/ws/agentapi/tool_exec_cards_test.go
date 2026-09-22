package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/toolcard"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsGrixInternalToolName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"grix_message_send", true},
		{"grix_admin", true},
		{"grix_query", true},
		{"mcp__grix-claude__send_message", true},
		{"mcp__grix-claude__complete", true},
		{"Bash", false},
		{"Read", false},
		{"Edit", false},
		{"mcp__other__tool", false},
		{"grix", false},
		{"some_grix_thing", false},
		{"TaskUpdate", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isGrixInternalToolName(tt.name))
		})
	}
}

func buildTestToolExecCard(summaryText string) string {
	payload := map[string]any{"summary_text": summaryText}
	values := url.Values{}
	data, _ := json.Marshal(payload)
	values.Set("d", string(data))
	uri := (&url.URL{
		Scheme:   "grix",
		Host:     "card",
		Path:     "/tool_execution",
		RawQuery: values.Encode(),
	}).String()
	return fmt.Sprintf("[Tool] %s(%s)", summaryText, uri)
}

func TestIsGrixInternalToolCard(t *testing.T) {
	grixCard := buildTestToolExecCard("grix_message_send")
	assert.True(t, isGrixInternalToolCard(grixCard))

	mcpGrixCard := buildTestToolExecCard("mcp__grix-claude__send_message")
	assert.True(t, isGrixInternalToolCard(mcpGrixCard))

	normalCard := buildTestToolExecCard("Bash: ls -la")
	assert.False(t, isGrixInternalToolCard(normalCard))

	assert.False(t, isGrixInternalToolCard("just plain text"))
}

// Regression: the stream-conflict gate in handleSendMsg must never swallow
// interaction cards. A Kimi ACP agent streams thinking via
// client_stream_chunk, then mid-turn delivers an exec_approval card as a
// send_msg on the SAME event_id; before the isInteractionCard exemption the
// gate nacked that message, the approval prompt never reached the user, and
// the agent's session/request_permission RPC hung the turn forever.
func TestIsInteractionCard(t *testing.T) {
	assert.True(t, isInteractionCard(
		"[Exec Approval] npm test (Kimi ACP)\n/approve tool_x allow-once(grix://card/exec_approval?d=%7B%7D)"))
	assert.True(t, isInteractionCard(
		"[Question] pick one(grix://card/agent_question?d=%7B%7D)"))
	assert.False(t, isInteractionCard(buildTestToolExecCard("Bash: ls -la")))
	assert.False(t, isInteractionCard("plain text with no card"))
	assert.False(t, isInteractionCard(
		"[[Tools] 3 executions](grix://card/tool_execution_group?d=%7B%7D)"))
}

func TestCompactToolExecutionPayload_StripsRawSuccessDetail(t *testing.T) {
	largeOutput := strings.Repeat("raw output ", 8000)
	content := toolcard.BuildExecutionCardURI(map[string]any{
		"summary_text": "Bash: go test ./...",
		"detail_text":  largeOutput,
	})
	extra := json.RawMessage(`{
		"channel_data":{
			"grix":{"toolExecution":{"summary_text":"Bash: go test ./...","detail_text":"duplicate"}},
			"codex":{"raw_event":{"tool_call_id":"call-1","tool_input":{"command":"go test ./..."},"raw_output":"huge"}}
		},
		"biz_card":{"type":"tool_execution","payload":{"raw_output":"huge"}},
		"thread_id":"thread-1"
	}`)

	compactContent, compactExtra, meta, ok := compactToolExecutionPayload(content, extra)
	require.True(t, ok)
	assert.Equal(t, "call-1", meta.ToolCallID)
	assert.False(t, meta.Failed)
	assert.Empty(t, meta.DetailText)
	assert.NotContains(t, compactContent, "raw+output")
	assert.Less(t, len(compactContent), 1024)
	assert.NotContains(t, string(compactExtra), "raw_event")
	assert.NotContains(t, string(compactExtra), "tool_input")
	assert.NotContains(t, string(compactExtra), "biz_card")
	assert.Contains(t, string(compactExtra), `"compacted":true`)
	assert.Contains(t, string(compactExtra), `"thread_id":"thread-1"`)
}

func TestCompactToolExecutionPayload_PreservesBoundedFailureDetail(t *testing.T) {
	content := toolcard.BuildExecutionCardURI(map[string]any{
		"summary_text": "Bash failed",
		"detail_text":  strings.Repeat("失败详情", 2000),
	})
	compactContent, _, meta, ok := compactToolExecutionPayload(content, nil)
	require.True(t, ok)
	assert.True(t, meta.Failed)
	assert.LessOrEqual(t, len(meta.DetailText), toolExecFailureDetailMaxBytes)
	assert.Less(t, len(compactContent), 16<<10)
}

func useMockToolExecRedis(t *testing.T) {
	t.Helper()
	previousRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		_ = store.RDB.Close()
		store.RDB = previousRDB
	})
}

func TestReserveToolExecCard_RetryResolvesToStoredMessage(t *testing.T) {
	useMockToolExecRedis(t)
	ctx := context.Background()
	conn := &agentConn{agentID: 9911, ownerID: 8822}
	meta := toolExecPayloadMeta{SummaryText: "Bash: go test ./...", ToolCallID: "call-stable-1"}

	first := reserveToolExecCard(ctx, conn, "session-tool-dedup", "event-1", "client-1", meta)
	require.False(t, first.handled)
	require.NotEmpty(t, first.dedupKey)
	finishToolExecCard(ctx, first, 7001)

	retry := reserveToolExecCard(ctx, conn, "session-tool-dedup", "event-1", "client-retry", meta)
	assert.True(t, retry.handled)
	assert.Equal(t, int64(7001), retry.msgID)

	next := reserveToolExecCard(ctx, conn, "session-tool-dedup", "event-1", "client-2",
		toolExecPayloadMeta{SummaryText: "Read: a.go", ToolCallID: "call-stable-2"})
	assert.False(t, next.handled, "a different tool call must be stored as its own message")
}

func TestReserveToolExecCard_FailedPersistReleasesReservation(t *testing.T) {
	useMockToolExecRedis(t)
	ctx := context.Background()
	conn := &agentConn{agentID: 9911, ownerID: 8822}
	meta := toolExecPayloadMeta{SummaryText: "Bash: ls", ToolCallID: "call-release"}

	first := reserveToolExecCard(ctx, conn, "session-tool-release", "event-1", "client-1", meta)
	require.False(t, first.handled)
	finishToolExecCard(ctx, first, 0)

	again := reserveToolExecCard(ctx, conn, "session-tool-release", "event-1", "client-1", meta)
	assert.False(t, again.handled, "a failed persist must not block the retry")
}

func TestReserveToolExecCard_SkipsGrixInternalTools(t *testing.T) {
	conn := &agentConn{agentID: 9911, ownerID: 8822}
	got := reserveToolExecCard(context.Background(), conn, "session-internal", "event-1", "client-1",
		toolExecPayloadMeta{SummaryText: "grix_message_send"})
	assert.True(t, got.handled)
	assert.Zero(t, got.msgID)
}

// Consecutive tool calls must be stored as independent messages without
// editing earlier ones; clients fold adjacent tool cards into one group, so
// text between tool calls starts a new group on its own.
func TestHandleSendMsg_StoresEachToolExecutionCardSeparately(t *testing.T) {
	useMockToolExecRedis(t)

	var calls []SendMessageReq
	nextMsgID := int64(5000)
	mgr := NewManager("", 30*time.Second, func(_ context.Context, req SendMessageReq) (*SendMessageResult, error) {
		calls = append(calls, req)
		nextMsgID++
		return &SendMessageResult{MsgID: nextMsgID, CreatedAt: 1704067212000}, nil
	}, nil, nil, nil)
	defer mgr.Shutdown()
	editCount := 0
	mgr.SetEditMsgHandler(func(context.Context, int64, int64, EditMsgPayload) error {
		editCount++
		return nil
	})

	event := DelegateEventPayload{
		EventID:     "evt-tool-cards-separate",
		AgentID:     100,
		OwnerID:     200,
		SenderID:    200,
		SessionID:   "sess-tool-cards-separate",
		SessionType: 1,
		MsgID:       302,
	}
	mgr.registerPendingEventAck(event, 1)
	mgr.registerActiveRun(event)
	conn := &agentConn{agentID: event.AgentID, ownerID: event.OwnerID, clientID: "tool-agent", send: make(chan []byte, 64)}

	send := func(seq int64, clientMsgID, content string) {
		mgr.handleSendMsg(conn, makePacket(t, protocol.CmdSendMsg, seq, SendMsgPayload{
			EventID:     event.EventID,
			SessionID:   event.SessionID,
			ClientMsgID: clientMsgID,
			MsgType:     1,
			Content:     content,
		}))
	}
	send(1, "tool-1", buildTestToolExecCard("Bash: pwd"))
	send(2, "tool-2", buildTestToolExecCard("Read: a.go"))
	send(3, "text-1", "看完了，接着改。")
	send(4, "tool-3", buildTestToolExecCard("Edit: a.go"))
	send(5, "tool-3", buildTestToolExecCard("Edit: a.go")) // exact retry

	require.Len(t, calls, 4, "three tool cards plus one text, retry deduplicated")
	assert.Zero(t, editCount, "tool cards must never edit an earlier message")
	for _, i := range []int{0, 1, 3} {
		assert.Contains(t, calls[i].Content, "grix://card/tool_execution?")
		assert.NotContains(t, calls[i].Content, "tool_execution_group")
	}
	assert.Equal(t, "看完了，接着改。", calls[2].Content)
}
