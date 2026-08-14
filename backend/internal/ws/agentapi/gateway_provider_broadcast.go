package agentapi

import (
	"fmt"

	"github.com/askie/grix/backend/internal/gateway/provisioning"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// handleBroadcastConfigureGatewayProvider 在每个 ws 节点订阅到广播 cmd 时执行。
// payload 含 API key，必须只投主实例（agent 主人的连接）：先显式解析 agent.OwnerID，
// 再按 (agentID, ownerID) 精确路由。广播在每个节点都执行，主连接所在节点自然命中，
// 其余节点 SendLocalActionForOwner 返回 false，保持"恰好一份"，且不跨节点转发，
// 避免广播场景下的重复投递。
func handleBroadcastConfigureGatewayProvider(cfg provisioning.GatewayProviderConfig) {
	globalMu.RLock()
	mgr := globalManager
	globalMu.RUnlock()
	if mgr == nil || cfg.AgentID <= 0 {
		return
	}
	if store.DB == nil {
		logger.L.Warnf("configure_gateway_provider skipped agent=%d: db unavailable, cannot resolve agent owner", cfg.AgentID)
		return
	}
	var agent model.Agent
	if err := store.DB.Select("id", "owner_id").First(&agent, cfg.AgentID).Error; err != nil || agent.OwnerID <= 0 {
		logger.L.Warnf("configure_gateway_provider skipped agent=%d: resolve owner failed err=%v owner=%d", cfg.AgentID, err, agent.OwnerID)
		return
	}
	action := protocol.LocalActionPayload{
		ActionID:   fmt.Sprintf("gateway-provider:%d", snowflake.GenID()),
		ActionType: "configure_gateway_provider",
		Params: map[string]any{
			"api_key":            cfg.APIKey,
			"anthropic_base_url": cfg.AnthropicBaseURL,
			"openai_base_url":    cfg.OpenAIBaseURL,
			"model":              cfg.Model,
		},
	}
	if mgr.SendLocalActionForOwner(cfg.AgentID, agent.OwnerID, action) {
		logger.L.Infof("configure_gateway_provider sent to agent=%d owner=%d node=%s", cfg.AgentID, agent.OwnerID, mgr.getNodeID())
	}
}
