package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/inboxseq"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/textutil"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

// ReachSystemMessage is the result of writing one reach in-app system message,
// carrying everything the ws sender needs to build the realtime push payload.
type ReachSystemMessage struct {
	SessionID string
	MsgID     int64
	InboxSeq  int64
	Content   string
	CreatedAt time.Time
}

// WriteAppReleaseSystemMessage writes a single in-app (站内) system message from
// the configured customer account to targetUserID announcing an app release, in
// the user↔customer private session. It reuses the register auto-conversation
// primitives so the message lands in the same "官方客服" thread. The message is
// SenderType=3 (system) / MsgType=3 (system notice). The copy comes from the
// task's announcement snapshot, localized per the target user's preferred
// language. The caller is responsible for realtime/offline delivery of the
// returned message.
func WriteAppReleaseSystemMessage(customerUserID, targetUserID int64, announcement ReachAnnouncementContent) (ReachSystemMessage, error) {
	if store.DB == nil {
		return ReachSystemMessage{}, errors.New("db not initialized")
	}
	if customerUserID <= 0 || targetUserID <= 0 || customerUserID == targetUserID {
		return ReachSystemMessage{}, errors.New("invalid customer/target user id")
	}

	now := time.Now().UTC()
	var out ReachSystemMessage
	err := store.DB.Transaction(func(tx *gorm.DB) error {
		language, err := loadUserPreferredLanguageWithDB(tx, targetUserID)
		if err != nil {
			return err
		}
		content := announcement.Locale(language).InAppText()
		if content == "" {
			return errors.New("empty announcement content")
		}

		sessionID, err := ensureCustomerPrivateSessionTx(tx, customerUserID, targetUserID, now)
		if err != nil {
			return err
		}
		if strings.TrimSpace(sessionID) == "" {
			return errors.New("reach session creation failed")
		}

		msgID := snowflake.GenID()
		if err := tx.Create(&model.Message{
			MsgID:      msgID,
			SessionID:  sessionID,
			SenderID:   customerUserID,
			SenderType: 3,
			MsgType:    3,
			Content:    content,
			CreatedAt:  now,
		}).Error; err != nil {
			return err
		}

		summary := textutil.TruncateRunes(content, sessionSummaryMaxRunes)
		if err := tx.Model(&model.Session{}).
			Where("session_id = ?", sessionID).
			Updates(map[string]any{
				"last_msg_id":      msgID,
				"last_msg_summary": summary,
				"updated_at":       now,
			}).Error; err != nil {
			return err
		}

		inboxSeq, err := inboxseq.NextTx(context.Background(), tx, targetUserID)
		if err != nil {
			return err
		}
		if err := tx.Create(&model.UserInbox{
			UserID:    targetUserID,
			InboxSeq:  inboxSeq,
			MsgID:     msgID,
			SessionID: sessionID,
			EventKind: model.UserInboxEventKindMessage,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.SessionMember{}).
			Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, targetUserID).
			Updates(map[string]any{
				"last_active_at": now,
				"unread_count":   gorm.Expr("unread_count + 1"),
			}).Error; err != nil {
			return err
		}

		out = ReachSystemMessage{
			SessionID: sessionID,
			MsgID:     msgID,
			InboxSeq:  inboxSeq,
			Content:   content,
			CreatedAt: now,
		}
		return nil
	})
	if err != nil {
		return ReachSystemMessage{}, err
	}
	return out, nil
}

func WriteMarketingSystemMessage(customerUserID, targetUserID int64, title, body string) (ReachSystemMessage, error) {
	if store.DB == nil {
		return ReachSystemMessage{}, errors.New("db not initialized")
	}
	if customerUserID <= 0 || targetUserID <= 0 || customerUserID == targetUserID {
		return ReachSystemMessage{}, errors.New("invalid customer/target user id")
	}

	content := title
	if body != "" {
		content = title + "\n" + body
	}

	now := time.Now().UTC()
	var out ReachSystemMessage
	err := store.DB.Transaction(func(tx *gorm.DB) error {
		sessionID, err := ensureCustomerPrivateSessionTx(tx, customerUserID, targetUserID, now)
		if err != nil {
			return err
		}
		if strings.TrimSpace(sessionID) == "" {
			return errors.New("reach session creation failed")
		}

		msgID := snowflake.GenID()
		if err := tx.Create(&model.Message{
			MsgID:      msgID,
			SessionID:  sessionID,
			SenderID:   customerUserID,
			SenderType: 3,
			MsgType:    3,
			Content:    content,
			CreatedAt:  now,
		}).Error; err != nil {
			return err
		}

		summary := textutil.TruncateRunes(content, sessionSummaryMaxRunes)
		if err := tx.Model(&model.Session{}).
			Where("session_id = ?", sessionID).
			Updates(map[string]any{
				"last_msg_id":      msgID,
				"last_msg_summary": summary,
				"updated_at":       now,
			}).Error; err != nil {
			return err
		}

		inboxSeq, err := inboxseq.NextTx(context.Background(), tx, targetUserID)
		if err != nil {
			return err
		}
		if err := tx.Create(&model.UserInbox{
			UserID:    targetUserID,
			InboxSeq:  inboxSeq,
			MsgID:     msgID,
			SessionID: sessionID,
			EventKind: model.UserInboxEventKindMessage,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.SessionMember{}).
			Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, targetUserID).
			Updates(map[string]any{
				"last_active_at": now,
				"unread_count":   gorm.Expr("unread_count + 1"),
			}).Error; err != nil {
			return err
		}

		out = ReachSystemMessage{
			SessionID: sessionID,
			MsgID:     msgID,
			InboxSeq:  inboxSeq,
			Content:   content,
			CreatedAt: now,
		}
		return nil
	})
	if err != nil {
		return ReachSystemMessage{}, err
	}
	return out, nil
}

const sessionSummaryMaxRunes = 60
