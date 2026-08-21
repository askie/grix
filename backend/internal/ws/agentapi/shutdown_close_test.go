package agentapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// 通过真实 websocket 对建立一条 agentConn，返回客户端连接。
func dialAgentConnPair(t *testing.T) (client *websocket.Conn, conn *agentConn) {
	t.Helper()

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
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = clientConn.Close() })

	var serverWS *websocket.Conn
	select {
	case serverWS = <-serverConnCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server conn")
	}

	conn = &agentConn{
		ws:   serverWS,
		send: make(chan []byte, 16),
		done: make(chan struct{}),
	}
	return clientConn, conn
}

func readCloseError(t *testing.T, client *websocket.Conn) *websocket.CloseError {
	t.Helper()
	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, _, readErr := client.ReadMessage()
		if readErr == nil {
			continue
		}
		closeErr, ok := readErr.(*websocket.CloseError)
		if !ok {
			t.Fatalf("expected CloseError, got %v", readErr)
		}
		return closeErr
	}
}

// Manager.Shutdown 置位 shutdownClose 后，writePump 退出必须写 1001 going away
// 关闭帧：连接器据此立即重连到其他节点，而不是等心跳超时。
func TestAgentConnShutdownCloseSendsGoingAway(t *testing.T) {
	client, conn := dialAgentConnPair(t)
	conn.shutdownClose.Store(true)

	go conn.writePump(time.Minute)
	conn.close()

	closeErr := readCloseError(t, client)
	if closeErr.Code != websocket.CloseGoingAway {
		t.Fatalf("close code=%d want=%d (going away), text=%q", closeErr.Code, websocket.CloseGoingAway, closeErr.Text)
	}
}

// 非关停关闭（kick、违规熔断等）保持原有行为：close() 立即关 TCP，
// 对端看到 1006 异常断开，而不是关停专属的 1001。
func TestAgentConnNonShutdownCloseSendsEmptyCloseFrame(t *testing.T) {
	client, conn := dialAgentConnPair(t)

	go conn.writePump(time.Minute)
	conn.close()

	closeErr := readCloseError(t, client)
	if closeErr.Code == websocket.CloseGoingAway {
		t.Fatalf("non-shutdown close must not send %d (going away)", websocket.CloseGoingAway)
	}
}
