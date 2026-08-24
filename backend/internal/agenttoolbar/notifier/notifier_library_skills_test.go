package notifier

import (
	"testing"

	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
)

func TestToWireSnapshotIncludesLibrarySkills(t *testing.T) {
	wire := toWireSnapshot(toolprotocol.Snapshot{
		SessionID: "sess-1",
		AgentID:   9001,
		LibrarySkills: []toolruntime.LibrarySkillEntry{{
			Name: "demo",
			EnableScopes: toolruntime.LibrarySkillEnableScopes{
				Global:  "none",
				Project: "unavailable",
			},
		}},
	})
	if len(wire.LibrarySkills) != 1 || wire.LibrarySkills[0].Name != "demo" {
		t.Fatalf("library_skills=%+v", wire.LibrarySkills)
	}
	if wire.LibrarySkills[0].EnableScopes.Global != "none" {
		t.Fatalf("global=%q", wire.LibrarySkills[0].EnableScopes.Global)
	}
}

func TestToWireSnapshotIncludesProgressWindowMinutes(t *testing.T) {
	wire := toWireSnapshot(toolprotocol.Snapshot{
		Items: []toolprotocol.Item{{
			ItemID:                "rate-limit-extra",
			Kind:                  toolprotocol.ItemKindProgress,
			ProgressWindowMinutes: 10080,
		}},
	})
	if len(wire.Items) != 1 {
		t.Fatalf("items=%d, want 1", len(wire.Items))
	}
	if wire.Items[0].ProgressWindowMinutes != 10080 {
		t.Fatalf("progress_window_minutes=%v, want 10080", wire.Items[0].ProgressWindowMinutes)
	}
}
