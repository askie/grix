package agentslashcmd

// 命令清单以 Claude Code CLI 2.1.x 实际内置命令为准：
// 移除已下线的 /review /agents，补充 /goal /context /effort 等新命令。
func init() {
	Register("claude", []SlashCommand{
		{Name: "/clear", Description: "清空上下文，开启新会话（别名 /reset /new）"},
		{Name: "/compact", Description: "压缩当前会话上下文以释放 token 空间"},
		{Name: "/context", Description: "查看当前上下文占用情况"},
		{Name: "/autocompact", Description: "设置自动压缩上下文的触发阈值"},
		{Name: "/resume", Description: "恢复一个之前的会话（别名 /continue）"},
		{Name: "/rewind", Description: "回退到上一个检查点并撤销文件改动（别名 /undo /checkpoint）"},
		{Name: "/rename", Description: "重命名当前会话（别名 /name）"},
		{Name: "/goal", Description: "设定完成目标，Claude 在停止前会检查目标是否达成（/goal clear 清除）"},
		{Name: "/plan", Description: "切换到 Plan 模式，让 Claude 先规划再执行"},
		{Name: "/model", Description: "切换当前使用的 Claude 模型"},
		{Name: "/effort", Description: "设置模型推理力度（思考深度）"},
		{Name: "/fast", Description: "切换快速输出模式"},
		{Name: "/tasks", Description: "查看和管理后台运行的任务（别名 /bashes）"},
		{Name: "/background", Description: "把当前任务转入后台继续执行（别名 /bg）"},
		{Name: "/stop", Description: "停止当前正在进行的输出"},
		{Name: "/diff", Description: "显示当前会话中的文件变更差异"},
		{Name: "/init", Description: "分析当前目录并生成 CLAUDE.md 上下文文件"},
		{Name: "/memory", Description: "管理持久化记忆（添加/查看/清除）"},
		{Name: "/mcp", Description: "查看和管理 MCP 服务器"},
		{Name: "/skills", Description: "查看和运行可用技能"},
		{Name: "/hooks", Description: "管理会话生命周期钩子"},
		{Name: "/permissions", Description: "查看和管理工具执行权限"},
		{Name: "/config", Description: "查看和修改 Claude Code 设置"},
		{Name: "/add-dir", Description: "添加一个新的工作目录"},
		{Name: "/usage", Description: "显示当前会话的 token 用量与费用（别名 /cost /stats）"},
		{Name: "/status", Description: "显示版本、模型、账号与连接状态"},
		{Name: "/doctor", Description: "诊断 Claude Code 环境配置问题"},
		{Name: "/help", Description: "显示可用命令列表"},
	})
}
