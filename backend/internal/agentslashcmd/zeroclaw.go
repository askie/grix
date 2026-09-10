package agentslashcmd

// zeroclaw round5 探针卡在 session/new 要求的 agentAlias（"session/new requires
// `agentAlias`"——需要预先 `zeroclaw agents create <alias>` 配好 provider，
// 本机没有可用凭证走到这一步），没能拿到它自己的 available_commands_update，
// 查不到真实命令表。按连接器通用 ACP 兜底注册最小会话生命周期集：无实测依据时
// 不编造 CLI 专属命令名，只给 agenttoolbar 里 zeroclaw 的 session_control 实际
// 放行的操作（status/stop/unbind，无 restart）。
func init() {
	Register("zeroclaw", []SlashCommand{
		{Name: "/status", Description: "查看当前会话状态"},
		{Name: "/stop", Description: "停止当前正在进行的输出"},
	})
}
