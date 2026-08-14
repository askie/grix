// Package provisioning 是"把Grix中转虚拟Key下发给某个托管Agent"这条广播的最小公共依赖层。
// 单独成包是为了避开循环依赖：internal/api/service（发布方，api进程）和
// internal/ws/agentapi（订阅/派发方，ws进程）都需要引用同一份 cmd 常量和payload结构，
// 但 internal/ws/agentapi 本身（经 agenttoolbar/resolver）已经反向依赖 internal/api/service，
// 若把这层逻辑直接放进 internal/ws/agentapi，internal/api/service 就不能再导入它。
// 这个包只依赖 internal/store 和 internal/pkg/logger，不会跟任何一边形成环。
package provisioning

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

// BroadcastChannel 是所有 ws 节点都订阅的 Redis pub/sub 全局广播 channel，
// 必须与 internal/ws/agentapi.BroadcastChannel() 返回值保持一致。
const BroadcastChannel = "chan:broadcast"

// RedisCmdConfigureGatewayProvider 是"下发Grix中转虚拟Key配置给某个具体Agent"的广播cmd。
const RedisCmdConfigureGatewayProvider = "configure_gateway_provider_broadcast"

// GatewayProviderConfig 是要下发给某个托管Agent的中性供应商配置（网关地址+虚拟Key+模型名）。
// connector 收到后按该 agent 的接入方式自动分流：Claude/Codex 走 MITM 改路由，
// 其余走"原生配置"类型（qwen/opencode等）更新 entry.provider 并重启生效。
type GatewayProviderConfig struct {
	AgentID          int64  `json:"agent_id,string"`
	APIKey           string `json:"api_key"`
	AnthropicBaseURL string `json:"anthropic_base_url,omitempty"`
	OpenAIBaseURL    string `json:"openai_base_url,omitempty"`
	Model            string `json:"model,omitempty"`
}

// PublishConfigureGatewayProvider 由 C端网关API调用，向指定 agentID 下发"Grix中转"虚拟Key配置。
// 不需要知道该 agent 连在哪个 ws 节点上：广播给所有节点，真正持有该连接的节点会处理，
// 其余节点上 SendLocalAction 因找不到本地连接直接返回 false，不会重复投递。
func PublishConfigureGatewayProvider(cfg GatewayProviderConfig) error {
	if store.RDB == nil {
		return fmt.Errorf("redis unavailable")
	}
	envelope := map[string]any{
		"cmd":     RedisCmdConfigureGatewayProvider,
		"payload": cfg,
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		logger.L.Warnf("marshal broadcast configure_gateway_provider failed: %v", err)
		return err
	}
	if err := store.RDB.Publish(context.Background(), BroadcastChannel, data).Err(); err != nil {
		logger.L.Warnf("publish broadcast configure_gateway_provider failed: %v", err)
		return err
	}
	return nil
}
