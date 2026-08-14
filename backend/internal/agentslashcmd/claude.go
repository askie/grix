package agentslashcmd

func init() {
	Register("claude", []SlashCommand{
		{Name: "/clear", Description: "清空上下文，开启新会话（别名 /reset /new）"},
		{Name: "/compact", Description: "压缩当前会话上下文以释放 token 空间"},
		{Name: "/model", Description: "切换当前使用的 Claude 模型"},
		{Name: "/plan", Description: "切换到 Plan 模式，让 Claude 先规划再执行"},
		{Name: "/init", Description: "分析当前目录并生成 CLAUDE.md 上下文文件"},
		{Name: "/memory", Description: "管理持久化记忆（添加/查看/清除）"},
		{Name: "/mcp", Description: "查看和管理 MCP 服务器"},
		{Name: "/resume", Description: "恢复一个之前的会话"},
		{Name: "/rewind", Description: "回退到上一个检查点并撤销文件改动（别名 /undo）"},
		{Name: "/review", Description: "对当前代码变更进行 AI 代码审查"},
		{Name: "/diff", Description: "显示当前会话中的文件变更差异"},
		{Name: "/usage", Description: "显示当前会话的 token 用量与费用（别名 /cost /stats）"},
		{Name: "/permissions", Description: "查看和管理工具执行权限"},
		{Name: "/hooks", Description: "管理会话生命周期钩子"},
		{Name: "/skills", Description: "查看和运行可用技能"},
		{Name: "/agents", Description: "查看子代理列表及运行状态"},
		{Name: "/background", Description: "在后台执行一个任务"},
		{Name: "/stop", Description: "停止当前正在进行的输出"},
		{Name: "/doctor", Description: "诊断 Claude Code 环境配置问题"},
		{Name: "/help", Description: "显示可用命令列表"},
	})
}
