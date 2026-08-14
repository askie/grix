package service

import (
	"errors"
	"fmt"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrUserBlockedByPeer = errors.New("you have been blocked by this user")
	ErrUserBlockTarget   = errors.New("invalid blocked user")
)

func IsUserBlocked(blockerID, targetID int64) (bool, error) {
	if blockerID <= 0 || targetID <= 0 {
		return false, nil
	}

	var count int64
	if err := store.DB.Model(&model.UserBlock{}).
		Where("user_id = ? AND blocked_user_id = ?", blockerID, targetID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func EnsureUserNotBlocked(blockerID, targetID int64) error {
	blocked, err := IsUserBlocked(blockerID, targetID)
	if err != nil {
		return err
	}
	if blocked {
		return ErrUserBlockedByPeer
	}
	return nil
}

func BlockUser(userID, blockedUserID int64) error {
	if userID <= 0 || blockedUserID <= 0 || userID == blockedUserID {
		return ErrUserBlockTarget
	}

	var target model.User
	if err := store.DB.Select("id", "username", "status").
		Where("id = ? AND status = ?", blockedUserID, model.UserStatusActive).
		First(&target).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("target user not found")
		}
		return err
	}
	if isHiddenFriendSearchUsername(target.Username) {
		return errors.New("target user not found")
	}

	deletedForUser := false
	deletedForBlockedUser := false

	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		block := model.UserBlock{
			ID:            nextFriendID(),
			UserID:        userID,
			BlockedUserID: blockedUserID,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&block).Error; err != nil {
			return err
		}

		var err error
		deletedForUser, deletedForBlockedUser, err = deleteFriendRelation(tx, userID, blockedUserID)
		if err != nil {
			return err
		}

		return deletePendingFriendRequests(tx, userID, blockedUserID)
	}); err != nil {
		return err
	}

	if deletedForUser {
		pushFriendDeletedEvent(userID, blockedUserID)
	}
	if deletedForBlockedUser {
		pushFriendDeletedEvent(blockedUserID, userID)
	}
	return nil
}

func deletePendingFriendRequests(tx *gorm.DB, userID, targetUserID int64) error {
	if tx == nil {
		return errors.New("nil transaction")
	}

	return tx.Where(
		"status = ? AND ((from_user_id = ? AND to_user_id = ?) OR (from_user_id = ? AND to_user_id = ?))",
		friendRequestStatusPending,
		userID,
		targetUserID,
		targetUserID,
		userID,
	).Delete(&model.FriendRequest{}).Error
}

func deleteFriendRelation(tx *gorm.DB, userID, friendID int64) (bool, bool, error) {
	if tx == nil {
		return false, false, errors.New("nil transaction")
	}

	res := tx.Where("user_id = ? AND friend_id = ?", userID, friendID).
		Delete(&model.Friend{})
	if res.Error != nil {
		return false, false, res.Error
	}
	deletedForUser := res.RowsAffected > 0

	res = tx.Where("user_id = ? AND friend_id = ?", friendID, userID).
		Delete(&model.Friend{})
	if res.Error != nil {
		return false, false, res.Error
	}
	deletedForFriend := res.RowsAffected > 0

	return deletedForUser, deletedForFriend, nil
}

func pushFriendDeletedEvent(userID, friendID int64) {
	pushFriendEvent(userID, map[string]interface{}{
		"event":          "friend_deleted",
		"friend_user_id": fmt.Sprintf("%d", friendID),
	})
}
