package agentslashcmd

func init() {
	Register("openclaw", []SlashCommand{
		{Name: "/new", Description: "归档当前会话，开启全新会话"},
		{Name: "/reset", Description: "就地重置会话（保留线程）"},
		{Name: "/compact", Description: "压缩会话上下文以节省 token"},
		{Name: "/stop", Description: "中止当前正在运行的任务"},
		{Name: "/model", Description: "查看或切换当前使用的模型"},
		{Name: "/steer", Description: "向正在运行的任务注入引导指令（别名 /tell）"},
		{Name: "/queue", Description: "管理任务队列"},
		{Name: "/goal", Description: "管理持久会话目标（status/start/pause/resume/clear）"},
		{Name: "/approve", Description: "批准待审的危险命令或操作"},
		{Name: "/think", Description: "设置思维链推理强度（别名 /thinking /t）"},
		{Name: "/status", Description: "查看执行状态、运行时信息和用量"},
		{Name: "/tools", Description: "查看当前可用工具列表"},
		{Name: "/skill", Description: "按名称运行一个技能"},
		{Name: "/agents", Description: "查看当前线程绑定的代理列表"},
		{Name: "/commands", Description: "显示完整命令目录"},
		{Name: "/help", Description: "显示帮助摘要"},
	})
}
