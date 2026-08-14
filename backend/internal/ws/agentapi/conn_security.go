package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/askie/grix/backend/internal/model"
	pkgagentapi "github.com/askie/grix/backend/internal/pkg/agentapi"
	"github.com/askie/grix/backend/internal/pkg/ipgeo"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"github.com/redis/go-redis/v9"
)

// agent WS 连接安全（阶段0）：握手 IP 封禁拦截、连接来源落库、Redis 在线信息。
// 全部动作对握手主流程尽量无侵入：封禁命中拒绝连接；记录失败只告警不影响连接。

const disconnectReasonClosed = "closed"

// deleteConnInfoScript 只在 key 仍属于本连接（log_id 一致）时删除，
// 防止顶号场景下旧连接断开时误删新连接刚写入的信息。
var deleteConnInfoScript = redis.NewScript(`
local raw = redis.call("GET", KEYS[1])
if not raw then
  return 0
end
local ok, data = pcall(cjson.decode, raw)
if ok and data["log_id"] == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

// checkAgentIPBanned 握手期封禁检查。命中返回 true，调用方直接拒绝连接。
func checkAgentIPBanned(agentID int64, clientIP string) bool {
	if agentID <= 0 || clientIP == "" || store.DB == nil {
		return false
	}
	if security.IsAgentIPBanned(agentID, clientIP) {
		logger.L.Warnf("agent api handshake rejected by ip ban: agent=%d ip=%s", agentID, clientIP)
		return true
	}
	return false
}

// recordAgentConnection 在认证成功后记录连接来源：
// 查地理归属 → 与上一次成功连接比对生成异地标记 → 白名单观测 → 落日志表。
// 任一步失败只告警，不影响连接建立。
func recordAgentConnection(m *Manager, conn *agentConn) {
	if conn == nil || conn.agentID <= 0 || store.DB == nil {
		return
	}
	location := ipgeo.Lookup(conn.clientIP)
	conn.ipLocation = location

	geoChanged := false
	if location != "" {
		var prev model.AgentConnectionLog
		err := store.DB.Select("id", "ip_location").
			Where("agent_id = ? AND owner_id = ? AND ip_location <> ''", conn.agentID, conn.ownerID).
			Order("connected_at DESC").Limit(1).First(&prev).Error
		if err == nil && prev.IPLocation != location {
			geoChanged = true
			logger.L.Warnf(
				"agent api geo change detected: agent=%d owner=%d ip=%s location=%q previous=%q",
				conn.agentID, conn.ownerID, conn.clientIP, location, prev.IPLocation,
			)
		}
	}

	allowlistMiss := false
	if conn.clientIP != "" {
		if exists, matched := security.AgentIPAllowlistState(conn.agentID, conn.clientIP); exists && !matched {
			allowlistMiss = true
			logger.L.Warnf(
				"agent api allowlist miss (observe only): agent=%d owner=%d ip=%s location=%q",
				conn.agentID, conn.ownerID, conn.clientIP, location,
			)
		}
	}

	entry := &model.AgentConnectionLog{
		ID:            snowflake.GenID(),
		AgentID:       conn.agentID,
		OwnerID:       conn.ownerID,
		IsPrimary:     conn.isPrimary,
		ClientType:    conn.clientType,
		ClientIP:      conn.clientIP,
		IPLocation:    location,
		GeoChanged:    geoChanged,
		AllowlistMiss: allowlistMiss,
		NodeID:        m.getNodeID(),
		ConnectedAt:   time.Now(),
	}
	if err := store.DB.Create(entry).Error; err != nil {
		logger.L.Errorf("agent api connection log create failed: agent=%d owner=%d err=%v", conn.agentID, conn.ownerID, err)
		return
	}
	conn.connLogID = entry.ID
}

// refreshConnInfo 把在线连接实时信息写入 Redis，随租约续期调用。
func (m *Manager) refreshConnInfo(conn *agentConn, ttl time.Duration) {
	if conn == nil || conn.agentID <= 0 || conn.connLogID == 0 || store.RDB == nil || ttl <= 0 {
		return
	}
	info := pkgagentapi.ConnInfo{
		LogID:       conn.connLogID,
		AgentID:     conn.agentID,
		OwnerID:     conn.ownerID,
		IsPrimary:   conn.isPrimary,
		ClientType:  conn.clientType,
		ClientIP:    conn.clientIP,
		IPLocation:  conn.ipLocation,
		NodeID:      m.getNodeID(),
		ConnectedAt: conn.connectedAt.UnixMilli(),
	}
	raw, err := json.Marshal(info)
	if err != nil {
		return
	}
	if conn.connectionEpoch > 0 {
		ok, writeErr := m.setAgentConnectionMetadata(
			conn,
			ttl,
			pkgagentapi.ConnInfoKey(conn.agentID, conn.ownerID),
			raw,
			true,
			pkgagentapi.ConnInfoKey(conn.agentID, conn.ownerID),
			nil,
			false,
		)
		if writeErr != nil {
			logger.L.Warnf(
				"agent api conninfo authority refresh failed: agent=%d owner=%d epoch=%d err=%v",
				conn.agentID,
				conn.ownerID,
				conn.connectionEpoch,
				writeErr,
			)
		} else if !ok {
			logger.L.Warnf(
				"reject stale agent conninfo refresh: agent=%d owner=%d epoch=%d",
				conn.agentID,
				conn.ownerID,
				conn.connectionEpoch,
			)
		}
		return
	}
	if err := store.RDB.Set(context.Background(), pkgagentapi.ConnInfoKey(conn.agentID, conn.ownerID), raw, ttl).Err(); err != nil {
		logger.L.Warnf("agent api conninfo refresh failed: agent=%d owner=%d err=%v", conn.agentID, conn.ownerID, err)
	}
}

// finalizeAgentConnection 连接断开时回填日志（断开时间/原因）并清理 Redis 在线信息。
// 幂等：每条连接只执行一次（先到的 reason 生效，如 kick 原因优先于读循环退出的 closed）。
func finalizeAgentConnection(conn *agentConn, reason string) {
	if conn == nil || conn.connLogID == 0 {
		return
	}
	conn.finalizeOnce.Do(func() {
		if reason == "" {
			reason = disconnectReasonClosed
		}
		if store.DB != nil {
			now := time.Now()
			if err := store.DB.Model(&model.AgentConnectionLog{}).
				Where("id = ?", conn.connLogID).
				Updates(map[string]any{
					"disconnected_at":   now,
					"disconnect_reason": reason,
				}).Error; err != nil {
				logger.L.Warnf("agent api connection log finalize failed: log=%d err=%v", conn.connLogID, err)
			}
		}
		if store.RDB != nil {
			logID := fmt.Sprintf("%d", conn.connLogID)
			_ = deleteConnInfoScript.Run(
				context.Background(), store.RDB,
				[]string{pkgagentapi.ConnInfoKey(conn.agentID, conn.ownerID)},
				logID,
			).Err()
		}
	})
}
