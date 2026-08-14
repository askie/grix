package agentslashcmd

func init() {
	Register("opencode", []SlashCommand{
		{Name: "/new", Description: "开启新会话（别名 /clear）"},
		{Name: "/compact", Description: "压缩当前会话上下文（别名 /summarize）"},
		{Name: "/undo", Description: "撤销上一条消息并恢复文件改动（需 Git 仓库）"},
		{Name: "/redo", Description: "重做已撤销的消息（需 Git 仓库）"},
		{Name: "/sessions", Description: "列出并切换会话（别名 /resume /continue）"},
		{Name: "/models", Description: "列出可用模型"},
		{Name: "/init", Description: "引导式创建或更新 AGENTS.md"},
		{Name: "/export", Description: "将当前会话导出为 Markdown 并在编辑器中打开"},
		{Name: "/share", Description: "分享当前会话"},
		{Name: "/themes", Description: "列出可用主题"},
		{Name: "/thinking", Description: "切换推理/思考块的可见性"},
		{Name: "/details", Description: "切换工具执行详情的显示"},
		{Name: "/editor", Description: "打开外部编辑器撰写消息"},
		{Name: "/help", Description: "显示帮助对话框"},
	})
}
