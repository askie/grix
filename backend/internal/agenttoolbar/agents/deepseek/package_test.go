package deepseek

import (
	"context"
	"strings"
	"testing"
	"time"
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
			LocalActions: []string{"session_control", "set_provider", "set_model", "set_mode", "set_thinking", "set_reasoning_effort", "set_preset", "dsh_list_plugins", "dsh_enable_plugin", "dsh_disable_plugin", "dsh_refresh_plugins", "dsh_list_skills", "dsh_enable_skill", "dsh_disable_skill", "get_session_usage", "get_rate_limits"},
		},
		Binding: core.BindingInfo{
			BindingID: "dsh:sess-deepseek",
			Cwd:       "/workspace/project",
			Meta: map[string]any{
				"provider_id":               "deepseek-official",
				"model_id":                  "deepseek-v4-pro",
				"mode_id":                   "full_auto",
				"thinking_mode":             "enabled",
				"reasoning_effort":          "max",
				"applied_provider_id":       "deepseek-official",
				"applied_model_id":          "deepseek-v4-flash",
				"applied_mode_id":           "approval",
				"applied_thinking_mode":     "disabled",
				"applied_reasoning_effort":  "off",
				"settings_revision":         float64(12),
				"applied_settings_revision": float64(11),
				"settings_state":            "pending",
				"settings_pending_at":       float64(time.Now().UnixMilli()),
				"agent_preset_id":           "standard",
				"agent_preset_locked":       false,
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
				"dsh_plugin_restart_required": true,
				"dsh_plugins": []any{
					map[string]any{"name": "@acme/dsh-notes", "version": "1.0.0", "enabled": false, "locked": false},
					map[string]any{"name": "@grix/dsh-bridge", "version": "3.26.10", "enabled": true, "locked": true, "lock_reason": "Grix Bridge 由连接器安装，不能开关"},
				},
				"dsh_skills": []any{
					map[string]any{"name": "message-send", "description": "Send a message", "enabled": true},
					map[string]any{"name": "grix-admin", "description": "Manage Grix", "enabled": false},
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
	if !snapshot.Visible || len(snapshot.Items) != 11 {
		t.Fatalf("visible=%v items=%d", snapshot.Visible, len(snapshot.Items))
	}
	wantOrder := []string{"session_control", "provider_quota_balance", "context_usage", "select_preset", "select_mode", "select_provider", "select_model", "select_thinking", "select_reasoning_effort", "dsh_plugins", "skills"}
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
	if preset.Disabled || preset.Value != "standard" || preset.Label != "" || preset.BadgeText != "标准模式" || len(preset.Options) != 3 {
		t.Fatalf("preset=%+v", preset)
	}
	mode, _ := snapshot.FindItem("select_mode")
	if !mode.Disabled || !mode.Loading || mode.Value != "full_auto" || mode.Label != "" || mode.BadgeText != "自动" || len(mode.Options) != 2 || mode.Options[1].Label != "自动" {
		t.Fatalf("mode=%+v", mode)
	}
	providerItem, _ := snapshot.FindItem("select_provider")
	if !providerItem.Disabled || !providerItem.Loading || providerItem.Value != "deepseek-official" || providerItem.Label != "" || providerItem.BadgeText != "" || providerItem.Variant != "primary" || len(providerItem.Options) != 2 || providerItem.Options[1].Label != "OpenCode Go" {
		t.Fatalf("provider=%+v", providerItem)
	}
	modelItem, _ := snapshot.FindItem("select_model")
	if !modelItem.Disabled || !modelItem.Loading || modelItem.Value != "deepseek-v4-pro" || modelItem.Label != "" || modelItem.BadgeText != "DeepSeek-V4-Pro" || modelItem.Variant != "primary" || len(modelItem.Options) != 2 {
		t.Fatalf("model=%+v", modelItem)
	}
	plugins, _ := snapshot.FindItem("dsh_plugins")
	if plugins.Kind != toolprotocol.ItemKindToggleList || plugins.LocalAction != "client:toggle_list" || plugins.Value != "restart_required" || plugins.BadgeText != "需重启" || len(plugins.Toggles) != 2 || plugins.Toggles[0].ID != "@acme/dsh-notes" {
		t.Fatalf("plugins=%+v", plugins)
	}
	if !plugins.Toggles[1].Locked || plugins.Toggles[1].LockReason != "Grix Bridge 由连接器安装，不能开关" {
		t.Fatalf("locked plugin=%+v", plugins.Toggles[1])
	}
	skills, _ := snapshot.FindItem("skills")
	if !skills.ShowToggles || skills.ActionID != "dsh_skills" || skills.LocalAction != "client:command_list" || len(skills.Commands) != 2 || len(skills.Toggles) != 2 || !skills.Toggles[0].Enabled || skills.BadgeText != "1/2" {
		t.Fatalf("skills=%+v", skills)
	}
}

func TestSkillTogglesRequireSessionSkillCapability(t *testing.T) {
	in := baseInput()
	in.Runtime.LocalActions = []string{"session_control"}
	in.Runtime.Skills = []toolruntime.SkillEntry{{Name: "runtime-skill", Description: "Runtime fallback"}}

	snapshot, err := New().Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build() error=%v", err)
	}
	skills, ok := snapshot.FindItem("skills")
	if !ok || skills.ShowToggles || len(skills.Toggles) != 0 || len(skills.Commands) != 1 || skills.Commands[0].Name != "runtime-skill" {
		t.Fatalf("skills=%+v", skills)
	}
}

func TestThinkingControlsBuildAndDispatch(t *testing.T) {
	in := baseInput()
	in.Binding.Meta["settings_state"] = "applied"
	snapshot, err := New().Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build() error=%v", err)
	}
	thinking, ok := snapshot.FindItem("select_thinking")
	if !ok || thinking.Disabled || thinking.Value != "enabled" || len(thinking.Options) != 2 {
		t.Fatalf("thinking=%+v", thinking)
	}
	effort, ok := snapshot.FindItem("select_reasoning_effort")
	if !ok || effort.Disabled || effort.Value != "max" || len(effort.Options) != 2 {
		t.Fatalf("effort=%+v", effort)
	}

	for _, tc := range []struct {
		actionID, optionID, actionType, param string
	}{
		{"select_thinking", "disabled", "set_thinking", "thinking_mode"},
		{"select_reasoning_effort", "high", "set_reasoning_effort", "reasoning_effort"},
	} {
		executor := &testExecutor{}
		result, err := New().HandleAction(context.Background(), core.ActionInput{
			BuildInput: in,
			Request:    toolprotocol.ActionRequest{ActionID: tc.actionID, OptionID: tc.optionID},
			Executor:   executor,
		})
		if err != nil || result.Outcome == toolprotocol.ActionOutcomeRejected || len(executor.local) != 1 {
			t.Fatalf("%s result=%+v actions=%+v err=%v", tc.actionID, result, executor.local, err)
		}
		if got := executor.local[0]; got.ActionType != tc.actionType || got.Params[tc.param] != tc.optionID {
			t.Fatalf("%s dispatch=%+v", tc.actionID, got)
		}
	}

	in.Binding.Meta["thinking_mode"] = "disabled"
	snapshot, err = New().Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build(disabled) error=%v", err)
	}
	effort, _ = snapshot.FindItem("select_reasoning_effort")
	if !effort.Disabled {
		t.Fatalf("effort should be disabled while Thinking is off: %+v", effort)
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
	mode, _ := snapshot.FindItem("select_mode")
	preset, _ := snapshot.FindItem("select_preset")
	if _, ok := snapshot.FindItem("select_provider"); ok {
		t.Fatalf("provider selector should be hidden without catalog: %+v", snapshot.Items)
	}
	if !modelItem.Disabled || len(modelItem.Options) != 0 || !mode.Disabled || !preset.Disabled {
		t.Fatalf("model=%+v mode=%+v preset=%+v", modelItem, mode, preset)
	}
}

func TestBuildHidesProviderWithoutCatalog(t *testing.T) {
	in := baseInput()
	in.Binding.Meta["available_providers"] = []any{}

	snapshot, err := New().Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build() error=%v", err)
	}
	if _, ok := snapshot.FindItem("select_provider"); ok {
		t.Fatalf("provider selector should be hidden without catalog: %+v", snapshot.Items)
	}
	if _, ok := snapshot.FindItem("select_model"); !ok {
		t.Fatalf("model selector should remain visible: %+v", snapshot.Items)
	}
}

// 过期或无时间戳的存量 pending 不再卡住选择器：停止 loading、解除禁用，且不再拒绝新设置。
func TestBuildStalePendingRecovers(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(meta map[string]any)
	}{
		{name: "expired_timestamp", mutate: func(meta map[string]any) {
			meta["settings_pending_at"] = float64(time.Now().Add(-10 * time.Minute).UnixMilli())
		}},
		{name: "missing_timestamp", mutate: func(meta map[string]any) {
			delete(meta, "settings_pending_at")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := baseInput()
			tc.mutate(in.Binding.Meta)
			snapshot, err := New().Build(context.Background(), in)
			if err != nil {
				t.Fatalf("Build() error=%v", err)
			}
			for _, id := range []string{"select_mode", "select_provider", "select_model", "select_thinking", "select_reasoning_effort"} {
				item, ok := snapshot.FindItem(id)
				if !ok || item.Disabled || item.Loading {
					t.Fatalf("%s should recover from stale pending: %+v", id, item)
				}
			}
			executor := &testExecutor{}
			result, _ := New().HandleAction(context.Background(), core.ActionInput{
				BuildInput: in,
				Request:    toolprotocol.ActionRequest{ActionID: "select_model", OptionID: "deepseek-v4-flash"},
				Executor:   executor,
			})
			if result.Code == "settings_pending" || len(executor.local) != 1 {
				t.Fatalf("stale pending should not block new settings: %+v actions=%d", result, len(executor.local))
			}
		})
	}
}

func TestBuildFailedStateUsesWarningVariant(t *testing.T) {
	in := baseInput()
	in.Binding.Meta["settings_state"] = "failed"
	in.Binding.Meta["settings_error_code"] = "apply_failed"
	snapshot, err := New().Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build() error=%v", err)
	}
	providerItem, _ := snapshot.FindItem("select_provider")
	if providerItem.Variant != "warning" || providerItem.BadgeText != "" || providerItem.Label != "" || providerItem.Value != "deepseek-official" {
		t.Fatalf("failed provider=%+v", providerItem)
	}
	modelItem, _ := snapshot.FindItem("select_model")
	if modelItem.Variant != "warning" || modelItem.BadgeText != "DeepSeek-V4-Pro" || modelItem.Label != "" || modelItem.Value != "deepseek-v4-pro" {
		t.Fatalf("failed model=%+v", modelItem)
	}
	mode, _ := snapshot.FindItem("select_mode")
	if mode.Label != "" || mode.BadgeText != "自动" || mode.Value != "full_auto" || mode.Variant != "warning" {
		t.Fatalf("failed mode=%+v", mode)
	}
}

func TestBuildModelItemFollowsCurrentProviderCatalog(t *testing.T) {
	in := baseInput()
	in.Binding.Meta["settings_state"] = "applied"
	in.Binding.Meta["provider_id"] = "opencode-go"
	in.Binding.Meta["model_id"] = "deepseek-v4-pro"
	in.Binding.Meta["available_models"] = []any{
		map[string]any{"id": "deepseek-v4-pro", "displayName": "DeepSeek-V4-Pro", "provider_id": "deepseek-official"},
		map[string]any{"id": "go-model", "displayName": "Go Model", "provider_id": "opencode-go"},
	}
	snapshot, err := New().Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build() error=%v", err)
	}
	modelItem, _ := snapshot.FindItem("select_model")
	if modelItem.Value != "" || modelItem.BadgeText != "" || len(modelItem.Options) != 1 || modelItem.Options[0].OptionID != "go-model" {
		t.Fatalf("model=%+v", modelItem)
	}

	rejected, _ := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: in,
		Request:    toolprotocol.ActionRequest{ActionID: "select_model", OptionID: "deepseek-v4-pro"},
		Executor:   &testExecutor{},
	})
	if rejected.Code != "invalid_option" {
		t.Fatalf("cross-provider model=%+v", rejected)
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

func TestBuildHidesLockedPreset(t *testing.T) {
	in := baseInput()
	in.Binding.Meta["agent_preset_locked"] = true
	snapshot, err := New().Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build() error=%v", err)
	}
	if _, ok := snapshot.FindItem("select_preset"); ok {
		t.Fatalf("locked preset should be omitted: %+v", snapshot.Items)
	}
	if _, ok := snapshot.FindItem("select_mode"); !ok {
		t.Fatalf("mode should stay visible after preset lock")
	}
	if _, ok := snapshot.FindItem("select_provider"); !ok {
		t.Fatalf("provider should stay visible after preset lock")
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

	enablePlugin, _ := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: in,
		Request:    toolprotocol.ActionRequest{ActionID: "dsh_plugins", Event: "enable", OptionID: "@acme/dsh-notes"},
		Executor:   executor,
	})
	if enablePlugin.Outcome != toolprotocol.ActionOutcomeAcceptedNoStateChange || len(executor.local) != 5 || executor.local[4].ActionType != "dsh_enable_plugin" {
		t.Fatalf("enable plugin=%+v actions=%+v", enablePlugin, executor.local)
	}

	disableSkill, _ := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: in,
		Request:    toolprotocol.ActionRequest{ActionID: "dsh_skills", Event: "disable", OptionID: "message-send"},
		Executor:   executor,
	})
	if disableSkill.Outcome != toolprotocol.ActionOutcomeAcceptedNoStateChange || len(executor.local) != 6 || executor.local[5].ActionType != "dsh_disable_skill" {
		t.Fatalf("disable skill=%+v actions=%+v", disableSkill, executor.local)
	}

	in.Run = toolruntime.RunState{HasActiveRun: true}
	busy, _ := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: in,
		Request:    toolprotocol.ActionRequest{ActionID: "select_mode", OptionID: "full_auto"},
		Executor:   executor,
	})
	if busy.Code != "worker_busy" || len(executor.local) != 6 {
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
		{name: "preset_locked", input: func() core.BuildInput {
			in := baseInput()
			in.Binding.Meta["agent_preset_locked"] = true
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
		{ActionID: "select_preset", OptionID: "code"},
		{ActionID: "select_preset", OptionID: "not-in-catalog"},
		{ActionID: "select_provider", OptionID: "not-in-catalog"},
		{ActionID: "session_control", OptionID: "usage"},
		{ActionID: "session_control", OptionID: "status"},
		{ActionID: "get_rate_limits"},
		{ActionID: "unknown_action"},
		{ActionID: "dsh_plugins", Event: "enable", OptionID: "@acme/dsh-notes"},
		{ActionID: "dsh_plugins", Event: "disable", OptionID: "@acme/dsh-notes"},
		{ActionID: "dsh_plugins", Event: "refresh"},
		{ActionID: "dsh_skills", Event: "enable", OptionID: "grix-admin"},
		{ActionID: "dsh_skills", Event: "disable", OptionID: "message-send"},
		{ActionID: "dsh_skills", Event: "refresh"},
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
	busyPreset, _ := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: busy, Request: toolprotocol.ActionRequest{ActionID: "select_preset", OptionID: "code"}, Executor: executor,
	})
	if got := tooli18n.LocalizeText("en", busyPreset.Message); containsHan(got) {
		t.Errorf("busy preset message still has Han: %q -> %q", busyPreset.Message, got)
	}

	locked := in
	locked.Binding.Meta = map[string]any{}
	for key, value := range in.Binding.Meta {
		locked.Binding.Meta[key] = value
	}
	locked.Binding.Meta["agent_preset_locked"] = true
	lockedResult, _ := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: locked, Request: toolprotocol.ActionRequest{ActionID: "select_preset", OptionID: "minimal"}, Executor: executor,
	})
	if got := tooli18n.LocalizeText("en", lockedResult.Message); containsHan(got) {
		t.Errorf("locked preset message still has Han: %q -> %q", lockedResult.Message, got)
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
	for _, toggle := range item.Toggles {
		fields = append(fields, toggle.LockReason)
	}
	return fields
}

func containsHan(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool {
		return unicode.Is(unicode.Han, r)
	}) >= 0
}

func withProfileMeta(in core.BuildInput) core.BuildInput {
	in.Binding.Meta["dsh_profile"] = "web"
	in.Binding.Meta["dsh_profile_locked"] = false
	in.Binding.Meta["dsh_profile_create"] = true
	in.Binding.Meta["available_profiles"] = []any{
		map[string]any{"id": "web", "displayName": "web（插件托管）"},
		map[string]any{"id": "team", "displayName": "team"},
	}
	in.Runtime.LocalActions = append(in.Runtime.LocalActions, "set_profile", "create_profile")
	return in
}

func TestBuildProfileItemWebFallback(t *testing.T) {
	pkg := New()

	// dsh_profile 缺失且目录里有 web：兜底选中 web（与 connector 默认一致）。
	withWeb := baseInput()
	withWeb.Binding.Meta["dsh_profile_create"] = true
	withWeb.Binding.Meta["available_profiles"] = []any{
		map[string]any{"id": "web", "displayName": "web（插件托管）"},
		map[string]any{"id": "team", "displayName": "team"},
	}
	withWeb.Runtime.LocalActions = append(withWeb.Runtime.LocalActions, "set_profile", "create_profile")
	snapshotWeb, err := pkg.Build(context.Background(), withWeb)
	if err != nil {
		t.Fatalf("Build() error=%v", err)
	}
	itemWeb, ok := snapshotWeb.FindItem("dsh_profile")
	if !ok || itemWeb.Value != "web" {
		t.Fatalf("web fallback item=%+v ok=%v", itemWeb, ok)
	}

	// dsh_profile 缺失且目录里没有 web：不虚构选中值，留空待用户选择。
	noWeb := baseInput()
	noWeb.Binding.Meta["available_profiles"] = []any{
		map[string]any{"id": "team", "displayName": "team"},
	}
	noWeb.Runtime.LocalActions = append(noWeb.Runtime.LocalActions, "set_profile")
	snapshotNoWeb, err := pkg.Build(context.Background(), noWeb)
	if err != nil {
		t.Fatalf("Build() error=%v", err)
	}
	itemNoWeb, ok := snapshotNoWeb.FindItem("dsh_profile")
	if !ok || itemNoWeb.Value != "" || itemNoWeb.BadgeText != "" {
		t.Fatalf("no-web item=%+v ok=%v", itemNoWeb, ok)
	}
}

func TestNormalizeDshProfileNameRuneCount(t *testing.T) {
	// 长度上限按 rune 计数，与前端 maxLength（Dart 字符）对齐：
	// 30 个汉字（90 字节）放行，65 个汉字拒绝。
	if name, ok := normalizeDshProfileName(strings.Repeat("名", 30)); !ok || name != strings.Repeat("名", 30) {
		t.Fatalf("30 han chars should pass, got %q ok=%v", name, ok)
	}
	if _, ok := normalizeDshProfileName(strings.Repeat("名", 65)); ok {
		t.Fatal("65 han chars should be rejected")
	}
	if _, ok := normalizeDshProfileName(strings.Repeat("x", 64)); !ok {
		t.Fatal("64 ascii chars should pass")
	}
	if _, ok := normalizeDshProfileName(strings.Repeat("x", 65)); ok {
		t.Fatal("65 ascii chars should be rejected")
	}
}

func TestBuildProfileItem(t *testing.T) {
	pkg := New()

	// 无 profile meta（旧 connector）：不出项，避免噪音。
	plain, err := pkg.Build(context.Background(), baseInput())
	if err != nil {
		t.Fatalf("Build() error=%v", err)
	}
	if _, ok := plain.FindItem("dsh_profile"); ok {
		t.Fatalf("profile item should be hidden without meta: %+v", plain.Items)
	}

	in := withProfileMeta(baseInput())
	snapshot, err := pkg.Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build() error=%v", err)
	}
	item, ok := snapshot.FindItem("dsh_profile")
	if !ok {
		t.Fatalf("profile item missing: %+v", snapshot.Items)
	}
	if item.Kind != toolprotocol.ItemKindSelect || item.ActionID != "select_profile" || item.Icon != "profile" {
		t.Fatalf("profile item=%+v", item)
	}
	if item.Disabled || item.Value != "web" || item.BadgeText != "" {
		t.Fatalf("profile item=%+v", item)
	}
	if len(item.Options) != 3 || item.Options[2].OptionID != createProfileOptionID {
		t.Fatalf("profile options=%+v", item.Options)
	}
	// 顺序：profile 在 preset 之前。
	var profileIdx, presetIdx = -1, -1
	for i, it := range snapshot.Items {
		if it.ItemID == "dsh_profile" {
			profileIdx = i
		}
		if it.ItemID == "select_preset" {
			presetIdx = i
		}
	}
	if profileIdx < 0 || presetIdx < 0 || profileIdx > presetIdx {
		t.Fatalf("profile=%d preset=%d", profileIdx, presetIdx)
	}

	// 不允许新建：没有伪选项。
	noCreate := withProfileMeta(baseInput())
	noCreate.Binding.Meta["dsh_profile_create"] = false
	snapshotNoCreate, err := pkg.Build(context.Background(), noCreate)
	if err != nil {
		t.Fatalf("Build() error=%v", err)
	}
	itemNoCreate, _ := snapshotNoCreate.FindItem("dsh_profile")
	if len(itemNoCreate.Options) != 2 {
		t.Fatalf("no-create options=%+v", itemNoCreate.Options)
	}

	// 目录里真存在同名 __create__ profile（CLI 可建）：不追加伪选项，避免撞名。
	collision := withProfileMeta(baseInput())
	collision.Binding.Meta["available_profiles"] = []any{
		map[string]any{"id": "web", "displayName": "web（插件托管）"},
		map[string]any{"id": createProfileOptionID, "displayName": createProfileOptionID},
	}
	snapshotCollision, err := pkg.Build(context.Background(), collision)
	if err != nil {
		t.Fatalf("Build() error=%v", err)
	}
	itemCollision, _ := snapshotCollision.FindItem("dsh_profile")
	if len(itemCollision.Options) != 2 {
		t.Fatalf("collision options=%+v", itemCollision.Options)
	}

	// 锁定：与 preset 一致，整项收起。
	locked := withProfileMeta(baseInput())
	locked.Binding.Meta["dsh_profile_locked"] = true
	snapshotLocked, err := pkg.Build(context.Background(), locked)
	if err != nil {
		t.Fatalf("Build() error=%v", err)
	}
	if _, ok := snapshotLocked.FindItem("dsh_profile"); ok {
		t.Fatalf("locked profile item should be omitted: %+v", snapshotLocked.Items)
	}
}

func TestHandleProfileActions(t *testing.T) {
	pkg := New()
	in := withProfileMeta(baseInput())
	executor := &testExecutor{}

	result, err := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: in,
		Request:    toolprotocol.ActionRequest{ActionID: "select_profile", OptionID: "team"},
		Executor:   executor,
	})
	if err != nil || result.Outcome != toolprotocol.ActionOutcomeAcceptedNoStateChange || len(executor.local) != 1 {
		t.Fatalf("select result=%+v actions=%d err=%v", result, len(executor.local), err)
	}
	request := executor.local[0]
	if request.ActionType != "set_profile" || request.TimeoutMs != 30_000 || request.Params["profile_id"] != "team" || request.Params["display_label"] != "team" {
		t.Fatalf("select request=%+v", request)
	}
	if got := tooli18n.LocalizeText("en", result.Message); containsHan(got) {
		t.Errorf("select message still has Han: %q -> %q", result.Message, got)
	}

	invalid, _ := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: in,
		Request:    toolprotocol.ActionRequest{ActionID: "select_profile", OptionID: "ghost"},
		Executor:   executor,
	})
	if invalid.Outcome != toolprotocol.ActionOutcomeAcceptedNoStateChange || len(executor.local) != 2 {
		t.Fatalf("legacy create=%+v actions=%d", invalid, len(executor.local))
	}
	legacyCreate := executor.local[1]
	if legacyCreate.ActionType != "create_profile" || legacyCreate.Params["profile_id"] != "ghost" {
		t.Fatalf("legacy create request=%+v", legacyCreate)
	}

	pseudoCreate, _ := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: in,
		Request:    toolprotocol.ActionRequest{ActionID: "select_profile", OptionID: createProfileOptionID},
		Executor:   executor,
	})
	if pseudoCreate.Code != "profile_invalid" || len(executor.local) != 2 {
		t.Fatalf("pseudo create=%+v actions=%d", pseudoCreate, len(executor.local))
	}
	if got := tooli18n.LocalizeText("en", pseudoCreate.Message); containsHan(got) {
		t.Errorf("pseudo-create message still has Han: %q -> %q", pseudoCreate.Message, got)
	}

	noCreateSelect := withProfileMeta(baseInput())
	noCreateSelect.Binding.Meta["dsh_profile_create"] = false
	noCreateResult, _ := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: noCreateSelect,
		Request:    toolprotocol.ActionRequest{ActionID: "select_profile", OptionID: "ghost"},
		Executor:   executor,
	})
	if noCreateResult.Code != "invalid_option" || len(executor.local) != 2 {
		t.Fatalf("no-create invalid=%+v actions=%d", noCreateResult, len(executor.local))
	}
	if got := tooli18n.LocalizeText("en", noCreateResult.Message); containsHan(got) {
		t.Errorf("invalid message still has Han: %q -> %q", noCreateResult.Message, got)
	}

	created, err := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: in,
		Request:    toolprotocol.ActionRequest{ActionID: "create_profile", OptionID: "new-profile"},
		Executor:   executor,
	})
	if err != nil || created.Outcome != toolprotocol.ActionOutcomeAcceptedNoStateChange || len(executor.local) != 3 {
		t.Fatalf("create result=%+v actions=%d err=%v", created, len(executor.local), err)
	}
	createRequest := executor.local[2]
	if createRequest.ActionType != "create_profile" || createRequest.TimeoutMs != 120_000 || createRequest.Params["profile_id"] != "new-profile" {
		t.Fatalf("create request=%+v", createRequest)
	}
	if got := tooli18n.LocalizeText("en", created.Message); containsHan(got) {
		t.Errorf("create message still has Han: %q -> %q", created.Message, got)
	}

	for _, bad := range []string{"", "headless", "a/b", `a\b`, "..", ".", strings.Repeat("x", 65)} {
		rejectedResult, _ := pkg.HandleAction(context.Background(), core.ActionInput{
			BuildInput: in,
			Request:    toolprotocol.ActionRequest{ActionID: "create_profile", OptionID: bad},
			Executor:   executor,
		})
		if rejectedResult.Code != "profile_invalid" || len(executor.local) != 3 {
			t.Fatalf("bad name %q should be rejected: %+v", bad, rejectedResult)
		}
		if got := tooli18n.LocalizeText("en", rejectedResult.Message); containsHan(got) {
			t.Errorf("invalid-name message still has Han: %q -> %q", rejectedResult.Message, got)
		}
	}

	exists, _ := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: in,
		Request:    toolprotocol.ActionRequest{ActionID: "create_profile", OptionID: "web"},
		Executor:   executor,
	})
	if exists.Code != "profile_exists" || len(executor.local) != 3 {
		t.Fatalf("exists=%+v actions=%d", exists, len(executor.local))
	}

	lockedIn := withProfileMeta(baseInput())
	lockedIn.Binding.Meta["dsh_profile_locked"] = true
	for _, actionID := range []string{"select_profile", "create_profile"} {
		lockedResult, _ := pkg.HandleAction(context.Background(), core.ActionInput{
			BuildInput: lockedIn,
			Request:    toolprotocol.ActionRequest{ActionID: actionID, OptionID: "team"},
			Executor:   executor,
		})
		if lockedResult.Code != "profile_locked" || len(executor.local) != 3 {
			t.Fatalf("%s locked=%+v actions=%d", actionID, lockedResult, len(executor.local))
		}
	}

	noCreateIn := withProfileMeta(baseInput())
	noCreateIn.Binding.Meta["dsh_profile_create"] = false
	noCreate, _ := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: noCreateIn,
		Request:    toolprotocol.ActionRequest{ActionID: "create_profile", OptionID: "fresh"},
		Executor:   executor,
	})
	if noCreate.Code != "profile_create_unavailable" || len(executor.local) != 3 {
		t.Fatalf("noCreate=%+v actions=%d", noCreate, len(executor.local))
	}

	busyIn := withProfileMeta(baseInput())
	busyIn.Run = toolruntime.RunState{HasActiveRun: true}
	busy, _ := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: busyIn,
		Request:    toolprotocol.ActionRequest{ActionID: "select_profile", OptionID: "team"},
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
