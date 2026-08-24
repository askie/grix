package agentslashcmd

// 命令清单核对自 Qwen Code 0.14.5 内置命令表：
// 移除已下线的 /review /undo，补充当前版本命令。
func init() {
	Register("qwen", []SlashCommand{
		{Name: "/clear", Description: "清空上下文，开启新会话（别名 /reset /new）"},
		{Name: "/compress", Description: "压缩会话历史以节省 token（别名 /summarize）"},
		{Name: "/resume", Description: "恢复一个之前的会话"},
		{Name: "/restore", Description: "将会话和文件恢复到某次工具调用前的状态"},
		{Name: "/export", Description: "导出当前会话消息历史到文件"},
		{Name: "/summary", Description: "生成项目摘要并保存到 .qwen/PROJECT_SUMMARY.md"},
		{Name: "/model", Description: "切换当前使用的模型"},
		{Name: "/plan", Description: "进入或退出 Plan 模式"},
		{Name: "/approval-mode", Description: "查看或更改工具批准模式"},
		{Name: "/agents", Description: "管理用于专项任务的子代理"},
		{Name: "/btw", Description: "旁支快速提问，不影响主对话"},
		{Name: "/init", Description: "分析当前目录并生成 QWEN.md 上下文文件"},
		{Name: "/memory", Description: "管理记忆（查看/添加/删除）"},
		{Name: "/mcp", Description: "打开 MCP 管理面板"},
		{Name: "/tools", Description: "显示可用工具列表"},
		{Name: "/skills", Description: "列出可用技能"},
		{Name: "/hooks", Description: "管理生命周期钩子"},
		{Name: "/permissions", Description: "管理权限规则"},
		{Name: "/trust", Description: "管理文件夹信任设置"},
		{Name: "/settings", Description: "查看和编辑 Qwen Code 设置"},
		{Name: "/directory", Description: "管理工作区目录（别名 /dir）"},
		{Name: "/language", Description: "查看或更改语言设置"},
		{Name: "/stats", Description: "显示当前会话的详细统计（别名 /usage）"},
		{Name: "/status", Description: "显示版本与系统信息（别名 /about）"},
		{Name: "/theme", Description: "切换视觉主题"},
		{Name: "/vim", Description: "切换 Vim 输入模式"},
		{Name: "/help", Description: "显示帮助信息"},
	})
}
