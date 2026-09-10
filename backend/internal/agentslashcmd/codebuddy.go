package agentslashcmd

// 命令名清单核对自腾讯 CodeBuddy Code 本机实测：真实 stream-json 会话 init 事件的
// slash_commands 字段（round1 探针，见 ~/grix-agent-gap/logs/04_codebuddy_streamjson_sample.log，
// 完整 75 条）。探针跑的是 --acp 之外的 stream-json 模式（同一 CLI 的另一种
// 接入方式，见 manager.ts codebuddy 分支注释：参数几乎逐字段对齐 claude adapter），
// 命令表本身与协议无关，同一份对 ACP 会话同样适用。stream-json 载荷只带命令名，
// 没有描述文本，以下描述文案是按命令名与该产品同源于 Claude Code 的通用惯例写的，
// 不是引用官方文案；只收录用户在工具栏里有意义的一部分，未逐条搬运全部 75 条。
func init() {
	Register("codebuddy", []SlashCommand{
		{Name: "/status", Description: "显示账号与会话状态"},
		{Name: "/model", Description: "查看或切换当前使用的模型"},
		{Name: "/clear", Description: "清除对话历史，开启新会话"},
		{Name: "/compact", Description: "压缩对话历史以节省上下文窗口"},
		{Name: "/resume", Description: "恢复一个之前的会话"},
		{Name: "/rewind", Description: "回退到之前的对话轮次"},
		{Name: "/rename", Description: "为当前会话设置自定义标题"},
		{Name: "/mcp", Description: "管理已配置的 MCP 服务器"},
		{Name: "/permissions", Description: "管理工具权限规则"},
		{Name: "/plan", Description: "切换到 Plan 模式，先规划再执行"},
		{Name: "/hooks", Description: "管理生命周期钩子"},
		{Name: "/agents", Description: "管理子代理"},
		{Name: "/review", Description: "审查当前改动"},
		{Name: "/code-review", Description: "对代码改动做结构化审查"},
		{Name: "/security-review", Description: "对改动做安全审查"},
		{Name: "/simplify", Description: "审查代码复用性、简洁性与效率并修复问题"},
		{Name: "/verify", Description: "运行并观察实际行为，验证改动是否生效"},
		{Name: "/deep-research", Description: "多路并行搜索并生成带引用的研究报告"},
		{Name: "/commit", Description: "生成提交信息并提交改动"},
		{Name: "/init", Description: "分析当前目录并生成上下文文件"},
		{Name: "/goal", Description: "设置并跟踪本会话目标"},
		{Name: "/context", Description: "查看上下文窗口用量"},
		{Name: "/cost", Description: "查看本会话用量与花费"},
		{Name: "/skills", Description: "管理可用技能"},
		{Name: "/help", Description: "显示可用命令列表"},
	})
}
