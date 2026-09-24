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
// 不带任何厂商专属项，也不带会话控制（目录由配置静态带入，用户不可改）。
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
		t.Fatal("snapshot invisible although selectors were reported")
	}
	for _, itemID := range []string{"select_model", "select_mode"} {
		item, ok := withLists.FindItem(itemID)
		if !ok {
			t.Fatalf("%s item not found", itemID)
		}
		if item.Disabled {
			t.Fatalf("%s disabled, tooltip=%q", itemID, item.Tooltip)
		}
	}
	// 通用接入不挂厂商专属入口，也不挂目录绑定相关入口。
	for _, itemID := range []string{"slash_commands", "provider_quota", "context_window", "session_control"} {
		if _, ok := withLists.FindItem(itemID); ok {
			t.Fatalf("%s must not be rendered for the generic ACP toolbar", itemID)
		}
	}
	// 会话列表要扫描已知 CLI 的历史目录布局，未知 CLI 扫不出来，不前置该按钮。
	if !withLists.OmitListSessionsButton {
		t.Fatal("OmitListSessionsButton must be set for the generic ACP toolbar")
	}
}

// 空闲（无运行中任务、连接器未上报清单）时工具栏整体不可见。
func TestACPPackageBuild_HiddenWhenIdle(t *testing.T) {
	snapshot, err := acp.New().Build(context.Background(), acpBuildInput(nil, []string{"session_control"}))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if snapshot.Visible {
		t.Fatalf("snapshot visible while idle, items=%d", len(snapshot.Items))
	}
}

// 绑定信息为空不再影响工具栏：目录由连接器静态带入，后端不拿它当门禁。
// 跑任务时仍要能停。
func TestACPPackageBuild_StopVisibleWithoutBinding(t *testing.T) {
	in := acpBuildInput(nil, []string{"session_control"})
	in.Binding = core.BindingInfo{}
	in.Run = toolruntime.RunState{HasActiveRun: true, CanStop: true, RunID: "run-1"}
	snapshot, err := acp.New().Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !snapshot.Visible {
		t.Fatal("snapshot invisible although a run is stoppable")
	}
	if _, ok := snapshot.FindItem("stop_output"); !ok {
		t.Fatal("stop_output item not found")
	}
}

// 会话控制入口已移除：即便前端拿着旧快照点过来也必须当场拒绝，不下发 local action。
func TestACPPackageHandleAction_RejectsSessionControl(t *testing.T) {
	executor := &packageTestExecutor{}
	for _, actionID := range []string{"session_control", "get_session_usage"} {
		result, err := acp.New().HandleAction(context.Background(), core.ActionInput{
			BuildInput: acpBuildInput(nil, []string{"session_control", "get_session_usage"}),
			Request:    toolprotocol.ActionRequest{ActionID: actionID, OptionID: "status"},
			Executor:   executor,
		})
		if err != nil {
			t.Fatalf("HandleAction(%s) error = %v", actionID, err)
		}
		if result.Outcome != toolprotocol.ActionOutcomeRejected || result.Code != "invalid_action" {
			t.Fatalf("%s outcome=%q code=%q, want rejected/invalid_action", actionID, result.Outcome, result.Code)
		}
	}
	if len(executor.localActions) != 0 {
		t.Fatalf("local actions = %d, want 0", len(executor.localActions))
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
