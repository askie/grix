package handler

import (
	"testing"

	"github.com/askie/grix/backend/internal/model"
)

// TestNormalizeAgentSessionProviderKey_OmpSharesPiBucket guards against the
// round5 finding: omp (adapterType "pi" on the connector side, same session
// history/skill/session-scan provider key as pi — see grix-connector's
// pi/session-history.ts and pi/session-scanner.ts) fell through to the "acp"
// default here, mixing its session binding / rate-limit bucket with every
// other unclassified ACP client instead of sharing pi's.
func TestNormalizeAgentSessionProviderKey_OmpSharesPiBucket(t *testing.T) {
	if got := normalizeAgentSessionProviderKey(model.AgentClientTypeOmp); got != "pi" {
		t.Fatalf("normalizeAgentSessionProviderKey(omp) = %q, want %q", got, "pi")
	}
	if got := normalizeAgentSessionProviderKey(model.AgentClientTypePi); got != "pi" {
		t.Fatalf("normalizeAgentSessionProviderKey(pi) = %q, want %q", got, "pi")
	}
	// A genuinely unclassified ACP client still falls back to "acp".
	if got := normalizeAgentSessionProviderKey(model.AgentClientTypeQoderCLI); got != "acp" {
		t.Fatalf("normalizeAgentSessionProviderKey(qodercli) = %q, want %q", got, "acp")
	}
}
