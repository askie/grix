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
