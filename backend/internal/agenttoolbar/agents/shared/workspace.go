package shared

import (
	"context"
	"strings"

	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
)

// BuildWorkspaceSnapshot 构建基础工具栏:仅保留停止输出与技能入口,
// 并声明不前置队列按钮。不再产出会话(session_control)下拉项。
// 文件浏览按钮由规范化阶段统一前置。
func BuildWorkspaceSnapshot(_ context.Context, in core.BuildInput) (toolprotocol.Snapshot, error) {
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

	if len(in.Runtime.Skills) > 0 {
		items = append(items, BuildSkillsItem(in.Runtime.Skills))
	}

	return toolprotocol.Snapshot{
		Visible:         true,
		Items:           items,
		OmitQueueButton: true,
	}, nil
}

// HandleWorkspaceAction 仅处理停止输出动作,其余动作一律拒绝。
func HandleWorkspaceAction(_ context.Context, in core.ActionInput) (toolprotocol.ActionResult, error) {
	if strings.TrimSpace(in.Request.ActionID) != "stop_output" {
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_action",
			Message: "工具栏动作无效",
		}, nil
	}
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
