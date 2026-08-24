package agentslashcmd

func init() {
	// 命令清单核对自 Kimi Code CLI 0.38.0 的 ACP 适配器内置命令表
	// （kimi-code 仓库 packages/acp-adapter/src/builtin-commands.ts）。
	//
	// 只列 ACP 命令，不列终端 TUI 命令：Grix 经 ACP 通道接入 Kimi，适配器只拦截
	// 自己能直接执行的斜杠命令（内置命令 + 技能命令），其余斜杠输入一律判为未知
	// 命令返回，不会转给模型（见 acp-adapter/src/slash.ts 的 detectSlashIntent）。
	// TUI 那套命令（/new、/fork、/sessions、/btw 等）在 ACP 会话里发过去没有效果。
	//
	// 技能命令由工具栏 Skills 项单独渲染，此处不重复。
	Register("kimi", []SlashCommand{
		{Name: "/compact", Description: "压缩对话上下文（可附带自定义压缩说明）"},
		{Name: "/status", Description: "显示当前会话状态"},
		{Name: "/usage", Description: "显示会话 token 用量"},
		{Name: "/mcp", Description: "显示 MCP 服务器状态"},
		{Name: "/tasks", Description: "列出后台任务"},
		{Name: "/help", Description: "显示可用的 ACP 命令"},
	})
}
