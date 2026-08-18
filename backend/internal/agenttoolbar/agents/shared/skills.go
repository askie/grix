package shared

import (
	"strings"

	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
)

func BuildSkillsItem(skills []toolruntime.SkillEntry) toolprotocol.Item {
	commands := make([]toolprotocol.CommandItem, 0, len(skills))
	for _, s := range skills {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			continue
		}
		// 技能通过 /<name> 斜杠命令调用；Trigger 是“何时使用”的自然语言提示，
		// 不能作为待插入的命令文本，否则点击技能会把整段提示句塞进输入框。
		commands = append(commands, toolprotocol.CommandItem{
			ID:          name,
			Name:        name,
			Description: strings.TrimSpace(s.Description),
			Exec:        name,
			Source:      strings.TrimSpace(s.Source),
			Path:        strings.TrimSpace(s.Path),
			Managed:     s.Managed,
			SyncState:   s.SyncState,
		})
	}
	return toolprotocol.Item{
		ItemID:      "skills",
		GroupID:     "skills",
		Kind:        toolprotocol.ItemKindButton,
		ActionID:    "skills",
		Label:       "技能",
		Icon:        "spark",
		Variant:     "secondary",
		LocalAction: "client:command_list",
		Commands:    commands,
		Disabled:    len(commands) == 0,
		Tooltip:     skillsTooltip(len(commands)),
	}
}

func skillsTooltip(count int) string {
	if count == 0 {
		return "暂无可用技能"
	}
	return "查看可用技能"
}
