package agentslashcmd

func init() {
	Register("qwen", []SlashCommand{
		{Name: "/new", Description: "开启新会话（别名 /clear）"},
		{Name: "/compress", Description: "压缩会话历史以节省 token"},
		{Name: "/init", Description: "分析当前目录并生成初始上下文文件"},
		{Name: "/resume", Description: "恢复一个之前的会话"},
		{Name: "/model", Description: "切换当前使用的模型"},
		{Name: "/plan", Description: "切换到 Plan 模式（进入/退出）"},
		{Name: "/review", Description: "结构化代码审查（并行 agent 分析）"},
		{Name: "/undo", Description: "撤销最后一条消息并恢复文件改动"},
		{Name: "/restore", Description: "将文件恢复到某次工具调用前的状态"},
		{Name: "/memory", Description: "打开记忆管理器（查看/添加/删除）"},
		{Name: "/mcp", Description: "查看已配置的 MCP 服务器和工具"},
		{Name: "/tools", Description: "显示可用工具列表"},
		{Name: "/skills", Description: "列出并运行可用技能"},
		{Name: "/approval-mode", Description: "切换工具批准模式（plan/default/auto-edit/yolo）"},
		{Name: "/stats", Description: "显示当前会话的详细统计"},
		{Name: "/theme", Description: "切换视觉主题"},
		{Name: "/vim", Description: "切换 Vim 输入模式"},
		{Name: "/help", Description: "显示帮助信息"},
	})
}
