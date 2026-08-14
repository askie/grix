package agentslashcmd

func init() {
	Register("pi", []SlashCommand{
		{Name: "/status", Description: "查看当前 Pi 会话状态"},
		{Name: "/restart", Description: "重启当前 Pi 会话"},
		{Name: "/stop", Description: "停止当前正在进行的输出"},
	})
}
