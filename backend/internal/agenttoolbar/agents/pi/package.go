package pi

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
func (p *Package) Key() string { return model.AgentClientTypePi }
func (p *Package) Match(ctx core.MatchContext) bool {
	return ctx.Agent.ClientType == model.AgentClientTypePi
}

func (p *Package) Build(_ context.Context, in core.BuildInput) (toolprotocol.Snapshot, error) {
	if !hasPiSessionBinding(in.Binding) {
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
			Tooltip:  piStopOutputTooltip(runState),
			Loading:  runState == "stopping",
			Selected: runState == "stopping",
		})
	}

	sessionDisabled := !in.Runtime.Online || !in.Runtime.HasLocalAction("session_control")
	sessionTooltip := "Pi 会话操作"
	switch {
	case !in.Runtime.Online:
		sessionTooltip = "Pi 当前离线"
	case !in.Runtime.HasLocalAction("session_control"):
		sessionTooltip = "当前插件未声明 session_control"
	case strings.TrimSpace(in.Binding.Cwd) != "":
		sessionTooltip = "Pi 会话操作\n工作目录: " + strings.TrimSpace(in.Binding.Cwd)
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
				{OptionID: "restart", Label: "重启会话"},
				{OptionID: "unbind", Label: "解绑"},
				{
					OptionID: "usage",
					Label:    "查看用量",
					Disabled: usageDisabled,
				},
			},
		},
	)
	if providerItem, ok := piProviderItem(in); ok {
		items = append(items, providerItem)
	}
	items = append(items, shared.BuildSelect(in, piModelSelect(in)))

	// 厂商限额（5h/周）：读 binding meta 的 provider_quota（connector 经 models.json 反查后下发）。
	if quotaItems := shared.BuildProviderQuotaItems(
		shared.ParseProviderQuota(in.Binding.Meta),
		in.Runtime.HasLocalAction("get_rate_limits"),
	); len(quotaItems) > 0 {
		items = append(items, quotaItems...)
	}

	if len(in.Runtime.Skills) > 0 {
		items = append(items, shared.BuildSkillsItem(in.Runtime.Skills))
	}

	if item, ok := shared.BuildSlashCommandsItem("pi"); ok {
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
	case "session_control":
		return handleSessionControl(in)
	case "get_session_usage":
		return handleGetSessionUsage(in)
	case "select_provider":
		return handleSelectProvider(in)
	case "select_model":
		return handleSelectModel(in)
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
	case "status", "restart", "stop", "unbind":
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

func handleSelectProvider(in core.ActionInput) (toolprotocol.ActionResult, error) {
	providerID := strings.TrimSpace(in.Request.OptionID)
	if providerID == "" {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_option",
			Message: "未选择供应商",
		}, nil
	}
	spec, ok := piProviderSelect(in.BuildInput)
	if !ok {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "action_unavailable",
			Message: "当前没有可切换的供应商",
		}, nil
	}
	if item := shared.BuildSelect(in.BuildInput, spec); item.Disabled {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "action_unavailable",
			Message: item.Tooltip,
		}, nil
	}
	if in.BuildInput.Run.HasActiveRun {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "run_active",
			Message: piRunActiveTooltip("供应商"),
		}, nil
	}
	label, known := piProviderLabel(providerID, spec.Options)
	if !known {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_option",
			Message: "供应商不在当前可用列表中",
		}, nil
	}
	return dispatchLocalAction(in, "set_provider", map[string]any{
		"session_id":    in.BuildInput.Session.SessionID,
		"provider_id":   providerID,
		"display_label": label,
	}, 15_000, "已提交供应商切换请求")
}

func handleSelectModel(in core.ActionInput) (toolprotocol.ActionResult, error) {
	optionID := strings.TrimSpace(in.Request.OptionID)
	if optionID == "" {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_option",
			Message: "未选择模型",
		}, nil
	}
	if item := shared.BuildSelect(in.BuildInput, piModelSelect(in.BuildInput)); item.Disabled {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "action_unavailable",
			Message: item.Tooltip,
		}, nil
	}
	if in.BuildInput.Run.HasActiveRun {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "run_active",
			Message: piRunActiveTooltip("模型"),
		}, nil
	}
	// 反查用未过滤的目录：快照按当前供应商过滤，但选项 id 仍要能定位到原始条目。
	selected := resolvePiModelOption(optionID, piModelOptions(in.BuildInput.Binding.Meta))
	params := map[string]any{
		"session_id":    in.BuildInput.Session.SessionID,
		"model_id":      selected.id,
		"display_label": selected.label,
	}
	if selected.provider != "" {
		params["provider"] = selected.provider
	}
	return dispatchLocalAction(in, "set_model", params, 15_000, "已提交模型切换请求")
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

func piStopOutputTooltip(runState string) string {
	if strings.TrimSpace(runState) == "stopping" {
		return "正在停止当前输出"
	}
	return "停止当前输出"
}

func hasPiSessionBinding(binding core.BindingInfo) bool {
	return strings.TrimSpace(binding.BindingID) != "" ||
		strings.TrimSpace(binding.Cwd) != ""
}

// piModelOption 是一条模型目录项；optionID 在连接器上报 provider 时是
// "provider:model"，老连接器（无 provider 字段）保持裸 model id。
type piModelOption struct {
	optionID string
	id       string
	label    string
	provider string
}

// piCurrentProvider 当前生效供应商：provider_id 由 set_provider 回执写入，
// model_provider 是插件快照的历史键，两者兼容读取。
func piCurrentProvider(meta map[string]any) string {
	if provider := shared.MetaString(meta, "provider_id"); provider != "" {
		return provider
	}
	return shared.MetaString(meta, "model_provider")
}

// piProviderSelect 组装供应商选择器；连接器未上报 available_providers 时不渲染。
func piProviderSelect(in core.BuildInput) (shared.SelectSpec, bool) {
	options := shared.ParseMetaOptions(in.Binding.Meta, "available_providers")
	if len(options) == 0 {
		return shared.SelectSpec{}, false
	}
	providerID := piCurrentProvider(in.Binding.Meta)
	label, _ := piProviderLabel(providerID, options)
	spec := shared.ProviderSelect("Pi")
	spec.Label = label
	spec.Value = providerID
	spec.Options = options
	return spec, true
}

// piProviderItem 在 BuildSelect 的离线/未声明判定之外补上"有任务运行中"禁用。
func piProviderItem(in core.BuildInput) (toolprotocol.Item, bool) {
	spec, ok := piProviderSelect(in)
	if !ok {
		return toolprotocol.Item{}, false
	}
	item := shared.BuildSelect(in, spec)
	if !item.Disabled && in.Run.HasActiveRun {
		item.Disabled = true
		item.Tooltip = piRunActiveTooltip("供应商")
	}
	return item, true
}

func piProviderLabel(providerID string, options []toolprotocol.Option) (string, bool) {
	id := strings.TrimSpace(providerID)
	if id == "" {
		return "", false
	}
	for _, option := range options {
		if option.OptionID == id {
			return option.Label, true
		}
	}
	return id, false
}

func piRunActiveTooltip(noun string) string {
	return "当前有任务运行中，完成后可切换" + noun
}

// piModelOptions 解析 available_models：按 optionID 忽略大小写去重，
// 缺显示名时回落 id，带 provider 时把 provider 前缀进 optionID。
func piModelOptions(meta map[string]any) []piModelOption {
	if len(meta) == 0 {
		return nil
	}
	list, ok := meta["available_models"].([]any)
	if !ok {
		return nil
	}
	out := make([]piModelOption, 0, len(list))
	seen := map[string]struct{}{}
	for _, raw := range list {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id := shared.MetaString(entry, "id")
		if id == "" {
			continue
		}
		provider := ""
		for _, providerKey := range []string{"provider", "provider_id", "model_provider"} {
			if provider = shared.MetaString(entry, providerKey); provider != "" {
				break
			}
		}
		optionID := id
		if provider != "" {
			optionID = provider + ":" + id
		}
		if _, dup := seen[strings.ToLower(optionID)]; dup {
			continue
		}
		seen[strings.ToLower(optionID)] = struct{}{}
		label := ""
		for _, labelKey := range []string{"display_name", "displayName", "label", "name"} {
			if label = shared.MetaString(entry, labelKey); label != "" {
				break
			}
		}
		if label == "" {
			label = id
		}
		out = append(out, piModelOption{optionID: optionID, id: id, label: label, provider: provider})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// piFilterModelOptions 已知当前供应商且目录里有它的模型时，只保留该供应商的模型。
func piFilterModelOptions(options []piModelOption, provider string) []piModelOption {
	if provider == "" {
		return options
	}
	filtered := make([]piModelOption, 0, len(options))
	for _, option := range options {
		if option.provider == provider {
			filtered = append(filtered, option)
		}
	}
	if len(filtered) == 0 {
		return options
	}
	return filtered
}

func resolvePiModelOption(optionID string, options []piModelOption) piModelOption {
	normalized := strings.TrimSpace(optionID)
	for _, option := range options {
		if option.optionID == normalized {
			return option
		}
	}
	// 目录里找不到（例如快照已过期）：只有目录带 provider 时才按 "provider:model"
	// 拆分，避免把老连接器里本就带冒号的裸 model id 拆坏。
	for _, option := range options {
		if option.provider == "" {
			continue
		}
		if provider, id, found := strings.Cut(normalized, ":"); found && provider != "" && id != "" {
			return piModelOption{optionID: normalized, id: id, label: id, provider: provider}
		}
		break
	}
	return piModelOption{optionID: normalized, id: normalized, label: normalized}
}

// piModelLabel 当前模型显示名：按 (model_id, provider) 二元组匹配，
// 目录里没有时回落 model_id 本身。
func piModelLabel(modelID, provider string, options []piModelOption) string {
	id := strings.TrimSpace(modelID)
	if id == "" {
		return ""
	}
	matches := func(option piModelOption) bool {
		return provider == "" || option.provider == "" || option.provider == provider
	}
	for _, option := range options {
		if option.id == id && matches(option) {
			return option.label
		}
	}
	for _, option := range options {
		if strings.EqualFold(option.id, id) && matches(option) {
			return option.label
		}
	}
	return id
}

// piModelSelect 组装模型选择器：已知当前供应商时只列该供应商的模型，
// 当前值展示显示名，缺值时回落清单第一项。
func piModelSelect(in core.BuildInput) shared.SelectSpec {
	provider := piCurrentProvider(in.Binding.Meta)
	options := piFilterModelOptions(piModelOptions(in.Binding.Meta), provider)
	label := piModelLabel(shared.MetaString(in.Binding.Meta, "model_id"), provider, options)
	if label == "" && len(options) > 0 {
		label = options[0].label
	}
	if label == "" {
		label = "模型"
	}
	protocolOptions := make([]toolprotocol.Option, 0, len(options))
	for _, option := range options {
		protocolOptions = append(protocolOptions, toolprotocol.Option{OptionID: option.optionID, Label: option.label})
	}
	if len(protocolOptions) == 0 {
		protocolOptions = nil
	}
	spec := shared.ModelSelect("Pi")
	spec.Placeholder = "模型"
	spec.Label = label
	spec.Value = label
	spec.Options = protocolOptions
	return spec
}
