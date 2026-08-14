package service

import (
	"context"
	"errors"
	"time"

	"github.com/askie/grix/backend/internal/agentreceive"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func SessionAddMembers(userID int64, sessionID string, memberIDs []int64, memberTypes []int16) (*SessionAddMembersResp, error) {
	memberIDs, memberTypes, err := normalizeGroupMembers(userID, memberIDs, memberTypes)
	if err != nil {
		return nil, err
	}

	var operator model.SessionMember
	if err := store.DB.Select("role").
		Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, userID).
		First(&operator).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionPermissionDenied
		}
		return nil, err
	}
	if operator.Role != 1 && operator.Role != 2 && operator.Role != 3 {
		return nil, ErrSessionPermissionDenied
	}

	var session model.Session
	if err := store.DB.Where("session_id = ? AND is_deleted = false", sessionID).
		First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	if err := ensureLoadedSessionAccessible(session); err != nil {
		return nil, err
	}
	if session.SessionType != 2 {
		return nil, ErrSessionInvalidType
	}
	if err := validateSessionMemberInvitePermission(operator.Role, session, sessionID); err != nil {
		return nil, err
	}
	if err := validateGroupMemberTargets(userID, memberIDs, memberTypes); err != nil {
		return nil, err
	}

	now := time.Now()
	addedCount := 0
	addedMemberIDsForAutoDelegate := make([]int64, 0, len(memberIDs))
	addedMemberTypesForAutoDelegate := make([]int16, 0, len(memberIDs))
	addedHumanMemberIDsForPush := make([]int64, 0, len(memberIDs))

	if len(memberIDs) > 0 {
		var existing []model.SessionMember
		if err := store.DB.Select("member_id", "member_type").
			Where(
				"session_id = ? AND member_id IN ? AND member_type IN ?",
				sessionID,
				memberIDs,
				uniqueMemberTypes(memberTypes),
			).
			Find(&existing).Error; err != nil {
			return nil, err
		}
		existingMemberIDs := make(map[memberIdentity]struct{}, len(existing))
		for _, m := range existing {
			existingMemberIDs[memberIdentity{
				MemberID:   m.MemberID,
				MemberType: m.MemberType,
			}] = struct{}{}
		}

		toInsert := make([]model.SessionMember, 0, len(memberIDs))
		for i, mid := range memberIDs {
			key := memberIdentity{
				MemberID:   mid,
				MemberType: memberTypes[i],
			}
			if _, exists := existingMemberIDs[key]; exists {
				continue
			}
			toInsert = append(toInsert, model.SessionMember{
				SessionID:    sessionID,
				MemberID:     mid,
				MemberType:   memberTypes[i],
				Role:         1,
				JoinedAt:     now,
				LastActiveAt: now,
			})
			addedMemberIDsForAutoDelegate = append(addedMemberIDsForAutoDelegate, mid)
			addedMemberTypesForAutoDelegate = append(addedMemberTypesForAutoDelegate, memberTypes[i])
			if memberTypes[i] == 1 {
				addedHumanMemberIDsForPush = append(addedHumanMemberIDsForPush, mid)
			}
		}
		if err := validateHumanTargetsAllowGroupInvite(
			collectHumanMemberIDsFromSessionMembers(toInsert),
		); err != nil {
			return nil, err
		}

		if len(toInsert) > 0 {
			if err := store.DB.Transaction(func(tx *gorm.DB) error {
				result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&toInsert)
				if result.Error != nil {
					return result.Error
				}
				addedCount = int(result.RowsAffected)
				return tx.Model(&model.Session{}).
					Where("session_id = ?", sessionID).
					Update("updated_at", now).Error
			}); err != nil {
				return nil, err
			}
		}
	}
	if len(addedMemberIDsForAutoDelegate) > 0 {
		ensureAutoDelegateForGroupSessionMembers(
			sessionID,
			userID,
			addedMemberIDsForAutoDelegate,
			addedMemberTypesForAutoDelegate,
		)
	}

	var count int64
	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ?", sessionID).
		Count(&count).Error; err != nil {
		return nil, err
	}
	if addedCount > 0 {
		humanMemberIDs, err := listSessionHumanMemberIDs(sessionID)
		if err != nil {
			return nil, err
		}
		notifySessionMemberChanged(
			sessionID,
			"add",
			userID,
			humanMemberIDs,
			sessionMemberChangedNotifyMeta{},
		)
		sessionMemberAddedOfflinePushRunner(
			sessionID,
			userID,
			session.GroupName,
			addedHumanMemberIDsForPush,
		)
	}

	return &SessionAddMembersResp{
		SessionID:   sessionID,
		AddedCount:  addedCount,
		MemberCount: int(count),
	}, nil
}

func SessionRemoveMembers(userID int64, sessionID string, memberIDs []int64, memberTypes []int16) (*SessionRemoveMembersResp, error) {
	memberIDs, memberTypes, err := normalizeMemberTargets(memberIDs, memberTypes)
	if err != nil {
		return nil, err
	}

	var operator model.SessionMember
	if err := store.DB.Select("role").
		Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, userID).
		First(&operator).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionPermissionDenied
		}
		return nil, err
	}

	var session model.Session
	if err := store.DB.Where("session_id = ? AND is_deleted = false", sessionID).
		First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	if err := ensureLoadedSessionAccessible(session); err != nil {
		return nil, err
	}
	if session.SessionType != 2 {
		return nil, ErrSessionInvalidType
	}

	toRemove := make([]memberIdentity, 0, len(memberIDs))
	for i, memberID := range memberIDs {
		toRemove = append(toRemove, memberIdentity{
			MemberID:   memberID,
			MemberType: memberTypes[i],
		})
	}
	if len(toRemove) == 0 {
		var count int64
		if err := store.DB.Model(&model.SessionMember{}).
			Where("session_id = ?", sessionID).
			Count(&count).Error; err != nil {
			return nil, err
		}
		return &SessionRemoveMembersResp{
			SessionID:    sessionID,
			RemovedCount: 0,
			MemberCount:  int(count),
		}, nil
	}

	var existing []model.SessionMember
	if err := store.DB.
		Where("session_id = ? AND member_id IN ? AND member_type IN ?", sessionID, memberIDs, uniqueMemberTypes(memberTypes)).
		Find(&existing).Error; err != nil {
		return nil, err
	}

	existingMap := make(map[memberIdentity]model.SessionMember, len(existing))
	for _, m := range existing {
		existingMap[memberIdentity{
			MemberID:   m.MemberID,
			MemberType: m.MemberType,
		}] = m
	}

	removedHumanMemberIDs := make([]int64, 0, len(toRemove))
	for _, id := range toRemove {
		target, ok := existingMap[id]
		if !ok {
			continue
		}
		if target.MemberType == 1 && target.MemberID == userID {
			return nil, ErrSessionCannotOperateSelf
		}
		if target.Role == 3 {
			return nil, ErrSessionCannotRemoveOwner
		}

		if operator.Role == 1 {
			if target.MemberType != 2 {
				return nil, ErrSessionRemoveDenied
			}
			var agent model.Agent
			if err := store.DB.Select("owner_id").Where("id = ?", target.MemberID).First(&agent).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, ErrSessionRemoveDenied
				}
				return nil, err
			}
			if agent.OwnerID != userID {
				return nil, ErrSessionRemoveDenied
			}
		} else if operator.Role == 2 && target.Role != 1 {
			return nil, ErrSessionRemoveDenied
		}

		if target.MemberType == 1 {
			removedHumanMemberIDs = append(removedHumanMemberIDs, target.MemberID)
		}
	}

	removedCount := 0
	now := time.Now()
	if len(existing) > 0 {
		if err := store.DB.Transaction(func(tx *gorm.DB) error {
			for _, id := range toRemove {
				result := tx.Where(
					"session_id = ? AND member_id = ? AND member_type = ?",
					sessionID,
					id.MemberID,
					id.MemberType,
				).Delete(&model.SessionMember{})
				if result.Error != nil {
					return result.Error
				}
				removedCount += int(result.RowsAffected)
			}
			if removedCount == 0 {
				return nil
			}
			if err := recordSessionTombstones(tx, sessionID, removedHumanMemberIDs, now); err != nil {
				return err
			}
			return tx.Model(&model.Session{}).
				Where("session_id = ?", sessionID).
				Update("updated_at", now).Error
		}); err != nil {
			return nil, err
		}
	}

	var count int64
	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ?", sessionID).
		Count(&count).Error; err != nil {
		return nil, err
	}
	if removedCount > 0 {
		for _, id := range toRemove {
			_ = agentreceive.ClearBuffer(context.Background(), sessionID, id.MemberType, id.MemberID)
		}
		humanMemberIDs, err := listSessionHumanMemberIDs(sessionID)
		if err != nil {
			return nil, err
		}
		humanMemberIDs = append(humanMemberIDs, removedHumanMemberIDs...)
		notifySessionMemberChanged(
			sessionID,
			"remove",
			userID,
			humanMemberIDs,
			sessionMemberChangedNotifyMeta{
				RemovedUserIDs: removedHumanMemberIDs,
			},
		)
	}

	return &SessionRemoveMembersResp{
		SessionID:    sessionID,
		RemovedCount: removedCount,
		MemberCount:  int(count),
	}, nil
}

func SessionUpdateMemberRole(
	userID int64,
	sessionID string,
	memberID int64,
	memberType int16,
	role int16,
) (*SessionUpdateMemberRoleResp, error) {
	if memberID <= 0 {
		return nil, ErrInvalidMemberID
	}
	if memberType == 0 {
		memberType = 1
	}
	if memberType != 1 && memberType != 2 {
		return nil, ErrInvalidMemberType
	}
	if role != 1 && role != 2 {
		return nil, ErrSessionInvalidRole
	}

	var operator model.SessionMember
	if err := store.DB.Select("role").
		Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, userID).
		First(&operator).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionPermissionDenied
		}
		return nil, err
	}
	if operator.Role != 3 {
		return nil, ErrSessionOwnerRequired
	}

	var session model.Session
	if err := store.DB.Where("session_id = ? AND is_deleted = false", sessionID).
		First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	if err := ensureLoadedSessionAccessible(session); err != nil {
		return nil, err
	}
	if session.SessionType != 2 {
		return nil, ErrSessionInvalidType
	}
	if memberType != 1 {
		return nil, ErrInvalidMemberType
	}
	if memberID == userID {
		return nil, ErrSessionCannotOperateSelf
	}

	var target model.SessionMember
	if err := store.DB.
		Where("session_id = ? AND member_id = ? AND member_type = ?", sessionID, memberID, memberType).
		First(&target).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionMemberNotFound
		}
		return nil, err
	}
	if target.Role == 3 {
		return nil, ErrSessionCannotRemoveOwner
	}

	now := time.Now()
	roleUpdated := false
	if target.Role != role {
		if err := store.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&model.SessionMember{}).
				Where("session_id = ? AND member_id = ? AND member_type = ?", sessionID, memberID, memberType).
				Update("role", role).Error; err != nil {
				return err
			}
			return tx.Model(&model.Session{}).
				Where("session_id = ?", sessionID).
				Update("updated_at", now).Error
		}); err != nil {
			return nil, err
		}
		roleUpdated = true
	}
	if roleUpdated {
		humanMemberIDs, err := listSessionHumanMemberIDs(sessionID)
		if err != nil {
			return nil, err
		}
		notifySessionMemberChanged(
			sessionID,
			"role",
			userID,
			humanMemberIDs,
			sessionMemberChangedNotifyMeta{},
		)
	}

	return &SessionUpdateMemberRoleResp{
		SessionID:  sessionID,
		MemberID:   memberID,
		MemberType: memberType,
		Role:       role,
	}, nil
}

func SessionTransferOwner(userID int64, sessionID string, targetMemberID int64) (*SessionTransferOwnerResp, error) {
	if targetMemberID <= 0 {
		return nil, ErrInvalidMemberID
	}
	if targetMemberID == userID {
		return nil, ErrSessionCannotOperateSelf
	}

	var operator model.SessionMember
	if err := store.DB.Select("role").
		Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, userID).
		First(&operator).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionPermissionDenied
		}
		return nil, err
	}
	if operator.Role != 3 {
		return nil, ErrSessionOwnerRequired
	}

	var session model.Session
	if err := store.DB.Where("session_id = ? AND is_deleted = false", sessionID).
		First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	if err := ensureLoadedSessionAccessible(session); err != nil {
		return nil, err
	}
	if session.SessionType != 2 {
		return nil, ErrSessionInvalidType
	}

	var target model.SessionMember
	if err := store.DB.
		Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, targetMemberID).
		First(&target).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionMemberNotFound
		}
		return nil, err
	}
	if target.MemberID == userID {
		return nil, ErrSessionCannotOperateSelf
	}

	now := time.Now()
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.SessionMember{}).
			Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, userID).
			Update("role", 2).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.SessionMember{}).
			Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, targetMemberID).
			Update("role", 3).Error; err != nil {
			return err
		}
		return tx.Model(&model.Session{}).
			Where("session_id = ?", sessionID).
			Updates(map[string]any{
				"owner_id":   targetMemberID,
				"updated_at": now,
			}).Error
	}); err != nil {
		return nil, err
	}
	humanMemberIDs, err := listSessionHumanMemberIDs(sessionID)
	if err != nil {
		return nil, err
	}
	notifySessionMemberChanged(
		sessionID,
		"transfer_owner",
		userID,
		humanMemberIDs,
		sessionMemberChangedNotifyMeta{},
	)

	return &SessionTransferOwnerResp{
		SessionID: sessionID,
		OwnerID:   targetMemberID,
	}, nil
}

func SessionDissolve(userID int64, sessionID string) (*SessionDissolveResp, error) {
	var operator model.SessionMember
	if err := store.DB.Select("role").
		Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, userID).
		First(&operator).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionPermissionDenied
		}
		return nil, err
	}
	if operator.Role != 3 {
		return nil, ErrSessionDissolveDenied
	}

	var session model.Session
	if err := store.DB.Where("session_id = ? AND is_deleted = false", sessionID).
		First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	if err := ensureLoadedSessionAccessible(session); err != nil {
		return nil, err
	}
	if session.SessionType != 2 {
		return nil, ErrSessionInvalidType
	}

	humanMemberIDs, err := listSessionHumanMemberIDs(sessionID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	dissolveMsgID := snowflake.GenID()
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&model.Message{
			MsgID:      dissolveMsgID,
			SessionID:  sessionID,
			SenderID:   0,
			SenderType: 3,
			MsgType:    3,
			Content:    dissolveSystemContent,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.Session{}).
			Where("session_id = ? AND is_deleted = false", sessionID).
			Updates(map[string]any{
				"last_msg_id":      dissolveMsgID,
				"last_msg_summary": dissolveSystemSummary,
				"is_deleted":       true,
				"updated_at":       now,
			}).Error; err != nil {
			return err
		}
		if err := tx.Where("session_id = ?", sessionID).
			Delete(&model.SessionMember{}).Error; err != nil {
			return err
		}
		return recordSessionTombstones(tx, sessionID, humanMemberIDs, now)
	}); err != nil {
		return nil, err
	}

	notifySessionMemberChanged(
		sessionID,
		"dissolve",
		userID,
		humanMemberIDs,
		sessionMemberChangedNotifyMeta{
			RemovedUserIDs: humanMemberIDs,
		},
	)
	return &SessionDissolveResp{SessionID: sessionID}, nil
}
