package hermes

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
)

func TestBuildShowsConfiguredModelAsSelectableOption(t *testing.T) {
	in := core.BuildInput{
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"set_model"},
		},
		Binding: core.BindingInfo{Meta: map[string]any{
			"model_id":       "deepseek-v3.2",
			"model_provider": "opencode-go",
			"available_models": []any{
				map[string]any{"id": "deepseek-v3.2", "displayName": "DeepSeek V3.2", "provider": "opencode-go"},
			},
		}},
	}

	snapshot, err := New().Build(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	item, ok := snapshot.FindItem("select_model")
	if !ok {
		t.Fatal("select_model item not found")
	}
	if item.ActionID != "select_model" {
		t.Fatalf("action_id=%q want=select_model", item.ActionID)
	}
	if item.Disabled {
		t.Fatal("select_model should be enabled when set_model is declared")
	}
	if item.Value != "opencode-go:deepseek-v3.2" || item.Label != "DeepSeek V3.2" || item.BadgeText != "" {
		t.Fatalf("value=%q label=%q badge=%q", item.Value, item.Label, item.BadgeText)
	}
	if len(item.Options) != 1 {
		t.Fatalf("options len=%d want=1", len(item.Options))
	}
	option := item.Options[0]
	if option.OptionID != "opencode-go:deepseek-v3.2" || option.Label != "DeepSeek V3.2" || option.Disabled {
		t.Fatalf("option=%+v", option)
	}
}

func TestSelectModelActionDispatchesSetModel(t *testing.T) {
	exec := &hermesTestExecutor{}
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: core.BuildInput{
			OwnerID: 1,
			Session: core.SessionInfo{SessionID: "sess-1"},
			Agent:   core.AgentInfo{AgentID: 9},
			Runtime: toolruntime.Profile{
				Online:       true,
				LocalActions: []string{"set_model"},
			},
			Binding: core.BindingInfo{Meta: map[string]any{
				"available_models": []any{
					map[string]any{"id": "deepseek-v4-pro", "displayName": "DeepSeek Pro", "provider": "opencode-go"},
				},
			}},
		},
		Request: toolprotocol.ActionRequest{
			SessionID: "sess-1",
			ActionID:  "select_model",
			Event:     "select",
			OptionID:  "opencode-go:deepseek-v4-pro",
		},
		Executor: exec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "accepted_no_state_change" {
		t.Fatalf("outcome=%q want accepted_no_state_change", result.Outcome)
	}
	if result.Code != "accepted" {
		t.Fatalf("code=%q want accepted", result.Code)
	}
	if len(exec.localActions) != 1 {
		t.Fatalf("local actions=%+v want 1", exec.localActions)
	}
	action := exec.localActions[0]
	if action.ActionType != "set_model" {
		t.Fatalf("action_type=%q want=set_model", action.ActionType)
	}
	if action.Params["model_id"] != "deepseek-v4-pro" || action.Params["provider"] != "opencode-go" {
		t.Fatalf("params=%+v", action.Params)
	}
}

func TestBuildInfersConfiguredModelFromSingleOption(t *testing.T) {
	in := core.BuildInput{
		Binding: core.BindingInfo{Meta: map[string]any{
			"available_models": []map[string]any{
				{"id": "qwen3-coder", "display_name": "Qwen3 Coder"},
			},
		}},
	}

	snapshot, err := New().Build(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	item, ok := snapshot.FindItem("select_model")
	if !ok || item.Value != "qwen3-coder" || item.Label != "Qwen3 Coder" || item.BadgeText != "" {
		t.Fatalf("select_model=%+v found=%v", item, ok)
	}
}

func TestBuildPlacesStopBeforeModel(t *testing.T) {
	snapshot, err := New().Build(context.Background(), core.BuildInput{
		Binding: core.BindingInfo{Meta: map[string]any{
			"model_id": "qwen3-coder",
		}},
		Run: toolruntime.RunState{HasActiveRun: true, CanStop: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	stopIndex, modelIndex := -1, -1
	for i, item := range snapshot.Items {
		if item.ActionID == "stop_output" {
			stopIndex = i
		}
		if item.ActionID == "select_model" {
			modelIndex = i
		}
	}
	if stopIndex < 0 || modelIndex < 0 || stopIndex >= modelIndex {
		t.Fatalf("items=%+v want stop_output before select_model", snapshot.Items)
	}
}

func TestBuildHidesConfiguredModelWithoutMetadata(t *testing.T) {
	snapshot, err := New().Build(context.Background(), core.BuildInput{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snapshot.FindItem("select_model"); ok {
		t.Fatal("select_model must be hidden without model metadata")
	}
}

func TestBuildProviderQuotaFromBindingMeta(t *testing.T) {
	in := core.BuildInput{
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"get_session_usage", "get_rate_limits"},
		},
		Binding: core.BindingInfo{Meta: map[string]any{
			"model_id": "deepseek-v4-flash",
			"provider_quota": map[string]any{
				"provider":      "deepseek",
				"providerLabel": "DeepSeek",
				"balance": map[string]any{
					"remaining": 944.55,
					"unit":      "CNY",
				},
			},
		}},
	}
	snap, err := New().Build(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	item, ok := snap.FindItem("provider_quota_balance")
	if !ok {
		t.Fatal("provider_quota_balance item not found")
	}
	if item.LocalAction != "get_rate_limits" {
		t.Fatalf("localAction=%q want=get_rate_limits", item.LocalAction)
	}
	if item.Kind != toolprotocol.ItemKindButton {
		t.Fatalf("kind=%q want=button", item.Kind)
	}
	if item.Label != "¥944.55" {
		t.Fatalf("label=%q want=¥944.55", item.Label)
	}
	if item.BadgeText != "CNY" {
		t.Fatalf("badge=%q want=CNY", item.BadgeText)
	}
	if item.Tooltip != "DeepSeek 剩余余额 ¥944.55，点击刷新" {
		t.Fatalf("tooltip=%q", item.Tooltip)
	}
}

func TestBuildProviderQuotaPlaceholderWhenDeclared(t *testing.T) {
	// 无 provider_quota 数据时不渲染 0% 的 5H 占位。
	snap, err := New().Build(context.Background(), core.BuildInput{
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"get_rate_limits"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snap.FindItem("provider_quota_five_hour"); ok {
		t.Fatal("provider_quota_five_hour placeholder must not appear without quota data")
	}
}

func TestBuildProviderQuotaHiddenWithoutLocalAction(t *testing.T) {
	snap, err := New().Build(context.Background(), core.BuildInput{
		Runtime: toolruntime.Profile{Online: true, LocalActions: []string{"get_session_usage"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snap.FindItem("provider_quota_five_hour"); ok {
		t.Fatal("quota placeholder must not appear when get_rate_limits not declared")
	}
}

type hermesTestExecutor struct {
	localActions []core.LocalActionRequest
}

func (e *hermesTestExecutor) DispatchLocalAction(_ context.Context, req core.LocalActionRequest) error {
	e.localActions = append(e.localActions, req)
	return nil
}
func (e *hermesTestExecutor) StopOutput(_ context.Context, _ core.StopOutputRequest) error {
	return nil
}
func (e *hermesTestExecutor) SendStopText(_ context.Context, _ core.StopOutputRequest) error {
	return nil
}

func TestHandleGetRateLimitsDispatchesLocalAction(t *testing.T) {
	exec := &hermesTestExecutor{}
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: core.BuildInput{
			OwnerID: 1,
			Session: core.SessionInfo{SessionID: "sess-1"},
			Agent:   core.AgentInfo{AgentID: 9},
			Runtime: toolruntime.Profile{
				Online:       true,
				LocalActions: []string{"get_rate_limits"},
			},
		},
		Request:  toolprotocol.ActionRequest{SessionID: "sess-1", ActionID: "provider_quota_balance", Event: "click"},
		Executor: exec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != "accepted" {
		t.Fatalf("get_rate_limits rejected: %s", result.Code)
	}
	if len(exec.localActions) != 1 || exec.localActions[0].ActionType != "get_rate_limits" {
		t.Fatalf("expected get_rate_limits dispatch, got %+v", exec.localActions)
	}
}
