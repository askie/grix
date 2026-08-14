package service

import (
	"errors"
	"strconv"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func createQRLoginSession(rec *model.AuthQRLoginSession) error {
	if rec == nil {
		return errors.New("qr login session is nil")
	}
	return store.DB.Create(rec).Error
}

func getQRLoginSessionForUpdateBySessionAndPollTokenHash(
	tx *gorm.DB,
	sessionID string,
	pollTokenHash string,
) (*model.AuthQRLoginSession, error) {
	var rec model.AuthQRLoginSession
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("session_id = ? AND poll_token_hash = ?", sessionID, pollTokenHash).
		First(&rec).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}

func getQRLoginSessionForUpdateBySessionAndQRTokenHash(
	tx *gorm.DB,
	sessionID string,
	qrTokenHash string,
) (*model.AuthQRLoginSession, error) {
	var rec model.AuthQRLoginSession
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("session_id = ? AND qr_token_hash = ?", sessionID, qrTokenHash).
		First(&rec).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}

func getQRLoginSessionForUpdateBySessionID(tx *gorm.DB, sessionID string) (*model.AuthQRLoginSession, error) {
	var rec model.AuthQRLoginSession
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("session_id = ?", sessionID).
		First(&rec).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}

func loadQRLoginScannerUser(userID int64) (*QRLoginScannerUser, error) {
	var user model.User
	if err := store.DB.Select("id", "nickname", "avatar_url").
		Where("id = ? AND status = ?", userID, model.UserStatusActive).
		First(&user).Error; err != nil {
		return nil, err
	}
	return &QRLoginScannerUser{
		UserID:    formatInt64ID(user.ID),
		Nickname:  user.Nickname,
		AvatarURL: user.AvatarURL,
	}, nil
}

func formatInt64ID(v int64) string {
	return strconv.FormatInt(v, 10)
}
