package service

import (
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

func ListReports(params ReportListParams) (*ReportListResult, error) {
	page := params.Page
	if page < 1 {
		page = 1
	}
	pageSize := params.PageSize
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}

	query := store.DB.Model(&model.Report{})
	keyword := strings.TrimSpace(params.Query)
	if keyword != "" {
		like := "%" + strings.ToLower(keyword) + "%"
		query = query.Where(
			"CAST(id AS TEXT) = ? OR CAST(reporter_user_id AS TEXT) = ? OR CAST(target_user_id AS TEXT) = ? OR target_session_id = ? OR source_session_id = ? OR LOWER(reason_code) LIKE ? OR LOWER(description) LIKE ?",
			keyword,
			keyword,
			keyword,
			keyword,
			keyword,
			like,
			like,
		)
	}
	if params.Status > 0 {
		query = query.Where("status = ?", params.Status)
	}
	if params.TargetType > 0 {
		query = query.Where("target_type = ?", params.TargetType)
	}
	if reasonCode := strings.ToLower(strings.TrimSpace(params.ReasonCode)); reasonCode != "" {
		query = query.Where("reason_code = ?", reasonCode)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
	if totalPages == 0 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}

	var reports []model.Report
	if err := query.Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&reports).Error; err != nil {
		return nil, err
	}

	items := make([]ReportListItem, 0, len(reports))
	for _, report := range reports {
		reporterSnapshot := parseSnapshot(report.ReporterSnapshot)
		targetSnapshot := parseSnapshot(report.TargetSnapshot)
		targetTitle, targetInfo := buildReportListTarget(targetSnapshot, report.TargetType)
		reporterName, reporterInfo := buildReportListReporter(reporterSnapshot)

		items = append(items, ReportListItem{
			ID:             report.ID,
			Status:         report.Status,
			StatusText:     reportStatusText(report.Status),
			TargetType:     report.TargetType,
			TargetTypeText: reportTargetTypeText(report.TargetType),
			ReasonCode:     report.ReasonCode,
			ReasonText:     reportReasonText(report.ReasonCode),
			ReporterName:   reporterName,
			ReporterInfo:   reporterInfo,
			TargetTitle:    targetTitle,
			TargetInfo:     targetInfo,
			CreatedAt:      report.CreatedAt,
			ResolvedAt:     report.ResolvedAt,
		})
	}

	return &ReportListResult{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}
