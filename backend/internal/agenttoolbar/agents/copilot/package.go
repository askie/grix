package copilot

import (
	"context"
	"strings"

	"github.com/askie/grix/backend/internal/agenttoolbar/agents/shared"
	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	"github.com/askie/grix/backend/internal/model"
)

type Package struct{}

func New() *Package            { return &Package{} }
func (p *Package) Key() string { return model.AgentClientTypeCopilot }
func (p *Package) Match(ctx core.MatchContext) bool {
	return ctx.Agent.ClientType == model.AgentClientTypeCopilot
}

var copilotModeOptions = []toolprotocol.Option{
	{OptionID: "interactive", Label: "交互模式"},
	{OptionID: "plan", Label: "规划模式"},
	{OptionID: "autopilot", Label: "自动驾驶"},
}

func (p *Package) Build(_ context.Context, in core.BuildInput) (toolprotocol.Snapshot, error) {
	if !hasCopilotSessionBinding(in.Binding) {
		return toolprotocol.Snapshot{Visible: false, Items: []toolprotocol.Item{}}, nil
	}

	runState := strings.TrimSpace(in.Run.State)
	showStopOutput := in.Run.HasActiveRun && (in.Run.CanStop || runState == "stopping")

	sessionDisabled := !in.Runtime.Online || !in.Runtime.HasLocalAction("session_control")
	sessionTooltip := ""
	switch {
	case !in.Runtime.Online:
		sessionTooltip = "Copilot 当前离线"
	case !in.Runtime.HasLocalAction("session_control"):
		sessionTooltip = "当前插件未声明 session_control"
	case strings.TrimSpace(in.Binding.Cwd) != "":
		sessionTooltip = in.Binding.Cwd
	}

	badge := ""
	if cwd := strings.TrimSpace(in.Binding.Cwd); cwd != "" {
		badge = shared.PathBase(cwd)
	} else if worker := strings.TrimSpace(in.Binding.WorkerStatus); worker == "session_expired" {
		badge = "会话已过期"
	} else if worker != "" {
		badge = worker
	} else if in.Runtime.Online {
		badge = "在线"
	} else {
		badge = "离线"
	}

	currentModeID := bindingMetaString(in.Binding.Meta, "mode_id")
	currentModelID := bindingMetaString(in.Binding.Meta, "model_id")

	modeDisabled := !in.Runtime.Online || !in.Runtime.HasLocalAction("set_mode")
	modelOptions := buildCopilotModelOptions(in.Binding.Meta)
	currentModelLabel := resolveCopilotModelLabel(currentModelID, modelOptions)

	modelDisabled := !in.Runtime.Online || !in.Runtime.HasLocalAction("set_model") || len(modelOptions) == 0
	usageDisabled := !in.Runtime.Online || !in.Runtime.HasLocalAction("get_session_usage")

	items := []toolprotocol.Item{}

	if showStopOutput {
		items = append(items, toolprotocol.Item{
			ItemID:   "stop_output",
			GroupID:  "run_control",
			Kind:     toolprotocol.ItemKindButton,
			ActionID: "stop_output",
			Icon:     "stop",
			Variant:  "danger",
			Disabled: !in.Run.CanStop,
			Loading:  runState == "stopping",
			Selected: runState == "stopping",
		})
	}

	items = append(items, toolprotocol.Item{
		ItemID:      "session_control",
		GroupID:     "session_control",
		Kind:        toolprotocol.ItemKindSelect,
		ActionID:    "session_control",
		Icon:        "status",
		Variant:     "secondary",
		Disabled:    sessionDisabled,
		Tooltip:     sessionTooltip,
		BadgeText:   badge,
		Placeholder: "选择 Copilot 会话操作",
		Options: []toolprotocol.Option{
			{OptionID: "status", Label: "查看状态"},
			{OptionID: "restart", Label: "重启会话"},
			{OptionID: "unbind", Label: "解绑"},
			{OptionID: "usage", Label: "查看用量", Disabled: usageDisabled},
		},
	})

	pq := shared.ParseProviderQuota(in.Binding.Meta)
	if quotaItems := shared.BuildProviderQuotaItems(
		pq,
		pq != nil && in.Runtime.HasLocalAction("get_rate_limits"),
	); len(quotaItems) > 0 {
		items = append(items, quotaItems...)
	}

	items = append(items, toolprotocol.Item{
		ItemID:      "select_model",
		GroupID:     "model_control",
		Kind:        toolprotocol.ItemKindSelect,
		ActionID:    "select_model",
		Icon:        "cpu",
		Variant:     "secondary",
		Disabled:    modelDisabled,
		Tooltip:     "切换 Copilot 模型",
		Value:       currentModelID,
		BadgeText:   currentModelLabel,
		Placeholder: "选择模型",
		Options:     toCopilotModelProtocolOptions(modelOptions),
	}, toolprotocol.Item{
		ItemID:      "select_mode",
		GroupID:     "mode_control",
		Kind:        toolprotocol.ItemKindSelect,
		ActionID:    "select_mode",
		Icon:        "shield",
		Variant:     "secondary",
		Disabled:    modeDisabled,
		Tooltip:     "切换 Copilot 模式",
		Value:       currentModeID,
		BadgeText:   resolveOptionLabel(currentModeID, copilotModeOptions),
		Placeholder: "选择模式",
		Options:     copilotModeOptions,
	})

	if len(in.Runtime.Skills) > 0 {
		items = append(items, shared.BuildSkillsItem(in.Runtime.Skills))
	}

	if item, ok := shared.BuildSlashCommandsItem("copilot"); ok {
		items = append([]toolprotocol.Item{item}, items...)
	}

	return toolprotocol.Snapshot{Visible: true, Items: items}, nil
}

func (p *Package) HandleAction(_ context.Context, in core.ActionInput) (toolprotocol.ActionResult, error) {
	switch strings.TrimSpace(in.Request.ActionID) {
	case "stop_output":
		return handleStopOutput(in)
	case "session_control":
		return handleSessionControl(in)
	case "get_session_usage":
		return handleGetSessionUsage(in)
	case "select_model":
		return handleSelectModel(in)
	case "select_mode":
		return handleSelectMode(in)
	case "get_rate_limits", "provider_quota_balance":
		return handleGetRateLimits(in)
	default:
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_action",
			Message: "工具栏动作无效",
		}, nil
	}
}

func handleStopOutput(in core.ActionInput) (toolprotocol.ActionResult, error) {
	if !in.BuildInput.Run.HasActiveRun || !in.BuildInput.Run.CanStop {
		return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeRejected, Code: "stop_unavailable", Message: "当前没有可停止的输出"}, nil
	}
	if err := in.Executor.StopOutput(context.Background(), core.StopOutputRequest{
		OwnerID: in.BuildInput.OwnerID, SessionID: in.BuildInput.Session.SessionID, RunID: in.BuildInput.Run.RunID,
		AgentID:   in.BuildInput.Agent.AgentID,
	}); err != nil {
		return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeRejected, Code: "stop_failed", Message: err.Error()}, nil
	}
	return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeAcceptedWithImmediateRefresh, Code: "accepted", Message: "已提交停止请求"}, nil
}

func handleSessionControl(in core.ActionInput) (toolprotocol.ActionResult, error) {
	verb := strings.TrimSpace(in.Request.OptionID)
	switch verb {
	case "status", "restart", "unbind":
	case "usage":
		return handleGetSessionUsage(in)
	default:
		return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeRejected, Code: "invalid_option", Message: "工具栏选项无效"}, nil
	}
	if !in.BuildInput.Runtime.Online {
		return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeRejected, Code: "agent_offline", Message: "当前 agent 不在线"}, nil
	}
	if !in.BuildInput.Runtime.HasLocalAction("session_control") {
		return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeRejected, Code: "local_action_unavailable", Message: "当前 agent 未声明 session_control"}, nil
	}
	if err := in.Executor.DispatchLocalAction(context.Background(), core.LocalActionRequest{
		OwnerID: in.BuildInput.OwnerID, AgentID: in.BuildInput.Agent.AgentID, SessionID: in.BuildInput.Session.SessionID,
		ActionType: "session_control", Params: map[string]any{"session_id": in.BuildInput.Session.SessionID, "verb": verb}, TimeoutMs: 15_000,
	}); err != nil {
		return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeRejected, Code: "dispatch_failed", Message: err.Error()}, nil
	}
	return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeAcceptedNoStateChange, Code: "accepted", Message: "已提交会话操作"}, nil
}

func handleSelectModel(in core.ActionInput) (toolprotocol.ActionResult, error) {
	modelId := strings.TrimSpace(in.Request.OptionID)
	if modelId == "" {
		return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeRejected, Code: "invalid_option", Message: "未选择模型"}, nil
	}
	if !in.BuildInput.Runtime.Online {
		return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeRejected, Code: "agent_offline", Message: "当前 agent 不在线"}, nil
	}
	if !in.BuildInput.Runtime.HasLocalAction("set_model") {
		return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeRejected, Code: "local_action_unavailable", Message: "当前 agent 未声明 set_model"}, nil
	}
	if err := in.Executor.DispatchLocalAction(context.Background(), core.LocalActionRequest{
		OwnerID: in.BuildInput.OwnerID, AgentID: in.BuildInput.Agent.AgentID, SessionID: in.BuildInput.Session.SessionID,
		ActionType: "set_model", Params: map[string]any{"session_id": in.BuildInput.Session.SessionID, "model_id": modelId, "display_label": resolveCopilotModelLabel(modelId, buildCopilotModelOptions(in.BuildInput.Binding.Meta))}, TimeoutMs: 15_000,
	}); err != nil {
		return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeRejected, Code: "dispatch_failed", Message: err.Error()}, nil
	}
	return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeAcceptedWithImmediateRefresh, Code: "accepted", Message: "已切换模型"}, nil
}

func handleSelectMode(in core.ActionInput) (toolprotocol.ActionResult, error) {
	modeId := strings.TrimSpace(in.Request.OptionID)
	if modeId == "" {
		return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeRejected, Code: "invalid_option", Message: "未选择模式"}, nil
	}
	if !in.BuildInput.Runtime.Online {
		return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeRejected, Code: "agent_offline", Message: "当前 agent 不在线"}, nil
	}
	if !in.BuildInput.Runtime.HasLocalAction("set_mode") {
		return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeRejected, Code: "local_action_unavailable", Message: "当前 agent 未声明 set_mode"}, nil
	}
	if err := in.Executor.DispatchLocalAction(context.Background(), core.LocalActionRequest{
		OwnerID: in.BuildInput.OwnerID, AgentID: in.BuildInput.Agent.AgentID, SessionID: in.BuildInput.Session.SessionID,
		ActionType: "set_mode", Params: map[string]any{"session_id": in.BuildInput.Session.SessionID, "mode_id": modeId, "display_label": resolveOptionLabel(modeId, copilotModeOptions)}, TimeoutMs: 15_000,
	}); err != nil {
		return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeRejected, Code: "dispatch_failed", Message: err.Error()}, nil
	}
	return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeAcceptedWithImmediateRefresh, Code: "accepted", Message: "已切换模式"}, nil
}

func handleGetRateLimits(in core.ActionInput) (toolprotocol.ActionResult, error) {
	if !in.BuildInput.Runtime.Online {
		return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeRejected, Code: "agent_offline", Message: "当前 agent 不在线"}, nil
	}
	if !in.BuildInput.Runtime.HasLocalAction("get_rate_limits") {
		return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeRejected, Code: "local_action_unavailable", Message: "当前 agent 未声明 get_rate_limits"}, nil
	}
	if err := in.Executor.DispatchLocalAction(context.Background(), core.LocalActionRequest{
		OwnerID: in.BuildInput.OwnerID, AgentID: in.BuildInput.Agent.AgentID, SessionID: in.BuildInput.Session.SessionID,
		ActionType: "get_rate_limits", Params: map[string]any{"session_id": in.BuildInput.Session.SessionID}, TimeoutMs: 20_000,
	}); err != nil {
		return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeRejected, Code: "dispatch_failed", Message: err.Error()}, nil
	}
	return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeAcceptedNoStateChange, Code: "accepted", Message: "已提交额度查询请求"}, nil
}

func handleGetSessionUsage(in core.ActionInput) (toolprotocol.ActionResult, error) {
	if !in.BuildInput.Runtime.Online {
		return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeRejected, Code: "agent_offline", Message: "当前 agent 不在线"}, nil
	}
	if !in.BuildInput.Runtime.HasLocalAction("get_session_usage") {
		return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeRejected, Code: "local_action_unavailable", Message: "当前 agent 未声明 get_session_usage"}, nil
	}
	if err := in.Executor.DispatchLocalAction(context.Background(), core.LocalActionRequest{
		OwnerID: in.BuildInput.OwnerID, AgentID: in.BuildInput.Agent.AgentID, SessionID: in.BuildInput.Session.SessionID,
		ActionType: "get_session_usage", Params: map[string]any{"session_id": in.BuildInput.Session.SessionID}, TimeoutMs: 20_000,
	}); err != nil {
		return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeRejected, Code: "dispatch_failed", Message: err.Error()}, nil
	}
	return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeAcceptedNoStateChange, Code: "accepted", Message: "已提交用量查询请求"}, nil
}

// ── 辅助函数 ──

func hasCopilotSessionBinding(binding core.BindingInfo) bool {
	return strings.TrimSpace(binding.BindingID) != "" ||
		strings.TrimSpace(binding.Cwd) != "" ||
		strings.TrimSpace(binding.WorkerStatus) != ""
}

func bindingMetaString(meta map[string]any, key string) string {
	if len(meta) == 0 {
		return ""
	}
	value, _ := meta[key].(string)
	return strings.TrimSpace(value)
}

func resolveOptionLabel(optionID string, options []toolprotocol.Option) string {
	id := strings.TrimSpace(optionID)
	if id == "" {
		return ""
	}
	for _, opt := range options {
		if strings.EqualFold(strings.TrimSpace(opt.OptionID), id) {
			return strings.TrimSpace(opt.Label)
		}
	}
	return id
}

type copilotModelOption struct {
	ID    string
	Label string
}

func buildCopilotModelOptions(meta map[string]any) []copilotModelOption {
	models, ok := meta["available_models"].([]any)
	if !ok {
		return nil
	}
	opts := make([]copilotModelOption, 0, len(models))
	seen := map[string]struct{}{}
	for _, raw := range models {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id := strings.TrimSpace(bindingMetaString(entry, "id"))
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		label := strings.TrimSpace(bindingMetaString(entry, "displayName"))
		if label == "" {
			label = id
		}
		opts = append(opts, copilotModelOption{ID: id, Label: label})
	}
	return opts
}

func resolveCopilotModelLabel(modelID string, options []copilotModelOption) string {
	id := strings.TrimSpace(modelID)
	if id == "" {
		return ""
	}
	for _, opt := range options {
		if opt.ID == id {
			return opt.Label
		}
	}
	return id
}

func toCopilotModelProtocolOptions(options []copilotModelOption) []toolprotocol.Option {
	if len(options) == 0 {
		return nil
	}
	out := make([]toolprotocol.Option, 0, len(options))
	for _, opt := range options {
		out = append(out, toolprotocol.Option{OptionID: opt.ID, Label: opt.Label})
	}
	return out
}
