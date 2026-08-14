package shared

import (
	"github.com/askie/grix/backend/internal/agentslashcmd"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
)

// BuildSlashCommandsItem 构建斜杠命令工具栏 Item。
// clientType 对应 model.AgentClientType* 常量。
// 返回第二个值 false 表示该 client_type 无命令注册，调用方可选择不追加。
func BuildSlashCommandsItem(clientType string) (toolprotocol.Item, bool) {
	cmds := agentslashcmd.Commands(clientType)
	if len(cmds) == 0 {
		return toolprotocol.Item{}, false
	}
	commands := make([]toolprotocol.CommandItem, 0, len(cmds))
	for _, c := range cmds {
		commands = append(commands, toolprotocol.CommandItem{
			ID:          c.Name,
			Name:        c.Name,
			Description: c.Description,
			Exec:        c.Name,
		})
	}
	return toolprotocol.Item{
		ItemID:      "slash_commands",
		GroupID:     "slash_commands",
		Kind:        toolprotocol.ItemKindButton,
		ActionID:    "slash_commands",
		Label:       "",
		Icon:        "terminal",
		Variant:     "secondary",
		LocalAction: "client:command_list",
		Commands:    commands,
		Tooltip:     "查看支持的斜杠命令",
	}, true
}
