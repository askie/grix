package agentslashcmd

func init() {
	Register("hermes", []SlashCommand{
		{Name: "/new", Description: "开启新会话（重置会话 ID 和历史），别名 /reset"},
		{Name: "/retry", Description: "重发最后一条消息"},
		{Name: "/undo", Description: "撤销最后一轮对话（用户 + 助手消息）"},
		{Name: "/stop", Description: "终止所有正在运行的后台进程"},
		{Name: "/approve", Description: "批准一条待审的危险命令"},
		{Name: "/deny", Description: "拒绝一条待审的危险命令"},
		{Name: "/background", Description: "在后台执行一条提示词，别名 /bg /btw"},
		{Name: "/queue", Description: "将提示词排队到下一轮执行，别名 /q"},
		{Name: "/steer", Description: "在下一次工具调用后注入消息，不打断当前执行"},
		{Name: "/goal", Description: "设置跨轮次持续目标（text/pause/resume/clear/status）"},
		{Name: "/compress", Description: "手动压缩当前会话上下文"},
		{Name: "/rollback", Description: "列出或恢复文件系统检查点"},
		{Name: "/branch", Description: "从当前会话分支出新路径，别名 /fork"},
		{Name: "/resume", Description: "恢复一个之前命名的会话"},
		{Name: "/sessions", Description: "浏览并恢复历史会话"},
		{Name: "/title", Description: "为当前会话设置标题"},
		{Name: "/model", Description: "切换当前会话使用的模型，别名 /provider"},
		{Name: "/reasoning", Description: "管理推理强度与显示方式"},
		{Name: "/yolo", Description: "切换 YOLO 模式（跳过所有危险命令审批）"},
		{Name: "/status", Description: "显示会话信息"},
		{Name: "/usage", Description: "显示当前会话的 token 用量与速率限制"},
		{Name: "/agents", Description: "显示活跃 agent 和运行中的任务，别名 /tasks"},
		{Name: "/commands", Description: "分页浏览所有命令和技能"},
		{Name: "/help", Description: "显示可用命令列表"},
		{Name: "/restart", Description: "优雅重启网关（等待活跃任务完成后重启）"},
		{Name: "/update", Description: "更新 Hermes Agent 到最新版本"},
	})
}
