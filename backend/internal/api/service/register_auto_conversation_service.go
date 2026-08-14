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
	"gorm.io/gorm/clause"
)

const (
	registerWelcomeMessageSummaryMaxRunes = 60
	registerWelcomeCompensateMaxAttempts  = 5
)

const (
	registerWelcomeMessageContentZH = "你好，我是 Grix 官方客服，\n有什么需要我帮您的？"
	registerWelcomeMessageContentEN = "Hello, I'm the official Grix support agent.\nHow can I help you?"
)

var registerWelcomeMessageContents = []string{
	registerWelcomeMessageContentZH,
	registerWelcomeMessageContentEN,
}

type configuredCustomerConversationResult struct {
	sessionID string
}

func compensateRegisterWelcome(registerUserID, customerUserID int64) error {
	if customerUserID == 0 {
		return nil
	}
	if registerUserID <= 0 || customerUserID <= 0 || registerUserID == customerUserID {
		return errors.New("系统客户账户ID配置无效")
	}
	if store.DB == nil {
		return errors.New("系统繁忙，请稍后重试")
	}

	var result *configuredCustomerConversationResult
	err := store.DB.Transaction(func(tx *gorm.DB) error {
		if err := addConfiguredCustomerFriendTx(tx, registerUserID, customerUserID); err != nil {
			return err
		}

		now := time.Now().UTC()
		sessionID, err := ensureCustomerPrivateSessionTx(tx, customerUserID, registerUserID, now)
		if err != nil {
			return err
		}
		if strings.TrimSpace(sessionID) == "" {
			return errors.New("会话创建失败")
		}

		msgID, err := ensureRegisterWelcomeMessageTx(tx, sessionID, registerUserID, customerUserID, now)
		if err != nil {
			return err
		}
		if msgID <= 0 {
			result = &configuredCustomerConversationResult{
				sessionID: sessionID,
			}
			return nil
		}

		if err := ensureRegisterWelcomeInboxTx(tx, sessionID, msgID, registerUserID, customerUserID, now); err != nil {
			return err
		}
		result = &configuredCustomerConversationResult{
			sessionID: sessionID,
		}
		return nil
	})
	if err != nil {
		return err
	}

	if result != nil && result.sessionID != "" {
		ensureAutoDelegateForPrivateSession(result.sessionID, registerUserID, customerUserID, 1)
	}
	return nil
}

func ensureRegisterWelcomeMessageTx(tx *gorm.DB, sessionID string, registerUserID, customerUserID int64, now time.Time) (int64, error) {
	content, err := registerWelcomeMessageContentForUser(tx, registerUserID)
	if err != nil {
		return 0, err
	}

	var existing model.Message
	if err := tx.Select("msg_id").
		Where(
			"session_id = ? AND sender_id = ? AND sender_type = ? AND msg_type = ? AND content IN ?",
			sessionID,
			customerUserID,
			1,
			1,
			registerWelcomeMessageContents,
		).
		Order("created_at ASC, msg_id ASC").
		First(&existing).Error; err == nil {
		return 0, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}

	msgID := snowflake.GenID()
	summary := textutil.TruncateRunes(content, registerWelcomeMessageSummaryMaxRunes)
	if err := tx.Create(&model.Message{
		MsgID:      msgID,
		SessionID:  sessionID,
		SenderID:   customerUserID,
		SenderType: 1,
		MsgType:    1,
		Content:    content,
		CreatedAt:  now,
	}).Error; err != nil {
		return 0, err
	}

	if err := tx.Model(&model.Session{}).
		Where("session_id = ?", sessionID).
		Updates(map[string]any{
			"last_msg_id":      msgID,
			"last_msg_summary": summary,
			"updated_at":       now,
		}).Error; err != nil {
		return 0, err
	}
	return msgID, nil
}

func registerWelcomeMessageContentForUser(tx *gorm.DB, userID int64) (string, error) {
	language, err := loadUserPreferredLanguageWithDB(tx, userID)
	if err != nil {
		return "", err
	}
	if language == preferredLanguageEN {
		return registerWelcomeMessageContentEN, nil
	}
	return registerWelcomeMessageContentZH, nil
}

func ensureRegisterWelcomeInboxTx(
	tx *gorm.DB,
	sessionID string,
	msgID int64,
	registerUserID, customerUserID int64,
	now time.Time,
) error {
	if tx == nil {
		return errors.New("系统繁忙，请稍后重试")
	}

	userIDs := []int64{customerUserID, registerUserID}
	for _, userID := range userIDs {
		var existing int64
		if err := tx.Model(&model.UserInbox{}).
			Where("user_id = ? AND session_id = ? AND msg_id = ?", userID, sessionID, msgID).
			Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			continue
		}

		nextSeq, err := inboxseq.NextTx(context.Background(), tx, userID)
		if err != nil {
			return err
		}
		if err := tx.Create(&model.UserInbox{
			UserID:    userID,
			InboxSeq:  nextSeq,
			MsgID:     msgID,
			SessionID: sessionID,
			EventKind: model.UserInboxEventKindMessage,
		}).Error; err != nil {
			return err
		}
	}

	if err := tx.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, customerUserID).
		Updates(map[string]any{
			"last_active_at":   now,
			"last_read_msg_id": gorm.Expr("CASE WHEN last_read_msg_id < ? THEN ? ELSE last_read_msg_id END", msgID, msgID),
		}).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, registerUserID).
		Updates(map[string]any{
			"last_active_at": now,
			"unread_count":   gorm.Expr("unread_count + 1"),
		}).Error; err != nil {
		return err
	}
	return nil
}

func ensureCustomerPrivateSessionTx(tx *gorm.DB, customerUserID, registerUserID int64, now time.Time) (string, error) {
	directKey := buildDirectKey(customerUserID, registerUserID, 1)

	var existing model.Session
	if err := tx.
		Where("direct_key = ? AND session_type = 1 AND is_deleted = false", directKey).
		Order("updated_at DESC").
		First(&existing).Error; err == nil {
		return existing.SessionID, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}

	sessionID := newSessionID()
	directKeyValue := directKey
	session := model.Session{
		SessionID:   sessionID,
		DirectKey:   &directKeyValue,
		OwnerID:     customerUserID,
		SessionType: 1,
	}
	if err := tx.Create(&session).Error; err != nil {
		return "", err
	}

	members := []model.SessionMember{
		{
			SessionID:    sessionID,
			MemberID:     customerUserID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     registerUserID,
			MemberType:   1,
			Role:         1,
			LastActiveAt: now,
			JoinedAt:     now,
		},
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&members).Error; err != nil {
		return "", err
	}
	return sessionID, nil
}
