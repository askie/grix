package service

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	friendQRCodeLength      = 24
	friendQRCodeMaxRetry    = 8
	friendQRCodeAlphabet    = "23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	defaultFriendQRLinkBase = "https://dhf.pub/u"
)

var errFriendQRCodeNotFound = errors.New("friend qr code not found")

type FriendQRCodeInfo struct {
	Code     string `json:"code"`
	ShareURL string `json:"share_url"`
}

type FriendQRCodeResolveResult struct {
	UserID            int64  `json:"user_id,string"`
	Username          string `json:"username"`
	Nickname          string `json:"nickname"`
	AvatarURL         string `json:"avatar_url"`
	IsSelf            bool   `json:"is_self"`
	IsFriend          bool   `json:"is_friend"`
	OutgoingPending   bool   `json:"outgoing_pending"`
	IncomingPending   bool   `json:"incoming_pending"`
	FriendRequestHint string `json:"friend_request_hint"`
}

func GetOrCreateFriendQRCode(userID int64) (*FriendQRCodeInfo, error) {
	if userID <= 0 {
		return nil, errors.New("invalid user")
	}

	var rec model.FriendQRCode
	if err := store.DB.Where("user_id = ?", userID).First(&rec).Error; err == nil {
		return buildFriendQRCodeInfo(rec.Code), nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	code, err := createFriendQRCode(userID)
	if err != nil {
		return nil, err
	}
	return buildFriendQRCodeInfo(code), nil
}

func ResolveFriendQRCode(viewerUserID int64, code string) (*FriendQRCodeResolveResult, error) {
	normalizedCode := strings.TrimSpace(code)
	if normalizedCode == "" {
		return nil, errFriendQRCodeNotFound
	}

	var rec model.FriendQRCode
	if err := store.DB.Where("code = ?", normalizedCode).First(&rec).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errFriendQRCodeNotFound
		}
		return nil, err
	}

	var target model.User
	if err := store.DB.Select("id", "username", "nickname", "avatar_url", "status").
		Where("id = ? AND status = ?", rec.UserID, model.UserStatusActive).
		First(&target).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errFriendQRCodeNotFound
		}
		return nil, err
	}
	if isHiddenFriendSearchUsername(target.Username) {
		return nil, errFriendQRCodeNotFound
	}

	isSelf := viewerUserID == target.ID && viewerUserID > 0

	isFriend := false
	outgoingPending := false
	incomingPending := false
	if !isSelf && viewerUserID > 0 {
		var cnt int64
		if err := store.DB.Model(&model.Friend{}).
			Where("user_id = ? AND friend_id = ?", viewerUserID, target.ID).
			Count(&cnt).Error; err != nil {
			return nil, err
		}
		isFriend = cnt > 0

		if err := store.DB.Model(&model.FriendRequest{}).
			Where("from_user_id = ? AND to_user_id = ? AND status = 0", viewerUserID, target.ID).
			Count(&cnt).Error; err != nil {
			return nil, err
		}
		outgoingPending = cnt > 0

		if err := store.DB.Model(&model.FriendRequest{}).
			Where("from_user_id = ? AND to_user_id = ? AND status = 0", target.ID, viewerUserID).
			Count(&cnt).Error; err != nil {
			return nil, err
		}
		incomingPending = cnt > 0
	}

	hint := ""
	switch {
	case isSelf:
		hint = "self"
	case isFriend:
		hint = "already_friends"
	case outgoingPending:
		hint = "request_already_sent"
	case incomingPending:
		hint = "request_waiting_for_you"
	}

	return &FriendQRCodeResolveResult{
		UserID:            target.ID,
		Username:          target.Username,
		Nickname:          target.Nickname,
		AvatarURL:         target.AvatarURL,
		IsSelf:            isSelf,
		IsFriend:          isFriend,
		OutgoingPending:   outgoingPending,
		IncomingPending:   incomingPending,
		FriendRequestHint: hint,
	}, nil
}

func IsFriendQRCodeNotFound(err error) bool {
	return errors.Is(err, errFriendQRCodeNotFound)
}

func buildFriendQRCodeInfo(code string) *FriendQRCodeInfo {
	return &FriendQRCodeInfo{
		Code:     code,
		ShareURL: buildFriendQRShareURL(code),
	}
}

func createFriendQRCode(userID int64) (string, error) {
	for i := 0; i < friendQRCodeMaxRetry; i++ {
		code, err := generateFriendQRCode(friendQRCodeLength)
		if err != nil {
			return "", err
		}

		now := time.Now()
		rec := model.FriendQRCode{
			UserID:    userID,
			Code:      code,
			RotatedAt: now,
		}
		err = store.DB.Transaction(func(tx *gorm.DB) error {
			var existing model.FriendQRCode
			err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("user_id = ?", userID).
				First(&existing).Error
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					if createErr := tx.Create(&rec).Error; createErr != nil {
						return createErr
					}
					return nil
				}
				return err
			}
			return tx.Model(&model.FriendQRCode{}).
				Where("user_id = ?", userID).
				Updates(map[string]any{
					"code":       code,
					"rotated_at": now,
				}).Error
		})
		if err == nil {
			return code, nil
		}
		if !isUniqueConstraintErr(err) {
			return "", err
		}
	}

	return "", errors.New("failed to allocate unique friend qr code")
}

func generateFriendQRCode(length int) (string, error) {
	if length <= 0 {
		return "", errors.New("invalid friend qr code length")
	}
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	alphabetLen := len(friendQRCodeAlphabet)
	result := make([]byte, length)
	for i := 0; i < length; i++ {
		result[i] = friendQRCodeAlphabet[int(buf[i])%alphabetLen]
	}
	return string(result), nil
}

func buildFriendQRShareURL(code string) string {
	normalizedCode := strings.TrimSpace(code)
	if normalizedCode == "" {
		return ""
	}

	base := strings.TrimSpace(config.C.Server.FriendQRBaseURL)
	if base == "" {
		base = defaultFriendQRLinkBase
	}
	return fmt.Sprintf("%s/%s", strings.TrimRight(base, "/"), normalizedCode)
}

func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "duplicate key") {
		return true
	}
	if strings.Contains(lower, "unique constraint") {
		return true
	}
	if strings.Contains(lower, "unique violation") {
		return true
	}
	return false
}
