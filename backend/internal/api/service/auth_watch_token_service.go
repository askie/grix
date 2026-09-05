package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WatchPlatform marks the login device sessions that belong to an Apple Watch
// companion. It is the only thing that tells a watch refresh family apart from
// a phone one, so every rule that must cover the watch keys off it.
const WatchPlatform = "watch"

// watchDeviceID is deterministic per user: one user has at most one watch
// credential, and the unique index on (user_id, device_id) where revoked_at is
// null turns that invariant into a database constraint.
func watchDeviceID(userID int64) string {
	return fmt.Sprintf("watch:%d", userID)
}

// IssueWatchTokens mints a token pair for the caller's Apple Watch in a refresh
// family of its own.
//
// The watch cannot log in — it has no keyboard and no password — so the phone
// asks for this pair with its own access token. Giving the watch its own family
// (rather than a copy of the phone's refresh token) is what keeps the two from
// rotating each other out of existence: refresh tokens rotate per use and a
// replay revokes the whole family.
//
// Re-issuing revokes the previous watch family first, so a user never holds two
// live watch credentials.
func IssueWatchTokens(userID int64) (*LoginResp, error) {
	if userID <= 0 {
		return nil, errors.New("用户不存在")
	}

	now := time.Now().UTC()
	var (
		resp            *LoginResp
		revokedFamilies []string
	)

	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.First(&user, userID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("用户不存在")
			}
			return fmt.Errorf("%w: %v", ErrAuthServiceUnavailable, err)
		}
		if err := security.EnsureUserActiveWithDB(tx, user.ID); err != nil {
			if errors.Is(err, security.ErrUserDisabled) {
				return err
			}
			if errors.Is(err, security.ErrUserNotFound) {
				return errors.New("用户不存在")
			}
			return fmt.Errorf("%w: %v", ErrAuthServiceUnavailable, err)
		}

		families, err := revokeWatchLoginSessionsTx(tx, user.ID, now)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrAuthServiceUnavailable, err)
		}
		revokedFamilies = families

		familyID := uuid.NewString()
		accessToken, expiresIn, err := jwtpkg.GenerateAccessTokenWithSession(user.ID, familyID)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrAuthServiceUnavailable, err)
		}
		refreshToken, refreshJTI, refreshExpiresAt, err := jwtpkg.GenerateRefreshTokenWithFamily(user.ID, familyID, "")
		if err != nil {
			return fmt.Errorf("%w: %v", ErrAuthServiceUnavailable, err)
		}
		if err := tx.Create(&model.RefreshToken{
			JTI:       refreshJTI,
			UserID:    user.ID,
			FamilyID:  familyID,
			Status:    model.RefreshTokenStatusActive,
			ExpiresAt: refreshExpiresAt,
		}).Error; err != nil {
			return fmt.Errorf("%w: %v", ErrAuthServiceUnavailable, err)
		}

		// The watch registers no push binding and joins no WebSocket route, so
		// this deliberately skips the device-handover cleanup that login does.
		if err := RegisterLoginDeviceSessionOnGrantTx(
			tx,
			user.ID,
			familyID,
			watchDeviceID(user.ID),
			WatchPlatform,
		); err != nil {
			return err
		}

		resp = &LoginResp{
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
			ExpiresIn:    expiresIn,
			User:         user,
			WsEndpoint:   config.C.Server.PublicWsURL,
			Region:       user.Region,
		}
		return nil
	}); err != nil {
		return nil, err
	}

	// Access tokens are stateless; only this cache makes the revoked family's
	// still-unexpired access token stop working.
	for _, familyID := range revokedFamilies {
		if err := security.MarkLoginSessionRevoked(userID, familyID); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrAuthServiceUnavailable, err)
		}
	}

	return resp, nil
}

// revokeWatchLoginSessionsTx revokes every live watch login session of a user
// and the refresh family behind each one. It returns the revoked family ids so
// the caller can invalidate their access tokens after the commit.
func revokeWatchLoginSessionsTx(tx *gorm.DB, userID int64, now time.Time) ([]string, error) {
	var sessions []model.LoginDeviceSession
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ? AND platform = ? AND revoked_at IS NULL", userID, WatchPlatform).
		Find(&sessions).Error; err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, nil
	}

	familyIDs := make([]string, 0, len(sessions))
	for _, session := range sessions {
		familyIDs = append(familyIDs, session.SessionID)
	}

	if err := tx.Model(&model.LoginDeviceSession{}).
		Where("user_id = ? AND session_id IN ?", userID, familyIDs).
		Updates(map[string]any{
			"revoked_at": now,
			"updated_at": now,
		}).Error; err != nil {
		return nil, err
	}

	if err := tx.Model(&model.RefreshToken{}).
		Where("user_id = ? AND family_id IN ? AND status IN ?", userID, familyIDs, []int16{
			model.RefreshTokenStatusActive,
			model.RefreshTokenStatusUsed,
		}).
		Updates(map[string]any{
			"status":     model.RefreshTokenStatusRevoked,
			"revoked_at": now,
			"updated_at": now,
		}).Error; err != nil {
		return nil, err
	}

	return familyIDs, nil
}

// listWatchFamilyIDsTx reports the user's live watch families without touching
// them. Used by logout, which revokes them alongside the caller's own family.
func listWatchFamilyIDsTx(tx *gorm.DB, userID int64) ([]string, error) {
	var sessions []model.LoginDeviceSession
	if err := tx.Where("user_id = ? AND platform = ? AND revoked_at IS NULL", userID, WatchPlatform).
		Find(&sessions).Error; err != nil {
		return nil, err
	}
	familyIDs := make([]string, 0, len(sessions))
	for _, session := range sessions {
		if id := strings.TrimSpace(session.SessionID); id != "" {
			familyIDs = append(familyIDs, id)
		}
	}
	return familyIDs, nil
}
