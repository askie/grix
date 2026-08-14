package agenttoolbar_test

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/agenttoolbar/agents/codex"
	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	"github.com/askie/grix/backend/internal/model"
)

func codexServiceTierBuildInput(meta map[string]any, localActions []string) core.BuildInput {
	return core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-tier"},
		Agent: core.AgentInfo{
			AgentID:      9001,
			OwnerID:      1001,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypeCodex,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: localActions,
		},
		Binding: core.BindingInfo{
			BindingID: "bind-tier",
			Cwd:       "/workspace/project",
			Meta:      meta,
		},
	}
}

func findToolbarItem(items []toolprotocol.Item, itemID string) *toolprotocol.Item {
	for i := range items {
		if items[i].ItemID == itemID {
			return &items[i]
		}
	}
	return nil
}

func TestCodexServiceTierSelectorBuild(t *testing.T) {
	meta := map[string]any{
		"model_id":     "gpt-5.6-sol",
		"service_tier": "priority",
		"available_service_tiers": []any{
			map[string]any{"id": "priority", "displayName": "Fast", "description": "1.5x speed, increased usage"},
		},
	}
	snapshot, err := codex.New().Build(context.Background(), codexServiceTierBuildInput(meta, []string{"set_service_tier"}))
	if err != nil {
		t.Fatalf("Build error = %v", err)
	}
	item := findToolbarItem(snapshot.Items, "select_service_tier")
	if item == nil {
		t.Fatalf("select_service_tier item missing")
	}
	if item.Disabled {
		t.Fatalf("select_service_tier should be enabled")
	}
	// 档位名以插件下发的 displayName 为准（这里是 "Fast"），本地不写死倍率——
	// 倍率是 OpenAI 的策略，写死一旦对方调整我们就在发假信息。
	if item.Label != "Fast" {
		t.Fatalf("label = %q, want Fast（插件下发的 displayName 优先）", item.Label)
	}
	if len(item.Options) != 2 {
		t.Fatalf("options = %d, want 2 (标准 + 快速)", len(item.Options))
	}
	if item.Options[0].OptionID != "default" || item.Options[0].Label != "标准" {
		t.Fatalf("first option = %+v, want default/标准", item.Options[0])
	}
	if item.Options[1].OptionID != "priority" || item.Options[1].Label != "Fast" {
		t.Fatalf("second option = %+v, want priority/Fast（插件下发的 displayName 优先）", item.Options[1])
	}
}

func TestCodexServiceTierSelectorHiddenWithoutTiers(t *testing.T) {
	meta := map[string]any{
		"model_id": "gpt-5.4-mini",
		// 模型未广告速度档（或旧版连接器未下发字段）
		"available_service_tiers": []any{},
	}
	snapshot, err := codex.New().Build(context.Background(), codexServiceTierBuildInput(meta, []string{"set_service_tier"}))
	if err != nil {
		t.Fatalf("Build error = %v", err)
	}
	if item := findToolbarItem(snapshot.Items, "select_service_tier"); item != nil {
		t.Fatalf("select_service_tier should be hidden when no tiers advertised")
	}
}

func TestCodexServiceTierSelectorDisabledOnOldPlugin(t *testing.T) {
	meta := map[string]any{
		"model_id": "gpt-5.6-sol",
		"available_service_tiers": []any{
			map[string]any{"id": "priority", "displayName": "Fast"},
		},
	}
	// 连接器未声明 set_service_tier（旧版插件）
	snapshot, err := codex.New().Build(context.Background(), codexServiceTierBuildInput(meta, []string{"set_reasoning_effort"}))
	if err != nil {
		t.Fatalf("Build error = %v", err)
	}
	item := findToolbarItem(snapshot.Items, "select_service_tier")
	if item == nil {
		t.Fatalf("select_service_tier item missing")
	}
	if !item.Disabled {
		t.Fatalf("select_service_tier should be disabled when plugin lacks set_service_tier")
	}
}

func TestCodexServiceTierDefaultLabelWhenUnset(t *testing.T) {
	meta := map[string]any{
		"model_id": "gpt-5.6-sol",
		"available_service_tiers": []any{
			map[string]any{"id": "priority", "displayName": "Fast"},
		},
	}
	snapshot, err := codex.New().Build(context.Background(), codexServiceTierBuildInput(meta, []string{"set_service_tier"}))
	if err != nil {
		t.Fatalf("Build error = %v", err)
	}
	item := findToolbarItem(snapshot.Items, "select_service_tier")
	if item == nil {
		t.Fatalf("select_service_tier item missing")
	}
	if item.Label != "标准" {
		t.Fatalf("label = %q, want 标准 (service_tier 未设置)", item.Label)
	}
}

func TestCodexServiceTierHandleAction(t *testing.T) {
	meta := map[string]any{
		"model_id": "gpt-5.6-sol",
		"available_service_tiers": []any{
			map[string]any{"id": "priority", "displayName": "Fast"},
		},
	}
	buildInput := codexServiceTierBuildInput(meta, []string{"set_service_tier"})
	pkg := codex.New()

	for _, optionID := range []string{"priority", "default"} {
		executor := &packageTestExecutor{}
		result, err := pkg.HandleAction(context.Background(), core.ActionInput{
			BuildInput: buildInput,
			Request:    toolprotocol.ActionRequest{SessionID: "sess-tier", ActionID: "select_service_tier", Event: "select", OptionID: optionID},
			Executor:   executor,
		})
		if err != nil {
			t.Fatalf("HandleAction(%s) error = %v", optionID, err)
		}
		if result.Outcome != toolprotocol.ActionOutcomeAcceptedNoStateChange {
			t.Fatalf("HandleAction(%s) outcome = %q", optionID, result.Outcome)
		}
		if len(executor.localActions) != 1 {
			t.Fatalf("local actions = %d, want 1", len(executor.localActions))
		}
		req := executor.localActions[0]
		if req.ActionType != "set_service_tier" {
			t.Fatalf("action_type = %q, want set_service_tier", req.ActionType)
		}
		if got := req.Params["service_tier"]; got != optionID {
			t.Fatalf("service_tier = %v, want %s", got, optionID)
		}
		// priority 用插件下发的 displayName；default 是我们本地补的选项，插件不下发，兜底中文。
		wantLabel := map[string]string{"priority": "Fast", "default": "标准"}[optionID]
		if got := req.Params["display_label"]; got != wantLabel {
			t.Fatalf("display_label = %v, want %s", got, wantLabel)
		}
	}
}

func TestCodexServiceTierOptionsDedupeAndDisplayName(t *testing.T) {
	meta := map[string]any{
		"model_id": "gpt-5.6-sol",
		"available_service_tiers": []any{
			map[string]any{"id": "priority", "displayName": "Fast"},
			map[string]any{"id": "Priority", "displayName": "Fast dup"},
			map[string]any{"id": "flex", "displayName": "Flex"},
		},
	}
	snapshot, err := codex.New().Build(context.Background(), codexServiceTierBuildInput(meta, []string{"set_service_tier"}))
	if err != nil {
		t.Fatalf("Build error = %v", err)
	}
	item := findToolbarItem(snapshot.Items, "select_service_tier")
	if item == nil {
		t.Fatalf("select_service_tier item missing")
	}
	// default + priority(去重) + flex
	if len(item.Options) != 3 {
		t.Fatalf("options = %d, want 3", len(item.Options))
	}
	// 未知 tier id 用连接器下发的 displayName 作为展示文案
	if item.Options[2].OptionID != "flex" || item.Options[2].Label != "Flex" {
		t.Fatalf("flex option = %+v, want flex/Flex", item.Options[2])
	}
}

func TestCodexServiceTierHandleActionRejectedOnOldPlugin(t *testing.T) {
	meta := map[string]any{
		"model_id": "gpt-5.6-sol",
		"available_service_tiers": []any{
			map[string]any{"id": "priority", "displayName": "Fast"},
		},
	}
	buildInput := codexServiceTierBuildInput(meta, []string{"set_reasoning_effort"})
	executor := &packageTestExecutor{}
	result, err := codex.New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: buildInput,
		Request:    toolprotocol.ActionRequest{SessionID: "sess-tier", ActionID: "select_service_tier", Event: "select", OptionID: "priority"},
		Executor:   executor,
	})
	if err != nil {
		t.Fatalf("HandleAction error = %v", err)
	}
	if result.Outcome != toolprotocol.ActionOutcomeRejected {
		t.Fatalf("outcome = %q, want rejected", result.Outcome)
	}
	if len(executor.localActions) != 0 {
		t.Fatalf("local actions = %d, want 0", len(executor.localActions))
	}
}
