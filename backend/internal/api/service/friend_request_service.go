package service

import (
	"errors"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

type FriendRequestItem struct {
	ID         int64     `json:"id,string"`
	FromUserID int64     `json:"from_user_id,string"`
	Username   string    `json:"username"`
	Nickname   string    `json:"nickname"`
	AvatarURL  string    `json:"avatar_url"`
	Message    string    `json:"message"`
	Status     int8      `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

func SendFriendRequest(fromID, toID int64, message string) error {
	if toID <= 0 {
		return errors.New("invalid target user")
	}
	if err := ensureFriendRequestTargetIsNotAgent(fromID, toID); err != nil {
		return err
	}
	if err := ensureUsersCanEstablishFriendship(fromID, toID); err != nil {
		return err
	}

	var target model.User
	if err := store.DB.Select("id", "username").
		Where("id = ? AND status = ?", toID, model.UserStatusActive).
		First(&target).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("target user not found")
		}
		return err
	}
	if isHiddenFriendSearchUsername(target.Username) {
		return errors.New("target user not found")
	}

	if fromID == toID {
		return errors.New("cannot add yourself as friend")
	}

	var count int64
	store.DB.Model(&model.Friend{}).Where("user_id = ? AND friend_id = ?", fromID, toID).Count(&count)
	if count > 0 {
		return errors.New("already friends")
	}

	var pendingReq model.FriendRequest
	hasPendingReq := false
	if err := store.DB.
		Where(
			"from_user_id = ? AND to_user_id = ? AND status = ?",
			fromID,
			toID,
			friendRequestStatusPending,
		).
		First(&pendingReq).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	} else {
		hasPendingReq = true
	}

	friendAddMode, err := getUserFriendAddMode(toID)
	if err != nil {
		return err
	}
	switch friendAddMode {
	case model.FriendAddSettingNeedApproval:
		if hasPendingReq {
			return errors.New("friend request already sent")
		}

		req := model.FriendRequest{
			ID:         nextFriendID(),
			FromUserID: fromID,
			ToUserID:   toID,
			Status:     friendRequestStatusPending,
			Message:    message,
		}
		if err := store.DB.Create(&req).Error; err != nil {
			return err
		}

		notifyFriendRequestReceived(req)
		return nil
	case model.FriendAddSettingAutoApprove:
		if hasPendingReq {
			return HandleFriendRequest(pendingReq.ID, toID, true)
		}

		req := model.FriendRequest{
			ID:         nextFriendID(),
			FromUserID: fromID,
			ToUserID:   toID,
			Status:     friendRequestStatusAccepted,
			Message:    message,
		}
		if err := store.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&req).Error; err != nil {
				return err
			}
			return createBidirectionalFriendship(tx, fromID, toID, time.Now())
		}); err != nil {
			return err
		}

		notifyFriendRequestHandled(req, friendRequestStatusAccepted)
		notifyFriendAdded(fromID, toID)
		notifyFriendAdded(toID, fromID)
		return nil
	case model.FriendAddSettingForbidden:
		return errors.New("target user does not allow adding friends")
	default:
		return errors.New("target user friend-add setting is invalid")
	}
}

func GetFriendRequests(userID int64) ([]FriendRequestItem, error) {
	var requests []model.FriendRequest
	if err := store.DB.Where("to_user_id = ?", userID).Order("created_at DESC").Find(&requests).Error; err != nil {
		return nil, err
	}

	items := make([]FriendRequestItem, 0, len(requests))
	for _, r := range requests {
		var user model.User
		store.DB.First(&user, r.FromUserID)
		items = append(items, FriendRequestItem{
			ID:         r.ID,
			FromUserID: r.FromUserID,
			Username:   user.Username,
			Nickname:   user.Nickname,
			AvatarURL:  user.AvatarURL,
			Message:    r.Message,
			Status:     r.Status,
			CreatedAt:  r.CreatedAt,
		})
	}
	return items, nil
}

func HandleFriendRequest(requestID int64, userID int64, accept bool) error {
	var req model.FriendRequest
	if err := store.DB.First(&req, requestID).Error; err != nil {
		return errors.New("friend request not found")
	}

	if req.ToUserID != userID {
		return errors.New("not authorized to handle this request")
	}
	if req.Status != friendRequestStatusPending {
		return errors.New("request already handled")
	}

	if accept {
		if err := ensureUsersCanEstablishFriendship(req.FromUserID, req.ToUserID); err != nil {
			return err
		}
		if err := store.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&req).Update("status", friendRequestStatusAccepted).Error; err != nil {
				return err
			}
			return createBidirectionalFriendship(tx, req.FromUserID, req.ToUserID, time.Now())
		}); err != nil {
			return err
		}

		notifyFriendRequestHandled(req, friendRequestStatusAccepted)
		notifyFriendAdded(req.FromUserID, req.ToUserID)
		notifyFriendAdded(req.ToUserID, req.FromUserID)
		return nil
	}

	if err := store.DB.Model(&req).Update("status", friendRequestStatusRejected).Error; err != nil {
		return err
	}
	notifyFriendRequestHandled(req, friendRequestStatusRejected)
	return nil
}

func ensureFriendRequestTargetIsNotAgent(fromID, toID int64) error {
	var agent model.Agent
	if err := store.DB.Select("id", "owner_id", "status").
		Where("id = ? AND status != ?", toID, 3).
		First(&agent).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}

	if agent.OwnerID != fromID {
		return errors.New("cannot add other user's agent as friend")
	}
	return errors.New("cannot add agent as friend")
}

func ensureUsersCanEstablishFriendship(fromID, toID int64) error {
	if err := EnsureUserNotBlocked(toID, fromID); err != nil {
		if errors.Is(err, ErrUserBlockedByPeer) {
			return errors.New("target user has blocked you")
		}
		return err
	}
	if err := EnsureUserNotBlocked(fromID, toID); err != nil {
		if errors.Is(err, ErrUserBlockedByPeer) {
			return errors.New("you have blocked this user")
		}
		return err
	}
	return nil
}

func createBidirectionalFriendship(tx *gorm.DB, userID int64, friendID int64, now time.Time) error {
	baseID := nextFriendID()
	friends := []model.Friend{
		{ID: baseID, UserID: userID, FriendID: friendID, CreatedAt: now},
		{ID: nextFriendID(), UserID: friendID, FriendID: userID, CreatedAt: now},
	}
	for _, relation := range friends {
		if err := tx.Create(&relation).Error; err != nil {
			return err
		}
	}
	return nil
}
