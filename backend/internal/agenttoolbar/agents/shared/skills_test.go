package shared

import (
	"testing"

	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
)

// 技能通过 /<name> 斜杠命令调用，Exec 必须等于技能名；
// Trigger 是“何时使用”的自然语言提示，绝不能作为待插入命令文本。
func TestBuildSkillsItemExecIsName(t *testing.T) {
	item := BuildSkillsItem([]toolruntime.SkillEntry{
		{Name: "grix-query", Description: "查找联系人", Trigger: "当用户要查找联系人时"},
		{Name: "tailnet-file-share", Description: "分享文件"},
	})

	if len(item.Commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(item.Commands))
	}
	for _, c := range item.Commands {
		if c.Exec != c.Name {
			t.Errorf("exec must equal name; got name=%q exec=%q", c.Name, c.Exec)
		}
	}
}

// 名称为空的技能应被跳过。
func TestBuildSkillsItemSkipsEmptyName(t *testing.T) {
	item := BuildSkillsItem([]toolruntime.SkillEntry{
		{Name: "  ", Description: "无名", Trigger: "x"},
		{Name: "grix-admin"},
	})
	if len(item.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(item.Commands))
	}
	if item.Commands[0].Exec != "grix-admin" {
		t.Errorf("exec=%q, want grix-admin", item.Commands[0].Exec)
	}
}
