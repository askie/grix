package kiro

import (
	"context"
	"fmt"
	"strings"

	"github.com/askie/grix/backend/internal/agenttoolbar/agents/shared"
	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	"github.com/askie/grix/backend/internal/model"
)

const kiroContextWarningThreshold = 80.0

type Package struct{}

func New() *Package            { return &Package{} }
func (p *Package) Key() string { return model.AgentClientTypeKiro }
func (p *Package) Match(ctx core.MatchContext) bool {
	return ctx.Agent.ClientType == model.AgentClientTypeKiro
}

var (
	kiroModeOptions = []toolprotocol.Option{
		{OptionID: "kiro_default", Label: "默认编码"},
		{OptionID: "kiro_planner", Label: "规划模式"},
		{OptionID: "kiro_guide", Label: "使用指南"},
	}
)

func (p *Package) Build(_ context.Context, in core.BuildInput) (toolprotocol.Snapshot, error) {
	if !hasKiroSessionBinding(in.Binding) {
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
		sessionTooltip = "Kiro 当前离线"
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

	currentModeID := shared.MetaString(in.Binding.Meta, "mode_id")
	currentModelID := shared.MetaString(in.Binding.Meta, "model_id")

	modelOptions := shared.ParseMetaOptions(in.Binding.Meta, "available_models")

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
		Placeholder: "选择 Kiro 会话操作",
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
	})

	pq := shared.ParseProviderQuota(in.Binding.Meta)
	if quotaItems := shared.BuildProviderQuotaItems(
		pq,
		pq != nil && in.Runtime.HasLocalAction("get_rate_limits"),
	); len(quotaItems) > 0 {
		items = append(items, quotaItems...)
	}

	modelSelect := shared.ModelSelect("Kiro")
	modelSelect.EmptyOptionsTooltip = "暂无可用模型"
	modelSelect.Value = currentModelID
	modelSelect.Badge = shared.OptionLabel(currentModelID, modelOptions)
	modelSelect.Options = modelOptions
	modeSelect := shared.ModeSelect("Kiro")
	modeSelect.Value = currentModeID
	modeSelect.Badge = shared.OptionLabel(currentModeID, kiroModeOptions)
	modeSelect.Options = kiroModeOptions
	items = append(items, shared.BuildSelect(in, modelSelect), shared.BuildSelect(in, modeSelect))

	items = append(items, buildKiroContextCompactProgressItem(in))

	if len(in.Runtime.Skills) > 0 {
		items = append(items, shared.BuildSkillsItem(in.Runtime.Skills))
	}

	if item, ok := shared.BuildSlashCommandsItem("kiro"); ok {
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
	case "select_model":
		return handleSelectModel(in)
	case "select_mode":
		return handleSelectMode(in)
	case "get_rate_limits", "provider_quota_balance":
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
	case "status", "restart", "unbind":
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

func handleSelectModel(in core.ActionInput) (toolprotocol.ActionResult, error) {
	modelId := strings.TrimSpace(in.Request.OptionID)
	if modelId == "" {
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
			"model_id":      modelId,
			"display_label": shared.OptionLabel(modelId, shared.ParseMetaOptions(in.BuildInput.Binding.Meta, "available_models")),
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
	modeId := strings.TrimSpace(in.Request.OptionID)
	if modeId == "" {
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
	if err := in.Executor.DispatchLocalAction(context.Background(), core.LocalActionRequest{
		OwnerID:    in.BuildInput.OwnerID,
		AgentID:    in.BuildInput.Agent.AgentID,
		SessionID:  in.BuildInput.Session.SessionID,
		ActionType: "set_mode",
		Params: map[string]any{
			"session_id":    in.BuildInput.Session.SessionID,
			"mode_id":       modeId,
			"display_label": shared.OptionLabel(modeId, kiroModeOptions),
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
		Message: "已提交额度查询请求",
	}, nil
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
	if err := in.Executor.DispatchLocalAction(context.Background(), core.LocalActionRequest{
		OwnerID:    in.BuildInput.OwnerID,
		AgentID:    in.BuildInput.Agent.AgentID,
		SessionID:  in.BuildInput.Session.SessionID,
		ActionType: "get_session_usage",
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
		Message: "已提交用量查询请求",
	}, nil
}

func hasKiroSessionBinding(binding core.BindingInfo) bool {
	return strings.TrimSpace(binding.BindingID) != "" ||
		strings.TrimSpace(binding.Cwd) != "" ||
		strings.TrimSpace(binding.WorkerStatus) != ""
}

func buildKiroContextCompactProgressItem(in core.BuildInput) toolprotocol.Item {
	ctx := parseKiroContextWindow(in.Binding.Meta)
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
		Variant:        kiroContextProgressVariant(percent),
		Percent:        percent,
		CenterText:     shared.PercentCenterText(percent),
		ProgressDesc:   "会话上下文",
		ProgressDetail: detail,
		Tooltip:        kiroCompactTooltip(in),
		Disabled:       !canKiroCompactThread(in),
		LocalAction:    "thread_compact",
	}
}

func kiroContextProgressVariant(percent float64) string {
	if percent >= kiroContextWarningThreshold {
		return "warning"
	}
	return "secondary"
}

type kiroContextWindow struct {
	UsedPercentage      float64
	RemainingPercentage float64
	HasRemaining        bool
}

func parseKiroContextWindow(meta map[string]any) kiroContextWindow {
	if meta == nil {
		return kiroContextWindow{}
	}
	raw, ok := meta["context_window"]
	if !ok || raw == nil {
		return kiroContextWindow{}
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return kiroContextWindow{}
	}
	used := metaFloat64(obj, "usedPercentage")
	remaining := 100 - used
	if remaining < 0 {
		remaining = 0
	}
	_, hasRemaining := obj["usedPercentage"]
	return kiroContextWindow{
		UsedPercentage:      used,
		RemainingPercentage: remaining,
		HasRemaining:        hasRemaining,
	}
}

func canKiroCompactThread(in core.BuildInput) bool {
	return in.Runtime.Online && in.Runtime.HasLocalAction("thread_compact")
}

func kiroCompactTooltip(in core.BuildInput) string {
	switch {
	case !in.Runtime.Online:
		return "Kiro 当前离线"
	case !in.Runtime.HasLocalAction("thread_compact"):
		return "当前插件未声明 thread_compact"
	default:
		return "压缩当前 Kiro 上下文"
	}
}

func handleThreadCompact(in core.ActionInput) (toolprotocol.ActionResult, error) {
	if !canKiroCompactThread(in.BuildInput) {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "action_unavailable",
			Message: kiroCompactTooltip(in.BuildInput),
		}, nil
	}
	if err := in.Executor.DispatchLocalAction(context.Background(), core.LocalActionRequest{
		OwnerID:    in.BuildInput.OwnerID,
		AgentID:    in.BuildInput.Agent.AgentID,
		SessionID:  in.BuildInput.Session.SessionID,
		ActionType: "thread_compact",
		Params: map[string]any{
			"session_id": in.BuildInput.Session.SessionID,
		},
		TimeoutMs: 120_000,
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
		Message: "已提交 thread_compact 请求",
	}, nil
}

func metaFloat64(obj map[string]any, key string) float64 {
	v, ok := obj[key]
	if !ok {
		return 0
	}
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	}
	return 0
}
