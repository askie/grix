package agentapi

import (
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// pushSkillSyncToOwner 向本节点上该 owner 的全部主连接下发 skill_sync（docs/architecture/38 §6.2）。
// 每台机器的 connector 至少经由一条 agent 主连接收到提醒后立即拉取同步；
// 同机多 agent 会各收一份，connector 侧 SkillSyncer 自带防重入，不会重复落盘。
// 共享连接（isPrimary=false）不发：技能库按 owner 隔离，被共享者机器不同步他人技能。
func (m *Manager) pushSkillSyncToOwner(ownerID int64, name string, version int64) {
	if ownerID <= 0 {
		return
	}
	payload := protocol.SkillSyncPayload{OwnerID: ownerID, Name: name, Version: version}

	m.mu.RLock()
	targets := make([]*agentConn, 0, 4)
	for _, owners := range m.conns {
		for _, c := range owners {
			if c.isPrimary && c.ownerID == ownerID {
				targets = append(targets, c)
			}
		}
	}
	m.mu.RUnlock()

	for _, c := range targets {
		c.sendPayload(protocol.CmdSkillSync, c.nextSeq(), payload)
	}
}
