package agentapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/require"
)

type chatQueueInvokeFixture struct {
	ownerID     int64
	callerID    int64
	targetAgent int64
	sessionID   string
}

func setupChatQueueInvokeTest(t *testing.T) chatQueueInvokeFixture {
	t.Helper()
	testDB, cleanup := setupAgentInvokeDispatchTest(t)
	previousManager := GetGlobalManager()
	SetGlobal(nil)
	previousPoll := chatQueuePollInterval
	previousListTimeout := chatQueueListConfirmTimeout
	previousWriteTimeout := chatQueueWriteConfirmTimeout
	chatQueuePollInterval = time.Millisecond
	chatQueueListConfirmTimeout = 30 * time.Millisecond
	chatQueueWriteConfirmTimeout = 30 * time.Millisecond
	t.Cleanup(func() {
		chatQueuePollInterval = previousPoll
		chatQueueListConfirmTimeout = previousListTimeout
		chatQueueWriteConfirmTimeout = previousWriteTimeout
		SetGlobal(previousManager)
		cleanup()
	})

	fixture := chatQueueInvokeFixture{
		ownerID:     73101,
		callerID:    73102,
		targetAgent: 73103,
		sessionID:   "chat-queue-invoke-session",
	}
	seedAgentInvokeDispatchActor(t, testDB, fixture.ownerID, fixture.callerID, "ak_chat_queue_invoke")
	seedAgentInvokeDispatchScope(t, fixture.callerID, agentscope.ScopeAgentTaskQuery)
	now := time.Now()
	require.NoError(t, store.DB.Create(&model.Session{
		SessionID:        fixture.sessionID,
		OwnerID:          fixture.ownerID,
		SessionType:      model.SessionTypeDirect,
		ModerationStatus: model.SessionModerationStatusActive,
	}).Error)
	require.NoError(t, store.DB.Create(&[]model.SessionMember{
		{SessionID: fixture.sessionID, MemberID: fixture.ownerID, MemberType: 1, Role: 3, JoinedAt: now, LastActiveAt: now},
		{SessionID: fixture.sessionID, MemberID: fixture.targetAgent, MemberType: 2, Role: 1, JoinedAt: now, LastActiveAt: now},
	}).Error)
	return fixture
}

func (f chatQueueInvokeFixture) invoke(
	t *testing.T,
	ownerID int64,
	action string,
	params map[string]interface{},
	dispatch func(agentID, ownerID int64, cmd string, payload any) bool,
) (interface{}, int, string) {
	t.Helper()
	if params == nil {
		params = map[string]interface{}{}
	}
	params["session_id"] = f.sessionID
	return dispatchAgentInvokeWithHooks(f.callerID, ownerID, action, params, agentInvokeHooks{
		dispatchEventLifecycleCommand: dispatch,
	})
}

func (f chatQueueInvokeFixture) commandHook(t *testing.T, expectedCmd string, update func(any)) func(int64, int64, string, any) bool {
	t.Helper()
	return func(agentID, ownerID int64, cmd string, payload any) bool {
		require.Equal(t, f.targetAgent, agentID)
		require.Equal(t, f.ownerID, ownerID)
		require.Equal(t, expectedCmd, cmd)
		if update != nil {
			update(payload)
		}
		return true
	}
}

func requireChatQueueResult(t *testing.T, data interface{}) chatQueueInvokeResult {
	t.Helper()
	result, ok := data.(chatQueueInvokeResult)
	require.True(t, ok, "expected chatQueueInvokeResult, got %T", data)
	return result
}

func storeChatQueueSnapshot(t *testing.T, fixture chatQueueInvokeFixture, payload map[string]any) {
	t.Helper()
	payload["session_id"] = fixture.sessionID
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	storeQueueSnapshotMirror(context.Background(), fixture.ownerID, fixture.targetAgent, raw)
}

func connectorQueueItem(eventID string, position int, content string) map[string]any {
	return map[string]any{
		"event_id":        eventID,
		"position":        position,
		"content_preview": connectorQueuePreview(content),
		"content":         content,
	}
}

func connectorQueuePreview(content string) string {
	normalized := []rune(strings.Join(strings.Fields(content), " "))
	if len(normalized) > 64 {
		return string(normalized[:64]) + "..."
	}
	return string(normalized)
}

func TestDispatchChatQueueActionsConfirmationSuccess(t *testing.T) {
	t.Run("list", func(t *testing.T) {
		fixture := setupChatQueueInvokeTest(t)
		hook := fixture.commandHook(t, protocol.CmdQueueSnapshotQuery, func(any) {
			time.Sleep(2 * time.Millisecond)
			storeChatQueueSnapshot(t, fixture, map[string]any{
				"running": []string{},
				"queued":  []map[string]any{{"event_id": "evt-list", "position": 1, "content_preview": "queued"}},
			})
		})
		data, code, msg := fixture.invoke(t, fixture.ownerID, "chat_queue_list", nil, hook)
		require.Equal(t, 0, code, msg)
		result := requireChatQueueResult(t, data)
		require.True(t, result.Confirmed)
		require.True(t, result.Dispatched)
		require.Equal(t, []string{"evt-list"}, result.Queued)
		require.Len(t, result.Items, 1)
		require.Equal(t, "queued", result.Items[0].ContentPreview)
	})

	t.Run("cancel", func(t *testing.T) {
		fixture := setupChatQueueInvokeTest(t)
		storeChatQueueSnapshot(t, fixture, map[string]any{
			"running": []string{"evt-run"},
			"queued":  []map[string]any{{"event_id": "evt-cancel", "position": 1}, {"event_id": "evt-left", "position": 2}},
		})
		hook := fixture.commandHook(t, protocol.CmdEventCancel, func(payload any) {
			decoded, ok := payload.(protocol.EventCancelPayload)
			require.True(t, ok)
			require.Equal(t, "evt-cancel", decoded.EventID)
			storeChatQueueSnapshot(t, fixture, map[string]any{
				"running": []string{"evt-run"},
				"queued":  []map[string]any{{"event_id": "evt-left", "position": 1}},
			})
		})
		data, code, msg := fixture.invoke(t, fixture.ownerID, "chat_queue_cancel", map[string]interface{}{"event_id": "evt-cancel"}, hook)
		require.Equal(t, 0, code, msg)
		result := requireChatQueueResult(t, data)
		require.True(t, result.Confirmed)
		require.Equal(t, []string{"evt-left"}, result.Queued)
	})

	t.Run("clear", func(t *testing.T) {
		fixture := setupChatQueueInvokeTest(t)
		storeChatQueueSnapshot(t, fixture, map[string]any{
			"running": []string{"evt-run"},
			"queued":  []map[string]any{{"event_id": "evt-clear", "position": 1}},
		})
		hook := fixture.commandHook(t, protocol.CmdQueueClear, func(any) {
			storeChatQueueSnapshot(t, fixture, map[string]any{"running": []string{"evt-run"}, "queued": []map[string]any{}})
		})
		data, code, msg := fixture.invoke(t, fixture.ownerID, "chat_queue_clear", nil, hook)
		require.Equal(t, 0, code, msg)
		result := requireChatQueueResult(t, data)
		require.True(t, result.Confirmed)
		require.Empty(t, result.Queued)
		require.Equal(t, []string{"evt-run"}, result.Running)
	})

	t.Run("hold", func(t *testing.T) {
		fixture := setupChatQueueInvokeTest(t)
		storeChatQueueSnapshot(t, fixture, map[string]any{
			"running": []string{},
			"queued":  []map[string]any{{"event_id": "evt-hold", "position": 1, "held": false}},
		})
		hook := fixture.commandHook(t, protocol.CmdEventHold, func(payload any) {
			decoded, ok := payload.(protocol.EventHoldPayload)
			require.True(t, ok)
			require.True(t, decoded.Hold)
			require.Equal(t, "manual", decoded.Reason)
			require.Equal(t, int64(120000), decoded.TTLMS)
			storeChatQueueSnapshot(t, fixture, map[string]any{
				"running": []string{},
				"queued":  []map[string]any{{"event_id": "evt-hold", "position": 1, "held": true, "held_reason": "manual"}},
			})
		})
		data, code, msg := fixture.invoke(t, fixture.ownerID, "chat_queue_hold", map[string]interface{}{
			"event_id": "evt-hold", "hold": true, "ttl_ms": 120000,
		}, hook)
		require.Equal(t, 0, code, msg)
		result := requireChatQueueResult(t, data)
		require.True(t, result.Confirmed)
		require.Len(t, result.Items, 1)
		require.True(t, result.Items[0].Held)
		require.Equal(t, "manual", result.Items[0].HeldReason)
	})

	t.Run("edit over 64 chars with connector preview", func(t *testing.T) {
		fixture := setupChatQueueInvokeTest(t)
		content := strings.Repeat("a", 64) + "\n  " + strings.Repeat("界", 12)
		oldContent := strings.Repeat("old ", 30)
		storeChatQueueSnapshot(t, fixture, map[string]any{
			"running": []string{},
			"queued":  []map[string]any{connectorQueueItem("evt-edit", 1, oldContent)},
		})
		hook := fixture.commandHook(t, protocol.CmdQueueEdit, func(payload any) {
			decoded, ok := payload.(protocol.QueueEditPayload)
			require.True(t, ok)
			require.Equal(t, content, decoded.Content)
			storeChatQueueSnapshot(t, fixture, map[string]any{
				"running": []string{},
				"queued":  []map[string]any{connectorQueueItem("evt-edit", 1, content)},
			})
		})
		data, code, msg := fixture.invoke(t, fixture.ownerID, "chat_queue_edit", map[string]interface{}{
			"event_id": "evt-edit", "content": content,
		}, hook)
		require.Equal(t, 0, code, msg)
		result := requireChatQueueResult(t, data)
		require.True(t, result.Confirmed)
		require.Len(t, result.Items, 1)
		require.Equal(t, strings.Repeat("a", 64)+"...", result.Items[0].ContentPreview)
		require.NotEqual(t, truncateQueueContentPreview(content), result.Items[0].ContentPreview)
		expectedHash := sha256.Sum256([]byte(content))
		require.Equal(t, hex.EncodeToString(expectedHash[:]), result.Items[0].ContentSHA256)
	})

	t.Run("reorder partial ids with connector preview", func(t *testing.T) {
		fixture := setupChatQueueInvokeTest(t)
		contentA := strings.Repeat("a", 70)
		contentB := strings.Repeat("b", 70)
		contentC := strings.Repeat("c", 70)
		storeChatQueueSnapshot(t, fixture, map[string]any{
			"running": []string{},
			"queued": []map[string]any{
				connectorQueueItem("evt-a", 1, contentA),
				connectorQueueItem("evt-b", 2, contentB),
				connectorQueueItem("evt-c", 3, contentC),
			},
		})
		hook := fixture.commandHook(t, protocol.CmdQueueReorder, func(payload any) {
			decoded, ok := payload.(protocol.QueueReorderPayload)
			require.True(t, ok)
			require.Equal(t, []string{"evt-b"}, decoded.OrderedEventIDs)
			storeChatQueueSnapshot(t, fixture, map[string]any{
				"running": []string{},
				"queued": []map[string]any{
					connectorQueueItem("evt-b", 1, contentB),
					connectorQueueItem("evt-a", 2, contentA),
					connectorQueueItem("evt-c", 3, contentC),
				},
			})
		})
		data, code, msg := fixture.invoke(t, fixture.ownerID, "chat_queue_reorder", map[string]interface{}{
			"ordered_event_ids": []interface{}{"evt-b"},
		}, hook)
		require.Equal(t, 0, code, msg)
		result := requireChatQueueResult(t, data)
		require.True(t, result.Confirmed)
		require.Equal(t, []string{"evt-b", "evt-a", "evt-c"}, result.Queued)
		require.Len(t, result.Items, 3)
		for _, item := range result.Items {
			require.Len(t, item.ContentPreview, 67)
			require.NotEmpty(t, item.ContentSHA256)
		}
	})
}

func TestDispatchChatQueueActionTimeoutReturnsDispatched(t *testing.T) {
	fixture := setupChatQueueInvokeTest(t)
	storeChatQueueSnapshot(t, fixture, map[string]any{
		"running": []string{},
		"queued":  []map[string]any{{"event_id": "evt-timeout", "position": 1}},
	})
	hook := fixture.commandHook(t, protocol.CmdEventCancel, nil)
	data, code, msg := fixture.invoke(t, fixture.ownerID, "chat_queue_cancel", map[string]interface{}{"event_id": "evt-timeout"}, hook)
	require.Equal(t, 0, code, msg)
	result := requireChatQueueResult(t, data)
	require.False(t, result.Confirmed)
	require.True(t, result.Dispatched)
	require.False(t, result.Stale)
}

func TestDispatchChatQueueListOfflineReturnsStaleMirror(t *testing.T) {
	fixture := setupChatQueueInvokeTest(t)
	storeChatQueueSnapshot(t, fixture, map[string]any{
		"running": []string{"evt-run"},
		"queued":  []map[string]any{{"event_id": "evt-stale", "position": 1, "content_preview": "cached"}},
	})
	data, code, msg := fixture.invoke(t, fixture.ownerID, "chat_queue_list", nil, func(int64, int64, string, any) bool {
		return false
	})
	require.Equal(t, 0, code, msg)
	result := requireChatQueueResult(t, data)
	require.False(t, result.Confirmed)
	require.False(t, result.Dispatched)
	require.True(t, result.Stale)
	require.Equal(t, []string{"evt-stale"}, result.Queued)
	require.Len(t, result.Items, 2)
	require.Equal(t, "cached", result.Items[1].ContentPreview)
}

func TestDispatchChatQueueWriteOfflineReturnsUnavailable(t *testing.T) {
	fixture := setupChatQueueInvokeTest(t)
	storeChatQueueSnapshot(t, fixture, map[string]any{
		"running": []string{},
		"queued":  []map[string]any{{"event_id": "evt-offline", "position": 1}},
	})
	data, code, msg := fixture.invoke(t, fixture.ownerID, "chat_queue_cancel", map[string]interface{}{"event_id": "evt-offline"}, func(int64, int64, string, any) bool {
		return false
	})
	require.Nil(t, data)
	require.Equal(t, 5001, code)
	require.Equal(t, "agent channel unavailable", msg)
}

func TestDispatchChatQueueActionsRejectRunningTarget(t *testing.T) {
	fixture := setupChatQueueInvokeTest(t)
	storeChatQueueSnapshot(t, fixture, map[string]any{
		"running": []string{"evt-running"},
		"queued":  []map[string]any{},
	})
	for _, testCase := range []struct {
		action string
		params map[string]interface{}
	}{
		{action: "chat_queue_cancel", params: map[string]interface{}{"event_id": "evt-running"}},
		{action: "chat_queue_edit", params: map[string]interface{}{"event_id": "evt-running", "content": "new"}},
		{action: "chat_queue_hold", params: map[string]interface{}{"event_id": "evt-running", "hold": true}},
	} {
		t.Run(testCase.action, func(t *testing.T) {
			called := false
			data, code, msg := fixture.invoke(t, fixture.ownerID, testCase.action, testCase.params, func(int64, int64, string, any) bool {
				called = true
				return true
			})
			require.Nil(t, data)
			require.Equal(t, 4001, code)
			require.Contains(t, msg, "stop")
			require.False(t, called)
		})
	}
}

func TestDispatchChatQueueActionsRejectUnauthorizedSession(t *testing.T) {
	fixture := setupChatQueueInvokeTest(t)
	for _, testCase := range []struct {
		action string
		params map[string]interface{}
	}{
		{action: "chat_queue_list"},
		{action: "chat_queue_cancel", params: map[string]interface{}{"event_id": "evt"}},
		{action: "chat_queue_clear"},
		{action: "chat_queue_hold", params: map[string]interface{}{"event_id": "evt", "hold": true}},
		{action: "chat_queue_edit", params: map[string]interface{}{"event_id": "evt", "content": "new"}},
		{action: "chat_queue_reorder", params: map[string]interface{}{"ordered_event_ids": []interface{}{"evt"}}},
	} {
		t.Run(testCase.action, func(t *testing.T) {
			called := false
			data, code, _ := fixture.invoke(t, fixture.ownerID+999, testCase.action, testCase.params, func(int64, int64, string, any) bool {
				called = true
				return true
			})
			require.Nil(t, data)
			require.Equal(t, 4003, code)
			require.False(t, called)
		})
	}
}

func TestDispatchChatQueueActionsRejectInvalidParams(t *testing.T) {
	fixture := setupChatQueueInvokeTest(t)
	for _, testCase := range []struct {
		name   string
		action string
		params map[string]interface{}
	}{
		{name: "session", action: "chat_queue_list", params: map[string]interface{}{}},
		{name: "cancel event", action: "chat_queue_cancel", params: map[string]interface{}{"session_id": fixture.sessionID}},
		{name: "hold value", action: "chat_queue_hold", params: map[string]interface{}{"session_id": fixture.sessionID, "event_id": "evt"}},
		{name: "hold ttl", action: "chat_queue_hold", params: map[string]interface{}{"session_id": fixture.sessionID, "event_id": "evt", "hold": true, "ttl_ms": -1}},
		{name: "edit content", action: "chat_queue_edit", params: map[string]interface{}{"session_id": fixture.sessionID, "event_id": "evt", "content": " "}},
		{name: "reorder ids", action: "chat_queue_reorder", params: map[string]interface{}{"session_id": fixture.sessionID, "ordered_event_ids": []interface{}{}}},
		{name: "agent id", action: "chat_queue_clear", params: map[string]interface{}{"session_id": fixture.sessionID, "agent_id": "bad"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			data, code, _ := dispatchAgentInvokeWithHooks(fixture.callerID, fixture.ownerID, testCase.action, testCase.params, agentInvokeHooks{})
			require.Nil(t, data)
			require.Equal(t, 4001, code)
		})
	}
}

func TestChatQueueActionsUseTaskQueryScope(t *testing.T) {
	for _, action := range []string{"chat_queue_list", "chat_queue_cancel", "chat_queue_clear", "chat_queue_hold", "chat_queue_edit", "chat_queue_reorder"} {
		require.Equal(t, agentscope.ScopeAgentTaskQuery, actionRegistry[action].Scope, action)
	}
}

func TestLoadLegacyChatQueueMirrorWithoutItems(t *testing.T) {
	fixture := setupChatQueueInvokeTest(t)
	raw, err := json.Marshal(map[string]any{
		"agent_id":   fixture.targetAgent,
		"running":    []string{"evt-run"},
		"queued":     []string{"evt-legacy"},
		"updated_at": 123,
	})
	require.NoError(t, err)
	require.NoError(t, store.RDB.Set(context.Background(), queueSnapshotMirrorKey(fixture.ownerID, fixture.sessionID, fixture.targetAgent), raw, time.Hour).Err())

	mirror := loadQueueSnapshotMirror(context.Background(), fixture.ownerID, fixture.sessionID, fixture.targetAgent)
	require.NotNil(t, mirror)
	require.Equal(t, []string{"evt-run"}, mirror.Running)
	require.Equal(t, []string{"evt-legacy"}, mirror.Queued)
	require.Nil(t, mirror.Items)

	data, code, msg := fixture.invoke(t, fixture.ownerID, "chat_queue_list", nil, func(int64, int64, string, any) bool {
		return false
	})
	require.Equal(t, 0, code, msg)
	result := requireChatQueueResult(t, data)
	require.True(t, result.Stale)
	require.Equal(t, int64(123), result.UpdatedAt)
	require.Equal(t, []string{"evt-legacy"}, result.Queued)
	require.NotNil(t, result.Items)
	require.Empty(t, result.Items)
}
