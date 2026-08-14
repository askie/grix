package ws

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func TestHubRegisterAllowsMultipleConnectionsOnSamePlatform(t *testing.T) {
	logger.Init()
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = nil
	}()

	hub := NewHub("node-test")
	first := NewConn(nil)
	first.SetAuth(1001, "session-1", "ios-device-1", "ios")
	second := NewConn(nil)
	second.SetAuth(1001, "session-2", "ios-device-2", "ios")

	hub.Register(first)
	hub.Register(second)

	conns := hub.GetUserConns(1001)
	if len(conns) != 2 {
		t.Fatalf("GetUserConns len=%d want=2", len(conns))
	}

	ctx := context.Background()
	routeKey := "im:ws:route:1001"
	firstRoute, err := store.RDB.HGet(ctx, routeKey, "ios-device-1").Result()
	if err != nil {
		t.Fatalf("load first route error: %v", err)
	}
	if firstRoute != "node-test" {
		t.Fatalf("first route=%q want=node-test", firstRoute)
	}

	secondRoute, err := store.RDB.HGet(ctx, routeKey, "ios-device-2").Result()
	if err != nil {
		t.Fatalf("load second route error: %v", err)
	}
	if secondRoute != "node-test" {
		t.Fatalf("second route=%q want=node-test", secondRoute)
	}
}

func TestHubUnregisterDoesNotRemoveReplacedSameDeviceConn(t *testing.T) {
	logger.Init()
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = nil
	}()

	hub := NewHub("node-test")
	oldConn := NewConn(nil)
	oldConn.SetAuth(2001, "session-old", "ios-device-1", "ios")
	newConn := NewConn(nil)
	newConn.SetAuth(2001, "session-new", "ios-device-1", "ios")

	hub.Register(oldConn)
	hub.Register(newConn)
	hub.Unregister(oldConn)

	conns := hub.GetUserConns(2001)
	if len(conns) != 1 {
		t.Fatalf("GetUserConns len=%d want=1", len(conns))
	}
	if conns[0] != newConn {
		t.Fatalf("active conn should remain new conn")
	}

	ctx := context.Background()
	routeKey := "im:ws:route:2001"
	routeNode, err := store.RDB.HGet(ctx, routeKey, "ios-device-1").Result()
	if err != nil {
		t.Fatalf("load route after unregister old conn error: %v", err)
	}
	if routeNode != "node-test" {
		t.Fatalf("route node=%q want=node-test", routeNode)
	}

	aliveExists, err := store.RDB.Exists(ctx, "im:ws:alive:2001:ios-device-1").Result()
	if err != nil {
		t.Fatalf("check alive key exists error: %v", err)
	}
	if aliveExists != 1 {
		t.Fatalf("alive key exists=%d want=1", aliveExists)
	}

	hub.Unregister(newConn)

	routeExists, err := store.RDB.HExists(ctx, routeKey, "ios-device-1").Result()
	if err != nil {
		t.Fatalf("check route exists after unregister new conn error: %v", err)
	}
	if routeExists {
		t.Fatalf("route should be removed after unregister new conn")
	}

	aliveExists, err = store.RDB.Exists(ctx, "im:ws:alive:2001:ios-device-1").Result()
	if err != nil {
		t.Fatalf("check alive key after unregister new conn error: %v", err)
	}
	if aliveExists != 0 {
		t.Fatalf("alive key exists=%d want=0", aliveExists)
	}
}

func TestHubRegisterClosesPreviousConnOnSameDeviceReplacement(t *testing.T) {
	logger.Init()
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = nil
	}()

	hub := NewHub("node-test")
	oldConn := NewConn(nil)
	oldConn.SetAuth(3001, "session-old", "web-device-1", "web")
	newConn := NewConn(nil)
	newConn.SetAuth(3001, "session-new", "web-device-1", "web")

	hub.Register(oldConn)
	if oldConn.closed.Load() {
		t.Fatal("old conn should remain open before replacement")
	}

	hub.Register(newConn)

	if !oldConn.closed.Load() {
		t.Fatal("expected previous same-device conn to be closed on replacement")
	}
	conns := hub.GetUserConns(3001)
	if len(conns) != 1 {
		t.Fatalf("GetUserConns len=%d want=1", len(conns))
	}
	if conns[0] != newConn {
		t.Fatal("active conn should be the replacement conn")
	}
}
