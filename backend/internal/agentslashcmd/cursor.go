package agentslashcmd

func init() {
	Register("cursor", []SlashCommand{
		{Name: "/new-chat", Description: "开启新的聊天会话"},
		{Name: "/plan", Description: "切换到 Plan 模式（先规划再执行）"},
		{Name: "/model", Description: "切换当前使用的模型"},
		{Name: "/compress", Description: "压缩当前会话上下文"},
		{Name: "/resume", Description: "恢复一个之前的对话"},
		{Name: "/auto-run", Description: "切换自动运行模式"},
		{Name: "/sandbox", Description: "切换沙盒执行模式"},
		{Name: "/max-mode", Description: "切换 Max 模式（开/关）"},
		{Name: "/rules", Description: "查看和管理项目规则"},
		{Name: "/mcp", Description: "查看和管理 MCP 服务器"},
		{Name: "/usage", Description: "显示用量信息"},
		{Name: "/feedback", Description: "提交反馈"},
		{Name: "/help", Description: "显示可用命令"},
	})
}
