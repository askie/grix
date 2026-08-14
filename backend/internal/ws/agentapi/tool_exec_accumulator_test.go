package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
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
	content := buildToolExecutionCardURI(map[string]any{
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
	content := buildToolExecutionCardURI(map[string]any{
		"summary_text": "Bash failed",
		"detail_text":  strings.Repeat("失败详情", 2000),
	})
	compactContent, _, meta, ok := compactToolExecutionPayload(content, nil)
	require.True(t, ok)
	assert.True(t, meta.Failed)
	assert.LessOrEqual(t, len(meta.DetailText), toolExecFailureDetailMaxBytes)
	assert.Less(t, len(compactContent), 16<<10)
}

func TestTryAccumulateToolExec_DeduplicatesBeforeSecondEdit(t *testing.T) {
	previousRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		_ = store.RDB.Close()
		store.RDB = previousRDB
	})

	ctx := context.Background()
	const (
		agentID   = int64(9911)
		ownerID   = int64(8822)
		sessionID = "session-tool-dedup"
	)
	saveToolExecAccum(ctx, agentID, sessionID, &toolExecAccumState{
		MsgID:      7001,
		Children:   []toolExecAccumChild{{SummaryText: "Read: a.go"}},
		TotalCount: 1,
	})

	editCount := 0
	manager := &Manager{
		editMsgFn: func(_ context.Context, _, _ int64, payload EditMsgPayload) error {
			editCount++
			assert.Equal(t, int64(7001), payload.MsgID)
			assert.Empty(t, payload.Extra, "aggregated edits must retain the existing compact extra")
			return nil
		},
	}
	conn := &agentConn{agentID: agentID, ownerID: ownerID}
	meta := toolExecPayloadMeta{
		SummaryText: "Bash: go test ./...",
		ToolCallID:  "call-stable-1",
	}

	first := manager.tryAccumulateToolExec(ctx, conn, sessionID, "event-1", "client-1", meta)
	require.True(t, first.handled)
	assert.Equal(t, int64(7001), first.msgID)
	assert.Equal(t, 1, editCount)

	retry := manager.tryAccumulateToolExec(ctx, conn, sessionID, "event-1", "client-retry", meta)
	require.True(t, retry.handled)
	assert.Equal(t, int64(7001), retry.msgID)
	assert.Equal(t, 1, editCount, "an exact retry must not append or edit again")

	state, err := loadToolExecAccum(ctx, agentID, sessionID)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Len(t, state.Children, 2)
	assert.Equal(t, 2, state.TotalCount)
}

func TestAppendToolExecChildBounded_KeepsGroupUnderStorageBudget(t *testing.T) {
	state := &toolExecAccumState{}
	for i := 0; i < 200; i++ {
		appendToolExecChildBounded(state, toolExecAccumChild{
			SummaryText: fmt.Sprintf("Tool %03d failed", i),
			DetailText:  strings.Repeat("x", toolExecFailureDetailMaxBytes),
			Failed:      true,
		})
	}

	content := buildToolExecutionGroupCardWithCounts(state.Children, state.TotalCount, state.OmittedCount)
	assert.LessOrEqual(t, len(content), toolExecGroupMaxBytes)
	assert.Equal(t, 200, state.TotalCount)
	assert.Greater(t, state.OmittedCount, 0)
	assert.Less(t, len(state.Children), state.TotalCount)
	assert.Contains(t, content, "omitted_count")
}
