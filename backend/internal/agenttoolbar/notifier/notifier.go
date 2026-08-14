package notifier

import (
	"context"

	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	wsprotocol "github.com/askie/grix/backend/internal/ws/protocol"
)

type FanoutFunc func(ctx context.Context, ownerID int64, cmd string, payload any)

type Notifier struct {
	fanout FanoutFunc
}

func New(fanout FanoutFunc) *Notifier {
	return &Notifier{fanout: fanout}
}

func (n *Notifier) Sync(ctx context.Context, ownerID int64, snapshot toolprotocol.Snapshot) error {
	if n == nil || n.fanout == nil || ownerID <= 0 {
		return nil
	}
	n.fanout(ctx, ownerID, wsprotocol.CmdAgentToolbarSync, toWireSnapshot(snapshot))
	return nil
}

func toWireSnapshot(snapshot toolprotocol.Snapshot) wsprotocol.AgentToolbarSnapshotPayload {
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
			Toggles:        toWireToggles(item.Toggles),
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
		})
	}
	return out
}

func toWireToggles(toggles []toolprotocol.ToggleItem) []wsprotocol.AgentToolbarToggleItemPayload {
	if len(toggles) == 0 {
		return nil
	}
	out := make([]wsprotocol.AgentToolbarToggleItemPayload, 0, len(toggles))
	for _, item := range toggles {
		out = append(out, wsprotocol.AgentToolbarToggleItemPayload{
			ID:         item.ID,
			Name:       item.Name,
			Version:    item.Version,
			Enabled:    item.Enabled,
			Locked:     item.Locked,
			LockReason: item.LockReason,
		})
	}
	return out
}
