package agenttoolbar_test

import (
	"context"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/agenttoolbar/agents/claude"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/hermes"
	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	"github.com/askie/grix/backend/internal/model"
)

type stopBySessionResolver struct {
	input core.BuildInput
}

func (r stopBySessionResolver) Resolve(context.Context, int64, string, int64) (core.BuildInput, error) {
	return r.input, nil
}

type stopBySessionRegistry struct {
	pkg core.Package
}

func (r stopBySessionRegistry) Resolve(core.MatchContext) core.Package { return r.pkg }

type stopBySessionCache struct {
	snapshot toolprotocol.Snapshot
}

func (c *stopBySessionCache) LoadSnapshot(context.Context, int64, string, int64) (toolprotocol.Snapshot, bool, error) {
	return c.snapshot, true, nil
}

func (c *stopBySessionCache) SaveSnapshot(_ context.Context, _ int64, snapshot toolprotocol.Snapshot) (toolprotocol.Snapshot, bool, error) {
	if snapshot.Revision == 0 {
		snapshot.Revision = 1
	}
	c.snapshot = snapshot
	return snapshot, false, nil
}

func (c *stopBySessionCache) DeleteSnapshot(context.Context, int64, string, int64) error { return nil }
func (c *stopBySessionCache) ListIndexedSessions(context.Context, int64, int64) ([]string, error) {
	return nil, nil
}
func (c *stopBySessionCache) ReserveContextWarm(context.Context, int64, int64, string, time.Duration) (bool, error) {
	return false, nil
}
func (c *stopBySessionCache) ReserveRateLimitFetch(context.Context, int64, string, time.Duration) (bool, error) {
	return false, nil
}
func (c *stopBySessionCache) ReserveAction(context.Context, int64, string, int64, string) (bool, core.ActionAck, error) {
	return true, core.ActionAck{}, nil
}
func (c *stopBySessionCache) CompleteAction(context.Context, int64, string, int64, string, core.ActionAck) error {
	return nil
}

type stopBySessionNotifier struct{}

func (stopBySessionNotifier) Sync(context.Context, int64, toolprotocol.Snapshot) error { return nil }

// 手打 /stop 走 StopOutputBySession 后，必须落到各 agent 包 stop_output 分支原本的
// 停止实现上：Claude 等走 Executor.StopOutput（下发 event_stop），Hermes 走
// Executor.SendStopText（先 /stop 文本再 event_stop）。
func TestStopOutputBySessionUsesAgentStopPath(t *testing.T) {
	cases := []struct {
		name        string
		clientType  string
		pkg         core.Package
		stopViaText bool
	}{
		{name: "claude", clientType: model.AgentClientTypeClaude, pkg: claude.New()},
		{name: "hermes", clientType: model.AgentClientTypeHermes, pkg: hermes.New(), stopViaText: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			executor := &packageTestExecutor{}
			svc := core.NewService(
				stopBySessionResolver{input: core.BuildInput{
					OwnerID: 1001,
					Session: core.SessionInfo{SessionID: "sess-1"},
					Agent: core.AgentInfo{
						AgentID:      9001,
						OwnerID:      1001,
						ProviderType: model.AgentProviderAPI,
						ClientType:   tc.clientType,
					},
					Runtime: toolruntime.Profile{Online: true, LocalActions: []string{"session_control"}},
					Binding: core.BindingInfo{Cwd: "/workspace/project"},
					Run: toolruntime.RunState{
						HasActiveRun: true,
						RunID:        "run-1",
						CanStop:      true,
						State:        "streaming",
					},
				}},
				stopBySessionRegistry{pkg: tc.pkg},
				&stopBySessionCache{},
				stopBySessionNotifier{},
				executor,
			)

			ack, err := svc.StopOutputBySession(context.Background(), 1001, "sess-1", 9001, "slash_stop:9001:7001")
			if err != nil {
				t.Fatalf("StopOutputBySession() err = %v", err)
			}
			if !ack.Accepted {
				t.Fatalf("ack = %+v, want accepted", ack)
			}

			got := executor.stopRequests
			other := executor.stopTextRequests
			if tc.stopViaText {
				got, other = executor.stopTextRequests, executor.stopRequests
			}
			if len(got) != 1 {
				t.Fatalf("stop dispatches = %d, want 1 (stopViaText=%v)", len(got), tc.stopViaText)
			}
			if len(other) != 0 {
				t.Fatalf("unexpected dispatches on the other stop path = %d, want 0", len(other))
			}
			if got[0].RunID != "run-1" || got[0].SessionID != "sess-1" || got[0].AgentID != 9001 || got[0].OwnerID != 1001 {
				t.Fatalf("stop request = %+v, want the active run of sess-1/agent 9001", got[0])
			}
		})
	}
}
