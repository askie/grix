package hermes

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
func (p *Package) Key() string { return model.AgentClientTypeHermes }
func (p *Package) Match(ctx core.MatchContext) bool {
	return ctx.Agent.ClientType == model.AgentClientTypeHermes
}
func (p *Package) Build(_ context.Context, in core.BuildInput) (toolprotocol.Snapshot, error) {
	items := []toolprotocol.Item{}
	if in.Run.HasActiveRun && in.Run.CanStop {
		stopping := strings.TrimSpace(in.Run.State) == "stopping"
		items = append(items, toolprotocol.Item{
			ItemID:   "stop_output",
			GroupID:  "run_control",
			Kind:     toolprotocol.ItemKindButton,
			ActionID: "stop_output",
			Icon:     "stop",
			Variant:  "danger",
			Loading:  stopping,
			Selected: stopping,
		})
	}

	if providerItem, ok := buildHermesProviderItem(in); ok {
		items = append(items, providerItem)
	}

	if modelID, modelLabel, options, ok := hermesConfiguredModel(in.Binding.Meta); ok {
		modelDisabled := !in.Runtime.Online || !in.Runtime.HasLocalAction("set_model") || in.Run.HasActiveRun || len(options) == 0
		modelTooltip := "切换 Hermes 会话模型"
		switch {
		case !in.Runtime.Online:
			modelTooltip = "Hermes 当前离线"
		case !in.Runtime.HasLocalAction("set_model"):
			modelTooltip = "当前插件未声明 set_model"
		case in.Run.HasActiveRun:
			modelTooltip = "当前有任务运行中，完成后可切换模型"
		}
		items = append(items, toolprotocol.Item{
			ItemID:      "select_model",
			GroupID:     "model_control",
			Kind:        toolprotocol.ItemKindSelect,
			ActionID:    "select_model",
			Label:       modelLabel,
			Icon:        "cpu",
			Variant:     "secondary",
			Disabled:    modelDisabled,
			Tooltip:     modelTooltip,
			Value:       modelID,
			Placeholder: "模型",
			Options:     toHermesProtocolOptions(options),
		})
	}

	// get_session_usage 按钮
	usageDisabled := !in.Runtime.Online || !in.Runtime.HasLocalAction("get_session_usage")
	if in.Runtime.HasLocalAction("get_session_usage") {
		items = append(items, toolprotocol.Item{
			ItemID:      "get_session_usage",
			GroupID:     "utility",
			Kind:        toolprotocol.ItemKindButton,
			ActionID:    "get_session_usage",
			Icon:        "usage",
			Variant:     "secondary",
			Disabled:    usageDisabled,
			Tooltip:     "查看用量",
			LocalAction: "get_session_usage",
		})
	}

	// 厂商限额（5h/周/余额）：读 binding meta 的 provider_quota（grix-hermes 巡检下发）。
	if quotaItems := shared.BuildProviderQuotaItems(
		shared.ParseProviderQuota(in.Binding.Meta),
		in.Runtime.HasLocalAction("get_rate_limits"),
	); len(quotaItems) > 0 {
		items = append(items, quotaItems...)
	}

	if len(in.Runtime.Skills) > 0 {
		items = append(items, shared.BuildSkillsItem(in.Runtime.Skills))
	}

	if item, ok := shared.BuildSlashCommandsItem("hermes"); ok {
		items = append([]toolprotocol.Item{item}, items...)
	}

	// 队列按钮不再省略：hermes 插件已实现事件队列全套协议
	// （queue_snapshot / event_cancel / queue_reorder / queue_clear），
	// 工具栏与其他 agent 对齐，由规范化阶段统一前置 show_queue。
	return toolprotocol.Snapshot{
		Visible:                true,
		Items:                  items,
		OmitListSessionsButton: true,
	}, nil
}

type hermesModelOption struct {
	optionID string
	id       string
	label    string
	provider string
}

// hermesCurrentProvider 当前生效供应商：provider_id 为通用层（set_provider 回执）写入的键，
// model_provider 是插件快照的历史键，两者兼容读取。
func hermesCurrentProvider(meta map[string]any) string {
	return hermesFirstMetaString(meta, "provider_id", "model_provider", "provider")
}

type hermesProviderOption struct {
	id    string
	label string
}

func hermesProviderOptions(meta map[string]any) []hermesProviderOption {
	options := make([]hermesProviderOption, 0, 2)
	appendOption := func(raw map[string]any) {
		id := hermesFirstMetaString(raw, "id", "provider_id", "provider")
		if id == "" {
			return
		}
		label := hermesFirstMetaString(raw, "displayName", "display_name", "label", "name")
		if label == "" {
			label = id
		}
		options = append(options, hermesProviderOption{id: id, label: label})
	}
	switch raw := meta["available_providers"].(type) {
	case []any:
		for _, entry := range raw {
			if option, ok := entry.(map[string]any); ok {
				appendOption(option)
			}
		}
	case []map[string]any:
		for _, option := range raw {
			appendOption(option)
		}
	}
	return options
}

func hermesProviderLabel(id string, options []hermesProviderOption) (string, bool) {
	for _, option := range options {
		if option.id == id {
			return option.label, true
		}
	}
	return "", false
}

// buildHermesProviderItem 供应商下拉：插件上报 available_providers 时才出现，
// 与 DeepSeek Harness 的 select_provider 对齐；切换后插件按新供应商重推模型目录。
func buildHermesProviderItem(in core.BuildInput) (toolprotocol.Item, bool) {
	options := hermesProviderOptions(in.Binding.Meta)
	if len(options) == 0 {
		return toolprotocol.Item{}, false
	}
	providerID := hermesCurrentProvider(in.Binding.Meta)
	label, _ := hermesProviderLabel(providerID, options)
	disabled := !in.Runtime.Online || !in.Runtime.HasLocalAction("set_provider") || in.Run.HasActiveRun
	tooltip := "切换 Hermes 会话供应商"
	switch {
	case !in.Runtime.Online:
		tooltip = "Hermes 当前离线"
	case !in.Runtime.HasLocalAction("set_provider"):
		tooltip = "当前插件未声明 set_provider"
	case in.Run.HasActiveRun:
		tooltip = "当前有任务运行中，完成后可切换供应商"
	}
	protocolOptions := make([]toolprotocol.Option, 0, len(options))
	for _, option := range options {
		protocolOptions = append(protocolOptions, toolprotocol.Option{OptionID: option.id, Label: option.label})
	}
	return toolprotocol.Item{
		ItemID:      "select_provider",
		GroupID:     "provider_control",
		Kind:        toolprotocol.ItemKindSelect,
		ActionID:    "select_provider",
		Label:       label,
		Icon:        "server",
		Variant:     "secondary",
		Disabled:    disabled,
		Tooltip:     tooltip,
		Value:       providerID,
		Placeholder: "供应商",
		Options:     protocolOptions,
	}, true
}

func hermesConfiguredModel(meta map[string]any) (string, string, []hermesModelOption, bool) {
	modelID := hermesMetaString(meta, "model_id")
	provider := hermesCurrentProvider(meta)
	options := make([]hermesModelOption, 0, 1)
	appendOption := func(raw map[string]any) {
		id := hermesMetaString(raw, "id")
		if id == "" {
			return
		}
		label := hermesFirstMetaString(raw, "displayName", "display_name", "label", "name")
		if label == "" {
			label = id
		}
		optionProvider := hermesFirstMetaString(raw, "provider", "provider_id", "model_provider")
		optionID := id
		if optionProvider != "" {
			optionID = optionProvider + ":" + id
		}
		options = append(options, hermesModelOption{
			optionID: optionID,
			id:       id,
			label:    label,
			provider: optionProvider,
		})
	}

	switch raw := meta["available_models"].(type) {
	case []any:
		for _, entry := range raw {
			if option, ok := entry.(map[string]any); ok {
				appendOption(option)
			}
		}
	case []map[string]any:
		for _, option := range raw {
			appendOption(option)
		}
	}

	// 已知当前供应商且目录里有它的模型时，只展示该供应商下的模型。
	if provider != "" {
		filtered := options[:0:0]
		for _, option := range options {
			if option.provider == provider {
				filtered = append(filtered, option)
			}
		}
		if len(filtered) > 0 {
			options = filtered
		}
	}
	if modelID == "" && len(options) == 1 {
		modelID = options[0].id
		provider = options[0].provider
	}
	if modelID == "" {
		return "", "", nil, false
	}
	for _, option := range options {
		if option.id == modelID && (provider == "" || option.provider == "" || option.provider == provider) {
			return option.optionID, option.label, options, true
		}
	}
	return modelID, modelID, options, true
}

func toHermesProtocolOptions(options []hermesModelOption) []toolprotocol.Option {
	out := make([]toolprotocol.Option, 0, len(options))
	for _, option := range options {
		if strings.TrimSpace(option.optionID) == "" {
			continue
		}
		out = append(out, toolprotocol.Option{OptionID: option.optionID, Label: option.label})
	}
	return out
}

func resolveHermesModelOption(optionID string, options []hermesModelOption) hermesModelOption {
	normalized := strings.TrimSpace(optionID)
	for _, option := range options {
		if option.optionID == normalized {
			return option
		}
	}
	return hermesModelOption{optionID: normalized, id: normalized, label: normalized}
}

func hermesFirstMetaString(meta map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := hermesMetaString(meta, key); value != "" {
			return value
		}
	}
	return ""
}

func hermesMetaString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	value, _ := meta[key].(string)
	return strings.TrimSpace(value)
}

func (p *Package) HandleAction(ctx context.Context, in core.ActionInput) (toolprotocol.ActionResult, error) {
	switch strings.TrimSpace(in.Request.ActionID) {
	case "select_model":
		return handleSelectModel(ctx, in)
	case "select_provider":
		return handleSelectProvider(ctx, in)
	case "stop_output":
		if !in.BuildInput.Run.HasActiveRun || !in.BuildInput.Run.CanStop {
			return toolprotocol.ActionResult{
				Outcome: toolprotocol.ActionOutcomeRejected,
				Code:    "stop_unavailable",
				Message: "当前没有可停止的输出",
			}, nil
		}
		if err := in.Executor.SendStopText(ctx, core.StopOutputRequest{
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
	case "get_session_usage":
		if !in.BuildInput.Runtime.HasLocalAction("get_session_usage") {
			return toolprotocol.ActionResult{
				Outcome: toolprotocol.ActionOutcomeRejected,
				Code:    "local_action_unavailable",
				Message: "当前 agent 未声明 get_session_usage",
			}, nil
		}
		if err := in.Executor.DispatchLocalAction(ctx, core.LocalActionRequest{
			OwnerID:    in.BuildInput.OwnerID,
			AgentID:    in.BuildInput.Agent.AgentID,
			SessionID:  in.BuildInput.Session.SessionID,
			ActionType: "get_session_usage",
			Params:     map[string]any{"session_id": in.BuildInput.Session.SessionID},
			TimeoutMs:  20_000,
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
			Message: "已提交用量查询请求",
		}, nil
	case "get_rate_limits", "provider_quota_five_hour", "provider_quota_weekly_limit", "provider_quota_balance":
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
		if err := in.Executor.DispatchLocalAction(ctx, core.LocalActionRequest{
			OwnerID:    in.BuildInput.OwnerID,
			AgentID:    in.BuildInput.Agent.AgentID,
			SessionID:  in.BuildInput.Session.SessionID,
			ActionType: "get_rate_limits",
			Params:     map[string]any{"session_id": in.BuildInput.Session.SessionID},
			TimeoutMs:  20_000,
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
			Message: "已提交余量查询请求",
		}, nil
	default:
		return shared.HandleWorkspaceAction(ctx, in)
	}
}

func handleSelectProvider(ctx context.Context, in core.ActionInput) (toolprotocol.ActionResult, error) {
	providerID := strings.TrimSpace(in.Request.OptionID)
	if providerID == "" {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_option",
			Message: "未选择供应商",
		}, nil
	}
	if !in.BuildInput.Runtime.Online {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "agent_offline",
			Message: "Hermes 当前离线",
		}, nil
	}
	if !in.BuildInput.Runtime.HasLocalAction("set_provider") {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "local_action_unavailable",
			Message: "当前 agent 未声明 set_provider",
		}, nil
	}
	if in.BuildInput.Run.HasActiveRun {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "run_active",
			Message: "当前有任务运行中，完成后可切换供应商",
		}, nil
	}
	label, ok := hermesProviderLabel(providerID, hermesProviderOptions(in.BuildInput.Binding.Meta))
	if !ok {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_option",
			Message: "供应商不在当前可用列表中",
		}, nil
	}
	if err := in.Executor.DispatchLocalAction(ctx, core.LocalActionRequest{
		OwnerID:    in.BuildInput.OwnerID,
		AgentID:    in.BuildInput.Agent.AgentID,
		SessionID:  in.BuildInput.Session.SessionID,
		ActionType: "set_provider",
		Params: map[string]any{
			"session_id":    in.BuildInput.Session.SessionID,
			"provider_id":   providerID,
			"display_label": label,
		},
		TimeoutMs: 15_000,
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
		Message: "已提交供应商切换请求",
	}, nil
}

func handleSelectModel(ctx context.Context, in core.ActionInput) (toolprotocol.ActionResult, error) {
	optionID := strings.TrimSpace(in.Request.OptionID)
	if optionID == "" {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_option",
			Message: "未选择模型",
		}, nil
	}
	if !in.BuildInput.Runtime.Online {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "agent_offline",
			Message: "Hermes 当前离线",
		}, nil
	}
	if !in.BuildInput.Runtime.HasLocalAction("set_model") {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "local_action_unavailable",
			Message: "当前 agent 未声明 set_model",
		}, nil
	}
	if in.BuildInput.Run.HasActiveRun {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "run_active",
			Message: "当前有任务运行中，完成后可切换模型",
		}, nil
	}
	_, _, options, _ := hermesConfiguredModel(in.BuildInput.Binding.Meta)
	selected := resolveHermesModelOption(optionID, options)
	if selected.id == "" {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_option",
			Message: "工具栏选项无效",
		}, nil
	}
	params := map[string]any{
		"session_id":    in.BuildInput.Session.SessionID,
		"model_id":      selected.id,
		"display_label": selected.label,
	}
	if selected.provider != "" {
		params["provider"] = selected.provider
	}
	if err := in.Executor.DispatchLocalAction(ctx, core.LocalActionRequest{
		OwnerID:    in.BuildInput.OwnerID,
		AgentID:    in.BuildInput.Agent.AgentID,
		SessionID:  in.BuildInput.Session.SessionID,
		ActionType: "set_model",
		Params:     params,
		TimeoutMs:  15_000,
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
		Message: "已提交模型切换请求",
	}, nil
}
