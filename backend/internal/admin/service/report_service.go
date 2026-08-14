package service

import (
	"errors"
	"time"
)

const reportResolveNoteMaxRunes = 500

var (
	ErrReportNotFound             = errors.New("举报不存在")
	ErrReportAlreadyResolved      = errors.New("举报已处理")
	ErrReportResolveActionInvalid = errors.New("处理动作无效")
	ErrReportResolveNoteRequired  = errors.New("处理备注不能为空")
	ErrReportResolveNoteTooLong   = errors.New("处理备注长度不能超过 500 个字符")
	ErrReportResolveTargetInvalid = errors.New("举报目标与处理动作不匹配")
)

type ReportListParams struct {
	Query      string
	Status     int16
	TargetType int16
	ReasonCode string
	Page       int
	PageSize   int
}

type ReportListItem struct {
	ID             int64      `json:"id,string"`
	Status         int16      `json:"status"`
	StatusText     string     `json:"status_text"`
	TargetType     int16      `json:"target_type"`
	TargetTypeText string     `json:"target_type_text"`
	ReasonCode     string     `json:"reason_code"`
	ReasonText     string     `json:"reason_text"`
	ReporterName   string     `json:"reporter_name"`
	ReporterInfo   string     `json:"reporter_info"`
	TargetTitle    string     `json:"target_title"`
	TargetInfo     string     `json:"target_info"`
	CreatedAt      time.Time  `json:"created_at"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
}

type ReportListResult struct {
	Items    []ReportListItem
	Total    int64
	Page     int
	PageSize int
}

type ReportPersonView struct {
	UserID      string
	Username    string
	Nickname    string
	AvatarURL   string
	DisplayName string
}

type ReportTargetView struct {
	UserID      string
	Username    string
	SessionID   string
	Title       string
	Subtitle    string
	AvatarURL   string
	OwnerID     string
	MemberCount int64
}

type ReportAttachmentView struct {
	ID        int64
	SlotNo    int16
	MimeType  string
	SizeBytes int64
}

type ReportActionLogView struct {
	ActionText     string
	ResolutionText string
	Note           string
	AdminName      string
	CreatedAt      time.Time
}

type ReportDetail struct {
	ID              int64
	Status          int16
	StatusText      string
	Resolution      int16
	ResolutionText  string
	TargetType      int16
	TargetTypeText  string
	ReasonCode      string
	ReasonText      string
	Description     string
	SourceSessionID string
	Reporter        ReportPersonView
	Target          ReportTargetView
	Attachments     []ReportAttachmentView
	ActionLogs      []ReportActionLogView
	ResolvedNote    string
	AssignedAdmin   string
	ResolvedAdmin   string
	CreatedAt       time.Time
	ResolvedAt      *time.Time
	IsUserTarget    bool
	IsGroupTarget   bool
	CanResolve      bool
	CanBanUser      bool
	CanBanGroup     bool
}

type ResolveReportInput struct {
	Action string
	Note   string
}
