package notification

import (
	"context"
	"testing"
	"time"
)

func TestActionableSkipReasonStaleOnlyForActionableEvents(t *testing.T) {
	now := time.Now()
	old := now.Add(-actionableMaxAge - time.Second).UnixMilli()

	approval := &AgentNotificationEvent{EventKey: EventApprovalRequested, SessionID: "s", CreatedAtMs: old,
		ActionMeta: &ActionMeta{ApprovalCommandID: "cmd"}}
	if r := ActionableSkipReason(context.Background(), approval, now); r == "" {
		t.Fatal("expected stale approval to be skipped")
	}
	fresh := &AgentNotificationEvent{EventKey: EventApprovalRequested, SessionID: "s", CreatedAtMs: now.UnixMilli(),
		ActionMeta: &ActionMeta{ApprovalCommandID: "cmd"}}
	if r := ActionableSkipReason(context.Background(), fresh, now); r != "" {
		t.Fatalf("fresh approval must pass, got %q", r)
	}
	noStamp := &AgentNotificationEvent{EventKey: EventApprovalRequested, SessionID: "s",
		ActionMeta: &ActionMeta{ApprovalCommandID: "cmd"}}
	if r := ActionableSkipReason(context.Background(), noStamp, now); r != "" {
		t.Fatalf("unstamped event (older ws node) must pass, got %q", r)
	}
	done := &AgentNotificationEvent{EventKey: EventTaskCompleted, SessionID: "s", CreatedAtMs: old}
	if r := ActionableSkipReason(context.Background(), done, now); r != "" {
		t.Fatalf("task_completed is never gated, got %q", r)
	}
}
