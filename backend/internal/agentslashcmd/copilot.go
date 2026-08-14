package agentslashcmd

func init() {
	Register("copilot", []SlashCommand{
		{Name: "/explain", Description: "解释选中代码的工作原理"},
		{Name: "/fix", Description: "为选中代码的问题提出修复方案"},
		{Name: "/tests", Description: "为选中代码生成单元测试"},
		{Name: "/new", Description: "创建一个新项目或开始新对话"},
		{Name: "/clear", Description: "清除当前对话，开启新会话"},
		{Name: "/doc", Description: "为选中符号生成文档注释"},
		{Name: "/optimize", Description: "分析并改进选中代码的运行性能"},
		{Name: "/help", Description: "显示 GitHub Copilot 使用帮助"},
	})
}
