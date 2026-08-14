package protocol

const (
	// CmdControlShareSet 是后端 → Connector 的「agent 共享集合」下行控制命令：
	// 告诉 connector 当前该 agent 被共享给了哪些账户。connector 据此为每个被共享者
	// 用「主人 api_key + shared_owner_id」维护一条独立 WS 连接（新增建连、移除断连）。
	// 主连接建立时全量下发一次；共享变更时再次下发，connector 做增量 diff。
	CmdControlShareSet = "control_share_set"
)

// ControlShareSetPayload 全量下发某 agent 当前有效的被共享者列表。
// shared_to 为字符串化的 user_id 列表（避免 JS 大整数精度丢失）。
type ControlShareSetPayload struct {
	AgentID  int64    `json:"agent_id,string"`
	SharedTo []string `json:"shared_to"`
}

// RedisCmdAgentShareSync 是「共享变更」的跨进程通知 cmd：api 进程改动 agent_shares 后，
// 向 agent 主连接所在节点(chan:{node})发布此命令，ws 进程据此重新向 connector 下发 control_share_set。
const RedisCmdAgentShareSync = "_agent_share_sync"

// AgentShareSyncPayload 是 RedisCmdAgentShareSync 的载荷。
type AgentShareSyncPayload struct {
	AgentID int64 `json:"agent_id,string"`
}
