package deveco

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	"github.com/askie/grix/backend/internal/model"
)

func TestPackage_KeyAndMatch(t *testing.T) {
	p := New()
	if p.Key() != model.AgentClientTypeDeveco {
		t.Fatalf("Key()=%q want=%q", p.Key(), model.AgentClientTypeDeveco)
	}
	if !p.Match(core.MatchContext{Agent: core.AgentInfo{ClientType: model.AgentClientTypeDeveco}}) {
		t.Fatal("expected Match to accept deveco client_type")
	}
	if p.Match(core.MatchContext{Agent: core.AgentInfo{ClientType: model.AgentClientTypeOpenCode}}) {
		t.Fatal("expected Match to reject the plain opencode client_type")
	}
}

func TestPackage_Build_HiddenWithoutBinding(t *testing.T) {
	p := New()
	snap, err := p.Build(context.Background(), core.BuildInput{
		Runtime: toolruntime.Profile{Online: true},
	})
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if snap.Visible {
		t.Fatal("expected snapshot to be hidden without a session binding")
	}
}

func TestPackage_Build_VisibleWithBindingShowsSessionControl(t *testing.T) {
	p := New()
	snap, err := p.Build(context.Background(), core.BuildInput{
		Runtime: toolruntime.Profile{Online: true, LocalActions: []string{"session_control"}},
		Binding: core.BindingInfo{Cwd: "/tmp/proj"},
	})
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if !snap.Visible {
		t.Fatal("expected snapshot to be visible with a session binding")
	}
	found := false
	for _, item := range snap.Items {
		if item.ItemID == "session_control" {
			found = true
			if item.Disabled {
				t.Fatal("expected session_control enabled when online and local action declared")
			}
		}
	}
	if !found {
		t.Fatal("expected a session_control item")
	}
}

// TestPackage_Build_IncludesSlashCommands guards against the round4 review
// finding: agentslashcmd had no "deveco" registration, so the slash_commands
// item never appeared, and ApplyCustomSlashCommands (core/service.go) only
// merges a session's custom commands into an *existing* item — meaning a
// missing registration here silently breaks custom slash commands for every
// deveco agent, not just the built-in list.
func TestPackage_Build_IncludesSlashCommands(t *testing.T) {
	p := New()
	snap, err := p.Build(context.Background(), core.BuildInput{
		Runtime: toolruntime.Profile{Online: true, LocalActions: []string{"session_control"}},
		Binding: core.BindingInfo{Cwd: "/tmp/proj"},
	})
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	var slashItem *toolprotocol.Item
	for i := range snap.Items {
		if snap.Items[i].ItemID == "slash_commands" {
			slashItem = &snap.Items[i]
			break
		}
	}
	if slashItem == nil {
		t.Fatal("expected a slash_commands item (agentslashcmd must register \"deveco\")")
	}
	if len(slashItem.Commands) != 16 {
		t.Fatalf("slash command count=%d want=16", len(slashItem.Commands))
	}
	found := false
	for _, c := range slashItem.Commands {
		if c.Name == "/model" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected /model among the registered deveco slash commands")
	}
}

func TestPackage_HandleAction_UnknownActionRejected(t *testing.T) {
	p := New()
	result, err := p.HandleAction(context.Background(), core.ActionInput{
		Request: toolprotocol.ActionRequest{ActionID: "nope"},
	})
	if err != nil {
		t.Fatalf("HandleAction error: %v", err)
	}
	if result.Outcome != toolprotocol.ActionOutcomeRejected {
		t.Fatalf("Outcome=%q want=rejected", result.Outcome)
	}
	if result.Code != "invalid_action" {
		t.Fatalf("Code=%q want=invalid_action", result.Code)
	}
}
