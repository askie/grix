package agentslashcmd

// DevEco Code shares opencode's command surface at the binary level: decompiling the
// locally installed @deveco/deveco-code package shows the bundled config schema is
// literally `"$schema": "https://opencode.ai/config.json"` and the internal service
// names are unchanged (`@opencode/Global`, opencode's Config/Session/Flock modules) —
// only the env var prefix and a handful of top-level identifiers were renamed from
// OPENCODE_ to DEVECO_ (see opencode-adapter.ts's <VENDOR>_CONFIG_CONTENT fix). Round4
// found no evidence of a divergent TUI command set, so this reuses opencode.go's list
// verbatim rather than leaving deveco with no slash-commands item at all — the empty-list
// alternative silently breaks ApplyCustomSlashCommands (core/service.go), which only
// merges a session's custom commands into an *existing* slash_commands item.
func init() {
	Register("deveco", []SlashCommand{
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
