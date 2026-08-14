package service

import (
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/datatypes"
)

func TestResolveReportBanUser(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	admin := createAdminFixture(t, testDB, 5001, "root", "Root", "RootPassword123A", model.AdminStatusActive)
	reporter := createUserFixture(t, testDB, 5002, "reporter", "reporter@example.com")
	target := createUserFixture(t, testDB, 5003, "target", "target@example.com")

	report := createReportFixture(t, testDB, func(r *model.Report) {
		r.ReporterUserID = reporter.ID
		r.TargetType = model.ReportTargetTypeUser
		r.TargetUserID = target.ID
		r.ReasonCode = "fraud"
		r.Description = "诱导转账"
		r.ReporterSnapshot = mustReportJSON(t, map[string]any{
			"user_id":  reporter.ID,
			"username": reporter.Username,
			"nickname": reporter.Nickname,
		})
		r.TargetSnapshot = mustReportJSON(t, map[string]any{
			"user_id":  target.ID,
			"username": target.Username,
			"nickname": target.Nickname,
		})
	})

	if err := ResolveReport(admin.ID, report.ID, ResolveReportInput{
		Action: "ban_user",
		Note:   "确认诈骗行为",
	}, "127.0.0.1", "test-agent"); err != nil {
		t.Fatalf("ResolveReport() error = %v", err)
	}

	var updatedReport model.Report
	if err := testDB.DB.First(&updatedReport, report.ID).Error; err != nil {
		t.Fatalf("load report: %v", err)
	}
	if updatedReport.Status != model.ReportStatusResolved {
		t.Fatalf("expected report resolved, got %d", updatedReport.Status)
	}
	if updatedReport.Resolution != model.ReportResolutionBanUser {
		t.Fatalf("expected ban_user resolution, got %d", updatedReport.Resolution)
	}
	if updatedReport.ResolvedNote != "确认诈骗行为" {
		t.Fatalf("expected resolved note written, got %q", updatedReport.ResolvedNote)
	}
	if updatedReport.ResolvedAdminID == nil || *updatedReport.ResolvedAdminID != admin.ID {
		t.Fatalf("expected resolved admin %d, got %#v", admin.ID, updatedReport.ResolvedAdminID)
	}

	var updatedUser model.User
	if err := testDB.DB.First(&updatedUser, target.ID).Error; err != nil {
		t.Fatalf("load banned user: %v", err)
	}
	if updatedUser.Status != model.UserStatusBanned {
		t.Fatalf("expected user banned, got %d", updatedUser.Status)
	}
	if updatedUser.BannedBy == nil || *updatedUser.BannedBy != admin.ID {
		t.Fatalf("expected banned_by=%d, got %#v", admin.ID, updatedUser.BannedBy)
	}

	var actionLogs []model.ReportActionLog
	if err := testDB.DB.Where("report_id = ?", report.ID).Find(&actionLogs).Error; err != nil {
		t.Fatalf("load report action logs: %v", err)
	}
	if len(actionLogs) != 1 {
		t.Fatalf("expected 1 report action log, got %d", len(actionLogs))
	}
	if actionLogs[0].Action != "resolve" {
		t.Fatalf("expected resolve action log, got %s", actionLogs[0].Action)
	}

	var userBanLog model.AdminOperationLog
	if err := testDB.DB.Where("action = ? AND target_id = ?", "user_ban", strconv.FormatInt(target.ID, 10)).
		Order("id DESC").
		First(&userBanLog).Error; err != nil {
		t.Fatalf("load user_ban log: %v", err)
	}

	var reportResolveLog model.AdminOperationLog
	if err := testDB.DB.Where("action = ? AND target_id = ?", "report_resolve", strconv.FormatInt(report.ID, 10)).
		Order("id DESC").
		First(&reportResolveLog).Error; err != nil {
		t.Fatalf("load report_resolve log: %v", err)
	}
}

func TestResolveReportBanGroup(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	store.RDB = testutil.NewMockRedis()

	admin := createAdminFixture(t, testDB, 5101, "root", "Root", "RootPassword123A", model.AdminStatusActive)
	reporter := createUserFixture(t, testDB, 5102, "group_reporter", "group_reporter@example.com")
	owner := createUserFixture(t, testDB, 5103, "group_owner", "group_owner@example.com")

	session := &model.Session{
		SessionID:         "group-report-session",
		OwnerID:           owner.ID,
		SessionType:       model.SessionTypeGroup,
		GroupName:         "Fraud Group",
		ModerationStatus:  model.SessionModerationStatusActive,
		AllowMemberInvite: true,
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if err := testDB.DB.Create(session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	report := createReportFixture(t, testDB, func(r *model.Report) {
		r.ReporterUserID = reporter.ID
		r.TargetType = model.ReportTargetTypeGroup
		r.TargetSessionID = session.SessionID
		r.SourceSessionID = session.SessionID
		r.ReasonCode = "spam"
		r.Description = "群里持续刷屏引流"
		r.ReporterSnapshot = mustReportJSON(t, map[string]any{
			"user_id":  reporter.ID,
			"username": reporter.Username,
			"nickname": reporter.Nickname,
		})
		r.TargetSnapshot = mustReportJSON(t, map[string]any{
			"session_id":   session.SessionID,
			"group_name":   session.GroupName,
			"owner_id":     owner.ID,
			"member_count": 2,
		})
	})

	if err := ResolveReport(admin.ID, report.ID, ResolveReportInput{
		Action: "ban_group",
		Note:   "确认违规引流",
	}, "127.0.0.1", "test-agent"); err != nil {
		t.Fatalf("ResolveReport() error = %v", err)
	}

	var updatedReport model.Report
	if err := testDB.DB.First(&updatedReport, report.ID).Error; err != nil {
		t.Fatalf("load report: %v", err)
	}
	if updatedReport.Resolution != model.ReportResolutionBanGroup {
		t.Fatalf("expected ban_group resolution, got %d", updatedReport.Resolution)
	}

	var updatedSession model.Session
	if err := testDB.DB.Where("session_id = ?", session.SessionID).First(&updatedSession).Error; err != nil {
		t.Fatalf("load session: %v", err)
	}
	if updatedSession.ModerationStatus != model.SessionModerationStatusBanned {
		t.Fatalf("expected session banned, got %d", updatedSession.ModerationStatus)
	}
	if updatedSession.BannedBy == nil || *updatedSession.BannedBy != admin.ID {
		t.Fatalf("expected session banned_by=%d, got %#v", admin.ID, updatedSession.BannedBy)
	}
	expectedReason := "report:" + strconv.FormatInt(report.ID, 10)
	if updatedSession.BannedReason != expectedReason {
		t.Fatalf("expected session banned reason %s, got %q", expectedReason, updatedSession.BannedReason)
	}

	var groupBanLog model.AdminOperationLog
	if err := testDB.DB.Where("action = ? AND target_id = ?", "group_ban", session.SessionID).
		Order("id DESC").
		First(&groupBanLog).Error; err != nil {
		t.Fatalf("load group_ban log: %v", err)
	}
}

func TestResolveReportRejectsAlreadyResolved(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	admin := createAdminFixture(t, testDB, 5201, "root", "Root", "RootPassword123A", model.AdminStatusActive)
	reporter := createUserFixture(t, testDB, 5202, "resolved_reporter", "resolved_reporter@example.com")
	target := createUserFixture(t, testDB, 5203, "resolved_target", "resolved_target@example.com")

	resolvedAt := time.Now().UTC()
	createReportFixture(t, testDB, func(r *model.Report) {
		r.ID = 99
		r.ReporterUserID = reporter.ID
		r.TargetType = model.ReportTargetTypeUser
		r.TargetUserID = target.ID
		r.Status = model.ReportStatusResolved
		r.Resolution = model.ReportResolutionReject
		r.ResolvedNote = "already handled"
		r.ResolvedAdminID = &admin.ID
		r.AssignedAdminID = &admin.ID
		r.ResolvedAt = &resolvedAt
		r.ReporterSnapshot = mustReportJSON(t, map[string]any{"user_id": reporter.ID})
		r.TargetSnapshot = mustReportJSON(t, map[string]any{"user_id": target.ID})
	})

	err := ResolveReport(admin.ID, 99, ResolveReportInput{
		Action: "reject",
		Note:   "duplicate attempt",
	}, "127.0.0.1", "test-agent")
	if !errors.Is(err, ErrReportAlreadyResolved) {
		t.Fatalf("expected ErrReportAlreadyResolved, got %v", err)
	}
}

func TestListReportsClampsPageToLastPage(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	createReportFixture(t, testDB, func(r *model.Report) { r.ID = 6101 })
	createReportFixture(t, testDB, func(r *model.Report) { r.ID = 6102 })
	createReportFixture(t, testDB, func(r *model.Report) { r.ID = 6103 })

	result, err := ListReports(ReportListParams{
		Page:     9,
		PageSize: 2,
	})
	if err != nil {
		t.Fatalf("ListReports() error = %v", err)
	}
	if result.Page != 2 {
		t.Fatalf("expected clamped page=2, got %d", result.Page)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected 1 item on last page, got %d", len(result.Items))
	}
}

func createReportFixture(t *testing.T, db *testutil.TestDB, overrides ...func(*model.Report)) *model.Report {
	t.Helper()

	report := &model.Report{
		ReporterUserID:   1,
		TargetType:       model.ReportTargetTypeUser,
		ReasonCode:       "other",
		Status:           model.ReportStatusPending,
		Resolution:       model.ReportResolutionUnset,
		ReporterSnapshot: mustReportJSON(t, map[string]any{}),
		TargetSnapshot:   mustReportJSON(t, map[string]any{}),
	}
	for _, override := range overrides {
		override(report)
	}
	if err := db.DB.Create(report).Error; err != nil {
		t.Fatalf("create report fixture: %v", err)
	}
	return report
}

func mustReportJSON(t *testing.T, value any) datatypes.JSON {
	t.Helper()

	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal report json: %v", err)
	}
	return datatypes.JSON(raw)
}
