package service

import (
	"strings"
	"time"

	apiservice "github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func BanGroup(adminID int64, sessionID, reason, clientIP, userAgent string) error {
	now := time.Now().UTC()
	var banned bool
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		banned, err = banGroupTx(
			tx,
			adminID,
			sessionID,
			reason,
			now,
			clientIP,
			userAgent,
		)
		return err
	}); err != nil {
		return err
	}
	if !banned {
		return nil
	}
	return notifyGroupAccessRevoked(sessionID)
}

func UnbanGroup(adminID int64, sessionID, clientIP, userAgent string) error {
	now := time.Now().UTC()
	return store.DB.Transaction(func(tx *gorm.DB) error {
		normalizedSessionID := strings.TrimSpace(sessionID)
		if normalizedSessionID == "" {
			return gorm.ErrRecordNotFound
		}

		var session model.Session
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("session_id = ?", normalizedSessionID).
			First(&session).Error; err != nil {
			return err
		}

		if err := tx.Model(&model.Session{}).
			Where("session_id = ?", normalizedSessionID).
			Updates(map[string]any{
				"moderation_status": model.SessionModerationStatusActive,
				"banned_reason":     "",
				"banned_at":         nil,
				"banned_by":         nil,
				"updated_at":        now,
			}).Error; err != nil {
			return err
		}

		return recordOperationTx(tx, adminID, "group_unban", "session", normalizedSessionID, map[string]any{}, clientIP, userAgent)
	})
}

func banGroupTx(
	tx *gorm.DB,
	adminID int64,
	sessionID, reason string,
	now time.Time,
	clientIP, userAgent string,
) (bool, error) {
	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return false, gorm.ErrRecordNotFound
	}
	normalizedReason := strings.TrimSpace(reason)
	if normalizedReason == "" {
		normalizedReason = "admin_disabled"
	}

	var session model.Session
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("session_id = ?", normalizedSessionID).
		First(&session).Error; err != nil {
		return false, err
	}
	if session.ModerationStatus == model.SessionModerationStatusBanned {
		return false, nil
	}

	if err := tx.Model(&model.Session{}).
		Where("session_id = ?", normalizedSessionID).
		Updates(map[string]any{
			"moderation_status": model.SessionModerationStatusBanned,
			"banned_reason":     normalizedReason,
			"banned_at":         now,
			"banned_by":         adminID,
			"updated_at":        now,
		}).Error; err != nil {
		return false, err
	}

	if err := recordOperationTx(tx, adminID, "group_ban", "session", normalizedSessionID, map[string]any{
		"reason": normalizedReason,
	}, clientIP, userAgent); err != nil {
		return false, err
	}

	return true, nil
}

func notifyGroupAccessRevoked(sessionID string) error {
	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return nil
	}

	userIDs, err := listGroupHumanMemberIDs(normalizedSessionID)
	if err != nil {
		return err
	}
	apiservice.NotifySessionAccessRevoked(
		normalizedSessionID,
		userIDs,
		apiservice.SessionAccessRevokedReasonGroupBanned,
		"该群已被封禁",
	)
	return nil
}

func listGroupHumanMemberIDs(sessionID string) ([]int64, error) {
	var members []model.SessionMember
	if err := store.DB.Select("member_id").
		Where("session_id = ? AND member_type = 1", strings.TrimSpace(sessionID)).
		Find(&members).Error; err != nil {
		return nil, err
	}

	userIDs := make([]int64, 0, len(members))
	for _, member := range members {
		if member.MemberID <= 0 {
			continue
		}
		userIDs = append(userIDs, member.MemberID)
	}
	return userIDs, nil
}
