package ws

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/gorilla/websocket"
)

func TestWidgetWSAuthSuccessAndRejectsUnsupportedCmd(t *testing.T) {
	logger.Init()
	tdb := testutil.NewTestDB()
	defer tdb.Close()
	store.DB = tdb.DB
	store.RDB = testutil.NewMockRedis()

	jwtpkg.Init("widget-ws-test-secret", 3600, 86400)
	now := time.Now().UTC()
	if err := store.DB.Create(&model.WidgetSession{
		ID:           1,
		SiteID:       9001,
		OwnerUserID:  9101,
		VisitorID:    9201,
		VisitorKey:   "vk_auth_ok",
		SessionID:    "widget-session-1",
		Status:       model.WidgetSessionStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
		LastActiveAt: now,
	}).Error; err != nil {
		t.Fatalf("seed widget session error: %v", err)
	}
	token, _, err := jwtpkg.GenerateWidgetAccessToken(9001, "widget-session-1", 9201, 9101, nil)
	if err != nil {
		t.Fatalf("GenerateWidgetAccessToken() error = %v", err)
	}

	s := NewServer(0, "node-widget-test", "", "", 0, "", true)
	ts := httptest.NewServer(http.HandlerFunc(s.handleWidgetWS))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws error: %v", err)
	}
	defer conn.Close()

	authPayload, _ := json.Marshal(protocol.AuthPayload{Token: token, DeviceID: "widget_browser", Platform: "web_widget"})
	if err := conn.WriteJSON(protocol.Packet{Cmd: protocol.CmdAuth, Seq: 1, Payload: authPayload}); err != nil {
		t.Fatalf("write auth packet error: %v", err)
	}

	var authAck protocol.Packet
	if err := conn.ReadJSON(&authAck); err != nil {
		t.Fatalf("read auth_ack error: %v", err)
	}
	if authAck.Cmd != protocol.CmdAuthAck {
		t.Fatalf("expected auth_ack, got cmd=%s", authAck.Cmd)
	}
	var authAckPayload protocol.AuthAckPayload
	if err := json.Unmarshal(authAck.Payload, &authAckPayload); err != nil {
		t.Fatalf("unmarshal auth_ack payload error: %v", err)
	}
	if authAckPayload.Code != 0 {
		t.Fatalf("auth should success, payload=%+v", authAckPayload)
	}

	if err := conn.WriteJSON(protocol.Packet{Cmd: protocol.CmdSessionRead, Seq: 2, Payload: json.RawMessage(`{"session_id":"widget-session-1"}`)}); err != nil {
		t.Fatalf("write unsupported cmd error: %v", err)
	}
	var nack protocol.Packet
	if err := conn.ReadJSON(&nack); err != nil {
		t.Fatalf("read nack error: %v", err)
	}
	if nack.Cmd != protocol.CmdSendNack {
		t.Fatalf("expected send_nack, got=%s", nack.Cmd)
	}
	var nackPayload protocol.SendNackPayload
	if err := json.Unmarshal(nack.Payload, &nackPayload); err != nil {
		t.Fatalf("unmarshal nack payload error: %v", err)
	}
	if nackPayload.Code != 4004 {
		t.Fatalf("expected nack code 4004, got=%d payload=%+v", nackPayload.Code, nackPayload)
	}
}

func TestWidgetWSRejectsSessionBindingMismatch(t *testing.T) {
	logger.Init()
	tdb := testutil.NewTestDB()
	defer tdb.Close()
	store.DB = tdb.DB
	store.RDB = testutil.NewMockRedis()

	jwtpkg.Init("widget-ws-test-secret-2", 3600, 86400)
	now := time.Now().UTC()
	if err := store.DB.Create(&model.WidgetSession{
		ID:           2,
		SiteID:       9010,
		OwnerUserID:  9110,
		VisitorID:    9210,
		VisitorKey:   "vk_auth_bad",
		SessionID:    "widget-session-2",
		Status:       model.WidgetSessionStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
		LastActiveAt: now,
	}).Error; err != nil {
		t.Fatalf("seed widget session error: %v", err)
	}
	// visitor_id intentionally mismatched
	token, _, err := jwtpkg.GenerateWidgetAccessToken(9010, "widget-session-2", 999999, 9110, nil)
	if err != nil {
		t.Fatalf("GenerateWidgetAccessToken() error = %v", err)
	}

	s := NewServer(0, "node-widget-test-2", "", "", 0, "", true)
	ts := httptest.NewServer(http.HandlerFunc(s.handleWidgetWS))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws error: %v", err)
	}
	defer conn.Close()

	authPayload, _ := json.Marshal(protocol.AuthPayload{Token: token, DeviceID: "widget_browser", Platform: "web_widget"})
	if err := conn.WriteJSON(protocol.Packet{Cmd: protocol.CmdAuth, Seq: 1, Payload: authPayload}); err != nil {
		t.Fatalf("write auth packet error: %v", err)
	}

	var authAck protocol.Packet
	if err := conn.ReadJSON(&authAck); err != nil {
		// Server may close directly on auth failure; this is acceptable.
		return
	}
	if authAck.Cmd != protocol.CmdAuthAck {
		t.Fatalf("expected auth_ack, got cmd=%s", authAck.Cmd)
	}
	var authAckPayload protocol.AuthAckPayload
	if err := json.Unmarshal(authAck.Payload, &authAckPayload); err != nil {
		t.Fatalf("unmarshal auth_ack payload error: %v", err)
	}
	if authAckPayload.Code == 0 {
		t.Fatalf("auth should fail on session binding mismatch, payload=%+v", authAckPayload)
	}
}
