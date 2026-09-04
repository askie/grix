package agenttoolbar_test

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/agenttoolbar/agents/acp"
	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	"github.com/askie/grix/backend/internal/model"
)

func acpBuildInput(meta map[string]any, localActions []string) core.BuildInput {
	return core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-acp"},
		Agent: core.AgentInfo{
			AgentID:      9101,
			OwnerID:      1001,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypeACP,
		},
		Runtime: toolruntime.Profile{Online: true, LocalActions: localActions},
		Binding: core.BindingInfo{
			BindingID: "bind-acp",
			Cwd:       "/workspace/project",
			Meta:      meta,
		},
	}
}

// 通用 ACP 工具栏只在连接器上报清单后才渲染模型/模式选择器，
// 不带任何厂商专属项。
func TestACPPackageBuild_SelectorsFollowReportedMeta(t *testing.T) {
	actions := []string{"session_control", "set_model", "set_mode", "get_session_usage"}

	withLists, err := acp.New().Build(context.Background(), acpBuildInput(map[string]any{
		"model_id": "m-a",
		"mode_id":  "plan",
		"available_models": []any{
			map[string]any{"id": "m-a", "displayName": "Model A"},
		},
		"available_modes": []any{
			map[string]any{"id": "plan", "displayName": "计划"},
		},
	}, actions))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !withLists.Visible {
		t.Fatal("snapshot invisible for a bound ACP session")
	}
	for _, itemID := range []string{"session_control", "select_model", "select_mode"} {
		item, ok := withLists.FindItem(itemID)
		if !ok {
			t.Fatalf("%s item not found", itemID)
		}
		if item.Disabled {
			t.Fatalf("%s disabled, tooltip=%q", itemID, item.Tooltip)
		}
	}
	// 通用接入不挂厂商专属入口。
	for _, itemID := range []string{"slash_commands", "provider_quota", "context_window"} {
		if _, ok := withLists.FindItem(itemID); ok {
			t.Fatalf("%s must not be rendered for the generic ACP toolbar", itemID)
		}
	}

	noLists, err := acp.New().Build(context.Background(), acpBuildInput(nil, actions))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if _, ok := noLists.FindItem("select_model"); ok {
		t.Fatal("select_model rendered without any reported model")
	}
	if _, ok := noLists.FindItem("select_mode"); ok {
		t.Fatal("select_mode rendered without any reported mode")
	}
	if _, ok := noLists.FindItem("session_control"); !ok {
		t.Fatal("session_control must stay available without meta")
	}
}

// 未绑定会话时不显示工具栏。
func TestACPPackageBuild_HiddenWithoutBinding(t *testing.T) {
	in := acpBuildInput(nil, []string{"session_control"})
	in.Binding = core.BindingInfo{}
	snapshot, err := acp.New().Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if snapshot.Visible {
		t.Fatal("snapshot visible without a session binding")
	}
}

func TestACPPackageHandleAction_SessionControlDispatches(t *testing.T) {
	executor := &packageTestExecutor{}
	result, err := acp.New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: acpBuildInput(nil, []string{"session_control"}),
		Request:    toolprotocol.ActionRequest{ActionID: "session_control", OptionID: "status"},
		Executor:   executor,
	})
	if err != nil {
		t.Fatalf("HandleAction() error = %v", err)
	}
	if result.Outcome != toolprotocol.ActionOutcomeAcceptedNoStateChange {
		t.Fatalf("outcome = %q, want accepted_no_state_change", result.Outcome)
	}
	if len(executor.localActions) != 1 {
		t.Fatalf("local actions = %d, want 1", len(executor.localActions))
	}
	if got := executor.localActions[0].ActionType; got != "session_control" {
		t.Fatalf("action_type = %q, want session_control", got)
	}
	if got := executor.localActions[0].Params["verb"]; got != "status" {
		t.Fatalf("verb = %v, want status", got)
	}
}

// 连接器没声明的 local action 必须当场拒绝，不下发。
func TestACPPackageHandleAction_RejectsUndeclaredLocalAction(t *testing.T) {
	executor := &packageTestExecutor{}
	result, err := acp.New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: acpBuildInput(nil, []string{"session_control"}),
		Request:    toolprotocol.ActionRequest{ActionID: "select_model", OptionID: "m-a"},
		Executor:   executor,
	})
	if err != nil {
		t.Fatalf("HandleAction() error = %v", err)
	}
	if result.Outcome != toolprotocol.ActionOutcomeRejected || result.Code != "local_action_unavailable" {
		t.Fatalf("outcome=%q code=%q, want rejected/local_action_unavailable", result.Outcome, result.Code)
	}
	if len(executor.localActions) != 0 {
		t.Fatalf("local actions = %d, want 0", len(executor.localActions))
	}
}
