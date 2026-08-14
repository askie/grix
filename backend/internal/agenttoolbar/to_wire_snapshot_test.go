package agenttoolbar

import (
	"testing"

	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
)

func TestToWireSnapshotIncludesLibrarySkills(t *testing.T) {
	wire := ToWireSnapshot(toolprotocol.Snapshot{
		SessionID: "sess-1",
		AgentID:   9001,
		ToolbarID: "tb-1",
		Revision:  3,
		Visible:   true,
		UpdatedAt: 100,
		Items:     []toolprotocol.Item{{ItemID: "skills", Kind: "button"}},
		LibrarySkills: []toolruntime.LibrarySkillEntry{{
			Name:        "embedded-captions",
			Description: "captions",
			Digest:      "abc",
			Dir:         "embedded-captions",
			OwnerID:     42,
			System:      false,
			EnableScopes: toolruntime.LibrarySkillEnableScopes{
				Global:  "unmanaged",
				Project: "unavailable",
			},
		}},
	})

	if len(wire.LibrarySkills) != 1 {
		t.Fatalf("library_skills len=%d want=1", len(wire.LibrarySkills))
	}
	got := wire.LibrarySkills[0]
	if got.Name != "embedded-captions" {
		t.Fatalf("name=%q", got.Name)
	}
	if got.OwnerID != 42 {
		t.Fatalf("owner_id=%d", got.OwnerID)
	}
	if got.EnableScopes.Global != "unmanaged" || got.EnableScopes.Project != "unavailable" {
		t.Fatalf("enable_scopes=%+v", got.EnableScopes)
	}
}

func TestToWireSnapshotOmitsEmptyLibrarySkills(t *testing.T) {
	wire := ToWireSnapshot(toolprotocol.Snapshot{
		SessionID: "sess-1",
		Items:     []toolprotocol.Item{},
	})
	if wire.LibrarySkills != nil {
		t.Fatalf("library_skills=%v want nil", wire.LibrarySkills)
	}
}
