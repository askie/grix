package protocol

const (
	// CmdAgentProfilePush 是后端 → Connector 的「agent 资料变更」下行推送：
	// 用户在 APP 上修改 agent 名字或介绍后，通过此命令通知 connector 更新。
	CmdAgentProfilePush = "agent_profile_push"
)

// AgentProfilePushPayload 下发 agent 的最新名字、介绍和业务 system prompt。
type AgentProfilePushPayload struct {
	AgentID      int64  `json:"agent_id,string"`
	AgentName    string `json:"agent_name"`
	Introduction string `json:"introduction"`
	SystemPrompt string `json:"system_prompt"`
}

// RedisCmdAgentProfileSync 是「agent 资料变更」的跨进程通知 cmd：
// api 进程修改 agent 名字/介绍后，向 agent 主连接所在节点发布此命令，
// ws 进程据此重新从 DB 读取最新资料并下发 agent_profile_push。
const RedisCmdAgentProfileSync = "_agent_profile_sync"

// AgentProfileSyncPayload 是 RedisCmdAgentProfileSync 的载荷。
type AgentProfileSyncPayload struct {
	AgentID int64 `json:"agent_id,string"`
}
