package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const reportDescriptionMaxRunes = 500

var (
	ErrReportTargetTypeInvalid = errors.New("invalid report target type")
	ErrReportReasonInvalid     = errors.New("invalid report reason")
	ErrReportTargetNotFound    = errors.New("report target not found")
	ErrReportSelfNotAllowed    = errors.New("cannot report yourself")
	ErrReportPermissionDenied  = errors.New("permission denied")
	ErrReportDescriptionLong   = errors.New("report description too long")
)

var allowedReportReasons = map[string]struct{}{
	"harassment":    {},
	"pornography":   {},
	"violence":      {},
	"fraud":         {},
	"spam":          {},
	"impersonation": {},
	"illegal":       {},
	"other":         {},
}

type CreateReportReq struct {
	TargetType      string   `json:"target_type"`
	TargetUserID    int64    `json:"target_user_id,string"`
	TargetSessionID string   `json:"target_session_id"`
	SourceSessionID string   `json:"source_session_id"`
	ReasonCode      string   `json:"reason_code"`
	Description     string   `json:"description"`
	AssetKeys       []string `json:"asset_keys"`
}

type CreateReportResp struct {
	ReportID int64  `json:"report_id,string"`
	Status   string `json:"status"`
}

func CreateReport(userID int64, req CreateReportReq) (*CreateReportResp, error) {
	if userID <= 0 {
		return nil, ErrReportPermissionDenied
	}

	targetType, err := parseReportTargetType(req.TargetType)
	if err != nil {
		return nil, err
	}

	reasonCode := strings.ToLower(strings.TrimSpace(req.ReasonCode))
	if _, ok := allowedReportReasons[reasonCode]; !ok {
		return nil, ErrReportReasonInvalid
	}

	description := strings.TrimSpace(req.Description)
	if utf8.RuneCountInString(description) > reportDescriptionMaxRunes {
		return nil, ErrReportDescriptionLong
	}

	sourceSessionID := strings.TrimSpace(req.SourceSessionID)
	if sourceSessionID != "" {
		allowed, err := isHumanMemberOfSession(userID, sourceSessionID)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, ErrReportPermissionDenied
		}
	}

	reporterSnapshot, err := buildReporterSnapshot(userID)
	if err != nil {
		return nil, err
	}

	targetUserID := req.TargetUserID
	targetSessionID := strings.TrimSpace(req.TargetSessionID)
	targetSnapshot, err := buildReportTargetSnapshot(
		userID,
		targetType,
		targetUserID,
		targetSessionID,
	)
	if err != nil {
		return nil, err
	}

	assets, err := InspectReportAssets(userID, req.AssetKeys)
	if err != nil {
		return nil, err
	}

	reporterRaw, err := json.Marshal(reporterSnapshot)
	if err != nil {
		return nil, err
	}
	targetRaw, err := json.Marshal(targetSnapshot)
	if err != nil {
		return nil, err
	}

	var reportID int64
	err = store.DB.Transaction(func(tx *gorm.DB) error {
		report := model.Report{
			ReporterUserID:   userID,
			TargetType:       targetType,
			TargetUserID:     targetUserID,
			TargetSessionID:  targetSessionID,
			SourceSessionID:  sourceSessionID,
			ReasonCode:       reasonCode,
			Description:      description,
			Status:           model.ReportStatusPending,
			Resolution:       model.ReportResolutionUnset,
			ReporterSnapshot: datatypes.JSON(reporterRaw),
			TargetSnapshot:   datatypes.JSON(targetRaw),
		}
		if err := tx.Create(&report).Error; err != nil {
			return err
		}
		reportID = report.ID

		attachments := make([]model.ReportAttachment, 0, len(assets))
		for index, asset := range assets {
			attachments = append(attachments, model.ReportAttachment{
				ReportID:  report.ID,
				SlotNo:    int16(index + 1),
				ObjectKey: asset.ObjectKey,
				MimeType:  asset.MimeType,
				SizeBytes: asset.SizeBytes,
			})
		}
		if len(attachments) > 0 {
			if err := tx.Create(&attachments).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &CreateReportResp{
		ReportID: reportID,
		Status:   "pending",
	}, nil
}

func parseReportTargetType(raw string) (int16, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "user":
		return model.ReportTargetTypeUser, nil
	case "group":
		return model.ReportTargetTypeGroup, nil
	default:
		return 0, ErrReportTargetTypeInvalid
	}
}

func buildReporterSnapshot(userID int64) (map[string]any, error) {
	var user model.User
	if err := store.DB.Select("id", "username", "nickname", "avatar_url").
		First(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrReportPermissionDenied
		}
		return nil, err
	}

	return map[string]any{
		"user_id":    user.ID,
		"username":   strings.TrimSpace(user.Username),
		"nickname":   strings.TrimSpace(user.Nickname),
		"avatar_url": strings.TrimSpace(user.AvatarURL),
	}, nil
}

func buildReportTargetSnapshot(
	reporterUserID int64,
	targetType int16,
	targetUserID int64,
	targetSessionID string,
) (map[string]any, error) {
	switch targetType {
	case model.ReportTargetTypeUser:
		return buildUserReportTargetSnapshot(reporterUserID, targetUserID)
	case model.ReportTargetTypeGroup:
		return buildGroupReportTargetSnapshot(reporterUserID, targetSessionID)
	default:
		return nil, ErrReportTargetTypeInvalid
	}
}

func buildUserReportTargetSnapshot(reporterUserID, targetUserID int64) (map[string]any, error) {
	if targetUserID <= 0 {
		return nil, ErrReportTargetNotFound
	}
	if reporterUserID == targetUserID {
		return nil, ErrReportSelfNotAllowed
	}

	var user model.User
	if err := store.DB.Select("id", "username", "nickname", "avatar_url", "status").
		Where("id = ? AND status <> ?", targetUserID, model.UserStatusDeleted).
		First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrReportTargetNotFound
		}
		return nil, err
	}

	return map[string]any{
		"user_id":    user.ID,
		"username":   strings.TrimSpace(user.Username),
		"nickname":   strings.TrimSpace(user.Nickname),
		"avatar_url": strings.TrimSpace(user.AvatarURL),
		"status":     user.Status,
	}, nil
}

func buildGroupReportTargetSnapshot(reporterUserID int64, targetSessionID string) (map[string]any, error) {
	if targetSessionID == "" {
		return nil, ErrReportTargetNotFound
	}

	allowed, err := isHumanMemberOfSession(reporterUserID, targetSessionID)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrReportPermissionDenied
	}

	var session model.Session
	if err := store.DB.Select("session_id", "owner_id", "session_type", "group_name", "is_deleted").
		Where("session_id = ?", targetSessionID).
		First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrReportTargetNotFound
		}
		return nil, err
	}
	if session.IsDeleted || session.SessionType != model.SessionTypeGroup {
		return nil, ErrReportTargetNotFound
	}

	var memberCount int64
	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ?", targetSessionID).
		Count(&memberCount).Error; err != nil {
		return nil, err
	}

	return map[string]any{
		"session_id":   session.SessionID,
		"group_name":   strings.TrimSpace(session.GroupName),
		"owner_id":     fmt.Sprintf("%d", session.OwnerID),
		"member_count": memberCount,
	}, nil
}

func isHumanMemberOfSession(userID int64, sessionID string) (bool, error) {
	var count int64
	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, userID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
