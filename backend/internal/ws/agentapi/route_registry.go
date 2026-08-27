package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	pkgagentapi "github.com/askie/grix/backend/internal/pkg/agentapi"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/redis/go-redis/v9"
)

const (
	redisCmdForwardDelegateEvent = "_agent_api_delegate_event"
	redisCmdForwardDelegateRetry = "_agent_api_delegate_retry"
)

var clearAgentRouteScript = redis.NewScript(`
local current = redis.call("GET", KEYS[1])
if current == ARGV[1] then
  redis.call("DEL", KEYS[2])
  return redis.call("DEL", KEYS[1])
end
return 0
`)

var clearAgentRouteOwnerScript = redis.NewScript(`
local current = redis.call("GET", KEYS[1])
if current == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

// claimAgentConnectionAuthorityScript freezes the authoritative websocket
// generation for one (agent, owner) identity before auth_ack is sent. The
// authority hash intentionally has no TTL: an old TCP connection must not be
// able to resurrect after the newer connection disconnects and its route lease
// expires. Future handshakes can always replace it with a larger epoch.
//
// KEYS:
//
//	1 authority hash, 2 owner route, 3 primary route, 4 route owner set,
//	5 owner capabilities, 6 primary capabilities, 7 primary runtime profile,
//	8 connection info, 9 owner runtime profile.
var claimAgentConnectionAuthorityScript = redis.NewScript(`
local current_epoch = tonumber(redis.call("HGET", KEYS[1], "connection_epoch") or "0") or 0
local incoming_epoch = tonumber(ARGV[2]) or 0
if incoming_epoch <= 0 or current_epoch >= incoming_epoch then
  return 0
end

redis.call("HSET", KEYS[1],
  "node_id", ARGV[1],
  "connection_epoch", incoming_epoch,
  "active", "1")
redis.call("SET", KEYS[2], ARGV[1], "EX", ARGV[3])
redis.call("SADD", KEYS[4], ARGV[4])
redis.call("EXPIRE", KEYS[4], tonumber(ARGV[3]) + 30)

-- A new generation starts without metadata inherited from its predecessor.
-- Its first lease refresh repopulates the values it actually declared.
redis.call("DEL", KEYS[5], KEYS[8], KEYS[9])
if ARGV[5] == "1" then
  redis.call("SET", KEYS[3], ARGV[1], "EX", ARGV[3])
  redis.call("DEL", KEYS[6], KEYS[7])
end
return 1
`)

// refreshAgentConnectionAuthorityScript renews route leases only while both
// node and epoch still match the active authority generation. This is the
// fence that prevents a heartbeat from an old TCP socket on another node from
// taking the route back.
var refreshAgentConnectionAuthorityScript = redis.NewScript(`
local current_node = redis.call("HGET", KEYS[1], "node_id")
local current_epoch = tonumber(redis.call("HGET", KEYS[1], "connection_epoch") or "0") or 0
local active = redis.call("HGET", KEYS[1], "active")
local incoming_epoch = tonumber(ARGV[2]) or 0
if current_node ~= ARGV[1] or current_epoch ~= incoming_epoch or active ~= "1" then
  return 0
end

redis.call("SET", KEYS[2], ARGV[1], "EX", ARGV[3])
redis.call("SADD", KEYS[4], ARGV[4])
redis.call("EXPIRE", KEYS[4], tonumber(ARGV[3]) + 30)
if ARGV[5] == "1" then
  redis.call("SET", KEYS[3], ARGV[1], "EX", ARGV[3])
end
return 1
`)

// setAgentConnectionMetadataScript performs the authority check and metadata
// write in one Redis operation. A separate "check then SET" would retain a
// TOCTOU window in which a successor could claim authority between the two.
var setAgentConnectionMetadataScript = redis.NewScript(`
local current_node = redis.call("HGET", KEYS[1], "node_id")
local current_epoch = tonumber(redis.call("HGET", KEYS[1], "connection_epoch") or "0") or 0
local active = redis.call("HGET", KEYS[1], "active")
local incoming_epoch = tonumber(ARGV[2]) or 0
if current_node ~= ARGV[1] or current_epoch ~= incoming_epoch or active ~= "1" then
  return 0
end

if ARGV[4] == "1" then
  redis.call("SET", KEYS[2], ARGV[5], "EX", ARGV[3])
else
  redis.call("DEL", KEYS[2])
end
if ARGV[6] == "1" then
  redis.call("SET", KEYS[3], ARGV[7], "EX", ARGV[3])
end
return 1
`)

// releaseAgentConnectionAuthorityScript is the cleanup-side fence. Only the
// exact active generation may mark itself inactive or remove leased metadata.
// A stale unregister therefore cannot delete its successor's routes,
// capabilities, runtime snapshot, or connection info.
var releaseAgentConnectionAuthorityScript = redis.NewScript(`
local current_node = redis.call("HGET", KEYS[1], "node_id")
local current_epoch = tonumber(redis.call("HGET", KEYS[1], "connection_epoch") or "0") or 0
local active = redis.call("HGET", KEYS[1], "active")
local incoming_epoch = tonumber(ARGV[2]) or 0
if current_node ~= ARGV[1] or current_epoch ~= incoming_epoch or active ~= "1" then
  return 0
end

redis.call("HSET", KEYS[1], "active", "0")
if redis.call("GET", KEYS[2]) == ARGV[1] then
  redis.call("DEL", KEYS[2])
end
redis.call("DEL", KEYS[5], KEYS[8], KEYS[9])
if ARGV[3] == "1" then
  if redis.call("GET", KEYS[3]) == ARGV[1] then
    redis.call("DEL", KEYS[3])
  end
  redis.call("DEL", KEYS[6], KEYS[7])
end
return 1
`)

// agentRouteKey 仅按 agentID 的"主路由 key"。代表 agent 主连接所在的节点。
// 兼容旧调用点(无 owner 上下文),新代码请用 agentRouteKeyForOwner / loadAgentRouteForOwner。
func agentRouteKey(agentID int64) string {
	return fmt.Sprintf("im:agent_api:route:%d", agentID)
}

// agentRouteKeyForOwner 按 (agentID, ownerID) 的"owner 路由 key"。
// agent 共享多连接物理隔离下,每个 owner 的连接独立记自己所在的节点,
// 避免单值 key 在 A 主连接与 B 共享连接散到不同节点时互相覆盖。
func agentRouteKeyForOwner(agentID, ownerID int64) string {
	return fmt.Sprintf("im:agent_api:route:%d:%d", agentID, ownerID)
}

// agentRouteOwnerSetKey 记录该 agent 当前有连接的所有 ownerID 集合,
// 用于撤销共享 / KickAgent 等场景下跨节点扫描该 agent 的全部 owner 路由。
func agentRouteOwnerSetKey(agentID int64) string {
	return fmt.Sprintf("im:agent_api:route_owners:%d", agentID)
}

// agentConnectionAuthorityKey stores the no-TTL node+epoch fencing token for
// one physical owner-scoped connector connection.
func agentConnectionAuthorityKey(agentID, ownerID int64) string {
	return fmt.Sprintf("im:agent_api:authority:%d:%d", agentID, ownerID)
}

// agentCapabilitiesKey 主能力 key(无 owner 维度)。兼容旧 caller,主连接写、回退查。
func agentCapabilitiesKey(agentID int64) string {
	return fmt.Sprintf("im:agent_api:capabilities:%d", agentID)
}

// agentCapabilitiesKeyForOwner 按 (agentID, ownerID) 的能力 key。
// agent 共享场景下不同 connector 版本可能上报不同的 local_actions(A 已升 B 未升或反之),
// 按 owner 区分,避免互相覆盖、漏发或误发。
func agentCapabilitiesKeyForOwner(agentID, ownerID int64) string {
	return fmt.Sprintf("im:agent_api:capabilities:%d:%d", agentID, ownerID)
}

type agentConnectionAuthority struct {
	NodeID          string
	ConnectionEpoch int64
	Active          bool
}

func authorityTTLSeconds(ttl time.Duration) int64 {
	seconds := int64(ttl / time.Second)
	if seconds <= 0 {
		return 1
	}
	return seconds
}

func loadAgentConnectionAuthority(
	ctx context.Context,
	agentID, ownerID int64,
) (agentConnectionAuthority, bool, error) {
	if agentID <= 0 || ownerID <= 0 || store.RDB == nil {
		return agentConnectionAuthority{}, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	values, err := store.RDB.HMGet(
		ctx,
		agentConnectionAuthorityKey(agentID, ownerID),
		"node_id",
		"connection_epoch",
		"active",
	).Result()
	if err != nil {
		return agentConnectionAuthority{}, false, err
	}
	if len(values) != 3 || values[0] == nil || values[1] == nil {
		return agentConnectionAuthority{}, false, nil
	}
	nodeID := strings.TrimSpace(fmt.Sprint(values[0]))
	epoch, err := strconv.ParseInt(strings.TrimSpace(fmt.Sprint(values[1])), 10, 64)
	if err != nil || nodeID == "" || epoch <= 0 {
		return agentConnectionAuthority{}, false, nil
	}
	return agentConnectionAuthority{
		NodeID:          nodeID,
		ConnectionEpoch: epoch,
		Active:          strings.TrimSpace(fmt.Sprint(values[2])) == "1",
	}, true, nil
}

func (m *Manager) claimAgentConnectionAuthority(conn *agentConn, ttl time.Duration) (bool, error) {
	if conn == nil || conn.agentID <= 0 || conn.ownerID <= 0 {
		return false, nil
	}
	// Epoch zero is retained only for standalone managers and legacy tests.
	// Server.Start always installs the Redis allocator and therefore always
	// enters the fenced path below.
	if conn.connectionEpoch <= 0 || store.RDB == nil || strings.TrimSpace(m.getNodeID()) == "" {
		return true, nil
	}
	nodeID := m.getNodeID()
	claimed, err := claimAgentConnectionAuthorityScript.Run(
		context.Background(),
		store.RDB,
		[]string{
			agentConnectionAuthorityKey(conn.agentID, conn.ownerID),
			agentRouteKeyForOwner(conn.agentID, conn.ownerID),
			agentRouteKey(conn.agentID),
			agentRouteOwnerSetKey(conn.agentID),
			agentCapabilitiesKeyForOwner(conn.agentID, conn.ownerID),
			agentCapabilitiesKey(conn.agentID),
			toolruntime.Key(conn.agentID),
			pkgagentapi.ConnInfoKey(conn.agentID, conn.ownerID),
			toolruntime.KeyForOwner(conn.agentID, conn.ownerID),
		},
		nodeID,
		conn.connectionEpoch,
		authorityTTLSeconds(ttl),
		conn.ownerID,
		boolIntString(conn.isPrimary),
	).Int()
	return claimed == 1, err
}

func (m *Manager) refreshAgentConnectionAuthority(conn *agentConn, ttl time.Duration) (bool, error) {
	if conn == nil || conn.agentID <= 0 || conn.ownerID <= 0 {
		return false, nil
	}
	if conn.connectionEpoch <= 0 || store.RDB == nil || strings.TrimSpace(m.getNodeID()) == "" {
		return true, nil
	}
	refreshed, err := refreshAgentConnectionAuthorityScript.Run(
		context.Background(),
		store.RDB,
		[]string{
			agentConnectionAuthorityKey(conn.agentID, conn.ownerID),
			agentRouteKeyForOwner(conn.agentID, conn.ownerID),
			agentRouteKey(conn.agentID),
			agentRouteOwnerSetKey(conn.agentID),
		},
		m.getNodeID(),
		conn.connectionEpoch,
		authorityTTLSeconds(ttl),
		conn.ownerID,
		boolIntString(conn.isPrimary),
	).Int()
	return refreshed == 1, err
}

func (m *Manager) isAgentConnectionAuthoritative(conn *agentConn) bool {
	if conn == nil || conn.agentID <= 0 {
		return false
	}
	if conn.connectionEpoch <= 0 || store.RDB == nil || strings.TrimSpace(m.getNodeID()) == "" {
		route := loadAgentRouteForOwner(context.Background(), conn.agentID, conn.ownerID)
		return route == "" || route == m.getNodeID()
	}
	authority, ok, err := loadAgentConnectionAuthority(
		context.Background(),
		conn.agentID,
		conn.ownerID,
	)
	if err != nil || !ok {
		return false
	}
	return authority.Active &&
		authority.NodeID == m.getNodeID() &&
		authority.ConnectionEpoch == conn.connectionEpoch
}

// ensureAgentConnectionAuthoritative is used immediately before local wire
// delivery. A stale positive-epoch connection is closed on rejection so it
// cannot keep consuming inbound/outbound work until the next heartbeat.
func (m *Manager) ensureAgentConnectionAuthoritative(conn *agentConn) bool {
	if m.isAgentConnectionAuthoritative(conn) {
		return true
	}
	if conn != nil && conn.connectionEpoch > 0 {
		logger.L.Warnf(
			"close stale agent connection agent=%d owner=%d node=%s epoch=%d",
			conn.agentID,
			conn.ownerID,
			m.getNodeID(),
			conn.connectionEpoch,
		)
		conn.close()
	}
	return false
}

func boolIntString(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func (m *Manager) setAgentConnectionMetadata(
	conn *agentConn,
	ttl time.Duration,
	ownerKey string,
	ownerValue []byte,
	writeOwner bool,
	mainKey string,
	mainValue []byte,
	writeMain bool,
) (bool, error) {
	if conn == nil || conn.agentID <= 0 {
		return false, nil
	}
	if conn.connectionEpoch <= 0 || store.RDB == nil || strings.TrimSpace(m.getNodeID()) == "" {
		return false, nil
	}
	written, err := setAgentConnectionMetadataScript.Run(
		context.Background(),
		store.RDB,
		[]string{
			agentConnectionAuthorityKey(conn.agentID, conn.ownerID),
			ownerKey,
			mainKey,
		},
		m.getNodeID(),
		conn.connectionEpoch,
		authorityTTLSeconds(ttl),
		boolIntString(writeOwner),
		ownerValue,
		boolIntString(writeMain),
		mainValue,
	).Int()
	return written == 1, err
}

// releaseAgentConnectionAuthority returns true only for the generation that is
// still globally current. It atomically marks that generation inactive and
// removes only the metadata owned by it.
func (m *Manager) releaseAgentConnectionAuthority(conn *agentConn) bool {
	if conn == nil || conn.agentID <= 0 || conn.ownerID <= 0 {
		return false
	}
	if conn.connectionEpoch <= 0 || store.RDB == nil || strings.TrimSpace(m.getNodeID()) == "" {
		return true
	}
	released, err := releaseAgentConnectionAuthorityScript.Run(
		context.Background(),
		store.RDB,
		[]string{
			agentConnectionAuthorityKey(conn.agentID, conn.ownerID),
			agentRouteKeyForOwner(conn.agentID, conn.ownerID),
			agentRouteKey(conn.agentID),
			agentRouteOwnerSetKey(conn.agentID),
			agentCapabilitiesKeyForOwner(conn.agentID, conn.ownerID),
			agentCapabilitiesKey(conn.agentID),
			toolruntime.Key(conn.agentID),
			pkgagentapi.ConnInfoKey(conn.agentID, conn.ownerID),
			toolruntime.KeyForOwner(conn.agentID, conn.ownerID),
		},
		m.getNodeID(),
		conn.connectionEpoch,
		boolIntString(conn.isPrimary),
	).Int()
	if err != nil {
		logger.L.Warnf(
			"release agent authority failed agent=%d owner=%d node=%s epoch=%d err=%v",
			conn.agentID,
			conn.ownerID,
			m.getNodeID(),
			conn.connectionEpoch,
			err,
		)
		return false
	}
	return released == 1
}

// loadAgentRoute 返回 agent 主连接所在节点(无 owner 维度的旧路径,兼容用)。
func loadAgentRoute(ctx context.Context, agentID int64) string {
	if agentID <= 0 || store.RDB == nil {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}

	nodeID, err := store.RDB.Get(ctx, agentRouteKey(agentID)).Result()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(nodeID)
}

// loadAgentRouteForOwner 优先返回 (agentID, ownerID) 的精确路由节点;找不到回退主路由。
// ownerID<=0 退化为 loadAgentRoute,兼容无 owner 上下文的旧调用。
func loadAgentRouteForOwner(ctx context.Context, agentID, ownerID int64) string {
	if agentID <= 0 || store.RDB == nil {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ownerID > 0 {
		if nodeID, err := store.RDB.Get(ctx, agentRouteKeyForOwner(agentID, ownerID)).Result(); err == nil {
			if trimmed := strings.TrimSpace(nodeID); trimmed != "" {
				return trimmed
			}
		}
	}
	return loadAgentRoute(ctx, agentID)
}

// loadAgentRouteAllNodes 返回该 agent 当前所有 owner 连接所在的节点集合(去重)。
// 用于撤销共享 / KickAgent 等需要跨节点广播的场景。
func loadAgentRouteAllNodes(ctx context.Context, agentID int64) []string {
	if agentID <= 0 || store.RDB == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	owners, err := store.RDB.SMembers(ctx, agentRouteOwnerSetKey(agentID)).Result()
	if err != nil || len(owners) == 0 {
		// 兜底:无 owner 集时返回主路由节点(单节点场景)
		if main := loadAgentRoute(ctx, agentID); main != "" {
			return []string{main}
		}
		return nil
	}
	seen := make(map[string]struct{}, len(owners)+1)
	out := make([]string, 0, len(owners)+1)
	for _, ownerStr := range owners {
		var ownerID int64
		if _, scanErr := fmt.Sscan(ownerStr, &ownerID); scanErr != nil || ownerID <= 0 {
			continue
		}
		nodeID := loadAgentRouteForOwner(ctx, agentID, ownerID)
		if nodeID == "" {
			continue
		}
		if _, dup := seen[nodeID]; dup {
			continue
		}
		seen[nodeID] = struct{}{}
		out = append(out, nodeID)
	}
	// 主路由也补一遍,防止 owner set 漏写时丢节点
	if main := loadAgentRoute(ctx, agentID); main != "" {
		if _, dup := seen[main]; !dup {
			out = append(out, main)
		}
	}
	return out
}

// IsAgentChannelAvailable returns whether an Agent API channel can be reached.
// It checks the current node's in-memory connection first, then the Redis route.
func IsAgentChannelAvailable(agentID int64) bool {
	if agentID <= 0 {
		return false
	}

	globalMu.RLock()
	manager := globalManager
	globalMu.RUnlock()

	if manager != nil && manager.lookupConn(agentID) != nil {
		return true
	}

	return loadAgentRoute(context.Background(), agentID) != ""
}

// IsAgentChannelAvailableForOwner reports reachability the same way delivery
// actually routes an event: by the precise (agentID, ownerID) connection/route,
// not just the agent's primary one. Share scenarios need this — the primary
// connection being up says nothing about whether a specific shared user's own
// connection is reachable, and vice versa for a lagging primary route key.
func IsAgentChannelAvailableForOwner(agentID, ownerID int64) bool {
	if agentID <= 0 {
		return false
	}
	if ownerID <= 0 {
		return IsAgentChannelAvailable(agentID)
	}

	globalMu.RLock()
	manager := globalManager
	globalMu.RUnlock()

	if manager != nil && manager.lookupConnByOwner(agentID, ownerID) != nil {
		return true
	}

	return loadAgentRouteForOwner(context.Background(), agentID, ownerID) != ""
}

// GetAgentClientType returns the client type of a connected agent from the
// in-memory connection state. Returns empty string if the agent is not found.
func GetAgentClientType(agentID int64) string {
	if agentID <= 0 {
		return ""
	}

	globalMu.RLock()
	manager := globalManager
	globalMu.RUnlock()

	if manager == nil {
		return ""
	}
	conn := manager.lookupConn(agentID)
	if conn == nil {
		return ""
	}
	return conn.clientType
}

// refreshAgentRoute 写主路由 key(仅主连接)+ owner 路由 key(所有连接)。
// 主连接写主 key 让兼容旧调用点的 loadAgentRoute 仍能返回正确节点;
// 每条连接都写自己的 owner key 让按 owner 跨节点路由(loadAgentRouteForOwner)生效;
// 同时把 ownerID 加入该 agent 的 owner 集合,便于跨节点广播(loadAgentRouteAllNodes)。
func (m *Manager) refreshAgentRoute(conn *agentConn, ttl time.Duration) {
	if conn == nil || conn.agentID <= 0 || ttl <= 0 || store.RDB == nil {
		return
	}
	nodeID := m.getNodeID()
	if nodeID == "" {
		return
	}
	if conn.connectionEpoch > 0 {
		ok, err := m.refreshAgentConnectionAuthority(conn, ttl)
		if err != nil {
			logger.L.Warnf(
				"refresh agent route authority failed agent=%d owner=%d epoch=%d err=%v",
				conn.agentID,
				conn.ownerID,
				conn.connectionEpoch,
				err,
			)
		} else if !ok {
			logger.L.Warnf(
				"reject stale agent route refresh agent=%d owner=%d node=%s epoch=%d",
				conn.agentID,
				conn.ownerID,
				nodeID,
				conn.connectionEpoch,
			)
		}
		return
	}
	ctx := context.Background()
	if conn.isPrimary {
		if err := store.RDB.Set(ctx, agentRouteKey(conn.agentID), nodeID, ttl).Err(); err != nil {
			logger.L.Warnf("refresh agent route failed agent=%d node=%s err=%v", conn.agentID, nodeID, err)
		}
	}
	if conn.ownerID > 0 {
		if err := store.RDB.Set(ctx, agentRouteKeyForOwner(conn.agentID, conn.ownerID), nodeID, ttl).Err(); err != nil {
			logger.L.Warnf("refresh agent owner route failed agent=%d owner=%d node=%s err=%v", conn.agentID, conn.ownerID, nodeID, err)
		}
		// owner 集合 TTL 略宽于路由 TTL,降低集合先于路由 key 过期的边界情况
		_ = store.RDB.SAdd(ctx, agentRouteOwnerSetKey(conn.agentID), conn.ownerID).Err()
		_ = store.RDB.Expire(ctx, agentRouteOwnerSetKey(conn.agentID), ttl+30*time.Second).Err()
	}
}

// refreshAgentCapabilities 把 agent 的 local_actions 持久化到 Redis,供跨节点 local_action 路由使用。
// 主连接同时写主 key(兼容旧 loadAgentCapabilities) + 自己的 owner key;
// 共享连接只写自己的 owner key,确保 connector 版本异构时不互相覆盖。
func (m *Manager) refreshAgentCapabilities(conn *agentConn, ttl time.Duration) {
	if conn == nil || conn.agentID <= 0 || store.RDB == nil || ttl <= 0 {
		return
	}
	data, err := json.Marshal(conn.localActions)
	if err != nil {
		return
	}
	if conn.connectionEpoch > 0 {
		ok, writeErr := m.setAgentConnectionMetadata(
			conn,
			ttl,
			agentCapabilitiesKeyForOwner(conn.agentID, conn.ownerID),
			data,
			len(conn.localActions) > 0,
			agentCapabilitiesKey(conn.agentID),
			data,
			conn.isPrimary && len(conn.localActions) > 0,
		)
		if writeErr != nil {
			logger.L.Warnf(
				"refresh agent capabilities authority failed agent=%d owner=%d epoch=%d err=%v",
				conn.agentID,
				conn.ownerID,
				conn.connectionEpoch,
				writeErr,
			)
		} else if !ok {
			logger.L.Warnf(
				"reject stale agent capabilities refresh agent=%d owner=%d epoch=%d",
				conn.agentID,
				conn.ownerID,
				conn.connectionEpoch,
			)
		}
		return
	}
	if len(conn.localActions) == 0 {
		return
	}
	ctx := context.Background()
	if conn.isPrimary {
		if err := store.RDB.Set(ctx, agentCapabilitiesKey(conn.agentID), data, ttl).Err(); err != nil {
			logger.L.Warnf("refresh agent capabilities failed agent=%d err=%v", conn.agentID, err)
		}
	}
	if conn.ownerID > 0 {
		if err := store.RDB.Set(ctx, agentCapabilitiesKeyForOwner(conn.agentID, conn.ownerID), data, ttl).Err(); err != nil {
			logger.L.Warnf("refresh agent owner capabilities failed agent=%d owner=%d err=%v", conn.agentID, conn.ownerID, err)
		}
	}
}

func (m *Manager) refreshAgentRuntimeProfile(conn *agentConn, ttl time.Duration) {
	if conn == nil || conn.agentID <= 0 {
		return
	}
	leaseUntil := time.Now().Add(ttl).UnixMilli()
	profile := toolruntime.Profile{
		AgentID:       conn.agentID,
		OwnerID:       conn.ownerID,
		AdapterID:     strings.TrimSpace(conn.adapterID),
		ClientType:    firstNonEmpty(strings.TrimSpace(conn.clientType), strings.TrimSpace(conn.adapterID)),
		Capabilities:  append([]string(nil), conn.capabilities...),
		LocalActions:  append([]string(nil), conn.localActions...),
		Skills:        conn.skills,
		LibrarySkills: conn.librarySkills,
		Online:        true,
		LeaseUntil:    leaseUntil,
		UpdatedAt:     time.Now().UnixMilli(),
	}
	if conn.connectionEpoch > 0 {
		if store.RDB == nil {
			return
		}
		data, err := json.Marshal(profile)
		if err != nil {
			return
		}
		ok, writeErr := m.setAgentConnectionMetadata(
			conn,
			ttl,
			toolruntime.KeyForOwner(conn.agentID, conn.ownerID),
			data,
			true,
			toolruntime.Key(conn.agentID),
			data,
			conn.isPrimary,
		)
		if writeErr != nil {
			logger.L.Warnf(
				"refresh agent runtime authority failed agent=%d owner=%d epoch=%d err=%v",
				conn.agentID,
				conn.ownerID,
				conn.connectionEpoch,
				writeErr,
			)
		} else if !ok {
			logger.L.Warnf(
				"reject stale agent runtime refresh agent=%d owner=%d epoch=%d",
				conn.agentID,
				conn.ownerID,
				conn.connectionEpoch,
			)
		}
		return
	}
	if err := toolruntime.StoreProfileForOwner(context.Background(), profile, ttl); err != nil {
		logger.L.Warnf("refresh agent owner runtime profile failed agent=%d owner=%d err=%v", conn.agentID, conn.ownerID, err)
	}
	// Epoch zero is legacy/test compatibility. Historically every connection
	// wrote the agent-scoped key, so retain that behavior outside production.
	if err := toolruntime.StoreProfile(context.Background(), profile, ttl); err != nil {
		logger.L.Warnf("refresh agent runtime profile failed agent=%d err=%v", conn.agentID, err)
	}
}

// loadAgentCapabilities loads the cached local_actions for an agent from Redis.
// 兼容入口:无 owner 上下文时返回主 key 的能力集。新代码请用 loadAgentCapabilitiesForOwner。
func loadAgentCapabilities(ctx context.Context, agentID int64) []string {
	return loadAgentCapabilitiesForOwner(ctx, agentID, 0)
}

// loadAgentCapabilitiesForOwner 优先查 (agentID, ownerID) 的能力 key,找不到回退主 key。
// 共享场景下 connector 版本异构时,B 借 X 调反向动作能据 B 自己 connector 的能力集判定。
func loadAgentCapabilitiesForOwner(ctx context.Context, agentID, ownerID int64) []string {
	if agentID <= 0 || store.RDB == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ownerID > 0 {
		if data, err := store.RDB.Get(ctx, agentCapabilitiesKeyForOwner(agentID, ownerID)).Bytes(); err == nil {
			var actions []string
			if jerr := json.Unmarshal(data, &actions); jerr == nil && len(actions) > 0 {
				return actions
			}
		}
		profile, ok, profileErr := toolruntime.LoadProfileForOwner(ctx, agentID, ownerID)
		if profileErr == nil && ok && len(profile.LocalActions) > 0 {
			return profile.LocalActions
		}
	}
	data, err := store.RDB.Get(ctx, agentCapabilitiesKey(agentID)).Bytes()
	if err != nil {
		profile, ok, profileErr := toolruntime.LoadProfile(ctx, agentID)
		if profileErr != nil || !ok {
			return nil
		}
		return profile.LocalActions
	}
	var actions []string
	if err := json.Unmarshal(data, &actions); err != nil {
		profile, ok, profileErr := toolruntime.LoadProfile(ctx, agentID)
		if profileErr != nil || !ok {
			return nil
		}
		return profile.LocalActions
	}
	return actions
}

// clearAgentRoute 清空该 agent 在本节点的路由。
// 仅在该 agent 已无任何连接(agentGone)时调用,清主路由 + capabilities;
// 同时移除该 agent 的 owner 集合(各 owner 路由 key 由 clearAgentRouteForOwner 在各连接断时清)。
func (m *Manager) clearAgentRoute(agentID int64) {
	nodeID := m.getNodeID()
	if agentID <= 0 || nodeID == "" || store.RDB == nil {
		return
	}
	if err := clearAgentRouteScript.Run(
		context.Background(),
		store.RDB,
		[]string{agentRouteKey(agentID), agentCapabilitiesKey(agentID)},
		nodeID,
	).Err(); err != nil {
		logger.L.Warnf("clear agent route failed agent=%d node=%s err=%v", agentID, nodeID, err)
	}
	_ = store.RDB.Del(context.Background(), agentRouteOwnerSetKey(agentID)).Err()
	_ = toolruntime.DeleteProfile(context.Background(), agentID)
}

// clearAgentRouteForOwner 清单个 owner 的路由(连接断开时调用)。仅删自己写的那条 key,
// 避免误删别的节点上同 owner 的路由(在 LB 抖动场景下 owner 可能在两节点都有过路由)。
func (m *Manager) clearAgentRouteForOwner(agentID, ownerID int64) {
	nodeID := m.getNodeID()
	if agentID <= 0 || ownerID <= 0 || nodeID == "" || store.RDB == nil {
		return
	}
	if err := clearAgentRouteOwnerScript.Run(
		context.Background(),
		store.RDB,
		[]string{agentRouteKeyForOwner(agentID, ownerID)},
		nodeID,
	).Err(); err != nil {
		logger.L.Warnf("clear agent owner route failed agent=%d owner=%d node=%s err=%v", agentID, ownerID, nodeID, err)
	}
	// 顺手清自己 owner 的 capabilities key:连接断开后该 owner 的能力集已失效,
	// 不该让跨节点 reverse-call 用过期数据。
	if err := store.RDB.Del(context.Background(), agentCapabilitiesKeyForOwner(agentID, ownerID)).Err(); err != nil {
		logger.L.Warnf("clear agent owner capabilities failed agent=%d owner=%d err=%v", agentID, ownerID, err)
	}
	if err := toolruntime.DeleteProfileForOwner(context.Background(), agentID, ownerID); err != nil {
		logger.L.Warnf("clear agent owner runtime failed agent=%d owner=%d err=%v", agentID, ownerID, err)
	}
	// 不从 owner set 删:set 是用于跨节点扫描,本节点清自己的 owner key 后,
	// set 里的 owner 可能仍指向其他节点的连接(重连/漂移),不应在本地清就把 owner 从 set 里抹掉。
	// owner set 自身的 TTL(refreshAgentRoute 时刷新)和 agent 级 clearAgentRoute 会兜底清理。
}
