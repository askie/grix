package agy

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/askie/grix/backend/internal/agenttoolbar/agents/shared"
	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	"github.com/askie/grix/backend/internal/model"
)

// Package 为 agy（Antigravity CLI）提供聊天窗口工具栏。
// agy 经 grix-connector 的 print 模式接入，每条消息独立 spawn 子进程，
// 暴露：停止输出、工作空间下拉（查看状态/重启/解绑）、账号限额条、模型切换。
// 连接器对 agy 不声明 get_session_usage（print 模式无可靠会话累计），
// 工具栏因此不提供「查看用量」入口；账号额度经 get_rate_limits 刷新。
type Package struct{}

func New() *Package            { return &Package{} }
func (p *Package) Key() string { return model.AgentClientTypeAgy }
func (p *Package) Match(ctx core.MatchContext) bool {
	return ctx.Agent.ClientType == model.AgentClientTypeAgy
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

	if hasAgySessionBinding(in.Binding) {
		sessionDisabled := !in.Runtime.Online || !in.Runtime.HasLocalAction("session_control")
		sessionTooltip := "agy 会话操作"
		switch {
		case !in.Runtime.Online:
			sessionTooltip = "agy 当前离线"
		case !in.Runtime.HasLocalAction("session_control"):
			sessionTooltip = "当前插件未声明 session_control"
		case strings.TrimSpace(in.Binding.Cwd) != "":
			sessionTooltip = "agy 会话操作\n工作目录: " + strings.TrimSpace(in.Binding.Cwd)
		}

		badge := ""
		if cwd := strings.TrimSpace(in.Binding.Cwd); cwd != "" {
			badge = shared.PathBase(cwd)
		} else if in.Runtime.Online {
			badge = "在线"
		} else {
			badge = "离线"
		}

		items = append(items, toolprotocol.Item{
			ItemID:    "session_control",
			GroupID:   "session_control",
			Kind:      toolprotocol.ItemKindSelect,
			ActionID:  "session_control",
			Icon:      "status",
			Variant:   "secondary",
			Disabled:  sessionDisabled,
			Tooltip:   sessionTooltip,
			BadgeText: badge,
			Options: []toolprotocol.Option{
				{OptionID: "status", Label: "查看状态"},
				{OptionID: "restart", Label: "重启会话"},
				{OptionID: "unbind", Label: "解绑"},
			},
		})

		// 用量条目（限额进度或 legacy 配额兜底）紧跟工作空间下拉之后。
		items = append(items, buildAgyUsageItems(in)...)

		items = append(items, shared.BuildSelect(in, agyModelSelect(in)))
	} else {
		items = append(items, buildAgyUsageItems(in)...)
	}

	if len(in.Runtime.Skills) > 0 {
		items = append(items, shared.BuildSkillsItem(in.Runtime.Skills))
	}

	if item, ok := shared.BuildSlashCommandsItem("agy"); ok {
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
		return shared.HandleWorkspaceAction(ctx, in)
	case "session_control":
		return handleAgySessionControl(in)
	case "select_model":
		return handleSelectModel(in)
	case "get_rate_limits":
		return handleAgyGetRateLimits(in)
	default:
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_action",
			Message: "工具栏动作无效",
		}, nil
	}
}

func handleAgySessionControl(in core.ActionInput) (toolprotocol.ActionResult, error) {
	optionID := strings.TrimSpace(in.Request.OptionID)
	switch optionID {
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
			Message: "agy 当前离线",
		}, nil
	}
	if !in.BuildInput.Runtime.HasLocalAction("session_control") {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "local_action_unavailable",
			Message: "当前插件未声明 session_control",
		}, nil
	}
	params := map[string]any{
		"session_id": in.BuildInput.Session.SessionID,
		"verb":       optionID,
	}
	timeoutMs := 15_000
	msg := "已提交会话操作"
	if optionID == "restart" {
		timeoutMs = 30_000
		msg = "已提交重启请求"
	}
	if err := in.Executor.DispatchLocalAction(context.Background(), core.LocalActionRequest{
		OwnerID:    in.BuildInput.OwnerID,
		AgentID:    in.BuildInput.Agent.AgentID,
		SessionID:  in.BuildInput.Session.SessionID,
		ActionType: "session_control",
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
		Message: msg,
	}, nil
}

func handleAgyGetRateLimits(in core.ActionInput) (toolprotocol.ActionResult, error) {
	if !in.BuildInput.Runtime.Online {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "agent_offline",
			Message: "agy 当前离线",
		}, nil
	}
	if !in.BuildInput.Runtime.HasLocalAction("get_rate_limits") {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "local_action_unavailable",
			Message: "当前插件未声明 get_rate_limits",
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
		// 连接器 agy 走 CLI 采集（exec 超时 35s）+ legacy 回退，留足余量。
		TimeoutMs: 45_000,
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

func handleSelectModel(in core.ActionInput) (toolprotocol.ActionResult, error) {
	modelID := strings.TrimSpace(in.Request.OptionID)
	if modelID == "" {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_option",
			Message: "未选择模型",
		}, nil
	}
	modelSelect := agyModelSelect(in.BuildInput)
	if item := shared.BuildSelect(in.BuildInput, modelSelect); item.Disabled {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "action_unavailable",
			Message: item.Tooltip,
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
			"display_label": shared.OptionLabel(modelID, modelSelect.Options),
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
		Message: "已提交模型切换请求",
	}, nil
}

// buildAgyModelOptions 从 binding meta 的 available_models 解析模型选项。
// agy 的模型 id 即显示名（来自 `agy models`）。
// buildAgyModelOptions 读取连接器上报的 available_models（统一契约见 shared.ParseMetaOptions）。

// agyModelSelect 组装模型选择器：Label/Value 都是显示名（缺值回落清单第一项）。
func agyModelSelect(in core.BuildInput) shared.SelectSpec {
	options := shared.ParseMetaOptions(in.Binding.Meta, "available_models")
	currentModelID := agyMetaString(in.Binding.Meta, "model_id")
	label := shared.OptionLabel(currentModelID, options)
	if label == "" && len(options) > 0 {
		label = options[0].Label
	}
	if label == "" {
		label = "模型"
	}
	spec := shared.ModelSelect("agy")
	spec.Placeholder = "模型"
	spec.Label = label
	spec.Value = label
	spec.Options = options
	return spec
}

func agyMetaString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	value, ok := meta[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func hasAgySessionBinding(binding core.BindingInfo) bool {
	return strings.TrimSpace(binding.BindingID) != "" ||
		strings.TrimSpace(binding.Cwd) != ""
}

type agyRateLimitWindow struct {
	UsedPercent   float64
	HasPercent    bool
	WindowMinutes float64
	ResetsAt      string
}

// buildAgyUsageItems 返回用量展示条目：优先限额进度条，
// 无限额数据时回退 legacy 配额条目（配额耗尽或积分）。
func buildAgyUsageItems(in core.BuildInput) []toolprotocol.Item {
	rateLimitItems, hasRateLimitData := buildAgyRateLimitItems(in)
	if len(rateLimitItems) > 0 {
		return rateLimitItems
	}
	if !hasRateLimitData {
		if quotaItem := buildAgyQuotaItem(in.Binding.Meta); quotaItem != nil {
			return []toolprotocol.Item{*quotaItem}
		}
	}
	return nil
}

func buildAgyRateLimitItems(in core.BuildInput) ([]toolprotocol.Item, bool) {
	if !in.Runtime.Online || !in.Runtime.HasLocalAction("get_rate_limits") {
		return nil, false
	}
	limits := parseAgyRateLimits(in.Binding.Meta)
	extras := parseAgyExtraLimits(in.Binding.Meta)

	var items []toolprotocol.Item
	if primary, ok := limits["primary"]; ok && primary.HasPercent {
		items = append(items, buildAgyRateLimitProgressItem(
			"rate_limit_primary",
			agyWindowCenterText(primary.WindowMinutes, "5H"),
			"Gemini 5H",
			primary.UsedPercent,
			primary.ResetsAt,
			primary.WindowMinutes,
		))
	}
	if secondary, ok := limits["secondary"]; ok && secondary.HasPercent {
		items = append(items, buildAgyRateLimitProgressItem(
			"rate_limit_secondary",
			agyWindowCenterText(secondary.WindowMinutes, "7D"),
			"Gemini weekly",
			secondary.UsedPercent,
			secondary.ResetsAt,
			secondary.WindowMinutes,
		))
	}
	for i, extra := range extras {
		items = append(items, buildAgyRateLimitProgressItem(
			fmt.Sprintf("rate_limit_extra_%d", i),
			shared.PercentCenterText(extra.UsedPercent),
			extra.Label,
			extra.UsedPercent,
			extra.ResetsAt,
			extra.WindowMinutes,
		))
	}

	// 连接器采集失败时会把 rate_limits/extra_limits 置空（nullable 键覆盖），
	// 同时用 legacy 字段（quota_exhausted/available_credits/plan）回退上报。
	// 这里只按「解析出了可展示的窗口」判定有无数据，空值不压制 legacy 兜底条目。
	return items, len(items) > 0
}

// buildAgyRateLimitProgressItem 构造限额进度条目。
// ProgressDetail only carries the reset timestamp. ProgressWindowMinutes is
// carried separately so the client can calculate the elapsed-time ring even
// when CenterText is the used percentage for an extra limit.
func buildAgyRateLimitProgressItem(itemID, centerText, desc string, percent float64, resetsAt string, windowMinutes float64) toolprotocol.Item {
	return toolprotocol.Item{
		ItemID:                itemID,
		GroupID:               "rate_limits",
		Kind:                  toolprotocol.ItemKindProgress,
		ActionID:              "get_rate_limits",
		Variant:               "secondary",
		Percent:               percent,
		CenterText:            centerText,
		ProgressDesc:          desc,
		ProgressDetail:        strings.TrimSpace(resetsAt),
		ProgressWindowMinutes: windowMinutes,
		LocalAction:           "get_rate_limits",
	}
}

func parseAgyRateLimits(meta map[string]any) map[string]agyRateLimitWindow {
	if meta == nil {
		return nil
	}
	raw, ok := meta["rate_limits"]
	if !ok || raw == nil {
		return nil
	}
	limitsMap, ok := raw.(map[string]any)
	if !ok || limitsMap["sampledAt"] == nil {
		return nil
	}

	result := make(map[string]agyRateLimitWindow, 2)
	for _, key := range []string{"primary", "secondary"} {
		entry, ok := limitsMap[key].(map[string]any)
		if !ok {
			continue
		}
		usedPercent, hasPercent := agyMetaValidPercent(entry, "usedPercent")
		result[key] = agyRateLimitWindow{
			UsedPercent:   usedPercent,
			HasPercent:    hasPercent,
			WindowMinutes: agyMetaFloat64(entry, "windowMinutes"),
			ResetsAt:      agyMetaString(entry, "resetsAt"),
		}
	}
	return result
}

func parseAgyExtraLimits(meta map[string]any) []shared.ExtraLimit {
	if meta == nil {
		return nil
	}
	raw, ok := meta["extra_limits"]
	if !ok || raw == nil {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}

	result := make([]shared.ExtraLimit, 0, len(arr))
	for _, item := range arr {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		label := strings.TrimSpace(agyMetaString(obj, "label"))
		usedPercent, ok := agyMetaValidPercent(obj, "usedPercent")
		if label == "" || !ok {
			continue
		}
		result = append(result, shared.ExtraLimit{
			ID:            strings.TrimSpace(agyMetaString(obj, "id")),
			Label:         label,
			UsedPercent:   usedPercent,
			WindowMinutes: agyMetaFloat64(obj, "windowMinutes"),
			ResetsAt:      agyMetaString(obj, "resetsAt"),
		})
	}
	return result
}

func agyWindowCenterText(windowMinutes float64, fallback string) string {
	if label := shared.FormatWindowLabel(windowMinutes); label != "" {
		return label
	}
	return fallback
}

// buildAgyQuotaItem 根据 connector 上报的配额信息构建工具栏条目。
// 仅当有实质性信息时才返回条目（配额耗尽或有积分数据），否则返回 nil。
func buildAgyQuotaItem(meta map[string]any) *toolprotocol.Item {
	exhausted := agyMetaBool(meta, "quota_exhausted")
	resetAt := agyMetaInt64(meta, "quota_reset_at")
	credits := agyMetaFloat64(meta, "available_credits")
	plan := agyMetaString(meta, "plan")

	if exhausted {
		detail := ""
		if resetAt > 0 {
			detail = fmt.Sprintf("%d", resetAt)
		}
		desc := "agy 配额耗尽"
		if plan != "" {
			desc = plan + " 配额耗尽"
		}
		return &toolprotocol.Item{
			ItemID:         "agy_quota",
			GroupID:        "agy_quota",
			Kind:           toolprotocol.ItemKindProgress,
			ActionID:       "agy_quota",
			Variant:        "danger",
			Percent:        100,
			CenterText:     "耗尽",
			ProgressDesc:   desc,
			ProgressDetail: detail,
		}
	}

	if credits > 0 {
		desc := "agy 配额"
		if plan != "" {
			desc = plan
		}
		return &toolprotocol.Item{
			ItemID:         "agy_quota",
			GroupID:        "agy_quota",
			Kind:           toolprotocol.ItemKindProgress,
			ActionID:       "agy_quota",
			Variant:        "secondary",
			Percent:        0,
			CenterText:     "积分",
			ProgressDesc:   desc,
			ProgressDetail: fmt.Sprintf("%.0f 积分", credits),
		}
	}

	return nil
}

func agyMetaBool(meta map[string]any, key string) bool {
	if meta == nil {
		return false
	}
	v, ok := meta[key]
	if !ok || v == nil {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

func agyMetaFloat64(meta map[string]any, key string) float64 {
	value, ok := agyMetaNumber(meta, key)
	if !ok {
		return 0
	}
	return value
}

func agyMetaValidPercent(meta map[string]any, key string) (float64, bool) {
	value, ok := agyMetaNumber(meta, key)
	return value, ok && value >= 0
}

func agyMetaNumber(meta map[string]any, key string) (float64, bool) {
	if meta == nil {
		return 0, false
	}
	v, ok := meta[key]
	if !ok || v == nil {
		return 0, false
	}
	var value float64
	switch n := v.(type) {
	case float64:
		value = n
	case float32:
		value = float64(n)
	case int:
		value = float64(n)
	case int64:
		value = float64(n)
	default:
		return 0, false
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return value, true
}

func agyMetaInt64(meta map[string]any, key string) int64 {
	return int64(agyMetaFloat64(meta, key))
}
