package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/textutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var (
	ErrMessageContentEmpty = errors.New("message content required")
	ErrMessageEditDenied   = errors.New("message edit denied")
	ErrMessageNotFound     = errors.New("message not found")
)

type MessageEditActor struct {
	UserID  int64
	AgentID int64
}

func (a MessageEditActor) canEdit(msg model.Message) bool {
	switch msg.SenderType {
	case 1:
		return a.UserID > 0 && msg.SenderID == a.UserID
	case 2:
		return a.AgentID > 0 && msg.SenderID == a.AgentID
	default:
		return false
	}
}

func buildMessageEditPayload(
	msg model.Message,
	sessionType int16,
	inboxSeq int64,
) protocol.EditEventPayload {
	return protocol.EditEventPayload{
		InboxSeq:        inboxSeq,
		MsgID:           msg.MsgID,
		SessionID:       msg.SessionID,
		ThreadID:        msg.ThreadID,
		SessionType:     sessionType,
		SenderID:        msg.SenderID,
		SenderType:      msg.SenderType,
		MsgType:         msg.MsgType,
		Content:         msg.Content,
		Extra:           json.RawMessage(msg.Extra),
		QuotedMessageID: msg.QuotedMessageID,
		SyncEvent:       model.UserInboxEventKindEdit,
		CreatedAt:       msg.CreatedAt.UTC().UnixMilli(),
	}
}

func EditMessage(
	ctx context.Context,
	sessionID string,
	msgID int64,
	actor MessageEditActor,
	content string,
	extra ...json.RawMessage,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ensureSessionAccessible(ctx, sessionID); err != nil {
		return err
	}
	if msgID <= 0 {
		return ErrMessageNotFound
	}
	if strings.TrimSpace(content) == "" {
		return ErrMessageContentEmpty
	}

	var session model.Session
	if err := store.DB.Select("session_id", "owner_id", "session_type", "last_msg_id").
		Where("session_id = ?", sessionID).
		First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSessionNotFound
		}
		return err
	}

	var msg model.Message
	if err := store.DB.Where(
		"msg_id = ? AND session_id = ? AND is_deleted = false AND is_revoked = false",
		msgID,
		sessionID,
	).First(&msg).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrMessageNotFound
		}
		return err
	}
	if !actor.canEdit(msg) {
		return ErrMessageEditDenied
	}

	// Determine if we also need to update extra.
	var extraJSON json.RawMessage
	if len(extra) > 0 {
		extraJSON = extra[0]
	}
	contentChanged := msg.Content != content
	extraChanged := extraJSON != nil && string(extraJSON) != string(msg.Extra)
	if !contentChanged && !extraChanged {
		return nil
	}

	var members []model.SessionMember
	var inboxRows []model.UserInbox
	err := store.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("session_id = ?", sessionID).Find(&members).Error; err != nil {
			return err
		}
		// If the message has visible_to set, filter members to only
		// those who can see the message (plus the sender).
		if msg.VisibleTo != nil {
			members = filterMembersByVisibleTo(members, msg.VisibleTo, msg.SenderID)
		}
		updates := map[string]any{"content": content}
		if extraChanged {
			updates["extra"] = string(extraJSON)
		}
		if err := tx.Model(&model.Message{}).
			Where("msg_id = ? AND session_id = ?", msg.MsgID, msg.SessionID).
			Updates(updates).Error; err != nil {
			return err
		}
		if session.LastMsgID != nil && *session.LastMsgID == msg.MsgID {
			if msg.VisibleTo == nil && !textutil.IsStandaloneCardMessage(content) {
				summary := textutil.TruncateRunes(content, 60)
				if err := tx.Model(&model.Session{}).
					Where("session_id = ?", sessionID).
					Update("last_msg_summary", summary).Error; err != nil {
					return err
				}
			}
		}
		rows, err := buildMessageEditInboxRowsTx(ctx, tx, members, sessionID, msg.MsgID)
		if err != nil {
			return err
		}
		if len(rows) > 0 {
			if err := tx.Create(&rows).Error; err != nil {
				return err
			}
		}
		inboxRows = rows
		return nil
	})
	if err != nil {
		return err
	}

	msg.Content = content
	if extraChanged {
		msg.Extra = datatypes.JSON(extraJSON)
	}
	inboxSeqByUserID := make(map[int64]int64, len(inboxRows))
	for _, row := range inboxRows {
		if row.UserID <= 0 || row.InboxSeq <= 0 {
			continue
		}
		inboxSeqByUserID[row.UserID] = row.InboxSeq
	}

	for _, member := range members {
		if member.MemberType == 1 {
			payload := buildMessageEditPayload(msg, session.SessionType, inboxSeqByUserID[member.MemberID])
			pushRealtimeEvent(member.MemberID, protocol.CmdPushEdit, payload)
			continue
		}
		if member.MemberType == 2 {
			payload := buildMessageEditPayload(msg, session.SessionType, 0)
			// agent 共享多连接物理隔离:私聊按 session.OwnerID 精确路由(共享场景下 owner 是被共享者);
			// 群聊投主实例(共享只在私聊生效):owner=0 已被路由层视为非法,
			// 必须显式解析 agent.OwnerID 后按精确路由推送。
			pushOwnerID := int64(0)
			if session.SessionType == 1 {
				pushOwnerID = session.OwnerID
			} else {
				pushOwnerID = resolveAgentPrimaryOwnerID(member.MemberID)
			}
			pushAgentChannelEvent(member.MemberID, pushOwnerID, protocol.CmdEventEdit, payload)
		}
	}

	return nil
}

// filterMembersByVisibleTo restricts members to only those in the visible_to list
// plus the sender. Used by EditMessage to avoid leaking content to unauthorized members.
func filterMembersByVisibleTo(members []model.SessionMember, visibleToJSON datatypes.JSON, senderID int64) []model.SessionMember {
	var visibleIDs []int64
	if err := json.Unmarshal(visibleToJSON, &visibleIDs); err != nil {
		return members
	}
	if len(visibleIDs) == 0 {
		return members
	}
	allowed := make(map[int64]struct{}, len(visibleIDs)+1)
	allowed[senderID] = struct{}{}
	for _, id := range visibleIDs {
		allowed[id] = struct{}{}
	}
	filtered := make([]model.SessionMember, 0, len(members))
	for _, m := range members {
		if _, ok := allowed[m.MemberID]; ok {
			filtered = append(filtered, m)
		}
	}
	return filtered
}
