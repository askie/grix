package agentslashcmd

// omp shares the pi adapter's connector-level session commands verbatim
// (see PiAdapter.getSupportedCommands() in grix-connector) — model/interrupt/
// status/skills, surfaced here the same way pi's are.
func init() {
	Register("omp", []SlashCommand{
		{Name: "/status", Description: "查看当前 omp 会话状态"},
		{Name: "/restart", Description: "重启当前 omp 会话"},
		{Name: "/stop", Description: "停止当前正在进行的输出"},
	})
}
