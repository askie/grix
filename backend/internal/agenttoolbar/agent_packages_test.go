package agenttoolbar_test

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/agenttoolbar/agents/agy"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/claude"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/codewhale"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/codex"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/cursor"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/deepseek"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/gemini"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/hermes"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/kiro"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/openclaw"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/opencode"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/openhuman"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/pi"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/qwen"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/reasonix"
	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	"github.com/askie/grix/backend/internal/model"
)

type packageTestExecutor struct {
	localActions     []core.LocalActionRequest
	stopRequests     []core.StopOutputRequest
	stopTextRequests []core.StopOutputRequest
}

func (e *packageTestExecutor) DispatchLocalAction(_ context.Context, req core.LocalActionRequest) error {
	e.localActions = append(e.localActions, req)
	return nil
}

func (e *packageTestExecutor) StopOutput(_ context.Context, req core.StopOutputRequest) error {
	e.stopRequests = append(e.stopRequests, req)
	return nil
}

func (e *packageTestExecutor) SendStopText(_ context.Context, req core.StopOutputRequest) error {
	e.stopTextRequests = append(e.stopTextRequests, req)
	return nil
}

func TestAgentPackagesBuildAndHandleAction(t *testing.T) {
	cases := []struct {
		name          string
		clientType    string
		pkg           core.Package
		localActions  []string
		wantItemCount int
		firstActionID string
		lastActionID  string
		hasStopOutput bool
		// stopViaText 为 true 时该 agent 的停止走 SendStopText(/stop) 分叉，而非 StopOutput。
		stopViaText bool
		// skipSessionSelect 为 true 时跳过 session_control 选择子测试(该 agent 不再提供工作空间下拉)。
		skipSessionSelect bool
	}{
		// agy has no slash commands registered, items: stop_output + session_control + select_model = 3
		{name: "agy", clientType: model.AgentClientTypeAgy, pkg: agy.New(), localActions: []string{"session_control", "set_model", "get_session_usage"}, wantItemCount: 3, firstActionID: "stop_output", lastActionID: "select_model", hasStopOutput: true},
		{name: "claude", clientType: model.AgentClientTypeClaude, pkg: claude.New(), localActions: []string{"session_control", "set_mode", "get_session_usage"}, wantItemCount: 6, firstActionID: "slash_commands", lastActionID: "thread_compact", hasStopOutput: true},
		{name: "codex", clientType: model.AgentClientTypeCodex, pkg: codex.New(), localActions: []string{"session_control", "thread_compact", "set_model", "set_mode", "get_session_usage"}, wantItemCount: 7, firstActionID: "slash_commands", lastActionID: "select_sandbox_mode", hasStopOutput: true},
		{name: "gemini", clientType: model.AgentClientTypeGemini, pkg: gemini.New(), localActions: []string{"session_control", "set_model", "set_mode", "get_session_usage"}, wantItemCount: 5, firstActionID: "slash_commands", lastActionID: "select_mode", hasStopOutput: false},
		{name: "openclaw", clientType: model.AgentClientTypeOpenClaw, pkg: openclaw.New(), localActions: []string{"session_control", "get_session_usage"}, wantItemCount: 3, firstActionID: "slash_commands", lastActionID: "get_session_usage", hasStopOutput: true, skipSessionSelect: true},
		{name: "qwen", clientType: model.AgentClientTypeQwen, pkg: qwen.New(), localActions: []string{"session_control", "set_model", "set_mode", "get_session_usage"}, wantItemCount: 5, firstActionID: "slash_commands", lastActionID: "select_mode", hasStopOutput: true},
		{name: "hermes", clientType: model.AgentClientTypeHermes, pkg: hermes.New(), localActions: []string{"session_control", "get_session_usage"}, wantItemCount: 4, firstActionID: "slash_commands", lastActionID: "get_session_usage", hasStopOutput: true, stopViaText: true, skipSessionSelect: true},
		{name: "pi", clientType: model.AgentClientTypePi, pkg: pi.New(), localActions: []string{"session_control", "set_model", "get_session_usage", "get_rate_limits"}, wantItemCount: 4, firstActionID: "slash_commands", lastActionID: "select_model", hasStopOutput: true},
		{name: "cursor", clientType: model.AgentClientTypeCursor, pkg: cursor.New(), localActions: []string{"session_control", "set_model", "set_mode", "get_rate_limits", "thread_compact"}, wantItemCount: 7, firstActionID: "slash_commands", lastActionID: "select_mode", hasStopOutput: true},
		{name: "openhuman", clientType: model.AgentClientTypeOpenHuman, pkg: openhuman.New(), localActions: []string{"session_control"}, wantItemCount: 3, firstActionID: "slash_commands", lastActionID: "session_control", hasStopOutput: true},
		{name: "reasonix", clientType: model.AgentClientTypeReasonix, pkg: reasonix.New(), localActions: []string{"session_control", "set_model", "set_mode", "get_session_usage"}, wantItemCount: 5, firstActionID: "slash_commands", lastActionID: "select_mode", hasStopOutput: true},
		{name: "codewhale", clientType: model.AgentClientTypeCodeWhale, pkg: codewhale.New(), localActions: []string{"session_control", "get_session_usage"}, wantItemCount: 4, firstActionID: "slash_commands", lastActionID: "select_model", hasStopOutput: false},
		{name: "opencode", clientType: model.AgentClientTypeOpenCode, pkg: opencode.New(), localActions: []string{"session_control", "set_model", "set_mode", "get_session_usage"}, wantItemCount: 4, firstActionID: "slash_commands", lastActionID: "select_model", hasStopOutput: true},
		{name: "deepseek", clientType: model.AgentClientTypeDeepSeek, pkg: deepseek.New(), localActions: []string{"session_control", "set_provider", "set_model", "set_mode", "set_thinking", "set_reasoning_effort", "get_session_usage", "get_rate_limits"}, wantItemCount: 6, firstActionID: "stop_output", lastActionID: "select_reasoning_effort", hasStopOutput: true},
	}

	for _, tc := range cases {
		t.Run(tc.name+"_build", func(t *testing.T) {
			snapshot, err := tc.pkg.Build(context.Background(), core.BuildInput{
				OwnerID: 1001,
				Session: core.SessionInfo{SessionID: "sess-1"},
				Agent: core.AgentInfo{
					AgentID:      9001,
					OwnerID:      1001,
					ProviderType: model.AgentProviderAPI,
					ClientType:   tc.clientType,
				},
				Runtime: toolruntime.Profile{
					Online:       true,
					LocalActions: tc.localActions,
				},
				Binding: core.BindingInfo{
					Cwd: "/workspace/project",
					Meta: map[string]any{
						"model_id": "gpt-5.4",
						"mode_id":  "default",
						"available_models": []any{
							map[string]any{"id": "gpt-5.4", "display_name": "gpt-5.4"},
						},
					},
				},
				Run: toolruntime.RunState{
					HasActiveRun: true,
					RunID:        "run-1",
					CanStop:      true,
					State:        "streaming",
				},
			})
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}
			if !snapshot.Visible {
				t.Fatalf("snapshot.Visible = false, want true")
			}
			if len(snapshot.Items) != tc.wantItemCount {
				t.Fatalf("len(snapshot.Items) = %d, want %d", len(snapshot.Items), tc.wantItemCount)
			}

			if snapshot.Items[0].ActionID != tc.firstActionID {
				t.Fatalf("first action_id = %q, want %q", snapshot.Items[0].ActionID, tc.firstActionID)
			}
			lastIdx := len(snapshot.Items) - 1
			if snapshot.Items[lastIdx].ActionID != tc.lastActionID {
				t.Fatalf("last action_id = %q, want %q", snapshot.Items[lastIdx].ActionID, tc.lastActionID)
			}
			if tc.clientType == model.AgentClientTypeHermes {
				item, ok := snapshot.FindItem("select_model")
				if !ok || !item.Disabled || item.Value != "gpt-5.4" || len(item.Options) != 1 || item.Options[0].Disabled {
					t.Fatalf("Hermes select_model=%+v found=%v", item, ok)
				}
			}
			if tc.clientType == model.AgentClientTypeCursor {
				item, ok := snapshot.FindItem("select_mode")
				if !ok || item.Disabled || len(item.Options) != 3 {
					t.Fatalf("Cursor select_mode=%+v found=%v", item, ok)
				}
				if item.Options[0].OptionID != "approval" || item.Options[1].OptionID != "full_auto" || item.Options[2].OptionID != "plan" {
					t.Fatalf("Cursor mode options=%+v", item.Options)
				}
				// 包内固定中文源文案，英文由 agenttoolbar/i18n.LocalizeText 统一映射（与 Gemini/Claude 等一致）。
				if item.Options[0].Label != "人工确认" || item.Options[1].Label != "自由模式" || item.Options[2].Label != "计划模式" {
					t.Fatalf("Cursor mode labels=%+v want 人工确认/自由模式/计划模式", item.Options)
				}
				monthly, ok := snapshot.FindItem("rate_limit_monthly")
				if !ok || monthly.CenterText != "M" || monthly.LocalAction != "get_rate_limits" {
					t.Fatalf("Cursor rate_limit_monthly=%+v found=%v", monthly, ok)
				}
				compact, ok := snapshot.FindItem("thread_compact")
				if !ok || compact.Kind != toolprotocol.ItemKindProgress || compact.LocalAction != "thread_compact" || compact.Disabled {
					t.Fatalf("Cursor thread_compact=%+v found=%v", compact, ok)
				}
			}
		})

		t.Run(tc.name+"_handle_stop", func(t *testing.T) {
			if !tc.hasStopOutput {
				t.Skip("package does not support stop_output")
			}
			executor := &packageTestExecutor{}
			result, err := tc.pkg.HandleAction(context.Background(), core.ActionInput{
				BuildInput: core.BuildInput{
					OwnerID: 1001,
					Session: core.SessionInfo{SessionID: "sess-1"},
					Agent: core.AgentInfo{
						AgentID:      9001,
						OwnerID:      1001,
						ProviderType: model.AgentProviderAPI,
						ClientType:   tc.clientType,
					},
					Runtime: toolruntime.Profile{
						Online:       true,
						LocalActions: tc.localActions,
					},
					Run: toolruntime.RunState{
						HasActiveRun: true,
						RunID:        "run-1",
						CanStop:      true,
						State:        "streaming",
					},
				},
				Request: toolprotocol.ActionRequest{
					SessionID: "sess-1",
					ActionID:  "stop_output",
					Event:     "click",
				},
				Executor: executor,
			})
			if err != nil {
				t.Fatalf("HandleAction(stop_output) error = %v", err)
			}
			if result.Outcome != toolprotocol.ActionOutcomeAcceptedWithImmediateRefresh {
				t.Fatalf("stop outcome = %q, want %q", result.Outcome, toolprotocol.ActionOutcomeAcceptedWithImmediateRefresh)
			}
			if tc.stopViaText {
				if len(executor.stopTextRequests) != 1 {
					t.Fatalf("stop text requests = %d, want 1", len(executor.stopTextRequests))
				}
				if len(executor.stopRequests) != 0 {
					t.Fatalf("stop requests = %d, want 0 (hermes uses /stop fork)", len(executor.stopRequests))
				}
			} else if len(executor.stopRequests) != 1 {
				t.Fatalf("stop requests = %d, want 1", len(executor.stopRequests))
			}
		})

		t.Run(tc.name+"_handle_select", func(t *testing.T) {
			if tc.skipSessionSelect {
				t.Skip("package does not provide session_control select")
			}
			executor := &packageTestExecutor{}
			result, err := tc.pkg.HandleAction(context.Background(), core.ActionInput{
				BuildInput: core.BuildInput{
					OwnerID: 1001,
					Session: core.SessionInfo{SessionID: "sess-1"},
					Agent: core.AgentInfo{
						AgentID:      9001,
						OwnerID:      1001,
						ProviderType: model.AgentProviderAPI,
						ClientType:   tc.clientType,
					},
					Runtime: toolruntime.Profile{
						Online:       true,
						LocalActions: tc.localActions,
					},
				},
				Request: toolprotocol.ActionRequest{
					SessionID: "sess-1",
					ActionID:  "session_control",
					Event:     "select",
					OptionID:  "status",
				},
				Executor: executor,
			})
			if err != nil {
				t.Fatalf("HandleAction(session_control) error = %v", err)
			}
			// 不同 package 对 session_control select 的 outcome 实现不一:
			// cursor / codewhale 走 dispatchLocalAction 默认 AcceptedWithImmediateRefresh,
			// 其余走 shared.HandleSessionControlAction 默认 AcceptedNoStateChange。
			// 这里只校验"被接受"的语义,具体 outcome 留给各 package 实现决定。
			if result.Outcome != toolprotocol.ActionOutcomeAcceptedNoStateChange &&
				result.Outcome != toolprotocol.ActionOutcomeAcceptedWithImmediateRefresh {
				t.Fatalf("select outcome = %q, want accepted_no_state_change or accepted_with_immediate_refresh", result.Outcome)
			}
			if len(executor.localActions) != 1 {
				t.Fatalf("local actions = %d, want 1", len(executor.localActions))
			}
			if got := executor.localActions[0].Params["verb"]; got != "status" {
				t.Fatalf("verb = %v, want status", got)
			}
		})
	}
}

func TestOpenCodePackageBuild_ModelAndModeSelectorsFromMeta(t *testing.T) {
	snapshot, err := opencode.New().Build(context.Background(), core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-opencode"},
		Agent: core.AgentInfo{
			AgentID:      9001,
			OwnerID:      1001,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypeOpenCode,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"session_control", "set_model", "set_mode", "get_session_usage"},
		},
		Binding: core.BindingInfo{
			BindingID: "bind-opencode",
			Cwd:       "/workspace/project",
			Meta: map[string]any{
				"model_id": "qwen3-coder",
				"mode_id":  "plan",
				"available_models": []any{
					map[string]any{"id": "qwen3-coder", "displayName": "Qwen3 Coder"},
					map[string]any{"id": "gpt-5", "display_name": "GPT-5"},
				},
				"available_modes": []any{
					map[string]any{"id": "default", "displayName": "默认"},
					map[string]any{"id": "plan", "displayName": "计划"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	modelItem, ok := snapshot.FindItem("select_model")
	if !ok {
		t.Fatal("select_model item not found")
	}
	if modelItem.Disabled {
		t.Fatalf("select_model disabled, tooltip=%q", modelItem.Tooltip)
	}
	if modelItem.Value != "qwen3-coder" || modelItem.BadgeText != "Qwen3 Coder" {
		t.Fatalf("select_model value=%q badge=%q, want qwen3-coder/Qwen3 Coder", modelItem.Value, modelItem.BadgeText)
	}
	if len(modelItem.Options) != 2 {
		t.Fatalf("select_model options=%d want=2", len(modelItem.Options))
	}

	modeItem, ok := snapshot.FindItem("select_mode")
	if !ok {
		t.Fatal("select_mode item not found")
	}
	if modeItem.Disabled {
		t.Fatalf("select_mode disabled, tooltip=%q", modeItem.Tooltip)
	}
	if modeItem.Value != "plan" || modeItem.BadgeText != "计划" {
		t.Fatalf("select_mode value=%q badge=%q, want plan/计划", modeItem.Value, modeItem.BadgeText)
	}
	if len(modeItem.Options) != 2 {
		t.Fatalf("select_mode options=%d want=2", len(modeItem.Options))
	}
}

func TestOpenCodePackageBuild_RateLimitsAndContextWindow(t *testing.T) {
	snapshot, err := opencode.New().Build(context.Background(), core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-opencode"},
		Agent: core.AgentInfo{
			AgentID:      9001,
			OwnerID:      1001,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypeOpenCode,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"session_control", "set_model", "set_mode", "get_rate_limits"},
		},
		Binding: core.BindingInfo{
			BindingID: "bind-opencode",
			Cwd:       "/workspace/project",
			Meta: map[string]any{
				"provider_quota": map[string]any{
					"provider":      "kimi",
					"providerLabel": "Kimi",
					"tiers": []any{
						map[string]any{"name": "five_hour", "label": "5h limit", "usedPercent": 62.5, "resetsAt": "2026-08-07T08:00:00Z"},
						map[string]any{"name": "weekly_limit", "label": "7d limit", "usedPercent": 25.0, "resetsAt": "2026-08-14T08:00:00Z"},
					},
				},
				"context_window": map[string]any{
					"used":                50000,
					"size":                200000,
					"usedPercentage":      25.0,
					"remainingPercentage": 75.0,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	fh, ok := snapshot.FindItem("provider_quota_five_hour")
	if !ok {
		t.Fatal("provider_quota_five_hour item not found")
	}
	if fh.Percent != 62.5 {
		t.Fatalf("provider_quota_five_hour percent=%v want=62.5", fh.Percent)
	}
	if fh.LocalAction != "get_rate_limits" {
		t.Fatalf("provider_quota_five_hour LocalAction=%q", fh.LocalAction)
	}

	sd, ok := snapshot.FindItem("provider_quota_weekly_limit")
	if !ok {
		t.Fatal("provider_quota_weekly_limit item not found")
	}
	if sd.Percent != 25.0 {
		t.Fatalf("provider_quota_weekly_limit percent=%v want=25", sd.Percent)
	}

	cw, ok := snapshot.FindItem("context_window")
	if !ok {
		t.Fatal("context_window item not found")
	}
	if cw.Percent != 25.0 {
		t.Fatalf("context_window percent=%v want=25", cw.Percent)
	}
	if cw.ProgressDetail != "剩余 75.0%" {
		t.Fatalf("context_window detail=%q, want 剩余 75.0%%", cw.ProgressDetail)
	}
}

func TestOpenCodePackageBuild_NoRateLimitsWithoutMeta(t *testing.T) {
	snapshot, err := opencode.New().Build(context.Background(), core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-opencode"},
		Agent: core.AgentInfo{
			AgentID:      9001,
			OwnerID:      1001,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypeOpenCode,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"session_control", "set_model", "set_mode", "get_rate_limits"},
		},
		Binding: core.BindingInfo{
			BindingID: "bind-opencode",
			Cwd:       "/workspace/project",
			Meta:      map[string]any{"model_id": "qwen3-coder"},
		},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if _, ok := snapshot.FindItem("provider_quota_five_hour"); ok {
		t.Fatal("provider_quota item should not render without provider_quota meta")
	}
	if _, ok := snapshot.FindItem("context_window"); ok {
		t.Fatal("context_window should not render without context_window meta")
	}
}

func TestOpenCodePackageHandleAction_ModelAndMode(t *testing.T) {
	pkg := opencode.New()
	buildInput := core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-opencode"},
		Agent: core.AgentInfo{
			AgentID:      9001,
			OwnerID:      1001,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypeOpenCode,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"set_model", "set_mode"},
		},
		Binding: core.BindingInfo{
			BindingID: "bind-opencode",
			Cwd:       "/workspace/project",
			Meta: map[string]any{
				"available_models": []any{
					map[string]any{"id": "gpt-5", "display_name": "GPT-5"},
				},
				"available_modes": []any{
					map[string]any{"id": "plan", "displayName": "计划"},
				},
			},
		},
	}

	executor := &packageTestExecutor{}
	tests := []struct {
		name       string
		request    toolprotocol.ActionRequest
		actionType string
		paramKey   string
		wantValue  string
		wantLabel  string
	}{
		{
			name:       "model",
			request:    toolprotocol.ActionRequest{SessionID: "sess-opencode", ActionID: "select_model", Event: "select", OptionID: "gpt-5"},
			actionType: "set_model",
			paramKey:   "model_id",
			wantValue:  "gpt-5",
			wantLabel:  "GPT-5",
		},
		{
			name:       "mode",
			request:    toolprotocol.ActionRequest{SessionID: "sess-opencode", ActionID: "select_mode", Event: "select", OptionID: "plan"},
			actionType: "set_mode",
			paramKey:   "mode_id",
			wantValue:  "plan",
			wantLabel:  "计划",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := pkg.HandleAction(context.Background(), core.ActionInput{
				BuildInput: buildInput,
				Request:    tt.request,
				Executor:   executor,
			})
			if err != nil {
				t.Fatalf("HandleAction(%s) error = %v", tt.request.ActionID, err)
			}
			if result.Outcome != toolprotocol.ActionOutcomeAcceptedWithImmediateRefresh {
				t.Fatalf("HandleAction(%s) outcome=%q", tt.request.ActionID, result.Outcome)
			}
		})
	}

	if len(executor.localActions) != 2 {
		t.Fatalf("local actions = %d, want 2", len(executor.localActions))
	}
	for i, tt := range tests {
		action := executor.localActions[i]
		if action.ActionType != tt.actionType {
			t.Fatalf("local action[%d].ActionType=%q want=%q", i, action.ActionType, tt.actionType)
		}
		if got := action.Params[tt.paramKey]; got != tt.wantValue {
			t.Fatalf("local action[%d].%s=%v want=%s", i, tt.paramKey, got, tt.wantValue)
		}
		if got := action.Params["display_label"]; got != tt.wantLabel {
			t.Fatalf("local action[%d].display_label=%v want=%s", i, got, tt.wantLabel)
		}
		if got := action.Params["session_id"]; got != "sess-opencode" {
			t.Fatalf("local action[%d].session_id=%v want=sess-opencode", i, got)
		}
	}
}

func TestCursorPackageDispatchesMode(t *testing.T) {
	pkg := cursor.New()
	buildInput := core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-cursor"},
		Agent:   core.AgentInfo{AgentID: 9001, ClientType: model.AgentClientTypeCursor},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"set_mode"},
		},
	}

	executor := &packageTestExecutor{}
	result, err := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: buildInput,
		Request: toolprotocol.ActionRequest{
			SessionID: "sess-cursor",
			ActionID:  "select_mode",
			OptionID:  "full_auto",
		},
		Executor: executor,
	})
	if err != nil {
		t.Fatalf("select_mode error = %v", err)
	}
	if result.Outcome != toolprotocol.ActionOutcomeAcceptedWithImmediateRefresh {
		t.Fatalf("select_mode outcome = %q", result.Outcome)
	}
	if len(executor.localActions) != 1 || executor.localActions[0].ActionType != "set_mode" {
		t.Fatalf("select_mode local actions = %+v", executor.localActions)
	}
	if got := executor.localActions[0].Params["mode_id"]; got != "full_auto" {
		t.Fatalf("select_mode mode_id = %v", got)
	}
}

func TestCursorPackageBuild_IncludesRestartSessionControl(t *testing.T) {
	snapshot, err := cursor.New().Build(context.Background(), core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-cursor"},
		Agent: core.AgentInfo{
			AgentID:    9001,
			OwnerID:    1001,
			ClientType: model.AgentClientTypeCursor,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"session_control", "set_model", "set_mode"},
		},
		Binding: core.BindingInfo{Cwd: "/workspace/cursor-demo"},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	sessionItem, ok := snapshot.FindItem("session_control")
	if !ok {
		t.Fatal("session_control item not found")
	}
	if len(sessionItem.Options) != 3 {
		t.Fatalf("session_control options=%d want=3", len(sessionItem.Options))
	}
	if sessionItem.Options[0].OptionID != "status" || sessionItem.Options[1].OptionID != "restart" || sessionItem.Options[2].OptionID != "unbind" {
		t.Fatalf("session_control options=%v want [status, restart, unbind]", sessionItem.Options)
	}
	if sessionItem.Options[1].Label != "重启会话" {
		t.Fatalf("restart label=%q want=重启会话", sessionItem.Options[1].Label)
	}
}

func TestCursorPackageHandleAction_RestartDispatchesSessionControl(t *testing.T) {
	executor := &packageTestExecutor{}
	result, err := cursor.New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: core.BuildInput{
			OwnerID: 1001,
			Session: core.SessionInfo{SessionID: "sess-cursor"},
			Agent: core.AgentInfo{
				AgentID:    9001,
				OwnerID:    1001,
				ClientType: model.AgentClientTypeCursor,
			},
			Runtime: toolruntime.Profile{
				Online:       true,
				LocalActions: []string{"session_control"},
			},
		},
		Request: toolprotocol.ActionRequest{
			SessionID: "sess-cursor",
			ActionID:  "session_control",
			Event:     "select",
			OptionID:  "restart",
		},
		Executor: executor,
	})
	if err != nil {
		t.Fatalf("HandleAction(restart) error = %v", err)
	}
	if result.Outcome != toolprotocol.ActionOutcomeAcceptedWithImmediateRefresh {
		t.Fatalf("restart outcome=%q want=%q", result.Outcome, toolprotocol.ActionOutcomeAcceptedWithImmediateRefresh)
	}
	if len(executor.localActions) != 1 {
		t.Fatalf("local actions=%d want=1", len(executor.localActions))
	}
	action := executor.localActions[0]
	if action.ActionType != "session_control" {
		t.Fatalf("action_type=%q want=session_control", action.ActionType)
	}
	if got := action.Params["verb"]; got != "restart" {
		t.Fatalf("verb=%v want=restart", got)
	}
	if got := action.Params["display_label"]; got != "重启" {
		t.Fatalf("display_label=%v want=重启", got)
	}
	if action.TimeoutMs != 30_000 {
		t.Fatalf("timeout_ms=%d want=30000", action.TimeoutMs)
	}
}

func TestClaudePackageHandleModeAndRestartActions(t *testing.T) {
	pkg := claude.New()
	executor := &packageTestExecutor{}
	buildInput := core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-1"},
		Agent: core.AgentInfo{
			AgentID:      9001,
			OwnerID:      1001,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypeClaude,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"session_control", "set_mode"},
		},
	}

	result, err := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: buildInput,
		Request: toolprotocol.ActionRequest{
			SessionID: "sess-1",
			ActionID:  "select_mode",
			Event:     "select",
			OptionID:  "full_auto",
		},
		Executor: executor,
	})
	if err != nil {
		t.Fatalf("HandleAction(select_mode) error = %v", err)
	}
	if result.Outcome != toolprotocol.ActionOutcomeAcceptedNoStateChange {
		t.Fatalf("HandleAction(select_mode) outcome = %q", result.Outcome)
	}
	if len(executor.localActions) != 1 {
		t.Fatalf("local actions = %d, want 1", len(executor.localActions))
	}
	if got := executor.localActions[0].ActionType; got != "set_mode" {
		t.Fatalf("action_type = %q, want set_mode", got)
	}
	if got := executor.localActions[0].Params["mode_id"]; got != "full_auto" {
		t.Fatalf("mode_id = %v, want full_auto", got)
	}
}

func TestClaudePackageBuild_HidesToolbarWithoutSessionBinding(t *testing.T) {
	snapshot, err := claude.New().Build(context.Background(), core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-claude"},
		Agent: core.AgentInfo{
			AgentID:      9001,
			OwnerID:      1001,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypeClaude,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"session_control", "set_mode"},
		},
		Binding: core.BindingInfo{},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if snapshot.Visible {
		t.Fatal("snapshot.Visible = true, want false when session is not bound")
	}
	if len(snapshot.Items) != 0 {
		t.Fatalf("len(snapshot.Items) = %d, want 0", len(snapshot.Items))
	}
}

func TestGeminiPackageBuild_UsesStoredSelectionValues(t *testing.T) {
	snapshot, err := gemini.New().Build(context.Background(), core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-gemini"},
		Agent: core.AgentInfo{
			AgentID:      9001,
			OwnerID:      1001,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypeGemini,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"session_control", "set_model", "set_mode"},
		},
		Binding: core.BindingInfo{
			Cwd: "/workspace/demo",
			Meta: map[string]any{
				"model_id": "gemini-2.5-pro",
				"mode_id":  "default",
			},
		},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	modelItem, ok := snapshot.FindItem("select_model")
	if !ok {
		t.Fatal("select_model item not found")
	}
	// 测试 meta 未提供 available_models 列表, 当前实现下 Value=Label=id 本身。
	if modelItem.Value != "gemini-2.5-pro" {
		t.Fatalf("model value=%q want=gemini-2.5-pro", modelItem.Value)
	}

	modeItem, ok := snapshot.FindItem("select_mode")
	if !ok {
		t.Fatal("select_mode item not found")
	}
	if modeItem.Value != "default" {
		t.Fatalf("mode value=%q want=default", modeItem.Value)
	}
	if modeItem.BadgeText != "默认（需确认）" {
		t.Fatalf("mode badge=%q want=默认（需确认）", modeItem.BadgeText)
	}

	sessionItem, ok := snapshot.FindItem("session_control")
	if !ok {
		t.Fatal("session_control item not found")
	}
	if len(sessionItem.Options) != 4 {
		t.Fatalf("session_control options=%d want=4", len(sessionItem.Options))
	}
	for _, option := range sessionItem.Options {
		if option.OptionID == "where" {
			t.Fatal("session_control should not include where option")
		}
	}
}

func TestGeminiPackageBuild_DisablesSelectorsWhenLocalActionMissing(t *testing.T) {
	snapshot, err := gemini.New().Build(context.Background(), core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-gemini"},
		Agent: core.AgentInfo{
			AgentID:      9001,
			OwnerID:      1001,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypeGemini,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"session_control"},
		},
		Binding: core.BindingInfo{
			BindingID: "gemini-binding-1",
			Cwd:       "/workspace/demo",
		},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	modelItem, ok := snapshot.FindItem("select_model")
	if !ok {
		t.Fatal("select_model item not found")
	}
	if !modelItem.Disabled {
		t.Fatal("select_model should be disabled when set_model is missing")
	}
	if modelItem.Tooltip != "当前插件未声明 set_model" {
		t.Fatalf("select_model tooltip=%q want current plugin message", modelItem.Tooltip)
	}

	modeItem, ok := snapshot.FindItem("select_mode")
	if !ok {
		t.Fatal("select_mode item not found")
	}
	if !modeItem.Disabled {
		t.Fatal("select_mode should be disabled when set_mode is missing")
	}
	if modeItem.Tooltip != "当前插件未声明 set_mode" {
		t.Fatalf("select_mode tooltip=%q want current plugin message", modeItem.Tooltip)
	}
}

func TestGeminiPackageBuild_HidesToolbarWithoutSessionBinding(t *testing.T) {
	snapshot, err := gemini.New().Build(context.Background(), core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-gemini"},
		Agent: core.AgentInfo{
			AgentID:      9001,
			OwnerID:      1001,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypeGemini,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"session_control", "set_model", "set_mode"},
		},
		Binding: core.BindingInfo{},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if snapshot.Visible {
		t.Fatal("snapshot.Visible = true, want false when session is not bound")
	}
	if len(snapshot.Items) != 0 {
		t.Fatalf("len(snapshot.Items) = %d, want 0", len(snapshot.Items))
	}
}

func TestCodexPackageHandleDedicatedActions(t *testing.T) {
	pkg := codex.New()
	executor := &packageTestExecutor{}
	buildInput := core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-1"},
		Agent: core.AgentInfo{
			AgentID:      9001,
			OwnerID:      1001,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypeCodex,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"thread_compact", "set_model", "set_mode"},
		},
		Run: toolruntime.RunState{
			HasActiveRun: true,
			RunID:        "run-1",
			CanStop:      true,
			State:        "streaming",
		},
		Binding: core.BindingInfo{
			Cwd: "/workspace/project",
			Meta: map[string]any{
				"model_id": "gpt-5.4",
				"mode_id":  "default",
				"available_models": []any{
					map[string]any{"id": "gpt-5.4", "display_name": "gpt-5.4"},
					map[string]any{"id": "gpt-5.4-mini", "display_name": "GPT-5.4-Mini"},
				},
			},
		},
	}

	tests := []toolprotocol.ActionRequest{
		{SessionID: "sess-1", ActionID: "thread_compact", Event: "click"},
		{SessionID: "sess-1", ActionID: "select_model", Event: "select", OptionID: "gpt-5.4-mini"},
		{SessionID: "sess-1", ActionID: "select_mode", Event: "select", OptionID: "plan"},
	}
	for _, req := range tests {
		result, err := pkg.HandleAction(context.Background(), core.ActionInput{
			BuildInput: buildInput,
			Request:    req,
			Executor:   executor,
		})
		if err != nil {
			t.Fatalf("HandleAction(%s) error = %v", req.ActionID, err)
		}
		if result.Outcome != toolprotocol.ActionOutcomeAcceptedNoStateChange {
			t.Fatalf("HandleAction(%s) outcome = %q", req.ActionID, result.Outcome)
		}
	}
	if len(executor.localActions) != 3 {
		t.Fatalf("local actions = %d, want 3", len(executor.localActions))
	}
	if got := executor.localActions[1].ActionType; got != "set_model" {
		t.Fatalf("second action_type = %q, want set_model", got)
	}
	if got := executor.localActions[1].Params["model_id"]; got != "gpt-5.4-mini" {
		t.Fatalf("model_id = %v, want gpt-5.4-mini", got)
	}
	if got := executor.localActions[2].ActionType; got != "set_mode" {
		t.Fatalf("third action_type = %q, want set_mode", got)
	}
	if got := executor.localActions[2].Params["mode_id"]; got != "plan" {
		t.Fatalf("mode_id = %v, want plan", got)
	}
}

func TestCodexPackageBuild_DisplaysCurrentSelectionsInRequestedOrder(t *testing.T) {
	snapshot, err := codex.New().Build(context.Background(), core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-codex"},
		Agent: core.AgentInfo{
			AgentID:      9001,
			OwnerID:      1001,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypeCodex,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"session_control", "thread_compact", "set_model", "set_mode"},
		},
		Binding: core.BindingInfo{
			BindingID: "bind-codex",
			Cwd:       "/workspace/project",
			Meta: map[string]any{
				"model_id": "gpt-5.4",
				"mode_id":  "default",
				"context_window": map[string]any{
					"usedPercentage": float64(42),
				},
				"available_models": []any{
					map[string]any{"id": "gpt-5.4", "display_name": "GPT-5.4"},
					map[string]any{"id": "gpt-5.4-mini", "display_name": "GPT-5.4-Mini"},
				},
			},
		},
		Run: toolruntime.RunState{
			HasActiveRun: true,
			RunID:        "run-1",
			CanStop:      true,
			State:        "streaming",
		},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	wantActions := []string{"slash_commands", "stop_output", "session_control", "select_mode", "select_model", "thread_compact", "select_sandbox_mode"}
	if len(snapshot.Items) != len(wantActions) {
		t.Fatalf("len(snapshot.Items) = %d, want %d", len(snapshot.Items), len(wantActions))
	}
	for i, want := range wantActions {
		if got := snapshot.Items[i].ActionID; got != want {
			t.Fatalf("item[%d].ActionID = %q, want %q", i, got, want)
		}
	}

	assertItemLabel := func(itemID, want string) {
		t.Helper()
		item, ok := snapshot.FindItem(itemID)
		if !ok {
			t.Fatalf("%s item not found", itemID)
		}
		if item.Label != want {
			t.Fatalf("%s label = %q, want %q", itemID, item.Label, want)
		}
		if item.Value != "" && item.Value != want {
			t.Fatalf("%s value = %q, want empty or %q", itemID, item.Value, want)
		}
	}
	assertItemLabel("session_control", "project")
	assertItemLabel("select_mode", "自动")
	assertItemLabel("select_model", "GPT-5.4")
	compactItem, ok := snapshot.FindItem("thread_compact")
	if !ok {
		t.Fatal("thread_compact item not found")
	}
	if compactItem.Kind != toolprotocol.ItemKindProgress {
		t.Fatalf("thread_compact kind=%q want %q", compactItem.Kind, toolprotocol.ItemKindProgress)
	}
	if compactItem.CenterText != "42" {
		t.Fatalf("thread_compact centerText=%q want %q", compactItem.CenterText, "42")
	}
}

func TestCodexPackageBuild_StopOutputVisibilityAcrossRunStates(t *testing.T) {
	pkg := codex.New()
	buildBase := func(state string, canStop bool) core.BuildInput {
		return core.BuildInput{
			OwnerID: 1001,
			Session: core.SessionInfo{SessionID: "sess-stop-visibility"},
			Agent: core.AgentInfo{
				AgentID:      9001,
				OwnerID:      1001,
				ProviderType: model.AgentProviderAPI,
				ClientType:   model.AgentClientTypeCodex,
			},
			Runtime: toolruntime.Profile{
				Online:       true,
				LocalActions: []string{"session_control", "thread_compact", "set_model", "set_mode"},
			},
			Binding: core.BindingInfo{
				BindingID: "bind-stop-visibility",
				Cwd:       "/home/user/project",
				Meta: map[string]any{
					"model_id": "gpt-5.4",
					"mode_id":  "default",
					"available_models": []any{
						map[string]any{"id": "gpt-5.4", "display_name": "gpt-5.4"},
					},
				},
			},
			Run: toolruntime.RunState{
				HasActiveRun: true,
				RunID:        "run-stop-1",
				State:        state,
				CanStop:      canStop,
			},
		}
	}

	assertStop := func(t *testing.T, state string, canStop bool, shouldShow bool, shouldDisable bool, shouldLoad bool) {
		t.Helper()
		snapshot, err := pkg.Build(context.Background(), buildBase(state, canStop))
		if err != nil {
			t.Fatalf("Build(%s) error = %v", state, err)
		}
		item, ok := snapshot.FindItem("stop_output")
		if !shouldShow {
			if ok {
				t.Fatalf("stop_output should be hidden for state=%s can_stop=%t", state, canStop)
			}
			return
		}
		if !ok {
			t.Fatalf("stop_output should be visible for state=%s can_stop=%t", state, canStop)
		}
		if item.Disabled != shouldDisable {
			t.Fatalf("stop_output disabled=%t want=%t state=%s", item.Disabled, shouldDisable, state)
		}
		if item.Loading != shouldLoad {
			t.Fatalf("stop_output loading=%t want=%t state=%s", item.Loading, shouldLoad, state)
		}
	}

	assertStop(t, "queued", true, true, false, false)
	assertStop(t, "received", true, true, false, false)
	assertStop(t, "streaming", true, true, false, false)
	assertStop(t, "stopping", false, true, true, true)
	assertStop(t, "completed", false, false, false, false)
}

func TestCodexPackageBuild_ContextProgressTurnsWarningAtHighUsage(t *testing.T) {
	snapshot, err := codex.New().Build(context.Background(), core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-codex-warning"},
		Agent: core.AgentInfo{
			AgentID:      9001,
			OwnerID:      1001,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypeCodex,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"session_control", "thread_compact", "set_model", "set_mode", "get_rate_limits"},
		},
		Binding: core.BindingInfo{
			Cwd: "/workspace/project",
			Meta: map[string]any{
				"model_id": "gpt-5.4",
				"mode_id":  "default",
				"context_window": map[string]any{
					"usedPercentage":      float64(86.5),
					"remainingPercentage": float64(13.5),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	item, ok := snapshot.FindItem("thread_compact")
	if !ok {
		t.Fatal("thread_compact item not found")
	}
	if item.Kind != toolprotocol.ItemKindProgress {
		t.Fatalf("thread_compact kind=%q want %q", item.Kind, toolprotocol.ItemKindProgress)
	}
	if item.Percent != 86.5 {
		t.Fatalf("thread_compact percent=%v want 86.5", item.Percent)
	}
	if item.CenterText != "87" {
		t.Fatalf("thread_compact centerText=%q want %q", item.CenterText, "87")
	}
	if item.Variant != "warning" {
		t.Fatalf("thread_compact variant=%q want %q", item.Variant, "warning")
	}
}

func TestCodexPackageBuild_DisablesSelectorsWithUpgradeHintWhenLocalActionMissing(t *testing.T) {
	snapshot, err := codex.New().Build(context.Background(), core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-codex"},
		Agent: core.AgentInfo{
			AgentID:      9001,
			OwnerID:      1001,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypeCodex,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"session_control", "thread_compact"},
		},
		Binding: core.BindingInfo{
			Cwd: "/workspace/demo",
			Meta: map[string]any{
				"model_id": "gpt-5.4",
				"mode_id":  "default",
				"available_models": []any{
					map[string]any{"id": "gpt-5.4", "display_name": "gpt-5.4"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	modelItem, ok := snapshot.FindItem("select_model")
	if !ok {
		t.Fatal("select_model item not found")
	}
	if !modelItem.Disabled {
		t.Fatal("select_model should be disabled when set_model is missing")
	}
	if modelItem.Tooltip != "当前 Codex 插件未声明 set_model，请升级并重启 grix-codex" {
		t.Fatalf("select_model tooltip=%q want codex upgrade hint", modelItem.Tooltip)
	}

	modeItem, ok := snapshot.FindItem("select_mode")
	if !ok {
		t.Fatal("select_mode item not found")
	}
	if !modeItem.Disabled {
		t.Fatal("select_mode should be disabled when set_mode is missing")
	}
	if modeItem.Tooltip != "当前 Codex 插件未声明 set_mode，请升级并重启 grix-codex" {
		t.Fatalf("select_mode tooltip=%q want codex upgrade hint", modeItem.Tooltip)
	}
}

func TestCodexPackageBuild_IncludesProgressItemsWhenRateLimitData(t *testing.T) {
	snapshot, err := codex.New().Build(context.Background(), core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-codex"},
		Agent: core.AgentInfo{
			AgentID:      9001,
			OwnerID:      1001,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypeCodex,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"session_control", "set_model", "set_mode", "get_rate_limits"},
		},
		Binding: core.BindingInfo{
			Cwd: "/workspace/project",
			Meta: map[string]any{
				"model_id": "codex-1",
				"mode_id":  "default",
				"rate_limits": map[string]any{
					"primary": map[string]any{
						"usedPercent":   float64(65),
						"windowMinutes": float64(300),
						"resetsAt":      "2026-05-19T21:00:00Z",
					},
					"secondary": map[string]any{
						"usedPercent":   float64(30),
						"windowMinutes": float64(10080),
						"resetsAt":      "2026-05-24T08:00:00Z",
					},
					"sampledAt": float64(1716152400000),
				},
			},
		},
		Run: toolruntime.RunState{},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	primaryItem, ok := snapshot.FindItem("rate_limit_primary")
	if !ok {
		t.Fatal("rate_limit_primary item not found when rate limit data is present")
	}
	if primaryItem.Kind != toolprotocol.ItemKindProgress {
		t.Fatalf("rate_limit_primary kind=%q want %q", primaryItem.Kind, toolprotocol.ItemKindProgress)
	}
	if primaryItem.Percent != 65 {
		t.Fatalf("rate_limit_primary percent=%v want 65", primaryItem.Percent)
	}
	if primaryItem.CenterText != "5H" {
		t.Fatalf("rate_limit_primary centerText=%q want %q", primaryItem.CenterText, "5H")
	}
	if primaryItem.LocalAction != "get_rate_limits" {
		t.Fatalf("rate_limit_primary localAction=%q want %q", primaryItem.LocalAction, "get_rate_limits")
	}

	secondaryItem, ok := snapshot.FindItem("rate_limit_secondary")
	if !ok {
		t.Fatal("rate_limit_secondary item not found when rate limit data is present")
	}
	if secondaryItem.Percent != 30 {
		t.Fatalf("rate_limit_secondary percent=%v want 30", secondaryItem.Percent)
	}
	if secondaryItem.CenterText != "7D" {
		t.Fatalf("rate_limit_secondary centerText=%q want %q", secondaryItem.CenterText, "7D")
	}

	if len(snapshot.Items) != 8 {
		t.Fatalf("len(snapshot.Items) = %d, want 8 (includes 2 progress)", len(snapshot.Items))
	}
}

func TestCodexPackageBuild_OmitsRateLimitItemsWhenZero(t *testing.T) {
	snapshot, err := codex.New().Build(context.Background(), core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-codex-rl-zero"},
		Agent: core.AgentInfo{
			AgentID:      9001,
			OwnerID:      1001,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypeCodex,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"session_control", "set_model", "set_mode", "get_rate_limits"},
		},
		Binding: core.BindingInfo{
			Cwd: "/workspace/project",
			Meta: map[string]any{
				"model_id": "codex-1",
				"mode_id":  "default",
				"rate_limits": map[string]any{
					"primary": map[string]any{
						"usedPercent":   float64(0),
						"windowMinutes": float64(300),
						"resetsAt":      "2026-05-19T21:00:00Z",
					},
					"secondary": map[string]any{
						"usedPercent":   float64(30),
						"windowMinutes": float64(10080),
						"resetsAt":      "2026-05-24T08:00:00Z",
					},
					"sampledAt": float64(1716152400000),
				},
			},
		},
		Run: toolruntime.RunState{},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if _, ok := snapshot.FindItem("rate_limit_primary"); ok {
		t.Fatal("rate_limit_primary should be omitted when percent is 0")
	}
	secondaryItem, ok := snapshot.FindItem("rate_limit_secondary")
	if !ok {
		t.Fatal("rate_limit_secondary item not found")
	}
	if secondaryItem.Percent != 30 {
		t.Fatalf("rate_limit_secondary percent=%v want 30", secondaryItem.Percent)
	}
}

func TestCodexPackageBuild_OmitsProgressItemsWhenRateLimitsSupportedButNoData(t *testing.T) {
	snapshot, err := codex.New().Build(context.Background(), core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-codex"},
		Agent: core.AgentInfo{
			AgentID:      9001,
			OwnerID:      1001,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypeCodex,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"session_control", "set_model", "set_mode", "get_rate_limits"},
		},
		Binding: core.BindingInfo{
			Cwd:  "/workspace/project",
			Meta: map[string]any{},
		},
		Run: toolruntime.RunState{},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// No placeholder for either window when no data — items appear once the provider reports.
	if _, ok := snapshot.FindItem("rate_limit_primary"); ok {
		t.Fatal("rate_limit_primary placeholder should not be present when no data")
	}
	if _, ok := snapshot.FindItem("rate_limit_secondary"); ok {
		t.Fatal("rate_limit_secondary placeholder should not be present when no data")
	}
	if len(snapshot.Items) != 6 {
		t.Fatalf("len(snapshot.Items) = %d, want 6 (no rate-limit progress without data)", len(snapshot.Items))
	}
}

func TestClaudePackageBuild_IncludesProgressItemsWhenRateLimitDataSnakeCase(t *testing.T) {
	snapshot, err := claude.New().Build(context.Background(), core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-claude"},
		Agent: core.AgentInfo{
			AgentID:      9002,
			OwnerID:      1001,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypeClaude,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"session_control", "set_mode", "get_rate_limits"},
		},
		Binding: core.BindingInfo{
			Cwd: "/workspace/project",
			Meta: map[string]any{
				"mode_id": "full_auto",
				"rate_limits": map[string]any{
					"five_hour": map[string]any{
						"used_percentage": float64(42),
						"resets_at":       float64(1779255527),
					},
					"seven_day": map[string]any{
						"used_percentage": float64(9),
						"resets_at":       float64(1779842327),
					},
					"sampledAt": "1779237696538",
				},
			},
		},
		Run: toolruntime.RunState{},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	fiveHourItem, ok := snapshot.FindItem("rate_limit_5h")
	if !ok {
		t.Fatal("rate_limit_5h item not found")
	}
	if fiveHourItem.Percent != 42 {
		t.Fatalf("rate_limit_5h percent=%v want 42", fiveHourItem.Percent)
	}
	sevenDayItem, ok := snapshot.FindItem("rate_limit_7d")
	if !ok {
		t.Fatal("rate_limit_7d item not found")
	}
	if sevenDayItem.Percent != 9 {
		t.Fatalf("rate_limit_7d percent=%v want 9", sevenDayItem.Percent)
	}
}

func TestClaudePackageBuild_OmitsRateLimitItemsWhenZero(t *testing.T) {
	snapshot, err := claude.New().Build(context.Background(), core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-claude-rl-zero"},
		Agent: core.AgentInfo{
			AgentID:      9002,
			OwnerID:      1001,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypeClaude,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"session_control", "set_mode", "get_rate_limits"},
		},
		Binding: core.BindingInfo{
			Cwd: "/workspace/project",
			Meta: map[string]any{
				"mode_id": "full_auto",
				"rate_limits": map[string]any{
					"fiveHour": map[string]any{
						"usedPercentage": float64(0),
						"resetsAt":       float64(1779255527),
					},
					"sevenDay": map[string]any{
						"usedPercentage": float64(12),
						"resetsAt":       float64(1779842327),
					},
					"sampledAt": float64(1779237696538),
				},
			},
		},
		Run: toolruntime.RunState{},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if _, ok := snapshot.FindItem("rate_limit_5h"); ok {
		t.Fatal("rate_limit_5h should be omitted when percent is 0")
	}
	sevenDayItem, ok := snapshot.FindItem("rate_limit_7d")
	if !ok {
		t.Fatal("rate_limit_7d item not found")
	}
	if sevenDayItem.Percent != 12 {
		t.Fatalf("rate_limit_7d percent=%v want 12", sevenDayItem.Percent)
	}
}

func TestClaudePackageBuild_IncludesProgressItemsWhenRateLimitDataMixedType(t *testing.T) {
	snapshot, err := claude.New().Build(context.Background(), core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-claude"},
		Agent: core.AgentInfo{
			AgentID:      9002,
			OwnerID:      1001,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypeClaude,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"session_control", "set_mode", "get_rate_limits"},
		},
		Binding: core.BindingInfo{
			Cwd: "/workspace/project",
			Meta: map[string]any{
				"mode_id": "full_auto",
				"rate_limits": map[string]any{
					"fiveHour": map[string]any{
						"usedPercentage": "57.5",
						"resetsAt":       "1779255527",
					},
					"sevenDay": map[string]any{
						"usedPercentage": float64(12),
						"resetsAt":       float64(1779842327),
					},
					"sampledAt": float64(1779237696538),
				},
			},
		},
		Run: toolruntime.RunState{},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	fiveHourItem, ok := snapshot.FindItem("rate_limit_5h")
	if !ok {
		t.Fatal("rate_limit_5h item not found")
	}
	if fiveHourItem.Percent != 57.5 {
		t.Fatalf("rate_limit_5h percent=%v want 57.5", fiveHourItem.Percent)
	}
	sevenDayItem, ok := snapshot.FindItem("rate_limit_7d")
	if !ok {
		t.Fatal("rate_limit_7d item not found")
	}
	if sevenDayItem.Percent != 12 {
		t.Fatalf("rate_limit_7d percent=%v want 12", sevenDayItem.Percent)
	}
}

func TestCursorPackageBuild_IncludesMonthlyAndApiProgressItems(t *testing.T) {
	pkg := cursor.New()
	snapshot, err := pkg.Build(context.Background(), core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-cursor-rl"},
		Agent: core.AgentInfo{
			AgentID:      9001,
			OwnerID:      1001,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypeCursor,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"session_control", "set_model", "set_mode", "get_rate_limits"},
		},
		Binding: core.BindingInfo{
			Cwd: "/workspace/project",
			Meta: map[string]any{
				"model_id": "auto",
				"rate_limits": map[string]any{
					"sampledAt": float64(1_700_000_000_000),
					"fiveHour": map[string]any{
						"usedPercentage": 18.4,
						"resetsAt":       float64(1_787_455_773),
					},
					"sevenDay": map[string]any{
						"usedPercentage": 33.2,
						"resetsAt":       float64(1_787_455_773),
					},
				},
			},
		},
		Run: toolruntime.RunState{},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	monthly, ok := snapshot.FindItem("rate_limit_monthly")
	if !ok {
		t.Fatal("rate_limit_monthly item not found")
	}
	if monthly.CenterText != "M" {
		t.Fatalf("monthly centerText=%q want M", monthly.CenterText)
	}
	if monthly.Percent != 18.4 {
		t.Fatalf("monthly percent=%v want 18.4", monthly.Percent)
	}
	if monthly.LocalAction != "get_rate_limits" {
		t.Fatalf("monthly localAction=%q want get_rate_limits", monthly.LocalAction)
	}
	apiItem, ok := snapshot.FindItem("rate_limit_api")
	if !ok {
		t.Fatal("rate_limit_api item not found")
	}
	if apiItem.CenterText != "API" {
		t.Fatalf("api centerText=%q want API", apiItem.CenterText)
	}
	if apiItem.Percent != 33.2 {
		t.Fatalf("api percent=%v want 33.2", apiItem.Percent)
	}
}

func TestCursorPackageBuild_OmitsApiProgressWhenZero(t *testing.T) {
	pkg := cursor.New()
	snapshot, err := pkg.Build(context.Background(), core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-cursor-rl-zero"},
		Agent: core.AgentInfo{
			AgentID:      9001,
			OwnerID:      1001,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypeCursor,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"session_control", "set_model", "set_mode", "get_rate_limits"},
		},
		Binding: core.BindingInfo{
			Cwd: "/workspace/project",
			Meta: map[string]any{
				"model_id": "auto",
				"rate_limits": map[string]any{
					"sampledAt": float64(1_700_000_000_000),
					"fiveHour": map[string]any{
						"usedPercentage": 18.4,
						"resetsAt":       float64(1_787_455_773),
					},
					"sevenDay": map[string]any{
						"usedPercentage": 0,
						"resetsAt":       float64(1_787_455_773),
					},
				},
			},
		},
		Run: toolruntime.RunState{},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if _, ok := snapshot.FindItem("rate_limit_monthly"); !ok {
		t.Fatal("rate_limit_monthly item not found")
	}
	if _, ok := snapshot.FindItem("rate_limit_api"); ok {
		t.Fatal("rate_limit_api item should be omitted when percent is 0")
	}
}

func TestCursorPackageBuild_OmitsMonthlyProgressWhenZero(t *testing.T) {
	pkg := cursor.New()
	snapshot, err := pkg.Build(context.Background(), core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-cursor-rl-monthly-zero"},
		Agent: core.AgentInfo{
			AgentID:      9001,
			OwnerID:      1001,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypeCursor,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"session_control", "set_model", "set_mode", "get_rate_limits"},
		},
		Binding: core.BindingInfo{
			Cwd: "/workspace/project",
			Meta: map[string]any{
				"model_id": "auto",
				"rate_limits": map[string]any{
					"sampledAt": float64(1_700_000_000_000),
					"fiveHour": map[string]any{
						"usedPercentage": 0,
						"resetsAt":       float64(1_787_455_773),
					},
					"sevenDay": map[string]any{
						"usedPercentage": 12.5,
						"resetsAt":       float64(1_787_455_773),
					},
				},
			},
		},
		Run: toolruntime.RunState{},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if _, ok := snapshot.FindItem("rate_limit_monthly"); ok {
		t.Fatal("rate_limit_monthly item should be omitted when percent is 0")
	}
	if _, ok := snapshot.FindItem("rate_limit_api"); !ok {
		t.Fatal("rate_limit_api item not found")
	}
}

func TestCursorPackageBuild_OmitsAllRateLimitsWhenZero(t *testing.T) {
	pkg := cursor.New()
	snapshot, err := pkg.Build(context.Background(), core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-cursor-rl-all-zero"},
		Agent: core.AgentInfo{
			AgentID:      9001,
			OwnerID:      1001,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypeCursor,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"session_control", "set_model", "set_mode", "get_rate_limits"},
		},
		Binding: core.BindingInfo{
			Cwd: "/workspace/project",
			Meta: map[string]any{
				"model_id": "auto",
				"rate_limits": map[string]any{
					"sampledAt": float64(1_700_000_000_000),
					"fiveHour": map[string]any{
						"usedPercentage": 0,
						"resetsAt":       float64(1_787_455_773),
					},
					"sevenDay": map[string]any{
						"usedPercentage": 0,
						"resetsAt":       float64(1_787_455_773),
					},
				},
			},
		},
		Run: toolruntime.RunState{},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if _, ok := snapshot.FindItem("rate_limit_monthly"); ok {
		t.Fatal("rate_limit_monthly should be omitted when all percents are 0")
	}
	if _, ok := snapshot.FindItem("rate_limit_api"); ok {
		t.Fatal("rate_limit_api should be omitted when all percents are 0")
	}
}

func TestCursorPackageBuild_ContextProgressAndCompact(t *testing.T) {
	pkg := cursor.New()
	build := func(localActions []string, meta map[string]any) toolprotocol.Snapshot {
		t.Helper()
		snapshot, err := pkg.Build(context.Background(), core.BuildInput{
			OwnerID: 1001,
			Session: core.SessionInfo{SessionID: "sess-cursor-ctx"},
			Agent: core.AgentInfo{
				AgentID:      9001,
				OwnerID:      1001,
				ProviderType: model.AgentProviderAPI,
				ClientType:   model.AgentClientTypeCursor,
			},
			Runtime: toolruntime.Profile{
				Online:       true,
				LocalActions: localActions,
			},
			Binding: core.BindingInfo{
				Cwd:  "/workspace/project",
				Meta: meta,
			},
			Run: toolruntime.RunState{},
		})
		if err != nil {
			t.Fatalf("Build() error = %v", err)
		}
		return snapshot
	}

	t.Run("shows progress from context_window meta", func(t *testing.T) {
		snapshot := build(
			[]string{"session_control", "set_model", "set_mode", "thread_compact", "get_rate_limits"},
			map[string]any{
				"context_window": map[string]any{
					"usedPercentage":      float64(42.5),
					"remainingPercentage": float64(57.5),
				},
			},
		)
		item, ok := snapshot.FindItem("thread_compact")
		if !ok {
			t.Fatal("thread_compact item not found")
		}
		if item.Kind != toolprotocol.ItemKindProgress {
			t.Fatalf("kind=%q want progress", item.Kind)
		}
		if item.Percent != 42.5 {
			t.Fatalf("percent=%v want 42.5", item.Percent)
		}
		if item.Disabled {
			t.Fatal("item should be enabled when thread_compact is declared")
		}
		if item.LocalAction != "thread_compact" {
			t.Fatalf("localAction=%q want thread_compact", item.LocalAction)
		}
	})

	t.Run("warning variant when usedPercentage >= 80", func(t *testing.T) {
		snapshot := build(
			[]string{"session_control", "thread_compact"},
			map[string]any{"context_window": map[string]any{"usedPercentage": 85.0}},
		)
		item, ok := snapshot.FindItem("thread_compact")
		if !ok {
			t.Fatal("thread_compact item not found")
		}
		if item.Variant != "warning" {
			t.Fatalf("variant=%q want warning", item.Variant)
		}
	})

	t.Run("disabled when thread_compact not declared", func(t *testing.T) {
		snapshot := build(
			[]string{"session_control", "set_model"},
			map[string]any{"context_window": map[string]any{"usedPercentage": 50.0}},
		)
		item, ok := snapshot.FindItem("thread_compact")
		if !ok {
			t.Fatal("thread_compact item not found")
		}
		if !item.Disabled {
			t.Fatal("item should be disabled when thread_compact not declared")
		}
	})
}

func TestCursorPackageHandle_ThreadCompact(t *testing.T) {
	pkg := cursor.New()
	executor := &packageTestExecutor{}
	result, err := pkg.HandleAction(context.Background(), core.ActionInput{
		BuildInput: core.BuildInput{
			OwnerID: 1001,
			Session: core.SessionInfo{SessionID: "sess-cursor-compact"},
			Agent: core.AgentInfo{
				AgentID:      9001,
				OwnerID:      1001,
				ProviderType: model.AgentProviderAPI,
				ClientType:   model.AgentClientTypeCursor,
			},
			Runtime: toolruntime.Profile{
				Online:       true,
				LocalActions: []string{"thread_compact"},
			},
			Binding: core.BindingInfo{Cwd: "/workspace/project"},
			Run:     toolruntime.RunState{},
		},
		Request: toolprotocol.ActionRequest{
			SessionID: "sess-cursor-compact",
			ActionID:  "thread_compact",
			Event:     "click",
		},
		Executor: executor,
	})
	if err != nil {
		t.Fatalf("HandleAction() error = %v", err)
	}
	if result.Outcome != toolprotocol.ActionOutcomeAcceptedNoStateChange {
		t.Fatalf("outcome=%v want accepted_no_state_change", result.Outcome)
	}
	if len(executor.localActions) != 1 || executor.localActions[0].ActionType != "thread_compact" {
		t.Fatalf("localActions=%+v", executor.localActions)
	}
}

func TestKiroPackageBuild_StopOutputVisibilityAcrossRunStates(t *testing.T) {
	pkg := kiro.New()
	buildBase := func(state string, canStop bool) core.BuildInput {
		return core.BuildInput{
			OwnerID: 1001,
			Session: core.SessionInfo{SessionID: "sess-kiro-stop"},
			Agent: core.AgentInfo{
				AgentID:      9001,
				OwnerID:      1001,
				ProviderType: model.AgentProviderAPI,
				ClientType:   model.AgentClientTypeKiro,
			},
			Runtime: toolruntime.Profile{
				Online:       true,
				LocalActions: []string{"session_control", "set_model", "set_mode"},
			},
			Binding: core.BindingInfo{
				BindingID: "bind-kiro-stop",
				Cwd:       "/home/user/project",
			},
			Run: toolruntime.RunState{
				HasActiveRun: true,
				RunID:        "run-kiro-1",
				State:        state,
				CanStop:      canStop,
			},
		}
	}

	assertStop := func(t *testing.T, state string, canStop bool, shouldShow bool, shouldDisable bool, shouldLoad bool) {
		t.Helper()
		snapshot, err := pkg.Build(context.Background(), buildBase(state, canStop))
		if err != nil {
			t.Fatalf("Build(%s) error = %v", state, err)
		}
		item, ok := snapshot.FindItem("stop_output")
		if !shouldShow {
			if ok {
				t.Fatalf("stop_output should be hidden for state=%s can_stop=%t", state, canStop)
			}
			return
		}
		if !ok {
			t.Fatalf("stop_output should be visible for state=%s can_stop=%t", state, canStop)
		}
		if item.Disabled != shouldDisable {
			t.Fatalf("stop_output disabled=%t want=%t state=%s", item.Disabled, shouldDisable, state)
		}
		if item.Loading != shouldLoad {
			t.Fatalf("stop_output loading=%t want=%t state=%s", item.Loading, shouldLoad, state)
		}
	}

	assertStop(t, "queued", true, true, false, false)
	assertStop(t, "streaming", true, true, false, false)
	assertStop(t, "stopping", false, true, true, true)
	assertStop(t, "completed", false, false, false, false)
}

func TestKiroPackageBuild_ContextWindowProgress(t *testing.T) {
	pkg := kiro.New()

	buildInput := func(localActions []string, meta map[string]any) core.BuildInput {
		return core.BuildInput{
			OwnerID: 1001,
			Session: core.SessionInfo{SessionID: "sess-kiro-ctx"},
			Agent: core.AgentInfo{
				AgentID:    9001,
				OwnerID:    1001,
				ClientType: model.AgentClientTypeKiro,
			},
			Runtime: toolruntime.Profile{
				Online:       true,
				LocalActions: localActions,
			},
			Binding: core.BindingInfo{
				BindingID: "bind-kiro-ctx",
				Cwd:       "/home/user/project",
				Meta:      meta,
			},
		}
	}

	t.Run("shows progress item with usedPercentage from context_window meta", func(t *testing.T) {
		in := buildInput(
			[]string{"session_control", "set_model", "set_mode", "thread_compact", "get_rate_limits"},
			map[string]any{
				"context_window": map[string]any{"usedPercentage": 42.5},
			},
		)
		snapshot, err := pkg.Build(context.Background(), in)
		if err != nil {
			t.Fatalf("Build error = %v", err)
		}
		item, ok := snapshot.FindItem("thread_compact")
		if !ok {
			t.Fatal("thread_compact item not found")
		}
		if item.Kind != toolprotocol.ItemKindProgress {
			t.Fatalf("kind=%q want %q", item.Kind, toolprotocol.ItemKindProgress)
		}
		if item.Percent != 42.5 {
			t.Fatalf("percent=%v want 42.5", item.Percent)
		}
		if item.Variant != "secondary" {
			t.Fatalf("variant=%q want secondary", item.Variant)
		}
		if item.Disabled {
			t.Fatal("item should be enabled when thread_compact is declared")
		}
	})

	t.Run("warning variant when usedPercentage >= 80", func(t *testing.T) {
		in := buildInput(
			[]string{"session_control", "set_model", "set_mode", "thread_compact"},
			map[string]any{"context_window": map[string]any{"usedPercentage": 85.0}},
		)
		snapshot, _ := pkg.Build(context.Background(), in)
		item, ok := snapshot.FindItem("thread_compact")
		if !ok {
			t.Fatal("thread_compact item not found")
		}
		if item.Variant != "warning" {
			t.Fatalf("variant=%q want warning", item.Variant)
		}
	})

	t.Run("disabled when thread_compact not declared", func(t *testing.T) {
		in := buildInput(
			[]string{"session_control", "set_model", "set_mode"},
			map[string]any{"context_window": map[string]any{"usedPercentage": 50.0}},
		)
		snapshot, _ := pkg.Build(context.Background(), in)
		item, ok := snapshot.FindItem("thread_compact")
		if !ok {
			t.Fatal("thread_compact item not found")
		}
		if !item.Disabled {
			t.Fatal("item should be disabled when thread_compact not declared")
		}
	})

	t.Run("zero percent when no context_window meta", func(t *testing.T) {
		in := buildInput(
			[]string{"session_control", "set_model", "set_mode", "thread_compact"},
			nil,
		)
		snapshot, _ := pkg.Build(context.Background(), in)
		item, ok := snapshot.FindItem("thread_compact")
		if !ok {
			t.Fatal("thread_compact item not found")
		}
		if item.Percent != 0 {
			t.Fatalf("percent=%v want 0", item.Percent)
		}
	})
}
