package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type VisitorBanListParams struct {
	Query    string
	Status   int16
	Page     int
	PageSize int
}

type VisitorBanListItem struct {
	ID               int64     `json:"id,string"`
	SiteID           int64     `json:"site_id,string"`
	SiteName         string    `json:"site_name"`
	SiteKey          string    `json:"site_key"`
	OwnerUserID      int64     `json:"owner_user_id,string"`
	OwnerUsername    string    `json:"owner_username"`
	OwnerNickname    string    `json:"owner_nickname"`
	VisitorID        int64     `json:"visitor_id,string"`
	VisitorKey       string    `json:"visitor_key"`
	VisitorName      string    `json:"visitor_name"`
	VisitorEmail     string    `json:"visitor_email"`
	SessionID        string    `json:"session_id"`
	LastPageURL      string    `json:"last_page_url"`
	LastInitIPPrefix string    `json:"last_init_ip_prefix"`
	Status           int16     `json:"status"`
	HasIPBan         bool      `json:"has_ip_ban"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	LastActiveAt     time.Time `json:"last_active_at"`
	LastInitAt       time.Time `json:"last_init_at"`
}

type VisitorBanListResult struct {
	Items    []VisitorBanListItem
	Total    int64
	Page     int
	PageSize int
}

var ErrVisitorBanNotFound = errors.New("访客封禁记录不存在")

func ListVisitorBans(params VisitorBanListParams) (*VisitorBanListResult, error) {
	page := params.Page
	if page < 1 {
		page = 1
	}
	pageSize := params.PageSize
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}

	query := store.DB.Table("widget_sessions AS ws").
		Joins("LEFT JOIN widget_sites AS site ON site.id = ws.site_id").
		Joins("LEFT JOIN users AS owner ON owner.id = ws.owner_user_id")
	if params.Status > 0 {
		query = query.Where("ws.status = ?", params.Status)
	}
	keyword := strings.TrimSpace(params.Query)
	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where(
			"CAST(ws.id AS TEXT) = ? OR CAST(ws.visitor_id AS TEXT) = ? OR CAST(ws.owner_user_id AS TEXT) = ? OR ws.session_id = ? OR LOWER(ws.visitor_key) LIKE LOWER(?) OR LOWER(ws.visitor_name) LIKE LOWER(?) OR LOWER(ws.visitor_email) LIKE LOWER(?) OR LOWER(ws.last_page_url) LIKE LOWER(?) OR LOWER(site.site_name) LIKE LOWER(?) OR LOWER(owner.username) LIKE LOWER(?) OR LOWER(owner.nickname) LIKE LOWER(?)",
			keyword,
			keyword,
			keyword,
			keyword,
			like,
			like,
			like,
			like,
			like,
			like,
			like,
		)
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

	var items []VisitorBanListItem
	if err := query.Select(`
			ws.id,
			ws.site_id,
			COALESCE(site.site_name, '') AS site_name,
			COALESCE(site.site_key, '') AS site_key,
			ws.owner_user_id,
			COALESCE(owner.username, '') AS owner_username,
			COALESCE(owner.nickname, '') AS owner_nickname,
			ws.visitor_id,
			ws.visitor_key,
			ws.visitor_name,
			ws.visitor_email,
			ws.session_id,
			ws.last_page_url,
			ws.last_init_ip_prefix,
			ws.status,
			EXISTS (
				SELECT 1 FROM widget_ip_bans AS ipb
				WHERE ipb.owner_user_id = ws.owner_user_id
					AND ipb.source_session_id = ws.session_id
			) AS has_ip_ban,
			ws.created_at,
			ws.updated_at,
			ws.last_active_at,
			ws.last_init_at
		`).
		Order("ws.updated_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Scan(&items).Error; err != nil {
		return nil, err
	}

	return &VisitorBanListResult{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func UnbanWidgetVisitor(adminID int64, sessionID, clientIP, userAgent string) error {
	normalizedSessionID := strings.TrimSpace(sessionID)
	if adminID <= 0 || normalizedSessionID == "" {
		return ErrVisitorBanNotFound
	}

	var ownerUserID int64
	var deletedIPBans int64
	err := store.DB.Transaction(func(tx *gorm.DB) error {
		var session model.WidgetSession
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("session_id = ?", normalizedSessionID).
			First(&session).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrVisitorBanNotFound
			}
			return err
		}
		if session.Status != model.WidgetSessionStatusBanned {
			return ErrVisitorBanNotFound
		}
		ownerUserID = session.OwnerUserID

		var bannedSessions []model.WidgetSession
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("owner_user_id = ? AND site_id = ? AND visitor_key = ? AND status = ?",
				session.OwnerUserID,
				session.SiteID,
				session.VisitorKey,
				model.WidgetSessionStatusBanned,
			).
			Find(&bannedSessions).Error; err != nil {
			return err
		}
		if len(bannedSessions) == 0 {
			return ErrVisitorBanNotFound
		}
		sourceSessionIDs := make([]string, 0, len(bannedSessions))
		for _, item := range bannedSessions {
			sourceSessionIDs = append(sourceSessionIDs, item.SessionID)
		}

		now := time.Now().UTC()
		if err := tx.Model(&model.WidgetSession{}).
			Where("owner_user_id = ? AND site_id = ? AND visitor_key = ? AND status = ?",
				session.OwnerUserID,
				session.SiteID,
				session.VisitorKey,
				model.WidgetSessionStatusBanned,
			).
			Updates(map[string]any{
				"status":     model.WidgetSessionStatusClosed,
				"updated_at": now,
			}).Error; err != nil {
			return err
		}

		res := tx.Where("owner_user_id = ? AND source_session_id IN ?", session.OwnerUserID, sourceSessionIDs).
			Delete(&model.WidgetIPBan{})
		if res.Error != nil {
			return res.Error
		}
		deletedIPBans = res.RowsAffected

		return recordOperationTx(tx, adminID, "widget_visitor_unban", "widget_session", normalizedSessionID, map[string]any{
			"owner_user_id":   fmt.Sprintf("%d", session.OwnerUserID),
			"site_id":         fmt.Sprintf("%d", session.SiteID),
			"visitor_key":     session.VisitorKey,
			"session_count":   len(bannedSessions),
			"deleted_ip_bans": deletedIPBans,
		}, clientIP, userAgent)
	})
	if err != nil {
		return err
	}
	if deletedIPBans > 0 {
		security.InvalidateWidgetIPBanCache(ownerUserID)
	}
	return nil
}
