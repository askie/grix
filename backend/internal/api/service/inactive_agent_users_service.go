package service

import (
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

// InactiveAgentReachEventKey 是「沉默用户触达」走触达链路时的 event_key。
const InactiveAgentReachEventKey = "inactive_agent_marketing"

const (
	// inactiveAgentDefaultDays 是「多久没连过 agent 才算沉默」的默认天数。
	inactiveAgentDefaultDays = 14
	// inactiveAgentMaxDays 兜住离谱入参（cutoff 早于建站时间等于全量用户）。
	inactiveAgentMaxDays = 365
)

type ListInactiveAgentUsersReq struct {
	NoAgentDays int
	Region      string
	Page        int
	PageSize    int
}

// InactiveAgentUser 是一名近 N 天没有任何 agent 连接过的用户。
// 手机号只给末四位脱敏串，本期不发短信，仅供后台辨认账号。
type InactiveAgentUser struct {
	UserID               int64  `json:"user_id,string"`
	Nickname             string `json:"nickname"`
	Email                string `json:"email"`
	PhoneMasked          string `json:"phone_masked"`
	AgentTotal           int    `json:"agent_total"`
	CreatedAt            string `json:"created_at"`
	LastAgentConnectedAt string `json:"last_agent_connected_at"`
}

type ListInactiveAgentUsersResult struct {
	Total int64               `json:"total"`
	Users []InactiveAgentUser `json:"users"`
	// NoAgentDays / Page / PageSize 是 clamp 之后真正生效的值，供接口如实回显。
	NoAgentDays int `json:"no_agent_days"`
	Page        int `json:"page"`
	PageSize    int `json:"page_size"`
	// DefaultEmailTemplateID 是当前配置生效的阿里云模板 ID，供后台预填「按次覆盖」输入框，
	// 避免前端硬编码模板号。
	DefaultEmailTemplateID int `json:"default_email_template_id"`
}

// inactiveAgentUserRow 承接联表查询结果；手机号读末四位与旧明文列，脱敏后再下发。
type inactiveAgentUserRow struct {
	UserID               int64
	Nickname             string
	Email                string
	PhoneE164            string
	PhoneLast4           string
	CreatedAt            time.Time
	AgentTotal           int
	LastAgentConnectedAt *time.Time
}

// ListInactiveAgentUsers 查出近 NoAgentDays 天内没有任何 agent 连接过的活跃用户。
//
// 口径：agent_connection_logs 按 owner_id 反查，NOT EXISTS 天然覆盖「从没建过 agent」的人；
// AgentTotal 只数未删除的 agent，用来在后台区分「建了不用」和「压根没建」。
func ListInactiveAgentUsers(req ListInactiveAgentUsersReq) (*ListInactiveAgentUsersResult, *errcode.ErrCode) {
	days := req.NoAgentDays
	if days <= 0 {
		days = inactiveAgentDefaultDays
	}
	if days > inactiveAgentMaxDays {
		days = inactiveAgentMaxDays
	}
	region := strings.ToLower(strings.TrimSpace(req.Region))
	switch region {
	case "", "cn", "global":
	default:
		return nil, &errcode.ErrBadRequest
	}
	page, pageSize := req.Page, req.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -days)

	// Count 与 Find 各自从零建一次查询：GORM 的 builder 有状态（Count 会改写 Selects），
	// 复用同一个 *gorm.DB 会让第二次查询串味。
	newQuery := func() *gorm.DB {
		q := store.DB.Table("users").
			Where("users.status = ?", model.UserStatusActive).
			Where("NOT EXISTS (SELECT 1 FROM agent_connection_logs l WHERE l.owner_id = users.id AND l.connected_at >= ?)", cutoff)
		if region != "" {
			q = q.Where("users.region = ?", region)
		}
		return q
	}

	var total int64
	if err := newQuery().Count(&total).Error; err != nil {
		return nil, &errcode.ErrInternal
	}

	var rows []inactiveAgentUserRow
	if err := newQuery().
		Select(`users.id AS user_id, users.nickname, users.email, users.phone_e164, users.phone_last4, users.created_at,
			(SELECT COUNT(*) FROM agents a WHERE a.owner_id = users.id AND a.status <> ?) AS agent_total,
			(SELECT l2.connected_at FROM agent_connection_logs l2 WHERE l2.owner_id = users.id
				ORDER BY l2.connected_at DESC LIMIT 1) AS last_agent_connected_at`,
			model.AgentStatusDeleted).
		Order("users.created_at DESC, users.id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&rows).Error; err != nil {
		return nil, &errcode.ErrInternal
	}

	users := make([]InactiveAgentUser, 0, len(rows))
	for _, r := range rows {
		lastConnected := ""
		if r.LastAgentConnectedAt != nil {
			lastConnected = r.LastAgentConnectedAt.UTC().Format(time.RFC3339)
		}
		users = append(users, InactiveAgentUser{
			UserID:               r.UserID,
			Nickname:             r.Nickname,
			Email:                r.Email,
			PhoneMasked:          MaskUserPhone(model.User{PhoneE164: r.PhoneE164, PhoneLast4: r.PhoneLast4}),
			AgentTotal:           r.AgentTotal,
			CreatedAt:            r.CreatedAt.UTC().Format(time.RFC3339),
			LastAgentConnectedAt: lastConnected,
		})
	}

	return &ListInactiveAgentUsersResult{
		Total:                  total,
		Users:                  users,
		NoAgentDays:            days,
		Page:                   page,
		PageSize:               pageSize,
		DefaultEmailTemplateID: ReachEmailTemplateID(),
	}, nil
}
