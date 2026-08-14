package agentapi

// DispatchEventLifecycleCommand sends an event lifecycle command to an agent.
// 走统一收口 dispatchAgentCommand：本节点直发，否则跨副本转发到 agent 所在节点。
// ownerID 是命令发起者(被共享者 B 或主人 A);agent 共享场景下必须按 owner 精确路由,
// 否则 B 的 stop/cancel/clear 会落到 A 的 connector,B 的 run 不停、A 被误打扰。
func DispatchEventLifecycleCommand(agentID, ownerID int64, cmd string, payload any) bool {
	mgr := GetGlobal()
	if mgr == nil || agentID <= 0 {
		return false
	}
	return mgr.dispatchAgentCommand(agentID, ownerID, cmd, payload)
}
