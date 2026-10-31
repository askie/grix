package core_test

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	"github.com/askie/grix/backend/internal/agenttoolbar/store"
)

type mockReminder struct {
	checkedSnapshots []toolprotocol.Snapshot
	ownerIDs         []int64
}

func (m *mockReminder) CheckSnapshot(_ context.Context, ownerID int64, snapshot toolprotocol.Snapshot) error {
	m.ownerIDs = append(m.ownerIDs, ownerID)
	m.checkedSnapshots = append(m.checkedSnapshots, snapshot)
	return nil
}

type testResolver struct {
	buildInput core.BuildInput
}

func (r testResolver) Resolve(_ context.Context, _ int64, _ string, _ int64) (core.BuildInput, error) {
	return r.buildInput, nil
}

type testRegistry struct {
	pkg core.Package
}

func (r testRegistry) Resolve(_ core.MatchContext) core.Package {
	return r.pkg
}

type testPkg struct {
	items []toolprotocol.Item
}

func (p testPkg) Key() string { return "test" }
func (p testPkg) Match(_ core.MatchContext) bool {
	return true
}
func (p testPkg) Build(_ context.Context, _ core.BuildInput) (toolprotocol.Snapshot, error) {
	return toolprotocol.Snapshot{
		Visible: true,
		Items:   p.items,
	}, nil
}
func (p testPkg) HandleAction(_ context.Context, _ core.ActionInput) (toolprotocol.ActionResult, error) {
	return toolprotocol.ActionResult{}, nil
}

func TestService_MaybeCheckQuotaReminderCalledOnSnapshot(t *testing.T) {
	reminder := &mockReminder{}
	pkg := &testPkg{
		items: []toolprotocol.Item{
			{
				ItemID:                "rate_limit_primary",
				Kind:                  toolprotocol.ItemKindProgress,
				CenterText:            "5H",
				ProgressWindowMinutes: 300,
				Percent:               95.0,
				ProgressDetail:        "2026-10-31T15:00:00Z",
			},
		},
	}

	svc := core.NewService(
		testResolver{
			buildInput: core.BuildInput{
				OwnerID: 1001,
				Session: core.SessionInfo{SessionID: "sess-1"},
				Agent:   core.AgentInfo{AgentID: 2001},
			},
		},
		testRegistry{pkg: pkg},
		store.NewCacheStore(),
		nil,
		nil,
	)
	svc.SetQuotaReminder(reminder)

	// Call RefreshSession
	err := svc.RefreshSession(context.Background(), 1001, "sess-1", "local_action_result")
	if err != nil {
		t.Fatalf("RefreshSession err: %v", err)
	}

	if len(reminder.checkedSnapshots) != 1 {
		t.Fatalf("expected 1 checked snapshot, got %d", len(reminder.checkedSnapshots))
	}
	if reminder.ownerIDs[0] != 1001 {
		t.Fatalf("ownerID=%d, want 1001", reminder.ownerIDs[0])
	}
	snap := reminder.checkedSnapshots[0]
	if snap.SessionID != "sess-1" || snap.AgentID != 2001 {
		t.Fatalf("snapshot session=%q agent=%d, want sess-1 / 2001", snap.SessionID, snap.AgentID)
	}
}
