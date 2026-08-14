package agentslashcmd

func init() {
	// agy（Antigravity CLI）通过 grix-connector 的 print 模式接入，
	// 每条消息独立 spawn 子进程，CLI 交互式斜杠命令不可用。
	Register("agy", []SlashCommand{})
}
