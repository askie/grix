package agentapi

import (
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// pushProfileToPrimary 向该 agent 的主连接下发最新的名字、介绍和业务 system prompt。
func (m *Manager) pushProfileToPrimary(agentID int64) {
	if agentID <= 0 || store.DB == nil {
		return
	}
	var agent model.Agent
	if err := store.DB.Select("agent_name", "introduction", "system_prompt").First(&agent, agentID).Error; err != nil {
		logger.L.Warnf("load agent profile for push failed agent=%d err=%v", agentID, err)
		return
	}

	m.mu.RLock()
	owners := m.conns[agentID]
	var primary *agentConn
	for _, c := range owners {
		if c.isPrimary {
			primary = c
			break
		}
	}
	m.mu.RUnlock()

	if primary == nil {
		return
	}

	primary.sendPayload(protocol.CmdAgentProfilePush, primary.nextSeq(), protocol.AgentProfilePushPayload{
		AgentID:      agentID,
		AgentName:    agent.AgentName,
		Introduction: agent.Introduction,
		SystemPrompt: agent.SystemPrompt,
	})
}
