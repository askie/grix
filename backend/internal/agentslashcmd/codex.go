package agentslashcmd

// 命令清单核对自 Codex CLI 0.147.0 二进制内置命令表：
// 移除已下线的 /agent /stop /approve /memories /help，补充当前版本命令。
func init() {
	Register("codex", []SlashCommand{
		{Name: "/new", Description: "开启新会话"},
		{Name: "/resume", Description: "恢复一个之前保存的会话"},
		{Name: "/fork", Description: "从当前会话分支出新会话"},
		{Name: "/side", Description: "在临时分支中发起旁支对话（别名 /btw）"},
		{Name: "/compact", Description: "压缩会话上下文，避免触及上下文上限"},
		{Name: "/plan", Description: "切换到 Plan 模式"},
		{Name: "/goal", Description: "设置或查看长时任务的目标"},
		{Name: "/model", Description: "选择模型和推理力度"},
		{Name: "/permissions", Description: "设置 Codex 的操作权限（别名 /approvals）"},
		{Name: "/review", Description: "审查当前代码变更并找出问题"},
		{Name: "/diff", Description: "显示 git diff（含未跟踪文件）"},
		{Name: "/mention", Description: "提及一个文件"},
		{Name: "/init", Description: "生成 AGENTS.md 项目说明文件"},
		{Name: "/memory", Description: "配置记忆的使用与生成"},
		{Name: "/skills", Description: "查看和使用技能"},
		{Name: "/hooks", Description: "查看和管理生命周期钩子"},
		{Name: "/mcp", Description: "列出已配置的 MCP 工具"},
		{Name: "/apps", Description: "管理应用集成"},
		{Name: "/plugins", Description: "浏览插件"},
		{Name: "/agents", Description: "切换活跃的 agent 线程"},
		{Name: "/rename", Description: "重命名当前会话线程"},
		{Name: "/status", Description: "显示会话配置与 token 用量"},
		{Name: "/usage", Description: "查看账户用量与限额"},
		{Name: "/ps", Description: "列出后台终端任务"},
		{Name: "/style", Description: "选择 Codex 的沟通风格"},
		{Name: "/import", Description: "从 Claude Code 导入配置、项目与历史会话"},
		{Name: "/feedback", Description: "发送日志和反馈给维护者"},
	})
}
