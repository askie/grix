package cursor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/agenttoolbar/agents/shared"
	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	"github.com/askie/grix/backend/internal/model"
)

type Package struct{}

var cursorModeOptions = []toolprotocol.Option{
	{OptionID: "approval", Label: "人工确认"},
	{OptionID: "full_auto", Label: "自由模式"},
	{OptionID: "plan", Label: "计划模式"},
}

func New() *Package            { return &Package{} }
func (p *Package) Key() string { return model.AgentClientTypeCursor }
func (p *Package) Match(ctx core.MatchContext) bool {
	return ctx.Agent.ClientType == model.AgentClientTypeCursor
}

func (p *Package) Build(_ context.Context, in core.BuildInput) (toolprotocol.Snapshot, error) {
	if !hasCursorSessionBinding(in.Binding) {
		return toolprotocol.Snapshot{
			Visible: false,
			Items:   []toolprotocol.Item{},
		}, nil
	}

	items := make([]toolprotocol.Item, 0, 9)

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
			Tooltip:  cursorStopOutputTooltip(runState),
			Loading:  runState == "stopping",
			Selected: runState == "stopping",
		})
	}

	sessionDisabled := !in.Runtime.Online || !in.Runtime.HasLocalAction("session_control")
	sessionTooltip := "Cursor 会话操作"
	switch {
	case !in.Runtime.Online:
		sessionTooltip = "Cursor 当前离线"
	case !in.Runtime.HasLocalAction("session_control"):
		sessionTooltip = "当前插件未声明 session_control"
	case strings.TrimSpace(in.Binding.Cwd) != "":
		sessionTooltip = "Cursor 会话操作\n工作目录: " + strings.TrimSpace(in.Binding.Cwd)
	}

	badge := ""
	if cwd := strings.TrimSpace(in.Binding.Cwd); cwd != "" {
		badge = shared.PathBase(cwd)
	} else if in.Runtime.Online {
		badge = "在线"
	} else {
		badge = "离线"
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
				{OptionID: "restart", Label: "重启会话"},
				{OptionID: "unbind", Label: "解绑"},
			},
		},
	)

	rateLimitItems := buildCursorRateLimitItems(in)
	if len(rateLimitItems) > 0 {
		items = append(items, rateLimitItems...)
	}

	items = append(items, buildCursorContextCompactProgressItem(in))

	// Dynamic model selector
	currentModelID := shared.MetaString(in.Binding.Meta, "model_id")
	modelOptions := shared.ParseMetaOptions(in.Binding.Meta, "available_models")
	currentModelLabel := shared.OptionLabel(currentModelID, modelOptions)
	if currentModelLabel == "" {
		currentModelLabel = "模型"
	}
	modelSelect := shared.ModelSelect("Cursor")
	modelSelect.Label = currentModelLabel
	modelSelect.Value = currentModelLabel
	modelSelect.Options = modelOptions

	// Mode selector
	currentModeID := shared.MetaString(in.Binding.Meta, "mode_id")
	if currentModeID == "" {
		currentModeID = "approval"
	}
	modeSelect := shared.ModeSelect("Cursor")
	modeSelect.Label = resolveCursorModeLabel(currentModeID)
	modeSelect.Value = currentModeID
	modeSelect.Options = cursorModeOptions

	items = append(items, shared.BuildSelect(in, modelSelect), shared.BuildSelect(in, modeSelect))

	if len(in.Runtime.Skills) > 0 {
		items = append(items, shared.BuildSkillsItem(in.Runtime.Skills))
	}

	if item, ok := shared.BuildSlashCommandsItem("cursor"); ok {
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
	case "select_model":
		return handleSelectModel(in)
	case "select_mode":
		return handleSelectMode(in)
	case "get_rate_limits", "rate_limit_monthly", "rate_limit_api", "rate_limit_5h", "rate_limit_7d":
		return handleGetRateLimits(in)
	case "thread_compact":
		return handleCursorCompactThread(in)
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
		"verb":       verb,
	}
	timeoutMs := 15_000
	msg := "已提交会话操作"
	if verb == "restart" {
		params["display_label"] = "重启"
		timeoutMs = 30_000
		msg = "已提交重启请求"
	}
	return dispatchLocalAction(in, "session_control", params, timeoutMs, msg)
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
	modelOptions := shared.ParseMetaOptions(in.BuildInput.Binding.Meta, "available_models")
	return dispatchLocalAction(in, "set_model", map[string]any{
		"session_id":    in.BuildInput.Session.SessionID,
		"model_id":      modelId,
		"display_label": shared.OptionLabel(modelId, modelOptions),
	}, 15_000, "已切换模型")
}

func handleSelectMode(in core.ActionInput) (toolprotocol.ActionResult, error) {
	modeId := strings.TrimSpace(in.Request.OptionID)
	if !isCursorMode(modeId) {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_option",
			Message: "Cursor 模式无效",
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
		"session_id": in.BuildInput.Session.SessionID,
		"mode_id":    modeId,
	}, 15_000, "已切换模式")
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

func cursorStopOutputTooltip(runState string) string {
	if strings.TrimSpace(runState) == "stopping" {
		return "正在停止当前输出"
	}
	return "停止当前输出"
}

func hasCursorSessionBinding(binding core.BindingInfo) bool {
	return strings.TrimSpace(binding.BindingID) != "" ||
		strings.TrimSpace(binding.Cwd) != ""
}

func isCursorMode(modeID string) bool {
	for _, option := range cursorModeOptions {
		if option.OptionID == strings.TrimSpace(modeID) {
			return true
		}
	}
	return false
}

func resolveCursorModeLabel(modeID string) string {
	for _, option := range cursorModeOptions {
		if option.OptionID == modeID {
			return option.Label
		}
	}
	return "模式"
}

type cursorRateLimitWindow struct {
	UsedPercentage float64
	ResetsAt       int64
}

// buildCursorRateLimitItems 复用 connector 的 fiveHour/sevenDay 槽位：
// fiveHour → 月度套餐已用%（centerText「M」）
// sevenDay → API 分项已用%（centerText「API」）
// 用量为 0 的档位不渲染；尚无 rate_limits 数据时保留一条「M」占位以便点击拉取。
func buildCursorRateLimitItems(in core.BuildInput) []toolprotocol.Item {
	if !in.Runtime.Online || !in.Runtime.HasLocalAction("get_rate_limits") {
		return nil
	}
	limits := parseCursorRateLimits(in.Binding.Meta)
	if limits == nil {
		// 占位一条「M」，点击可触发 get_rate_limits 拉取。
		return []toolprotocol.Item{
			buildCursorRateLimitProgressItem("rate_limit_monthly", "rate_limits", "M", "rate_limit_monthly_usage", 0, 0),
		}
	}
	var items []toolprotocol.Item
	// 用量为 0 的月度/API 限额不渲染；有真实数据且全为 0 时不回退占位。
	if fh, ok := limits["fiveHour"]; ok && fh.ResetsAt > 0 && fh.UsedPercentage != 0 {
		items = append(items, buildCursorRateLimitProgressItem("rate_limit_monthly", "rate_limits", "M", "rate_limit_monthly_usage", fh.UsedPercentage, fh.ResetsAt))
	}
	if sd, ok := limits["sevenDay"]; ok && sd.ResetsAt > 0 && sd.UsedPercentage != 0 {
		items = append(items, buildCursorRateLimitProgressItem("rate_limit_api", "rate_limits", "API", "rate_limit_api_usage", sd.UsedPercentage, sd.ResetsAt))
	}
	return items
}

func buildCursorRateLimitProgressItem(itemID, groupID, centerText, descPrefix string, percent float64, resetsAt int64) toolprotocol.Item {
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

func parseCursorRateLimits(meta map[string]any) map[string]cursorRateLimitWindow {
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
	result := make(map[string]cursorRateLimitWindow, 2)
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
		result[key] = cursorRateLimitWindow{
			UsedPercentage: pct,
			ResetsAt:       int64(resetsAt),
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func nestedMap(parent map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if raw, ok := parent[key]; ok {
			if m, ok := raw.(map[string]any); ok {
				return m
			}
		}
	}
	return nil
}

func toSnakeCaseKey(key string) string {
	switch key {
	case "fiveHour":
		return "five_hour"
	case "sevenDay":
		return "seven_day"
	default:
		return key
	}
}

func parseNumber(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case int32:
		return float64(n)
	case string:
		s := strings.TrimSpace(n)
		if s == "" {
			return 0
		}
		// connector 偶发 ISO；工具栏要求 unix ms / epoch seconds 数值。
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return float64(t.UnixMilli())
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return float64(t.UnixMilli())
		}
		var f float64
		_, _ = fmt.Sscanf(s, "%f", &f)
		return f
	default:
		return 0
	}
}

const cursorContextWarningThreshold = 80.0

type cursorContextWindow struct {
	UsedPercentage      float64
	RemainingPercentage float64
	HasRemaining        bool
}

func parseCursorContextWindow(meta map[string]any) cursorContextWindow {
	if meta == nil {
		return cursorContextWindow{}
	}
	raw, ok := meta["context_window"]
	if !ok || raw == nil {
		return cursorContextWindow{}
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return cursorContextWindow{}
	}
	return cursorContextWindow{
		UsedPercentage:      parseNumber(obj["usedPercentage"]),
		RemainingPercentage: parseNumber(obj["remainingPercentage"]),
		HasRemaining:        obj["remainingPercentage"] != nil,
	}
}

func canCursorCompactThread(in core.BuildInput) bool {
	return in.Runtime.Online && in.Runtime.HasLocalAction("thread_compact")
}

func cursorCompactTooltip(in core.BuildInput) string {
	switch {
	case !in.Runtime.Online:
		return "Cursor 当前离线"
	case !in.Runtime.HasLocalAction("thread_compact"):
		return "当前插件未声明 thread_compact"
	default:
		return "压缩当前会话上下文（摘要后换新 chat）"
	}
}

func handleCursorCompactThread(in core.ActionInput) (toolprotocol.ActionResult, error) {
	if !canCursorCompactThread(in.BuildInput) {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "action_unavailable",
			Message: cursorCompactTooltip(in.BuildInput),
		}, nil
	}
	// 与 Claude/Codex 对齐：压缩是长异步，AcceptedNoStateChange 让前端 loading
	// 保持到 thread_compact_result 刷新快照；不要 ImmediateRefresh 提前清掉 loading。
	if err := in.Executor.DispatchLocalAction(context.Background(), core.LocalActionRequest{
		OwnerID:    in.BuildInput.OwnerID,
		AgentID:    in.BuildInput.Agent.AgentID,
		SessionID:  in.BuildInput.Session.SessionID,
		ActionType: "thread_compact",
		Params: map[string]any{
			"session_id": in.BuildInput.Session.SessionID,
			"actor_id":   in.BuildInput.OwnerID,
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

func buildCursorContextCompactProgressItem(in core.BuildInput) toolprotocol.Item {
	ctx := parseCursorContextWindow(in.Binding.Meta)
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
	if percent >= cursorContextWarningThreshold {
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
		Tooltip:        cursorCompactTooltip(in),
		Disabled:       !canCursorCompactThread(in),
		LocalAction:    "thread_compact",
	}
}
