package claude

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/askie/grix/backend/internal/agenttoolbar/agents/shared"
	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	"github.com/askie/grix/backend/internal/model"
)

type Package struct{}

type claudeModelOption struct {
	ID    string
	Label string
}

const (
	claudeModeApproval = "approval"
	claudeModeFullAuto = "full_auto"
)

var claudeModeOptions = []toolprotocol.Option{
	{OptionID: claudeModeFullAuto, Label: "全自动"},
	{OptionID: claudeModeApproval, Label: "审批"},
}

func New() *Package            { return &Package{} }
func (p *Package) Key() string { return model.AgentClientTypeClaude }
func (p *Package) Match(ctx core.MatchContext) bool {
	return ctx.Agent.ClientType == model.AgentClientTypeClaude
}
func (p *Package) Build(ctx context.Context, in core.BuildInput) (toolprotocol.Snapshot, error) {
	if !hasClaudeSessionBinding(in.Binding) {
		return toolprotocol.Snapshot{
			Visible: false,
			Items:   []toolprotocol.Item{},
		}, nil
	}

	items := make([]toolprotocol.Item, 0, 5)

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
			Tooltip:  claudeStopOutputTooltip(runState),
			Loading:  runState == "stopping",
			Selected: runState == "stopping",
		})
	}

	sessionDisabled := !in.Runtime.Online || !in.Runtime.HasLocalAction("session_control")
	sessionTooltip := "Claude 会话操作"
	switch {
	case !in.Runtime.Online:
		sessionTooltip = "Claude 当前离线"
	case !in.Runtime.HasLocalAction("session_control"):
		sessionTooltip = "当前插件未声明 session_control"
	case strings.TrimSpace(in.Binding.Cwd) != "":
		sessionTooltip = "Claude 会话操作\n工作目录: " + strings.TrimSpace(in.Binding.Cwd)
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

	modelOptions := buildClaudeModelOptions(in.Binding.Meta)
	currentModelID := metaString(in.Binding.Meta, "model_id")
	currentModelLabel := resolveClaudeModelLabel(currentModelID, modelOptions)

	modeID := normalizeClaudeModeID(metaString(in.Binding.Meta, "mode_id"))
	modeDisabled := !in.Runtime.Online || !in.Runtime.HasLocalAction("set_mode")
	modeTooltip := "切换 Claude 执行模式并重启会话"
	switch {
	case !in.Runtime.Online:
		modeTooltip = "Claude 当前离线"
	case !in.Runtime.HasLocalAction("set_mode"):
		modeTooltip = "当前插件未声明 set_mode，请升级并重启 grix-connector"
	}
	usageDisabled := !in.Runtime.Online || !in.Runtime.HasLocalAction("get_session_usage")
	sessionOpts := []toolprotocol.Option{
		{OptionID: "status", Label: "查看状态"},
		{OptionID: "restart", Label: "重启会话"},
		{
			OptionID: "usage",
			Label:    "查看用量",
			Disabled: usageDisabled,
		},
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
			Options:     sessionOpts,
		},
	)

	rateLimitItems := buildClaudeRateLimitItems(in)
	if len(rateLimitItems) > 0 {
		items = append(items, rateLimitItems...)
	}

	items = append(items,
		toolprotocol.Item{
			ItemID:      "select_model",
			GroupID:     "model_control",
			Kind:        toolprotocol.ItemKindSelect,
			ActionID:    "select_model",
			Label:       currentModelLabel,
			Value:       currentModelID,
			Icon:        "cpu",
			Variant:     "secondary",
			Disabled:    !canClaudeSelectModel(in, modelOptions),
			Tooltip:     claudeModelTooltip(in, modelOptions),
			Placeholder: "模型",
			Options:     toClaudeProtocolOptions(modelOptions),
		},
	)

	effortOptions := buildClaudeEffortOptions(in.Binding.Meta)
	if len(effortOptions) > 0 {
		currentEffort := claudeCurrentEffort(in.Binding.Meta)
		currentEffortID := resolveClaudeEffortID(currentEffort, effortOptions)
		currentEffortLabel := resolveClaudeEffortLabel(currentEffortID, effortOptions)
		items = append(items, toolprotocol.Item{
			ItemID:      "select_reasoning_effort",
			GroupID:     "effort_control",
			Kind:        toolprotocol.ItemKindSelect,
			ActionID:    "select_reasoning_effort",
			Label:       currentEffortLabel,
			Value:       currentEffortID,
			Icon:        "spark",
			Variant:     "secondary",
			Disabled:    !canClaudeSelectReasoningEffort(in, effortOptions),
			Tooltip:     claudeReasoningEffortTooltip(in, effortOptions),
			Placeholder: "推理力度",
			Options:     toClaudeProtocolOptions(effortOptions),
		})
	}

	items = append(items,
		toolprotocol.Item{
			ItemID:      "select_mode",
			GroupID:     "mode_control",
			Kind:        toolprotocol.ItemKindSelect,
			ActionID:    "select_mode",
			Icon:        "shield",
			Variant:     "secondary",
			Disabled:    modeDisabled,
			Tooltip:     modeTooltip,
			Value:       modeID,
			BadgeText:   claudeModeDisplayLabel(modeID),
			Placeholder: "选择模式",
			Options:     claudeModeOptions,
		},
		buildClaudeContextCompactProgressItem(in),
	)

	if len(in.Runtime.Skills) > 0 {
		items = append(items, shared.BuildSkillsItem(in.Runtime.Skills))
	}

	if item, ok := shared.BuildSlashCommandsItem("claude"); ok {
		items = append([]toolprotocol.Item{item}, items...)
	}

	return toolprotocol.Snapshot{
		Visible: true,
		Items:   items,
	}, nil
}
func (p *Package) HandleAction(ctx context.Context, in core.ActionInput) (toolprotocol.ActionResult, error) {
	switch strings.TrimSpace(in.Request.ActionID) {
	case "stop_output":
		return handleStopOutput(in)
	case "session_control":
		return handleSessionControl(in)
	case "get_session_usage":
		return handleGetSessionUsage(in)
	case "get_rate_limits":
		return handleGetRateLimits(in)
	case "select_model":
		return handleClaudeSelectModel(in)
	case "select_reasoning_effort":
		return handleClaudeSelectReasoningEffort(in)
	case "select_mode":
		return handleSelectMode(in)
	case "thread_compact":
		return handleClaudeCompactThread(in)
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
	optionID := strings.TrimSpace(in.Request.OptionID)

	switch optionID {
	case "status", "restart", "stop":
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
	params := map[string]any{
		"session_id": in.BuildInput.Session.SessionID,
		"verb":       optionID,
	}
	timeoutMs := 15_000
	msg := "已提交会话操作"
	if optionID == "restart" {
		params["display_label"] = "重启"
		timeoutMs = 30_000
		msg = "已提交重启请求"
	}
	return dispatchLocalAction(in, "session_control", params, timeoutMs, msg)
}

func handleClaudeSelectModel(in core.ActionInput) (toolprotocol.ActionResult, error) {
	modelID := strings.TrimSpace(in.Request.OptionID)
	if modelID == "" {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_option",
			Message: "未选择模型",
		}, nil
	}
	if !canClaudeSelectModel(in.BuildInput, buildClaudeModelOptions(in.BuildInput.Binding.Meta)) {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "action_unavailable",
			Message: claudeModelTooltip(in.BuildInput, buildClaudeModelOptions(in.BuildInput.Binding.Meta)),
		}, nil
	}
	return dispatchLocalAction(in, "set_model", map[string]any{
		"session_id": in.BuildInput.Session.SessionID,
		"model_id":   modelID,
	}, 15_000, "已提交模型切换请求")
}

func handleClaudeSelectReasoningEffort(in core.ActionInput) (toolprotocol.ActionResult, error) {
	effort := strings.ToLower(strings.TrimSpace(in.Request.OptionID))
	if effort == "" {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_option",
			Message: "未选择推理力度",
		}, nil
	}
	effortOptions := buildClaudeEffortOptions(in.BuildInput.Binding.Meta)
	if !canClaudeSelectReasoningEffort(in.BuildInput, effortOptions) {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "action_unavailable",
			Message: claudeReasoningEffortTooltip(in.BuildInput, effortOptions),
		}, nil
	}
	if !hasClaudeEffortOption(effort, effortOptions) {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_option",
			Message: "工具栏选项无效",
		}, nil
	}

	// effort 是 Claude 的 canonical 参数；reasoning_effort 保留给旧 connector。
	// auto 由 connector 解释为清除覆盖并回到模型默认，不在 aibot 侧保存业务状态。
	return dispatchLocalAction(in, "set_reasoning_effort", map[string]any{
		"session_id":       in.BuildInput.Session.SessionID,
		"effort":           effort,
		"reasoning_effort": effort,
	}, 15_000, "已提交推理力度切换请求")
}

func handleSelectMode(in core.ActionInput) (toolprotocol.ActionResult, error) {
	modeID := normalizeClaudeModeID(in.Request.OptionID)
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
		"display_label": claudeModeDisplayLabel(modeID),
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

func buildClaudeModelOptions(meta map[string]any) []claudeModelOption {
	models, ok := meta["available_models"].([]any)
	if !ok {
		return nil
	}
	opts := make([]claudeModelOption, 0, len(models))
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
		opts = append(opts, claudeModelOption{ID: id, Label: label})
	}
	return opts
}

func buildClaudeEffortOptions(meta map[string]any) []claudeModelOption {
	if meta == nil {
		return nil
	}
	raw, ok := meta["available_efforts"]
	if !ok || raw == nil {
		return nil
	}

	var efforts []any
	switch typed := raw.(type) {
	case []any:
		efforts = typed
	case []string:
		efforts = make([]any, len(typed))
		for i, effort := range typed {
			efforts[i] = effort
		}
	default:
		return nil
	}

	opts := make([]claudeModelOption, 0, len(efforts))
	seen := make(map[string]struct{}, len(efforts))
	for _, rawEffort := range efforts {
		effort, ok := rawEffort.(string)
		if !ok {
			continue
		}
		effort = strings.ToLower(strings.TrimSpace(effort))
		if effort == "" {
			continue
		}
		if _, exists := seen[effort]; exists {
			continue
		}
		seen[effort] = struct{}{}
		opts = append(opts, claudeModelOption{
			ID:    effort,
			Label: claudeEffortDisplayLabel(effort),
		})
	}
	return opts
}

func claudeCurrentEffort(meta map[string]any) string {
	if meta != nil {
		if _, ok := meta["effort"]; ok {
			return strings.ToLower(strings.TrimSpace(metaString(meta, "effort")))
		}
	}
	return strings.ToLower(strings.TrimSpace(metaString(meta, "reasoning_effort")))
}

func resolveClaudeEffortID(currentEffort string, options []claudeModelOption) string {
	currentEffort = strings.ToLower(strings.TrimSpace(currentEffort))
	for _, option := range options {
		if strings.EqualFold(option.ID, currentEffort) {
			return option.ID
		}
	}
	if len(options) > 0 {
		return options[0].ID
	}
	return ""
}

func resolveClaudeEffortLabel(currentEffort string, options []claudeModelOption) string {
	currentEffort = strings.ToLower(strings.TrimSpace(currentEffort))
	for _, option := range options {
		if strings.EqualFold(option.ID, currentEffort) {
			return option.Label
		}
	}
	if len(options) > 0 {
		return options[0].Label
	}
	return "推理力度"
}

func hasClaudeEffortOption(effort string, options []claudeModelOption) bool {
	for _, option := range options {
		if strings.EqualFold(option.ID, strings.TrimSpace(effort)) {
			return true
		}
	}
	return false
}

func claudeEffortDisplayLabel(effort string) string {
	switch strings.ToLower(strings.TrimSpace(effort)) {
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
	case "auto":
		return "自动"
	default:
		return strings.TrimSpace(effort)
	}
}

func canClaudeSelectReasoningEffort(in core.BuildInput, options []claudeModelOption) bool {
	return in.Runtime.Online && in.Runtime.HasLocalAction("set_reasoning_effort") && len(options) > 0
}

func claudeReasoningEffortTooltip(in core.BuildInput, options []claudeModelOption) string {
	switch {
	case !in.Runtime.Online:
		return "Claude 当前离线"
	case !in.Runtime.HasLocalAction("set_reasoning_effort"):
		return "当前插件未声明 set_reasoning_effort，请升级并重启 grix-connector"
	case len(options) == 0:
		return "当前模型不支持调整推理力度"
	default:
		return "切换推理力度"
	}
}

func resolveClaudeModelLabel(modelID string, options []claudeModelOption) string {
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

func toClaudeProtocolOptions(options []claudeModelOption) []toolprotocol.Option {
	out := make([]toolprotocol.Option, 0, len(options))
	for _, option := range options {
		out = append(out, toolprotocol.Option{
			OptionID: option.ID,
			Label:    option.Label,
		})
	}
	return out
}

func canClaudeSelectModel(in core.BuildInput, options []claudeModelOption) bool {
	return in.Runtime.Online && in.Runtime.HasLocalAction("set_model") && len(options) > 0
}

func claudeModelTooltip(in core.BuildInput, options []claudeModelOption) string {
	switch {
	case !in.Runtime.Online:
		return "Claude 当前离线"
	case !in.Runtime.HasLocalAction("set_model"):
		return "当前插件未声明 set_model，请升级并重启 grix-connector"
	case len(options) == 0:
		return "等待 Claude 模型列表同步"
	default:
		return "切换 Claude 模型（重启会话生效）"
	}
}

func normalizeClaudeModeID(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "approval":
		return claudeModeApproval
	default:
		return claudeModeFullAuto
	}
}

func claudeModeDisplayLabel(modeID string) string {
	if normalizeClaudeModeID(modeID) == claudeModeFullAuto {
		return "全自动"
	}
	return "审批"
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

func claudeStopOutputTooltip(runState string) string {
	if strings.TrimSpace(runState) == "stopping" {
		return "正在停止当前输出"
	}
	return "停止当前输出"
}

func hasClaudeSessionBinding(binding core.BindingInfo) bool {
	return strings.TrimSpace(binding.BindingID) != "" ||
		strings.TrimSpace(binding.Cwd) != ""
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
	}, 20_000, "已提交用量查询请求")
}

type claudeRateLimitWindow struct {
	UsedPercentage float64
	ResetsAt       int64
}

func buildClaudeRateLimitItems(in core.BuildInput) []toolprotocol.Item {
	if !in.Runtime.Online || !in.Runtime.HasLocalAction("get_rate_limits") {
		return nil
	}
	limits := parseClaudeRateLimits(in.Binding.Meta)
	if limits == nil {
		return nil
	}
	var items []toolprotocol.Item
	// 用量为 0 的 5H/7D 限额不渲染。
	if fh, ok := limits["fiveHour"]; ok && fh.ResetsAt > 0 && fh.UsedPercentage != 0 {
		items = append(items, buildClaudeRateLimitProgressItem("rate_limit_5h", "rate_limits", "5H", "rate_limit_5h_usage", fh.UsedPercentage, fh.ResetsAt))
	}
	if sd, ok := limits["sevenDay"]; ok && sd.ResetsAt > 0 && sd.UsedPercentage != 0 {
		items = append(items, buildClaudeRateLimitProgressItem("rate_limit_7d", "rate_limits", "7D", "rate_limit_7d_usage", sd.UsedPercentage, sd.ResetsAt))
	}

	// Credits — agent-agnostic key from meta["credits"]
	if credits := shared.ParseCredits(in.Binding.Meta); credits != nil && credits.ShouldShow() {
		items = append(items, buildClaudeCreditsItem(credits))
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
			items = append(items, buildClaudeRateLimitProgressItem(itemID, "rate_limits", centerText, extra.Label, extra.UsedPercent, 0))
			items[len(items)-1].ProgressDetail = detail
		}
	}

	return items
}

func buildClaudeCreditsItem(credits *shared.CreditsInfo) toolprotocol.Item {
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

func buildClaudeRateLimitProgressItem(itemID, groupID, centerText, descPrefix string, percent float64, resetsAt int64) toolprotocol.Item {
	progressDetail := ""
	if resetsAt > 0 {
		progressDetail = fmt.Sprintf("%d", resetsAt)
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

func parseClaudeRateLimits(meta map[string]any) map[string]claudeRateLimitWindow {
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
	if parseNumber(limitsMap["sampledAt"]) <= 0 {
		return nil
	}
	result := make(map[string]claudeRateLimitWindow, 2)
	for _, key := range []string{"fiveHour", "sevenDay"} {
		entry := nestedMap(limitsMap, key, toSnakeCaseKey(key))
		if len(entry) == 0 {
			continue
		}
		pct := parseNumber(entry["usedPercentage"])
		if pct <= 0 {
			pct = parseNumber(entry["used_percent"])
		}
		if pct <= 0 {
			pct = parseNumber(entry["used_percentage"])
		}
		resetsAt := parseNumber(entry["resetsAt"])
		if resetsAt <= 0 {
			resetsAt = parseNumber(entry["resets_at"])
		}
		if resetsAt <= 0 {
			continue
		}
		result[key] = claudeRateLimitWindow{
			UsedPercentage: pct,
			ResetsAt:       int64(resetsAt),
		}
	}
	return result
}

func nestedMap(root map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		entry, ok := root[key].(map[string]any)
		if !ok {
			continue
		}
		return entry
	}
	return nil
}

func parseNumber(v any) float64 {
	switch value := v.(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	case int32:
		return float64(value)
	case uint64:
		return float64(value)
	case uint32:
		return float64(value)
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return 0
		}
		parsed, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}

func toSnakeCaseKey(camel string) string {
	if camel == "" {
		return ""
	}
	var b strings.Builder
	for i, r := range camel {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func canClaudeCompactThread(in core.BuildInput) bool {
	return in.Runtime.Online && in.Runtime.HasLocalAction("thread_compact")
}

func claudeCompactTooltip(in core.BuildInput) string {
	switch {
	case !in.Runtime.Online:
		return "Claude 当前离线"
	case !in.Runtime.HasLocalAction("thread_compact"):
		return "当前插件未声明 thread_compact"
	default:
		return "压缩当前会话上下文"
	}
}

func handleClaudeCompactThread(in core.ActionInput) (toolprotocol.ActionResult, error) {
	if !canClaudeCompactThread(in.BuildInput) {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "action_unavailable",
			Message: claudeCompactTooltip(in.BuildInput),
		}, nil
	}
	return dispatchLocalAction(in, "thread_compact", map[string]any{
		"session_id": in.BuildInput.Session.SessionID,
		"actor_id":   in.BuildInput.OwnerID,
	}, 120_000, "已提交压缩请求")
}

const claudeContextWarningThreshold = 80.0

type claudeContextWindow struct {
	UsedPercentage      float64
	RemainingPercentage float64
	HasRemaining        bool
}

func parseClaudeContextWindow(meta map[string]any) claudeContextWindow {
	if meta == nil {
		return claudeContextWindow{}
	}
	raw, ok := meta["context_window"]
	if !ok || raw == nil {
		return claudeContextWindow{}
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return claudeContextWindow{}
	}
	return claudeContextWindow{
		UsedPercentage:      parseNumber(obj["usedPercentage"]),
		RemainingPercentage: parseNumber(obj["remainingPercentage"]),
		HasRemaining:        obj["remainingPercentage"] != nil,
	}
}

func buildClaudeContextCompactProgressItem(in core.BuildInput) toolprotocol.Item {
	ctx := parseClaudeContextWindow(in.Binding.Meta)
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
	variant := "secondary"
	if percent >= claudeContextWarningThreshold {
		variant = "warning"
	}
	return toolprotocol.Item{
		ItemID:         "thread_compact",
		GroupID:        "session_control",
		Kind:           toolprotocol.ItemKindProgress,
		ActionID:       "thread_compact",
		Variant:        variant,
		Percent:        percent,
		CenterText:     shared.PercentCenterText(percent),
		ProgressDesc:   "会话上下文",
		ProgressDetail: detail,
		Tooltip:        claudeCompactTooltip(in),
		Disabled:       !canClaudeCompactThread(in),
		LocalAction:    "thread_compact",
	}
}
