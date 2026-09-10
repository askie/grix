package agentslashcmd

// dim（DimAgent/dimcode）round5 探针本机未登录，session/new 报
// "Authentication required: Provider credentials are required"，没能拿到它自己的
// available_commands_update，查不到真实命令表。按连接器通用 ACP 兜底注册最小
// 会话生命周期集（与 pi.go / reasonix.go / omp.go 同一处理方式：无实测依据时
// 不编造 CLI 专属命令名，只给协议保证存在的会话操作）。
func init() {
	Register("dim", []SlashCommand{
		{Name: "/status", Description: "查看当前会话状态"},
		{Name: "/restart", Description: "重启当前会话"},
		{Name: "/stop", Description: "停止当前正在进行的输出"},
	})
}
