package agentslashcmd

// 命令清单核对自 opencode 1.18.17 TUI 斜杠命令表：
// 移除已下线的 /sessions /models /init /themes /details /editor /help，
// 补充 /fork /rename /timeline /timestamps 等当前版本命令。
func init() {
	Register("opencode", []SlashCommand{
		{Name: "/new", Description: "开启新会话"},
		{Name: "/compact", Description: "压缩当前会话上下文（生成摘要）"},
		{Name: "/undo", Description: "撤销上一条消息并恢复文件改动"},
		{Name: "/redo", Description: "重做已撤销的消息"},
		{Name: "/fork", Description: "从某条消息分支出新会话"},
		{Name: "/rename", Description: "重命名当前会话"},
		{Name: "/model", Description: "选择当前使用的模型"},
		{Name: "/agent", Description: "切换当前使用的 agent"},
		{Name: "/mcp", Description: "查看和管理 MCP 服务器"},
		{Name: "/timeline", Description: "显示会话时间线"},
		{Name: "/timestamps", Description: "切换消息时间戳显示"},
		{Name: "/thinking", Description: "切换推理/思考内容的可见性"},
		{Name: "/copy", Description: "复制会话记录"},
		{Name: "/export", Description: "导出当前会话记录"},
		{Name: "/share", Description: "分享当前会话"},
		{Name: "/unshare", Description: "取消分享当前会话"},
	})
}
