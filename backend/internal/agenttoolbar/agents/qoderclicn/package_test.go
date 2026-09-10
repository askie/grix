package qoderclicn

import (
	"testing"

	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	"github.com/askie/grix/backend/internal/model"
)

func TestKeyAndMatch(t *testing.T) {
	p := New()
	if p.Key() != model.AgentClientTypeQoderCLICN {
		t.Fatalf("Key() = %q, want %q", p.Key(), model.AgentClientTypeQoderCLICN)
	}
	if !p.Match(core.MatchContext{Agent: core.AgentInfo{ClientType: model.AgentClientTypeQoderCLICN}}) {
		t.Errorf("Match() should be true for client_type=%q", model.AgentClientTypeQoderCLICN)
	}
	if p.Match(core.MatchContext{Agent: core.AgentInfo{ClientType: "other"}}) {
		t.Errorf("Match() should be false for a different client_type")
	}
}

func TestBuild_HiddenWithoutBinding(t *testing.T) {
	p := New()
	snap, err := p.Build(nil, core.BuildInput{})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Visible {
		t.Error("Build() should be invisible when there is no session binding")
	}
}

// TestBuild_IncludesSlashCommands guards against the round5 finding: this
// package built a snapshot with no slash_commands item because agentslashcmd
// had no registration for this client_type. ApplyCustomSlashCommands
// (core/service.go) only merges a session's custom commands into an
// *existing* item, so a missing registration silently breaks custom slash
// commands too, not just the built-in list.
func TestBuild_IncludesSlashCommands(t *testing.T) {
	p := New()
	snap, err := p.Build(nil, core.BuildInput{
		Runtime: toolruntime.Profile{Online: true, LocalActions: []string{"session_control"}},
		Binding: core.BindingInfo{Cwd: "/tmp/proj"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var slashItem *toolprotocol.Item
	for i := range snap.Items {
		if snap.Items[i].ItemID == "slash_commands" {
			slashItem = &snap.Items[i]
			break
		}
	}
	if slashItem == nil {
		t.Fatalf("expected a slash_commands item (agentslashcmd must register %q)", model.AgentClientTypeQoderCLICN)
	}
	if len(slashItem.Commands) != 17 {
		t.Fatalf("slash command count=%d want=%d", len(slashItem.Commands), 17)
	}
	found := false
	for _, c := range slashItem.Commands {
		if c.Name == "/goal" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected /goal among the registered slash commands")
	}
}
