package core

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/agentslashcmd"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	"github.com/askie/grix/backend/internal/model"
)

func customSlashCommandsService(items []toolprotocol.Item, custom []agentslashcmd.SlashCommand) *Service {
	return NewService(
		testResolver{buildInput: BuildInput{
			OwnerID:             9301,
			Session:             SessionInfo{SessionID: "s-custom", SessionType: model.SessionTypeDirect},
			Agent:               AgentInfo{AgentID: 9401, ClientType: "claude"},
			Language:            "en",
			CustomSlashCommands: custom,
		}},
		testRegistry{pkg: testPackage{snapshot: toolprotocol.Snapshot{Visible: true, Items: items}}},
		&testCache{},
		noopNotifier{},
		noopExecutor{},
	)
}

func findSnapshotItem(t *testing.T, snapshot toolprotocol.Snapshot, itemID string) toolprotocol.Item {
	t.Helper()
	item, ok := snapshot.FindItem(itemID)
	if !ok {
		t.Fatalf("snapshot must carry item %s, got=%+v", itemID, snapshot.Items)
	}
	return item
}

// 自定义命令按创建顺序追加到内置命令之后，带 source=custom；
// 说明文字是用户自己写的，即使请求语言是 en 也不能被 i18n 改写。
func TestGetSnapshotAppendsCustomSlashCommands(t *testing.T) {
	svc := customSlashCommandsService(
		[]toolprotocol.Item{{
			ItemID:      "slash_commands",
			GroupID:     "slash_commands",
			Kind:        toolprotocol.ItemKindButton,
			ActionID:    "slash_commands",
			LocalAction: "client:command_list",
			Tooltip:     "查看支持的斜杠命令",
			Commands: []toolprotocol.CommandItem{
				{ID: "/clear", Name: "/clear", Description: "清空上下文", Exec: "/clear"},
			},
		}},
		[]agentslashcmd.SlashCommand{
			{Name: "/deploy", Description: "发布到预发环境"},
			{Name: "/standup", Description: ""},
		},
	)

	snapshot, err := svc.GetSnapshot(context.Background(), 9301, "s-custom", 0)
	if err != nil {
		t.Fatalf("get snapshot: %v", err)
	}
	item := findSnapshotItem(t, snapshot, "slash_commands")
	if len(item.Commands) != 3 {
		t.Fatalf("expected builtin + 2 custom commands, got=%+v", item.Commands)
	}
	if item.Commands[0].Name != "/clear" || item.Commands[0].Source != "" {
		t.Fatalf("builtin command must stay first and unmarked, got=%+v", item.Commands[0])
	}
	want := []toolprotocol.CommandItem{
		{ID: "/deploy", Name: "/deploy", Description: "发布到预发环境", Exec: "/deploy", Source: "custom"},
		{ID: "/standup", Name: "/standup", Description: "", Exec: "/standup", Source: "custom"},
	}
	for i, expected := range want {
		if item.Commands[i+1] != expected {
			t.Fatalf("custom command %d mismatch: want=%+v got=%+v", i, expected, item.Commands[i+1])
		}
	}
}

// 快照里没有斜杠命令项时（工具栏不可见，或该 client_type 没有内置命令）不合并，
// 也不会凭空造出一个命令面板。
func TestGetSnapshotSkipsCustomSlashCommandsWithoutItem(t *testing.T) {
	svc := customSlashCommandsService(
		[]toolprotocol.Item{{
			ItemID:   "select_model",
			GroupID:  "session",
			Kind:     toolprotocol.ItemKindSelect,
			ActionID: "set_model",
		}},
		[]agentslashcmd.SlashCommand{{Name: "/deploy", Description: "发布到预发环境"}},
	)

	snapshot, err := svc.GetSnapshot(context.Background(), 9301, "s-custom", 0)
	if err != nil {
		t.Fatalf("get snapshot: %v", err)
	}
	if _, ok := snapshot.FindItem("slash_commands"); ok {
		t.Fatalf("custom commands must not create a command panel, got=%+v", snapshot.Items)
	}
}
