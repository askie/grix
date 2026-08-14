package pi

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	"github.com/askie/grix/backend/internal/model"
)

type testExecutor struct {
	localActions []core.LocalActionRequest
	stopRequests []core.StopOutputRequest
}

func (e *testExecutor) DispatchLocalAction(_ context.Context, req core.LocalActionRequest) error {
	e.localActions = append(e.localActions, req)
	return nil
}

func (e *testExecutor) StopOutput(_ context.Context, req core.StopOutputRequest) error {
	e.stopRequests = append(e.stopRequests, req)
	return nil
}

func (e *testExecutor) SendStopText(_ context.Context, req core.StopOutputRequest) error {
	return nil
}

func buildInput(online bool, localActions []string, hasCwd bool) core.BuildInput {
	cwd := ""
	if hasCwd {
		cwd = "/workspace/project"
	}
	return core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-1"},
		Agent: core.AgentInfo{
			AgentID:    9001,
			OwnerID:    1001,
			ClientType: model.AgentClientTypePi,
		},
		Runtime: toolruntime.Profile{
			Online:       online,
			LocalActions: localActions,
		},
		Binding: core.BindingInfo{
			Cwd: cwd,
			Meta: map[string]any{
				"model_id": "k3",
				"available_models": []any{
					map[string]any{"id": "k3", "display_name": "Kimi K3"},
				},
			},
		},
	}
}

func findItem(snap toolprotocol.Snapshot, itemID string) *toolprotocol.Item {
	for i := range snap.Items {
		if snap.Items[i].ItemID == itemID {
			return &snap.Items[i]
		}
	}
	return nil
}

func TestPiBuildProviderQuotaFromBindingMeta(t *testing.T) {
	in := buildInput(true, []string{"session_control", "set_model", "get_rate_limits"}, true)
	in.Binding.Meta["provider_quota"] = map[string]any{
		"provider":      "kimi",
		"providerLabel": "Kimi",
		"tiers": []any{
			map[string]any{"name": "five_hour", "label": "5h limit", "usedPercent": 36.0, "resetsAt": "2026-07-25T14:58:07Z"},
			map[string]any{"name": "weekly_limit", "label": "Weekly limit", "usedPercent": 87.0, "resetsAt": "2026-07-27T04:58:07Z"},
		},
	}
	snap, err := New().Build(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	fiveHour := findItem(snap, "provider_quota_five_hour")
	if fiveHour == nil {
		t.Fatal("provider_quota_five_hour item not found")
	}
	if fiveHour.Percent != 36 {
		t.Fatalf("five_hour percent=%v want=36", fiveHour.Percent)
	}
	if fiveHour.LocalAction != "get_rate_limits" {
		t.Fatalf("five_hour localAction=%q want=get_rate_limits", fiveHour.LocalAction)
	}
	weekly := findItem(snap, "provider_quota_weekly_limit")
	if weekly == nil {
		t.Fatal("provider_quota_weekly_limit item not found")
	}
	if weekly.Percent != 87 {
		t.Fatalf("weekly percent=%v want=87", weekly.Percent)
	}
}

func TestPiBuildProviderQuotaPlaceholderWhenDeclared(t *testing.T) {
	// 无 provider_quota 数据时不渲染 0% 的 5H 占位。
	snap, err := New().Build(context.Background(), buildInput(true, []string{"session_control", "get_rate_limits"}, true))
	if err != nil {
		t.Fatal(err)
	}
	if findItem(snap, "provider_quota_five_hour") != nil {
		t.Fatal("provider_quota_five_hour placeholder must not appear without quota data")
	}
}

func TestPiBuildProviderQuotaHiddenWithoutLocalAction(t *testing.T) {
	snap, err := New().Build(context.Background(), buildInput(true, []string{"session_control", "set_model"}, true))
	if err != nil {
		t.Fatal(err)
	}
	if findItem(snap, "provider_quota_five_hour") != nil {
		t.Fatal("quota placeholder must not appear when get_rate_limits not declared")
	}
}

func TestPiHandleGetRateLimitsDispatches(t *testing.T) {
	bi := buildInput(true, []string{"session_control", "get_rate_limits"}, true)
	exec := &testExecutor{}
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: bi,
		Request:    toolprotocol.ActionRequest{SessionID: "sess-1", ActionID: "provider_quota_five_hour", Event: "click"},
		Executor:   exec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome == toolprotocol.ActionOutcomeRejected {
		t.Fatalf("get_rate_limits rejected: %s", result.Code)
	}
	if len(exec.localActions) != 1 || exec.localActions[0].ActionType != "get_rate_limits" {
		t.Fatalf("expected get_rate_limits dispatch, got %+v", exec.localActions)
	}
	if exec.localActions[0].Params["session_id"] != "sess-1" {
		t.Fatalf("session_id=%v want=sess-1", exec.localActions[0].Params["session_id"])
	}
}
