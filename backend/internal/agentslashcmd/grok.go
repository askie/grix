package agentslashcmd

// 命令清单核对自 xAI Grok Build CLI 本机实测：真实 ACP initialize 响应
// _meta.availableCommands 载荷（round5 ACP 探针实测）。
func init() {
	Register("grok", []SlashCommand{
		{Name: "/compact", Description: "压缩对话历史以节省上下文窗口"},
		{Name: "/always-approve", Description: "切换始终批准模式（跳过所有权限确认）"},
		{Name: "/context", Description: "显示上下文窗口用量与会话统计"},
		{Name: "/session-info", Description: "显示会话详情（模型、轮次、上下文用量）"},
		{Name: "/deep-research", Description: "多路有界并行代理研究，交叉核实证据并生成带引用的报告"},
		{Name: "/workflow", Description: "启动已保存的工作流、列出运行记录，或管理运行（暂停/恢复/停止/保存）"},
		{Name: "/goal", Description: "设置、管理或查看自主目标"},
	})
}
