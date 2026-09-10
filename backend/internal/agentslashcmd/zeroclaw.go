package agentslashcmd

// zeroclaw round5 探针卡在 session/new 要求的 agentAlias（"session/new requires
// `agentAlias`"——需要预先 `zeroclaw agents create <alias>` 配好 provider，
// 本机没有可用凭证走到这一步），没能拿到它自己的 available_commands_update，
// 查不到真实命令表。按连接器通用 ACP 兜底注册最小会话生命周期集（与 pi.go /
// reasonix.go / omp.go 同一处理方式：无实测依据时不编造 CLI 专属命令名，只给
// 协议保证存在的会话操作）。
func init() {
	Register("zeroclaw", []SlashCommand{
		{Name: "/status", Description: "查看当前会话状态"},
		{Name: "/restart", Description: "重启当前会话"},
		{Name: "/stop", Description: "停止当前正在进行的输出"},
	})
}
