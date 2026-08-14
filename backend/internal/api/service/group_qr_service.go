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
	groupQRCodeLength      = 24
	groupQRCodeMaxRetry    = 8
	groupQRCodeAlphabet    = "23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	defaultGroupQRLinkBase = "https://dhf.pub/g"
	groupQRCodeTTL         = 7 * 24 * time.Hour
)

var errGroupQRCodeNotFound = errors.New("group qr code not found")

type GroupQRCodeInfo struct {
	Code     string `json:"code"`
	ShareURL string `json:"share_url"`
}

type GroupQRCodeResolveResult struct {
	Code          string `json:"code"`
	SessionID     string `json:"session_id"`
	GroupName     string `json:"group_name"`
	OwnerID       int64  `json:"owner_id,string"`
	OwnerNickname string `json:"owner_nickname"`
	MemberCount   int    `json:"member_count"`
	IsMember      bool   `json:"is_member"`
}

type GroupQRCodeJoinResult struct {
	SessionID string `json:"session_id"`
	GroupName string `json:"group_name"`
	Joined    bool   `json:"joined"`
}

func GetOrCreateGroupQRCode(userID int64, sessionID string) (*GroupQRCodeInfo, error) {
	if userID <= 0 {
		return nil, errors.New("invalid user")
	}
	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return nil, ErrSessionNotFound
	}

	if err := validateUserCanManageGroupQRCode(userID, normalizedSessionID); err != nil {
		return nil, err
	}

	code, err := getOrCreateOrRotateGroupQRCode(userID, normalizedSessionID)
	if err != nil {
		return nil, err
	}
	return buildGroupQRCodeInfo(code), nil
}

func ResolveGroupQRCode(viewerUserID int64, code string) (*GroupQRCodeResolveResult, error) {
	normalizedCode := strings.TrimSpace(code)
	if normalizedCode == "" {
		return nil, errGroupQRCodeNotFound
	}

	session, err := getGroupSessionByQRCode(normalizedCode)
	if err != nil {
		return nil, err
	}

	var owner model.User
	if err := store.DB.Select("nickname").
		Where("id = ?", session.OwnerID).
		First(&owner).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	var memberCount int64
	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ?", session.SessionID).
		Count(&memberCount).Error; err != nil {
		return nil, err
	}

	isMember := false
	if viewerUserID > 0 {
		var cnt int64
		if err := store.DB.Model(&model.SessionMember{}).
			Where(
				"session_id = ? AND member_id = ? AND member_type = 1",
				session.SessionID,
				viewerUserID,
			).
			Count(&cnt).Error; err != nil {
			return nil, err
		}
		isMember = cnt > 0
	}

	return &GroupQRCodeResolveResult{
		Code:          normalizedCode,
		SessionID:     session.SessionID,
		GroupName:     strings.TrimSpace(session.GroupName),
		OwnerID:       session.OwnerID,
		OwnerNickname: strings.TrimSpace(owner.Nickname),
		MemberCount:   int(memberCount),
		IsMember:      isMember,
	}, nil
}

func JoinGroupByQRCode(joinUserID int64, code string) (*GroupQRCodeJoinResult, error) {
	if joinUserID <= 0 {
		return nil, ErrSessionPermissionDenied
	}
	normalizedCode := strings.TrimSpace(code)
	if normalizedCode == "" {
		return nil, errGroupQRCodeNotFound
	}

	session, err := getGroupSessionByQRCode(normalizedCode)
	if err != nil {
		return nil, err
	}

	joined := false
	now := time.Now()
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		var existing model.SessionMember
		if err := tx.Select("member_id").
			Where(
				"session_id = ? AND member_id = ? AND member_type = 1",
				session.SessionID,
				joinUserID,
			).
			First(&existing).Error; err == nil {
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := validateSessionMemberInvitePermissionWithDB(
			tx,
			1,
			*session,
			session.SessionID,
		); err != nil {
			return err
		}

		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.SessionMember{
			SessionID:    session.SessionID,
			MemberID:     joinUserID,
			MemberType:   1,
			Role:         1,
			JoinedAt:     now,
			LastActiveAt: now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		joined = true
		return tx.Model(&model.Session{}).
			Where("session_id = ?", session.SessionID).
			Update("updated_at", now).Error
	}); err != nil {
		return nil, err
	}

	if joined {
		ensureAutoDelegateForGroupSessionMembers(session.SessionID, joinUserID, []int64{joinUserID}, []int16{1})
		humanMemberIDs, err := listSessionHumanMemberIDs(session.SessionID)
		if err != nil {
			return nil, err
		}
		notifySessionMemberChanged(
			session.SessionID,
			"add",
			joinUserID,
			humanMemberIDs,
			sessionMemberChangedNotifyMeta{},
		)
	}

	return &GroupQRCodeJoinResult{
		SessionID: session.SessionID,
		GroupName: strings.TrimSpace(session.GroupName),
		Joined:    joined,
	}, nil
}

func IsGroupQRCodeNotFound(err error) bool {
	return errors.Is(err, errGroupQRCodeNotFound)
}

func buildGroupQRCodeInfo(code string) *GroupQRCodeInfo {
	return &GroupQRCodeInfo{
		Code:     code,
		ShareURL: buildGroupQRShareURL(code),
	}
}

func getOrCreateOrRotateGroupQRCode(userID int64, sessionID string) (string, error) {
	for i := 0; i < groupQRCodeMaxRetry; i++ {
		code, err := generateGroupQRCode(groupQRCodeLength)
		if err != nil {
			return "", err
		}

		assignedCode := ""
		now := time.Now()
		expiresAt := now.Add(groupQRCodeTTL)
		err = store.DB.Transaction(func(tx *gorm.DB) error {
			var existing model.GroupQRCode
			err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("session_id = ?", sessionID).
				First(&existing).Error
			if err == nil {
				creatorStillPrivileged, creatorCheckErr := isSessionInviteManagerInTx(
					tx,
					sessionID,
					existing.CreatorUserID,
				)
				if creatorCheckErr != nil {
					return creatorCheckErr
				}
				if strings.TrimSpace(existing.Code) != "" &&
					existing.ExpiresAt.After(now) &&
					creatorStillPrivileged {
					assignedCode = strings.TrimSpace(existing.Code)
					return nil
				}
				if updateErr := tx.Model(&model.GroupQRCode{}).
					Where("session_id = ?", sessionID).
					Updates(map[string]any{
						"code":            code,
						"creator_user_id": userID,
						"expires_at":      expiresAt,
						"rotated_at":      now,
					}).Error; updateErr != nil {
					return updateErr
				}
				assignedCode = code
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if createErr := tx.Create(&model.GroupQRCode{
				SessionID:     sessionID,
				Code:          code,
				CreatorUserID: userID,
				ExpiresAt:     expiresAt,
				RotatedAt:     now,
			}).Error; createErr != nil {
				return createErr
			}
			assignedCode = code
			return nil
		})
		if err == nil {
			if assignedCode != "" {
				return assignedCode, nil
			}
			return "", errors.New("empty group qr code")
		}
		if !isUniqueConstraintErr(err) {
			return "", err
		}
	}
	return "", errors.New("failed to allocate unique group qr code")
}

func generateGroupQRCode(length int) (string, error) {
	if length <= 0 {
		return "", errors.New("invalid group qr code length")
	}
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	alphabetLen := len(groupQRCodeAlphabet)
	result := make([]byte, length)
	for i := 0; i < length; i++ {
		result[i] = groupQRCodeAlphabet[int(buf[i])%alphabetLen]
	}
	return string(result), nil
}

func buildGroupQRShareURL(code string) string {
	normalizedCode := strings.TrimSpace(code)
	if normalizedCode == "" {
		return ""
	}

	base := strings.TrimSpace(config.C.Server.GroupQRBaseURL)
	if base == "" {
		base = defaultGroupQRLinkBase
	}
	return fmt.Sprintf("%s/%s", strings.TrimRight(base, "/"), normalizedCode)
}

func validateUserCanManageGroupQRCode(userID int64, sessionID string) error {
	var session model.Session
	if err := store.DB.Select("session_id", "session_type", "is_deleted", "moderation_status").
		Where("session_id = ?", sessionID).
		First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSessionNotFound
		}
		return err
	}
	if err := ensureLoadedSessionAccessible(session); err != nil {
		return err
	}
	if session.SessionType != 2 {
		return ErrSessionInvalidType
	}

	var operator model.SessionMember
	if err := store.DB.Select("role").
		Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, userID).
		First(&operator).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSessionPermissionDenied
		}
		return err
	}
	if operator.Role != 2 && operator.Role != 3 {
		return ErrSessionRoleDenied
	}
	return nil
}

func getGroupSessionByQRCode(code string) (*model.Session, error) {
	var qr model.GroupQRCode
	now := time.Now()
	if err := store.DB.Select("session_id", "creator_user_id", "expires_at").
		Where("code = ?", code).
		First(&qr).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errGroupQRCodeNotFound
		}
		return nil, err
	}
	if !qr.ExpiresAt.After(now) {
		return nil, errGroupQRCodeNotFound
	}

	var session model.Session
	if err := store.DB.Select(
		"session_id",
		"owner_id",
		"session_type",
		"group_name",
		"is_deleted",
		"moderation_status",
		"allow_member_invite",
	).
		Where("session_id = ?", qr.SessionID).
		First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errGroupQRCodeNotFound
		}
		return nil, err
	}
	if err := ensureLoadedSessionAccessible(session); err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil, errGroupQRCodeNotFound
		}
		return nil, err
	}
	if session.SessionType != 2 {
		return nil, errGroupQRCodeNotFound
	}

	var creator model.SessionMember
	if err := store.DB.Select("role").
		Where("session_id = ? AND member_id = ? AND member_type = 1", qr.SessionID, qr.CreatorUserID).
		First(&creator).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errGroupQRCodeNotFound
		}
		return nil, err
	}
	if creator.Role != 2 && creator.Role != 3 {
		return nil, errGroupQRCodeNotFound
	}
	return &session, nil
}

func isSessionInviteManagerInTx(
	tx *gorm.DB,
	sessionID string,
	userID int64,
) (bool, error) {
	if tx == nil || strings.TrimSpace(sessionID) == "" || userID <= 0 {
		return false, nil
	}

	var member model.SessionMember
	if err := tx.Select("role").
		Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, userID).
		First(&member).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return member.Role == 2 || member.Role == 3, nil
}
