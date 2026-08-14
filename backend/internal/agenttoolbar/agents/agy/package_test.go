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
			map[string]any{"id": ""},                          // 空 id，应跳过
			"not-a-map",                                        // 非法项，应跳过
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
