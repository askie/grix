package agentslashcmd

// qoderclicn shares qodercli.go's slash command set verbatim — same underlying
// product family (国区构建，见 acp-adapter.ts checkQoderSessionActivity 用同一
// 逻辑处理 .qoder/.qoder-cn 的先例）。round5 探针里 qoderclicn 本机未登录，
// session/new 卡在鉴权，没能拿到它自己独立的 available_commands_update；这里
// 直接复用 qodercli 已实测确认的命令表，不是独立验证过 qoderclicn 本身——
// 与 omp.go 复用 pi.go 同一处理方式（字面量复制，不依赖 init() 跨文件顺序）。
func init() {
	Register("qoderclicn", []SlashCommand{
		{Name: "/rewind", Description: "回退对话与已生成的文件到指定用户消息之前"},
		{Name: "/rename", Description: "为当前会话设置自定义标题"},
		{Name: "/simplify", Description: "审查改动代码的复用性、质量与效率并修复问题"},
		{Name: "/debug", Description: "为本会话启用调试日志以辅助排障"},
		{Name: "/quest", Description: "智能工作流编排器，引导通过专用子代理完成功能开发"},
		{Name: "/batch", Description: "在隔离 worktree 中派发并行工作代理，跨代码库应用批量改动"},
		{Name: "/mcp-config", Description: "交互式添加、更新或移除 MCP 服务器配置"},
		{Name: "/run", Description: "启动并驱动项目应用，观察改动是否生效"},
		{Name: "/verify", Description: "运行应用并观察实际行为，验证代码改动是否符合预期"},
		{Name: "/run-skill-generator", Description: "创建或改进项目专属的运行/启动技能"},
		{Name: "/agent-creator", Description: "创建在独立上下文中运行、带自定义系统提示与工具权限的子代理"},
		{Name: "/hook-config", Description: "创建和配置生命周期钩子"},
		{Name: "/sdk", Description: "指导基于 Qoder TypeScript SDK 构建应用、脚本或自动化流程"},
		{Name: "/skill-creator", Description: "创建或更新扩展 QoderCLI 能力的技能"},
		{Name: "/security-scan", Description: "对仓库或指定路径做安全扫描（L2 轻量 / L3 深度）"},
		{Name: "/goal", Description: "管理当前会话的持久化目标"},
		{Name: "/deep-research", Description: "多路并行网络搜索、交叉验证并生成带引用的研究报告"},
	})
}
