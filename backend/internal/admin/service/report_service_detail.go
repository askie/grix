package service

import (
	"errors"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

func GetReportDetail(reportID int64) (*ReportDetail, error) {
	var report model.Report
	if err := store.DB.First(&report, reportID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrReportNotFound
		}
		return nil, err
	}

	var attachments []model.ReportAttachment
	if err := store.DB.Where("report_id = ?", reportID).
		Order("slot_no ASC").
		Find(&attachments).Error; err != nil {
		return nil, err
	}

	var actionLogs []model.ReportActionLog
	if err := store.DB.Where("report_id = ?", reportID).
		Order("created_at ASC").
		Find(&actionLogs).Error; err != nil {
		return nil, err
	}

	adminIDs := make(map[int64]struct{})
	if report.AssignedAdminID != nil {
		adminIDs[*report.AssignedAdminID] = struct{}{}
	}
	if report.ResolvedAdminID != nil {
		adminIDs[*report.ResolvedAdminID] = struct{}{}
	}
	for _, item := range actionLogs {
		if item.AdminID > 0 {
			adminIDs[item.AdminID] = struct{}{}
		}
	}

	adminNameByID, err := loadAdminNames(adminIDs)
	if err != nil {
		return nil, err
	}

	reporterSnapshot := parseSnapshot(report.ReporterSnapshot)
	targetSnapshot := parseSnapshot(report.TargetSnapshot)

	detail := &ReportDetail{
		ID:              report.ID,
		Status:          report.Status,
		StatusText:      reportStatusText(report.Status),
		Resolution:      report.Resolution,
		ResolutionText:  reportResolutionText(report.Resolution),
		TargetType:      report.TargetType,
		TargetTypeText:  reportTargetTypeText(report.TargetType),
		ReasonCode:      report.ReasonCode,
		ReasonText:      reportReasonText(report.ReasonCode),
		Description:     report.Description,
		SourceSessionID: strings.TrimSpace(report.SourceSessionID),
		Reporter:        buildReporterView(reporterSnapshot),
		Target:          buildTargetView(targetSnapshot, report.TargetType),
		ResolvedNote:    strings.TrimSpace(report.ResolvedNote),
		CreatedAt:       report.CreatedAt,
		ResolvedAt:      report.ResolvedAt,
		IsUserTarget:    report.TargetType == model.ReportTargetTypeUser,
		IsGroupTarget:   report.TargetType == model.ReportTargetTypeGroup,
		CanResolve:      report.Status != model.ReportStatusResolved,
		CanBanUser: report.Status != model.ReportStatusResolved &&
			report.TargetType == model.ReportTargetTypeUser,
		CanBanGroup: report.Status != model.ReportStatusResolved &&
			report.TargetType == model.ReportTargetTypeGroup,
	}
	if report.AssignedAdminID != nil {
		detail.AssignedAdmin = adminNameByID[*report.AssignedAdminID]
	}
	if report.ResolvedAdminID != nil {
		detail.ResolvedAdmin = adminNameByID[*report.ResolvedAdminID]
	}

	detail.Attachments = make([]ReportAttachmentView, 0, len(attachments))
	for _, item := range attachments {
		detail.Attachments = append(detail.Attachments, ReportAttachmentView{
			ID:        item.ID,
			SlotNo:    item.SlotNo,
			MimeType:  item.MimeType,
			SizeBytes: item.SizeBytes,
		})
	}

	detail.ActionLogs = make([]ReportActionLogView, 0, len(actionLogs))
	for _, item := range actionLogs {
		resolutionText, note := parseActionLogDetail(item.Detail)
		detail.ActionLogs = append(detail.ActionLogs, ReportActionLogView{
			ActionText:     reportActionText(item.Action),
			ResolutionText: resolutionText,
			Note:           note,
			AdminName:      adminNameByID[item.AdminID],
			CreatedAt:      item.CreatedAt,
		})
	}

	return detail, nil
}
