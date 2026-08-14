package agy

import (
	"context"
	"fmt"
	"strings"

	"github.com/askie/grix/backend/internal/agenttoolbar/agents/shared"
	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	"github.com/askie/grix/backend/internal/model"
)

// Package 为 agy（Antigravity CLI）提供聊天窗口工具栏。
// agy 经 grix-connector 的 print 模式接入，每条消息独立 spawn 子进程，
// 暴露：停止输出、工作空间下拉（查看状态/重启/用量）、模型切换。
type Package struct{}

type agyModelOption struct {
	ID    string
	Label string
}

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

		usageDisabled := !in.Runtime.Online || !in.Runtime.HasLocalAction("get_session_usage")
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
				{OptionID: "usage", Label: "查看用量", Disabled: usageDisabled},
			},
		})

		options := buildAgyModelOptions(in.Binding.Meta)
		currentLabel := resolveAgyModelLabel(agyMetaString(in.Binding.Meta, "model_id"), options)
		items = append(items, toolprotocol.Item{
			ItemID:      "select_model",
			GroupID:     "model_control",
			Kind:        toolprotocol.ItemKindSelect,
			ActionID:    "select_model",
			Label:       currentLabel,
			Value:       currentLabel,
			Icon:        "cpu",
			Variant:     "secondary",
			Disabled:    !canAgySelectModel(in, options),
			Tooltip:     agyModelTooltip(in, options),
			Placeholder: "模型",
			Options:     toAgyProtocolOptions(options),
		})
	}

	if quotaItem := buildAgyQuotaItem(in.Binding.Meta); quotaItem != nil {
		items = append(items, *quotaItem)
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
	case "status", "restart":
	case "usage":
		return handleAgyGetSessionUsage(in)
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

func handleAgyGetSessionUsage(in core.ActionInput) (toolprotocol.ActionResult, error) {
	if !in.BuildInput.Runtime.Online {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "agent_offline",
			Message: "agy 当前离线",
		}, nil
	}
	if !in.BuildInput.Runtime.HasLocalAction("get_session_usage") {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "local_action_unavailable",
			Message: "当前插件未声明 get_session_usage",
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

func handleSelectModel(in core.ActionInput) (toolprotocol.ActionResult, error) {
	modelID := strings.TrimSpace(in.Request.OptionID)
	if modelID == "" {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_option",
			Message: "未选择模型",
		}, nil
	}
	options := buildAgyModelOptions(in.BuildInput.Binding.Meta)
	if !canAgySelectModel(in.BuildInput, options) {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "action_unavailable",
			Message: agyModelTooltip(in.BuildInput, options),
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
			"display_label": resolveAgyModelLabel(modelID, options),
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
func buildAgyModelOptions(meta map[string]any) []agyModelOption {
	models, ok := meta["available_models"].([]any)
	if !ok {
		return nil
	}
	opts := make([]agyModelOption, 0, len(models))
	seen := map[string]struct{}{}
	for _, raw := range models {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id := strings.TrimSpace(agyMetaString(entry, "id"))
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		label := strings.TrimSpace(agyMetaString(entry, "displayName"))
		if label == "" {
			label = strings.TrimSpace(agyMetaString(entry, "display_name"))
		}
		if label == "" {
			label = id
		}
		opts = append(opts, agyModelOption{ID: id, Label: label})
	}
	return opts
}

func resolveAgyModelLabel(modelID string, options []agyModelOption) string {
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

func toAgyProtocolOptions(options []agyModelOption) []toolprotocol.Option {
	out := make([]toolprotocol.Option, 0, len(options))
	for _, option := range options {
		out = append(out, toolprotocol.Option{
			OptionID: option.ID,
			Label:    option.Label,
		})
	}
	return out
}

func canAgySelectModel(in core.BuildInput, options []agyModelOption) bool {
	return in.Runtime.Online && in.Runtime.HasLocalAction("set_model") && len(options) > 0
}

func agyModelTooltip(in core.BuildInput, options []agyModelOption) string {
	switch {
	case !in.Runtime.Online:
		return "agy 当前离线"
	case !in.Runtime.HasLocalAction("set_model"):
		return "当前插件未声明 set_model"
	case len(options) == 0:
		return "等待 agy 模型列表同步"
	default:
		return "切换 agy 模型"
	}
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
	if meta == nil {
		return 0
	}
	v, ok := meta[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}

func agyMetaInt64(meta map[string]any, key string) int64 {
	return int64(agyMetaFloat64(meta, key))
}
