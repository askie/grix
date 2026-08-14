package openclaw

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
func (p *Package) Key() string { return model.AgentClientTypeOpenClaw }
func (p *Package) Match(ctx core.MatchContext) bool {
	return ctx.Agent.ClientType == model.AgentClientTypeOpenClaw
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

	if in.Runtime.HasLocalAction("get_session_usage") {
		usageDisabled := !in.Runtime.Online || !in.Runtime.HasLocalAction("get_session_usage")
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

	if len(in.Runtime.Skills) > 0 {
		items = append(items, shared.BuildSkillsItem(in.Runtime.Skills))
	}

	if item, ok := shared.BuildSlashCommandsItem("openclaw"); ok {
		items = append([]toolprotocol.Item{item}, items...)
	}

	return toolprotocol.Snapshot{
		Visible:                true,
		Items:                  items,
		OmitQueueButton:        true,
		OmitListSessionsButton: true,
	}, nil
}

func (p *Package) HandleAction(ctx context.Context, in core.ActionInput) (toolprotocol.ActionResult, error) {
	switch strings.TrimSpace(in.Request.ActionID) {
	case "stop_output":
		return shared.HandleWorkspaceAction(ctx, in)
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
	default:
		return toolprotocol.ActionResult{
			Outcome: toolprotocol.ActionOutcomeRejected,
			Code:    "invalid_action",
			Message: "工具栏动作无效",
		}, nil
	}
}
