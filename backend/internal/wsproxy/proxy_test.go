package wsproxy

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func TestRegisterRoutesWithTargetProxiesWebSocketEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade upstream websocket: %v", err)
		}
		defer conn.Close()

		_, payload, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read upstream websocket message: %v", err)
		}
		if err := conn.WriteMessage(websocket.TextMessage, []byte(r.URL.Path+"?"+r.URL.RawQuery+":"+string(payload))); err != nil {
			t.Fatalf("write upstream websocket message: %v", err)
		}
	}))
	defer upstream.Close()

	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream url: %v", err)
	}

	engine := gin.New()
	if !registerRoutesWithTarget(engine, target, "/ws", "/v1/widget/ws", "/v1/agent-api/ws") {
		t.Fatalf("expected ws proxy routes to register")
	}

	proxyServer := httptest.NewServer(engine)
	defer proxyServer.Close()

	testCases := []struct {
		path        string
		message     string
		wantMessage string
	}{
		{
			path:        "/ws?client=web",
			message:     "hello",
			wantMessage: "/ws?client=web:hello",
		},
		{
			path:        "/v1/widget/ws?session=widget_1",
			message:     "widget",
			wantMessage: "/v1/widget/ws?session=widget_1:widget",
		},
		{
			path:        "/v1/agent-api/ws?agent_id=42",
			message:     "agent",
			wantMessage: "/v1/agent-api/ws?agent_id=42:agent",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			conn, _, err := websocket.DefaultDialer.Dial(toWebSocketURL(proxyServer.URL)+tc.path, nil)
			if err != nil {
				t.Fatalf("dial websocket via proxy: %v", err)
			}
			defer conn.Close()

			if err := conn.WriteMessage(websocket.TextMessage, []byte(tc.message)); err != nil {
				t.Fatalf("write websocket message: %v", err)
			}

			_, payload, err := conn.ReadMessage()
			if err != nil {
				t.Fatalf("read websocket message: %v", err)
			}
			if string(payload) != tc.wantMessage {
				t.Fatalf("expected message %q, got %q", tc.wantMessage, string(payload))
			}
		})
	}
}

func TestAgentAPIWSPath(t *testing.T) {
	testCases := []struct {
		name     string
		apiPath  string
		wsPath   string
		wantPath string
	}{
		{
			name:     "uses defaults when empty",
			apiPath:  "",
			wsPath:   "",
			wantPath: "/v1/agent-api/ws",
		},
		{
			name:     "normalizes separators",
			apiPath:  "/v1/agent-api/",
			wsPath:   "ws",
			wantPath: "/v1/agent-api/ws",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := agentAPIWSPath(tc.apiPath, tc.wsPath); got != tc.wantPath {
				t.Fatalf("expected path %q, got %q", tc.wantPath, got)
			}
		})
	}
}

func toWebSocketURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http")
}
