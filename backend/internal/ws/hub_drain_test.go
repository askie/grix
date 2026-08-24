package ws

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gorilla/websocket"
)

// 节点关停 drain 必须给存量连接发 1001 going away 关闭帧：
// 客户端据此立即重连到其他节点，而不是等心跳超时才发现连接已死。
func TestHubCloseAllForShutdownSendsGoingAway(t *testing.T) {
	logger.Init()
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = nil
	}()

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
	defer func() { _ = clientConn.Close() }()

	var serverWS *websocket.Conn
	select {
	case serverWS = <-serverConnCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server conn")
	}

	hub := NewHub("node-test")
	conn := NewConn(serverWS)
	conn.SetAuth(4001, "session-1", "web-device-1", "web")
	hub.Register(conn)
	go conn.WritePump()

	hub.CloseAllForShutdown("server shutting down")

	if !conn.closed.Load() {
		t.Fatal("conn should be closed after shutdown drain")
	}

	_ = clientConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, _, readErr := clientConn.ReadMessage()
		if readErr == nil {
			continue
		}
		closeErr, ok := readErr.(*websocket.CloseError)
		if !ok {
			t.Fatalf("expected CloseError, got %v", readErr)
		}
		if closeErr.Code != websocket.CloseGoingAway {
			t.Fatalf("close code=%d want=%d (going away), text=%q", closeErr.Code, websocket.CloseGoingAway, closeErr.Text)
		}
		break
	}
}

// 未设置 drain 关闭码时保持原有行为：空关闭帧（对端看到 1005）。
func TestConnCloseWithoutDrainCodeSendsEmptyCloseFrame(t *testing.T) {
	logger.Init()

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
	defer func() { _ = clientConn.Close() }()

	var serverWS *websocket.Conn
	select {
	case serverWS = <-serverConnCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server conn")
	}

	conn := NewConn(serverWS)
	conn.SetAuth(4002, "session-1", "web-device-1", "web")
	go conn.WritePump()

	conn.Close()

	_ = clientConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, _, readErr := clientConn.ReadMessage()
		if readErr == nil {
			continue
		}
		closeErr, ok := readErr.(*websocket.CloseError)
		if !ok {
			t.Fatalf("expected CloseError, got %v", readErr)
		}
		if closeErr.Code != websocket.CloseNoStatusReceived {
			t.Fatalf("close code=%d want=%d (no status)", closeErr.Code, websocket.CloseNoStatusReceived)
		}
		break
	}
}
