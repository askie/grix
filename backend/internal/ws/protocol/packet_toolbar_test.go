package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAgentToolbarItemPayloadMarshalsProgressWindowMinutes(t *testing.T) {
	raw, err := json.Marshal(AgentToolbarItemPayload{ProgressWindowMinutes: 10080})
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	if !strings.Contains(string(raw), `"progress_window_minutes":10080`) {
		t.Fatalf("payload=%s, want progress_window_minutes", raw)
	}
}
