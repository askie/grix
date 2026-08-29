package opencode

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/agenttoolbar/agents/shared"
	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	"github.com/askie/grix/backend/internal/model"
)

type Package struct{}

func New() *Package            { return &Package{} }
func (p *Package) Key() string { return model.AgentClientTypeOpenCode }
func (p *Package) Match(ctx core.MatchContext) bool {
	return ctx.Agent.ClientType == model.AgentClientTypeOpenCode
}

func (p *Package) Build(_ context.Context, in core.BuildInput) (toolprotocol.Snapshot, error) {
	if !hasOpenCodeSessionBinding(in.Binding) {
		return toolprotocol.Snapshot{
			Visible: false,
			Items:   []toolprotocol.Item{},
		}, nil
	}

	sessionDisabled := !in.Runtime.Online || !in.Runtime.HasLocalAction("session_control")
	sessionTooltip := ""
	switch {
	case !in.Runtime.Online:
		sessionTooltip = "OpenCode 当前离线"
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

	usageDisabled := !in.Runtime.Online || !in.Runtime.HasLocalAction("get_session_usage")

	canStop := in.Run.HasActiveRun && in.Run.CanStop
	runState := strings.TrimSpace(in.Run.State)
	showStopOutput := in.Run.HasActiveRun && (in.Run.CanStop || runState == "stopping")

	modelOptions := shared.ParseMetaOptions(in.Binding.Meta, "available_models")
	currentModelID := shared.MetaString(in.Binding.Meta, "model_id")
	modeOptions := shared.ParseMetaOptions(in.Binding.Meta, "available_modes")
	currentModeID := shared.MetaString(in.Binding.Meta, "mode_id")

	items := []toolprotocol.Item{}

	if showStopOutput {
		items = append(items, toolprotocol.Item{
			ItemID:   "stop_output",
			GroupID:  "run_control",
			Kind:     toolprotocol.ItemKindButton,
			ActionID: "stop_output",
			Icon:     "stop",
			Variant:  "danger",
			Disabled: !canStop,
			Tooltip:  stopOutputTooltip(runState),
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
		Placeholder: "选择 OpenCode 会话操作",
		Options: []toolprotocol.Option{
			{OptionID: "status", Label: "查看状态"},
			{OptionID: "stop", Label: "停止会话"},
			{OptionID: "unbind", Label: "解绑"},
			{
				OptionID: "usage",
				Label:    "查看用量",
				Disabled: usageDisabled,
			},
		},
	})

	if len(modelOptions) > 0 || currentModelID != "" {
		modelSelect := shared.ModelSelect("OpenCode")
		modelSelect.Value = currentModelID
		modelSelect.Badge = shared.OptionLabel(currentModelID, modelOptions)
		modelSelect.Options = modelOptions
		items = append(items, shared.BuildSelect(in, modelSelect))
	}

	if len(modeOptions) > 0 {
		modeSelect := shared.ModeSelect("OpenCode")
		modeSelect.Noun = "运行模式"
		modeSelect.Placeholder = "选择运行模式"
		modeSelect.Value = currentModeID
		modeSelect.Badge = shared.OptionLabel(currentModeID, modeOptions)
		modeSelect.Options = modeOptions
		items = append(items, shared.BuildSelect(in, modeSelect))
	}

	// 5H/7D 限额（provider_quota，get_rate_limits 点击刷新）+ 会话上下文压缩剩余（context_window）。
	// 数据来自连接器 binding 卡 meta；provider_quota 为空时不渲染占位（避免无 provider 配置时误导）。
	if quota := shared.ParseProviderQuota(in.Binding.Meta); quota != nil {
		hasRateLimitsAction := in.Runtime.Online && in.Runtime.HasLocalAction("get_rate_limits")
		items = append(items, shared.BuildProviderQuotaItems(quota, hasRateLimitsAction)...)
	}
	if item := buildOpenCodeContextWindowItem(in); item != nil {
		items = append(items, *item)
	}

	if len(in.Runtime.Skills) > 0 {
		items = append(items, shared.BuildSkillsItem(in.Runtime.Skills))
	}

	if item, ok := shared.BuildSlashCommandsItem("opencode"); ok {
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
	case "get_rate_limits":
		return handleGetRateLimits(in)
	case "select_model":
		return handleSelectModel(in)
	case "select_mode":
		return handleSelectMode(in)
	default:
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_action",
			Message: "工具栏动作无效",
		}, nil
	}
}

func handleSessionControl(in core.ActionInput) (toolprotocol.ActionResult, error) {
	verb := strings.TrimSpace(in.Request.OptionID)
	log.Printf("[opencode:session_control] START verb=%q agentID=%d sessionID=%s ownerID=%d",
		verb, in.BuildInput.Agent.AgentID, in.BuildInput.Session.SessionID, in.BuildInput.OwnerID)

	switch verb {
	case "status", "stop", "unbind":
	default:
		log.Printf("[opencode:session_control] REJECT invalid verb=%q", verb)
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_option",
			Message: "工具栏选项无效",
		}, nil
	}
	if !in.BuildInput.Runtime.Online {
		log.Printf("[opencode:session_control] REJECT agent offline")
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "agent_offline",
			Message: "当前 agent 不在线",
		}, nil
	}
	if !in.BuildInput.Runtime.HasLocalAction("session_control") {
		log.Printf("[opencode:session_control] REJECT local_action unavailable, declared actions: %v",
			in.BuildInput.Runtime.LocalActions)
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "local_action_unavailable",
			Message: "当前 agent 未声明 session_control",
		}, nil
	}

	log.Printf("[opencode:session_control] DISPATCH verb=%q timeout=60s", verb)
	startTime := time.Now()

	if err := in.Executor.DispatchLocalAction(context.Background(), core.LocalActionRequest{
		OwnerID:    in.BuildInput.OwnerID,
		AgentID:    in.BuildInput.Agent.AgentID,
		SessionID:  in.BuildInput.Session.SessionID,
		ActionType: "session_control",
		Params: map[string]any{
			"session_id": in.BuildInput.Session.SessionID,
			"verb":       verb,
		},
		TimeoutMs: 60_000,
	}); err != nil {
		elapsed := time.Since(startTime)
		log.Printf("[opencode:session_control] FAILED verb=%q elapsed=%v error=%v", verb, elapsed, err)
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "dispatch_failed",
			Message: err.Error(),
		}, nil
	}

	elapsed := time.Since(startTime)
	log.Printf("[opencode:session_control] SUCCESS verb=%q elapsed=%v", verb, elapsed)

	return toolprotocol.ActionResult{
		Outcome: toolprotocol.ActionOutcomeAcceptedNoStateChange,
		Code:    "accepted",
		Message: "已提交会话操作",
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

// ── 会话上下文压缩剩余渲染 ──

const openCodeContextWarningThreshold = 80.0

type openCodeContextWindow struct {
	UsedPercentage      float64
	RemainingPercentage float64
	HasRemaining        bool
}

func parseOpenCodeContextWindow(meta map[string]any) openCodeContextWindow {
	if meta == nil {
		return openCodeContextWindow{}
	}
	raw, ok := meta["context_window"]
	if !ok || raw == nil {
		return openCodeContextWindow{}
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return openCodeContextWindow{}
	}
	return openCodeContextWindow{
		UsedPercentage:      parseNumber(obj["usedPercentage"]),
		RemainingPercentage: parseNumber(obj["remainingPercentage"]),
		HasRemaining:        obj["remainingPercentage"] != nil,
	}
}

func buildOpenCodeContextWindowItem(in core.BuildInput) *toolprotocol.Item {
	ctx := parseOpenCodeContextWindow(in.Binding.Meta)
	if ctx.UsedPercentage <= 0 && ctx.RemainingPercentage <= 0 {
		return nil
	}
	percent := math.Min(100, math.Max(0, ctx.UsedPercentage))
	detail := ""
	if ctx.HasRemaining {
		detail = fmt.Sprintf("剩余 %.1f%%", ctx.RemainingPercentage)
	}
	variant := "secondary"
	if percent >= openCodeContextWarningThreshold {
		variant = "warning"
	}
	return &toolprotocol.Item{
		ItemID:         "context_window",
		GroupID:        "session_control",
		Kind:           toolprotocol.ItemKindProgress,
		ActionID:       "context_window",
		Variant:        variant,
		Percent:        percent,
		CenterText:     shared.PercentCenterText(percent),
		ProgressDesc:   "会话上下文",
		ProgressDetail: detail,
	}
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
	modelOptions := shared.ParseMetaOptions(in.BuildInput.Binding.Meta, "available_models")
	return dispatchLocalAction(in, "set_model", map[string]any{
		"session_id":    in.BuildInput.Session.SessionID,
		"model_id":      modelID,
		"display_label": shared.OptionLabel(modelID, modelOptions),
	}, 15_000, "已切换模型")
}

func handleSelectMode(in core.ActionInput) (toolprotocol.ActionResult, error) {
	modeID := strings.TrimSpace(in.Request.OptionID)
	if modeID == "" {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_option",
			Message: "未选择运行模式",
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
	modeOptions := shared.ParseMetaOptions(in.BuildInput.Binding.Meta, "available_modes")
	return dispatchLocalAction(in, "set_mode", map[string]any{
		"session_id":    in.BuildInput.Session.SessionID,
		"mode_id":       modeID,
		"display_label": shared.OptionLabel(modeID, modeOptions),
	}, 15_000, "已切换运行模式")
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
		Outcome: toolprotocol.ActionOutcomeAcceptedWithImmediateRefresh,
		Code:    "accepted",
		Message: message,
	}, nil
}

func stopOutputTooltip(runState string) string {
	if strings.TrimSpace(runState) == "stopping" {
		return "正在停止当前输出"
	}
	return "停止当前输出"
}

func hasOpenCodeSessionBinding(binding core.BindingInfo) bool {
	return strings.TrimSpace(binding.BindingID) != "" ||
		strings.TrimSpace(binding.Cwd) != ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
