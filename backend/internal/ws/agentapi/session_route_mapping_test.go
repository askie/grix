package agentapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func setupSessionRouteMappingTest(t *testing.T) func() {
	t.Helper()
	previousDB := store.DB
	previousRDB := store.RDB

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	return func() {
		_ = store.RDB.Close()
		testDB.Close()
		store.DB = previousDB
		store.RDB = previousRDB
	}
}

func seedSessionWithAgentMember(t *testing.T, sessionID string, ownerID, peerID, agentID int64) {
	t.Helper()
	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: peerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
	}
	if err := store.DB.Create(&members).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}
}

func decodeAgentPacket(t *testing.T, raw []byte) (protocol.Packet, map[string]any) {
	t.Helper()
	var pkt protocol.Packet
	if err := json.Unmarshal(raw, &pkt); err != nil {
		t.Fatalf("unmarshal packet error: %v", err)
	}
	payload := map[string]any{}
	if len(pkt.Payload) > 0 {
		if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
			t.Fatalf("unmarshal packet payload error: %v", err)
		}
	}
	return pkt, payload
}

func TestSessionRouteBindAndResolve(t *testing.T) {
	cleanup := setupSessionRouteMappingTest(t)
	defer cleanup()

	const (
		agentID   = int64(9992)
		ownerID   = int64(1001)
		peerID    = int64(2002)
		sessionID = "03d66ef4-9ad0-41e6-921c-49750e604c46"
	)
	seedSessionWithAgentMember(t, sessionID, ownerID, peerID, agentID)

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:  agentID,
		ownerID:  ownerID,
		clientID: "test",
		send:     make(chan []byte, 8),
	}

	bindPkt := makePacket(t, cmdSessionRouteBind, 1, SessionRouteBindPayload{
		Channel:         "grix",
		AccountID:       "default",
		RouteSessionKey: "route-key-a",
		SessionID:       sessionID,
	})
	mgr.handleSessionRouteBind(conn, bindPkt)

	rawBindResp := <-conn.send
	bindRespPkt, bindRespPayload := decodeAgentPacket(t, rawBindResp)
	if bindRespPkt.Cmd != "send_ack" {
		t.Fatalf("bind expected send_ack, got=%s payload=%v", bindRespPkt.Cmd, bindRespPayload)
	}
	if bindRespPayload["session_id"] != sessionID {
		t.Fatalf("bind ack session_id=%v want=%s", bindRespPayload["session_id"], sessionID)
	}

	resolvePkt := makePacket(t, cmdSessionRouteResolve, 2, SessionRouteResolvePayload{
		Channel:         "grix",
		AccountID:       "default",
		RouteSessionKey: "route-key-a",
	})
	mgr.handleSessionRouteResolve(conn, resolvePkt)

	rawResolveResp := <-conn.send
	resolveRespPkt, resolveRespPayload := decodeAgentPacket(t, rawResolveResp)
	if resolveRespPkt.Cmd != "send_ack" {
		t.Fatalf("resolve expected send_ack, got=%s payload=%v", resolveRespPkt.Cmd, resolveRespPayload)
	}
	if resolveRespPayload["session_id"] != sessionID {
		t.Fatalf("resolve ack session_id=%v want=%s", resolveRespPayload["session_id"], sessionID)
	}
}

func TestSessionRouteBindSupportsOneToManyByRouteKey(t *testing.T) {
	cleanup := setupSessionRouteMappingTest(t)
	defer cleanup()

	const (
		agentID   = int64(9993)
		ownerID   = int64(1002)
		peerID    = int64(2003)
		sessionID = "49dc128a-1c7c-4750-b739-d0d4076ea1b5"
	)
	seedSessionWithAgentMember(t, sessionID, ownerID, peerID, agentID)

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:  agentID,
		ownerID:  ownerID,
		clientID: "test",
		send:     make(chan []byte, 8),
	}

	for i, routeKey := range []string{"route-key-a", "route-key-b"} {
		bindPkt := makePacket(t, cmdSessionRouteBind, int64(i+1), SessionRouteBindPayload{
			Channel:         "grix",
			AccountID:       "default",
			RouteSessionKey: routeKey,
			SessionID:       sessionID,
		})
		mgr.handleSessionRouteBind(conn, bindPkt)
		rawResp := <-conn.send
		respPkt, respPayload := decodeAgentPacket(t, rawResp)
		if respPkt.Cmd != "send_ack" {
			t.Fatalf("bind route=%s expected send_ack, got=%s payload=%v", routeKey, respPkt.Cmd, respPayload)
		}
	}

	var rows []model.AgentSessionRouteMapping
	if err := store.DB.Where("agent_id = ? AND session_id = ?", agentID, sessionID).Order("route_session_key ASC").Find(&rows).Error; err != nil {
		t.Fatalf("query mapping rows error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("mapping row count=%d want=2 rows=%#v", len(rows), rows)
	}
	if rows[0].RouteSessionKey == rows[1].RouteSessionKey {
		t.Fatalf("expected distinct route_session_key rows, got=%#v", rows)
	}
}

func TestSessionRouteResolveNotFound(t *testing.T) {
	cleanup := setupSessionRouteMappingTest(t)
	defer cleanup()

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:  9994,
		ownerID:  1003,
		clientID: "test",
		send:     make(chan []byte, 4),
	}

	resolvePkt := makePacket(t, cmdSessionRouteResolve, 1, SessionRouteResolvePayload{
		Channel:         "grix",
		AccountID:       "default",
		RouteSessionKey: "missing-key",
	})
	mgr.handleSessionRouteResolve(conn, resolvePkt)

	rawResp := <-conn.send
	respPkt, respPayload := decodeAgentPacket(t, rawResp)
	if respPkt.Cmd != "send_nack" {
		t.Fatalf("expected send_nack, got=%s payload=%v", respPkt.Cmd, respPayload)
	}
	if respPayload["code"] != float64(4044) {
		t.Fatalf("nack code=%v want=4044", respPayload["code"])
	}
}

func TestSessionRouteBindPermissionDenied(t *testing.T) {
	cleanup := setupSessionRouteMappingTest(t)
	defer cleanup()

	const (
		agentID   = int64(9995)
		ownerID   = int64(1004)
		peerID    = int64(2004)
		sessionID = "8503b116-4735-40f2-ab5a-7ed968bd5993"
	)
	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	// Intentionally do not add agent member (member_type=2) to trigger permission denied.
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: peerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
	}
	if err := store.DB.Create(&members).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:  agentID,
		ownerID:  ownerID,
		clientID: "test",
		send:     make(chan []byte, 4),
	}

	bindPkt := makePacket(t, cmdSessionRouteBind, 1, SessionRouteBindPayload{
		Channel:         "grix",
		AccountID:       "default",
		RouteSessionKey: "route-key-denied",
		SessionID:       sessionID,
	})
	mgr.handleSessionRouteBind(conn, bindPkt)

	rawResp := <-conn.send
	respPkt, respPayload := decodeAgentPacket(t, rawResp)
	if respPkt.Cmd != "send_nack" {
		t.Fatalf("expected send_nack, got=%s payload=%v", respPkt.Cmd, respPayload)
	}
	if respPayload["code"] != float64(4003) {
		t.Fatalf("nack code=%v want=4003", respPayload["code"])
	}
}
