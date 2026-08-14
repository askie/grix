package ws

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/handler"
)

// aliveTTL 是设备存活标记 im:ws:alive:<user>:<device> 的有效期。
// 客户端心跳 30s 一次续期，容忍 3 次丢失。它是判断连接是否真实存活的唯一权威信号：
// 路由表 im:ws:route:<user> 是无 TTL 的 hash，进程被强杀时来不及清理，只能靠它对账。
const aliveTTL = 90 * time.Second

// Hub manages all connections on this node.
type Hub struct {
	mu     sync.RWMutex
	conns  map[int64]map[string]*Conn // map[userID]map[deviceID]*Conn
	nodeID string
	// OnUserAllDevicesOffline 在用户最后一个设备断开时触发。
	// 用于通知 call controller 结束该用户所有进行中的通话。
	OnUserAllDevicesOffline func(userID int64)
	// OnUserRegistered 在用户设备注册后异步触发。
	// 用于检查是否有进行中的 AI 代接通话并重推通知。
	OnUserRegistered func(userID int64)
}

// Ensure Hub implements handler.HubInterface
var _ handler.HubInterface = (*Hub)(nil)

func NewHub(nodeID string) *Hub {
	return &Hub{
		conns:  make(map[int64]map[string]*Conn),
		nodeID: nodeID,
	}
}

func (h *Hub) GetNodeID() string { return h.nodeID }

// CountConns 返回本节点当前人类客户端连接总数（所有用户的所有设备）。
// 供 metrics 采样器读取，反映本节点 WS 人类连接负载。
func (h *Hub) CountConns() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	n := 0
	for _, devices := range h.conns {
		n += len(devices)
	}
	return n
}

func (h *Hub) Register(c handler.ConnInterface) {
	conn := c.(*Conn)
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.conns[conn.userID]; !ok {
		h.conns[conn.userID] = make(map[string]*Conn)
	}

	if existing, ok := h.conns[conn.userID][conn.deviceID]; ok && existing != conn {
		logger.L.Infof("call trace: hub kick_old_conn user=%d device=%s", conn.userID, conn.deviceID)
		existing.Close()
		existing.closeWebsocket()
	}

	h.conns[conn.userID][conn.deviceID] = conn

	// Register in Redis
	// 先写 alive 再写 route：清道夫按「有 route 无 alive」判定僵尸，反过来写会留出
	// 一个新连接被误删路由的窗口。
	ctx := context.Background()
	store.RDB.Set(ctx, aliveKey(conn.userID, conn.deviceID), "1", aliveTTL)
	store.RDB.HSet(ctx, userRouteKey(conn.userID), conn.deviceID, h.nodeID)
	// 同步设备前台状态，供 pickMcpTargetDevice 把 APP 工具指令投给用户在用的那台。
	publishDeviceAppState(conn.userID, conn.deviceID, conn.appStateString())

	logger.L.Infof("user %d device %s registered", conn.userID, conn.deviceID)

	if h.OnUserRegistered != nil {
		userID := conn.userID
		go h.OnUserRegistered(userID)
	}
}

func (h *Hub) Unregister(c handler.ConnInterface) {
	conn := c.(*Conn)
	h.mu.Lock()
	defer h.mu.Unlock()
	logger.L.Infof("user %d device %s unregistered", conn.userID, conn.deviceID)
	h.removeConn(conn)
}

func (h *Hub) removeConn(c *Conn) {
	shouldPruneRoute := false
	allOffline := false
	if devices, ok := h.conns[c.userID]; ok {
		if current, exists := devices[c.deviceID]; exists && current == c {
			delete(devices, c.deviceID)
			shouldPruneRoute = true
			if len(devices) == 0 {
				delete(h.conns, c.userID)
				allOffline = true
			}
		}
	}
	if !shouldPruneRoute {
		return
	}

	ctx := context.Background()
	store.RDB.HDel(ctx, userRouteKey(c.userID), c.deviceID)
	store.RDB.Del(ctx, aliveKey(c.userID, c.deviceID))
	deleteDeviceAppState(c.userID, c.deviceID)

	// 设备断开：释放该设备可能持有的语音通话参与锁，让 owner 其它设备可重新接入。
	handler.ReleaseParticipateOnDisconnect(c.userID, c.deviceID)

	// 用户本节点所有设备已离线：再查 Redis 确认其他节点也没有该用户的连接，
	// 避免多 WS 副本下本节点误判用户全离线而杀掉通话。
	if allOffline && h.OnUserAllDevicesOffline != nil {
		userID := c.userID
		go func() {
			online := isUserOnlineAnywhere(userID)
			logger.L.Infof("call trace: hub all_offline user=%d online_anywhere=%v", userID, online)
			if online {
				return
			}
			h.OnUserAllDevicesOffline(userID)
		}()
	}
}

func (h *Hub) GetUserConns(userID int64) []handler.ConnInterface {
	h.mu.RLock()
	defer h.mu.RUnlock()
	devices, ok := h.conns[userID]
	if !ok {
		return nil
	}
	result := make([]handler.ConnInterface, 0, len(devices))
	for _, c := range devices {
		result = append(result, c)
	}
	return result
}

// RefreshAlive 在收到客户端心跳时续期存活标记，并顺带幂等重建路由。
// 重建是必要的：设备挂起超过 aliveTTL 后 alive 会过期，路由随即被清道夫或投递侧的
// 懒清理删掉；此时连接其实还活着，若不在心跳里补回路由，该设备将永远收不到跨节点消息。
func (h *Hub) RefreshAlive(c handler.ConnInterface) {
	conn := c.(*Conn)
	ctx := context.Background()
	store.RDB.Set(ctx, aliveKey(conn.userID, conn.deviceID), "1", aliveTTL)
	store.RDB.HSet(ctx, userRouteKey(conn.userID), conn.deviceID, h.nodeID)
}

func userRouteKey(userID int64) string {
	return fmt.Sprintf("%s%d", routeKeyPrefix, userID)
}

func aliveKey(userID int64, deviceID string) string {
	return fmt.Sprintf("im:ws:alive:%d:%s", userID, deviceID)
}

// isUserOnlineAnywhere 查 Redis alive keys 判断用户是否在任意 WS 节点仍有连接。
// 用于多副本场景：本节点设备全部断开后，需确认其他节点也没有该用户连接，
// 才能安全触发离线回调（如 CleanupUserCalls）。
func isUserOnlineAnywhere(userID int64) bool {
	if store.RDB == nil {
		return false
	}
	ctx := context.Background()
	pattern := fmt.Sprintf("im:ws:alive:%d:*", userID)
	iter := store.RDB.Scan(ctx, 0, pattern, 10).Iterator()
	for iter.Next(ctx) {
		return true
	}
	if err := iter.Err(); err != nil {
		logger.L.Warnf("hub: check user online anywhere failed user=%d err=%v", userID, err)
		// Redis 故障时保守处理：不确定则认为用户仍在线，避免误杀通话
		return true
	}
	return false
}
