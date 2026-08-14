package agentapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

// TestUpgraderNegotiatesPermessageDeflate 验证 agent-api 的 WebSocket 升级器会与
// 请求压缩的客户端（connector 的 ws 客户端默认就会请求）协商上 permessage-deflate。
// 这是"节省 connector↔服务器流量"改动的回归防线：一旦有人把 EnableCompression 去掉，
// 握手响应里就不会再带 Sec-WebSocket-Extensions，本测试即失败。
func TestUpgraderNegotiatesPermessageDeflate(t *testing.T) {
	m := NewManager("", 0, nil, nil, nil, nil)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := m.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		_ = conn.Close()
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	dialer := websocket.Dialer{EnableCompression: true}
	conn, resp, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	ext := resp.Header.Get("Sec-WebSocket-Extensions")
	if !strings.Contains(ext, "permessage-deflate") {
		t.Fatalf("server did not negotiate permessage-deflate; Sec-WebSocket-Extensions=%q", ext)
	}
}
