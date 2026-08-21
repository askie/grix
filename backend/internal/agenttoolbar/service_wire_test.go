package agenttoolbar

import (
	"testing"

	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
)

func TestToolnotifierSnapshotIncludesProgressWindowMinutes(t *testing.T) {
	wire := toolnotifierSnapshot(toolprotocol.Snapshot{
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
