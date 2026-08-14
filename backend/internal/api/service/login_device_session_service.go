package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrLoginDeviceSessionNotFound     = errors.New("device session not found")
	ErrLoginDeviceSessionIDRequired   = errors.New("session_id required")
	ErrLoginDeviceSessionDeviceIDMiss = errors.New("device_id required")
	ErrLoginDeviceSessionPlatformMiss = errors.New("platform required")
	ErrLoginDeviceSessionMismatch     = errors.New("device session mismatch")
)

type LoginDeviceSessionItem struct {
	SessionID  string    `json:"session_id"`
	DeviceID   string    `json:"device_id"`
	Platform   string    `json:"platform"`
	Online     bool      `json:"online"`
	Current    bool      `json:"current"`
	LastSeenAt time.Time `json:"last_seen_at"`
	CreatedAt  time.Time `json:"created_at"`
}

func RegisterLoginDeviceSession(userID int64, sessionID, deviceID, platform string) error {
	normalizedSessionID, normalizedDeviceID, normalizedPlatform, err := normalizeLoginDeviceSessionInput(
		sessionID,
		deviceID,
		platform,
	)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	return upsertLoginDeviceSessionTx(
		store.DB,
		userID,
		normalizedSessionID,
		normalizedDeviceID,
		normalizedPlatform,
		now,
	)
}

func RegisterLoginDeviceSessionOnGrantTx(tx *gorm.DB, userID int64, sessionID, deviceID, platform string) error {
	normalizedSessionID, normalizedDeviceID, normalizedPlatform, err := normalizeLoginDeviceSessionInput(
		sessionID,
		deviceID,
		platform,
	)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	if _, err := revokeOtherLoginDeviceSessionsOnSameDeviceTx(
		tx,
		userID,
		normalizedSessionID,
		normalizedDeviceID,
		now,
	); err != nil {
		return err
	}

	return upsertLoginDeviceSessionTx(
		tx,
		userID,
		normalizedSessionID,
		normalizedDeviceID,
		normalizedPlatform,
		now,
	)
}

func EnsureLoginDeviceSessionMatch(userID int64, sessionID, deviceID, platform string) error {
	normalizedSessionID, normalizedDeviceID, normalizedPlatform, err := normalizeLoginDeviceSessionInput(
		sessionID,
		deviceID,
		platform,
	)
	if err != nil {
		return err
	}

	var found model.LoginDeviceSession
	if err := store.DB.
		Where("user_id = ? AND session_id = ? AND revoked_at IS NULL", userID, normalizedSessionID).
		First(&found).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrLoginDeviceSessionNotFound
		}
		return err
	}

	if found.DeviceID != normalizedDeviceID || found.Platform != normalizedPlatform {
		return ErrLoginDeviceSessionMismatch
	}
	return nil
}

func EnsureLoginDeviceSessionReady(userID int64, sessionID, deviceID, platform string) error {
	normalizedSessionID, normalizedDeviceID, normalizedPlatform, err := normalizeLoginDeviceSessionInput(
		sessionID,
		deviceID,
		platform,
	)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	return store.DB.Transaction(func(tx *gorm.DB) error {
		var found model.LoginDeviceSession
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND session_id = ?", userID, normalizedSessionID).
			First(&found).Error; err == nil {
			if found.RevokedAt != nil {
				return ErrLoginDeviceSessionNotFound
			}
			if found.DeviceID != normalizedDeviceID || found.Platform != normalizedPlatform {
				return ErrLoginDeviceSessionMismatch
			}
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		active, err := hasActiveRefreshTokenFamilyTx(tx, userID, normalizedSessionID, now)
		if err != nil {
			return err
		}
		if !active {
			return ErrLoginDeviceSessionNotFound
		}

		if err := upsertLoginDeviceSessionTx(
			tx,
			userID,
			normalizedSessionID,
			normalizedDeviceID,
			normalizedPlatform,
			now,
		); err != nil {
			if isDuplicatedConstraintErr(err, "idx_login_device_sessions_user_device_active") {
				return ErrLoginDeviceSessionMismatch
			}
			return err
		}
		return nil
	})
}

func TouchLoginDeviceSession(userID int64, sessionID string) error {
	if userID <= 0 {
		return nil
	}

	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return nil
	}

	now := time.Now().UTC()
	return store.DB.Model(&model.LoginDeviceSession{}).
		Where("user_id = ? AND session_id = ? AND revoked_at IS NULL", userID, normalizedSessionID).
		Updates(map[string]any{
			"last_seen_at": now,
			"updated_at":   now,
		}).Error
}

func ListLoginDeviceSessions(userID int64, currentSessionID string) ([]LoginDeviceSessionItem, error) {
	var sessions []model.LoginDeviceSession
	if err := store.DB.
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Order("last_seen_at DESC").
		Find(&sessions).Error; err != nil {
		return nil, err
	}

	items := make([]LoginDeviceSessionItem, 0, len(sessions))
	for _, session := range sessions {
		items = append(items, LoginDeviceSessionItem{
			SessionID:  session.SessionID,
			DeviceID:   session.DeviceID,
			Platform:   session.Platform,
			Online:     isLoginDeviceOnline(userID, session.DeviceID),
			Current:    session.SessionID == strings.TrimSpace(currentSessionID),
			LastSeenAt: session.LastSeenAt,
			CreatedAt:  session.CreatedAt,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Current != items[j].Current {
			return items[i].Current
		}
		if items[i].Online != items[j].Online {
			return items[i].Online
		}
		if !items[i].LastSeenAt.Equal(items[j].LastSeenAt) {
			return items[i].LastSeenAt.After(items[j].LastSeenAt)
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})

	return items, nil
}

func RevokeLoginDeviceSession(userID int64, sessionID string) error {
	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return ErrLoginDeviceSessionIDRequired
	}

	var found *model.LoginDeviceSession
	now := time.Now().UTC()
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		session, err := setLoginDeviceSessionRevokedTx(tx, userID, normalizedSessionID, now)
		if err != nil {
			return err
		}
		found = session
		return revokeRefreshFamilyTx(tx, userID, normalizedSessionID, now)
	}); err != nil {
		return err
	}

	if err := security.MarkLoginSessionRevoked(userID, normalizedSessionID); err != nil {
		return err
	}
	if found != nil {
		if err := deactivateUserDeviceBinding(userID, found.DeviceID); err != nil {
			return err
		}
		if err := publishKickToDevice(userID, found.DeviceID, "session_revoked"); err != nil {
			return err
		}
	}

	return nil
}

func setLoginDeviceSessionRevokedTx(
	tx *gorm.DB,
	userID int64,
	sessionID string,
	revokedAt time.Time,
) (*model.LoginDeviceSession, error) {
	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return nil, ErrLoginDeviceSessionIDRequired
	}

	var found model.LoginDeviceSession
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ? AND session_id = ?", userID, normalizedSessionID).
		First(&found).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLoginDeviceSessionNotFound
		}
		return nil, err
	}

	if found.RevokedAt != nil {
		return &found, nil
	}

	if err := tx.Model(&model.LoginDeviceSession{}).
		Where("user_id = ? AND session_id = ? AND revoked_at IS NULL", userID, normalizedSessionID).
		Updates(map[string]any{
			"revoked_at": revokedAt,
			"updated_at": revokedAt,
		}).Error; err != nil {
		return nil, err
	}

	found.RevokedAt = &revokedAt
	found.UpdatedAt = revokedAt
	return &found, nil
}

func setAllLoginDeviceSessionsRevokedTx(
	tx *gorm.DB,
	userID int64,
	revokedAt time.Time,
) ([]model.LoginDeviceSession, error) {
	var sessions []model.LoginDeviceSession
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Find(&sessions).Error; err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, nil
	}

	if err := tx.Model(&model.LoginDeviceSession{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Updates(map[string]any{
			"revoked_at": revokedAt,
			"updated_at": revokedAt,
		}).Error; err != nil {
		return nil, err
	}

	for i := range sessions {
		sessions[i].RevokedAt = &revokedAt
		sessions[i].UpdatedAt = revokedAt
	}
	return sessions, nil
}

func isLoginDeviceOnline(userID int64, deviceID string) bool {
	if store.RDB == nil || userID <= 0 {
		return false
	}

	normalizedDeviceID := strings.TrimSpace(deviceID)
	if normalizedDeviceID == "" {
		return false
	}

	exists, err := store.RDB.Exists(
		context.Background(),
		fmt.Sprintf("im:ws:alive:%d:%s", userID, normalizedDeviceID),
	).Result()
	return err == nil && exists > 0
}

func publishKickToDevice(userID int64, deviceID, reason string) error {
	if store.RDB == nil || userID <= 0 {
		return nil
	}

	normalizedDeviceID := strings.TrimSpace(deviceID)
	if normalizedDeviceID == "" {
		return nil
	}

	nodeID, err := store.RDB.HGet(
		context.Background(),
		fmt.Sprintf("im:ws:route:%d", userID),
		normalizedDeviceID,
	).Result()
	if errors.Is(err, redis.Nil) {
		return nil
	}
	if err != nil {
		return err
	}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return nil
	}

	payload, err := json.Marshal(map[string]any{
		"user_id":          userID,
		"cmd":              protocol.CmdKicked,
		"payload":          map[string]string{"reason": reason},
		"target_device_id": normalizedDeviceID,
	})
	if err != nil {
		return err
	}

	return store.RDB.Publish(
		context.Background(),
		fmt.Sprintf("chan:%s", nodeID),
		string(payload),
	).Err()
}

func normalizeLoginDeviceSessionInput(sessionID, deviceID, platform string) (string, string, string, error) {
	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return "", "", "", ErrLoginDeviceSessionIDRequired
	}

	normalizedDeviceID := strings.TrimSpace(deviceID)
	if normalizedDeviceID == "" {
		return "", "", "", ErrLoginDeviceSessionDeviceIDMiss
	}

	normalizedPlatform := strings.TrimSpace(platform)
	if normalizedPlatform == "" {
		return "", "", "", ErrLoginDeviceSessionPlatformMiss
	}

	return normalizedSessionID, normalizedDeviceID, normalizedPlatform, nil
}

func upsertLoginDeviceSessionTx(
	tx *gorm.DB,
	userID int64,
	sessionID, deviceID, platform string,
	now time.Time,
) error {
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "session_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"user_id":      userID,
			"device_id":    deviceID,
			"platform":     platform,
			"last_seen_at": now,
			"revoked_at":   nil,
			"updated_at":   now,
		}),
	}).Create(&model.LoginDeviceSession{
		SessionID:  sessionID,
		UserID:     userID,
		DeviceID:   deviceID,
		Platform:   platform,
		LastSeenAt: now,
	}).Error
}

func hasActiveRefreshTokenFamilyTx(
	tx *gorm.DB,
	userID int64,
	sessionID string,
	now time.Time,
) (bool, error) {
	var count int64
	if err := tx.Model(&model.RefreshToken{}).
		Where(
			"user_id = ? AND family_id = ? AND status = ? AND expires_at > ? AND revoked_at IS NULL",
			userID,
			sessionID,
			model.RefreshTokenStatusActive,
			now,
		).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func revokeOtherLoginDeviceSessionsOnSameDeviceTx(
	tx *gorm.DB,
	userID int64,
	sessionID, deviceID string,
	revokedAt time.Time,
) ([]string, error) {
	var sessions []model.LoginDeviceSession
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(
			"user_id = ? AND device_id = ? AND revoked_at IS NULL AND session_id <> ?",
			userID,
			deviceID,
			sessionID,
		).
		Find(&sessions).Error; err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, nil
	}

	sessionIDs := make([]string, 0, len(sessions))
	for _, session := range sessions {
		sessionIDs = append(sessionIDs, session.SessionID)
	}

	if err := tx.Model(&model.LoginDeviceSession{}).
		Where(
			"user_id = ? AND device_id = ? AND revoked_at IS NULL AND session_id <> ?",
			userID,
			deviceID,
			sessionID,
		).
		Updates(map[string]any{
			"revoked_at": revokedAt,
			"updated_at": revokedAt,
		}).Error; err != nil {
		return nil, err
	}

	if err := tx.Model(&model.RefreshToken{}).
		Where("user_id = ? AND family_id IN ? AND status IN ?", userID, sessionIDs, []int16{
			model.RefreshTokenStatusActive,
			model.RefreshTokenStatusUsed,
		}).
		Updates(map[string]any{
			"status":     model.RefreshTokenStatusRevoked,
			"revoked_at": revokedAt,
			"updated_at": revokedAt,
		}).Error; err != nil {
		return nil, err
	}

	return sessionIDs, nil
}
