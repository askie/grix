package provisioning

import (
	"context"
	"encoding/json"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

// ApplyRelayStateActionType 是 local_action 下发"应用中转开关期望态"的 action_type。
// connector 通过能力声明（localActions 含该值）门控：老版本未声明时
// SendLocalActionForOwner 自动拒发，天然兼容；它同时是 state_known 的能力位判据
// 与 local_action_result 写回的识别依据，三处必须一致。
const ApplyRelayStateActionType = "apply_relay_state"

// RedisCmdApplyRelayState 是"把某个 Agent 的中转开关期望态即时下发给在线 connector"
// 的广播 cmd（设计 §2.4 路径 B：用户操作触发；agent 离线时 pub/sub 天然静默丢弃，
// connector 下次上线走 relay_state_sync_request 路径 A 对齐兜底）。
const RedisCmdApplyRelayState = "apply_relay_state_broadcast"

// RelayStateApplyConfig 是 apply_relay_state 的广播 payload；revision 是下发时的
// desired 版本号，connector 回执（local_action_result / relay_state_report）必须带回，
// 服务端据此丢弃过期回执（设计 §2.4 回执幂等）。
type RelayStateApplyConfig struct {
	AgentID  int64  `json:"agent_id,string"`
	Enabled  bool   `json:"enabled"`
	Model    string `json:"model"`
	Revision int64  `json:"revision"`
}

// PublishApplyRelayState 由 C端网关API在用户设置中转开关后调用（仅 gateway.relay_state_enabled
// 开启时）。广播给所有 ws 节点，持有该 agent 主人权威连接的节点下发 local_action，
// 其余节点找不到本地连接自然跳过。
func PublishApplyRelayState(cfg RelayStateApplyConfig) error {
	if store.RDB == nil {
		return nil // 单机/无 Redis 部署没有跨节点广播，agent 上线走 sync 对齐
	}
	envelope := map[string]any{
		"cmd":     RedisCmdApplyRelayState,
		"payload": cfg,
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		logger.L.Warnf("marshal broadcast apply_relay_state failed: %v", err)
		return err
	}
	if err := store.RDB.Publish(context.Background(), BroadcastChannel, data).Err(); err != nil {
		logger.L.Warnf("publish broadcast apply_relay_state failed: %v", err)
		return err
	}
	return nil
}
