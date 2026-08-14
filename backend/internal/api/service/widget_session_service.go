package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/gorm"
)

type WidgetSessionDTO struct {
	ID           int64  `json:"id,string"`
	SiteID       int64  `json:"site_id,string"`
	SessionID    string `json:"session_id"`
	VisitorID    int64  `json:"visitor_id,string"`
	VisitorKey   string `json:"visitor_key"`
	VisitorName  string `json:"visitor_name"`
	VisitorEmail string `json:"visitor_email"`
	LastPageURL  string `json:"last_page_url"`
	Status       int16  `json:"status"`
	LastActiveAt int64  `json:"last_active_at"`
	UpdatedAt    int64  `json:"updated_at"`
}

type WidgetSessionListInput struct {
	OwnerUserID int64
	SiteID      int64
	Status      int16
	Limit       int
	Offset      int
}

type WidgetSessionListResp struct {
	Items  []WidgetSessionDTO `json:"items"`
	Total  int64              `json:"total"`
	Limit  int                `json:"limit"`
	Offset int                `json:"offset"`
}

type WidgetSessionStatusUpdateInput struct {
	OwnerUserID int64
	SessionID   string
	Status      int16
}

const (
	widgetSessionStatusAll    int16 = 0
	widgetSessionDefaultLimit       = 20
	widgetSessionMaxLimit           = 100
)

var ErrWidgetSessionNotOwned = errors.New("widget session not found or forbidden")

func WidgetSessionList(in WidgetSessionListInput) (*WidgetSessionListResp, error) {
	if in.OwnerUserID <= 0 {
		return nil, ErrWidgetSiteInvalidInput
	}
	if in.SiteID < 0 {
		return nil, ErrWidgetSiteInvalidInput
	}
	if in.Status != widgetSessionStatusAll && !isWidgetSessionStatusValid(in.Status) {
		return nil, ErrWidgetSiteInvalidInput
	}
	limit := in.Limit
	if limit <= 0 {
		limit = widgetSessionDefaultLimit
	}
	if limit > widgetSessionMaxLimit {
		limit = widgetSessionMaxLimit
	}
	offset := in.Offset
	if offset < 0 {
		offset = 0
	}

	query := store.DB.Model(&model.WidgetSession{}).Where("owner_user_id = ?", in.OwnerUserID)
	if in.SiteID > 0 {
		query = query.Where("site_id = ?", in.SiteID)
	}
	if in.Status != widgetSessionStatusAll {
		query = query.Where("status = ?", in.Status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	var sessions []model.WidgetSession
	if err := query.Order("updated_at DESC").Limit(limit).Offset(offset).Find(&sessions).Error; err != nil {
		return nil, err
	}

	items := make([]WidgetSessionDTO, 0, len(sessions))
	for _, s := range sessions {
		items = append(items, toWidgetSessionDTO(s))
	}
	return &WidgetSessionListResp{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

func WidgetSessionClose(in WidgetSessionStatusUpdateInput) (*WidgetSessionDTO, error) {
	in.Status = model.WidgetSessionStatusClosed
	dto, _, err := widgetSessionSetStatus(in)
	if err != nil {
		return nil, err
	}
	publishWidgetSessionClosed(dto.VisitorID, dto.SessionID, "closed")
	return dto, nil
}

func WidgetSessionBan(in WidgetSessionStatusUpdateInput) (*WidgetSessionDTO, error) {
	in.Status = model.WidgetSessionStatusBanned
	dto, session, err := widgetSessionSetStatus(in)
	if err != nil {
		return nil, err
	}
	// 附带把该访客会话最近 init IP 加入 owner 维度的 IP 封禁（默认 7 天）；
	// 与 visitor_key 封禁相互独立。LastInitIP 为空时 BanWidgetIP 静默跳过，
	// 写封禁失败只告警，不回滚已完成的会话封禁。
	if err := security.BanWidgetIP(in.OwnerUserID, session.LastInitIP, "session_ban", session.SessionID, security.WidgetIPBanDefaultTTL); err != nil {
		logger.L.Warnf("widget session ban: write ip ban failed owner=%d session=%s err=%v", in.OwnerUserID, session.SessionID, err)
	}
	publishWidgetSessionClosed(dto.VisitorID, dto.SessionID, "banned")
	return dto, nil
}

func widgetSessionSetStatus(in WidgetSessionStatusUpdateInput) (*WidgetSessionDTO, *model.WidgetSession, error) {
	if in.OwnerUserID <= 0 || strings.TrimSpace(in.SessionID) == "" || !isWidgetSessionStatusValid(in.Status) {
		return nil, nil, ErrWidgetSiteInvalidInput
	}
	var session model.WidgetSession
	if err := store.DB.Where("session_id = ? AND owner_user_id = ?", strings.TrimSpace(in.SessionID), in.OwnerUserID).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrWidgetSessionNotOwned
		}
		return nil, nil, err
	}
	now := time.Now().UTC()
	if err := store.DB.Model(&model.WidgetSession{}).Where("id = ?", session.ID).Updates(map[string]interface{}{
		"status":     in.Status,
		"updated_at": now,
	}).Error; err != nil {
		return nil, nil, err
	}
	session.Status = in.Status
	session.UpdatedAt = now
	dto := toWidgetSessionDTO(session)
	return &dto, &session, nil
}

func isWidgetSessionStatusValid(status int16) bool {
	return status == model.WidgetSessionStatusActive || status == model.WidgetSessionStatusClosed || status == model.WidgetSessionStatusBanned
}

func toWidgetSessionDTO(s model.WidgetSession) WidgetSessionDTO {
	return WidgetSessionDTO{
		ID:           s.ID,
		SiteID:       s.SiteID,
		SessionID:    s.SessionID,
		VisitorID:    s.VisitorID,
		VisitorKey:   s.VisitorKey,
		VisitorName:  s.VisitorName,
		VisitorEmail: s.VisitorEmail,
		LastPageURL:  s.LastPageURL,
		Status:       s.Status,
		LastActiveAt: s.LastActiveAt.Unix(),
		UpdatedAt:    s.UpdatedAt.Unix(),
	}
}

// publishWidgetSessionClosed publishes a widget_session_closed WS command
// to the visitor's live connections via Redis pub/sub.
func publishWidgetSessionClosed(visitorID int64, sessionID, reason string) {
	if store.RDB == nil || visitorID <= 0 {
		return
	}
	ctx := context.Background()
	routeKey := fmt.Sprintf("im:ws:route:%d", visitorID)
	devices, err := store.RDB.HGetAll(ctx, routeKey).Result()
	if err != nil {
		logger.L.Warnf("widget session closed: load route failed visitor=%d err=%v", visitorID, err)
		return
	}

	seen := make(map[string]struct{}, len(devices))
	payload := protocol.WidgetSessionClosedPayload{
		SessionID: sessionID,
		Reason:    reason,
	}
	data, err := json.Marshal(map[string]interface{}{
		"user_id": visitorID,
		"cmd":     protocol.CmdWidgetSessionClosed,
		"payload": payload,
	})
	if err != nil {
		return
	}
	for _, rawNodeID := range devices {
		nodeID := strings.TrimSpace(rawNodeID)
		if nodeID == "" {
			continue
		}
		if _, ok := seen[nodeID]; ok {
			continue
		}
		seen[nodeID] = struct{}{}
		if err := store.RDB.Publish(ctx, fmt.Sprintf("chan:%s", nodeID), string(data)).Err(); err != nil {
			logger.L.Warnf("widget session closed: publish failed visitor=%d node=%s err=%v", visitorID, nodeID, err)
		}
	}
}
