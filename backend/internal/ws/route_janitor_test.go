package ws

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func setupJanitorRedis(t *testing.T) {
	t.Helper()
	logger.Init()
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		_ = store.RDB.Close()
		store.RDB = nil
	})
}

// 有路由但存活标记已过期 = 僵尸，必须清掉。
func TestRouteJanitorPrunesRouteWithoutAliveKey(t *testing.T) {
	setupJanitorRedis(t)
	ctx := context.Background()

	store.RDB.HSet(ctx, userRouteKey(2001), "dead-device", "node-a")

	if pruned := pruneZombieRoutesOnce(ctx); pruned != 1 {
		t.Fatalf("pruned=%d want=1", pruned)
	}

	n, err := store.RDB.Exists(ctx, userRouteKey(2001)).Result()
	if err != nil {
		t.Fatalf("exists error: %v", err)
	}
	if n != 0 {
		t.Fatalf("route key still exists after all fields pruned")
	}
}

// 活连接（Register 写了存活标记）的路由不能被误删。
func TestRouteJanitorKeepsLiveRoute(t *testing.T) {
	setupJanitorRedis(t)
	ctx := context.Background()

	hub := NewHub("node-a")
	conn := NewConn(nil)
	conn.SetAuth(2002, "session-1", "live-device", "ios")
	hub.Register(conn)

	if pruned := pruneZombieRoutesOnce(ctx); pruned != 0 {
		t.Fatalf("pruned=%d want=0 (live route must survive)", pruned)
	}

	node, err := store.RDB.HGet(ctx, userRouteKey(2002), "live-device").Result()
	if err != nil {
		t.Fatalf("load route error: %v", err)
	}
	if node != "node-a" {
		t.Fatalf("route=%q want=node-a", node)
	}
}

// 同一用户的僵尸设备被清、活设备保留。
func TestRouteJanitorPrunesOnlyZombieDevices(t *testing.T) {
	setupJanitorRedis(t)
	ctx := context.Background()

	hub := NewHub("node-a")
	conn := NewConn(nil)
	conn.SetAuth(2003, "session-1", "live-device", "macos")
	hub.Register(conn)
	store.RDB.HSet(ctx, userRouteKey(2003), "dead-device", "node-b")

	if pruned := pruneZombieRoutesOnce(ctx); pruned != 1 {
		t.Fatalf("pruned=%d want=1", pruned)
	}

	devices, err := store.RDB.HGetAll(ctx, userRouteKey(2003)).Result()
	if err != nil {
		t.Fatalf("load routes error: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("devices=%v want only live-device", devices)
	}
	if _, ok := devices["live-device"]; !ok {
		t.Fatalf("live device route was pruned: %v", devices)
	}
}

// 分布式锁：同一周期内只有一个节点真正执行扫描。
func TestRouteJanitorLockSkipsSecondNode(t *testing.T) {
	setupJanitorRedis(t)
	ctx := context.Background()

	store.RDB.HSet(ctx, userRouteKey(2004), "dead-a", "node-a")
	store.RDB.HSet(ctx, userRouteKey(2005), "dead-b", "node-b")

	if pruned := pruneZombieRoutesOnce(ctx); pruned != 2 {
		t.Fatalf("first run pruned=%d want=2", pruned)
	}

	store.RDB.HSet(ctx, userRouteKey(2006), "dead-c", "node-c")
	if pruned := pruneZombieRoutesOnce(ctx); pruned != 0 {
		t.Fatalf("second run pruned=%d want=0 (lock still held)", pruned)
	}
}

// 心跳必须幂等重建路由：设备挂起导致存活标记过期、路由被清后，
// 心跳恢复要能把路由补回来，否则该设备永远收不到跨节点消息。
func TestRefreshAliveRebuildsPrunedRoute(t *testing.T) {
	setupJanitorRedis(t)
	ctx := context.Background()

	hub := NewHub("node-a")
	conn := NewConn(nil)
	conn.SetAuth(2007, "session-1", "suspended-device", "ios")
	hub.Register(conn)

	// 模拟设备长时间挂起：存活标记过期，清道夫清掉路由
	store.RDB.Del(ctx, aliveKey(2007, "suspended-device"))
	if pruned := pruneZombieRoutesOnce(ctx); pruned != 1 {
		t.Fatalf("pruned=%d want=1", pruned)
	}

	// 设备恢复，心跳到达
	hub.RefreshAlive(conn)

	node, err := store.RDB.HGet(ctx, userRouteKey(2007), "suspended-device").Result()
	if err != nil {
		t.Fatalf("route not rebuilt by heartbeat: %v", err)
	}
	if node != "node-a" {
		t.Fatalf("rebuilt route=%q want=node-a", node)
	}
}

// 用户 ID 解析必须拒收一切脏 key：解析出错会导致查错 alive key，
// 进而把整个 hash 的设备全当僵尸删掉。溢出是其中最隐蔽的一条。
func TestUserIDFromRouteKeyRejectsMalformed(t *testing.T) {
	bad := []string{
		"im:ws:route:99999999999999999999", // 溢出 int64
		"im:ws:route:abc",
		"im:ws:route:12ab34",
		"im:ws:route:",
		"im:ws:route:-1",
		"im:ws:route:0",
		"im:ws:route: 123",
		"im:ws:route:+123", // ParseInt 认，但写入侧永远吐不出，挡掉
		"im:ws:route:1:2",
		"im:ws:alive:123:dev",
	}
	for _, key := range bad {
		if _, ok := userIDFromRouteKey(key); ok {
			t.Fatalf("key %q should be rejected", key)
		}
	}
	if id, ok := userIDFromRouteKey("im:ws:route:2062532125415968768"); !ok || id != 2062532125415968768 {
		t.Fatalf("valid key parsed as id=%d ok=%v", id, ok)
	}
}

// 脏 key 不能被清道夫动手（原样保留，交由运维处理）。
func TestRouteJanitorLeavesMalformedKeyUntouched(t *testing.T) {
	setupJanitorRedis(t)
	ctx := context.Background()

	store.RDB.HSet(ctx, "im:ws:route:abc", "dead-device", "node-a")

	if pruned := pruneZombieRoutesOnce(ctx); pruned != 0 {
		t.Fatalf("pruned=%d want=0 (malformed key must be left alone)", pruned)
	}
	n, _ := store.RDB.Exists(ctx, "im:ws:route:abc").Result()
	if n != 1 {
		t.Fatalf("malformed key was removed")
	}
}

// 多设备走 pipeline 批量查存活：活的一个不能少，僵尸一个不能留。
func TestRouteJanitorPipelineHandlesManyDevices(t *testing.T) {
	setupJanitorRedis(t)
	ctx := context.Background()

	hub := NewHub("node-a")
	for i := 0; i < 5; i++ {
		conn := NewConn(nil)
		conn.SetAuth(2009, "session", "live-"+strconv.Itoa(i), "ios")
		hub.Register(conn)
	}
	for i := 0; i < 7; i++ {
		store.RDB.HSet(ctx, userRouteKey(2009), "dead-"+strconv.Itoa(i), "node-b")
	}

	if pruned := pruneZombieRoutesOnce(ctx); pruned != 7 {
		t.Fatalf("pruned=%d want=7", pruned)
	}
	devices, _ := store.RDB.HGetAll(ctx, userRouteKey(2009)).Result()
	if len(devices) != 5 {
		t.Fatalf("surviving devices=%v want 5 live", devices)
	}
	for dev := range devices {
		if !strings.HasPrefix(dev, "live-") {
			t.Fatalf("zombie device %q survived", dev)
		}
	}
}

// 注销后路由与存活标记都要清干净（回归保护）。
func TestUnregisterClearsRouteAndAlive(t *testing.T) {
	setupJanitorRedis(t)
	ctx := context.Background()

	hub := NewHub("node-a")
	conn := NewConn(nil)
	conn.SetAuth(2008, "session-1", "device-1", "web")
	hub.Register(conn)
	hub.Unregister(conn)

	routeN, _ := store.RDB.Exists(ctx, userRouteKey(2008)).Result()
	aliveN, _ := store.RDB.Exists(ctx, aliveKey(2008, "device-1")).Result()
	if routeN != 0 || aliveN != 0 {
		t.Fatalf("unregister left residue route=%d alive=%d", routeN, aliveN)
	}
}
