package agentslashcmd

// 命令清单核对自 Gemini CLI 0.42.0 内置命令表（BuiltinCommandLoader）。
func init() {
	Register("gemini", []SlashCommand{
		{Name: "/clear", Description: "清屏并开启新会话（别名 /new）"},
		{Name: "/compress", Description: "压缩当前会话上下文为摘要（别名 /compact /summarize）"},
		{Name: "/rewind", Description: "回退到指定消息并从该处重启对话"},
		{Name: "/restore", Description: "将文件恢复到某次工具调用前的状态"},
		{Name: "/resume", Description: "浏览自动保存的会话并恢复（save/resume/share/delete）"},
		{Name: "/model", Description: "管理和切换 Gemini 模型"},
		{Name: "/plan", Description: "切换到 Plan 模式并查看当前计划"},
		{Name: "/init", Description: "分析当前目录并生成 GEMINI.md 上下文文件"},
		{Name: "/memory", Description: "管理分层记忆（GEMINI.md）"},
		{Name: "/mcp", Description: "管理已配置的 MCP 服务器"},
		{Name: "/tools", Description: "显示当前可用的工具列表"},
		{Name: "/skills", Description: "管理 Agent 技能（list/enable/disable/reload）"},
		{Name: "/agents", Description: "管理本地和远程子代理"},
		{Name: "/permissions", Description: "管理文件夹信任等权限设置"},
		{Name: "/hooks", Description: "管理生命周期钩子"},
		{Name: "/settings", Description: "查看和编辑 Gemini CLI 设置"},
		{Name: "/extensions", Description: "管理扩展"},
		{Name: "/commands", Description: "管理自定义斜杠命令（list/reload）"},
		{Name: "/directory", Description: "管理工作区目录（别名 /dir）"},
		{Name: "/tasks", Description: "切换后台任务视图（别名 /bg）"},
		{Name: "/copy", Description: "复制最后的结果或代码片段到剪贴板"},
		{Name: "/stats", Description: "显示当前会话的详细统计（别名 /usage）"},
		{Name: "/about", Description: "显示版本信息"},
		{Name: "/theme", Description: "更改视觉主题"},
		{Name: "/vim", Description: "切换 Vim 编辑模式"},
		{Name: "/quit", Description: "退出 Gemini CLI（别名 /exit）"},
		{Name: "/help", Description: "显示帮助信息"},
	})
}
