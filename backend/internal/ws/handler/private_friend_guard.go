package handler

import (
	"errors"

	apiservice "github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

var errPrivatePeerNotFriend = errors.New("member is not friend")
var errPrivatePeerBlocked = errors.New("you have been blocked by this user")

func validatePrivateHumanSendPermission(sessionID string, senderID int64, senderType int16, sessionType int16) error {
	if senderType != 1 || senderID <= 0 || sessionID == "" {
		return nil
	}

	if sessionType <= 0 { // 未由调用方预加载时回退(走进程内缓存)
		sessionType = loadSessionType(sessionID)
	}
	if sessionType != 1 {
		return nil
	}
	if isWidgetSession, err := isWidgetSessionBySessionID(sessionID); err != nil {
		return err
	} else if isWidgetSession {
		return nil
	}

	peerID, err := loadPrivateHumanPeerID(sessionID, senderID)
	if err != nil {
		return err
	}
	if peerID <= 0 {
		return nil
	}
	if err := apiservice.EnsureUserNotBlocked(peerID, senderID); err != nil {
		if errors.Is(err, apiservice.ErrUserBlockedByPeer) {
			return errPrivatePeerBlocked
		}
		return err
	}

	var count int64
	if err := store.DB.Model(&model.Friend{}).
		Where("user_id = ? AND friend_id = ?", senderID, peerID).
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return errPrivatePeerNotFriend
	}
	return nil
}

func isWidgetSessionBySessionID(sessionID string) (bool, error) {
	var count int64
	if err := store.DB.Model(&model.WidgetSession{}).
		Where("session_id = ? AND status = ?", sessionID, model.WidgetSessionStatusActive).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func loadPrivateHumanPeerID(sessionID string, senderID int64) (int64, error) {
	var peer model.SessionMember
	if err := store.DB.Select("member_id").
		Where("session_id = ? AND member_type = 1 AND member_id != ?", sessionID, senderID).
		Order("joined_at ASC").
		First(&peer).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return peer.MemberID, nil
}
