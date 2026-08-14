package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"gorm.io/gorm"
)

var (
	ErrSessionRuntimeSettingDenied         = errors.New("only owner/admin can update group settings")
	ErrSessionMemberInviteDisabled         = errors.New("member invites are disabled for this group")
	ErrSessionMemberInviteThresholdReached = errors.New("member invite threshold reached")
)

type SessionUpdateInviteSettingResp struct {
	SessionID         string `json:"session_id"`
	AllowMemberInvite bool   `json:"allow_member_invite"`
}

type sessionMemberInviteThresholdError struct {
	Threshold int
}

func (e *sessionMemberInviteThresholdError) Error() string {
	return fmt.Sprintf(
		"normal members cannot invite new members when group size exceeds %d",
		e.Threshold,
	)
}

func (e *sessionMemberInviteThresholdError) Unwrap() error {
	return ErrSessionMemberInviteThresholdReached
}

func SessionUpdateInviteSetting(
	userID int64,
	sessionID string,
	allowMemberInvite bool,
) (*SessionUpdateInviteSettingResp, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil, ErrSessionNotFound
	}

	var operator model.SessionMember
	if err := store.DB.Select("role").
		Where("session_id = ? AND member_id = ? AND member_type = 1", sid, userID).
		First(&operator).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionPermissionDenied
		}
		return nil, err
	}
	if operator.Role != 2 && operator.Role != 3 {
		return nil, ErrSessionRuntimeSettingDenied
	}

	var session model.Session
	if err := store.DB.Select("session_id", "session_type", "allow_member_invite", "moderation_status").
		Where("session_id = ? AND is_deleted = false", sid).
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

	if session.AllowMemberInvite != allowMemberInvite {
		now := time.Now()
		if err := store.DB.Model(&model.Session{}).
			Where("session_id = ?", sid).
			Updates(map[string]any{
				"allow_member_invite": allowMemberInvite,
				"updated_at":          now,
			}).Error; err != nil {
			return nil, err
		}
	}

	return &SessionUpdateInviteSettingResp{
		SessionID:         sid,
		AllowMemberInvite: allowMemberInvite,
	}, nil
}

func validateSessionMemberInvitePermission(
	operatorRole int16,
	session model.Session,
	sessionID string,
) error {
	return validateSessionMemberInvitePermissionWithDB(
		nil,
		operatorRole,
		session,
		sessionID,
	)
}

func validateSessionMemberInvitePermissionWithDB(
	db *gorm.DB,
	operatorRole int16,
	session model.Session,
	sessionID string,
) error {
	if operatorRole != 1 {
		return nil
	}
	if !session.AllowMemberInvite {
		return ErrSessionMemberInviteDisabled
	}

	queryDB := db
	if queryDB == nil {
		queryDB = store.DB
	}

	settings, err := getGroupSettingsForInviteValidation(queryDB)
	if err != nil {
		return err
	}

	var memberCount int64
	if err := queryDB.Model(&model.SessionMember{}).
		Where("session_id = ?", sessionID).
		Count(&memberCount).Error; err != nil {
		return err
	}
	if int(memberCount) > settings.MemberInviteThreshold {
		return &sessionMemberInviteThresholdError{Threshold: settings.MemberInviteThreshold}
	}
	return nil
}

func getGroupSettingsForInviteValidation(db *gorm.DB) (systemsetting.GroupSettings, error) {
	if db == nil || db == store.DB {
		return systemsetting.GetGroupSettings()
	}

	settings := systemsetting.DefaultGroupSettings()
	var row model.SystemSetting
	if err := db.First(&row, "key = ?", "group").Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return settings, nil
		}
		return systemsetting.GroupSettings{}, err
	}
	if len(row.Value) == 0 {
		return settings, nil
	}
	if err := json.Unmarshal(row.Value, &settings); err != nil {
		return systemsetting.GroupSettings{}, err
	}
	return settings, nil
}
