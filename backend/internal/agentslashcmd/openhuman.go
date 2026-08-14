package agentslashcmd

func init() {
	Register("openhuman", []SlashCommand{
		{Name: "/status", Description: "查看当前 OpenHuman 会话状态"},
		{Name: "/restart", Description: "重启当前 OpenHuman 会话"},
		{Name: "/stop", Description: "停止当前正在进行的输出"},
	})
}
