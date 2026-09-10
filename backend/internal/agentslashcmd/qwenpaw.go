package agentslashcmd

// 命令清单核对自 qwenpaw 本机实测：真实 ACP session/update 的
// available_commands_update 载荷（round5 探针，见 ~/grix-agent-gap/logs/qwenpaw.jsonl）。
func init() {
	Register("qwenpaw", []SlashCommand{
		{Name: "/model", Description: "查看或切换 AI 模型"},
		{Name: "/skills", Description: "列出可用技能并暴露显式技能命令"},
		{Name: "/checkpoint", Description: "查看和管理对话检查点（auto/timeline/snapshot/restore/gc/reset）"},
		{Name: "/clear", Description: "清空对话上下文"},
		{Name: "/compact", Description: "压缩对话上下文（可附加说明）"},
		{Name: "/tools", Description: "列出当前会话中活跃的工具调用"},
		{Name: "/tool-bg", Description: "将一个运行中的工具调用移到后台"},
		{Name: "/tool-cancel", Description: "取消一个运行中的工具调用"},
		{Name: "/mission", Description: "启动任务模式——拆解、实现并验证复杂任务"},
		{Name: "/goal", Description: "设置目标，代理持续工作直到完成"},
	})
}
