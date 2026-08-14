package agentapi

import (
	"strconv"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// sendShareSet 向某条连接全量下发该 agent 当前有效的被共享者列表（control_share_set）。
// 仅对 agent 主连接有意义：connector 据此为每个被共享者维护一条独立连接。
// sendIfEmpty=false 时，列表为空则不发（主连接刚建立、该 agent 无共享的常态，避免无谓包）；
// 共享变更（含撤销到空）走 sendIfEmpty=true，以便 connector 收到空集后断开全部被共享者连接。
// 已封号/注销的被共享者会被一并过滤掉，让 connector 主动断开其实例。
func (m *Manager) sendShareSet(conn *agentConn, agentID int64, sendIfEmpty bool) {
	if conn == nil || agentID <= 0 || store.DB == nil {
		return
	}
	activeShares, err := loadActiveSharedToIDs(agentID)
	if err != nil {
		logger.L.Warnf("load agent shares for control_share_set failed agent=%d err=%v", agentID, err)
		return
	}
	ids := make([]string, 0, len(activeShares))
	for _, sharedTo := range activeShares {
		ids = append(ids, strconv.FormatInt(sharedTo, 10))
	}
	if len(ids) == 0 && !sendIfEmpty {
		return
	}
	conn.sendPayload(protocol.CmdControlShareSet, conn.nextSeq(), protocol.ControlShareSetPayload{
		AgentID:  agentID,
		SharedTo: ids,
	})
}

// loadActiveSharedToIDs 拉该 agent 当前所有「有效共享 + 被共享者活跃」的 shared_to 列表。
// 被共享者封号/注销时，对应共享对外即不可见——和 hasActiveAgentShare/agentShareActive 口径一致。
func loadActiveSharedToIDs(agentID int64) ([]int64, error) {
	var shares []model.AgentShare
	if err := store.DB.
		Select("shared_to", "expires_at").
		Where("agent_id = ? AND status = ?", agentID, model.AgentShareStatusActive).
		Find(&shares).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	candidates := make([]int64, 0, len(shares))
	for i := range shares {
		if shares[i].ExpiresAt != nil && shares[i].ExpiresAt.Before(now) {
			continue
		}
		candidates = append(candidates, shares[i].SharedTo)
	}
	if len(candidates) == 0 {
		return candidates, nil
	}
	var activeUsers []model.User
	if err := store.DB.
		Select("id").
		Where("id IN ? AND status = ?", candidates, model.UserStatusActive).
		Find(&activeUsers).Error; err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(activeUsers))
	for i := range activeUsers {
		out = append(out, activeUsers[i].ID)
	}
	return out, nil
}

// pushShareSetToPrimary 找到本节点该 agent 的主连接并下发最新共享列表。
// 共享变更（创建/撤销）通过 Redis 通知到 agent 主连接所在节点后调用。
func (m *Manager) pushShareSetToPrimary(agentID int64) {
	if agentID <= 0 {
		return
	}
	m.mu.RLock()
	conn := m.primaryConnLocked(agentID)
	m.mu.RUnlock()
	if conn == nil {
		return
	}
	m.sendShareSet(conn, agentID, true)
}

// enforceAuthorizedShareConns 后端兜底：撤销共享通知到达时，主动踢掉本节点上
// 该 agent 已失去授权（非主人、且不在有效共享名单）的被共享者连接，不依赖 connector 时序。
// 被共享者封号/注销时同样会被踢（与 hasActiveAgentShare 口径一致）。
func (m *Manager) enforceAuthorizedShareConns(agentID int64) {
	if agentID <= 0 || store.DB == nil {
		return
	}
	var agent model.Agent
	if err := store.DB.Select("id", "owner_id").First(&agent, agentID).Error; err != nil {
		return
	}
	activeShares, err := loadActiveSharedToIDs(agentID)
	if err != nil {
		return
	}
	allowed := map[int64]bool{agent.OwnerID: true}
	for _, sharedTo := range activeShares {
		allowed[sharedTo] = true
	}

	m.mu.RLock()
	var toKick []*agentConn
	for ownerID, c := range m.conns[agentID] {
		if c != nil && !allowed[ownerID] {
			toKick = append(toKick, c)
		}
	}
	m.mu.RUnlock()

	for _, c := range toKick {
		sendShareRevokedNotice(c, "share_revoked")
		logger.L.Infof("kick revoked share conn agent=%d owner=%d", agentID, c.ownerID)
		m.unregister(c)
	}
}

// sendShareRevokedNotice 在断连前给 connector 发一条原因通知:
//   - 非 hermes:发 "kicked" 包(标准路径)
//   - hermes:协议未承诺 "kicked" 服务端 cmd,改发 error 包(在 hermes 服务端白名单内),
//     携带 CodeUnauthorized + reason,让 hermes connector 知道是被服务端踢而非网络断,
//     不会盲目重连。
func sendShareRevokedNotice(c *agentConn, reason string) {
	if c == nil {
		return
	}
	if isHermesConn(c) {
		c.sendPayload(protocol.CmdError, c.nextSeq(), SendNackPayload{
			Code: protocol.CodeUnauthorized,
			Msg:  reason,
		})
		return
	}
	c.sendPayload(protocol.CmdKicked, c.nextSeq(), map[string]string{"reason": reason})
}
