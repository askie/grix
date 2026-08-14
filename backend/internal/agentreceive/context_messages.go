package agentreceive

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/mention"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func BuildCurrentContextMessage(trigger MessageTrigger) protocol.ContextMessagePayload {
	createdAt := trigger.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	mentions := append([]int64(nil), trigger.MentionUserIDs...)
	if len(mentions) == 0 {
		mentions = mention.ParseUserIDs(trigger.ExtraRaw, trigger.Content)
	}
	return protocol.ContextMessagePayload{
		MsgID:           trigger.MsgID,
		SenderID:        trigger.SenderID,
		SenderType:      trigger.SenderType,
		MsgType:         trigger.MsgType,
		Content:         strings.TrimSpace(trigger.Content),
		QuotedMessageID: trigger.QuotedMessageID,
		MentionUserIDs:  protocol.StringInt64s(mentions),
		CreatedAt:       createdAt.UnixMilli(),
	}
}

func LoadContextMessages(sessionID string, msgIDs []int64) ([]protocol.ContextMessagePayload, error) {
	if store.DB == nil || sessionID == "" || len(msgIDs) == 0 {
		return nil, nil
	}

	var rows []model.Message
	if err := store.DB.
		Where("session_id = ? AND msg_id IN ? AND is_deleted = false AND is_revoked = false", sessionID, msgIDs).
		Order("msg_id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	return buildContextMessages(rows), nil
}

func LoadVisibleContextMessages(
	sessionID string,
	viewerUserID int64,
	msgIDs []int64,
) ([]protocol.ContextMessagePayload, error) {
	if store.DB == nil || sessionID == "" || len(msgIDs) == 0 {
		return nil, nil
	}

	query := store.DB.
		Where(
			"session_id = ? AND msg_id IN ? AND is_deleted = false AND is_revoked = false",
			sessionID,
			msgIDs,
		)
	if cutoff, ok := loadVisibleHistoryCutoff(sessionID, viewerUserID); ok {
		query = query.Where("created_at > ?", cutoff)
	}
	// Filter out messages restricted by visible_to that the viewer cannot see.
	if store.IsPostgres() && viewerUserID > 0 {
		query = query.Where(
			"visible_to IS NULL OR sender_id = ? OR visible_to @> to_jsonb(?::bigint)",
			viewerUserID, viewerUserID,
		)
	}

	var rows []model.Message
	if err := query.Order("msg_id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return buildContextMessages(rows), nil
}

func BufferVisibleMessage(
	ctx context.Context,
	sessionID string,
	memberType int16,
	memberID int64,
	msgID int64,
) error {
	return appendBufferedMsgID(ctx, sessionID, memberType, memberID, msgID)
}

func LoadBufferedVisibleContextMessages(
	ctx context.Context,
	sessionID string,
	memberType int16,
	memberID int64,
	viewerUserID int64,
	limit int,
) ([]protocol.ContextMessagePayload, error) {
	ids, err := peekBufferedMsgIDs(ctx, sessionID, memberType, memberID)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	normalizedLimit := normalizeContextWindowLimit(limit)
	if len(ids) > normalizedLimit {
		ids = ids[len(ids)-normalizedLimit:]
	}
	return LoadVisibleContextMessages(sessionID, viewerUserID, ids)
}

func HasVisibleBuffer(
	ctx context.Context,
	sessionID string,
	memberType int16,
	memberID int64,
) (bool, error) {
	ids, err := peekBufferedMsgIDs(ctx, sessionID, memberType, memberID)
	if err != nil {
		return false, err
	}
	return len(ids) > 0, nil
}

func normalizeContextWindowLimit(limit int) int {
	if limit <= 0 {
		return DefaultBacklogCount
	}
	if limit > MaxBacklogCount {
		return MaxBacklogCount
	}
	return limit
}

func loadVisibleHistoryCutoff(sessionID string, viewerUserID int64) (time.Time, bool) {
	if store.DB == nil || sessionID == "" || viewerUserID <= 0 {
		return time.Time{}, false
	}

	var member model.SessionMember
	memberResult := store.DB.Select("session_id").
		Where(
			"session_id = ? AND member_id = ? AND member_type = 1",
			sessionID,
			viewerUserID,
		).
		Limit(1).
		Find(&member)
	if memberResult.Error != nil || memberResult.RowsAffected == 0 {
		return time.Time{}, false
	}

	var reset model.SessionHistoryReset
	resetResult := store.DB.Select("deleted_before").
		Where("session_id = ? AND user_id = ?", sessionID, viewerUserID).
		Limit(1).
		Find(&reset)
	if resetResult.Error != nil || resetResult.RowsAffected == 0 || reset.DeletedBefore.IsZero() {
		return time.Time{}, false
	}
	return reset.DeletedBefore.UTC(), true
}

func buildContextMessages(rows []model.Message) []protocol.ContextMessagePayload {
	if len(rows) == 0 {
		return nil
	}

	messages := make([]protocol.ContextMessagePayload, 0, len(rows))
	for _, row := range rows {
		content := strings.TrimSpace(row.Content)
		if content == "" {
			continue
		}
		messages = append(messages, protocol.ContextMessagePayload{
			MsgID:           row.MsgID,
			SenderID:        row.SenderID,
			SenderType:      row.SenderType,
			MsgType:         row.MsgType,
			Content:         content,
			QuotedMessageID: row.QuotedMessageID,
			MentionUserIDs:  protocol.StringInt64s(mention.ParseUserIDs(json.RawMessage(row.Extra), row.Content)),
			CreatedAt:       row.CreatedAt.UTC().UnixMilli(),
		})
	}
	if len(messages) == 0 {
		return nil
	}
	return messages
}

func VisibleBufferKey(sessionID string, memberType int16, memberID int64) string {
	return fmt.Sprintf("im:agent_receive:buffer:%s:%d:%d", sessionID, memberType, memberID)
}
