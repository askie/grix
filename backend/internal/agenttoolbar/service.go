package agenttoolbar

import (
	"context"

	"github.com/askie/grix/backend/internal/agenttoolbar/agents/agy"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/claude"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/codewhale"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/codex"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/copilot"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/cursor"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/deepseek"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/gemini"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/hermes"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/kimi"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/kiro"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/openclaw"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/opencode"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/openhuman"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/pi"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/qwen"
	"github.com/askie/grix/backend/internal/agenttoolbar/agents/reasonix"
	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolnotifier "github.com/askie/grix/backend/internal/agenttoolbar/notifier"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	"github.com/askie/grix/backend/internal/agenttoolbar/registry"
	"github.com/askie/grix/backend/internal/agenttoolbar/resolver"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	toolstore "github.com/askie/grix/backend/internal/agenttoolbar/store"
	wsprotocol "github.com/askie/grix/backend/internal/ws/protocol"
)

type Dependencies struct {
	Fanout            toolnotifier.FanoutFunc
	Executor          core.Executor
	ActiveRunProvider resolver.ActiveRunProvider
}

type Service struct {
	core *core.Service
}

func NewService(deps Dependencies) *Service {
	reg := registry.New()
	reg.Register(agy.New())
	reg.Register(claude.New())
	reg.Register(codex.New())
	reg.Register(cursor.New())
	reg.Register(deepseek.New())
	reg.Register(gemini.New())
	reg.Register(hermes.New())
	reg.Register(openclaw.New())
	reg.Register(openhuman.New())
	reg.Register(pi.New())
	reg.Register(qwen.New())
	reg.Register(reasonix.New())
	reg.Register(codewhale.New())
	reg.Register(opencode.New())
	reg.Register(kimi.New())
	reg.Register(kiro.New())
	reg.Register(copilot.New())

	return &Service{
		core: core.NewService(
			resolver.New(deps.ActiveRunProvider),
			reg,
			toolstore.NewCacheStore(),
			toolnotifier.New(deps.Fanout),
			deps.Executor,
		),
	}
}

func (s *Service) GetSnapshot(ctx context.Context, ownerID int64, sessionID string, targetAgentID int64) (toolprotocol.Snapshot, error) {
	return s.core.GetSnapshot(ctx, ownerID, sessionID, targetAgentID)
}

func (s *Service) HandleAction(ctx context.Context, ownerID int64, req toolprotocol.ActionRequest) (core.ActionAck, error) {
	return s.core.HandleAction(ctx, ownerID, req)
}

func (s *Service) InvalidateSession(ctx context.Context, ownerID int64, sessionID string) {
	if s == nil {
		return
	}
	s.core.InvalidateSession(ctx, ownerID, sessionID)
}

func (s *Service) RefreshSession(ctx context.Context, ownerID int64, sessionID, reason string) error {
	return s.core.RefreshSession(ctx, ownerID, sessionID, reason)
}

func (s *Service) RefreshSessionForAgent(ctx context.Context, ownerID int64, sessionID string, agentID int64, reason string) error {
	return s.core.RefreshSessionForAgent(ctx, ownerID, sessionID, agentID, reason)
}

func (s *Service) RefreshByAgent(ctx context.Context, ownerID, agentID int64, reason string) error {
	return s.core.RefreshByAgent(ctx, ownerID, agentID, reason)
}

func ToWireSnapshot(snapshot toolprotocol.Snapshot) wsprotocol.AgentToolbarSnapshotPayload {
	return toolnotifierSnapshot(snapshot)
}

func ToWireActionAck(ack core.ActionAck) wsprotocol.AgentToolbarActionAckPayload {
	return wsprotocol.AgentToolbarActionAckPayload{
		SessionID:       ack.SessionID,
		ToolbarID:       ack.ToolbarID,
		ClientActionID:  ack.ClientActionID,
		Accepted:        ack.Accepted,
		Duplicate:       ack.Duplicate,
		Code:            ack.Code,
		Msg:             ack.Message,
		CurrentRevision: ack.CurrentRevision,
		UpdatedAt:       ack.UpdatedAt,
	}
}

func toolnotifierSnapshot(snapshot toolprotocol.Snapshot) wsprotocol.AgentToolbarSnapshotPayload {
	items := make([]wsprotocol.AgentToolbarItemPayload, 0, len(snapshot.Items))
	for _, item := range snapshot.Items {
		options := make([]wsprotocol.AgentToolbarOptionPayload, 0, len(item.Options))
		for _, option := range item.Options {
			options = append(options, wsprotocol.AgentToolbarOptionPayload{
				OptionID: option.OptionID,
				Label:    option.Label,
				Disabled: option.Disabled,
			})
		}
		items = append(items, wsprotocol.AgentToolbarItemPayload{
			ItemID:         item.ItemID,
			GroupID:        item.GroupID,
			Kind:           item.Kind,
			ActionID:       item.ActionID,
			Label:          item.Label,
			Icon:           item.Icon,
			Variant:        item.Variant,
			Disabled:       item.Disabled,
			Loading:        item.Loading,
			Selected:       item.Selected,
			Tooltip:        item.Tooltip,
			BadgeText:      item.BadgeText,
			ConfirmTitle:   item.ConfirmTitle,
			ConfirmText:    item.ConfirmText,
			Value:          item.Value,
			Placeholder:    item.Placeholder,
			Options:        options,
			Percent:        item.Percent,
			CenterText:     item.CenterText,
			ProgressDesc:   item.ProgressDesc,
			ProgressDetail: item.ProgressDetail,
			LocalAction:    item.LocalAction,
			Commands:       toWireCommands(item.Commands),
		})
	}
	return wsprotocol.AgentToolbarSnapshotPayload{
		SessionID:     snapshot.SessionID,
		AgentID:       snapshot.AgentID,
		ToolbarID:     snapshot.ToolbarID,
		Revision:      snapshot.Revision,
		Visible:       snapshot.Visible,
		UpdatedAt:     snapshot.UpdatedAt,
		Items:         items,
		LibrarySkills: toWireLibrarySkills(snapshot.LibrarySkills),
		AuditEnabled:  snapshot.AuditEnabled,
	}
}

func toWireLibrarySkills(skills []toolruntime.LibrarySkillEntry) []wsprotocol.AgentToolbarLibrarySkillPayload {
	if len(skills) == 0 {
		return nil
	}
	out := make([]wsprotocol.AgentToolbarLibrarySkillPayload, 0, len(skills))
	for _, skill := range skills {
		out = append(out, wsprotocol.AgentToolbarLibrarySkillPayload{
			Name:        skill.Name,
			Description: skill.Description,
			Digest:      skill.Digest,
			Dir:         skill.Dir,
			OwnerID:     skill.OwnerID,
			System:      skill.System,
			EnableScopes: wsprotocol.AgentToolbarLibrarySkillScopesPayload{
				Global:  skill.EnableScopes.Global,
				Project: skill.EnableScopes.Project,
			},
		})
	}
	return out
}

func toWireCommands(commands []toolprotocol.CommandItem) []wsprotocol.AgentToolbarCommandItemPayload {
	if len(commands) == 0 {
		return nil
	}
	out := make([]wsprotocol.AgentToolbarCommandItemPayload, 0, len(commands))
	for _, cmd := range commands {
		out = append(out, wsprotocol.AgentToolbarCommandItemPayload{
			ID:          cmd.ID,
			Name:        cmd.Name,
			Description: cmd.Description,
			Exec:        cmd.Exec,
			Managed:     cmd.Managed,
			SyncState:   cmd.SyncState,
		})
	}
	return out
}
