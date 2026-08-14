package ws

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

// 僵尸路由清道夫。
//
// 路由表 im:ws:route:<user> 是一个无 TTL 的 hash（field=deviceID，value=nodeID），
// 只在连接正常注销时删除对应 field。ws 进程被强杀（pod 重启、OOM、节点驱逐）时来不及
// 注销，路由就永久残留。存活标记 im:ws:alive:<user>:<device> 有 90s TTL、由心跳续期，
// 是唯一权威的存活信号，因此「有 route 无 alive」即僵尸。
//
// 消息投递路径（collectLiveRouteNodes）已经在读路由时顺手清理僵尸，但只覆盖「还有人给他
// 发消息」的用户；一个断线后再无来往的用户（典型是关掉页面的网站挂件访客）路由会永远留着，
// 污染在线统计、并让跨节点投递向不存在的连接空转。这个清道夫补上这条缝：周期性全量对账。
const (
	routeKeyPrefix       = "im:ws:route:"
	routeJanitorInterval = 5 * time.Minute
	// 锁 key 刻意不落在 routeKeyPrefix 命名空间下（route_janitor 而非 route:janitor），
	// 否则会被自己的 SCAN 匹到。改名前先想清楚这点。
	// 多个 ws 副本只需一个执行：SCAN 是全 keyspace 扫描，重复跑纯属浪费。
	routeJanitorLockKey = "im:ws:route_janitor:lock"
	// 比扫描周期短，锁到点自然过期、下一轮能正常接管，不会因赢家崩溃而永久卡死；
	// 留 30s 余量避免同一周期内两个副本都拿到锁。⚠️ 周期若调到 ≤30s，这里会算出 0 或
	// 负数导致 SetNX 报错、清道夫永久静默失效——改周期时必须一并复核。
	routeJanitorLockTTL   = routeJanitorInterval - 30*time.Second
	routeJanitorScanBatch = 200
)

// StartRouteJanitor 启动僵尸路由清道夫，随 ctx 结束而退出。
func StartRouteJanitor(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(routeJanitorInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pruneZombieRoutesOnce(ctx)
			}
		}
	}()
}

// pruneZombieRoutesOnce 扫一遍所有用户路由，删掉没有存活标记的设备条目。
// 返回被清理的设备条目数，便于测试与观测。
func pruneZombieRoutesOnce(ctx context.Context) int {
	if store.RDB == nil {
		return 0
	}
	if !acquireRouteJanitorLock(ctx) {
		return 0
	}

	pruned := 0
	iter := store.RDB.Scan(ctx, 0, routeKeyPrefix+"*", routeJanitorScanBatch).Iterator()
	for iter.Next(ctx) {
		pruned += pruneRouteKey(ctx, iter.Val())
	}
	if err := iter.Err(); err != nil {
		// 关停时 ctx 取消会让进行中的 Redis 调用报错，不是故障，别刷警告。
		if ctx.Err() == nil {
			logger.L.Warnf("route janitor: scan failed err=%v", err)
		}
		return pruned
	}
	if pruned > 0 {
		logger.L.Infof("route janitor: pruned %d zombie route entries", pruned)
	}
	return pruned
}

func pruneRouteKey(ctx context.Context, routeKey string) int {
	userID, ok := userIDFromRouteKey(routeKey)
	if !ok {
		return 0
	}
	devices, err := store.RDB.HGetAll(ctx, routeKey).Result()
	if err != nil {
		if ctx.Err() == nil {
			logger.L.Warnf("route janitor: read route failed key=%s err=%v", routeKey, err)
		}
		return 0
	}

	// 一个用户的所有设备用一次 pipeline 查存活，避免每设备一次 RTT——路由 key 数量等于
	// 历史连过的用户数，逐个往返会让一轮扫描长到超过锁的有效期。
	pipe := store.RDB.Pipeline()
	deviceIDs := make([]string, 0, len(devices))
	checks := make([]*redis.IntCmd, 0, len(devices))
	for deviceID := range devices {
		deviceIDs = append(deviceIDs, deviceID)
		checks = append(checks, pipe.Exists(ctx, aliveKey(userID, deviceID)))
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		// Redis 抖动时保守放弃本轮：宁可留着僵尸，也不能误删活连接的路由。
		if ctx.Err() == nil {
			logger.L.Warnf("route janitor: check alive failed user=%d err=%v", userID, err)
		}
		return 0
	}

	zombies := make([]string, 0, len(devices))
	for i, cmd := range checks {
		exists, err := cmd.Result()
		if err != nil {
			logger.L.Warnf("route janitor: check alive failed user=%d device=%s err=%v", userID, deviceIDs[i], err)
			continue
		}
		if exists == 0 {
			zombies = append(zombies, deviceIDs[i])
		}
	}
	if len(zombies) == 0 {
		return 0
	}

	if err := store.RDB.HDel(ctx, routeKey, zombies...).Err(); err != nil {
		if ctx.Err() == nil {
			logger.L.Warnf("route janitor: prune failed user=%d devices=%v err=%v", userID, zombies, err)
		}
		return 0
	}
	logger.L.Infof("route janitor: pruned user=%d devices=%v", userID, zombies)
	return len(zombies)
}

// userIDFromRouteKey 解析路由 key 里的用户 ID。解析失败即拒收：清道夫拿错 userID 会去查
// 一个不存在的 alive key，进而把整个 hash 的设备全当僵尸删掉，所以这里只认干净的十进制。
func userIDFromRouteKey(routeKey string) (int64, bool) {
	raw := strings.TrimPrefix(routeKey, routeKeyPrefix)
	// ParseInt 会接受前导 '+'，而写入侧（hub.go 的 %d）永远吐不出这种形状——挡掉，
	// 让解析的严格度与唯一的写入方严丝合缝。
	if raw == routeKey || strings.HasPrefix(raw, "+") {
		return 0, false
	}
	userID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return userID, userID > 0
}

func acquireRouteJanitorLock(ctx context.Context) bool {
	ok, err := store.RDB.SetNX(ctx, routeJanitorLockKey, "1", routeJanitorLockTTL).Result()
	if err != nil {
		logger.L.Warnf("route janitor: acquire lock failed err=%v", err)
		return false
	}
	return ok
}
