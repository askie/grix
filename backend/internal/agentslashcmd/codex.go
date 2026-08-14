package agentslashcmd

func init() {
	Register("codex", []SlashCommand{
		{Name: "/new", Description: "开启新会话（别名 /reset）"},
		{Name: "/clear", Description: "清空当前会话上下文"},
		{Name: "/compact", Description: "压缩会话上下文以节省 token"},
		{Name: "/model", Description: "切换当前使用的模型"},
		{Name: "/plan", Description: "切换到 Plan 模式（只读规划）"},
		{Name: "/goal", Description: "设置跨轮次持续目标"},
		{Name: "/agent", Description: "选择或切换 agent"},
		{Name: "/stop", Description: "终止当前运行中的任务（别名 /clean）"},
		{Name: "/fork", Description: "从当前会话分支出新路径"},
		{Name: "/resume", Description: "恢复一个之前的会话"},
		{Name: "/review", Description: "对代码变更进行审查"},
		{Name: "/diff", Description: "显示当前文件变更差异"},
		{Name: "/approve", Description: "批准待审的操作"},
		{Name: "/skills", Description: "查看和运行可用技能"},
		{Name: "/memories", Description: "查看和管理持久化记忆"},
		{Name: "/mcp", Description: "查看和管理 MCP 服务器"},
		{Name: "/status", Description: "显示执行状态和运行信息"},
		{Name: "/usage", Description: "显示 token 用量"},
		{Name: "/feedback", Description: "提交反馈"},
		{Name: "/help", Description: "显示可用命令列表"},
	})
}
