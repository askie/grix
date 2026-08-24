package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/inboxseq"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/textutil"
	"github.com/askie/grix/backend/internal/pkg/userpref"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type agentNoticeDelivery struct {
	memberID              int64
	inboxSeq              int64
	shouldIncrementUnread bool
}

// EmitAgentDeliveryFailureMessage writes an agent delivery failure as a normal
// chat message so the conversation, summary, and unread badge share one path.
func EmitAgentDeliveryFailureMessage(
	hub HubInterface,
	ctx context.Context,
	sessionID string,
	ownerID int64,
	agentID int64,
	triggerMsgID int64,
	scope string,
	code string,
	reason string,
) {
	if agentID <= 0 {
		return
	}
	if hub == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	if store.DB == nil || store.RDB == nil {
		logger.L.Warnf("skip agent delivery notice: db/redis unavailable session=%s", sessionID)
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	scope = strings.TrimSpace(scope)
	code = strings.TrimSpace(code)
	content := buildAgentDeliveryFailureMessageContent(code, reason, userpref.Language(ctx, ownerID))

	extraRaw, _ := json.Marshal(map[string]any{
		"type":           "agent_delivery_notice",
		"scope":          scope,
		"code":           code,
		"owner_id":       ownerID,
		"agent_id":       agentID,
		"trigger_msg_id": triggerMsgID,
	})

	msgID := snowflake.GenID()
	now := time.Now().UTC()
	summary := ""
	if !textutil.IsStandaloneCardMessage(content) {
		summary = textutil.TruncateRunes(content, sessionSummaryMaxRunes)
	}

	sessionType := int16(1)
	deliveries := make([]agentNoticeDelivery, 0, 4)

	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		var session model.Session
		if err := tx.Select("session_id", "session_type").
			Where("session_id = ? AND is_deleted = false", sessionID).
			First(&session).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		sessionType = session.SessionType

		if err := tx.Create(&model.Message{
			MsgID:      msgID,
			SessionID:  sessionID,
			SenderID:   agentID,
			SenderType: 2,
			MsgType:    1,
			Content:    content,
			Extra:      datatypes.JSON(extraRaw),
			CreatedAt:  now,
		}).Error; err != nil {
			return err
		}
		sessionUpdates := map[string]any{
			"last_msg_id": msgID,
			"updated_at":  now,
		}
		if summary != "" {
			sessionUpdates["last_msg_summary"] = summary
		}
		if err := tx.Model(&model.Session{}).
			Where("session_id = ?", sessionID).
			Updates(sessionUpdates).Error; err != nil {
			return err
		}

		var members []model.SessionMember
		if err := tx.Select("member_id").
			Where("session_id = ? AND member_type = 1", sessionID).
			Find(&members).Error; err != nil {
			return err
		}
		memberIDs := make([]int64, 0, len(members))
		for _, m := range members {
			memberIDs = append(memberIDs, m.MemberID)
		}
		viewingUsers := ResolveSessionViewingUsers(ctx, sessionID, memberIDs)
		nextSeqByUser, err := inboxseq.AllocateNextBatchTx(ctx, tx, memberIDs)
		if err != nil {
			return fmt.Errorf("allocate notice inbox_seq batch: %w", err)
		}
		for _, m := range members {
			if m.MemberID <= 0 {
				continue
			}

			inboxSeq := nextSeqByUser[m.MemberID]

			if err := tx.Create(&model.UserInbox{
				UserID:    m.MemberID,
				InboxSeq:  inboxSeq,
				MsgID:     msgID,
				SessionID: sessionID,
				EventKind: model.UserInboxEventKindMessage,
			}).Error; err != nil {
				return err
			}
			isViewing := viewingUsers[m.MemberID]
			if isViewing {
				if err := tx.Model(&model.SessionMember{}).
					Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, m.MemberID).
					Updates(map[string]interface{}{
						"last_active_at":   now,
						"unread_count":     0,
						"last_read_msg_id": gorm.Expr("CASE WHEN last_read_msg_id < ? THEN ? ELSE last_read_msg_id END", msgID, msgID),
					}).Error; err != nil {
					return err
				}
			} else {
				if err := tx.Model(&model.SessionMember{}).
					Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, m.MemberID).
					Updates(map[string]interface{}{
						"last_active_at": now,
						"unread_count":   gorm.Expr("unread_count + 1"),
					}).Error; err != nil {
					return err
				}
			}

			deliveries = append(deliveries, agentNoticeDelivery{
				memberID:              m.MemberID,
				inboxSeq:              inboxSeq,
				shouldIncrementUnread: !isViewing,
			})
		}
		return nil
	}); err != nil {
		logger.L.Warnf("emit agent delivery notice failed session=%s owner=%d agent=%d err=%v", sessionID, ownerID, agentID, err)
		return
	}

	if len(deliveries) == 0 {
		return
	}

	pushExtra := json.RawMessage(extraRaw)
	for _, d := range deliveries {
		if d.shouldIncrementUnread {
			if err := store.RDB.HIncrBy(ctx, fmt.Sprintf("im:unread:%d", d.memberID), sessionID, 1).Err(); err != nil {
				logger.L.Warnf("redis unread increment for notice failed user=%d session=%s: %v", d.memberID, sessionID, err)
			}
		} else {
			if err := store.RDB.HDel(ctx, fmt.Sprintf("im:unread:%d", d.memberID), sessionID).Err(); err != nil {
				logger.L.Warnf("redis unread clear for notice failed user=%d session=%s: %v", d.memberID, sessionID, err)
			}
		}
		broadcastPushMsgToUser(hub, ctx, d.memberID, protocol.PushMsgPayload{
			InboxSeq:    d.inboxSeq,
			MsgID:       msgID,
			SessionID:   sessionID,
			SessionType: sessionType,
			SenderID:    agentID,
			SenderType:  2,
			MsgType:     1,
			Content:     content,
			Extra:       pushExtra,
			CreatedAt:   now.UnixMilli(),
		})
	}
}

func buildAgentDeliveryFailureMessageContent(code string, reason string, language string) string {
	copy := agentDeliveryFailureCopyFor(language)
	switch strings.TrimSpace(code) {
	case protocol.AgentDeliveryCodeAckTimeout:
		return copy.ackTimeout
	}
	if strings.TrimSpace(reason) == "queue full" {
		return copy.queueFull
	}
	return copy.unavailable
}
