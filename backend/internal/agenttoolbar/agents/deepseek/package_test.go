package deepseek

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	"github.com/askie/grix/backend/internal/model"
)

type testExecutor struct {
	local []core.LocalActionRequest
	stops []core.StopOutputRequest
}

func (e *testExecutor) DispatchLocalAction(_ context.Context, req core.LocalActionRequest) error {
	e.local = append(e.local, req)
	return nil
}
func (e *testExecutor) StopOutput(_ context.Context, req core.StopOutputRequest) error {
	e.stops = append(e.stops, req)
	return nil
}
func (e *testExecutor) SendStopText(context.Context, core.StopOutputRequest) error { return nil }

func baseInput() core.BuildInput {
	return core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-deepseek"},
		Agent:   core.AgentInfo{AgentID: 9001, OwnerID: 1001, ProviderType: model.AgentProviderAPI, ClientType: model.AgentClientTypeDeepSeek},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"session_control", "set_provider", "set_model", "set_mode", "get_session_usage", "get_rate_limits"},
		},
		Binding: core.BindingInfo{
			BindingID: "dsh:sess-deepseek",
			Cwd:       "/workspace/project",
			Meta: map[string]any{
				"provider_id":               "deepseek-official",
				"model_id":                  "deepseek-v4-pro",
				"mode_id":                   "full_auto",
				"applied_provider_id":       "deepseek-official",
				"applied_model_id":          "deepseek-v4-flash",
				"applied_mode_id":           "approval",
				"settings_revision":         float64(12),
				"applied_settings_revision": float64(11),
				"settings_state":            "pending",
				"available_providers": []any{
					map[string]any{"id": "deepseek-official", "displayName": "DeepSeek"},
					map[string]any{"id": "opencode-go", "displayName": "OpenCode Go"},
				},
				"available_models": []any{
					map[string]any{"id": "deepseek-v4-flash", "displayName": "DeepSeek-V4-Flash"},
					map[string]any{"id": "deepseek-v4-pro", "displayName": "DeepSeek-V4-Pro"},
				},
				"context_window": map[string]any{
					"usedTokens": float64(812400), "totalTokens": float64(1000000),
					"remainingTokens": float64(187600), "usedPercentage": 81.24,
				},
				"provider_quota": map[string]any{
					"provider": "deepseek", "providerLabel": "DeepSeek", "success": true,
					"balance": map[string]any{"kind": "currency", "remaining": 83.52, "unit": "CNY"},
				},
			},
		},
	}
}

func TestBuildVisibilityAndProjection(t *testing.T) {
	pkg := New()
	hidden, err := pkg.Build(context.Background(), core.BuildInput{})
	if err != nil || hidden.Visible || len(hidden.Items) != 0 {
		t.Fatalf("hidden=%+v err=%v", hidden, err)
	}

	snapshot, err := pkg.Build(context.Background(), baseInput())
	if err != nil {
		t.Fatalf("Build() error=%v", err)
	}
	if !snapshot.Visible || len(snapshot.Items) != 6 {
		t.Fatalf("visible=%v items=%d", snapshot.Visible, len(snapshot.Items))
	}
	wantOrder := []string{"session_control", "provider_quota_balance", "context_usage", "select_mode", "select_provider", "select_model"}
	for i, want := range wantOrder {
		if snapshot.Items[i].ItemID != want {
			t.Fatalf("item[%d]=%q want=%q", i, snapshot.Items[i].ItemID, want)
		}
	}
	quota, _ := snapshot.FindItem("provider_quota_balance")
	if quota.Kind != toolprotocol.ItemKindButton || quota.Label != "¥83.52" || quota.BadgeText != "CNY" {
		t.Fatalf("quota=%+v", quota)
	}
	contextItem, _ := snapshot.FindItem("context_usage")
	if contextItem.Variant != "warning" || contextItem.CenterText != "81%" || contextItem.ProgressDetail != "812K / 1M，剩余 188K" {
		t.Fatalf("context=%+v", contextItem)
	}
	mode, _ := snapshot.FindItem("select_mode")
	if !mode.Disabled || !mode.Loading || mode.Value != "full_auto" || len(mode.Options) != 2 || mode.Options[1].Label != "自动（全权限）" {
		t.Fatalf("mode=%+v", mode)
	}
	providerItem, _ := snapshot.FindItem("select_provider")
	if !providerItem.Disabled || !providerItem.Loading || providerItem.Value != "deepseek-official" || len(providerItem.Options) != 2 || providerItem.Options[1].Label != "OpenCode Go" {
		t.Fatalf("provider=%+v", providerItem)
	}
	modelItem, _ := snapshot.FindItem("select_model")
	if !modelItem.Disabled || !modelItem.Loading || modelItem.Value != "deepseek-v4-pro" || len(modelItem.Options) != 2 {
		t.Fatalf("model=%+v", modelItem)
	}
}

func TestBuildActiveRunAndEmptyCatalog(t *testing.T) {
	in := baseInput()
	in.Binding.Meta["settings_state"] = "applied"
	in.Binding.Meta["available_models"] = []any{}
	in.Binding.Meta["available_providers"] = []any{}
	in.Run = toolruntime.RunState{HasActiveRun: true, CanStop: true, RunID: "run-1", State: "streaming"}
	snapshot, err := New().Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build() error=%v", err)
	}
	if snapshot.Items[0].ItemID != "stop_output" || snapshot.Items[0].Label != "" || snapshot.Items[0].Icon != "stop" {
		t.Fatalf("first=%+v", snapshot.Items[0])
	}
	modelItem, _ := snapshot.FindItem("select_model")
	providerItem, _ := snapshot.FindItem("select_provider")
	mode, _ := snapshot.FindItem("select_mode")
	if !modelItem.Disabled || len(modelItem.Options) != 0 || !mode.Disabled || !providerItem.Disabled || len(providerItem.Options) != 0 {
		t.Fatalf("model=%+v provider=%+v mode=%+v", modelItem, providerItem, mode)
	}
}

func TestHandleActionsValidateAndDispatch(t *testing.T) {
	pkg := New()
	in := baseInput()
	in.Binding.Meta["settings_state"] = "applied"
	executor := &testExecutor{}

	modelResult, err := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: in,
		Request:    toolprotocol.ActionRequest{ActionID: "select_model", OptionID: "deepseek-v4-flash"},
		Executor:   executor,
	})
	if err != nil || modelResult.Outcome != toolprotocol.ActionOutcomeAcceptedNoStateChange || len(executor.local) != 1 {
		t.Fatalf("model result=%+v actions=%d err=%v", modelResult, len(executor.local), err)
	}
	request := executor.local[0]
	if request.ActionType != "set_model" || request.TimeoutMs != 15000 || request.Params["actor_id"] != "1001" || request.Params["model_id"] != "deepseek-v4-flash" {
		t.Fatalf("request=%+v", request)
	}

	providerResult, err := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: in,
		Request:    toolprotocol.ActionRequest{ActionID: "select_provider", OptionID: "opencode-go"},
		Executor:   executor,
	})
	if err != nil || providerResult.Outcome != toolprotocol.ActionOutcomeAcceptedNoStateChange || len(executor.local) != 2 {
		t.Fatalf("provider result=%+v actions=%d err=%v", providerResult, len(executor.local), err)
	}
	providerRequest := executor.local[1]
	if providerRequest.ActionType != "set_provider" || providerRequest.Params["provider_id"] != "opencode-go" {
		t.Fatalf("provider request=%+v", providerRequest)
	}

	invalidProvider, _ := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: in,
		Request:    toolprotocol.ActionRequest{ActionID: "select_provider", OptionID: "not-in-catalog"},
		Executor:   executor,
	})
	if invalidProvider.Code != "invalid_option" || len(executor.local) != 2 {
		t.Fatalf("invalid provider=%+v actions=%d", invalidProvider, len(executor.local))
	}

	invalid, _ := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: in,
		Request:    toolprotocol.ActionRequest{ActionID: "select_model", OptionID: "not-in-catalog"},
		Executor:   executor,
	})
	if invalid.Code != "invalid_option" || len(executor.local) != 2 {
		t.Fatalf("invalid=%+v actions=%d", invalid, len(executor.local))
	}

	usage, _ := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: in,
		Request:    toolprotocol.ActionRequest{ActionID: "session_control", OptionID: "usage"},
		Executor:   executor,
	})
	if usage.Outcome != toolprotocol.ActionOutcomeAcceptedNoStateChange || len(executor.local) != 3 || executor.local[2].ActionType != "get_session_usage" {
		t.Fatalf("usage=%+v actions=%+v", usage, executor.local)
	}

	in.Run = toolruntime.RunState{HasActiveRun: true}
	busy, _ := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: in,
		Request:    toolprotocol.ActionRequest{ActionID: "select_mode", OptionID: "full_auto"},
		Executor:   executor,
	})
	if busy.Code != "worker_busy" || len(executor.local) != 3 {
		t.Fatalf("busy=%+v actions=%d", busy, len(executor.local))
	}
}

func TestHandleStopOutput(t *testing.T) {
	in := baseInput()
	in.Run = toolruntime.RunState{HasActiveRun: true, CanStop: true, RunID: "run-1", State: "streaming"}
	executor := &testExecutor{}
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: in, Request: toolprotocol.ActionRequest{ActionID: "stop_output"}, Executor: executor,
	})
	if err != nil || result.Outcome != toolprotocol.ActionOutcomeAcceptedWithImmediateRefresh || len(executor.stops) != 1 {
		t.Fatalf("result=%+v stops=%d err=%v", result, len(executor.stops), err)
	}
}
