package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const contentModerationSystemSettingKey = "content_moderation"

var ErrContentModerationMuteNotActive = errors.New("目标当前不处于可解除的审查禁言状态")

type ContentModerationEventListParams struct {
	Query     string
	MutedOnly bool
	Page      int
	PageSize  int
}

type ContentModerationEventListItem struct {
	ID                  int64          `json:"id,string"`
	SessionID           string         `json:"session_id"`
	MsgID               int64          `json:"msg_id,string"`
	SenderID            int64          `json:"sender_id,string"`
	SenderType          int16          `json:"sender_type"`
	SenderUsername      string         `json:"sender_username"`
	SenderEmail         string         `json:"sender_email"`
	SenderNickname      string         `json:"sender_nickname"`
	MatchedKeywordsDB   datatypes.JSON `gorm:"column:matched_keywords" json:"-"`
	MatchedKeywords     []string       `gorm:"-" json:"matched_keywords"`
	MatchedKeywordsText string         `gorm:"-" json:"matched_keywords_text"`
	RecallStatus        string         `json:"recall_status"`
	RecallStatusText    string         `gorm:"-" json:"recall_status_text"`
	RecallAttempts      int            `json:"recall_attempts"`
	HitCount            int            `json:"hit_count"`
	MuteApplied         bool           `json:"mute_applied"`
	CurrentlyMuted      bool           `json:"currently_muted"`
	CreatedAt           time.Time      `json:"created_at"`
}

type ContentModerationEventListResult struct {
	Items    []ContentModerationEventListItem
	Total    int64
	Page     int
	PageSize int
}

func GetContentModerationSettings() (systemsetting.ContentModerationSettings, error) {
	return systemsetting.GetContentModerationSettings()
}

func ListContentModerationEvents(params ContentModerationEventListParams) (*ContentModerationEventListResult, error) {
	page := params.Page
	if page < 1 {
		page = 1
	}
	pageSize := params.PageSize
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}

	query := buildContentModerationEventQuery(params)

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

	var items []ContentModerationEventListItem
	if err := query.
		Select(
			"e.id",
			"e.session_id",
			"e.msg_id",
			"e.sender_id",
			"e.sender_type",
			"u.username AS sender_username",
			"u.email AS sender_email",
			"u.nickname AS sender_nickname",
			"e.matched_keywords",
			"e.recall_status",
			"e.recall_attempts",
			"e.hit_count",
			"e.mute_applied",
			"CASE WHEN e.mute_applied = TRUE AND COALESCE(sm.is_speak_muted, FALSE) = TRUE THEN TRUE ELSE FALSE END AS currently_muted",
			"e.created_at",
		).
		Order("e.created_at DESC, e.id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Scan(&items).Error; err != nil {
		return nil, err
	}

	for i := range items {
		items[i].MatchedKeywords = parseMatchedKeywords(items[i].MatchedKeywordsDB)
		items[i].MatchedKeywordsText = strings.Join(items[i].MatchedKeywords, "、")
		items[i].RecallStatusText = formatModerationRecallStatus(items[i].RecallStatus)
	}

	return &ContentModerationEventListResult{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func UpdateContentModerationSettings(
	adminID int64,
	settings systemsetting.ContentModerationSettings,
	clientIP, userAgent string,
) error {
	if err := validateContentModerationSettings(settings); err != nil {
		return err
	}
	settings = systemsetting.NormalizeContentModerationSettings(settings)

	err := store.DB.Transaction(func(tx *gorm.DB) error {
		raw, err := json.Marshal(settings)
		if err != nil {
			return err
		}

		updatedBy := adminID
		row := model.SystemSetting{
			Key:       contentModerationSystemSettingKey,
			Value:     datatypes.JSON(raw),
			UpdatedBy: &updatedBy,
		}
		if err := tx.Where("key = ?", row.Key).Assign(row).FirstOrCreate(&row).Error; err != nil {
			return err
		}

		return recordOperationTx(
			tx,
			adminID,
			"content_moderation_settings_update",
			"system_setting",
			contentModerationSystemSettingKey,
			settings,
			clientIP,
			userAgent,
		)
	})
	if err != nil {
		return err
	}

	systemsetting.InvalidateContentModerationSettingsCache()
	return nil
}

func UnmuteModeratedSessionMember(
	adminID int64,
	sessionID string,
	memberID int64,
	clientIP, userAgent string,
) error {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return errors.New("会话ID不能为空")
	}
	if memberID <= 0 {
		return errors.New("用户ID不能为空")
	}

	now := time.Now().UTC()
	changed := false
	err := store.DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.SessionMember{}).
			Where(
				"session_id = ? AND member_id = ? AND member_type = ? AND is_speak_muted = ?",
				sid,
				memberID,
				1,
				true,
			).
			Where(
				`EXISTS (
					SELECT 1
					FROM content_moderation_events AS e
					WHERE e.session_id = session_members.session_id
					  AND e.sender_id = session_members.member_id
					  AND e.sender_type = session_members.member_type
					  AND e.mute_applied = ?
				)`,
				true,
			).
			Update("is_speak_muted", false)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrContentModerationMuteNotActive
		}

		changed = true
		if err := tx.Model(&model.Session{}).
			Where("session_id = ?", sid).
			Update("updated_at", now).Error; err != nil {
			return err
		}

		return recordOperationTx(tx, adminID, "content_moderation_unmute", "session_member", sid+":"+strconv.FormatInt(memberID, 10), map[string]any{
			"session_id": sid,
			"member_id":  memberID,
		}, clientIP, userAgent)
	})
	if err != nil {
		return err
	}
	if changed {
		notifyModerationSpeakingChanged(sid, memberID)
	}
	return nil
}

func UnmuteUserContentModerationSessions(
	adminID int64,
	userID int64,
	clientIP, userAgent string,
) error {
	if userID <= 0 {
		return errors.New("用户ID不能为空")
	}

	now := time.Now().UTC()
	sessionIDs := make([]string, 0)
	err := store.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		sessionIDs, err = listActiveModerationMutedSessionIDsTx(tx, userID)
		if err != nil {
			return err
		}
		if len(sessionIDs) == 0 {
			return ErrContentModerationMuteNotActive
		}

		result := tx.Model(&model.SessionMember{}).
			Where(
				"member_id = ? AND member_type = ? AND session_id IN ? AND is_speak_muted = ?",
				userID,
				1,
				sessionIDs,
				true,
			).
			Update("is_speak_muted", false)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			sessionIDs = nil
			return ErrContentModerationMuteNotActive
		}

		if err := tx.Model(&model.Session{}).
			Where("session_id IN ?", sessionIDs).
			Update("updated_at", now).Error; err != nil {
			return err
		}

		return recordOperationTx(tx, adminID, "content_moderation_user_unmute", "user", strconv.FormatInt(userID, 10), map[string]any{
			"user_id":       userID,
			"session_ids":   sessionIDs,
			"session_count": len(sessionIDs),
		}, clientIP, userAgent)
	})
	if err != nil {
		return err
	}

	for _, sessionID := range sessionIDs {
		notifyModerationSpeakingChanged(sessionID, userID)
	}
	return nil
}

func validateContentModerationSettings(settings systemsetting.ContentModerationSettings) error {
	if settings.HumanMuteThreshold <= 0 {
		return errors.New("累计命中禁言阈值必须为正整数")
	}
	return nil
}

func buildContentModerationEventQuery(params ContentModerationEventListParams) *gorm.DB {
	query := store.DB.Table("content_moderation_events AS e").
		Joins("LEFT JOIN users AS u ON e.sender_type = ? AND u.id = e.sender_id", 1).
		Joins("LEFT JOIN session_members AS sm ON sm.session_id = e.session_id AND sm.member_id = e.sender_id AND sm.member_type = e.sender_type")

	keyword := strings.TrimSpace(params.Query)
	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where(
			"CAST(e.sender_id AS TEXT) = ? OR CAST(e.msg_id AS TEXT) = ? OR e.session_id = ? OR LOWER(COALESCE(u.username, '')) LIKE LOWER(?) OR LOWER(COALESCE(u.email, '')) LIKE LOWER(?) OR LOWER(COALESCE(u.nickname, '')) LIKE LOWER(?)",
			keyword,
			keyword,
			keyword,
			like,
			like,
			like,
		)
	}
	if params.MutedOnly {
		query = query.Where("e.mute_applied = ? AND COALESCE(sm.is_speak_muted, FALSE) = ?", true, true)
	}

	return query
}

func parseMatchedKeywords(raw datatypes.JSON) []string {
	if len(raw) == 0 {
		return nil
	}

	var keywords []string
	if err := json.Unmarshal(raw, &keywords); err != nil {
		return nil
	}
	return keywords
}

func formatModerationRecallStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "pending":
		return "待撤回"
	case "revoked":
		return "已撤回"
	case "already_revoked":
		return "已提前撤回"
	case "message_missing":
		return "消息不存在"
	case "unsupported_sender":
		return "发送者类型不支持"
	case "revoke_failed":
		return "撤回失败"
	default:
		if strings.TrimSpace(status) == "" {
			return "未知"
		}
		return status
	}
}

func listActiveModerationMutedSessionIDsTx(tx *gorm.DB, userID int64) ([]string, error) {
	type sessionRow struct {
		SessionID string `gorm:"column:session_id"`
	}

	var rows []sessionRow
	if err := tx.Model(&model.SessionMember{}).
		Select("session_members.session_id").
		Where(
			"member_id = ? AND member_type = ? AND is_speak_muted = ?",
			userID,
			1,
			true,
		).
		Where(
			`EXISTS (
				SELECT 1
				FROM content_moderation_events AS e
				WHERE e.session_id = session_members.session_id
				  AND e.sender_id = session_members.member_id
				  AND e.sender_type = session_members.member_type
				  AND e.mute_applied = ?
			)`,
			true,
		).
		Group("session_members.session_id").
		Order("session_members.session_id ASC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	sessionIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.SessionID) == "" {
			continue
		}
		sessionIDs = append(sessionIDs, row.SessionID)
	}
	return sessionIDs, nil
}

func notifyModerationSpeakingChanged(sessionID string, memberID int64) {
	userIDs, err := listSessionHumanMemberIDsForAdmin(sessionID)
	if err != nil {
		logger.L.Warnf("list session human members for moderation notify failed session=%s err=%v", sessionID, err)
		return
	}
	if len(userIDs) == 0 {
		return
	}

	payload := protocol.SessionMemberChangedPayload{
		SessionID: sessionID,
		Action:    "speaking",
		MemberID:  memberID,
		UpdatedAt: time.Now().UnixMilli(),
	}
	publishAdminRealtimeEventToUsers(userIDs, protocol.CmdSessionMemberChanged, payload)
}

func listSessionHumanMemberIDsForAdmin(sessionID string) ([]int64, error) {
	var members []model.SessionMember
	if err := store.DB.Select("member_id").
		Where("session_id = ? AND member_type = 1", sessionID).
		Find(&members).Error; err != nil {
		return nil, err
	}

	userIDs := make([]int64, 0, len(members))
	for _, member := range members {
		if member.MemberID <= 0 {
			continue
		}
		userIDs = append(userIDs, member.MemberID)
	}
	return userIDs, nil
}

func publishAdminRealtimeEventToUsers(userIDs []int64, cmd string, payload any) {
	if len(userIDs) == 0 {
		return
	}

	sent := make(map[int64]struct{}, len(userIDs))
	for _, userID := range userIDs {
		if userID <= 0 {
			continue
		}
		if _, ok := sent[userID]; ok {
			continue
		}
		sent[userID] = struct{}{}
		publishAdminRealtimeEvent(userID, cmd, payload)
	}
}

func publishAdminRealtimeEvent(userID int64, cmd string, payload any) {
	if userID <= 0 || store.RDB == nil {
		return
	}

	raw, err := json.Marshal(map[string]any{
		"user_id": userID,
		"cmd":     cmd,
		"payload": payload,
	})
	if err != nil {
		logger.L.Warnf("marshal admin realtime event failed user=%d cmd=%s err=%v", userID, cmd, err)
		return
	}

	routeKey := fmt.Sprintf("im:ws:route:%d", userID)
	ctx := context.Background()
	routes, err := store.RDB.HGetAll(ctx, routeKey).Result()
	if err != nil || len(routes) == 0 {
		return
	}

	seenNodes := make(map[string]struct{}, len(routes))
	for _, nodeID := range routes {
		nodeID = strings.TrimSpace(nodeID)
		if nodeID == "" {
			continue
		}
		if _, ok := seenNodes[nodeID]; ok {
			continue
		}
		seenNodes[nodeID] = struct{}{}

		if err := store.RDB.Publish(ctx, fmt.Sprintf("chan:%s", nodeID), raw).Err(); err != nil {
			logger.L.Warnf("publish admin realtime event failed user=%d node=%s cmd=%s err=%v", userID, nodeID, cmd, err)
		}
	}
}
