package handler

import (
	"encoding/json"
	"sync"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// session_type 在生产代码中仅会话创建时写入、从不更新(不可变),因此进程内缓存
// 无需失效;仅按容量上限整体清空兜底,防止无界增长。它非鉴权数据(只决定群/私聊
// 分支),陈旧无安全风险。读路径热点(sessions 表被读上亿次)由此卸载至进程内。
const sessionTypeCacheMaxEntries = 200000

var (
	sessionTypeCacheMu sync.RWMutex
	sessionTypeCache   = make(map[string]int16)
)

// loadSessionType 返回会话类型(私聊=1/群聊=2),优先命中进程内不可变缓存,
// 未命中回源 PG 并回填;会话不存在或出错返回默认 1(不缓存)。
func loadSessionType(sessionID string) int16 {
	if sessionID == "" {
		return 1
	}
	sessionTypeCacheMu.RLock()
	v, ok := sessionTypeCache[sessionID]
	sessionTypeCacheMu.RUnlock()
	if ok {
		return v
	}
	var row struct{ SessionType int16 }
	res := store.DB.Model(&model.Session{}).
		Select("session_type").
		Where("session_id = ?", sessionID).
		Limit(1).
		Scan(&row)
	if res.Error != nil || res.RowsAffected == 0 || row.SessionType <= 0 {
		return 1
	}
	sessionTypeCacheMu.Lock()
	if len(sessionTypeCache) >= sessionTypeCacheMaxEntries {
		sessionTypeCache = make(map[string]int16, sessionTypeCacheMaxEntries)
	}
	sessionTypeCache[sessionID] = row.SessionType
	sessionTypeCacheMu.Unlock()
	return row.SessionType
}

// InvalidateSessionTypeCache 删除某会话的进程内 session_type 缓存，下次读将回源 PG。
// 用于会话类型可变的场景（如私聊转群），由跨节点广播触发。
func InvalidateSessionTypeCache(sessionID string) {
	if sessionID == "" {
		return
	}
	sessionTypeCacheMu.Lock()
	delete(sessionTypeCache, sessionID)
	sessionTypeCacheMu.Unlock()
}

// handleSessionTypeInvalidate 处理跨节点广播的 session_type 失效命令。
func handleSessionTypeInvalidate(payload json.RawMessage) bool {
	var p protocol.SessionTypeInvalidatePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		logger.L.Warnf("session_type_invalidate unmarshal error: %v", err)
		return true
	}
	InvalidateSessionTypeCache(p.SessionID)
	return true
}
