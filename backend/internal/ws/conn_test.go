package ws

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var initConnTestLoggerOnce sync.Once

func ensureConnTestLogger() {
	initConnTestLoggerOnce.Do(func() {
		logger.Init()
	})
}

func TestConnAckTimeoutEnqueuesOfflinePushForRecipient(t *testing.T) {
	ensureConnTestLogger()
	conn := NewConn(nil)
	conn.SetAuth(1002, "session-recipient", "device-recipient", "ios")

	payload, err := json.Marshal(protocol.PushMsgPayload{
		MsgID:     10001,
		SessionID: "session-recipient",
		SenderID:  1001,
		MsgType:   1,
		Content:   "hello",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	var capturedUserID int64
	var capturedCmd string
	var capturedPayload json.RawMessage
	original := enqueueOfflinePushAfterAckTimeout
	enqueueOfflinePushAfterAckTimeout = func(userID int64, cmd string, payload json.RawMessage) error {
		capturedUserID = userID
		capturedCmd = cmd
		capturedPayload = append(json.RawMessage(nil), payload...)
		return nil
	}
	defer func() {
		enqueueOfflinePushAfterAckTimeout = original
	}()

	conn.pendingPush[10001] = &pendingPushState{
		cmd:     protocol.CmdPushMsg,
		payload: payload,
		retries: maxPushRetry,
	}

	conn.onPushAckTimeout(10001)

	if capturedUserID != 1002 {
		t.Fatalf("captured user_id=%d want=1002", capturedUserID)
	}
	if capturedCmd != protocol.CmdPushMsg {
		t.Fatalf("captured cmd=%s want=%s", capturedCmd, protocol.CmdPushMsg)
	}

	var pushPayload protocol.PushMsgPayload
	if err := json.Unmarshal(capturedPayload, &pushPayload); err != nil {
		t.Fatalf("unmarshal captured payload: %v", err)
	}
	if pushPayload.MsgID != 10001 {
		t.Fatalf("captured msg_id=%d want=10001", pushPayload.MsgID)
	}
}

func TestConnAckTimeoutSkipsOfflinePushForSenderMirror(t *testing.T) {
	ensureConnTestLogger()
	conn := NewConn(nil)
	conn.SetAuth(1001, "session-sender", "device-sender-mirror", "ios")

	payload, err := json.Marshal(protocol.PushMsgPayload{
		MsgID:     10002,
		SessionID: "session-sender",
		SenderID:  1001,
		MsgType:   1,
		Content:   "self sync",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	called := false
	original := enqueueOfflinePushAfterAckTimeout
	enqueueOfflinePushAfterAckTimeout = func(userID int64, cmd string, payload json.RawMessage) error {
		called = true
		return nil
	}
	defer func() {
		enqueueOfflinePushAfterAckTimeout = original
	}()

	conn.pendingPush[10002] = &pendingPushState{
		cmd:     protocol.CmdPushMsg,
		payload: payload,
		retries: maxPushRetry,
	}

	conn.onPushAckTimeout(10002)

	if called {
		t.Fatal("sender mirror push should not trigger offline push")
	}
}

// ACK 超时补推是离线推送的第二个入口（消息投出去了但客户端不回执），对应 App 退后台
// 和僵尸路由——恰恰是用户最常撞上的场景。AI 连发的多条消息在这条路上也必须并入合并
// 窗口，否则开场白和正文照旧各响一次，去抖就只挡住了没人在线那一半。
//
// 用真实窗口跑：先确认没有立即入队（说明交给了窗口），再确认窗口到点确实推了出去
// （说明不是被吞掉）。窗口变量是 handler 包私有的，这里不为了测试把它导出去。
func TestConnAckTimeoutDebouncesAgentPushIntoWindow(t *testing.T) {
	ensureConnTestLogger()
	conn := NewConn(nil)
	conn.SetAuth(1003, "session-agent-debounce", "device-agent-debounce", "ios")

	payload, err := json.Marshal(protocol.PushMsgPayload{
		MsgID:      10003,
		SessionID:  "session-agent-debounce",
		SenderID:   2001,
		SenderType: 2, // AI agent
		MsgType:    1,
		Content:    "好的，我看一下",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	var mu sync.Mutex
	enqueued := 0
	original := enqueueOfflinePushAfterAckTimeout
	enqueueOfflinePushAfterAckTimeout = func(userID int64, cmd string, payload json.RawMessage) error {
		mu.Lock()
		enqueued++
		mu.Unlock()
		return nil
	}
	defer func() {
		enqueueOfflinePushAfterAckTimeout = original
	}()

	conn.pendingPush[10003] = &pendingPushState{
		cmd:     protocol.CmdPushMsg,
		payload: payload,
		retries: maxPushRetry,
	}

	conn.onPushAckTimeout(10003)

	// 立即检查：应该还没推，已经交给窗口了。
	mu.Lock()
	immediate := enqueued
	mu.Unlock()
	if immediate != 0 {
		t.Fatalf("AI 消息在 ACK 超时后立即入队了 %d 次，应该先并入合并窗口", immediate)
	}

	// 等窗口到点：必须真的推出去，不能被窗口吞掉。
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := enqueued
		mu.Unlock()
		if got > 0 {
			if got != 1 {
				t.Fatalf("窗口到点入队 %d 次，want=1", got)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("窗口到点后仍未入队——消息被窗口吞掉了")
}

// Close 批量补推必须按 msgID 升序派发。这些补推逐条挂进同一个窗口 key，而窗口是
// 「后来者覆盖前者」——若按 map 迭代的随机序派发，最后赢下窗口的就是随机一条，
// 用户有相当概率收到「好的，我看一下」这种开场白，正是这批改动要消灭的症状。
//
// 这里用真人消息（不进窗口、逐条立即入队）来观测派发顺序本身：sort 的契约是
// 「按 msgID 升序派发」，对所有消息一视同仁，能直接测且不必等窗口。
// 必须多跑几轮：Go 小 map 的迭代是随机起始点的循环移位，单轮有较高概率碰巧撞对，
// 轮数少了反证不出来（20 轮足以让漏 sort 的实现几乎必然露馅）。
func TestConnCloseDispatchesPendingPushesInMsgIDOrder(t *testing.T) {
	ensureConnTestLogger()

	const sessionID = "session-close-order"
	contents := []string{"好的，我看一下", "第二句", "第三句", "正文-最新"}

	for round := 0; round < 20; round++ {
		conn := NewConn(nil)
		conn.SetAuth(int64(1005+round), sessionID, "device-close-order", "ios")

		var mu sync.Mutex
		var order []int64
		original := enqueueOfflinePushAfterAckTimeout
		enqueueOfflinePushAfterAckTimeout = func(userID int64, cmd string, payload json.RawMessage) error {
			var pm protocol.PushMsgPayload
			if err := json.Unmarshal(payload, &pm); err != nil {
				return err
			}
			mu.Lock()
			order = append(order, pm.MsgID)
			mu.Unlock()
			return nil
		}

		base := int64(20000 + round*100)
		for i, content := range contents {
			msgID := base + int64(i)
			payload, err := json.Marshal(protocol.PushMsgPayload{
				MsgID:      msgID,
				SessionID:  sessionID,
				SenderID:   2003,
				SenderType: 1, // 真人：不进窗口，逐条立即入队，便于观测派发顺序
				MsgType:    1,
				Content:    content,
			})
			if err != nil {
				enqueueOfflinePushAfterAckTimeout = original
				t.Fatalf("marshal payload: %v", err)
			}
			conn.pendingPush[msgID] = &pendingPushState{
				msgID:   msgID,
				cmd:     protocol.CmdPushMsg,
				payload: payload,
			}
		}

		conn.Close()

		mu.Lock()
		got := append([]int64(nil), order...)
		mu.Unlock()
		enqueueOfflinePushAfterAckTimeout = original

		if len(got) != len(contents) {
			t.Fatalf("第 %d 轮：派发 %d 条，want=%d", round+1, len(got), len(contents))
		}
		for i := 1; i < len(got); i++ {
			if got[i-1] >= got[i] {
				t.Fatalf("第 %d 轮：派发顺序 %v 不是 msgID 升序——最新那条不会最后挂进窗口，"+
					"开场白会赢", round+1, got)
			}
		}
	}
}

// 端到端确认上面那条顺序契约的实际效果：同会话的多条 AI 消息在 Close 补推时被合并
// 成一条，且是最新的正文，不是开场白。
func TestConnCloseMergesAgentPushesIntoLatestContent(t *testing.T) {
	ensureConnTestLogger()

	const sessionID = "session-close-merge"
	conn := NewConn(nil)
	conn.SetAuth(1099, sessionID, "device-close-merge", "ios")

	msgs := []struct {
		msgID   int64
		content string
	}{
		{30001, "好的，我看一下"},
		{30002, "第二句"},
		{30003, "正文-最新"},
	}

	var mu sync.Mutex
	var pushed []string
	original := enqueueOfflinePushAfterAckTimeout
	enqueueOfflinePushAfterAckTimeout = func(userID int64, cmd string, payload json.RawMessage) error {
		var pm protocol.PushMsgPayload
		if err := json.Unmarshal(payload, &pm); err != nil {
			return err
		}
		mu.Lock()
		pushed = append(pushed, pm.Content)
		mu.Unlock()
		return nil
	}
	defer func() {
		enqueueOfflinePushAfterAckTimeout = original
	}()

	for _, m := range msgs {
		payload, err := json.Marshal(protocol.PushMsgPayload{
			MsgID:      m.msgID,
			SessionID:  sessionID,
			SenderID:   2003,
			SenderType: 2, // AI agent：进合并窗口
			MsgType:    1,
			Content:    m.content,
		})
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		conn.pendingPush[m.msgID] = &pendingPushState{
			msgID:   m.msgID,
			cmd:     protocol.CmdPushMsg,
			payload: payload,
		}
	}

	conn.Close()

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		done := len(pushed) > 0
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	mu.Lock()
	got := append([]string(nil), pushed...)
	mu.Unlock()

	if len(got) != 1 {
		t.Fatalf("窗口推出 %d 条 %v，want=1 条（同会话应合并成一条）", len(got), got)
	}
	if got[0] != "正文-最新" {
		t.Fatalf("窗口推出的是 %q，want=%q（开场白赢了窗口）", got[0], "正文-最新")
	}
}

func TestShouldCloseAfterPushAckTimeout(t *testing.T) {
	ensureConnTestLogger()
	tests := []struct {
		name     string
		platform string
		appState string
		want     bool
	}{
		{name: "ios foreground close", platform: "ios", appState: "foreground", want: true},
		{name: "ios background keep", platform: "ios", appState: "background", want: false},
		{name: "android foreground close", platform: "android", appState: "foreground", want: true},
		{name: "android background keep", platform: "android", appState: "background", want: false},
		{name: "ios mixed case background keep", platform: " iOS ", appState: "background", want: false},
		{name: "web close", platform: "web", want: true},
		{name: "desktop close", platform: "macos", want: true},
		{name: "empty close", platform: "", want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			conn := NewConn(nil)
			conn.SetAuth(1001, "session", "device", tc.platform)
			if tc.appState != "" {
				conn.setAppState(tc.appState)
			}
			if got := conn.shouldCloseAfterPushAckTimeout(); got != tc.want {
				t.Fatalf("shouldCloseAfterPushAckTimeout()=%v want=%v platform=%q", got, tc.want, tc.platform)
			}
		})
	}
}

func TestPushAckWaitForConn(t *testing.T) {
	ensureConnTestLogger()
	tests := []struct {
		name     string
		platform string
		appState string
		want     time.Duration
	}{
		{name: "ios foreground wait", platform: "ios", appState: "foreground", want: pushAckWait},
		{name: "ios background wait", platform: "ios", appState: "background", want: pushAckWaitForMobile},
		{name: "android foreground wait", platform: "android", appState: "foreground", want: pushAckWait},
		{name: "android background wait", platform: "android", appState: "background", want: pushAckWaitForMobile},
		{name: "ios mixed case background wait", platform: " iOS ", appState: "background", want: pushAckWaitForMobile},
		{name: "web wait", platform: "web", want: pushAckWait},
		{name: "desktop wait", platform: "macos", want: pushAckWait},
		{name: "empty wait", platform: "", want: pushAckWait},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			conn := NewConn(nil)
			conn.SetAuth(1001, "session", "device", tc.platform)
			if tc.appState != "" {
				conn.setAppState(tc.appState)
			}
			if got := conn.pushAckWaitForConn(); got != tc.want {
				t.Fatalf("pushAckWaitForConn()=%v want=%v platform=%q", got, tc.want, tc.platform)
			}
		})
	}
}

func TestReadDeadlineWindow(t *testing.T) {
	ensureConnTestLogger()
	conn := NewConn(nil)
	conn.SetAuth(1001, "session", "device", "ios")

	if got := conn.readDeadlineWindow(); got != pongWait {
		t.Fatalf("foreground readDeadlineWindow=%v want=%v", got, pongWait)
	}
	conn.setAppState("background")
	if got := conn.readDeadlineWindow(); got != pongWaitForBackground {
		t.Fatalf("background readDeadlineWindow=%v want=%v", got, pongWaitForBackground)
	}
}

func TestMobileFallbackWithoutAppStateReport(t *testing.T) {
	ensureConnTestLogger()
	conn := NewConn(nil)
	conn.SetAuth(1001, "session", "device", "ios")

	conn.lastInboundUnixMs.Store(time.Now().Add(-fallbackIdleForBackground - time.Second).UnixMilli())
	if got := conn.mobileConnRoutingMode(); got != "background_fallback" {
		t.Fatalf("mobileConnRoutingMode()=%s want=background_fallback", got)
	}
	if got := conn.shouldCloseAfterPushAckTimeout(); got {
		t.Fatal("shouldCloseAfterPushAckTimeout()=true want=false under fallback background mode")
	}
	if got := conn.pushAckWaitForConn(); got != pushAckWaitForMobile {
		t.Fatalf("pushAckWaitForConn()=%v want=%v", got, pushAckWaitForMobile)
	}
	if got := conn.readDeadlineWindow(); got != pongWaitForBackground {
		t.Fatalf("readDeadlineWindow()=%v want=%v", got, pongWaitForBackground)
	}
}

func TestSetAppState(t *testing.T) {
	ensureConnTestLogger()
	conn := NewConn(nil)
	if !conn.setAppState("foreground") || conn.appStateString() != "foreground" {
		t.Fatal("expected foreground state")
	}
	if !conn.setAppState("background") || conn.appStateString() != "background" {
		t.Fatal("expected background state")
	}
	if conn.setAppState("invalid") {
		t.Fatal("invalid state should be rejected")
	}
}

func TestIsMobileNormalDisconnect(t *testing.T) {
	ensureConnTestLogger()
	tests := []struct {
		name     string
		platform string
		errCode  int
		want     bool
	}{
		{name: "ios 1006 is normal", platform: "ios", errCode: websocket.CloseAbnormalClosure, want: true},
		{name: "ios 1005 is normal", platform: "ios", errCode: websocket.CloseNoStatusReceived, want: true},
		{name: "iOS mixed case 1006 is normal", platform: " iOS ", errCode: websocket.CloseAbnormalClosure, want: true},
		{name: "android 1006 is normal", platform: "android", errCode: websocket.CloseAbnormalClosure, want: true},
		{name: "android 1005 is normal", platform: "android", errCode: websocket.CloseNoStatusReceived, want: true},
		{name: "web 1006 is not normal", platform: "web", errCode: websocket.CloseAbnormalClosure, want: false},
		{name: "web 1005 is not normal", platform: "web", errCode: websocket.CloseNoStatusReceived, want: false},
		{name: "macos 1006 is not normal", platform: "macos", errCode: websocket.CloseAbnormalClosure, want: false},
		{name: "empty platform 1006 is not normal", platform: "", errCode: websocket.CloseAbnormalClosure, want: false},
		{name: "ios normal close is not abnormal", platform: "ios", errCode: websocket.CloseNormalClosure, want: false},
		{name: "ios going away is not abnormal", platform: "ios", errCode: websocket.CloseGoingAway, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			conn := NewConn(nil)
			conn.SetAuth(1001, "session", "device", tc.platform)
			err := &websocket.CloseError{Code: tc.errCode, Text: "test"}
			if got := conn.isMobileNormalDisconnect(err); got != tc.want {
				t.Fatalf("isMobileNormalDisconnect()=%v want=%v platform=%q code=%d", got, tc.want, tc.platform, tc.errCode)
			}
		})
	}
}

func TestReadPumpLogLevelMobileBackground(t *testing.T) {
	ensureConnTestLogger()

	upgrader := websocket.Upgrader{}
	serverConnCh := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wsConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade error: %v", err)
			return
		}
		serverConnCh <- wsConn
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	var serverWS *websocket.Conn
	select {
	case serverWS = <-serverConnCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server conn")
	}

	// Capture zap output to verify log level.
	var buf bytes.Buffer
	enc := zapcore.NewJSONEncoder(zapcore.EncoderConfig{
		LevelKey:    "level",
		MessageKey:  "msg",
		TimeKey:     "ts",
		LineEnding:  zapcore.DefaultLineEnding,
		EncodeLevel: zapcore.LowercaseLevelEncoder,
		EncodeTime:  zapcore.ISO8601TimeEncoder,
	})
	core := zapcore.NewCore(enc, zapcore.AddSync(&buf), zapcore.DebugLevel)
	savedL := logger.L
	logger.L = zap.New(core, zap.AddCaller()).Sugar()
	defer func() { logger.L = savedL }()

	conn := NewConn(serverWS)
	conn.SetAuth(1001, "session", "device-ios", "ios")

	hub := NewHub("test-node")

	// Client immediately closes -> server ReadPump sees close 1006.
	_ = clientConn.Close()

	done := make(chan struct{})
	go func() {
		conn.ReadPump(hub)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("ReadPump did not exit in time")
	}

	output := buf.String()
	if !strings.Contains(output, `"level":"info"`) {
		t.Fatalf("expected info level log for iOS 1006, got:\n%s", output)
	}
	if !strings.Contains(output, "mobile degraded") {
		t.Fatalf("expected 'mobile degraded' in log, got:\n%s", output)
	}
	if strings.Contains(output, `"level":"warn"`) {
		t.Fatalf("should not have warn level log for iOS 1006, got:\n%s", output)
	}
}

func TestReadPumpLogLevelNonMobileAbnormal(t *testing.T) {
	ensureConnTestLogger()

	upgrader := websocket.Upgrader{}
	serverConnCh := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wsConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade error: %v", err)
			return
		}
		serverConnCh <- wsConn
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	var serverWS *websocket.Conn
	select {
	case serverWS = <-serverConnCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server conn")
	}

	var buf bytes.Buffer
	enc := zapcore.NewJSONEncoder(zapcore.EncoderConfig{
		LevelKey:    "level",
		MessageKey:  "msg",
		TimeKey:     "ts",
		LineEnding:  zapcore.DefaultLineEnding,
		EncodeLevel: zapcore.LowercaseLevelEncoder,
		EncodeTime:  zapcore.ISO8601TimeEncoder,
	})
	core := zapcore.NewCore(enc, zapcore.AddSync(&buf), zapcore.DebugLevel)
	savedL := logger.L
	logger.L = zap.New(core, zap.AddCaller()).Sugar()
	defer func() { logger.L = savedL }()

	conn := NewConn(serverWS)
	conn.SetAuth(1002, "session", "device-web", "web")

	hub := NewHub("test-node")

	_ = clientConn.Close()

	done := make(chan struct{})
	go func() {
		conn.ReadPump(hub)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("ReadPump did not exit in time")
	}

	output := buf.String()
	if !strings.Contains(output, `"level":"warn"`) {
		t.Fatalf("expected warn level log for non-mobile 1006, got:\n%s", output)
	}
	if strings.Contains(output, "mobile background") {
		t.Fatalf("should not have 'mobile background' in log for non-mobile, got:\n%s", output)
	}
}

func TestCloseEnqueuesOfflinePushForPendingMessages(t *testing.T) {
	ensureConnTestLogger()
	conn := NewConn(nil)
	conn.SetAuth(1002, "session-recipient", "device-recipient", "ios")

	payload1, _ := json.Marshal(protocol.PushMsgPayload{
		MsgID:     20001,
		SessionID: "session-recipient",
		SenderID:  1001,
		MsgType:   1,
		Content:   "hello",
	})
	payload2, _ := json.Marshal(protocol.PushMsgPayload{
		MsgID:     20002,
		SessionID: "session-recipient",
		SenderID:  1003,
		MsgType:   1,
		Content:   "world",
	})

	var mu sync.Mutex
	var captured []int64
	original := enqueueOfflinePushAfterAckTimeout
	enqueueOfflinePushAfterAckTimeout = func(userID int64, cmd string, payload json.RawMessage) error {
		mu.Lock()
		captured = append(captured, userID)
		mu.Unlock()
		return nil
	}
	defer func() { enqueueOfflinePushAfterAckTimeout = original }()

	conn.pendingPush[20001] = &pendingPushState{
		cmd:     protocol.CmdPushMsg,
		payload: payload1,
		retries: 0,
	}
	conn.pendingPush[20002] = &pendingPushState{
		cmd:     protocol.CmdPushMsg,
		payload: payload2,
		retries: 1,
	}

	conn.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 2 {
		t.Fatalf("expected 2 offline push calls, got %d", len(captured))
	}
	for _, uid := range captured {
		if uid != 1002 {
			t.Fatalf("captured user_id=%d want=1002", uid)
		}
	}

	// Verify pendingPush is cleared after Close.
	conn.pendingMu.Lock()
	remaining := len(conn.pendingPush)
	conn.pendingMu.Unlock()
	if remaining != 0 {
		t.Fatalf("pendingPush should be empty after Close, got %d entries", remaining)
	}
}

func TestCloseSkipsOfflinePushForSenderMirror(t *testing.T) {
	ensureConnTestLogger()
	conn := NewConn(nil)
	conn.SetAuth(1001, "session-sender", "device-sender-mirror", "ios")

	payload, _ := json.Marshal(protocol.PushMsgPayload{
		MsgID:     20003,
		SessionID: "session-sender",
		SenderID:  1001, // sender == userID
		MsgType:   1,
		Content:   "self sync",
	})

	called := false
	original := enqueueOfflinePushAfterAckTimeout
	enqueueOfflinePushAfterAckTimeout = func(userID int64, cmd string, payload json.RawMessage) error {
		called = true
		return nil
	}
	defer func() { enqueueOfflinePushAfterAckTimeout = original }()

	conn.pendingPush[20003] = &pendingPushState{
		cmd:     protocol.CmdPushMsg,
		payload: payload,
		retries: 0,
	}

	conn.Close()

	if called {
		t.Fatal("sender mirror push should not trigger offline push in Close()")
	}
}

func TestCloseNoopWhenNoPendingPush(t *testing.T) {
	ensureConnTestLogger()
	conn := NewConn(nil)
	conn.SetAuth(1002, "session-recipient", "device-recipient", "ios")

	called := false
	original := enqueueOfflinePushAfterAckTimeout
	enqueueOfflinePushAfterAckTimeout = func(userID int64, cmd string, payload json.RawMessage) error {
		called = true
		return nil
	}
	defer func() { enqueueOfflinePushAfterAckTimeout = original }()

	// No pendingPush entries added.
	conn.Close()

	if called {
		t.Fatal("Close() with no pendingPush should not call offline push")
	}
}

func TestConnPongExtendsReadDeadline(t *testing.T) {
	upgrader := websocket.Upgrader{}
	serverConnCh := make(chan *websocket.Conn, 1)
	serverErrCh := make(chan error, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wsConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			serverErrCh <- err
			return
		}
		serverConnCh <- wsConn
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer clientConn.Close()

	var serverWS *websocket.Conn
	select {
	case serverWS = <-serverConnCh:
	case err := <-serverErrCh:
		t.Fatalf("upgrade websocket: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server websocket")
	}
	defer serverWS.Close()

	conn := NewConn(serverWS)
	if err := conn.ws.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatalf("set initial deadline: %v", err)
	}

	want := `{"cmd":"ping","seq":1,"payload":{}}`
	go func() {
		time.Sleep(10 * time.Millisecond)
		_ = clientConn.WriteControl(websocket.PongMessage, nil, time.Now().Add(time.Second))
		time.Sleep(70 * time.Millisecond)
		_ = clientConn.WriteMessage(websocket.TextMessage, []byte(want))
	}()

	_, got, err := conn.ws.ReadMessage()
	if err != nil {
		t.Fatalf("read message after pong: %v", err)
	}
	if string(got) != want {
		t.Fatalf("message=%s want=%s", string(got), want)
	}
}
