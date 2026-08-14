// Package agentslashcmd 定义每种 Agent 类型内置支持的斜杠命令。
// 命令列表与插件侧无关，由后端静态声明，随工具栏快照下发给前端。
package agentslashcmd

// SlashCommand 描述一条斜杠命令。
type SlashCommand struct {
	// Name 是命令名，包含前缀斜杠，如 "/compact"。
	Name string `json:"name"`
	// Description 是展示给用户的说明文字。
	Description string `json:"description"`
}

// registry 保存 client_type → 命令列表 的映射。
var registry = map[string][]SlashCommand{}

// Register 注册某个 client_type 的斜杠命令列表。
// 应在 init() 中调用，重复注册同一 client_type 会 panic。
func Register(clientType string, cmds []SlashCommand) {
	if _, exists := registry[clientType]; exists {
		panic("agentslashcmd: duplicate registration for " + clientType)
	}
	registry[clientType] = cmds
}

// Commands 返回指定 client_type 的斜杠命令列表，未注册则返回 nil。
func Commands(clientType string) []SlashCommand {
	return registry[clientType]
}
