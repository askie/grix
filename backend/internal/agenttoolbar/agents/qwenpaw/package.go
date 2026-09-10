// Package qwenpaw 提供 QwenPaw CLI 的聊天窗口工具栏。
//
// QwenPaw 走 grix-connector 的通用 ACP 适配器接入，与 agents/acp 包同构：只放协议
// 本身保证的通用能力，不带 QwenPaw 专属项。模型/供应商在 QwenPaw 侧配置
// （models config-key / set-llm），或经 Grix 中转以 --runtime-provider openai-env
// 临时覆盖；两种情况下可用模型清单都经标准 session/new models 块上报。
package qwenpaw

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
func (p *Package) Key() string { return model.AgentClientTypeQwenPaw }
func (p *Package) Match(ctx core.MatchContext) bool {
	return ctx.Agent.ClientType == model.AgentClientTypeQwenPaw
}

func (p *Package) Build(_ context.Context, in core.BuildInput) (toolprotocol.Snapshot, error) {
	if !hasSessionBinding(in.Binding) {
		return toolprotocol.Snapshot{
			Visible: false,
			Items:   []toolprotocol.Item{},
		}, nil
	}

	items := []toolprotocol.Item{}

	runState := strings.TrimSpace(in.Run.State)
	if in.Run.HasActiveRun && (in.Run.CanStop || runState == "stopping") {
		items = append(items, toolprotocol.Item{
			ItemID:   "stop_output",
			GroupID:  "run_control",
			Kind:     toolprotocol.ItemKindButton,
			ActionID: "stop_output",
			Icon:     "stop",
			Variant:  "danger",
			Disabled: !in.Run.CanStop,
			Tooltip:  stopOutputTooltip(runState),
			Loading:  runState == "stopping",
			Selected: runState == "stopping",
		})
	}

	usageDisabled := !in.Runtime.Online || !in.Runtime.HasLocalAction("get_session_usage")
	items = append(items, toolprotocol.Item{
		ItemID:      "session_control",
		GroupID:     "session_control",
		Kind:        toolprotocol.ItemKindSelect,
		ActionID:    "session_control",
		Icon:        "status",
		Variant:     "secondary",
		Disabled:    !in.Runtime.Online || !in.Runtime.HasLocalAction("session_control"),
		Tooltip:     sessionTooltip(in),
		BadgeText:   sessionBadge(in),
		Placeholder: "选择 QwenPaw 会话操作",
		Options: []toolprotocol.Option{
			{OptionID: "status", Label: "查看状态"},
			{OptionID: "stop", Label: "停止会话"},
			{OptionID: "unbind", Label: "解绑"},
			{OptionID: "usage", Label: "查看用量", Disabled: usageDisabled},
		},
	})

	// 模型/模式清单由连接器按 ACP 会话能力上报；后端不内置任何静态清单，
	// 上报为空就不渲染选择器，避免给不支持切换的 CLI 挂一个永远点不动的入口。
	modelOptions := shared.ParseMetaOptions(in.Binding.Meta, "available_models")
	currentModelID := shared.MetaString(in.Binding.Meta, "model_id")
	if len(modelOptions) > 0 || currentModelID != "" {
		modelSelect := shared.ModelSelect("QwenPaw")
		modelSelect.Value = currentModelID
		modelSelect.Badge = shared.OptionLabel(currentModelID, modelOptions)
		modelSelect.Options = modelOptions
		items = append(items, shared.BuildSelect(in, modelSelect))
	}

	modeOptions := shared.ParseMetaOptions(in.Binding.Meta, "available_modes")
	if len(modeOptions) > 0 {
		currentModeID := shared.MetaString(in.Binding.Meta, "mode_id")
		modeSelect := shared.ModeSelect("QwenPaw")
		modeSelect.Value = currentModeID
		modeSelect.Badge = shared.OptionLabel(currentModeID, modeOptions)
		modeSelect.Options = modeOptions
		items = append(items, shared.BuildSelect(in, modeSelect))
	}

	if len(in.Runtime.Skills) > 0 {
		items = append(items, shared.BuildSkillsItem(in.Runtime.Skills))
	}

	if item, ok := shared.BuildSlashCommandsItem("qwenpaw"); ok {
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
	default:
		return rejected("invalid_action", "工具栏动作无效"), nil
	}
}

func handleStopOutput(in core.ActionInput) (toolprotocol.ActionResult, error) {
	if !in.BuildInput.Run.HasActiveRun || !in.BuildInput.Run.CanStop {
		return rejected("stop_unavailable", "当前没有可停止的输出"), nil
	}
	if err := in.Executor.StopOutput(context.Background(), core.StopOutputRequest{
		OwnerID:   in.BuildInput.OwnerID,
		SessionID: in.BuildInput.Session.SessionID,
		RunID:     in.BuildInput.Run.RunID,
		AgentID:   in.BuildInput.Agent.AgentID,
	}); err != nil {
		return rejected("stop_failed", err.Error()), nil
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
	case "status", "stop", "unbind":
	case "usage":
		return handleGetSessionUsage(in)
	default:
		return rejected("invalid_option", "工具栏选项无效"), nil
	}
	return dispatch(in, "session_control", map[string]any{
		"session_id": in.BuildInput.Session.SessionID,
		"verb":       verb,
	}, 60_000, toolprotocol.ActionOutcomeAcceptedNoStateChange, "已提交会话操作")
}

func handleGetSessionUsage(in core.ActionInput) (toolprotocol.ActionResult, error) {
	return dispatch(in, "get_session_usage", map[string]any{
		"session_id": in.BuildInput.Session.SessionID,
	}, 20_000, toolprotocol.ActionOutcomeAcceptedNoStateChange, "已提交用量查询请求")
}

func handleSelectModel(in core.ActionInput) (toolprotocol.ActionResult, error) {
	modelID := strings.TrimSpace(in.Request.OptionID)
	if modelID == "" {
		return rejected("invalid_option", "未选择模型"), nil
	}
	return dispatch(in, "set_model", map[string]any{
		"session_id":    in.BuildInput.Session.SessionID,
		"model_id":      modelID,
		"display_label": shared.OptionLabel(modelID, shared.ParseMetaOptions(in.BuildInput.Binding.Meta, "available_models")),
	}, 15_000, toolprotocol.ActionOutcomeAcceptedWithImmediateRefresh, "已切换模型")
}

func handleSelectMode(in core.ActionInput) (toolprotocol.ActionResult, error) {
	modeID := strings.TrimSpace(in.Request.OptionID)
	if modeID == "" {
		return rejected("invalid_option", "未选择模式"), nil
	}
	return dispatch(in, "set_mode", map[string]any{
		"session_id":    in.BuildInput.Session.SessionID,
		"mode_id":       modeID,
		"display_label": shared.OptionLabel(modeID, shared.ParseMetaOptions(in.BuildInput.Binding.Meta, "available_modes")),
	}, 15_000, toolprotocol.ActionOutcomeAcceptedWithImmediateRefresh, "已切换模式")
}

// ── 辅助函数 ──

// dispatch 统一做"在线 + 已声明 local action"的前置校验再下发，
// 这两条拒绝语与其它 agent 包保持一致。
func dispatch(
	in core.ActionInput,
	actionType string,
	params map[string]any,
	timeoutMs int,
	outcome toolprotocol.ActionOutcome,
	message string,
) (toolprotocol.ActionResult, error) {
	if !in.BuildInput.Runtime.Online {
		return rejected("agent_offline", "当前 agent 不在线"), nil
	}
	if !in.BuildInput.Runtime.HasLocalAction(actionType) {
		return rejected("local_action_unavailable", "当前 agent 未声明 "+actionType), nil
	}
	if err := in.Executor.DispatchLocalAction(context.Background(), core.LocalActionRequest{
		OwnerID:    in.BuildInput.OwnerID,
		AgentID:    in.BuildInput.Agent.AgentID,
		SessionID:  in.BuildInput.Session.SessionID,
		ActionType: actionType,
		Params:     params,
		TimeoutMs:  timeoutMs,
	}); err != nil {
		return rejected("dispatch_failed", err.Error()), nil
	}
	return toolprotocol.ActionResult{Outcome: outcome, Code: "accepted", Message: message}, nil
}

func rejected(code, message string) toolprotocol.ActionResult {
	return toolprotocol.ActionResult{
		Outcome: toolprotocol.ActionOutcomeRejected,
		Code:    code,
		Message: message,
	}
}

func hasSessionBinding(binding core.BindingInfo) bool {
	return strings.TrimSpace(binding.BindingID) != "" ||
		strings.TrimSpace(binding.Cwd) != "" ||
		strings.TrimSpace(binding.WorkerStatus) != ""
}

func stopOutputTooltip(runState string) string {
	if runState == "stopping" {
		return "正在停止"
	}
	return "停止当前输出"
}

func sessionTooltip(in core.BuildInput) string {
	switch {
	case !in.Runtime.Online:
		return "QwenPaw 当前离线"
	case !in.Runtime.HasLocalAction("session_control"):
		return "当前插件未声明 session_control"
	case strings.TrimSpace(in.Binding.Cwd) != "":
		return strings.TrimSpace(in.Binding.Cwd)
	default:
		return ""
	}
}

func sessionBadge(in core.BuildInput) string {
	if cwd := strings.TrimSpace(in.Binding.Cwd); cwd != "" {
		return shared.PathBase(cwd)
	}
	switch worker := strings.TrimSpace(in.Binding.WorkerStatus); worker {
	case "":
	case "session_expired":
		return "会话已过期"
	default:
		return worker
	}
	if in.Runtime.Online {
		return "在线"
	}
	return "离线"
}
