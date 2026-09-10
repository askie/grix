package agentslashcmd

func init() {
	Register("reasonix", []SlashCommand{
		{Name: "/status", Description: "查看当前 Reasonix 会话状态"},
		{Name: "/stop", Description: "停止当前正在进行的输出"},
		{Name: "/think", Description: "启用深度推理模式"},
	})
}
