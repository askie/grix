package handler

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestEnqueueOfflinePushTaskReturnsErrorWhenJetStreamUnavailable(t *testing.T) {
	originalJS := store.JS
	store.JS = nil
	defer func() {
		store.JS = originalJS
	}()

	err := enqueueOfflinePushTask(42, "push_msg", map[string]any{
		"msg_id": "1",
	})
	if !errors.Is(err, errOfflinePushUnavailable) {
		t.Fatalf("expected errOfflinePushUnavailable, got=%v", err)
	}
}

func TestCollectLiveRouteNodesPrunesStaleAndSkipsLocalNode(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	ctx := context.Background()
	userID := int64(1501)
	routeKey := "im:ws:route:1501"
	if err := store.RDB.HSet(ctx, routeKey,
		"dev-local", "node-a",
		"dev-remote-live", "node-b",
		"dev-remote-stale", "node-c",
		"dev-excluded", "node-d",
		"dev-empty", "",
	).Err(); err != nil {
		t.Fatalf("seed route map error: %v", err)
	}
	if err := store.RDB.Set(ctx, aliveRouteKey(userID, "dev-local"), "1", time.Minute).Err(); err != nil {
		t.Fatalf("seed local alive error: %v", err)
	}
	if err := store.RDB.Set(ctx, aliveRouteKey(userID, "dev-remote-live"), "1", time.Minute).Err(); err != nil {
		t.Fatalf("seed remote live alive error: %v", err)
	}
	if err := store.RDB.Set(ctx, aliveRouteKey(userID, "dev-excluded"), "1", time.Minute).Err(); err != nil {
		t.Fatalf("seed excluded alive error: %v", err)
	}

	nodes, remoteLiveDevices, ok := collectLiveRouteNodes(ctx, userID, "dev-excluded", "node-a")
	if !ok {
		t.Fatal("expected route lookup success")
	}
	if remoteLiveDevices != 1 {
		t.Fatalf("expected one remote live device, got=%d", remoteLiveDevices)
	}
	if len(nodes) != 1 || nodes[0] != "node-b" {
		t.Fatalf("expected only node-b, got=%v", nodes)
	}

	if exists, err := store.RDB.HExists(ctx, routeKey, "dev-remote-stale").Result(); err != nil {
		t.Fatalf("check stale route removal error: %v", err)
	} else if exists {
		t.Fatal("stale route should be pruned")
	}
	if exists, err := store.RDB.HExists(ctx, routeKey, "dev-empty").Result(); err != nil {
		t.Fatalf("check empty route removal error: %v", err)
	} else if exists {
		t.Fatal("empty node route should be pruned")
	}
}

func TestBroadcastPushMsgToUserDoesNotFallbackWhenLocalDeviceConnected(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	recipient := &sendMsgMockConn{userID: 1601, deviceID: "dev-local"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			1601: {recipient},
		},
	}

	offlineCalled := false
	originalOfflinePush := enqueueOfflinePushTask
	enqueueOfflinePushTask = func(userID int64, cmd string, payload any) error {
		offlineCalled = true
		return nil
	}
	defer func() {
		enqueueOfflinePushTask = originalOfflinePush
	}()

	broadcastPushMsgToUser(hub, context.Background(), 1601, protocol.PushMsgPayload{
		MsgID:      11,
		SessionID:  "session-local-only",
		SenderID:   99,
		SenderType: 1,
		MsgType:    1,
		Content:    "hello",
	})

	if len(recipient.sent) != 1 {
		t.Fatalf("expected one local delivery, got=%d", len(recipient.sent))
	}
	if offlineCalled {
		t.Fatal("local delivery should not trigger offline fallback")
	}
}

func TestBroadcastPushMsgToUserPublishesToRemoteLiveRouteWithoutOfflineFallback(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	ctx := context.Background()
	userID := int64(1701)
	if err := store.RDB.HSet(ctx, "im:ws:route:1701", "dev-remote", "node-b").Err(); err != nil {
		t.Fatalf("seed remote route error: %v", err)
	}
	if err := store.RDB.Set(ctx, aliveRouteKey(userID, "dev-remote"), "1", time.Minute).Err(); err != nil {
		t.Fatalf("seed remote alive error: %v", err)
	}

	pubsub := store.RDB.Subscribe(ctx, "chan:node-b")
	defer pubsub.Close()
	_, _ = pubsub.Receive(ctx)

	offlineCalled := false
	originalOfflinePush := enqueueOfflinePushTask
	enqueueOfflinePushTask = func(userID int64, cmd string, payload any) error {
		offlineCalled = true
		return nil
	}
	defer func() {
		enqueueOfflinePushTask = originalOfflinePush
	}()

	hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{}}
	broadcastPushMsgToUser(hub, ctx, userID, protocol.PushMsgPayload{
		MsgID:      22,
		SessionID:  "session-remote-live",
		SenderID:   77,
		SenderType: 1,
		MsgType:    1,
		Content:    "fanout",
	})

	select {
	case msg := <-pubsub.Channel():
		var envelope struct {
			UserID  int64                   `json:"user_id"`
			Cmd     string                  `json:"cmd"`
			Payload protocol.PushMsgPayload `json:"payload"`
		}
		if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
			t.Fatalf("unmarshal pubsub payload error: %v", err)
		}
		if envelope.UserID != userID {
			t.Fatalf("expected user_id=%d, got=%d", userID, envelope.UserID)
		}
		if envelope.Cmd != protocol.CmdPushMsg {
			t.Fatalf("expected cmd=%s, got=%s", protocol.CmdPushMsg, envelope.Cmd)
		}
	case <-time.After(time.Second):
		t.Fatal("expected remote publish")
	}

	if offlineCalled {
		t.Fatal("remote live route should not trigger offline fallback")
	}
}

func TestBroadcastToUserExceptDeviceDoesNotFallbackToOfflinePush(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	offlineCalled := false
	originalOfflinePush := enqueueOfflinePushTask
	enqueueOfflinePushTask = func(userID int64, cmd string, payload any) error {
		offlineCalled = true
		return nil
	}
	defer func() {
		enqueueOfflinePushTask = originalOfflinePush
	}()

	hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{}}
	broadcastToUserExceptDevice(hub, context.Background(), 1801, "dev-origin", protocol.CmdPushMsg, protocol.PushMsgPayload{
		MsgID:     33,
		SessionID: "session-self-sync",
	})

	if offlineCalled {
		t.Fatal("sender multi-device sync path should not fallback to offline push")
	}
}

func TestBroadcastPushMsgToUserFallsBackWhenRouteLookupUnavailable(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	originalRDB := store.RDB
	store.RDB = nil
	defer func() {
		store.RDB = originalRDB
	}()

	var offlineCalls int
	originalOfflinePush := enqueueOfflinePushTask
	enqueueOfflinePushTask = func(userID int64, cmd string, payload any) error {
		offlineCalls++
		return nil
	}
	defer func() {
		enqueueOfflinePushTask = originalOfflinePush
	}()

	hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{}}
	broadcastPushMsgToUser(hub, context.Background(), 1901, protocol.PushMsgPayload{
		MsgID:      44,
		SessionID:  "session-route-unavailable",
		SenderID:   88,
		SenderType: 1,
		MsgType:    1,
		Content:    "fallback",
	})

	if offlineCalls != 1 {
		t.Fatalf("expected offline fallback when route lookup unavailable, got=%d", offlineCalls)
	}
}

func TestBroadcastPushMsgToUserWritesUnifiedSessionChatLog(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	logPath := filepath.Join(t.TempDir(), "session_chat_frontend.log")
	t.Setenv("AIBOT_SESSION_CHAT_LOG_FILE", logPath)
	t.Cleanup(closeSessionChatFrontendLogFile)

	originalOfflinePush := enqueueOfflinePushTask
	enqueueOfflinePushTask = func(userID int64, cmd string, payload any) error {
		return nil
	}
	defer func() {
		enqueueOfflinePushTask = originalOfflinePush
	}()

	hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{}}
	wantContent := "| 列1 | 列2 |\n| --- | --- |\n| 1 | $x^2$ |"
	wantExtra := json.RawMessage(`{"channel_data":{"grix":{"debug":true}}}`)

	broadcastPushMsgToUser(hub, context.Background(), 2001, protocol.PushMsgPayload{
		MsgID:      55,
		SessionID:  "session-chat-log",
		SenderID:   88,
		SenderType: 2,
		MsgType:    1,
		Content:    wantContent,
		Extra:      wantExtra,
	})

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read session chat log file error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 log line, got=%d content=%q", len(lines), string(raw))
	}

	var entry struct {
		LoggedAt string                  `json:"logged_at"`
		UserID   int64                   `json:"user_id,string"`
		Cmd      string                  `json:"cmd"`
		Payload  protocol.PushMsgPayload `json:"payload"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("unmarshal session chat log entry error: %v", err)
	}

	if entry.LoggedAt == "" {
		t.Fatal("expected logged_at to be populated")
	}
	if entry.UserID != 2001 {
		t.Fatalf("expected user_id=2001, got=%d", entry.UserID)
	}
	if entry.Cmd != protocol.CmdPushMsg {
		t.Fatalf("expected cmd=%s, got=%s", protocol.CmdPushMsg, entry.Cmd)
	}
	if entry.Payload.Content != wantContent {
		t.Fatalf("payload content mismatch got=%q want=%q", entry.Payload.Content, wantContent)
	}
	if string(entry.Payload.Extra) != string(wantExtra) {
		t.Fatalf("payload extra mismatch got=%s want=%s", string(entry.Payload.Extra), string(wantExtra))
	}
}

func withShortOfflinePushDebounceWindow(t *testing.T) {
	t.Helper()
	original := offlinePushAIDebounceWindow
	offlinePushAIDebounceWindow = 30 * time.Millisecond
	t.Cleanup(func() {
		offlinePushAIDebounceWindow = original
	})
}

func TestBroadcastPushMsgToUserDebouncesAgentMessagesAndUsesLatestContent(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()
	withShortOfflinePushDebounceWindow(t)

	calls := make(chan protocol.PushMsgPayload, 8)
	originalOfflinePush := enqueueOfflinePushTask
	enqueueOfflinePushTask = func(userID int64, cmd string, payload any) error {
		pm, _ := payload.(protocol.PushMsgPayload)
		calls <- pm
		return nil
	}
	defer func() {
		enqueueOfflinePushTask = originalOfflinePush
	}()

	hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{}}
	base := protocol.PushMsgPayload{
		SessionID:  "session-debounce",
		SenderID:   88,
		SenderType: senderTypeAgent,
		MsgType:    1,
	}

	first := base
	first.MsgID = 1
	first.Content = "好，工作"
	broadcastPushMsgToUser(hub, context.Background(), 2101, first)

	// Arrives within the debounce window: must cancel/replace the pending push.
	second := base
	second.MsgID = 2
	second.Content = "好，工作已经完成，详见下方总结。"
	broadcastPushMsgToUser(hub, context.Background(), 2101, second)

	select {
	case pm := <-calls:
		if pm.Content != second.Content {
			t.Fatalf("expected debounced push to carry latest content %q, got %q", second.Content, pm.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for debounced offline push")
	}

	select {
	case pm := <-calls:
		t.Fatalf("expected exactly one offline push, got extra call with content %q", pm.Content)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestBroadcastPushMsgToUserSkipsDebounceForHumanSender(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()
	withShortOfflinePushDebounceWindow(t)

	var offlineCalls int
	originalOfflinePush := enqueueOfflinePushTask
	enqueueOfflinePushTask = func(userID int64, cmd string, payload any) error {
		offlineCalls++
		return nil
	}
	defer func() {
		enqueueOfflinePushTask = originalOfflinePush
	}()

	hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{}}
	broadcastPushMsgToUser(hub, context.Background(), 2201, protocol.PushMsgPayload{
		MsgID:      1,
		SessionID:  "session-human",
		SenderID:   88,
		SenderType: 1,
		MsgType:    1,
		Content:    "hi",
	})

	if offlineCalls != 1 {
		t.Fatalf("expected immediate offline push for human sender, got=%d", offlineCalls)
	}
}

func TestBroadcastPushMsgToUserSkipsDebounceForForcePushAndCards(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()
	withShortOfflinePushDebounceWindow(t)

	var offlineCalls int
	originalOfflinePush := enqueueOfflinePushTask
	enqueueOfflinePushTask = func(userID int64, cmd string, payload any) error {
		offlineCalls++
		return nil
	}
	defer func() {
		enqueueOfflinePushTask = originalOfflinePush
	}()

	hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{}}

	broadcastPushMsgToUser(hub, context.Background(), 2301, protocol.PushMsgPayload{
		MsgID:      1,
		SessionID:  "session-force",
		SenderID:   88,
		SenderType: senderTypeAgent,
		MsgType:    1,
		Content:    "来电",
		ForcePush:  true,
	})
	if offlineCalls != 1 {
		t.Fatalf("expected immediate offline push for ForcePush message, got=%d", offlineCalls)
	}

	broadcastPushMsgToUser(hub, context.Background(), 2302, protocol.PushMsgPayload{
		MsgID:      2,
		SessionID:  "session-card",
		SenderID:   88,
		SenderType: senderTypeAgent,
		MsgType:    1,
		Content:    "[Exec Approval](grix://card/exec_approval?id=1)",
	})
	if offlineCalls != 2 {
		t.Fatalf("expected immediate offline push for approval card message, got=%d", offlineCalls)
	}
}
