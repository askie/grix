package agentslashcmd

// 命令清单核对自 Kiro CLI 2.14.2 内置命令表：
// 补充 /goal /effort /rewind /knowledge /code /guide /stats 等当前版本命令。
func init() {
	Register("kiro", []SlashCommand{
		{Name: "/clear", Description: "清除对话历史"},
		{Name: "/compact", Description: "压缩对话历史以节省 token"},
		{Name: "/model", Description: "选择或列出可用模型"},
		{Name: "/plan", Description: "切换到 Plan 代理，将想法拆分为实现计划"},
		{Name: "/agent", Description: "选择或列出可用代理"},
		{Name: "/goal", Description: "设定带验证标准的目标，迭代推进直到完成"},
		{Name: "/effort", Description: "设置本会话的思考力度"},
		{Name: "/rewind", Description: "回退到之前的对话轮次（分支为新会话）"},
		{Name: "/context", Description: "管理上下文文件或显示 token 用量"},
		{Name: "/knowledge", Description: "管理知识库（add/remove/show/update）"},
		{Name: "/code", Description: "代码智能工作区管理"},
		{Name: "/mcp", Description: "显示已配置的 MCP 服务器"},
		{Name: "/tools", Description: "显示可用工具"},
		{Name: "/hooks", Description: "查看已配置的生命周期钩子"},
		{Name: "/prompts", Description: "选择或列出可用提示词"},
		{Name: "/chat", Description: "加载历史会话或开启新会话"},
		{Name: "/usage", Description: "显示计费和用量信息"},
		{Name: "/stats", Description: "显示请求 ID 与耗时（诊断慢响应）"},
		{Name: "/guide", Description: "向引导代理咨询 Kiro CLI 的使用问题"},
		{Name: "/feedback", Description: "提交反馈或报告问题"},
		{Name: "/help", Description: "显示可用命令"},
	})
}
