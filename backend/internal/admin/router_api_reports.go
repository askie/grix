package admin

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	adminmiddleware "github.com/askie/grix/backend/internal/admin/middleware"
	adminservice "github.com/askie/grix/backend/internal/admin/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// registerReportAPIRoutes 注册举报管理相关 JSON 接口。
func registerReportAPIRoutes(g *gin.RouterGroup) {
	g.GET("/reports", apiListReports)
	g.GET("/reports/:id", apiReportDetail)
	g.POST("/reports/:id/resolve", apiResolveReport)
	g.GET("/reports/:id/attachments/:attachmentID", apiReportAttachmentURL)
}

func apiListReports(c *gin.Context) {
	var status int16
	switch strings.TrimSpace(c.Query("status")) {
	case "pending":
		status = model.ReportStatusPending
	case "review":
		status = model.ReportStatusReview
	case "resolved":
		status = model.ReportStatusResolved
	}

	var targetType int16
	switch strings.TrimSpace(c.Query("target_type")) {
	case "user":
		targetType = model.ReportTargetTypeUser
	case "group":
		targetType = model.ReportTargetTypeGroup
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	result, err := adminservice.ListReports(adminservice.ReportListParams{
		Query:      c.Query("q"),
		Status:     status,
		TargetType: targetType,
		ReasonCode: c.Query("reason_code"),
		Page:       page,
		PageSize:   pageSize,
	})
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}

	response.OK(c, gin.H{
		"items":     result.Items,
		"total":     result.Total,
		"page":      result.Page,
		"page_size": result.PageSize,
	})
}

func apiReportDetail(c *gin.Context) {
	reportID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "无效举报ID")
		return
	}

	detail, err := adminservice.GetReportDetail(reportID)
	if err != nil {
		if errors.Is(err, adminservice.ErrReportNotFound) {
			response.Fail(c, http.StatusNotFound, 10005, err.Error())
			return
		}
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}

	response.OK(c, gin.H{"report": reportDetailToJSON(detail)})
}

func apiResolveReport(c *gin.Context) {
	reportID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "无效举报ID")
		return
	}

	var body struct {
		Action string `json:"action"`
		Note   string `json:"note"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "参数错误")
		return
	}

	admin := adminmiddleware.CurrentAdmin(c)
	err = adminservice.ResolveReport(admin.ID, reportID, adminservice.ResolveReportInput{
		Action: body.Action,
		Note:   body.Note,
	}, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		if errors.Is(err, adminservice.ErrReportNotFound) {
			response.Fail(c, http.StatusNotFound, 10005, err.Error())
			return
		}
		response.Fail(c, http.StatusBadRequest, 10006, err.Error())
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func apiReportAttachmentURL(c *gin.Context) {
	reportID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "无效举报ID")
		return
	}
	attachmentID, err := strconv.ParseInt(c.Param("attachmentID"), 10, 64)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, 10002, "无效附件ID")
		return
	}

	url, err := adminservice.GetReportAttachmentViewURL(reportID, attachmentID)
	if err != nil {
		if errors.Is(err, adminservice.ErrReportAttachmentNotFound) {
			response.Fail(c, http.StatusNotFound, 10005, err.Error())
			return
		}
		response.Fail(c, http.StatusInternalServerError, 10004, err.Error())
		return
	}
	response.OK(c, gin.H{"url": url})
}

// reportDetailToJSON 将 ReportDetail（无 json tag）显式映射为统一 snake_case 结构，
// 避免改动 service 层结构体即可给出稳定的 API 契约。
func reportDetailToJSON(d *adminservice.ReportDetail) gin.H {
	attachments := make([]gin.H, 0, len(d.Attachments))
	for _, a := range d.Attachments {
		attachments = append(attachments, gin.H{
			"id":         strconv.FormatInt(a.ID, 10),
			"slot_no":    a.SlotNo,
			"mime_type":  a.MimeType,
			"size_bytes": a.SizeBytes,
		})
	}

	logs := make([]gin.H, 0, len(d.ActionLogs))
	for _, l := range d.ActionLogs {
		logs = append(logs, gin.H{
			"action_text":     l.ActionText,
			"resolution_text": l.ResolutionText,
			"note":            l.Note,
			"admin_name":      l.AdminName,
			"created_at":      l.CreatedAt,
		})
	}

	return gin.H{
		"id":                strconv.FormatInt(d.ID, 10),
		"status":            d.Status,
		"status_text":       d.StatusText,
		"resolution":        d.Resolution,
		"resolution_text":   d.ResolutionText,
		"target_type":       d.TargetType,
		"target_type_text":  d.TargetTypeText,
		"reason_code":       d.ReasonCode,
		"reason_text":       d.ReasonText,
		"description":       d.Description,
		"source_session_id": d.SourceSessionID,
		"reporter": gin.H{
			"user_id":      d.Reporter.UserID,
			"username":     d.Reporter.Username,
			"nickname":     d.Reporter.Nickname,
			"avatar_url":   d.Reporter.AvatarURL,
			"display_name": d.Reporter.DisplayName,
		},
		"target": gin.H{
			"user_id":      d.Target.UserID,
			"username":     d.Target.Username,
			"session_id":   d.Target.SessionID,
			"title":        d.Target.Title,
			"subtitle":     d.Target.Subtitle,
			"avatar_url":   d.Target.AvatarURL,
			"owner_id":     d.Target.OwnerID,
			"member_count": d.Target.MemberCount,
		},
		"attachments":     attachments,
		"action_logs":     logs,
		"resolved_note":   d.ResolvedNote,
		"assigned_admin":  d.AssignedAdmin,
		"resolved_admin":  d.ResolvedAdmin,
		"created_at":      d.CreatedAt,
		"resolved_at":     d.ResolvedAt,
		"is_user_target":  d.IsUserTarget,
		"is_group_target": d.IsGroupTarget,
		"can_resolve":     d.CanResolve,
		"can_ban_user":    d.CanBanUser,
		"can_ban_group":   d.CanBanGroup,
	}
}
