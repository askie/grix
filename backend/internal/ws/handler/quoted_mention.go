package handler

import (
	"encoding/json"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/mention"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/datatypes"
)

// quotedPlaceholderByMsgType yields a readable placeholder for a quoted message
// whose textual content is empty (image / call segment / other non-text types),
// so agents still see that a non-text message was quoted instead of nothing.
func quotedPlaceholderByMsgType(msgType int16) string {
	switch msgType {
	case model.MsgTypeImage:
		return "[图片]"
	case model.MsgTypeCallSegment:
		return "[语音]"
	case model.MsgTypeSystem:
		return "[系统消息]"
	default:
		return "[消息]"
	}
}

// ResolveQuotedMessageOwnerID returns the sender of the quoted message when the
// quote target exists in the same session.
func ResolveQuotedMessageOwnerID(sessionID string, quotedMessageID int64) int64 {
	if sessionID == "" || quotedMessageID <= 0 {
		return 0
	}

	var row struct {
		SenderID int64 `gorm:"column:sender_id"`
	}
	if err := store.DB.Model(&model.Message{}).
		Select("sender_id").
		Where("session_id = ? AND msg_id = ?", sessionID, quotedMessageID).
		Take(&row).Error; err != nil {
		return 0
	}
	if row.SenderID <= 0 {
		return 0
	}
	return row.SenderID
}

// ResolveQuotedMessageOwnerAndType returns the sender_id and sender_type of the
// quoted message. Returns (0, 0) if not found.
func ResolveQuotedMessageOwnerAndType(sessionID string, quotedMessageID int64) (senderID int64, senderType int16) {
	if sessionID == "" || quotedMessageID <= 0 {
		return 0, 0
	}

	var row struct {
		SenderID   int64 `gorm:"column:sender_id"`
		SenderType int16 `gorm:"column:sender_type"`
	}
	if err := store.DB.Model(&model.Message{}).
		Select("sender_id, sender_type").
		Where("session_id = ? AND msg_id = ?", sessionID, quotedMessageID).
		Take(&row).Error; err != nil {
		return 0, 0
	}
	if row.SenderID <= 0 {
		return 0, 0
	}
	return row.SenderID, row.SenderType
}

// ResolveQuotedMessageOwnerAndVisibility returns the sender_id and visible_to
// of the quoted message. Returns (0, nil) if not found.
func ResolveQuotedMessageOwnerAndVisibility(sessionID string, quotedMessageID int64) (senderID int64, visibleTo []int64) {
	if sessionID == "" || quotedMessageID <= 0 {
		return 0, nil
	}

	var row struct {
		SenderID  int64          `gorm:"column:sender_id"`
		VisibleTo datatypes.JSON `gorm:"column:visible_to"`
	}
	if err := store.DB.Model(&model.Message{}).
		Select("sender_id, visible_to").
		Where("session_id = ? AND msg_id = ?", sessionID, quotedMessageID).
		Take(&row).Error; err != nil {
		return 0, nil
	}
	if row.VisibleTo != nil {
		_ = json.Unmarshal(row.VisibleTo, &visibleTo)
	}
	return row.SenderID, visibleTo
}

// ResolveQuotedMessageContent returns the content of the quoted message.
// Returns empty string if not found.
func ResolveQuotedMessageContent(sessionID string, quotedMessageID int64) string {
	if sessionID == "" || quotedMessageID <= 0 {
		return ""
	}
	var row struct {
		Content string `gorm:"column:content"`
	}
	if err := store.DB.Model(&model.Message{}).
		Select("content").
		Where("session_id = ? AND msg_id = ?", sessionID, quotedMessageID).
		Take(&row).Error; err != nil {
		return ""
	}
	return row.Content
}

// ResolveQuotedContextMessage loads the quoted message as a context entry and
// prefixes the content so agents can tell it is the quoted message rather than
// the current trigger message.
func ResolveQuotedContextMessage(
	sessionID string,
	quotedMessageID int64,
) *protocol.ContextMessagePayload {
	if sessionID == "" || quotedMessageID <= 0 {
		return nil
	}

	var row model.Message
	if err := store.DB.
		Where("session_id = ? AND msg_id = ? AND is_deleted = false AND is_revoked = false", sessionID, quotedMessageID).
		Take(&row).Error; err != nil {
		return nil
	}
	if row.MsgID <= 0 {
		return nil
	}

	body := strings.TrimSpace(row.Content)
	if body == "" {
		body = quotedPlaceholderByMsgType(row.MsgType)
	}

	return &protocol.ContextMessagePayload{
		MsgID:           row.MsgID,
		SenderID:        row.SenderID,
		SenderType:      row.SenderType,
		MsgType:         row.MsgType,
		Content:         "[引用消息]\n" + body,
		QuotedMessageID: row.QuotedMessageID,
		MentionUserIDs:  protocol.StringInt64s(mention.ParseUserIDs(json.RawMessage(row.Extra), row.Content)),
		CreatedAt:       row.CreatedAt.UTC().UnixMilli(),
	}
}

func prependQuotedContextMessage(
	contextMessages []protocol.ContextMessagePayload,
	quotedContext *protocol.ContextMessagePayload,
) []protocol.ContextMessagePayload {
	if quotedContext == nil {
		return contextMessages
	}

	merged := make([]protocol.ContextMessagePayload, 0, len(contextMessages)+1)
	merged = append(merged, *quotedContext)
	for _, item := range contextMessages {
		if item.MsgID == quotedContext.MsgID {
			continue
		}
		merged = append(merged, item)
	}
	return merged
}
