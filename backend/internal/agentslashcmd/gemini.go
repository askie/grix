package agentslashcmd

func init() {
	Register("gemini", []SlashCommand{
		{Name: "/clear", Description: "清空终端屏幕及当前会话历史"},
		{Name: "/compress", Description: "压缩当前会话上下文以节省 token"},
		{Name: "/model", Description: "切换当前使用的 Gemini 模型"},
		{Name: "/plan", Description: "切换到 Plan 模式（只读规划）"},
		{Name: "/init", Description: "分析当前目录并生成 GEMINI.md 上下文文件"},
		{Name: "/memory", Description: "管理分层记忆（GEMINI.md；子命令 list/refresh/show）"},
		{Name: "/resume", Description: "恢复或管理历史会话（子命令 list/save/resume）"},
		{Name: "/restore", Description: "将文件恢复到某次工具调用前的状态"},
		{Name: "/rewind", Description: "回退会话历史并撤销文件改动"},
		{Name: "/mcp", Description: "查看和管理 MCP 服务器"},
		{Name: "/tools", Description: "显示当前可用的工具列表"},
		{Name: "/skills", Description: "管理 Agent 技能（enable/disable/list）"},
		{Name: "/agents", Description: "管理本地和远程子代理"},
		{Name: "/stats", Description: "显示当前会话的详细统计信息"},
		{Name: "/theme", Description: "更改视觉主题"},
		{Name: "/vim", Description: "切换 Vim 编辑模式"},
		{Name: "/quit", Description: "退出 Gemini CLI（别名 /exit）"},
		{Name: "/help", Description: "显示帮助信息"},
	})
}
