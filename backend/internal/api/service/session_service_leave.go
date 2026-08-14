package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/askie/grix/backend/internal/agentreceive"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type sessionLeaveTargets struct {
	members               []memberIdentity
	removedHumanMemberIDs []int64
	delegateStateOwnerID  int64
}

func SessionLeave(userID int64, sessionID string) (*SessionLeaveResp, error) {
	_, err := loadGroupSessionForLeave(sessionID)
	if err != nil {
		return nil, err
	}

	humanMember, err := findSessionMember(sessionID, userID, 1)
	if err != nil {
		return nil, err
	}
	if humanMember == nil {
		return &SessionLeaveResp{SessionID: sessionID, Left: false}, nil
	}
	if humanMember.Role == 3 {
		return nil, ErrSessionOwnerCannotLeave
	}

	targets := sessionLeaveTargets{
		members: []memberIdentity{{
			MemberID:   humanMember.MemberID,
			MemberType: humanMember.MemberType,
		}},
		removedHumanMemberIDs: []int64{humanMember.MemberID},
		delegateStateOwnerID:  humanMember.MemberID,
	}

	left, err := executeSessionLeave(sessionID, targets.members)
	if err != nil {
		return nil, err
	}
	if !left {
		return &SessionLeaveResp{SessionID: sessionID, Left: false}, nil
	}

	clearSessionLeaveState(sessionID, targets)
	if err := notifySessionLeave(sessionID, userID, targets.removedHumanMemberIDs); err != nil {
		return nil, err
	}

	return &SessionLeaveResp{
		SessionID: sessionID,
		Left:      true,
	}, nil
}

func SessionLeaveByAgent(agentID, ownerID int64, sessionID string) (*SessionLeaveResp, error) {
	session, err := loadGroupSessionForLeave(sessionID)
	if err != nil {
		return nil, err
	}

	targets, err := resolveSessionAgentLeaveTargets(agentID, ownerID, sessionID)
	if err != nil {
		return nil, err
	}
	if len(targets.members) == 0 {
		return &SessionLeaveResp{SessionID: sessionID, Left: false}, nil
	}

	left, err := executeSessionLeave(sessionID, targets.members)
	if err != nil {
		return nil, err
	}
	if !left {
		return &SessionLeaveResp{
			SessionID: sessionID,
			Left:      false,
		}, nil
	}

	clearSessionLeaveState(sessionID, targets)
	operatorID := ownerID
	if operatorID <= 0 {
		operatorID = session.OwnerID
	}
	if err := notifySessionLeave(sessionID, operatorID, targets.removedHumanMemberIDs); err != nil {
		return nil, err
	}

	return &SessionLeaveResp{
		SessionID: sessionID,
		Left:      true,
	}, nil
}

func resolveSessionAgentLeaveTargets(agentID, ownerID int64, sessionID string) (sessionLeaveTargets, error) {
	delegatedOwnerID, err := resolveDelegatedLeaveOwnerID(agentID, ownerID, sessionID)
	if err != nil {
		return sessionLeaveTargets{}, err
	}

	targets := sessionLeaveTargets{
		members: make([]memberIdentity, 0, 2),
	}
	if delegatedOwnerID > 0 {
		targets.delegateStateOwnerID = delegatedOwnerID
		humanMember, err := findSessionMember(sessionID, delegatedOwnerID, 1)
		if err != nil {
			return sessionLeaveTargets{}, err
		}
		if humanMember != nil {
			if humanMember.Role == 3 {
				return sessionLeaveTargets{}, ErrSessionOwnerCannotLeave
			}
			targets.members = append(targets.members, memberIdentity{
				MemberID:   humanMember.MemberID,
				MemberType: humanMember.MemberType,
			})
			targets.removedHumanMemberIDs = append(targets.removedHumanMemberIDs, humanMember.MemberID)
		}
	}

	agentMember, err := findSessionMember(sessionID, agentID, 2)
	if err != nil {
		return sessionLeaveTargets{}, err
	}
	if agentMember != nil {
		targets.members = append(targets.members, memberIdentity{
			MemberID:   agentMember.MemberID,
			MemberType: agentMember.MemberType,
		})
	}

	if len(targets.members) == 0 && logger.L != nil {
		logger.L.Warnf(
			"group_leave_self not_member agent=%d owner=%d session=%s",
			agentID,
			ownerID,
			sessionID,
		)
	}
	return targets, nil
}

func loadGroupSessionForLeave(sessionID string) (*model.Session, error) {
	var session model.Session
	if err := store.DB.Where("session_id = ? AND is_deleted = false", sessionID).
		First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	if session.SessionType != model.SessionTypeGroup {
		return nil, ErrSessionInvalidType
	}
	return &session, nil
}

func executeSessionLeave(sessionID string, members []memberIdentity) (bool, error) {
	left := false
	now := time.Now()
	var leftHumanIDs []int64
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		for _, member := range members {
			result := tx.Where(
				"session_id = ? AND member_id = ? AND member_type = ?",
				sessionID,
				member.MemberID,
				member.MemberType,
			).Delete(&model.SessionMember{})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected > 0 {
				left = true
				if member.MemberType == 1 {
					leftHumanIDs = append(leftHumanIDs, member.MemberID)
				}
			}
		}
		if !left {
			return nil
		}
		if err := recordSessionTombstones(tx, sessionID, leftHumanIDs, now); err != nil {
			return err
		}
		return tx.Model(&model.Session{}).
			Where("session_id = ?", sessionID).
			Update("updated_at", now).Error
	}); err != nil {
		return false, err
	}
	return left, nil
}

func notifySessionLeave(sessionID string, operatorID int64, removedHumanMemberIDs []int64) error {
	humanMemberIDs, err := listSessionHumanMemberIDs(sessionID)
	if err != nil {
		return err
	}
	humanMemberIDs = append(humanMemberIDs, removedHumanMemberIDs...)
	notifySessionMemberChanged(
		sessionID,
		"remove",
		operatorID,
		humanMemberIDs,
		sessionMemberChangedNotifyMeta{
			RemovedUserIDs: removedHumanMemberIDs,
		},
	)
	return nil
}

func resolveDelegatedLeaveOwnerID(agentID, ownerID int64, sessionID string) (int64, error) {
	if agentID <= 0 || ownerID <= 0 || sessionID == "" || store.RDB == nil {
		return 0, nil
	}

	delegateKey := fmt.Sprintf("im:delegate:%s:%d", sessionID, ownerID)
	delegatedAgentID, err := store.RDB.HGet(context.Background(), delegateKey, "agent_id").Int64()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if delegatedAgentID != agentID {
		return 0, nil
	}
	return ownerID, nil
}

func findSessionMember(sessionID string, memberID int64, memberType int16) (*model.SessionMember, error) {
	var member model.SessionMember
	if err := store.DB.
		Where("session_id = ? AND member_id = ? AND member_type = ?", sessionID, memberID, memberType).
		First(&member).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &member, nil
}

func clearSessionLeaveState(sessionID string, targets sessionLeaveTargets) {
	ctx := context.Background()
	for _, member := range targets.members {
		_ = agentreceive.ClearBuffer(ctx, sessionID, member.MemberType, member.MemberID)
	}
	if targets.delegateStateOwnerID <= 0 || store.RDB == nil {
		return
	}

	delegateKey := fmt.Sprintf("im:delegate:%s:%d", sessionID, targets.delegateStateOwnerID)
	streakKey := fmt.Sprintf("im:delegate:streak:%s:%d", sessionID, targets.delegateStateOwnerID)
	_ = store.RDB.Del(ctx, delegateKey, streakKey).Err()
}
