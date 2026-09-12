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
	// ErrMessageEditNotAllowed is returned for messages that must not be edited
	// as plain text: card messages (approval/question/binding/... rendered via
	// a standalone grix://card link) and any non-text msg_type. Server-internal
	// callers that rewrite their own status card in place set
	// MessageEditActor.AllowCardMessage to bypass this check.
	ErrMessageEditNotAllowed = errors.New("message edit not allowed for this message type")
)

type MessageEditActor struct {
	UserID  int64
	AgentID int64
	// AllowCardMessage bypasses the card/non-text message restriction. Only
	// set by trusted server-internal callers; never derived from a
	// wire-decoded edit_msg packet (see agentapi.EditMsgPayload.AllowCardMessage).
	AllowCardMessage bool
}

// memberID and memberType return the actor's identity in session_members
// terms (member_type 1=human, 2=agent), matching canEdit's own classification.
func (a MessageEditActor) memberID() int64 {
	if a.AgentID > 0 {
		return a.AgentID
	}
	return a.UserID
}

func (a MessageEditActor) memberType() int16 {
	if a.AgentID > 0 {
		return 2
	}
	return 1
}

// EditMentionDispatchContext carries what a caller needs to hand off to
// ws/handler.DispatchMessageEditMentionAdditions after a successful edit —
// the service layer cannot call that dispatch pipeline directly (it would
// import back into ws/handler, which already imports this package). Nil
// means the edit was a no-op (content and extra both unchanged), so there is
// nothing to diff.
type EditMentionDispatchContext struct {
	EditorMemberID   int64
	EditorMemberType int16
	QuotedMessageID  int64
	MsgType          int16
	OldContent       string
	OldExtra         json.RawMessage
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
) (*EditMentionDispatchContext, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ensureSessionAccessible(ctx, sessionID); err != nil {
		return nil, err
	}
	if msgID <= 0 {
		return nil, ErrMessageNotFound
	}
	if strings.TrimSpace(content) == "" {
		return nil, ErrMessageContentEmpty
	}

	var session model.Session
	if err := store.DB.Select("session_id", "owner_id", "session_type", "last_msg_id").
		Where("session_id = ?", sessionID).
		First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}

	var msg model.Message
	if err := store.DB.Where(
		"msg_id = ? AND session_id = ? AND is_deleted = false AND is_revoked = false",
		msgID,
		sessionID,
	).First(&msg).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMessageNotFound
		}
		return nil, err
	}
	if !actor.canEdit(msg) {
		return nil, ErrMessageEditDenied
	}
	if !actor.AllowCardMessage {
		if msg.MsgType != model.MsgTypeText || textutil.IsStandaloneCardMessage(msg.Content) {
			return nil, ErrMessageEditNotAllowed
		}
	}

	// Determine if we also need to update extra.
	var extraJSON json.RawMessage
	if len(extra) > 0 {
		extraJSON = extra[0]
	}
	contentChanged := msg.Content != content
	extraChanged := extraJSON != nil && string(extraJSON) != string(msg.Extra)
	if !contentChanged && !extraChanged {
		return nil, nil
	}
	oldContent := msg.Content
	oldExtra := json.RawMessage(msg.Extra)

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
		return nil, err
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

	return &EditMentionDispatchContext{
		EditorMemberID:   actor.memberID(),
		EditorMemberType: actor.memberType(),
		QuotedMessageID:  msg.QuotedMessageID,
		MsgType:          msg.MsgType,
		OldContent:       oldContent,
		OldExtra:         oldExtra,
	}, nil
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
