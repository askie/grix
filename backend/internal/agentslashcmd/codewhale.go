package agentslashcmd

func init() {
	Register("codewhale", []SlashCommand{
		{Name: "/compact", Description: "压缩长上下文以回收 token 预算"},
		{Name: "/model", Description: "选择模型或自动路由（别名 /models /provider）"},
		{Name: "/review", Description: "结构化代码审查工作流"},
		{Name: "/restore", Description: "从快照回滚到之前的版本"},
		{Name: "/sessions", Description: "会话选择器（'a' 显示所有会话）"},
		{Name: "/memory", Description: "查看和管理持久化记忆"},
		{Name: "/mcp", Description: "配置和查看 MCP 服务器集成"},
		{Name: "/skill", Description: "激活一个技能（/skills 列出已安装技能）"},
		{Name: "/plan", Description: "切换到 Plan 模式"},
		{Name: "/config", Description: "编辑运行时和提供商设置"},
		{Name: "/status", Description: "显示 token 用量分布"},
		{Name: "/theme", Description: "切换主题"},
		{Name: "/rename", Description: "为当前会话设置自定义标题"},
		{Name: "/help", Description: "显示帮助信息"},
	})
}
