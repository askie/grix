package agentslashcmd

func init() {
	Register("kiro", []SlashCommand{
		{Name: "/clear", Description: "清除对话历史"},
		{Name: "/compact", Description: "压缩对话历史以节省 token"},
		{Name: "/model", Description: "切换当前使用的模型"},
		{Name: "/plan", Description: "切换到 Plan 代理，将想法拆分为实现计划"},
		{Name: "/agent", Description: "选择或切换代理（别名 /spawn）"},
		{Name: "/context", Description: "管理上下文文件或显示 token 用量"},
		{Name: "/knowledge", Description: "管理知识库（add/remove/show/update）"},
		{Name: "/mcp", Description: "显示已配置的 MCP 服务器"},
		{Name: "/tools", Description: "显示和管理可用工具（trust/untrust）"},
		{Name: "/hooks", Description: "查看已配置的生命周期钩子"},
		{Name: "/prompts", Description: "选择或列出可用提示词"},
		{Name: "/chat", Description: "加载或保存会话（save/load/new）"},
		{Name: "/usage", Description: "显示计费和用量信息"},
		{Name: "/feedback", Description: "提交反馈或报告问题"},
		{Name: "/help", Description: "显示可用命令"},
	})
}
