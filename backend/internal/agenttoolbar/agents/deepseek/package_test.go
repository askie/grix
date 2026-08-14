package deepseek

import (
	"context"
	"strings"
	"testing"
	"unicode"

	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	tooli18n "github.com/askie/grix/backend/internal/agenttoolbar/i18n"
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
			LocalActions: []string{"session_control", "set_provider", "set_model", "set_mode", "set_preset", "get_session_usage", "get_rate_limits"},
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
				"agent_preset_id":     "standard",
				"agent_preset_locked": false,
				"available_presets": []any{
					map[string]any{"id": "standard", "displayName": "标准模式"},
					map[string]any{"id": "code", "displayName": "PTC 模式"},
					map[string]any{"id": "minimal", "displayName": "极简模式"},
				},
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
	if !snapshot.Visible || len(snapshot.Items) != 7 {
		t.Fatalf("visible=%v items=%d", snapshot.Visible, len(snapshot.Items))
	}
	wantOrder := []string{"session_control", "provider_quota_balance", "context_usage", "select_preset", "select_mode", "select_provider", "select_model"}
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
	preset, _ := snapshot.FindItem("select_preset")
	if preset.Disabled || preset.Value != "standard" || preset.BadgeText != "标准模式" || len(preset.Options) != 3 {
		t.Fatalf("preset=%+v", preset)
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
	preset, _ := snapshot.FindItem("select_preset")
	if !modelItem.Disabled || len(modelItem.Options) != 0 || !mode.Disabled || !providerItem.Disabled || len(providerItem.Options) != 0 || !preset.Disabled {
		t.Fatalf("model=%+v provider=%+v mode=%+v preset=%+v", modelItem, providerItem, mode, preset)
	}
}

func TestBuildEchoesPersistedSceneWithoutCatalog(t *testing.T) {
	in := baseInput()
	delete(in.Binding.Meta, "available_presets")
	in.Binding.Meta["agent_preset_id"] = "code"
	snapshot, err := New().Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build() error=%v", err)
	}
	preset, ok := snapshot.FindItem("select_preset")
	if !ok || preset.Value != "code" || preset.BadgeText != "PTC 模式" || len(preset.Options) != 4 {
		t.Fatalf("preset=%+v", preset)
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

	presetResult, err := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: in,
		Request:    toolprotocol.ActionRequest{ActionID: "select_preset", OptionID: "code"},
		Executor:   executor,
	})
	if err != nil || presetResult.Outcome != toolprotocol.ActionOutcomeAcceptedNoStateChange || len(executor.local) != 4 {
		t.Fatalf("preset result=%+v actions=%d err=%v", presetResult, len(executor.local), err)
	}
	if executor.local[3].ActionType != "set_preset" || executor.local[3].Params["agent_preset_id"] != "code" {
		t.Fatalf("preset request=%+v", executor.local[3])
	}

	in.Binding.Meta["agent_preset_locked"] = true
	locked, _ := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: in,
		Request:    toolprotocol.ActionRequest{ActionID: "select_preset", OptionID: "minimal"},
		Executor:   executor,
	})
	if locked.Code != "agent_preset_locked" || len(executor.local) != 4 {
		t.Fatalf("locked=%+v actions=%d", locked, len(executor.local))
	}
	in.Binding.Meta["agent_preset_locked"] = false

	in.Run = toolruntime.RunState{HasActiveRun: true}
	busy, _ := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: in,
		Request:    toolprotocol.ActionRequest{ActionID: "select_mode", OptionID: "full_auto"},
		Executor:   executor,
	})
	if busy.Code != "worker_busy" || len(executor.local) != 4 {
		t.Fatalf("busy=%+v actions=%d", busy, len(executor.local))
	}
}

func TestBuildEnglishChromeHasNoHan(t *testing.T) {
	pkg := New()
	cases := []struct {
		name  string
		input func() core.BuildInput
	}{
		{name: "pending", input: baseInput},
		{name: "applied_empty_catalog_running", input: func() core.BuildInput {
			in := baseInput()
			in.Binding.Meta["settings_state"] = "applied"
			in.Binding.Meta["available_models"] = []any{}
			in.Binding.Meta["available_providers"] = []any{}
			in.Run = toolruntime.RunState{HasActiveRun: true, CanStop: true, RunID: "run-1", State: "streaming"}
			return in
		}},
		{name: "failed", input: func() core.BuildInput {
			in := baseInput()
			in.Binding.Meta["settings_state"] = "failed"
			in.Binding.Meta["settings_error_code"] = "apply_failed"
			return in
		}},
		{name: "offline", input: func() core.BuildInput {
			in := baseInput()
			in.Runtime.Online = false
			in.Binding.Cwd = ""
			in.Binding.WorkerStatus = ""
			return in
		}},
		{name: "quota_refresh_unavailable", input: func() core.BuildInput {
			in := baseInput()
			in.Runtime.LocalActions = []string{"session_control", "set_provider", "set_model", "set_mode", "get_session_usage"}
			return in
		}},
		{name: "with_skills", input: func() core.BuildInput {
			in := baseInput()
			in.Runtime.Skills = []toolruntime.SkillEntry{{Name: "demo-skill", Description: "Demo skill"}}
			return in
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, err := pkg.Build(context.Background(), tc.input())
			if err != nil {
				t.Fatalf("Build() error=%v", err)
			}
			for _, item := range snapshot.Items {
				for _, field := range localizedChromeFields(item) {
					got := tooli18n.LocalizeText("en", field)
					if containsHan(got) {
						t.Errorf("item %s still has Han after localize: %q -> %q", item.ItemID, field, got)
					}
				}
			}
		})
	}
}

func TestHandleActionEnglishMessagesHaveNoHan(t *testing.T) {
	pkg := New()
	in := baseInput()
	in.Binding.Meta["settings_state"] = "applied"
	executor := &testExecutor{}
	requests := []toolprotocol.ActionRequest{
		{ActionID: "select_model", OptionID: "deepseek-v4-flash"},
		{ActionID: "select_provider", OptionID: "opencode-go"},
		{ActionID: "select_mode", OptionID: "full_auto"},
		{ActionID: "select_provider", OptionID: "not-in-catalog"},
		{ActionID: "session_control", OptionID: "usage"},
		{ActionID: "session_control", OptionID: "status"},
		{ActionID: "get_rate_limits"},
		{ActionID: "unknown_action"},
	}
	for _, req := range requests {
		result, err := pkg.HandleAction(context.Background(), core.ActionInput{
			BuildInput: in, Request: req, Executor: executor,
		})
		if err != nil {
			t.Fatalf("HandleAction(%+v) error=%v", req, err)
		}
		got := tooli18n.LocalizeText("en", result.Message)
		if containsHan(got) {
			t.Errorf("action %s message still has Han: %q -> %q", req.ActionID, result.Message, got)
		}
	}

	busy := in
	busy.Run = toolruntime.RunState{HasActiveRun: true}
	busyResult, _ := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: busy, Request: toolprotocol.ActionRequest{ActionID: "select_mode", OptionID: "full_auto"}, Executor: executor,
	})
	if got := tooli18n.LocalizeText("en", busyResult.Message); containsHan(got) {
		t.Errorf("busy message still has Han: %q -> %q", busyResult.Message, got)
	}

	stopIn := baseInput()
	stopIn.Run = toolruntime.RunState{HasActiveRun: true, CanStop: true, RunID: "run-1", State: "streaming"}
	stopResult, _ := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: stopIn, Request: toolprotocol.ActionRequest{ActionID: "stop_output"}, Executor: executor,
	})
	if got := tooli18n.LocalizeText("en", stopResult.Message); containsHan(got) {
		t.Errorf("stop message still has Han: %q -> %q", stopResult.Message, got)
	}
	unavailable, _ := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: in, Request: toolprotocol.ActionRequest{ActionID: "stop_output"}, Executor: executor,
	})
	if got := tooli18n.LocalizeText("en", unavailable.Message); containsHan(got) {
		t.Errorf("stop unavailable message still has Han: %q -> %q", unavailable.Message, got)
	}
}

func localizedChromeFields(item toolprotocol.Item) []string {
	fields := []string{
		item.Label, item.Tooltip, item.BadgeText, item.Placeholder,
		item.ConfirmTitle, item.ConfirmText, item.ProgressDesc, item.ProgressDetail,
	}
	for _, opt := range item.Options {
		fields = append(fields, opt.Label)
	}
	for _, cmd := range item.Commands {
		fields = append(fields, cmd.Name, cmd.Description)
	}
	return fields
}

func containsHan(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool {
		return unicode.Is(unicode.Han, r)
	}) >= 0
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
