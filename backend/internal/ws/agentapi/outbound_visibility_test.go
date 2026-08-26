package agentapi

import (
	"reflect"
	"testing"
	"time"
)

func TestResolveOutboundVisibleToExplicitWins(t *testing.T) {
	m := &Manager{}
	m.rememberOutboundVisibility(7, 1, "s1", 2, []int64{100})
	got := m.ResolveOutboundVisibleTo(7, 1, "s1", "", 0, []int64{42})
	if !reflect.DeepEqual(got, []int64{42}) {
		t.Fatalf("explicit visibility must win, got %v", got)
	}
}

func TestResolveOutboundVisibleToUsesLiveRunTrigger(t *testing.T) {
	m := &Manager{runs: map[string]*activeAgentRun{
		"evt-1": {EventID: "evt-1", SessionID: "s1", OwnerID: 1, AgentID: 7, SessionType: 2, TriggerVisibleTo: []int64{100}},
		"evt-2": {EventID: "evt-2", SessionID: "s1", OwnerID: 1, AgentID: 7, SessionType: 2},
	}}
	if got := m.ResolveOutboundVisibleTo(7, 1, "s1", "evt-1", 0, nil); !reflect.DeepEqual(got, []int64{100}) {
		t.Fatalf("hidden trigger run must hide output, got %v", got)
	}
	// A known public run is authoritative even if the session cache says hidden.
	m.rememberOutboundVisibility(7, 1, "s1", 2, []int64{100})
	if got := m.ResolveOutboundVisibleTo(7, 1, "s1", "evt-2", 0, nil); got != nil {
		t.Fatalf("public trigger run must stay public, got %v", got)
	}
}

func TestResolveOutboundVisibleToEventlessFallsBackToSessionCache(t *testing.T) {
	m := &Manager{}
	m.rememberOutboundVisibility(7, 1, "s1", 2, []int64{100})
	if got := m.ResolveOutboundVisibleTo(7, 1, "s1", "", 0, nil); !reflect.DeepEqual(got, []int64{100}) {
		t.Fatalf("eventless output must inherit latest hidden trigger, got %v", got)
	}
	if got := m.ResolveOutboundVisibleTo(7, 1, "s1", "expired-evt", 0, nil); !reflect.DeepEqual(got, []int64{100}) {
		t.Fatalf("expired event output must inherit latest hidden trigger, got %v", got)
	}
	// A newer public trigger overrides the hidden one.
	m.rememberOutboundVisibility(7, 1, "s1", 2, nil)
	if got := m.ResolveOutboundVisibleTo(7, 1, "s1", "", 0, nil); got != nil {
		t.Fatalf("latest public trigger must make output public, got %v", got)
	}
	// Other agents and other sessions are isolated.
	m.rememberOutboundVisibility(7, 1, "s1", 2, []int64{100})
	if got := m.ResolveOutboundVisibleTo(8, 1, "s1", "", 0, nil); got != nil {
		t.Fatalf("other agent must not inherit, got %v", got)
	}
	if got := m.ResolveOutboundVisibleTo(7, 1, "s2", "", 0, nil); got != nil {
		t.Fatalf("other session must not inherit, got %v", got)
	}
	// A shared agent serving another owner in the same group is isolated.
	if got := m.ResolveOutboundVisibleTo(7, 2, "s1", "", 0, nil); got != nil {
		t.Fatalf("other owner must not inherit, got %v", got)
	}
}

func TestRememberOutboundVisibilityNonGroupIsPublicAndExpires(t *testing.T) {
	m := &Manager{}
	m.rememberOutboundVisibility(7, 1, "dm", 1, []int64{100})
	if vt, ok := m.lookupOutboundVisibility(7, 1, "dm"); !ok || vt != nil {
		t.Fatalf("non-group sessions must be cached as public, got ok=%v vt=%v", ok, vt)
	}
	m.rememberOutboundVisibility(7, 1, "s1", 2, []int64{100})
	m.outboundVisMu.Lock()
	entry := m.outboundVis[outboundVisibilityKey(7, 1, "s1")]
	entry.expireAt = time.Now().Add(-time.Second).UnixMilli()
	m.outboundVis[outboundVisibilityKey(7, 1, "s1")] = entry
	m.outboundVisMu.Unlock()
	if _, ok := m.lookupOutboundVisibility(7, 1, "s1"); ok {
		t.Fatal("expired cache entry must be dropped")
	}
}

func TestResolveOutboundVisibleToSessionRunFallback(t *testing.T) {
	m := &Manager{
		runs:    map[string]*activeAgentRun{"evt-1": {EventID: "evt-1", SessionID: "s1", OwnerID: 1, AgentID: 7, SessionType: 2, TriggerVisibleTo: []int64{100}}},
		runBySX: map[string]string{activeRunSessionOwnerKey("s1", 1): "evt-1"},
	}
	if got := m.ResolveOutboundVisibleTo(7, 1, "s1", "stale", 0, nil); !reflect.DeepEqual(got, []int64{100}) {
		t.Fatalf("session active run must steer eventless output, got %v", got)
	}
}
