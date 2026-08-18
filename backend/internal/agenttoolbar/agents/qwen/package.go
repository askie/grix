package qwen

import (
	"context"
	"fmt"
	"strings"

	"github.com/askie/grix/backend/internal/agenttoolbar/agents/shared"
	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	"github.com/askie/grix/backend/internal/model"
)

type Package struct{}

type qwenModelOption struct {
	ID    string
	Label string
}

var qwenModeOptions = []toolprotocol.Option{
	{OptionID: "default", Label: "默认（需确认）"},
	{OptionID: "autoEdit", Label: "自动编辑"},
	{OptionID: "yolo", Label: "全自动"},
	{OptionID: "plan", Label: "只读计划"},
}

func New() *Package            { return &Package{} }
func (p *Package) Key() string { return model.AgentClientTypeQwen }
func (p *Package) Match(ctx core.MatchContext) bool {
	return ctx.Agent.ClientType == model.AgentClientTypeQwen
}

func (p *Package) Build(_ context.Context, in core.BuildInput) (toolprotocol.Snapshot, error) {
	if !hasQwenSessionBinding(in.Binding) {
		return toolprotocol.Snapshot{
			Visible: false,
			Items:   []toolprotocol.Item{},
		}, nil
	}

	items := make([]toolprotocol.Item, 0, 6)

	runState := strings.TrimSpace(in.Run.State)
	showStopOutput := in.Run.HasActiveRun && (in.Run.CanStop || runState == "stopping")
	if showStopOutput {
		items = append(items, toolprotocol.Item{
			ItemID:   "stop_output",
			GroupID:  "run_control",
			Kind:     toolprotocol.ItemKindButton,
			ActionID: "stop_output",
			Icon:     "stop",
			Variant:  "danger",
			Disabled: !in.Run.CanStop,
			Tooltip:  qwenStopOutputTooltip(runState),
			Loading:  runState == "stopping",
			Selected: runState == "stopping",
		})
	}

	sessionDisabled := !in.Runtime.Online || !in.Runtime.HasLocalAction("session_control")
	sessionTooltip := "Qwen 会话操作"
	switch {
	case !in.Runtime.Online:
		sessionTooltip = "Qwen 当前离线"
	case !in.Runtime.HasLocalAction("session_control"):
		sessionTooltip = "当前插件未声明 session_control"
	case strings.TrimSpace(in.Binding.Cwd) != "":
		sessionTooltip = "Qwen 会话操作\n工作目录: " + strings.TrimSpace(in.Binding.Cwd)
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

	usageDisabled := !in.Runtime.Online || !in.Runtime.HasLocalAction("get_session_usage")
	modelOptions := buildQwenModelOptions(in.Binding.Meta)
	currentModelID := qwenMetaString(in.Binding.Meta, "model_id")
	currentModelLabel := resolveQwenModelLabel(currentModelID, modelOptions)

	currentModeID := qwenMetaString(in.Binding.Meta, "mode_id")
	modeDisabled := !in.Runtime.Online || !in.Runtime.HasLocalAction("set_mode")
	modeTooltip := "切换 Qwen 审批模式"
	switch {
	case !in.Runtime.Online:
		modeTooltip = "Qwen 当前离线"
	case !in.Runtime.HasLocalAction("set_mode"):
		modeTooltip = "当前插件未声明 set_mode"
	}

	items = append(items,
		toolprotocol.Item{
			ItemID:      "session_control",
			GroupID:     "session_control",
			Kind:        toolprotocol.ItemKindSelect,
			ActionID:    "session_control",
			Icon:        "status",
			Variant:     "secondary",
			Disabled:    sessionDisabled,
			Tooltip:     sessionTooltip,
			BadgeText:   badge,
			Placeholder: "",
			Options: []toolprotocol.Option{
				{OptionID: "status", Label: "查看状态"},
				{OptionID: "stop", Label: "停止会话"},
				{
					OptionID: "usage",
					Label:    "查看用量",
					Disabled: usageDisabled,
				},
			},
		},
		toolprotocol.Item{
			ItemID:      "select_model",
			GroupID:     "model_control",
			Kind:        toolprotocol.ItemKindSelect,
			ActionID:    "select_model",
			Label:       currentModelLabel,
			Value:       currentModelLabel,
			Icon:        "cpu",
			Variant:     "secondary",
			Disabled:    !canQwenSelectModel(in, modelOptions),
			Tooltip:     qwenModelTooltip(in, modelOptions),
			Placeholder: "模型",
			Options:     toQwenProtocolOptions(modelOptions),
		},
		toolprotocol.Item{
			ItemID:      "select_mode",
			GroupID:     "mode_control",
			Kind:        toolprotocol.ItemKindSelect,
			ActionID:    "select_mode",
			Icon:        "shield",
			Variant:     "secondary",
			Disabled:    modeDisabled,
			Tooltip:     modeTooltip,
			Value:       currentModeID,
			BadgeText:   resolveQwenModeLabel(currentModeID),
			Placeholder: "选择审批模式",
			Options:     qwenModeOptions,
		},
	)

	if len(in.Runtime.Skills) > 0 {
		items = append(items, shared.BuildSkillsItem(in.Runtime.Skills))
	}

	if item, ok := shared.BuildSlashCommandsItem("qwen"); ok {
		items = append([]toolprotocol.Item{item}, items...)
	}

	// Provider quota (rate limits from third-party LLM providers)
	if quotaItems := shared.BuildProviderQuotaItems(shared.ParseProviderQuota(in.Binding.Meta), in.Runtime.HasLocalAction("get_rate_limits")); len(quotaItems) > 0 {
		items = append(items, quotaItems...)
	}

	return toolprotocol.Snapshot{
		Visible: true,
		Items:   items,
	}, nil
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
	case "get_rate_limits", "provider_quota_five_hour", "provider_quota_weekly_limit", "provider_quota_balance":
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
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "stop_unavailable",
			Message: "当前没有可停止的输出",
		}, nil
	}
	if err := in.Executor.StopOutput(context.Background(), core.StopOutputRequest{
		OwnerID:   in.BuildInput.OwnerID,
		SessionID: in.BuildInput.Session.SessionID,
		AgentID:   in.BuildInput.Agent.AgentID,
		RunID:     in.BuildInput.Run.RunID,
	}); err != nil {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "stop_failed",
			Message: err.Error(),
		}, nil
	}
	return toolprotocol.ActionResult{
		Outcome: toolprotocol.ActionOutcomeAcceptedWithImmediateRefresh,
		Code:    "accepted",
		Message: "已提交停止请求",
	}, nil
}

func handleSessionControl(in core.ActionInput) (toolprotocol.ActionResult, error) {
	verb := strings.TrimSpace(in.Request.OptionID)
	switch verb {
	case "status", "stop":
	case "usage":
		return handleGetSessionUsage(in)
	default:
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_option",
			Message: "工具栏选项无效",
		}, nil
	}
	if !in.BuildInput.Runtime.Online {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "agent_offline",
			Message: "当前 agent 不在线",
		}, nil
	}
	if !in.BuildInput.Runtime.HasLocalAction("session_control") {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "local_action_unavailable",
			Message: "当前 agent 未声明 session_control",
		}, nil
	}
	return dispatchLocalAction(in, "session_control", map[string]any{
		"session_id": in.BuildInput.Session.SessionID,
		"verb":       verb,
	}, 15_000, "已提交会话操作")
}

func handleSelectModel(in core.ActionInput) (toolprotocol.ActionResult, error) {
	modelID := strings.TrimSpace(in.Request.OptionID)
	if modelID == "" {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_option",
			Message: "未选择模型",
		}, nil
	}
	if !canQwenSelectModel(in.BuildInput, buildQwenModelOptions(in.BuildInput.Binding.Meta)) {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "action_unavailable",
			Message: qwenModelTooltip(in.BuildInput, buildQwenModelOptions(in.BuildInput.Binding.Meta)),
		}, nil
	}
	return dispatchLocalAction(in, "set_model", map[string]any{
		"session_id":    in.BuildInput.Session.SessionID,
		"model_id":      modelID,
		"display_label": resolveQwenModelLabel(modelID, buildQwenModelOptions(in.BuildInput.Binding.Meta)),
	}, 15_000, "已提交模型切换请求")
}

func handleSelectMode(in core.ActionInput) (toolprotocol.ActionResult, error) {
	modeID := strings.TrimSpace(in.Request.OptionID)
	if modeID == "" {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_option",
			Message: "未选择模式",
		}, nil
	}
	if !in.BuildInput.Runtime.Online {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "agent_offline",
			Message: "当前 agent 不在线",
		}, nil
	}
	if !in.BuildInput.Runtime.HasLocalAction("set_mode") {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "local_action_unavailable",
			Message: "当前 agent 未声明 set_mode",
		}, nil
	}
	return dispatchLocalAction(in, "set_mode", map[string]any{
		"session_id":    in.BuildInput.Session.SessionID,
		"mode_id":       modeID,
		"display_label": resolveQwenModeLabel(modeID),
	}, 15_000, "已提交模式切换请求")
}

func handleGetSessionUsage(in core.ActionInput) (toolprotocol.ActionResult, error) {
	if !in.BuildInput.Runtime.Online {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "agent_offline",
			Message: "当前 agent 不在线",
		}, nil
	}
	if !in.BuildInput.Runtime.HasLocalAction("get_session_usage") {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "local_action_unavailable",
			Message: "当前 agent 未声明 get_session_usage",
		}, nil
	}
	return dispatchLocalAction(in, "get_session_usage", map[string]any{
		"session_id": in.BuildInput.Session.SessionID,
	}, 20_000, "已提交用量查询请求")
}

func handleGetRateLimits(in core.ActionInput) (toolprotocol.ActionResult, error) {
	if !in.BuildInput.Runtime.Online {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "agent_offline",
			Message: "当前 agent 不在线",
		}, nil
	}
	if !in.BuildInput.Runtime.HasLocalAction("get_rate_limits") {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "local_action_unavailable",
			Message: "当前 agent 未声明 get_rate_limits",
		}, nil
	}
	return dispatchLocalAction(in, "get_rate_limits", map[string]any{
		"session_id": in.BuildInput.Session.SessionID,
	}, 20_000, "已提交余量查询请求")
}

func dispatchLocalAction(in core.ActionInput, actionType string, params map[string]any, timeoutMs int, message string) (toolprotocol.ActionResult, error) {
	if err := in.Executor.DispatchLocalAction(context.Background(), core.LocalActionRequest{
		OwnerID:    in.BuildInput.OwnerID,
		AgentID:    in.BuildInput.Agent.AgentID,
		SessionID:  in.BuildInput.Session.SessionID,
		ActionType: actionType,
		Params:     params,
		TimeoutMs:  timeoutMs,
	}); err != nil {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "dispatch_failed",
			Message: err.Error(),
		}, nil
	}
	return toolprotocol.ActionResult{
		Outcome: toolprotocol.ActionOutcomeAcceptedNoStateChange,
		Code:    "accepted",
		Message: message,
	}, nil
}

func buildQwenModelOptions(meta map[string]any) []qwenModelOption {
	models, ok := meta["available_models"].([]any)
	if !ok {
		return nil
	}
	opts := make([]qwenModelOption, 0, len(models))
	seen := map[string]struct{}{}
	for _, raw := range models {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id := strings.TrimSpace(qwenMetaString(entry, "id"))
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		label := strings.TrimSpace(qwenMetaString(entry, "display_name"))
		if label == "" {
			label = strings.TrimSpace(qwenMetaString(entry, "displayName"))
		}
		if label == "" {
			label = id
		}
		opts = append(opts, qwenModelOption{ID: id, Label: label})
	}
	return opts
}

func resolveQwenModelLabel(modelID string, options []qwenModelOption) string {
	modelID = strings.TrimSpace(modelID)
	for _, option := range options {
		if option.ID == modelID {
			return option.Label
		}
	}
	if modelID != "" {
		return modelID
	}
	if len(options) > 0 {
		return options[0].Label
	}
	return "模型"
}

func toQwenProtocolOptions(options []qwenModelOption) []toolprotocol.Option {
	out := make([]toolprotocol.Option, 0, len(options))
	for _, option := range options {
		out = append(out, toolprotocol.Option{
			OptionID: option.ID,
			Label:    option.Label,
		})
	}
	return out
}

func canQwenSelectModel(in core.BuildInput, options []qwenModelOption) bool {
	return in.Runtime.Online && in.Runtime.HasLocalAction("set_model") && len(options) > 0
}

func qwenModelTooltip(in core.BuildInput, options []qwenModelOption) string {
	switch {
	case !in.Runtime.Online:
		return "Qwen 当前离线"
	case !in.Runtime.HasLocalAction("set_model"):
		return "当前插件未声明 set_model"
	case len(options) == 0:
		return "等待 Qwen 模型列表同步"
	default:
		return "切换 Qwen 模型"
	}
}

func resolveQwenModeLabel(modeID string) string {
	for _, opt := range qwenModeOptions {
		if strings.EqualFold(strings.TrimSpace(opt.OptionID), strings.TrimSpace(modeID)) {
			return opt.Label
		}
	}
	return strings.TrimSpace(modeID)
}

func qwenStopOutputTooltip(runState string) string {
	if strings.TrimSpace(runState) == "stopping" {
		return "正在停止当前输出"
	}
	return "停止当前输出"
}

func qwenMetaString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	value, ok := meta[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func hasQwenSessionBinding(binding core.BindingInfo) bool {
	return strings.TrimSpace(binding.BindingID) != "" ||
		strings.TrimSpace(binding.Cwd) != ""
}
