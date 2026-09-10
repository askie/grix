package agentslashcmd

// mcode（MiniMax Code）round5 探针本机未登录，session/new 卡在
// "Authentication required: Run `mcode login`"，没能拿到它自己的
// available_commands_update，查不到真实命令表。按连接器通用 ACP 兜底注册最小
// 会话生命周期集：无实测依据时不编造 CLI 专属命令名，只给 agenttoolbar 里
// mcode 的 session_control 实际放行的操作（status/stop/unbind，无 restart）。
func init() {
	Register("mcode", []SlashCommand{
		{Name: "/status", Description: "查看当前会话状态"},
		{Name: "/stop", Description: "停止当前正在进行的输出"},
	})
}
