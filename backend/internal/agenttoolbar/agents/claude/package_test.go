package claude_test

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/agenttoolbar/agents/claude"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/gemini"
	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	"github.com/askie/grix/backend/internal/model"
)

type claudeEffortTestExecutor struct {
	localActions []core.LocalActionRequest
}

func (e *claudeEffortTestExecutor) DispatchLocalAction(_ context.Context, req core.LocalActionRequest) error {
	e.localActions = append(e.localActions, req)
	return nil
}

func (e *claudeEffortTestExecutor) StopOutput(context.Context, core.StopOutputRequest) error {
	return nil
}

func (e *claudeEffortTestExecutor) SendStopText(context.Context, core.StopOutputRequest) error {
	return nil
}

func claudeEffortBuildInput(meta map[string]any, localActions []string) core.BuildInput {
	return core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-claude-effort"},
		Agent:   core.AgentInfo{AgentID: 9001, OwnerID: 1001, ProviderType: model.AgentProviderAPI, ClientType: model.AgentClientTypeClaude},
		Runtime: toolruntime.Profile{Online: true, LocalActions: localActions},
		Binding: core.BindingInfo{BindingID: "claude-binding", Cwd: "/workspace/project", Meta: meta},
	}
}

func TestClaudeExtraRateLimitKeepsResetTimeAndWindow(t *testing.T) {
	snapshot, err := claude.New().Build(context.Background(), claudeEffortBuildInput(map[string]any{
		"rate_limits": map[string]any{"sampledAt": float64(1)},
		"extra_limits": []any{map[string]any{
			"label":         "Claude weekly",
			"usedPercent":   32.0,
			"windowMinutes": float64(10080),
			"resetsAt":      "2026-08-28T00:00:57Z",
		}},
	}, []string{"get_rate_limits"}))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	item, ok := snapshot.FindItem("rate_limit_extra_0")
	if !ok {
		t.Fatal("rate_limit_extra_0 item missing")
	}
	if item.ProgressDetail != "2026-08-28T00:00:57Z" {
		t.Fatalf("progress_detail = %q, want raw reset time", item.ProgressDetail)
	}
	if item.ProgressWindowMinutes != 10080 {
		t.Fatalf("progress_window_minutes = %v, want 10080", item.ProgressWindowMinutes)
	}
}

func TestClaudeReasoningEffortSelectorBuildAndAction(t *testing.T) {
	meta := map[string]any{
		"effort":           "high",
		"reasoning_effort": "low", // canonical effort wins over the compatibility alias.
		"available_efforts": []any{
			"low", "medium", "high", "xhigh", "max", "auto", "high",
		},
	}
	buildInput := claudeEffortBuildInput(meta, []string{"set_reasoning_effort"})
	snapshot, err := claude.New().Build(context.Background(), buildInput)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	item, ok := snapshot.FindItem("select_reasoning_effort")
	if !ok {
		t.Fatal("select_reasoning_effort item missing")
	}
	if item.Disabled {
		t.Fatalf("select_reasoning_effort disabled: %s", item.Tooltip)
	}
	if item.Label != "高" || item.Value != "high" {
		t.Fatalf("current effort label/value = %q/%q, want 高/high", item.Label, item.Value)
	}
	wantOptions := []string{"low", "medium", "high", "xhigh", "max", "auto"}
	if len(item.Options) != len(wantOptions) {
		t.Fatalf("effort options=%d want=%d", len(item.Options), len(wantOptions))
	}
	for i, want := range wantOptions {
		if item.Options[i].OptionID != want {
			t.Fatalf("effort option[%d]=%q want=%q", i, item.Options[i].OptionID, want)
		}
	}

	for _, optionID := range []string{"high", "auto"} {
		executor := &claudeEffortTestExecutor{}
		result, err := claude.New().HandleAction(context.Background(), core.ActionInput{
			BuildInput: buildInput,
			Request: toolprotocol.ActionRequest{
				SessionID: "sess-claude-effort",
				ActionID:  "select_reasoning_effort",
				Event:     "select",
				OptionID:  optionID,
			},
			Executor: executor,
		})
		if err != nil {
			t.Fatalf("HandleAction(%s) error = %v", optionID, err)
		}
		if result.Outcome != toolprotocol.ActionOutcomeAcceptedNoStateChange {
			t.Fatalf("HandleAction(%s) outcome=%q", optionID, result.Outcome)
		}
		if len(executor.localActions) != 1 {
			t.Fatalf("HandleAction(%s) local actions=%d want=1", optionID, len(executor.localActions))
		}
		req := executor.localActions[0]
		if req.ActionType != "set_reasoning_effort" {
			t.Fatalf("action_type=%q want=set_reasoning_effort", req.ActionType)
		}
		if req.Params["session_id"] != "sess-claude-effort" {
			t.Fatalf("session_id=%v", req.Params["session_id"])
		}
		if req.Params["effort"] != optionID {
			t.Fatalf("effort=%v want=%s", req.Params["effort"], optionID)
		}
		if req.Params["reasoning_effort"] != optionID {
			t.Fatalf("reasoning_effort alias=%v want=%s", req.Params["reasoning_effort"], optionID)
		}
	}

	autoMeta := map[string]any{
		"effort":            "auto",
		"available_efforts": []any{"low", "medium", "high", "auto"},
	}
	autoSnapshot, err := claude.New().Build(context.Background(), claudeEffortBuildInput(autoMeta, []string{"set_reasoning_effort"}))
	if err != nil {
		t.Fatalf("Build(auto meta) error = %v", err)
	}
	autoItem, ok := autoSnapshot.FindItem("select_reasoning_effort")
	if !ok {
		t.Fatal("auto select_reasoning_effort item missing")
	}
	if autoItem.Label != "自动" || autoItem.Value != "auto" {
		t.Fatalf("auto effort label/value = %q/%q, want 自动/auto", autoItem.Label, autoItem.Value)
	}

	invalidMeta := map[string]any{
		"effort":            "unsupported",
		"available_efforts": []any{"low", "medium", "auto"},
	}
	invalidSnapshot, err := claude.New().Build(context.Background(), claudeEffortBuildInput(invalidMeta, []string{"set_reasoning_effort"}))
	if err != nil {
		t.Fatalf("Build(invalid meta) error = %v", err)
	}
	invalidItem, ok := invalidSnapshot.FindItem("select_reasoning_effort")
	if !ok {
		t.Fatal("invalid select_reasoning_effort item missing")
	}
	if invalidItem.Label != "低" || invalidItem.Value != "low" {
		t.Fatalf("invalid effort fallback label/value = %q/%q, want 低/low", invalidItem.Label, invalidItem.Value)
	}
}

func TestClaudeReasoningEffortCompatibilityAndNonClaudeVisibility(t *testing.T) {
	aliasMeta := map[string]any{
		"reasoning_effort":  "xhigh",
		"available_efforts": []any{"low", "xhigh", "auto"},
	}
	snapshot, err := claude.New().Build(context.Background(), claudeEffortBuildInput(aliasMeta, []string{"set_reasoning_effort"}))
	if err != nil {
		t.Fatalf("Build(alias meta) error = %v", err)
	}
	item, ok := snapshot.FindItem("select_reasoning_effort")
	if !ok || item.Label != "极高" {
		t.Fatalf("alias current effort item=%+v found=%v", item, ok)
	}

	missingMeta := map[string]any{"reasoning_effort": "high"}
	snapshot, err = claude.New().Build(context.Background(), claudeEffortBuildInput(missingMeta, []string{"set_reasoning_effort"}))
	if err != nil {
		t.Fatalf("Build(old meta) error = %v", err)
	}
	if _, ok := snapshot.FindItem("select_reasoning_effort"); ok {
		t.Fatal("effort selector should be hidden when available_efforts is missing")
	}

	nonClaudeInput := claudeEffortBuildInput(aliasMeta, []string{"set_reasoning_effort"})
	nonClaudeInput.Agent.ClientType = model.AgentClientTypeGemini
	nonClaudeSnapshot, err := gemini.New().Build(context.Background(), nonClaudeInput)
	if err != nil {
		t.Fatalf("Build(non-Claude) error = %v", err)
	}
	if _, ok := nonClaudeSnapshot.FindItem("select_reasoning_effort"); ok {
		t.Fatal("non-Claude toolbar should not expose select_reasoning_effort")
	}
}

func TestClaudeReasoningEffortSelectorSafelyDisablesOldConnector(t *testing.T) {
	meta := map[string]any{
		"effort":            "medium",
		"available_efforts": []any{"low", "medium", "auto"},
	}
	snapshot, err := claude.New().Build(context.Background(), claudeEffortBuildInput(meta, nil))
	if err != nil {
		t.Fatalf("Build(old connector) error = %v", err)
	}
	item, ok := snapshot.FindItem("select_reasoning_effort")
	if !ok {
		t.Fatal("effort selector missing when meta advertises efforts")
	}
	if !item.Disabled {
		t.Fatal("effort selector should be disabled when set_reasoning_effort is missing")
	}
}
