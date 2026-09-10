package agentslashcmd

// 命令清单核对自 TraeCode CLI 本机实测：真实 ACP session/update 的
// available_commands_update 载荷（round5 探针，见 ~/grix-agent-gap/logs/traecli.jsonl）。
// 探针只跑通了这 4 条——CLI 尚年轻，命令表本身就比 Kiro/Gemini 这类成熟 CLI 薄，
// 不是探针遗漏。
func init() {
	Register("traecli", []SlashCommand{
		{Name: "/agent-new", Description: "创建一个新的子代理配置"},
		{Name: "/init", Description: "为当前目录初始化 AGENTS.md 文件"},
		{Name: "/loop", Description: "按固定间隔重复运行一条提示词，或列出/取消循环任务"},
		{Name: "/compact", Description: "清除对话历史但保留摘要上下文"},
	})
}
