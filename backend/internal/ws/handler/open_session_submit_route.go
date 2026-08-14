package handler

import (
	"context"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/grixactions"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
)

type openSessionCardTarget struct {
	AgentID   int64
	MsgID     int64
	CreatedAt time.Time
}

func isOpenSessionSubmit(content string) bool {
	_, matched, err := grixactions.ParseOpenSessionSubmit(content)
	return matched && err == nil
}

func resolveGroupOpenSessionSubmitTarget(ctx context.Context, sessionID string, quotedMessageID int64) int64 {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return 0
	}
	if ctx == nil {
		ctx = context.Background()
	}

	agents, err := loadDirectSessionAgents(sessionID)
	if err != nil || len(agents) == 0 {
		return 0
	}
	agentIDs := make([]int64, 0, len(agents))
	for _, row := range agents {
		if row.ID > 0 {
			agentIDs = append(agentIDs, row.ID)
		}
	}
	if len(agentIDs) == 0 {
		return 0
	}

	if target := resolveQuotedOpenSessionCardTarget(sessionID, quotedMessageID, agentIDs); target.AgentID > 0 {
		return target.AgentID
	}
	if target := resolveIndexedOpenSessionCardTarget(ctx, sessionID, agentIDs); target.AgentID > 0 {
		return target.AgentID
	}
	return 0
}

func resolveQuotedOpenSessionCardTarget(sessionID string, quotedMessageID int64, agentIDs []int64) openSessionCardTarget {
	if quotedMessageID <= 0 {
		return openSessionCardTarget{}
	}
	allowedAgentIDs := make(map[int64]struct{}, len(agentIDs))
	for _, agentID := range agentIDs {
		if agentID > 0 {
			allowedAgentIDs[agentID] = struct{}{}
		}
	}
	target, ok := loadValidOpenSessionCardTarget(sessionID, 0, quotedMessageID)
	if !ok {
		return openSessionCardTarget{}
	}
	if _, allowed := allowedAgentIDs[target.AgentID]; !allowed {
		return openSessionCardTarget{}
	}
	return target
}

func resolveIndexedOpenSessionCardTarget(ctx context.Context, sessionID string, agentIDs []int64) openSessionCardTarget {
	var selected openSessionCardTarget
	for _, agentID := range agentIDs {
		msgID := wsagentapi.LoadBindingCardMsgID(ctx, agentID, sessionID)
		if msgID <= 0 {
			continue
		}
		target, ok := loadValidOpenSessionCardTarget(sessionID, agentID, msgID)
		if !ok {
			continue
		}
		if selected.AgentID == 0 || target.CreatedAt.After(selected.CreatedAt) ||
			(target.CreatedAt.Equal(selected.CreatedAt) && target.MsgID > selected.MsgID) {
			selected = target
		}
	}
	return selected
}

func loadValidOpenSessionCardTarget(sessionID string, agentID int64, msgID int64) (openSessionCardTarget, bool) {
	query := store.DB.
		Select("msg_id", "sender_id", "created_at").
		Where("session_id = ? AND msg_id = ? AND sender_type = 2", sessionID, msgID).
		Where("is_deleted = ? AND is_revoked = ?", false, false).
		Where("content LIKE ?", "%grix://card/agent_open_session%")
	if agentID > 0 {
		query = query.Where("sender_id = ?", agentID)
	}
	var msg model.Message
	if err := query.First(&msg).Error; err != nil {
		return openSessionCardTarget{}, false
	}
	return openSessionCardTarget{AgentID: msg.SenderID, MsgID: msg.MsgID, CreatedAt: msg.CreatedAt}, true
}
