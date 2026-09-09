package qodercli

import (
	"testing"

	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	"github.com/askie/grix/backend/internal/model"
)

func TestKeyAndMatch(t *testing.T) {
	p := New()
	if p.Key() != model.AgentClientTypeQoderCLI {
		t.Fatalf("Key() = %q, want %q", p.Key(), model.AgentClientTypeQoderCLI)
	}
	if !p.Match(core.MatchContext{Agent: core.AgentInfo{ClientType: model.AgentClientTypeQoderCLI}}) {
		t.Errorf("Match() should be true for client_type=%q", model.AgentClientTypeQoderCLI)
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
