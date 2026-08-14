package protocol

const (
	// CmdSkillSync 是后端 → Connector 的「技能库变更」下行推送（docs/architecture/38 §6.2）：
	// owner 的自定义技能库发生增删改后，通知该 owner 所有在线 agent 连接，
	// connector 收到后立即触发一次下拉同步（拉 /v1/agent-api/skills 比对落盘）。
	// 指令只是"提醒去拉"，不携带技能内容；connector 的定时轮询继续作为离线兜底。
	CmdSkillSync = "skill_sync"
)

// SkillSyncPayload 下发技能库变更提醒。Name/Version 仅供 connector 日志观测，
// 同步行为始终是全量清单比对，不依赖这两个字段。
type SkillSyncPayload struct {
	OwnerID int64  `json:"owner_id,string"`
	Name    string `json:"name,omitempty"`
	Version int64  `json:"version,string,omitempty"`
}

// RedisCmdSkillLibraryChanged 是「技能库变更」的跨进程通知 cmd：
// api/ws 进程内的技能库落库成功后，向全体 ws 节点广播（chan:broadcast），
// 各节点向本节点上该 owner 的全部主连接下发 skill_sync。
const RedisCmdSkillLibraryChanged = "_skill_library_changed"

// SkillLibraryChangedPayload 是 RedisCmdSkillLibraryChanged 的载荷。
type SkillLibraryChangedPayload struct {
	OwnerID int64  `json:"owner_id,string"`
	Name    string `json:"name,omitempty"`
	Version int64  `json:"version,string,omitempty"`
}
