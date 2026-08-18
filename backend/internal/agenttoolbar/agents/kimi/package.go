// Package kimi 是 Kimi Code CLI 的聊天窗口工具栏包。
//
// 模型/模式均可会话级切换：Kimi Code CLI 0.26.0 起 session/set_model /
// session/set_mode 都是会话级操作（会话配置经 configOptions 上报，由连接器
// 解析进 binding meta），不再有"切模型写 ~/.kimi 全局配置"的旧限制。
package kimi

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/askie/grix/backend/internal/agenttoolbar/agents/shared"
	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	"github.com/askie/grix/backend/internal/model"
)

type Package struct{}

func New() *Package            { return &Package{} }
func (p *Package) Key() string { return model.AgentClientTypeKimi }
func (p *Package) Match(ctx core.MatchContext) bool {
	return ctx.Agent.ClientType == model.AgentClientTypeKimi
}

func (p *Package) Build(_ context.Context, in core.BuildInput) (toolprotocol.Snapshot, error) {
	if !hasKimiSessionBinding(in.Binding) {
		return toolprotocol.Snapshot{
			Visible: false,
			Items:   []toolprotocol.Item{},
		}, nil
	}

	runState := strings.TrimSpace(in.Run.State)
	showStopOutput := in.Run.HasActiveRun && (in.Run.CanStop || runState == "stopping")

	sessionDisabled := !in.Runtime.Online || !in.Runtime.HasLocalAction("session_control")
	sessionTooltip := ""
	switch {
	case !in.Runtime.Online:
		sessionTooltip = "Kimi 当前离线"
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
		Placeholder: "选择 Kimi 会话操作",
		// 刻意没有「查看用量」选项：连接器的 ACP 用量解析器只支持
		// kiro/gemini/qwen 的会话文件，kimi 会话解析不了，get_session_usage
		// 必然返回 usage_not_found——连接器补上 kimi 解析后再开放。
		Options: []toolprotocol.Option{
			{OptionID: "status", Label: "查看状态"},
			{OptionID: "restart", Label: "重启会话"},
		},
	})

	// 用量条紧跟工作区图标：连接器为 kimi 预拉取官方用量接口并随 binding meta 推送
	// provider_quota（kimi 只有 five_hour 一档，无周档）。
	if quotaItems := shared.BuildProviderQuotaItems(shared.ParseProviderQuota(in.Binding.Meta), in.Runtime.HasLocalAction("get_rate_limits")); len(quotaItems) > 0 {
		items = append(items, quotaItems...)
	}

	modelOptions := buildKimiModelOptions(in.Binding.Meta)
	currentModelID := bindingMetaString(in.Binding.Meta, "model_id")
	if len(modelOptions) > 0 || currentModelID != "" {
		modelDisabled := !in.Runtime.Online || !in.Runtime.HasLocalAction("set_model") || len(modelOptions) == 0
		modelTooltip := "切换 Kimi 模型"
		switch {
		case !in.Runtime.Online:
			modelTooltip = "Kimi 当前离线"
		case !in.Runtime.HasLocalAction("set_model"):
			modelTooltip = "当前插件未声明 set_model"
		case len(modelOptions) == 0:
			modelTooltip = "等待 Kimi 模型列表同步"
		}
		items = append(items, toolprotocol.Item{
			ItemID:      "select_model",
			GroupID:     "model_control",
			Kind:        toolprotocol.ItemKindSelect,
			ActionID:    "select_model",
			Icon:        "cpu",
			Variant:     "secondary",
			Disabled:    modelDisabled,
			Tooltip:     modelTooltip,
			Value:       currentModelID,
			BadgeText:   resolveKimiOptionLabel(currentModelID, modelOptions),
			Placeholder: "选择模型",
			Options:     toKimiProtocolOptions(modelOptions),
		})
	}

	modeOptions := buildKimiModeOptions(in.Binding.Meta)
	currentModeID := bindingMetaString(in.Binding.Meta, "mode_id")
	if len(modeOptions) > 0 {
		modeDisabled := !in.Runtime.Online || !in.Runtime.HasLocalAction("set_mode")
		modeTooltip := "切换 Kimi 模式"
		switch {
		case !in.Runtime.Online:
			modeTooltip = "Kimi 当前离线"
		case !in.Runtime.HasLocalAction("set_mode"):
			modeTooltip = "当前插件未声明 set_mode"
		}
		items = append(items, toolprotocol.Item{
			ItemID:      "select_mode",
			GroupID:     "mode_control",
			Kind:        toolprotocol.ItemKindSelect,
			ActionID:    "select_mode",
			Icon:        "shield",
			Variant:     "secondary",
			Disabled:    modeDisabled,
			Tooltip:     modeTooltip,
			Value:       currentModeID,
			BadgeText:   kimiModeBadgeLabel(currentModeID),
			Placeholder: "选择模式",
			Options:     toKimiProtocolOptions(modeOptions),
		})
	}

	if item, ok := buildKimiContextWindowItem(in.Binding.Meta); ok {
		items = append(items, item)
	}

	if len(in.Runtime.Skills) > 0 {
		items = append(items, shared.BuildSkillsItem(in.Runtime.Skills))
	}

	if item, ok := shared.BuildSlashCommandsItem("kimi"); ok {
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
	case "select_mode":
		return handleSelectMode(in)
	case "select_model":
		return handleSelectModel(in)
	case "get_rate_limits", "provider_quota_five_hour", "provider_quota_weekly_limit", "provider_quota_balance":
		return handleGetRateLimits(in)
	case "current_model":
		// 旧快照残留的只读模型徽标：已被 select_model 取代，点击一律拒绝。
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_action",
			Message: "工具栏动作无效",
		}, nil
	case "context_window":
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "read_only",
			Message: "上下文用量仅展示，Kimi 未提供压缩操作",
		}, nil
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
	case "status", "restart":
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
	if err := in.Executor.DispatchLocalAction(context.Background(), core.LocalActionRequest{
		OwnerID:    in.BuildInput.OwnerID,
		AgentID:    in.BuildInput.Agent.AgentID,
		SessionID:  in.BuildInput.Session.SessionID,
		ActionType: "session_control",
		Params: map[string]any{
			"session_id": in.BuildInput.Session.SessionID,
			"verb":       verb,
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
		Message: "已提交会话操作",
	}, nil
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
	if err := in.Executor.DispatchLocalAction(context.Background(), core.LocalActionRequest{
		OwnerID:    in.BuildInput.OwnerID,
		AgentID:    in.BuildInput.Agent.AgentID,
		SessionID:  in.BuildInput.Session.SessionID,
		ActionType: "get_rate_limits",
		Params: map[string]any{
			"session_id": in.BuildInput.Session.SessionID,
		},
		TimeoutMs: 20_000,
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
	if !in.BuildInput.Runtime.Online {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "agent_offline",
			Message: "当前 agent 不在线",
		}, nil
	}
	if !in.BuildInput.Runtime.HasLocalAction("set_model") {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "local_action_unavailable",
			Message: "当前 agent 未声明 set_model",
		}, nil
	}
	if err := in.Executor.DispatchLocalAction(context.Background(), core.LocalActionRequest{
		OwnerID:    in.BuildInput.OwnerID,
		AgentID:    in.BuildInput.Agent.AgentID,
		SessionID:  in.BuildInput.Session.SessionID,
		ActionType: "set_model",
		Params: map[string]any{
			"session_id":    in.BuildInput.Session.SessionID,
			"model_id":      modelID,
			"display_label": resolveKimiOptionLabel(modelID, buildKimiModelOptions(in.BuildInput.Binding.Meta)),
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
		Outcome: toolprotocol.ActionOutcomeAcceptedWithImmediateRefresh,
		Code:    "accepted",
		Message: "已切换模型",
	}, nil
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
	modeOptions := buildKimiModeOptions(in.BuildInput.Binding.Meta)
	canonicalModeID, allowed := kimiCanonicalModeID(modeID, modeOptions)
	if !allowed {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_option",
			Message: "工具栏选项无效",
		}, nil
	}
	modeID = canonicalModeID
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
	if err := in.Executor.DispatchLocalAction(context.Background(), core.LocalActionRequest{
		OwnerID:    in.BuildInput.OwnerID,
		AgentID:    in.BuildInput.Agent.AgentID,
		SessionID:  in.BuildInput.Session.SessionID,
		ActionType: "set_mode",
		Params: map[string]any{
			"session_id":    in.BuildInput.Session.SessionID,
			"mode_id":       modeID,
			"display_label": resolveKimiOptionLabel(modeID, modeOptions),
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
		Outcome: toolprotocol.ActionOutcomeAcceptedWithImmediateRefresh,
		Code:    "accepted",
		Message: "已切换模式",
	}, nil
}

// buildKimiContextWindowItem 渲染连接器 ACP 上下文窗口回调推送的
// meta.context_window（{usedPercentage}）。kimi 未声明 thread_compact，
// 只做只读展示，不挂压缩动作。
func buildKimiContextWindowItem(meta map[string]any) (toolprotocol.Item, bool) {
	if len(meta) == 0 {
		return toolprotocol.Item{}, false
	}
	raw, ok := meta["context_window"]
	if !ok || raw == nil {
		return toolprotocol.Item{}, false
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return toolprotocol.Item{}, false
	}
	used, ok := bindingMetaFloat64(obj, "usedPercentage")
	if !ok {
		// 值缺失或不是数字（null/字符串）时不渲染，避免展示 0%/剩余 100% 的假数据。
		return toolprotocol.Item{}, false
	}
	if used < 0 {
		used = 0
	} else if used > 100 {
		used = 100
	}
	variant := "secondary"
	if used >= 85 {
		variant = "warning"
	}
	return toolprotocol.Item{
		ItemID:         "context_window",
		GroupID:        "session_control",
		Kind:           toolprotocol.ItemKindProgress,
		ActionID:       "context_window",
		Variant:        variant,
		Percent:        used,
		CenterText:     shared.PercentCenterText(used),
		ProgressDesc:   "会话上下文",
		ProgressDetail: fmt.Sprintf("剩余 %.1f%%", 100-used),
		Tooltip:        "Kimi 会话上下文用量（只读）",
		Disabled:       true,
	}, true
}

func bindingMetaFloat64(meta map[string]any, key string) (float64, bool) {
	if meta == nil {
		return 0, false
	}
	switch v := meta[key].(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	default:
		return 0, false
	}
}

func bindingMetaString(meta map[string]any, key string) string {
	if len(meta) == 0 {
		return ""
	}
	value, _ := meta[key].(string)
	return strings.TrimSpace(value)
}

func hasKimiSessionBinding(binding core.BindingInfo) bool {
	return strings.TrimSpace(binding.BindingID) != "" ||
		strings.TrimSpace(binding.Cwd) != "" ||
		strings.TrimSpace(binding.WorkerStatus) != ""
}

type kimiOption struct {
	ID    string
	Label string
}

// buildKimiModelOptions / buildKimiModeOptions 解析连接器 ACP 适配器上报的
// binding meta（available_models / available_modes，条目为 {id, displayName}）。
func buildKimiModelOptions(meta map[string]any) []kimiOption {
	return buildKimiOptions(meta, "available_models")
}

// kimiModeWhitelist 收口工具栏暴露的 Kimi 模式为三档：默认（启动无参数）、
// 计划（--plan）、自动（yolo，自动批准全部操作）。CLI 0.26.0 还上报第四档
// auto（仅自动批准安全操作），产品上不暴露，过滤掉；label 用中文，
// 英文侧由 agenttoolbar/i18n 统一映射。
var kimiModeWhitelist = []kimiOption{
	{ID: "default", Label: "默认"},
	{ID: "plan", Label: "计划"},
	{ID: "yolo", Label: "自动"},
}

// kimiModeBadgeLabels 仅用于当前模式徽标展示：额外包含被过滤的 auto 档，
// 用户在 CLI 侧自行切到 auto 时徽标不落回裸英文 id；不进下拉选项。
var kimiModeBadgeLabels = map[string]string{
	"default": "默认",
	"plan":    "计划",
	"yolo":    "自动",
	"auto":    "自动（安全）",
}

func kimiModeBadgeLabel(modeID string) string {
	normalized := strings.ToLower(strings.TrimSpace(modeID))
	if label, ok := kimiModeBadgeLabels[normalized]; ok {
		return label
	}
	return strings.TrimSpace(modeID)
}

func buildKimiModeOptions(meta map[string]any) []kimiOption {
	reported := buildKimiOptions(meta, "available_modes")
	if len(reported) == 0 {
		return nil
	}
	ids := make(map[string]struct{}, len(reported))
	reportedIDs := make([]string, 0, len(reported))
	for _, opt := range reported {
		ids[strings.ToLower(opt.ID)] = struct{}{}
		reportedIDs = append(reportedIDs, opt.ID)
	}
	opts := make([]kimiOption, 0, len(kimiModeWhitelist))
	for _, opt := range kimiModeWhitelist {
		if _, ok := ids[opt.ID]; ok {
			opts = append(opts, opt)
		}
	}
	if len(opts) == 0 {
		// CLI 上报了模式但与白名单交集为空（如未来改了模式 id）：模式下拉
		// 会整体消失，留日志便于排查，避免静默丢能力。
		log.Printf("[kimi:select_mode] reported modes %v matched none of whitelist", reportedIDs)
	}
	return opts
}

func buildKimiOptions(meta map[string]any, key string) []kimiOption {
	if len(meta) == 0 {
		return nil
	}
	list, ok := meta[key].([]any)
	if !ok {
		return nil
	}
	opts := make([]kimiOption, 0, len(list))
	seen := map[string]struct{}{}
	for _, raw := range list {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id := bindingMetaString(entry, "id")
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		label := bindingMetaString(entry, "displayName")
		if label == "" {
			label = id
		}
		opts = append(opts, kimiOption{ID: id, Label: label})
	}
	return opts
}

// kimiCanonicalModeID 大小写不敏感地在当前选项里匹配模式 id，命中返回
// 白名单里的规范 id（发给 CLI 的以规范 id 为准）。
func kimiCanonicalModeID(modeID string, options []kimiOption) (string, bool) {
	for _, option := range options {
		if strings.EqualFold(option.ID, modeID) {
			return option.ID, true
		}
	}
	return "", false
}

func resolveKimiOptionLabel(optionID string, options []kimiOption) string {
	normalized := strings.TrimSpace(optionID)
	if normalized == "" {
		return ""
	}
	for _, option := range options {
		if strings.EqualFold(option.ID, normalized) {
			return option.Label
		}
	}
	return normalized
}

func toKimiProtocolOptions(options []kimiOption) []toolprotocol.Option {
	out := make([]toolprotocol.Option, 0, len(options))
	for _, option := range options {
		out = append(out, toolprotocol.Option{OptionID: option.ID, Label: option.Label})
	}
	return out
}
