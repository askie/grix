package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func ResolveReport(adminID, reportID int64, input ResolveReportInput, clientIP, userAgent string) error {
	action := strings.ToLower(strings.TrimSpace(input.Action))
	resolution, err := parseReportResolutionAction(action)
	if err != nil {
		return err
	}

	note := strings.TrimSpace(input.Note)
	if note == "" {
		return ErrReportResolveNoteRequired
	}
	if utf8.RuneCountInString(note) > reportResolveNoteMaxRunes {
		return ErrReportResolveNoteTooLong
	}

	now := time.Now().UTC()
	var kickUserID int64
	var bannedGroupSessionID string

	err = store.DB.Transaction(func(tx *gorm.DB) error {
		var report model.Report
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&report, reportID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrReportNotFound
			}
			return err
		}
		if report.Status == model.ReportStatusResolved {
			return ErrReportAlreadyResolved
		}

		switch resolution {
		case model.ReportResolutionReject, model.ReportResolutionNoAction, model.ReportResolutionDuplicate:
		case model.ReportResolutionBanUser:
			if report.TargetType != model.ReportTargetTypeUser || report.TargetUserID <= 0 {
				return ErrReportResolveTargetInvalid
			}
			banned, err := banUserTx(
				tx,
				adminID,
				report.TargetUserID,
				fmt.Sprintf("report:%d", report.ID),
				now,
				clientIP,
				userAgent,
			)
			if err != nil {
				return err
			}
			if banned {
				kickUserID = report.TargetUserID
			}
		case model.ReportResolutionBanGroup:
			if report.TargetType != model.ReportTargetTypeGroup || strings.TrimSpace(report.TargetSessionID) == "" {
				return ErrReportResolveTargetInvalid
			}
			banned, err := banGroupTx(
				tx,
				adminID,
				report.TargetSessionID,
				fmt.Sprintf("report:%d", report.ID),
				now,
				clientIP,
				userAgent,
			)
			if err != nil {
				return err
			}
			if banned {
				bannedGroupSessionID = report.TargetSessionID
			}
		default:
			return ErrReportResolveActionInvalid
		}

		if err := tx.Model(&model.Report{}).
			Where("id = ?", report.ID).
			Updates(map[string]any{
				"status":            model.ReportStatusResolved,
				"resolution":        resolution,
				"assigned_admin_id": adminID,
				"resolved_admin_id": adminID,
				"resolved_note":     note,
				"resolved_at":       now,
				"updated_at":        now,
			}).Error; err != nil {
			return err
		}

		actionRaw, err := json.Marshal(map[string]any{
			"resolution": action,
			"note":       note,
		})
		if err != nil {
			return err
		}
		if err := tx.Create(&model.ReportActionLog{
			ReportID: report.ID,
			AdminID:  adminID,
			Action:   "resolve",
			Detail:   datatypes.JSON(actionRaw),
		}).Error; err != nil {
			return err
		}

		return recordOperationTx(tx, adminID, "report_resolve", "report", fmt.Sprintf("%d", report.ID), map[string]any{
			"resolution": action,
			"note":       note,
		}, clientIP, userAgent)
	})
	if err != nil {
		return err
	}

	if kickUserID > 0 {
		return publishKickUser(context.Background(), kickUserID, "user_disabled")
	}
	if bannedGroupSessionID != "" {
		return notifyGroupAccessRevoked(bannedGroupSessionID)
	}
	return nil
}

func parseReportResolutionAction(action string) (int16, error) {
	switch action {
	case "reject":
		return model.ReportResolutionReject, nil
	case "ban_user":
		return model.ReportResolutionBanUser, nil
	case "ban_group":
		return model.ReportResolutionBanGroup, nil
	case "no_action":
		return model.ReportResolutionNoAction, nil
	case "duplicate":
		return model.ReportResolutionDuplicate, nil
	default:
		return 0, ErrReportResolveActionInvalid
	}
}
