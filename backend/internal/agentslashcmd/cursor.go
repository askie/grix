package agentslashcmd

// 命令清单核对自 Cursor Agent CLI 2026.08.11 内置命令表：
// 移除已下线的 /new-chat /compress /auto-run /sandbox /max-mode，补充当前版本命令。
func init() {
	Register("cursor", []SlashCommand{
		{Name: "/clear", Description: "开启新的聊天会话（别名 /new /new-chat）"},
		{Name: "/summarize", Description: "总结对话以压缩上下文"},
		{Name: "/resume", Description: "打开最近会话并恢复"},
		{Name: "/fork", Description: "把当前会话分支为新会话"},
		{Name: "/rewind", Description: "回退到之前的某条消息"},
		{Name: "/rename", Description: "重命名当前会话"},
		{Name: "/goal", Description: "启动持久目标，空闲时也持续推进"},
		{Name: "/plan", Description: "创建计划或查看现有计划（Plan 模式）"},
		{Name: "/ask", Description: "切换 Ask 模式（只读问答，不改代码）"},
		{Name: "/model", Description: "选择当前使用的模型"},
		{Name: "/fast", Description: "切换快速模式"},
		{Name: "/context", Description: "显示上下文占用明细"},
		{Name: "/changes", Description: "查看代码变更（会话/未暂存/已暂存/已提交）"},
		{Name: "/commit", Description: "让 agent 暂存并提交更改"},
		{Name: "/btw", Description: "旁支提问，不打断主对话也不入历史"},
		{Name: "/mcp", Description: "管理 MCP 服务器"},
		{Name: "/skills", Description: "打开技能菜单"},
		{Name: "/rule", Description: "管理 Cursor 规则"},
		{Name: "/command", Description: "管理自定义命令"},
		{Name: "/plugin", Description: "管理插件（查看/浏览/安装/卸载）"},
		{Name: "/add-dir", Description: "添加目录到当前工作区"},
		{Name: "/jobs", Description: "打开活跃任务列表"},
		{Name: "/usage", Description: "显示套餐与按需用量"},
		{Name: "/config", Description: "交互式配置 CLI 设置"},
		{Name: "/feedback", Description: "提交反馈"},
		{Name: "/help", Description: "显示帮助（/help [命令]）"},
	})
}
