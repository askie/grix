package handler

import (
	"strings"
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

// TestNormalizeAgentSessionProviderKey_DevecoOwnBucket guards against the
// round5 finding: deveco (Huawei DevEco Code, an opencode fork) registers its
// own session-history reader under "deveco" on the connector side — a
// different sqlite db than opencode's (see grix-connector's
// adapter/opencode/session-history.ts) — so it must not share opencode's
// bucket or fall through to the "acp" default.
func TestNormalizeAgentSessionProviderKey_DevecoOwnBucket(t *testing.T) {
	if got := normalizeAgentSessionProviderKey(model.AgentClientTypeDeveco); got != "deveco" {
		t.Fatalf("normalizeAgentSessionProviderKey(deveco) = %q, want %q", got, "deveco")
	}
}

// TestNormalizeAgentSessionProviderKey_OpencodeDeepseekOwnBuckets guards
// against the round6 finding: opencode and deepseek both fell through to the
// "acp" default here, so agent_session_sync_states/agent_native_message_imports
// rows and the sync_history dispatch's provider_key all landed in the generic
// "acp" bucket, which grix-connector's session-history registry doesn't
// recognize ("provider acp does not support sync_history") — see
// grix-connector's adapter/opencode/session-history.ts
// registerSessionHistoryReader('opencode', ...) and
// adapter/deepseek-harness/session-history.ts
// registerSessionHistoryReader('deepseek-harness', ...).
func TestNormalizeAgentSessionProviderKey_OpencodeDeepseekOwnBuckets(t *testing.T) {
	if got := normalizeAgentSessionProviderKey(model.AgentClientTypeOpenCode); got != "opencode" {
		t.Fatalf("normalizeAgentSessionProviderKey(opencode) = %q, want %q", got, "opencode")
	}
	if got := normalizeAgentSessionProviderKey(model.AgentClientTypeDeepSeek); got != "deepseek-harness" {
		t.Fatalf("normalizeAgentSessionProviderKey(deepseek) = %q, want %q", got, "deepseek-harness")
	}
}

// TestSanitizeReportedProviderKey guards the review finding that
// firstTrimmed(bindResp.ProviderKey, providerKey) used to trust ANY
// non-empty connector-reported provider_key unconditionally, letting a
// misbehaving or out-of-date connector build write an arbitrary string into
// agent_session_bindings.provider_key. A recognized report still wins over
// the backend's own computation (needed for the opencode family, whose
// bucket the connector derives from the actual spawned command); anything
// unrecognized or oversized falls back to the computed value instead.
func TestSanitizeReportedProviderKey(t *testing.T) {
	cases := []struct {
		name     string
		reported string
		computed string
		want     string
	}{
		{"empty report keeps computed", "", "opencode", "opencode"},
		{"whitespace-only report keeps computed", "   ", "opencode", "opencode"},
		{"recognized report wins over computed", "deveco", "opencode", "deveco"},
		{"acp is a recognized bucket", "acp", "opencode", "acp"},
		{"unrecognized report falls back to computed", "some-made-up-bucket", "opencode", "opencode"},
		{"oversized report falls back to computed", strings.Repeat("a", 33), "opencode", "opencode"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sanitizeReportedProviderKey(c.reported, c.computed, 1, "sess-1"); got != c.want {
				t.Fatalf("sanitizeReportedProviderKey(%q, %q) = %q, want %q", c.reported, c.computed, got, c.want)
			}
		})
	}
}
