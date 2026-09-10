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

// Registered 报告某个 client_type 是否调用过 Register——区别于 Commands 返回空切片：
// 有的 client_type（如 agy）故意注册一个空列表表示"确认过、就是没有斜杠命令"，
// 跟从未调用 Register 的遗漏是两回事，后者才是该发现的接线缺口。
func Registered(clientType string) bool {
	_, ok := registry[clientType]
	return ok
}
