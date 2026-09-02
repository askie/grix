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

// providerBuildInput 新连接器形状：available_providers + provider_id + 跨供应商同名模型。
func providerBuildInput() core.BuildInput {
	in := buildInput(true, []string{"session_control", "set_model", "set_provider"}, true)
	in.Binding.Meta = map[string]any{
		"provider_id": "grix-openai",
		"model_id":    "gpt-5",
		"available_providers": []any{
			map[string]any{"id": "openai", "display_name": "OpenAI"},
			map[string]any{"id": "grix-openai", "display_name": "Grix OpenAI"},
		},
		"available_models": []any{
			map[string]any{"id": "gpt-5", "display_name": "GPT-5", "provider": "openai"},
			map[string]any{"id": "gpt-5", "display_name": "GPT-5 (Grix)", "provider": "grix-openai"},
			map[string]any{"id": "o4-mini", "display_name": "o4 mini", "provider": "openai"},
		},
	}
	return in
}

func TestPiBuildProviderAndFilteredModelSelect(t *testing.T) {
	snap, err := New().Build(context.Background(), providerBuildInput())
	if err != nil {
		t.Fatal(err)
	}
	provider := findItem(snap, "select_provider")
	if provider == nil {
		t.Fatal("select_provider item not found")
	}
	if provider.Value != "grix-openai" || provider.Label != "Grix OpenAI" {
		t.Fatalf("provider value=%q label=%q want=grix-openai/Grix OpenAI", provider.Value, provider.Label)
	}
	if provider.Disabled {
		t.Fatalf("provider select disabled: %s", provider.Tooltip)
	}
	if len(provider.Options) != 2 || provider.Options[0].OptionID != "openai" || provider.Options[1].OptionID != "grix-openai" {
		t.Fatalf("provider options=%+v", provider.Options)
	}
	model := findItem(snap, "select_model")
	if model == nil {
		t.Fatal("select_model item not found")
	}
	if len(model.Options) != 1 || model.Options[0].OptionID != "grix-openai:gpt-5" {
		t.Fatalf("model options=%+v want only grix-openai:gpt-5", model.Options)
	}
	if model.Label != "GPT-5 (Grix)" {
		t.Fatalf("model label=%q want=GPT-5 (Grix)", model.Label)
	}
	// 供应商下拉排在模型下拉前面。
	providerIdx, modelIdx := -1, -1
	for i := range snap.Items {
		switch snap.Items[i].ItemID {
		case "select_provider":
			providerIdx = i
		case "select_model":
			modelIdx = i
		}
	}
	if providerIdx > modelIdx {
		t.Fatalf("provider index=%d must precede model index=%d", providerIdx, modelIdx)
	}
}

func TestPiBuildCrossProviderSameModelKeepsDistinctOptions(t *testing.T) {
	// 未上报当前供应商时不过滤，同名模型靠 provider 前缀区分。
	in := providerBuildInput()
	delete(in.Binding.Meta, "provider_id")
	snap, err := New().Build(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	model := findItem(snap, "select_model")
	if model == nil {
		t.Fatal("select_model item not found")
	}
	ids := make([]string, 0, len(model.Options))
	for _, option := range model.Options {
		ids = append(ids, option.OptionID)
	}
	want := []string{"openai:gpt-5", "grix-openai:gpt-5", "openai:o4-mini"}
	if len(ids) != len(want) {
		t.Fatalf("model option ids=%v want=%v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("model option ids=%v want=%v", ids, want)
		}
	}
}

func TestPiBuildWithoutProvidersKeepsFlatModels(t *testing.T) {
	// 老连接器：meta 没有 available_providers，供应商下拉不出现，option id 保持裸 model id。
	snap, err := New().Build(context.Background(), buildInput(true, []string{"session_control", "set_model"}, true))
	if err != nil {
		t.Fatal(err)
	}
	if findItem(snap, "select_provider") != nil {
		t.Fatal("select_provider must not appear without available_providers")
	}
	model := findItem(snap, "select_model")
	if model == nil {
		t.Fatal("select_model item not found")
	}
	if len(model.Options) != 1 || model.Options[0].OptionID != "k3" {
		t.Fatalf("model options=%+v want bare k3", model.Options)
	}
}

func TestPiHandleSelectProviderDispatches(t *testing.T) {
	exec := &testExecutor{}
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: providerBuildInput(),
		Request:    toolprotocol.ActionRequest{SessionID: "sess-1", ActionID: "select_provider", OptionID: "openai", Event: "select"},
		Executor:   exec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome == toolprotocol.ActionOutcomeRejected {
		t.Fatalf("select_provider rejected: %s %s", result.Code, result.Message)
	}
	if len(exec.localActions) != 1 || exec.localActions[0].ActionType != "set_provider" {
		t.Fatalf("expected set_provider dispatch, got %+v", exec.localActions)
	}
	params := exec.localActions[0].Params
	if params["session_id"] != "sess-1" || params["provider_id"] != "openai" || params["display_label"] != "OpenAI" {
		t.Fatalf("set_provider params=%+v", params)
	}
	if exec.localActions[0].TimeoutMs != 15_000 {
		t.Fatalf("set_provider timeout=%d want=15000", exec.localActions[0].TimeoutMs)
	}
}

func TestPiHandleSelectProviderRejectsUnknownOption(t *testing.T) {
	exec := &testExecutor{}
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: providerBuildInput(),
		Request:    toolprotocol.ActionRequest{SessionID: "sess-1", ActionID: "select_provider", OptionID: "ghost", Event: "select"},
		Executor:   exec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != "invalid_option" {
		t.Fatalf("code=%q want=invalid_option", result.Code)
	}
	if len(exec.localActions) != 0 {
		t.Fatalf("unexpected dispatch: %+v", exec.localActions)
	}
}

func TestPiHandleSelectProviderRejectsDuringActiveRun(t *testing.T) {
	in := providerBuildInput()
	in.Run = toolruntime.RunState{HasActiveRun: true, CanStop: true, State: "running"}
	exec := &testExecutor{}
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: in,
		Request:    toolprotocol.ActionRequest{SessionID: "sess-1", ActionID: "select_provider", OptionID: "openai", Event: "select"},
		Executor:   exec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != "run_active" {
		t.Fatalf("code=%q want=run_active", result.Code)
	}
	if len(exec.localActions) != 0 {
		t.Fatalf("unexpected dispatch: %+v", exec.localActions)
	}
	snap, err := New().Build(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	provider := findItem(snap, "select_provider")
	if provider == nil || !provider.Disabled {
		t.Fatalf("provider select must be disabled during active run: %+v", provider)
	}
}

func TestPiHandleSelectModelDispatchesProvider(t *testing.T) {
	exec := &testExecutor{}
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: providerBuildInput(),
		Request:    toolprotocol.ActionRequest{SessionID: "sess-1", ActionID: "select_model", OptionID: "openai:gpt-5", Event: "select"},
		Executor:   exec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome == toolprotocol.ActionOutcomeRejected {
		t.Fatalf("select_model rejected: %s %s", result.Code, result.Message)
	}
	if len(exec.localActions) != 1 || exec.localActions[0].ActionType != "set_model" {
		t.Fatalf("expected set_model dispatch, got %+v", exec.localActions)
	}
	params := exec.localActions[0].Params
	if params["session_id"] != "sess-1" || params["model_id"] != "gpt-5" ||
		params["provider"] != "openai" || params["display_label"] != "GPT-5" {
		t.Fatalf("set_model params=%+v", params)
	}
}

func TestPiHandleSelectModelWithoutProviderStaysFlat(t *testing.T) {
	// 老连接器：option id 是裸 model id，下发参数里不带 provider。
	exec := &testExecutor{}
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: buildInput(true, []string{"session_control", "set_model"}, true),
		Request:    toolprotocol.ActionRequest{SessionID: "sess-1", ActionID: "select_model", OptionID: "k3", Event: "select"},
		Executor:   exec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome == toolprotocol.ActionOutcomeRejected {
		t.Fatalf("select_model rejected: %s %s", result.Code, result.Message)
	}
	params := exec.localActions[0].Params
	if params["model_id"] != "k3" || params["display_label"] != "Kimi K3" {
		t.Fatalf("set_model params=%+v", params)
	}
	if _, ok := params["provider"]; ok {
		t.Fatalf("provider must be absent for legacy connector: %+v", params)
	}
}
