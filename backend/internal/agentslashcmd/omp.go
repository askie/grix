package agentslashcmd

// omp shares pi.go's slash command set verbatim (/status, /restart, /stop) —
// see pi.go in this package for the same registration for the real Pi CLI.
func init() {
	Register("omp", []SlashCommand{
		{Name: "/status", Description: "查看当前 omp 会话状态"},
		{Name: "/restart", Description: "重启当前 omp 会话"},
		{Name: "/stop", Description: "停止当前正在进行的输出"},
	})
}
