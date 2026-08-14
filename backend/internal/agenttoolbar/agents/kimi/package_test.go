package kimi

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	"github.com/askie/grix/backend/internal/model"
)

// ── test helpers ──────────────────────────────────────────────────────────────

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
			ClientType: model.AgentClientTypeKimi,
		},
		Runtime: toolruntime.Profile{
			Online:       online,
			LocalActions: localActions,
		},
		Binding: core.BindingInfo{
			Cwd: cwd,
			Meta: map[string]any{
				"model_id": "kimi-k2",
				"mode_id":  "default",
				"available_models": []any{
					map[string]any{"id": "kimi-k2", "displayName": "Kimi K2"},
				},
				// 模拟 Kimi CLI 0.26.0 真实上报的四档模式，工具栏只应暴露三档。
				"available_modes": []any{
					map[string]any{"id": "default", "displayName": "Default"},
					map[string]any{"id": "plan", "displayName": "Plan"},
					map[string]any{"id": "auto", "displayName": "Auto"},
					map[string]any{"id": "yolo", "displayName": "YOLO"},
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

// ── Build() ───────────────────────────────────────────────────────────────────

func TestKimiBuildNoBinding(t *testing.T) {
	snap, err := New().Build(context.Background(), buildInput(true, []string{"session_control", "set_mode", "get_session_usage"}, false))
	if err != nil {
		t.Fatal(err)
	}
	if snap.Visible {
		t.Fatal("snapshot should be hidden without binding")
	}
}

func TestKimiBuildWithBindingSessionControlItem(t *testing.T) {
	snap, err := New().Build(context.Background(), buildInput(true, []string{"session_control", "set_mode", "get_session_usage"}, true))
	if err != nil {
		t.Fatal(err)
	}
	found := findItem(snap, "session_control")
	if found == nil {
		t.Fatal("session_control item not found in snapshot")
	}
	if found.Disabled {
		t.Fatal("session_control should be enabled when online with localAction")
	}
	optIDs := make(map[string]bool)
	for _, o := range found.Options {
		optIDs[o.OptionID] = true
	}
	for _, want := range []string{"status", "restart"} {
		if !optIDs[want] {
			t.Fatalf("option %q missing from session_control", want)
		}
	}
	// 连接器暂无 kimi 会话用量解析，usage 入口不得出现（点了必失败）。
	if optIDs["usage"] {
		t.Fatal("usage option must not be exposed for kimi (connector cannot parse kimi session usage)")
	}
}

// 模型可会话级切换（kimi 0.26.0 起 session/set_model 是会话级操作）。
func TestKimiBuildModelSelectable(t *testing.T) {
	snap, err := New().Build(context.Background(), buildInput(true, []string{"session_control", "set_model", "set_mode", "get_session_usage"}, true))
	if err != nil {
		t.Fatal(err)
	}
	if item := findItem(snap, "current_model"); item != nil {
		t.Fatal("read-only current_model item must be replaced by select_model")
	}
	item := findItem(snap, "select_model")
	if item == nil {
		t.Fatal("select_model item not found")
	}
	if item.Disabled {
		t.Fatal("select_model should be enabled when online with set_model and options")
	}
	if item.Value != "kimi-k2" || item.BadgeText != "Kimi K2" {
		t.Fatalf("model value=%q badge=%q want kimi-k2/Kimi K2", item.Value, item.BadgeText)
	}
	if len(item.Options) != 1 {
		t.Fatalf("model options len=%d want=1", len(item.Options))
	}
}

func TestKimiBuildModelDisabledWithoutSetModelDeclaration(t *testing.T) {
	snap, err := New().Build(context.Background(), buildInput(true, []string{"session_control", "set_mode"}, true))
	if err != nil {
		t.Fatal(err)
	}
	item := findItem(snap, "select_model")
	if item == nil {
		t.Fatal("select_model item not found")
	}
	if !item.Disabled {
		t.Fatal("select_model should be disabled when set_model not declared")
	}
}

func TestKimiBuildModeOptionsFromBindingMeta(t *testing.T) {
	snap, err := New().Build(context.Background(), buildInput(true, []string{"session_control", "set_mode"}, true))
	if err != nil {
		t.Fatal(err)
	}
	item := findItem(snap, "select_mode")
	if item == nil {
		t.Fatal("select_mode item not found")
	}
	if item.Disabled {
		t.Fatal("select_mode should be enabled when online with set_mode")
	}
	// CLI 上报四档（default/plan/auto/yolo），工具栏收口为三档且 label 中文。
	if len(item.Options) != 3 {
		t.Fatalf("mode options len=%d want=3", len(item.Options))
	}
	wantOptions := []struct{ id, label string }{
		{"default", "默认"},
		{"plan", "计划"},
		{"yolo", "自动"},
	}
	for i, want := range wantOptions {
		if item.Options[i].OptionID != want.id || item.Options[i].Label != want.label {
			t.Fatalf("mode option[%d]=%q/%q want %q/%q", i, item.Options[i].OptionID, item.Options[i].Label, want.id, want.label)
		}
	}
	if item.Value != "default" || item.BadgeText != "默认" {
		t.Fatalf("mode value=%q badge=%q want default/默认", item.Value, item.BadgeText)
	}
}

func TestKimiBuildModeBadgeForFilteredCurrentMode(t *testing.T) {
	// 用户在 CLI 侧自行切到被过滤的 auto 档：徽标要给中文展示名，不落回裸 id。
	in := buildInput(true, []string{"session_control", "set_mode"}, true)
	in.Binding.Meta["mode_id"] = "auto"
	snap, err := New().Build(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	item := findItem(snap, "select_mode")
	if item == nil {
		t.Fatal("select_mode item not found")
	}
	if item.BadgeText != "自动（安全）" {
		t.Fatalf("mode badge=%q want 自动（安全）", item.BadgeText)
	}
	if len(item.Options) != 3 {
		t.Fatalf("mode options len=%d want=3 (auto must stay filtered)", len(item.Options))
	}
}

func TestKimiBuildModeOptionsPartialReport(t *testing.T) {
	// CLI 只上报部分模式时取交集，只暴露上报了的档位。
	in := buildInput(true, []string{"session_control", "set_mode"}, true)
	in.Binding.Meta["available_modes"] = []any{
		map[string]any{"id": "default", "displayName": "Default"},
		map[string]any{"id": "plan", "displayName": "Plan"},
	}
	snap, err := New().Build(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	item := findItem(snap, "select_mode")
	if item == nil {
		t.Fatal("select_mode item not found")
	}
	if len(item.Options) != 2 {
		t.Fatalf("mode options len=%d want=2", len(item.Options))
	}
	if item.Options[0].OptionID != "default" || item.Options[1].OptionID != "plan" {
		t.Fatalf("mode options=%q/%q want default/plan", item.Options[0].OptionID, item.Options[1].OptionID)
	}
}

func TestKimiHandleSelectModeCanonicalizesModeID(t *testing.T) {
	// 大小写不敏感命中后，发给 CLI 的必须是白名单里的规范小写 id。
	in := buildInput(true, []string{"session_control", "set_mode"}, true)
	exec := &testExecutor{}
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: in,
		Request:    toolprotocol.ActionRequest{SessionID: "sess-1", ActionID: "select_mode", Event: "select", OptionID: "YOLO"},
		Executor:   exec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome == toolprotocol.ActionOutcomeRejected {
		t.Fatalf("select_mode YOLO rejected: %s", result.Code)
	}
	if len(exec.localActions) != 1 || exec.localActions[0].Params["mode_id"] != "yolo" {
		t.Fatalf("expected canonical mode_id yolo, got %+v", exec.localActions)
	}
	if exec.localActions[0].Params["display_label"] != "自动" {
		t.Fatalf("display_label = %v, want 自动", exec.localActions[0].Params["display_label"])
	}
}

func TestKimiHandleSelectModeRejectsFilteredMode(t *testing.T) {
	in := buildInput(true, []string{"session_control", "set_mode"}, true)
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: in,
		Request:    toolprotocol.ActionRequest{SessionID: "sess-1", ActionID: "select_mode", Event: "select", OptionID: "auto"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != toolprotocol.ActionOutcomeRejected || result.Code != "invalid_option" {
		t.Fatalf("select_mode auto outcome=%q code=%q want rejected/invalid_option", result.Outcome, result.Code)
	}
}

func TestKimiBuildModeHiddenWithoutReportedModes(t *testing.T) {
	in := buildInput(true, []string{"session_control", "set_mode"}, true)
	delete(in.Binding.Meta, "available_modes")
	snap, err := New().Build(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if item := findItem(snap, "select_mode"); item != nil {
		t.Fatal("select_mode should be hidden when connector reports no modes")
	}
}

func TestKimiBuildSessionControlDisabledWhenOffline(t *testing.T) {
	snap, err := New().Build(context.Background(), buildInput(false, []string{"session_control", "get_session_usage"}, true))
	if err != nil {
		t.Fatal(err)
	}
	item := findItem(snap, "session_control")
	if item == nil {
		t.Fatal("session_control item not found")
	}
	if !item.Disabled {
		t.Fatal("session_control should be disabled when offline")
	}
}

func TestKimiBuildProviderQuotaFromBindingMeta(t *testing.T) {
	in := buildInput(true, []string{"session_control", "set_mode", "get_rate_limits"}, true)
	in.Binding.Meta["provider_quota"] = map[string]any{
		"provider":      "kimi",
		"providerLabel": "Kimi",
		"tiers": []any{
			map[string]any{"name": "five_hour", "label": "5h limit", "usedPercent": 12.5, "resetsAt": "2026-07-17T12:00:00Z"},
		},
	}
	snap, err := New().Build(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	item := findItem(snap, "provider_quota_five_hour")
	if item == nil {
		t.Fatal("provider_quota_five_hour item not found")
	}
	if item.Percent != 12.5 {
		t.Fatalf("quota percent=%v want=12.5", item.Percent)
	}
	if item.LocalAction != "get_rate_limits" {
		t.Fatalf("quota localAction=%q want=get_rate_limits", item.LocalAction)
	}
}

func TestKimiBuildProviderQuotaPlaceholderWhenDeclared(t *testing.T) {
	// 无 provider_quota meta 时不渲染 0% 的 5H 占位。
	snap, err := New().Build(context.Background(), buildInput(true, []string{"session_control", "get_rate_limits"}, true))
	if err != nil {
		t.Fatal(err)
	}
	if findItem(snap, "provider_quota_five_hour") != nil {
		t.Fatal("provider_quota_five_hour placeholder must not appear without quota data")
	}
}

func TestKimiBuildProviderQuotaHiddenWithoutLocalAction(t *testing.T) {
	snap, err := New().Build(context.Background(), buildInput(true, []string{"session_control", "set_mode"}, true))
	if err != nil {
		t.Fatal(err)
	}
	if findItem(snap, "provider_quota_five_hour") != nil {
		t.Fatal("quota placeholder must not appear when get_rate_limits not declared")
	}
}

func TestKimiBuildContextWindowReadOnly(t *testing.T) {
	in := buildInput(true, []string{"session_control", "set_mode"}, true)
	in.Binding.Meta["context_window"] = map[string]any{
		"usedPercentage":      42.5,
		"remainingPercentage": 57.5,
	}
	snap, err := New().Build(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	item := findItem(snap, "context_window")
	if item == nil {
		t.Fatal("context_window item not found")
	}
	if item.Percent != 42.5 {
		t.Fatalf("context percent=%v want=42.5", item.Percent)
	}
	if !item.Disabled {
		t.Fatal("context_window item must be read-only (disabled)")
	}
	if item.LocalAction != "" {
		t.Fatalf("context_window must not bind a local action, got %q", item.LocalAction)
	}
}

func TestKimiBuildContextWindowHiddenWithoutMeta(t *testing.T) {
	snap, err := New().Build(context.Background(), buildInput(true, []string{"session_control"}, true))
	if err != nil {
		t.Fatal(err)
	}
	if findItem(snap, "context_window") != nil {
		t.Fatal("context_window item must be hidden without meta")
	}
}

func TestKimiBuildContextWindowHiddenWhenValueNotNumeric(t *testing.T) {
	// usedPercentage 为 null/字符串时不得渲染假数据（0%/剩余 100%）。
	for _, bad := range []any{nil, "42.5"} {
		in := buildInput(true, []string{"session_control"}, true)
		in.Binding.Meta["context_window"] = map[string]any{"usedPercentage": bad}
		snap, err := New().Build(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		if findItem(snap, "context_window") != nil {
			t.Fatalf("context_window must be hidden when usedPercentage=%v", bad)
		}
	}
}

func TestKimiHandleContextWindowRejectedReadOnly(t *testing.T) {
	bi := buildInput(true, []string{"session_control"}, true)
	exec := &testExecutor{}
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: bi,
		Request:    toolprotocol.ActionRequest{SessionID: "sess-1", ActionID: "context_window", Event: "click"},
		Executor:   exec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != toolprotocol.ActionOutcomeRejected || result.Code != "read_only" {
		t.Fatalf("context_window should reject with read_only, got outcome=%v code=%s", result.Outcome, result.Code)
	}
	if len(exec.localActions) != 0 {
		t.Fatalf("context_window must not dispatch any local action, got %+v", exec.localActions)
	}
}

func TestKimiBuildSlashCommandsItem(t *testing.T) {
	snap, err := New().Build(context.Background(), buildInput(true, []string{"session_control"}, true))
	if err != nil {
		t.Fatal(err)
	}
	item := findItem(snap, "slash_commands")
	if item == nil {
		t.Fatal("slash_commands item not found (kimi commands not registered?)")
	}
	if len(item.Commands) == 0 {
		t.Fatal("slash_commands item has no commands")
	}
	for _, cmd := range item.Commands {
		if cmd.Name == "/model" {
			t.Fatal("/model must not be advertised for kimi (global-config persistence)")
		}
	}
}

// ── HandleAction ──────────────────────────────────────────────────────────────

func TestKimiHandleSessionControlStatus(t *testing.T) {
	bi := buildInput(true, []string{"session_control", "get_session_usage"}, true)
	exec := &testExecutor{}
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: bi,
		Request:    toolprotocol.ActionRequest{SessionID: "sess-1", ActionID: "session_control", Event: "select", OptionID: "status"},
		Executor:   exec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome == toolprotocol.ActionOutcomeRejected {
		t.Fatalf("status rejected: %s", result.Code)
	}
	if len(exec.localActions) != 1 || exec.localActions[0].ActionType != "session_control" {
		t.Fatalf("expected session_control dispatch, got %+v", exec.localActions)
	}
	if exec.localActions[0].Params["verb"] != "status" {
		t.Fatalf("verb = %v, want status", exec.localActions[0].Params["verb"])
	}
}

func TestKimiHandleSessionControlUsageRejected(t *testing.T) {
	// usage 入口已收掉：即便前端残留旧快照点了 usage，也必须拒绝而不是转发。
	bi := buildInput(true, []string{"session_control", "get_session_usage"}, true)
	exec := &testExecutor{}
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: bi,
		Request:    toolprotocol.ActionRequest{SessionID: "sess-1", ActionID: "session_control", Event: "select", OptionID: "usage"},
		Executor:   exec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != toolprotocol.ActionOutcomeRejected {
		t.Fatalf("usage should be rejected, got outcome=%v code=%s", result.Outcome, result.Code)
	}
	if len(exec.localActions) != 0 {
		t.Fatalf("usage must not dispatch any local action, got %+v", exec.localActions)
	}
}

func TestKimiHandleGetRateLimitsDispatches(t *testing.T) {
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
		t.Fatalf("session_id = %v, want sess-1", exec.localActions[0].Params["session_id"])
	}
}

func TestKimiHandleGetRateLimitsWithoutDeclarationRejected(t *testing.T) {
	bi := buildInput(true, []string{"session_control"}, true)
	exec := &testExecutor{}
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: bi,
		Request:    toolprotocol.ActionRequest{SessionID: "sess-1", ActionID: "get_rate_limits", Event: "click"},
		Executor:   exec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != toolprotocol.ActionOutcomeRejected || result.Code != "local_action_unavailable" {
		t.Fatalf("expected local_action_unavailable, got outcome=%v code=%s", result.Outcome, result.Code)
	}
	if len(exec.localActions) != 0 {
		t.Fatalf("must not dispatch without declaration, got %+v", exec.localActions)
	}
}

func TestKimiHandleSelectModeDispatches(t *testing.T) {
	bi := buildInput(true, []string{"session_control", "set_mode"}, true)
	exec := &testExecutor{}
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: bi,
		Request:    toolprotocol.ActionRequest{SessionID: "sess-1", ActionID: "select_mode", Event: "select", OptionID: "plan"},
		Executor:   exec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome == toolprotocol.ActionOutcomeRejected {
		t.Fatalf("select_mode rejected: %s", result.Code)
	}
	if len(exec.localActions) != 1 || exec.localActions[0].ActionType != "set_mode" {
		t.Fatalf("expected set_mode dispatch, got %+v", exec.localActions)
	}
	if exec.localActions[0].Params["mode_id"] != "plan" {
		t.Fatalf("mode_id = %v, want plan", exec.localActions[0].Params["mode_id"])
	}
	if exec.localActions[0].Params["display_label"] != "计划" {
		t.Fatalf("display_label = %v, want 计划", exec.localActions[0].Params["display_label"])
	}
}

func TestKimiHandleSelectModelDispatches(t *testing.T) {
	bi := buildInput(true, []string{"session_control", "set_model", "set_mode"}, true)
	exec := &testExecutor{}
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: bi,
		Request:    toolprotocol.ActionRequest{SessionID: "sess-1", ActionID: "select_model", Event: "select", OptionID: "kimi-k2"},
		Executor:   exec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome == toolprotocol.ActionOutcomeRejected {
		t.Fatalf("select_model rejected: %s", result.Code)
	}
	if len(exec.localActions) != 1 || exec.localActions[0].ActionType != "set_model" {
		t.Fatalf("expected set_model dispatch, got %+v", exec.localActions)
	}
	if exec.localActions[0].Params["model_id"] != "kimi-k2" {
		t.Fatalf("model_id = %v, want kimi-k2", exec.localActions[0].Params["model_id"])
	}
	if exec.localActions[0].Params["display_label"] != "Kimi K2" {
		t.Fatalf("display_label = %v, want Kimi K2", exec.localActions[0].Params["display_label"])
	}
}

func TestKimiHandleSelectModelWithoutDeclarationRejected(t *testing.T) {
	bi := buildInput(true, []string{"session_control", "set_mode"}, true)
	exec := &testExecutor{}
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: bi,
		Request:    toolprotocol.ActionRequest{SessionID: "sess-1", ActionID: "select_model", Event: "select", OptionID: "kimi-k2"},
		Executor:   exec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != toolprotocol.ActionOutcomeRejected || result.Code != "local_action_unavailable" {
		t.Fatalf("expected local_action_unavailable, got outcome=%v code=%s", result.Outcome, result.Code)
	}
	if len(exec.localActions) != 0 {
		t.Fatalf("must not dispatch without declaration, got %+v", exec.localActions)
	}
}

func TestKimiHandleSessionControlOfflineRejected(t *testing.T) {
	bi := buildInput(false, []string{"session_control"}, true)
	exec := &testExecutor{}
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: bi,
		Request:    toolprotocol.ActionRequest{SessionID: "sess-1", ActionID: "session_control", Event: "select", OptionID: "status"},
		Executor:   exec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != toolprotocol.ActionOutcomeRejected || result.Code != "agent_offline" {
		t.Fatalf("offline should reject with agent_offline, got outcome=%v code=%s", result.Outcome, result.Code)
	}
}

func TestKimiHandleStopOutput(t *testing.T) {
	bi := buildInput(true, []string{"session_control"}, true)
	bi.Run = toolruntime.RunState{HasActiveRun: true, CanStop: true, RunID: "run-1"}
	exec := &testExecutor{}
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: bi,
		Request:    toolprotocol.ActionRequest{SessionID: "sess-1", ActionID: "stop_output", Event: "click"},
		Executor:   exec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome == toolprotocol.ActionOutcomeRejected {
		t.Fatalf("stop_output rejected: %s", result.Code)
	}
	if len(exec.stopRequests) != 1 {
		t.Fatalf("expected 1 stop request, got %d", len(exec.stopRequests))
	}
}
