// Package acp 提供通用 ACP（Agent Client Protocol）接入的工具栏。
//
// 它服务 client_type "acp"：用户自己配置 command/args 拉起的任意 ACP 兼容 CLI。
// 后端不知道那是哪个厂商的 CLI，因此这里只放协议本身保证的通用能力——停止输出
// （ACP 的 session/cancel），以及连接器上报清单后才渲染的模型/模式选择，
// 不带任何厂商专属项（额度卡、上下文窗口、内置斜杠命令清单等一律不做）。
//
// 这里**没有**会话控制项。通用 ACP 的工作目录由连接器按 agent 配置静态带入、
// 用户不可更改（见连接器 src/bridge/cwd-policy.ts），所以绑定/解绑/查看目录
// 这几个入口对它没有意义，留着只会把用户带回一条已经关闭的流程。同理不前置
// 会话列表按钮：那要扫描已知 CLI 的历史会话目录布局，未知 CLI 扫不出来。
//
// 空闲时没有任何项，工具栏整体不可见；跑任务时才出现停止按钮。
//
// 例外是 agent 自己声明的工具栏项（下拉框与说明按钮，见 custom_items.go）：
// 声明了就常驻显示，因为那是 agent 主动要给用户的入口。
package acp

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
func (p *Package) Key() string { return model.AgentClientTypeACP }
func (p *Package) Match(ctx core.MatchContext) bool {
	return ctx.Agent.ClientType == model.AgentClientTypeACP
}

func (p *Package) Build(_ context.Context, in core.BuildInput) (toolprotocol.Snapshot, error) {
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

	// 模型/模式清单由连接器按 ACP 会话能力上报；后端不内置任何静态清单，
	// 上报为空就不渲染选择器，避免给不支持切换的 CLI 挂一个永远点不动的入口。
	modelOptions := shared.ParseMetaOptions(in.Binding.Meta, "available_models")
	currentModelID := shared.MetaString(in.Binding.Meta, "model_id")
	if len(modelOptions) > 0 || currentModelID != "" {
		modelSelect := shared.ModelSelect("ACP")
		modelSelect.Value = currentModelID
		modelSelect.Badge = shared.OptionLabel(currentModelID, modelOptions)
		modelSelect.Options = modelOptions
		items = append(items, shared.BuildSelect(in, modelSelect))
	}

	modeOptions := shared.ParseMetaOptions(in.Binding.Meta, "available_modes")
	if len(modeOptions) > 0 {
		currentModeID := shared.MetaString(in.Binding.Meta, "mode_id")
		modeSelect := shared.ModeSelect("ACP")
		modeSelect.Value = currentModeID
		modeSelect.Badge = shared.OptionLabel(currentModeID, modeOptions)
		modeSelect.Options = modeOptions
		items = append(items, shared.BuildSelect(in, modeSelect))
	}

	if len(in.Runtime.Skills) > 0 {
		items = append(items, shared.BuildSkillsItem(in.Runtime.Skills))
	}

	// agent 自己声明的工具栏项（见 custom_items.go）。只有通用 acp 读这个键，
	// 其余 client_type 的工具栏包不看它。
	items = append(items, buildCustomItems(
		parseCustomItems(in.Binding.Meta),
		in.Runtime.Online,
		in.Run.HasActiveRun,
	)...)

	return toolprotocol.Snapshot{
		Visible:                len(items) > 0,
		Items:                  items,
		OmitListSessionsButton: true,
	}, nil
}

func (p *Package) HandleAction(_ context.Context, in core.ActionInput) (toolprotocol.ActionResult, error) {
	switch strings.TrimSpace(in.Request.ActionID) {
	case "stop_output":
		return handleStopOutput(in)
	case "select_model":
		return handleSelectModel(in)
	case "select_mode":
		return handleSelectMode(in)
	case ActionIDCustomSelect:
		return handleCustomSelect(in)
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

// handleCustomSelect 把用户在 agent 自定义下拉框里的选择回传给 agent。
//
// 不走 dispatch()/local_action：那条通道没有 run 上下文，连接器转发 agent 输出
// 依赖 run.eventId，agent 收到选择后就算回话也一个字发不出去。改用主人身份的
// 命令文本（core.CommandTextSender），与停止按钮下发 /stop 是同一条既有链路。
func handleCustomSelect(in core.ActionInput) (toolprotocol.ActionResult, error) {
	if !in.BuildInput.Runtime.Online {
		return rejected("agent_offline", "当前 agent 不在线"), nil
	}
	if in.BuildInput.Run.HasActiveRun {
		return rejected("run_active", "当前有任务运行中，完成后可切换"), nil
	}
	// 以当前快照复核一次：前端可能拿着旧快照点过来，agent 也可能刚撤掉该项。
	item, ok := findCustomSelectOption(
		parseCustomItems(in.BuildInput.Binding.Meta),
		in.Request.ItemID,
		in.Request.OptionID,
	)
	if !ok {
		return rejected("invalid_option", "工具栏选项无效"), nil
	}
	sender, ok := in.Executor.(core.CommandTextSender)
	if !ok {
		return rejected("dispatch_failed", "当前运行环境不支持该操作"), nil
	}
	if err := sender.SendCommandText(context.Background(), core.CommandTextRequest{
		OwnerID:   in.BuildInput.OwnerID,
		AgentID:   in.BuildInput.Agent.AgentID,
		SessionID: in.BuildInput.Session.SessionID,
		Content:   buildCustomSelectCommand(item.ID, strings.TrimSpace(in.Request.OptionID)),
	}); err != nil {
		return rejected("dispatch_failed", err.Error()), nil
	}
	return toolprotocol.ActionResult{
		Outcome: toolprotocol.ActionOutcomeAcceptedWithImmediateRefresh,
		Code:    "accepted",
		Message: "已提交选择",
	}, nil
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

func stopOutputTooltip(runState string) string {
	if runState == "stopping" {
		return "正在停止"
	}
	return "停止当前输出"
}
