package codex

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

type modelOption struct {
	ID    string
	Label string
}

var codexExecCommandOptions = []toolprotocol.Option{
	{OptionID: "exec:rollback", Label: "回退"},
}

const codexContextWarningThreshold = 80.0

func New() *Package            { return &Package{} }
func (p *Package) Key() string { return model.AgentClientTypeCodex }
func (p *Package) Match(ctx core.MatchContext) bool {
	return ctx.Agent.ClientType == model.AgentClientTypeCodex
}

func (p *Package) Build(_ context.Context, in core.BuildInput) (toolprotocol.Snapshot, error) {
	if !hasCodexSessionBinding(in.Binding) {
		return toolprotocol.Snapshot{
			Visible: false,
			Items:   []toolprotocol.Item{},
		}, nil
	}

	items := make([]toolprotocol.Item, 0, 8)
	runState := strings.TrimSpace(in.Run.State)
	showStopOutput := in.Run.HasActiveRun && (in.Run.CanStop || runState == protocolStateStopping)
	if showStopOutput {
		items = append(items, toolprotocol.Item{
			ItemID:   "stop_output",
			GroupID:  "run_control",
			Kind:     toolprotocol.ItemKindButton,
			ActionID: "stop_output",
			Icon:     "stop",
			Variant:  "danger",
			Disabled: !in.Run.CanStop,
			Tooltip:  stopOutputTooltip(runState),
			Loading:  runState == protocolStateStopping,
			Selected: runState == protocolStateStopping,
		})
	}

	modelOptions := buildModelOptions(in.Binding.Meta)
	currentModelID := strings.TrimSpace(metaString(in.Binding.Meta, "model_id"))
	currentModelLabel := resolveModelLabel(currentModelID, modelOptions)
	modeID := normalizeModeID(metaString(in.Binding.Meta, "mode_id"))
	currentModeLabel := modeDisplayLabel(modeID)

	items = append(items,
		buildSessionControlItem(in),
	)

	rateLimitItems := buildCodexRateLimitItems(in)
	if len(rateLimitItems) > 0 {
		items = append(items, rateLimitItems...)
	}

	items = append(items,
		toolprotocol.Item{
			ItemID:      "select_mode",
			GroupID:     "mode_control",
			Kind:        toolprotocol.ItemKindSelect,
			ActionID:    "select_mode",
			Label:       currentModeLabel,
			Value:       currentModeLabel,
			Icon:        "shield",
			Variant:     "secondary",
			Disabled:    !canSelectMode(in),
			Tooltip:     modeTooltip(in),
			Placeholder: "模式",
			Options: []toolprotocol.Option{
				{OptionID: "default", Label: modeDisplayLabel("default")},
				{OptionID: "plan", Label: modeDisplayLabel("plan")},
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
			Disabled:    !canSelectModel(in, modelOptions),
			Tooltip:     modelTooltip(in, modelOptions),
			Placeholder: "模型",
			Options:     toProtocolOptions(modelOptions),
		},
	)

	effortOptions := buildEffortOptions(in.Binding.Meta)
	if len(effortOptions) > 0 {
		currentEffort := strings.TrimSpace(metaString(in.Binding.Meta, "reasoning_effort"))
		if currentEffort == "" {
			currentEffort = resolveDefaultEffort(modelOptions, currentModelID)
		}
		currentEffortLabel := resolveEffortLabel(currentEffort, effortOptions)
		items = append(items, toolprotocol.Item{
			ItemID:      "select_reasoning_effort",
			GroupID:     "effort_control",
			Kind:        toolprotocol.ItemKindSelect,
			ActionID:    "select_reasoning_effort",
			Label:       currentEffortLabel,
			Value:       currentEffortLabel,
			Icon:        "spark",
			Variant:     "secondary",
			Disabled:    !canSelectReasoningEffort(in, effortOptions),
			Tooltip:     effortTooltip(in, effortOptions),
			Placeholder: "推理力度",
			Options:     effortOptions,
		})
	}

	serviceTierOptions := buildServiceTierOptions(in.Binding.Meta)
	if len(serviceTierOptions) > 0 {
		currentTier := strings.TrimSpace(metaString(in.Binding.Meta, "service_tier"))
		// 选项列表 = 标准档(default) + 模型广告的速度档；连接器约定 default 表示回标准档
		tierSelectOptions := append(
			[]toolprotocol.Option{{OptionID: "default", Label: serviceTierDisplayLabel("default", "")}},
			serviceTierOptions...,
		)
		currentTierLabel := resolveServiceTierLabel(currentTier, tierSelectOptions)
		items = append(items, toolprotocol.Item{
			ItemID:      "select_service_tier",
			GroupID:     "service_tier_control",
			Kind:        toolprotocol.ItemKindSelect,
			ActionID:    "select_service_tier",
			Label:       currentTierLabel,
			Value:       currentTierLabel,
			Icon:        "run",
			Variant:     "secondary",
			Disabled:    !canSelectServiceTier(in, serviceTierOptions),
			Tooltip:     serviceTierTooltip(in, serviceTierOptions),
			Placeholder: "速度档",
			Options:     tierSelectOptions,
		})
	}

	items = append(items,
		buildCodexContextCompactProgressItem(in),
	)
	sandboxModeID := normalizeSandboxModeID(metaString(in.Binding.Meta, "sandbox_mode"))
	currentSandboxLabel := sandboxModeDisplayLabel(sandboxModeID)
	items = append(items,
		toolprotocol.Item{
			ItemID:      "select_sandbox_mode",
			GroupID:     "sandbox_control",
			Kind:        toolprotocol.ItemKindSelect,
			ActionID:    "select_sandbox_mode",
			Label:       currentSandboxLabel,
			Value:       currentSandboxLabel,
			Icon:        "shield",
			Variant:     "secondary",
			Disabled:    !canSelectSandboxMode(in),
			Tooltip:     sandboxModeTooltip(in),
			Placeholder: "沙箱",
			Options: []toolprotocol.Option{
				{OptionID: "default", Label: sandboxModeDisplayLabel("default")},
				{OptionID: "danger-full-access", Label: sandboxModeDisplayLabel("danger-full-access")},
				{OptionID: "workspace-write", Label: sandboxModeDisplayLabel("workspace-write")},
				{OptionID: "read-only", Label: sandboxModeDisplayLabel("read-only")},
			},
		},
	)

	if len(in.Runtime.Skills) > 0 {
		items = append(items, shared.BuildSkillsItem(in.Runtime.Skills))
	}

	if item, ok := shared.BuildSlashCommandsItem("codex"); ok {
		items = append([]toolprotocol.Item{item}, items...)
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
	case "select_model":
		return handleSelectModel(in)
	case "select_mode":
		return handleSelectMode(in)
	case "select_reasoning_effort":
		return handleSelectReasoningEffort(in)
	case "select_service_tier":
		return handleSelectServiceTier(in)
	case "select_sandbox_mode":
		return handleSelectSandboxMode(in)
	case "session_control":
		return handleSessionControl(in)
	case "get_session_usage":
		return handleGetSessionUsage(in)
	case "get_rate_limits":
		return handleGetRateLimits(in)
	case "thread_compact":
		return handleThreadCompact(in)
	default:
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_action",
			Message: "工具栏动作无效",
		}, nil
	}
}

func buildSessionControlItem(in core.BuildInput) toolprotocol.Item {
	sessionDisabled := !in.Runtime.Online || !in.Runtime.HasLocalAction("session_control")
	sessionTooltip := "Codex 会话操作"
	switch {
	case !in.Runtime.Online:
		sessionTooltip = "Codex 当前离线"
	case !in.Runtime.HasLocalAction("session_control"):
		sessionTooltip = "当前插件未声明 session_control"
	case strings.TrimSpace(in.Binding.Cwd) != "":
		sessionTooltip = "Codex 会话操作\n工作目录: " + in.Binding.Cwd
	}

	displayLabel := sessionDisplayLabel(in)

	opts := []toolprotocol.Option{
		{OptionID: "status", Label: "查看状态"},
		{OptionID: "restart", Label: "重启会话"},
		{OptionID: "unbind", Label: "解绑"},
		{
			OptionID: "usage",
			Label:    "查看用量",
			Disabled: !canGetSessionUsage(in),
		},
	}
	opts = append(opts, codexExecCommandOptions...)

	return toolprotocol.Item{
		ItemID:      "session_control",
		GroupID:     "session_control",
		Kind:        toolprotocol.ItemKindSelect,
		ActionID:    "session_control",
		Label:       displayLabel,
		Value:       displayLabel,
		Icon:        "status",
		Variant:     "secondary",
		Disabled:    sessionDisabled,
		Tooltip:     sessionTooltip,
		BadgeText:   "",
		Placeholder: "",
		Options:     opts,
	}
}

func buildCodexRateLimitItems(in core.BuildInput) []toolprotocol.Item {
	if !in.Runtime.Online || !in.Runtime.HasLocalAction("get_rate_limits") {
		return nil
	}
	limits := parseCodexRateLimits(in.Binding.Meta)
	if limits == nil {
		// No data yet — render nothing; items appear once the provider reports.
		return nil
	}

	// Data-driven: only render windows that the provider actually returns,
	// and skip buckets whose used percent is 0 (5H/7D 等限额为 0 不显示)。
	var items []toolprotocol.Item
	if primary, ok := limits["primary"]; ok && primary.UsedPercent != 0 {
		items = append(items, buildCodexRateLimitProgressItem("rate_limit_primary", "rate_limits", "5H", "rate_limit_5h_usage", primary.UsedPercent, primary.WindowMinutes, primary.ResetsAt))
	}
	if secondary, ok := limits["secondary"]; ok && secondary.UsedPercent != 0 {
		items = append(items, buildCodexRateLimitProgressItem("rate_limit_secondary", "rate_limits", "7D", "rate_limit_7d_usage", secondary.UsedPercent, secondary.WindowMinutes, secondary.ResetsAt))
	}

	// Credits — agent-agnostic key from meta["credits"]
	if credits := shared.ParseCredits(in.Binding.Meta); credits != nil && credits.ShouldShow() &&
		(credits.Unlimited || credits.Balance == nil || *credits.Balance != 0) {
		items = append(items, buildCreditsItem(credits))
	}

	// Extra limits — agent-agnostic key from meta["extra_limits"]
	if extras := shared.ParseExtraLimits(in.Binding.Meta); len(extras) > 0 {
		for i, extra := range extras {
			if extra.Label == "" || extra.UsedPercent == 0 {
				continue
			}
			itemID := fmt.Sprintf("rate_limit_extra_%d", i)
			centerText := shared.PercentCenterText(extra.UsedPercent)
			detail := shared.FormatResetsAtDetail(extra.WindowMinutes, extra.ResetsAt)
			items = append(items, buildCodexRateLimitProgressItem(itemID, "rate_limits", centerText, extra.Label, extra.UsedPercent, extra.WindowMinutes, extra.ResetsAt))
			// Override the progress detail with the formatted window info
			items[len(items)-1].ProgressDetail = detail
		}
	}

	return items
}

func buildCreditsItem(credits *shared.CreditsInfo) toolprotocol.Item {
	centerText := ""
	detail := ""
	if credits.Unlimited {
		centerText = "∞"
		detail = "无限额度"
	} else if credits.Balance != nil {
		centerText = fmt.Sprintf("%.1f", *credits.Balance)
		detail = fmt.Sprintf("剩余 %.1f", *credits.Balance)
	}
	return toolprotocol.Item{
		ItemID:         "account_credits",
		GroupID:        "rate_limits",
		Kind:           toolprotocol.ItemKindProgress,
		ActionID:       "account_credits",
		Variant:        "secondary",
		Percent:        0,
		CenterText:     centerText,
		ProgressDesc:   "账户额度",
		ProgressDetail: detail,
		LocalAction:    "get_rate_limits",
	}
}

type codexRateLimitWindow struct {
	UsedPercent   float64
	WindowMinutes float64
	ResetsAt      string
}

func buildCodexRateLimitProgressItem(itemID, groupID, centerText, descPrefix string, percent float64, windowMinutes float64, resetsAt string) toolprotocol.Item {
	progressDetail := resetsAt
	if progressDetail == "" && windowMinutes > 0 {
		progressDetail = fmt.Sprintf("%.0f", windowMinutes)
	}
	return toolprotocol.Item{
		ItemID:         itemID,
		GroupID:        groupID,
		Kind:           toolprotocol.ItemKindProgress,
		ActionID:       itemID,
		Variant:        "secondary",
		Percent:        percent,
		CenterText:     centerText,
		ProgressDesc:   descPrefix,
		ProgressDetail: progressDetail,
		LocalAction:    "get_rate_limits",
	}
}

func parseCodexRateLimits(meta map[string]any) map[string]codexRateLimitWindow {
	if meta == nil {
		return nil
	}
	raw, ok := meta["rate_limits"]
	if !ok || raw == nil {
		return nil
	}
	limitsMap, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	sampledAtVal, _ := limitsMap["sampledAt"]
	if sampledAtVal == nil {
		return nil
	}
	result := make(map[string]codexRateLimitWindow, 2)
	for _, key := range []string{"primary", "secondary"} {
		entry, ok := limitsMap[key].(map[string]any)
		if !ok {
			continue
		}
		pct := metaFloat64(entry, "usedPercent")
		window := metaFloat64(entry, "windowMinutes")
		resetsAt, _ := entry["resetsAt"].(string)
		result[key] = codexRateLimitWindow{
			UsedPercent:   pct,
			WindowMinutes: window,
			ResetsAt:      resetsAt,
		}
	}
	return result
}

func buildCodexContextCompactProgressItem(in core.BuildInput) toolprotocol.Item {
	ctx := parseCodexContextWindow(in.Binding.Meta)
	percent := ctx.UsedPercentage
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	detail := ""
	if ctx.HasRemaining {
		detail = fmt.Sprintf("剩余 %.1f%%", ctx.RemainingPercentage)
	}
	return toolprotocol.Item{
		ItemID:         "thread_compact",
		GroupID:        "session_control",
		Kind:           toolprotocol.ItemKindProgress,
		ActionID:       "thread_compact",
		Variant:        codexContextProgressVariant(percent),
		Percent:        percent,
		CenterText:     shared.PercentCenterText(percent),
		ProgressDesc:   "会话上下文",
		ProgressDetail: detail,
		Tooltip:        compactTooltip(in),
		Disabled:       !canCompactThread(in),
		LocalAction:    "thread_compact",
	}
}

func codexContextProgressVariant(percent float64) string {
	if percent >= codexContextWarningThreshold {
		return "warning"
	}
	return "secondary"
}

type codexContextWindow struct {
	UsedPercentage      float64
	RemainingPercentage float64
	HasRemaining        bool
}

func parseCodexContextWindow(meta map[string]any) codexContextWindow {
	if meta == nil {
		return codexContextWindow{}
	}
	raw, ok := meta["context_window"]
	if !ok || raw == nil {
		return codexContextWindow{}
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return codexContextWindow{}
	}
	return codexContextWindow{
		UsedPercentage:      metaFloat64(obj, "usedPercentage"),
		RemainingPercentage: metaFloat64(obj, "remainingPercentage"),
		HasRemaining:        obj["remainingPercentage"] != nil,
	}
}

func sessionDisplayLabel(in core.BuildInput) string {
	if cwd := strings.TrimSpace(in.Binding.Cwd); cwd != "" {
		return shared.PathBase(cwd)
	}
	if worker := strings.TrimSpace(in.Binding.WorkerStatus); worker == "session_expired" {
		return "会话已过期"
	} else if worker != "" {
		return worker
	}
	if in.Runtime.Online {
		return "在线"
	}
	return "离线"
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

func handleSelectModel(in core.ActionInput) (toolprotocol.ActionResult, error) {
	modelID := strings.TrimSpace(in.Request.OptionID)
	if modelID == "" {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_option",
			Message: "未选择模型",
		}, nil
	}
	if !canSelectModel(in.BuildInput, buildModelOptions(in.BuildInput.Binding.Meta)) {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "action_unavailable",
			Message: modelTooltip(in.BuildInput, buildModelOptions(in.BuildInput.Binding.Meta)),
		}, nil
	}
	return dispatchLocalAction(in, "set_model", map[string]any{
		"session_id": in.BuildInput.Session.SessionID,
		"model_id":   modelID,
		"actor_id":   fmt.Sprintf("%d", in.BuildInput.OwnerID),
	}, 15_000, "已提交模型切换请求")
}

func handleSelectMode(in core.ActionInput) (toolprotocol.ActionResult, error) {
	modeID := normalizeModeID(in.Request.OptionID)
	if modeID == "" {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_option",
			Message: "未选择模式",
		}, nil
	}
	if !canSelectMode(in.BuildInput) {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "action_unavailable",
			Message: modeTooltip(in.BuildInput),
		}, nil
	}
	return dispatchLocalAction(in, "set_mode", map[string]any{
		"session_id": in.BuildInput.Session.SessionID,
		"mode_id":    modeID,
		"actor_id":   fmt.Sprintf("%d", in.BuildInput.OwnerID),
	}, 15_000, "已提交模式切换请求")
}

func handleSessionControl(in core.ActionInput) (toolprotocol.ActionResult, error) {
	optionID := strings.TrimSpace(in.Request.OptionID)

	if strings.HasPrefix(optionID, "exec:") {
		return handleExecCommand(in, optionID)
	}

	switch optionID {
	case "status", "where", "stop", "restart", "unbind":
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
		"verb":       optionID,
	}, 15_000, "已提交会话操作")
}

func handleExecCommand(in core.ActionInput, optionID string) (toolprotocol.ActionResult, error) {
	cmdName := strings.TrimPrefix(optionID, "exec:")
	if cmdName == "" {
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
	msg := "已提交" + cmdName + "请求"
	for _, opt := range codexExecCommandOptions {
		if strings.TrimPrefix(opt.OptionID, "exec:") == cmdName {
			msg = "已提交" + opt.Label + "请求"
			break
		}
	}
	return dispatchLocalAction(in, "session_control", map[string]any{
		"session_id": in.BuildInput.Session.SessionID,
		"verb":       "exec",
		"args":       cmdName,
	}, 30_000, msg)
}

func handleThreadCompact(in core.ActionInput) (toolprotocol.ActionResult, error) {
	if !canCompactThread(in.BuildInput) {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "action_unavailable",
			Message: compactTooltip(in.BuildInput),
		}, nil
	}
	return dispatchLocalAction(in, "thread_compact", map[string]any{
		"session_id": in.BuildInput.Session.SessionID,
		"actor_id":   fmt.Sprintf("%d", in.BuildInput.OwnerID),
	}, 120_000, "已提交 thread_compact 请求")
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
		"actor_id":   fmt.Sprintf("%d", in.BuildInput.OwnerID),
	}, 20_000, "已提交用量查询请求")
}

func handleGetSessionUsage(in core.ActionInput) (toolprotocol.ActionResult, error) {
	if !canGetSessionUsage(in.BuildInput) {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "action_unavailable",
			Message: sessionUsageTooltip(in.BuildInput),
		}, nil
	}
	return dispatchLocalAction(in, "get_session_usage", map[string]any{
		"session_id": in.BuildInput.Session.SessionID,
		"actor_id":   fmt.Sprintf("%d", in.BuildInput.OwnerID),
	}, 20_000, "已提交用量查询请求")
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

func buildModelOptions(meta map[string]any) []modelOption {
	models, ok := meta["available_models"].([]any)
	if !ok {
		return nil
	}
	opts := make([]modelOption, 0, len(models))
	seen := map[string]struct{}{}
	for _, raw := range models {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id := strings.TrimSpace(metaString(entry, "id"))
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		label := strings.TrimSpace(metaString(entry, "display_name"))
		if label == "" {
			label = strings.TrimSpace(metaString(entry, "displayName"))
		}
		if label == "" {
			label = id
		}
		opts = append(opts, modelOption{ID: id, Label: label})
	}
	return opts
}

func resolveModelLabel(modelID string, options []modelOption) string {
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

func toProtocolOptions(options []modelOption) []toolprotocol.Option {
	out := make([]toolprotocol.Option, 0, len(options))
	for _, option := range options {
		out = append(out, toolprotocol.Option{
			OptionID: option.ID,
			Label:    option.Label,
		})
	}
	return out
}

func canSelectModel(in core.BuildInput, options []modelOption) bool {
	return in.Runtime.Online && in.Runtime.HasLocalAction("set_model") && len(options) > 0
}

func canSelectMode(in core.BuildInput) bool {
	return in.Runtime.Online && in.Runtime.HasLocalAction("set_mode")
}

func canCompactThread(in core.BuildInput) bool {
	return in.Runtime.Online && in.Runtime.HasLocalAction("thread_compact")
}

func canGetSessionUsage(in core.BuildInput) bool {
	return in.Runtime.Online && in.Runtime.HasLocalAction("get_session_usage")
}

func modelTooltip(in core.BuildInput, options []modelOption) string {
	switch {
	case !in.Runtime.Online:
		return "Codex 当前离线"
	case !in.Runtime.HasLocalAction("set_model"):
		return "当前 Codex 插件未声明 set_model，请升级并重启 grix-codex"
	case len(options) == 0:
		return "等待 Codex 模型列表同步"
	default:
		return "切换 Codex 模型"
	}
}

func modeTooltip(in core.BuildInput) string {
	switch {
	case !in.Runtime.Online:
		return "Codex 当前离线"
	case !in.Runtime.HasLocalAction("set_mode"):
		return "当前 Codex 插件未声明 set_mode，请升级并重启 grix-codex"
	default:
		return "切换 Codex 协作模式"
	}
}

func compactTooltip(in core.BuildInput) string {
	switch {
	case !in.Runtime.Online:
		return "Codex 当前离线"
	case !in.Runtime.HasLocalAction("thread_compact"):
		return "当前插件未声明 thread_compact"
	default:
		return "压缩当前 Codex 上下文"
	}
}

func sessionUsageTooltip(in core.BuildInput) string {
	switch {
	case !in.Runtime.Online:
		return "Codex 当前离线"
	case !in.Runtime.HasLocalAction("get_session_usage"):
		return "当前插件未声明 get_session_usage"
	default:
		return "查询当前会话用量"
	}
}

const protocolStateStopping = "stopping"

func stopOutputTooltip(runState string) string {
	if strings.TrimSpace(runState) == protocolStateStopping {
		return "正在停止当前输出"
	}
	return "停止当前输出"
}

func normalizeModeID(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "plan":
		return "plan"
	case "default":
		return "default"
	default:
		return ""
	}
}

func modeDisplayLabel(modeID string) string {
	if normalizeModeID(modeID) == "plan" {
		return "计划"
	}
	return "自动"
}

func handleSelectReasoningEffort(in core.ActionInput) (toolprotocol.ActionResult, error) {
	effort := strings.TrimSpace(in.Request.OptionID)
	if effort == "" {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_option",
			Message: "未选择推理力度",
		}, nil
	}
	if !canSelectReasoningEffort(in.BuildInput, buildEffortOptions(in.BuildInput.Binding.Meta)) {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "action_unavailable",
			Message: effortTooltip(in.BuildInput, buildEffortOptions(in.BuildInput.Binding.Meta)),
		}, nil
	}
	return dispatchLocalAction(in, "set_reasoning_effort", map[string]any{
		"session_id":      in.BuildInput.Session.SessionID,
		"reasoning_effort": effort,
		"actor_id":        fmt.Sprintf("%d", in.BuildInput.OwnerID),
	}, 15_000, "已提交推理力度切换请求")
}

func buildEffortOptions(meta map[string]any) []toolprotocol.Option {
	if meta == nil {
		return nil
	}
	raw, ok := meta["available_efforts"]
	if !ok || raw == nil {
		return nil
	}
	efforts, ok := raw.([]any)
	if !ok {
		return nil
	}
	opts := make([]toolprotocol.Option, 0, len(efforts))
	for _, raw := range efforts {
		effort, ok := raw.(string)
		if !ok {
			continue
		}
		effort = strings.TrimSpace(effort)
		if effort == "" {
			continue
		}
		opts = append(opts, toolprotocol.Option{
			OptionID: effort,
			Label:    effortDisplayLabel(effort),
		})
	}
	return opts
}

func resolveDefaultEffort(modelOptions []modelOption, currentModelID string) string {
	modelID := strings.TrimSpace(currentModelID)
	for _, m := range modelOptions {
		if strings.EqualFold(m.ID, modelID) {
			return ""
		}
	}
	return ""
}

func resolveEffortLabel(currentEffort string, options []toolprotocol.Option) string {
	currentEffort = strings.TrimSpace(currentEffort)
	for _, opt := range options {
		if opt.OptionID == currentEffort {
			return opt.Label
		}
	}
	if currentEffort != "" {
		return effortDisplayLabel(currentEffort)
	}
	if len(options) > 0 {
		return options[0].Label
	}
	return "推理力度"
}

func effortDisplayLabel(effortID string) string {
	switch strings.TrimSpace(strings.ToLower(effortID)) {
	case "none":
		return "无"
	case "minimal":
		return "极低"
	case "low":
		return "低"
	case "medium":
		return "中"
	case "high":
		return "高"
	case "xhigh":
		return "极高"
	case "max":
		return "最大"
	case "ultra":
		return "极限"
	default:
		return effortID
	}
}

func canSelectReasoningEffort(in core.BuildInput, options []toolprotocol.Option) bool {
	return in.Runtime.Online && in.Runtime.HasLocalAction("set_reasoning_effort") && len(options) > 0
}

func effortTooltip(in core.BuildInput, options []toolprotocol.Option) string {
	switch {
	case !in.Runtime.Online:
		return "Codex 当前离线"
	case !in.Runtime.HasLocalAction("set_reasoning_effort"):
		return "当前插件未声明 set_reasoning_effort，请升级并重启 grix-connector"
	case len(options) == 0:
		return "当前模型不支持调整推理力度"
	default:
		return "切换推理力度"
	}
}

func handleSelectServiceTier(in core.ActionInput) (toolprotocol.ActionResult, error) {
	tier := strings.TrimSpace(in.Request.OptionID)
	if tier == "" {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_option",
			Message: "未选择速度档",
		}, nil
	}
	if !canSelectServiceTier(in.BuildInput, buildServiceTierOptions(in.BuildInput.Binding.Meta)) {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "action_unavailable",
			Message: serviceTierTooltip(in.BuildInput, buildServiceTierOptions(in.BuildInput.Binding.Meta)),
		}, nil
	}
	tierOptions := append(
		[]toolprotocol.Option{{OptionID: "default", Label: serviceTierDisplayLabel("default", "")}},
		buildServiceTierOptions(in.BuildInput.Binding.Meta)...,
	)
	return dispatchLocalAction(in, "set_service_tier", map[string]any{
		"session_id":    in.BuildInput.Session.SessionID,
		"service_tier":  tier,
		"display_label": resolveServiceTierLabel(tier, tierOptions),
		"actor_id":      fmt.Sprintf("%d", in.BuildInput.OwnerID),
	}, 15_000, "已提交速度档切换请求")
}

// buildServiceTierOptions 解析连接器下发的 available_service_tiers：
// [{id, displayName, description}]，不含标准档（由构建方补 default 选项）。
func buildServiceTierOptions(meta map[string]any) []toolprotocol.Option {
	if meta == nil {
		return nil
	}
	raw, ok := meta["available_service_tiers"]
	if !ok || raw == nil {
		return nil
	}
	tiers, ok := raw.([]any)
	if !ok {
		return nil
	}
	opts := make([]toolprotocol.Option, 0, len(tiers))
	seen := map[string]struct{}{}
	for _, entry := range tiers {
		record, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		id := metaString(record, "id")
		if id == "" || strings.EqualFold(id, "default") {
			continue
		}
		key := strings.ToLower(id)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		opts = append(opts, toolprotocol.Option{
			OptionID: id,
			Label:    serviceTierDisplayLabel(id, metaString(record, "displayName")),
		})
	}
	return opts
}

func resolveServiceTierLabel(currentTier string, options []toolprotocol.Option) string {
	tier := strings.TrimSpace(currentTier)
	if tier == "" {
		tier = "default"
	}
	for _, opt := range options {
		if strings.EqualFold(opt.OptionID, tier) {
			return opt.Label
		}
	}
	return serviceTierDisplayLabel(tier, "")
}

// serviceTierDisplayLabel 以插件下发的 displayName 为准：倍率、档位名都是 OpenAI 的策略，
// 写死在这里一旦对方调整就成了发假信息。本地只兜底「标准」档（它是我们自己补的选项，
// 插件不会下发）和拿不到名字时的原值回退。
func serviceTierDisplayLabel(tierID, displayName string) string {
	if name := strings.TrimSpace(displayName); name != "" {
		return name
	}
	switch strings.TrimSpace(strings.ToLower(tierID)) {
	case "default", "":
		return "标准"
	case "priority":
		return "快速"
	}
	return tierID
}

func canSelectServiceTier(in core.BuildInput, options []toolprotocol.Option) bool {
	return in.Runtime.Online && in.Runtime.HasLocalAction("set_service_tier") && len(options) > 0
}

func serviceTierTooltip(in core.BuildInput, options []toolprotocol.Option) string {
	switch {
	case !in.Runtime.Online:
		return "Codex 当前离线"
	case !in.Runtime.HasLocalAction("set_service_tier"):
		return "当前插件未声明 set_service_tier，请升级并重启 grix-connector"
	case len(options) == 0:
		return "当前模型不支持速度档"
	default:
		return "切换速度档（快速档 1.5x 速度，消耗更多额度）"
	}
}

func handleSelectSandboxMode(in core.ActionInput) (toolprotocol.ActionResult, error) {
	sandboxMode := strings.TrimSpace(in.Request.OptionID)
	if sandboxMode == "" {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_option",
			Message: "未选择沙箱模式",
		}, nil
	}
	if !canSelectSandboxMode(in.BuildInput) {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "action_unavailable",
			Message: sandboxModeTooltip(in.BuildInput),
		}, nil
	}
	return dispatchLocalAction(in, "set_sandbox_mode", map[string]any{
		"session_id":   in.BuildInput.Session.SessionID,
		"sandbox_mode": sandboxMode,
		"actor_id":     fmt.Sprintf("%d", in.BuildInput.OwnerID),
	}, 15_000, "已提交沙箱模式切换请求（重启后生效）")
}

func canSelectSandboxMode(in core.BuildInput) bool {
	return in.Runtime.Online && in.Runtime.HasLocalAction("set_sandbox_mode")
}

func sandboxModeTooltip(in core.BuildInput) string {
	switch {
	case !in.Runtime.Online:
		return "Codex 当前离线"
	case !in.Runtime.HasLocalAction("set_sandbox_mode"):
		return "当前插件未声明 set_sandbox_mode，请升级并重启 grix-connector"
	default:
		return "切换沙箱模式（重启后生效）"
	}
}

func normalizeSandboxModeID(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "default", "":
		return "default"
	case "danger-full-access":
		return "danger-full-access"
	case "workspace-write":
		return "workspace-write"
	case "read-only":
		return "read-only"
	default:
		return ""
	}
}

func sandboxModeDisplayLabel(modeID string) string {
	switch strings.TrimSpace(strings.ToLower(modeID)) {
	case "default", "":
		return "默认"
	case "danger-full-access":
		return "完全访问"
	case "workspace-write":
		return "工作区写入"
	case "read-only":
		return "只读"
	default:
		return modeID
	}
}
func metaString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	value, ok := meta[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func hasCodexSessionBinding(binding core.BindingInfo) bool {
	return strings.TrimSpace(binding.BindingID) != "" ||
		strings.TrimSpace(binding.Cwd) != ""
}

func metaFloat64(meta map[string]any, key string) float64 {
	if meta == nil {
		return 0
	}
	value, ok := meta[key]
	if !ok || value == nil {
		return 0
	}
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return 0
	}
}
