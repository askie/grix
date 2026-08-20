package agy

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
			ClientType: model.AgentClientTypeAgy,
		},
		Runtime: toolruntime.Profile{
			Online:       online,
			LocalActions: localActions,
		},
		Binding: core.BindingInfo{
			Cwd: cwd,
			Meta: map[string]any{
				"model_id": "gemini-2.5-pro",
				"available_models": []any{
					map[string]any{"id": "gemini-2.5-pro", "displayName": "Gemini 2.5 Pro"},
				},
			},
		},
	}
}

func actionInput(bi core.BuildInput, actionID, optionID string) core.ActionInput {
	return core.ActionInput{
		BuildInput: bi,
		Request: toolprotocol.ActionRequest{
			SessionID: "sess-1",
			ActionID:  actionID,
			Event:     "select",
			OptionID:  optionID,
		},
		Executor: &testExecutor{},
	}
}

// ── Build() ───────────────────────────────────────────────────────────────────

func TestAgyBuildNoBinding(t *testing.T) {
	snap, err := New().Build(context.Background(), buildInput(true, []string{"session_control", "set_model", "get_session_usage"}, false))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range snap.Items {
		if item.ItemID == "session_control" || item.ItemID == "select_model" {
			t.Fatalf("item %q should not appear without binding", item.ItemID)
		}
	}
}

// agy 工具栏应保留平台通用的"会话列表"按钮（由 core 规范化阶段前置），
// 即不得置位 OmitListSessionsButton。connector 侧已按 agy 维度返回会话列表。
func TestAgyBuildExposesListSessionsButton(t *testing.T) {
	snap, err := New().Build(context.Background(), buildInput(true, []string{"session_control", "set_model", "get_session_usage"}, true))
	if err != nil {
		t.Fatal(err)
	}
	if snap.OmitListSessionsButton {
		t.Fatal("agy should NOT omit the list_sessions button")
	}
}

// agy 工具栏应显示队列数量按钮（由 core 规范化阶段前置），即不得置位 OmitQueueButton。
func TestAgyBuildExposesQueueButton(t *testing.T) {
	snap, err := New().Build(context.Background(), buildInput(true, []string{"session_control", "set_model", "get_session_usage"}, true))
	if err != nil {
		t.Fatal(err)
	}
	if snap.OmitQueueButton {
		t.Fatal("agy should NOT omit the show_queue button")
	}
}

func TestAgyBuildWithBindingSessionControlItem(t *testing.T) {
	snap, err := New().Build(context.Background(), buildInput(true, []string{"session_control", "set_model", "get_session_usage"}, true))
	if err != nil {
		t.Fatal(err)
	}
	var found *toolprotocol.Item
	for i := range snap.Items {
		if snap.Items[i].ItemID == "session_control" {
			found = &snap.Items[i]
		}
	}
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
	for _, want := range []string{"status", "restart", "usage"} {
		if !optIDs[want] {
			t.Fatalf("option %q missing from session_control", want)
		}
	}
}

func TestAgyBuildSessionControlDisabledWhenOffline(t *testing.T) {
	snap, err := New().Build(context.Background(), buildInput(false, []string{"session_control", "get_session_usage"}, true))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range snap.Items {
		if item.ItemID == "session_control" && !item.Disabled {
			t.Fatal("session_control should be disabled when offline")
		}
	}
}

func TestAgyBuildUsageOptionDisabledWhenNoLocalAction(t *testing.T) {
	// get_session_usage not advertised
	snap, err := New().Build(context.Background(), buildInput(true, []string{"session_control", "set_model"}, true))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range snap.Items {
		if item.ItemID != "session_control" {
			continue
		}
		for _, opt := range item.Options {
			if opt.OptionID == "usage" && !opt.Disabled {
				t.Fatal("usage option should be disabled when get_session_usage not in localActions")
			}
		}
	}
}

func TestAgyBuildRateLimitBuckets(t *testing.T) {
	in := buildInput(true, []string{"session_control", "set_model", "get_rate_limits"}, true)
	in.Binding.Meta["rate_limits"] = map[string]any{
		"primary": map[string]any{
			"usedPercent":   64.5,
			"windowMinutes": float64(300),
			"resetsAt":      "2026-08-20T10:00:00Z",
		},
		"secondary": map[string]any{
			"usedPercent":   31.0,
			"windowMinutes": float64(10080),
			"resetsAt":      "2026-08-24T10:00:00Z",
		},
		"sampledAt": float64(1787200800000),
	}
	in.Binding.Meta["extra_limits"] = []any{
		map[string]any{
			"id":            "claude_5h",
			"label":         "Claude 5H",
			"usedPercent":   72.0,
			"windowMinutes": float64(300),
			"resetsAt":      "2026-08-20T11:00:00Z",
		},
		map[string]any{
			"id":            "gpt_weekly",
			"label":         "GPT weekly",
			"usedPercent":   19.2,
			"windowMinutes": float64(10080),
			"resetsAt":      "2026-08-25T12:00:00Z",
		},
	}

	snap, err := New().Build(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}

	assertRateLimitItem := func(itemID, centerText, desc, detail string, percent float64) {
		t.Helper()
		item, ok := snap.FindItem(itemID)
		if !ok {
			t.Fatalf("%s item not found", itemID)
		}
		if item.Kind != toolprotocol.ItemKindProgress {
			t.Fatalf("%s kind=%q want progress", itemID, item.Kind)
		}
		if item.ActionID != "get_rate_limits" {
			t.Fatalf("%s actionID=%q want get_rate_limits", itemID, item.ActionID)
		}
		if item.LocalAction != "get_rate_limits" {
			t.Fatalf("%s localAction=%q want get_rate_limits", itemID, item.LocalAction)
		}
		if item.CenterText != centerText || item.ProgressDesc != desc || item.ProgressDetail != detail {
			t.Fatalf("%s display=(%q,%q,%q), want=(%q,%q,%q)",
				itemID, item.CenterText, item.ProgressDesc, item.ProgressDetail, centerText, desc, detail)
		}
		if item.Percent != percent {
			t.Fatalf("%s percent=%v want %v", itemID, item.Percent, percent)
		}
	}

	assertRateLimitItem("rate_limit_primary", "5H", "Gemini 5H", "5H / 2026-08-20T10:00:00Z", 64.5)
	assertRateLimitItem("rate_limit_secondary", "7D", "Gemini weekly", "7D / 2026-08-24T10:00:00Z", 31.0)
	assertRateLimitItem("rate_limit_extra_0", "72", "Claude 5H", "5H / 2026-08-20T11:00:00Z", 72.0)
	assertRateLimitItem("rate_limit_extra_1", "19", "GPT weekly", "7D / 2026-08-25T12:00:00Z", 19.2)
	if _, ok := snap.FindItem("agy_quota"); ok {
		t.Fatal("legacy agy_quota should be hidden when rate_limits are rendered")
	}
}

func TestAgyBuildLegacyQuotaFallback(t *testing.T) {
	t.Run("quota_exhausted", func(t *testing.T) {
		in := buildInput(true, []string{"session_control", "set_model", "get_rate_limits"}, true)
		in.Binding.Meta["quota_exhausted"] = true
		in.Binding.Meta["quota_reset_at"] = int64(1787200800)
		in.Binding.Meta["plan"] = "Legacy Plan"

		snap, err := New().Build(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		item, ok := snap.FindItem("agy_quota")
		if !ok {
			t.Fatal("legacy quota_exhausted fallback item not found")
		}
		if item.Variant != "danger" || item.Percent != 100 || item.CenterText != "耗尽" {
			t.Fatalf("legacy exhausted item=%+v", item)
		}
		if item.ProgressDesc != "Legacy Plan 配额耗尽" || item.ProgressDetail != "1787200800" {
			t.Fatalf("legacy exhausted display=(%q,%q)", item.ProgressDesc, item.ProgressDetail)
		}
	})

	t.Run("available_credits", func(t *testing.T) {
		in := buildInput(true, []string{"session_control", "set_model", "get_rate_limits"}, true)
		in.Binding.Meta["available_credits"] = 42.0
		in.Binding.Meta["plan"] = "Legacy Credits"

		snap, err := New().Build(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		item, ok := snap.FindItem("agy_quota")
		if !ok {
			t.Fatal("legacy available_credits fallback item not found")
		}
		if item.Variant != "secondary" || item.CenterText != "积分" {
			t.Fatalf("legacy credits item=%+v", item)
		}
		if item.ProgressDesc != "Legacy Credits" || item.ProgressDetail != "42 积分" {
			t.Fatalf("legacy credits display=(%q,%q)", item.ProgressDesc, item.ProgressDetail)
		}
	})

	t.Run("new_zero_rate_limits_do_not_fall_back_to_legacy", func(t *testing.T) {
		in := buildInput(true, []string{"session_control", "set_model", "get_rate_limits"}, true)
		in.Binding.Meta["available_credits"] = 42.0
		in.Binding.Meta["rate_limits"] = map[string]any{
			"primary": map[string]any{
				"usedPercent":   0.0,
				"windowMinutes": float64(300),
				"resetsAt":      "2026-08-20T10:00:00Z",
			},
			"secondary": map[string]any{
				"usedPercent":   0.0,
				"windowMinutes": float64(10080),
				"resetsAt":      "2026-08-24T10:00:00Z",
			},
			"sampledAt": float64(1787200800000),
		}

		snap, err := New().Build(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := snap.FindItem("agy_quota"); ok {
			t.Fatal("legacy quota should not render when a fresh rate_limits payload has zero-used buckets")
		}
		primary, ok := snap.FindItem("rate_limit_primary")
		if !ok {
			t.Fatal("zero-used primary rate limit should render")
		}
		if primary.Percent != 0 || primary.LocalAction != "get_rate_limits" || primary.CenterText != "5H" {
			t.Fatalf("primary zero item=%+v", primary)
		}
		secondary, ok := snap.FindItem("rate_limit_secondary")
		if !ok {
			t.Fatal("zero-used secondary rate limit should render")
		}
		if secondary.Percent != 0 || secondary.LocalAction != "get_rate_limits" || secondary.CenterText != "7D" {
			t.Fatalf("secondary zero item=%+v", secondary)
		}
	})

	t.Run("empty_rate_limits_do_not_fall_back_to_legacy", func(t *testing.T) {
		in := buildInput(true, []string{"session_control", "set_model", "get_rate_limits"}, true)
		in.Binding.Meta["available_credits"] = 42.0
		in.Binding.Meta["rate_limits"] = map[string]any{}

		snap, err := New().Build(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := snap.FindItem("agy_quota"); ok {
			t.Fatal("legacy quota should not render when rate_limits is explicitly empty")
		}
		if _, ok := snap.FindItem("rate_limit_primary"); ok {
			t.Fatal("empty rate_limits should not render a primary item")
		}
	})

	t.Run("invalid_or_missing_used_percent_is_hidden", func(t *testing.T) {
		in := buildInput(true, []string{"session_control", "set_model", "get_rate_limits"}, true)
		in.Binding.Meta["rate_limits"] = map[string]any{
			"primary": map[string]any{
				"windowMinutes": float64(300),
			},
			"secondary": map[string]any{
				"usedPercent":   -1.0,
				"windowMinutes": float64(10080),
			},
			"sampledAt": float64(1787200800000),
		}
		in.Binding.Meta["extra_limits"] = []any{
			map[string]any{
				"label":         "Zero Extra",
				"usedPercent":   0.0,
				"windowMinutes": float64(300),
			},
			map[string]any{
				"label":         "Bad Extra",
				"usedPercent":   "0",
				"windowMinutes": float64(300),
			},
		}

		snap, err := New().Build(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := snap.FindItem("rate_limit_primary"); ok {
			t.Fatal("missing usedPercent should not render primary")
		}
		if _, ok := snap.FindItem("rate_limit_secondary"); ok {
			t.Fatal("invalid usedPercent should not render secondary")
		}
		extra, ok := snap.FindItem("rate_limit_extra_0")
		if !ok {
			t.Fatal("zero-used extra rate limit should render")
		}
		if extra.Percent != 0 || extra.CenterText != "1" || extra.ProgressDesc != "Zero Extra" {
			t.Fatalf("zero extra item=%+v", extra)
		}
		if _, ok := snap.FindItem("rate_limit_extra_1"); ok {
			t.Fatal("invalid extra usedPercent should not render")
		}
	})
}

// ── HandleAction: session_control ────────────────────────────────────────────

func TestAgyHandleSessionControlStatus(t *testing.T) {
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

func TestAgyHandleSessionControlRestart(t *testing.T) {
	bi := buildInput(true, []string{"session_control"}, true)
	exec := &testExecutor{}
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: bi,
		Request:    toolprotocol.ActionRequest{SessionID: "sess-1", ActionID: "session_control", Event: "select", OptionID: "restart"},
		Executor:   exec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome == toolprotocol.ActionOutcomeRejected {
		t.Fatalf("restart rejected: %s", result.Code)
	}
	if len(exec.localActions) != 1 || exec.localActions[0].Params["verb"] != "restart" {
		t.Fatalf("expected restart dispatch, got %+v", exec.localActions)
	}
}

func TestAgyHandleSessionControlUsage(t *testing.T) {
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
	if result.Outcome == toolprotocol.ActionOutcomeRejected {
		t.Fatalf("usage rejected: %s", result.Code)
	}
	if len(exec.localActions) != 1 || exec.localActions[0].ActionType != "get_session_usage" {
		t.Fatalf("expected get_session_usage dispatch, got %+v", exec.localActions)
	}
}

func TestAgyHandleGetRateLimits(t *testing.T) {
	bi := buildInput(true, []string{"session_control", "get_rate_limits"}, true)
	exec := &testExecutor{}
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: bi,
		Request:    toolprotocol.ActionRequest{SessionID: "sess-1", ActionID: "get_rate_limits", Event: "click"},
		Executor:   exec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome == toolprotocol.ActionOutcomeRejected {
		t.Fatalf("get_rate_limits rejected: %s", result.Code)
	}
	if len(exec.localActions) != 1 {
		t.Fatalf("local action count=%d want 1", len(exec.localActions))
	}
	got := exec.localActions[0]
	if got.ActionType != "get_rate_limits" {
		t.Fatalf("actionType=%q want get_rate_limits", got.ActionType)
	}
	if got.Params["session_id"] != "sess-1" {
		t.Fatalf("session_id param=%v want sess-1", got.Params["session_id"])
	}
	if got.TimeoutMs != 20_000 {
		t.Fatalf("timeout=%d want 20000", got.TimeoutMs)
	}
}

func TestAgyHandleSessionControlOfflineGuard(t *testing.T) {
	bi := buildInput(false, []string{"session_control"}, true)
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: bi,
		Request:    toolprotocol.ActionRequest{SessionID: "sess-1", ActionID: "session_control", Event: "select", OptionID: "status"},
		Executor:   &testExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != toolprotocol.ActionOutcomeRejected || result.Code != "agent_offline" {
		t.Fatalf("want agent_offline rejection, got outcome=%q code=%q", result.Outcome, result.Code)
	}
}

func TestAgyHandleSessionControlMissingLocalActionGuard(t *testing.T) {
	bi := buildInput(true, []string{}, true) // no session_control declared
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: bi,
		Request:    toolprotocol.ActionRequest{SessionID: "sess-1", ActionID: "session_control", Event: "select", OptionID: "restart"},
		Executor:   &testExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != toolprotocol.ActionOutcomeRejected || result.Code != "local_action_unavailable" {
		t.Fatalf("want local_action_unavailable, got outcome=%q code=%q", result.Outcome, result.Code)
	}
}

func TestAgyHandleSessionControlInvalidOptionGuard(t *testing.T) {
	bi := buildInput(true, []string{"session_control"}, true)
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: bi,
		Request:    toolprotocol.ActionRequest{SessionID: "sess-1", ActionID: "session_control", Event: "select", OptionID: "invalid_verb"},
		Executor:   &testExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != toolprotocol.ActionOutcomeRejected || result.Code != "invalid_option" {
		t.Fatalf("want invalid_option rejection, got outcome=%q code=%q", result.Outcome, result.Code)
	}
}

func TestAgyHandleUsageOfflineGuard(t *testing.T) {
	bi := buildInput(false, []string{"get_session_usage"}, true)
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: bi,
		Request:    toolprotocol.ActionRequest{SessionID: "sess-1", ActionID: "session_control", Event: "select", OptionID: "usage"},
		Executor:   &testExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != toolprotocol.ActionOutcomeRejected || result.Code != "agent_offline" {
		t.Fatalf("want agent_offline for usage when offline, got outcome=%q code=%q", result.Outcome, result.Code)
	}
}

func TestAgyHandleUsageMissingLocalActionGuard(t *testing.T) {
	bi := buildInput(true, []string{"session_control"}, true) // no get_session_usage
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: bi,
		Request:    toolprotocol.ActionRequest{SessionID: "sess-1", ActionID: "session_control", Event: "select", OptionID: "usage"},
		Executor:   &testExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != toolprotocol.ActionOutcomeRejected || result.Code != "local_action_unavailable" {
		t.Fatalf("want local_action_unavailable for usage, got outcome=%q code=%q", result.Outcome, result.Code)
	}
}

func TestAgyHandleInvalidActionGuard(t *testing.T) {
	bi := buildInput(true, []string{"session_control"}, true)
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: bi,
		Request:    toolprotocol.ActionRequest{SessionID: "sess-1", ActionID: "unknown_action", Event: "click"},
		Executor:   &testExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != toolprotocol.ActionOutcomeRejected || result.Code != "invalid_action" {
		t.Fatalf("want invalid_action rejection, got outcome=%q code=%q", result.Outcome, result.Code)
	}
}

func TestBuildAgyModelOptions(t *testing.T) {
	meta := map[string]any{
		"model_id": "Gemini 3.5 Flash (Medium)",
		"available_models": []any{
			map[string]any{"id": "Gemini 3.5 Flash (Medium)", "displayName": "Gemini 3.5 Flash (Medium)"},
			map[string]any{"id": "Claude Opus 4.6 (Thinking)", "display_name": "Claude Opus 4.6 (Thinking)"},
			map[string]any{"id": "Gemini 3.5 Flash (Medium)"}, // 重复 id，应去重
			map[string]any{"id": ""}, // 空 id，应跳过
			"not-a-map",              // 非法项，应跳过
		},
	}

	opts := buildAgyModelOptions(meta)
	if len(opts) != 2 {
		t.Fatalf("len(opts) = %d, want 2", len(opts))
	}
	if opts[0].ID != "Gemini 3.5 Flash (Medium)" || opts[0].Label != "Gemini 3.5 Flash (Medium)" {
		t.Fatalf("opts[0] = %+v", opts[0])
	}
	// display_name 蛇形键也要能解析为 label
	if opts[1].ID != "Claude Opus 4.6 (Thinking)" || opts[1].Label != "Claude Opus 4.6 (Thinking)" {
		t.Fatalf("opts[1] = %+v", opts[1])
	}
}

func TestBuildAgyModelOptionsEmptyMeta(t *testing.T) {
	if opts := buildAgyModelOptions(nil); opts != nil {
		t.Fatalf("nil meta should yield nil options, got %+v", opts)
	}
	if opts := buildAgyModelOptions(map[string]any{}); opts != nil {
		t.Fatalf("empty meta should yield nil options, got %+v", opts)
	}
}

func TestResolveAgyModelLabel(t *testing.T) {
	opts := []agyModelOption{
		{ID: "a", Label: "Alpha"},
		{ID: "b", Label: "Beta"},
	}
	if got := resolveAgyModelLabel("b", opts); got != "Beta" {
		t.Fatalf("resolveAgyModelLabel(b) = %q, want Beta", got)
	}
	// 未知 id：原样返回 id
	if got := resolveAgyModelLabel("zzz", opts); got != "zzz" {
		t.Fatalf("resolveAgyModelLabel(zzz) = %q, want zzz", got)
	}
	// 空 id：回退到首个选项
	if got := resolveAgyModelLabel("", opts); got != "Alpha" {
		t.Fatalf("resolveAgyModelLabel(empty) = %q, want Alpha", got)
	}
	// 空 id 且无选项：占位
	if got := resolveAgyModelLabel("", nil); got != "模型" {
		t.Fatalf("resolveAgyModelLabel(empty,nil) = %q, want 模型", got)
	}
}
