package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/customercoach"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/secretcrypto"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type UserListParams struct {
	Query      string
	IDs        []int64
	Status     int16
	OnlineOnly bool
	Page       int
	PageSize   int
}

type UserListItem struct {
	ID                         int64      `json:"id,string"`
	Username                   string     `json:"username"`
	Email                      string     `json:"email"`
	PhoneCipher                string     `gorm:"column:phone_cipher" json:"-"` // 中转：从库读密文，解密后填入 PhoneE164 再下发塘主
	PhoneE164                  string     `gorm:"column:phone_e164" json:"phone_e164"`
	PhoneCountry               string     `json:"phone_country"`
	Nickname                   string     `json:"nickname"`
	AvatarURL                  string     `gorm:"column:avatar_url" json:"avatar_url"`
	Status                     int16      `json:"status"`
	LoginLocked                bool       `json:"login_locked"`
	LockRemaining              string     `json:"lock_remaining,omitempty"`
	BannedReason               string     `json:"banned_reason"`
	BannedAt                   *time.Time `json:"banned_at,omitempty"`
	ModerationMuted            bool       `json:"moderation_muted"`
	ModerationMuteSessionCount int        `json:"moderation_mute_session_count"`
	CreatedAt                  time.Time  `json:"created_at"`
}

type UserListResult struct {
	Items    []UserListItem
	Total    int64
	Page     int
	PageSize int
}

type UserCustomerCoachSnapshot struct {
	Snapshot customercoach.Snapshot `json:"snapshot"`
	Markdown string                 `json:"markdown"`
}

func ListUsers(params UserListParams) (*UserListResult, error) {
	page := params.Page
	if page < 1 {
		page = 1
	}
	pageSize := params.PageSize
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}

	query := store.DB.Model(&model.User{})
	if params.IDs != nil {
		if len(params.IDs) == 0 {
			return &UserListResult{
				Items:    []UserListItem{},
				Total:    0,
				Page:     page,
				PageSize: pageSize,
			}, nil
		}
		query = query.Where("id IN ?", params.IDs)
	}
	if params.OnlineOnly {
		onlineUserIDs, err := onlineUserIDsFromRedis(context.Background())
		if err != nil {
			return nil, err
		}
		if len(onlineUserIDs) == 0 {
			return &UserListResult{
				Items:    []UserListItem{},
				Total:    0,
				Page:     page,
				PageSize: pageSize,
			}, nil
		}
		query = query.Where("id IN ?", onlineUserIDs)
	}
	keyword := strings.TrimSpace(params.Query)
	if keyword != "" {
		like := "%" + keyword + "%"
		// 手机号已加密存储，无法模糊匹配；改为按末 4 位精确匹配 phone_last4。
		query = query.Where(
			"CAST(id AS TEXT) = ? OR LOWER(username) LIKE LOWER(?) OR LOWER(email) LIKE LOWER(?) OR LOWER(nickname) LIKE LOWER(?) OR phone_last4 = ?",
			keyword,
			like,
			like,
			like,
			keyword,
		)
	}
	if params.Status > 0 {
		query = query.Where("status = ?", params.Status)
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

	var items []UserListItem
	if err := query.Select("id", "username", "email", "phone_cipher", "phone_e164", "phone_country", "nickname", "avatar_url", "status", "banned_reason", "banned_at", "created_at").
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&items).Error; err != nil {
		return nil, err
	}
	// 塘主有权看真实号：把密文解密回填 phone_e164，再清掉中转密文不下发。
	for i := range items {
		if items[i].PhoneCipher != "" {
			if plain, derr := secretcrypto.Decrypt(items[i].PhoneCipher); derr == nil && plain != "" {
				items[i].PhoneE164 = plain
			}
		}
		items[i].PhoneCipher = ""
	}
	if err := attachUserLockStatus(context.Background(), items); err != nil {
		return nil, err
	}
	if err := attachUserModerationMuteStatus(items); err != nil {
		return nil, err
	}

	return &UserListResult{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func GetUserCustomerCoachSnapshot(ctx context.Context, userID int64) (UserCustomerCoachSnapshot, error) {
	snapshot, err := customercoach.BuildSnapshot(ctx, userID, "admin_api", "admin_view")
	if err != nil {
		return UserCustomerCoachSnapshot{}, err
	}
	return UserCustomerCoachSnapshot{
		Snapshot: snapshot,
		Markdown: customercoach.RenderMarkdown(snapshot),
	}, nil
}

func BanUser(adminID, userID int64, reason, clientIP, userAgent string) error {
	now := time.Now().UTC()
	var banned bool

	err := store.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		banned, err = banUserTx(tx, adminID, userID, reason, now, clientIP, userAgent)
		return err
	})
	if err != nil {
		return err
	}
	if !banned {
		return nil
	}

	return publishKickUser(context.Background(), userID, "user_disabled")
}

func UnbanUser(adminID, userID int64, clientIP, userAgent string) error {
	now := time.Now().UTC()
	return store.DB.Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, userID).Error; err != nil {
			return err
		}

		if err := tx.Model(&model.User{}).
			Where("id = ?", userID).
			Updates(map[string]any{
				"status":        model.UserStatusActive,
				"banned_reason": "",
				"banned_at":     nil,
				"banned_by":     nil,
				"updated_at":    now,
			}).Error; err != nil {
			return err
		}

		return recordOperationTx(tx, adminID, "user_unban", "user", fmt.Sprintf("%d", userID), map[string]any{}, clientIP, userAgent)
	})
}

// UnbindUserPhone 塘主强制解绑某个用户的手机号：清空 users.phone_e164 / phone_country
// 并删除 user_identities 里 provider=phone_sms_* 的记录。用于客服处理用户换号但旧号被占等场景。
func UnbindUserPhone(adminID, userID int64, clientIP, userAgent string) error {
	return store.DB.Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, userID).Error; err != nil {
			return err
		}
		prevPhone := user.PhoneE164
		if prevPhone == "" && user.PhoneCipher != "" {
			if plain, derr := secretcrypto.Decrypt(user.PhoneCipher); derr == nil {
				prevPhone = plain
			}
		}
		if err := tx.Model(&model.User{}).
			Where("id = ?", userID).
			Updates(map[string]any{
				"phone_e164":    "",
				"phone_cipher":  "",
				"phone_last4":   "",
				"phone_blind":   "",
				"phone_country": "",
				"updated_at":    time.Now().UTC(),
			}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ? AND provider IN ?", userID,
			[]string{"phone_sms_cn", "phone_sms_global"}).
			Delete(&model.UserIdentity{}).Error; err != nil {
			return err
		}
		return recordOperationTx(tx, adminID, "user_unbind_phone", "user", fmt.Sprintf("%d", userID), map[string]any{
			"phone_e164": prevPhone,
		}, clientIP, userAgent)
	})
}

func UnlockUserLogin(adminID, userID int64, clientIP, userAgent string) error {
	return store.DB.Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, userID).Error; err != nil {
			return err
		}

		if err := clearUserLoginLock(context.Background(), user); err != nil {
			return err
		}

		return recordOperationTx(tx, adminID, "user_login_unlock", "user", fmt.Sprintf("%d", userID), map[string]any{
			"username": user.Username,
			"email":    user.Email,
		}, clientIP, userAgent)
	})
}

func GetAuthSettings() (systemsetting.AuthSettings, error) {
	return systemsetting.GetAuthSettings()
}

func GetGroupSettings() (systemsetting.GroupSettings, error) {
	return systemsetting.GetGroupSettings()
}

func UpdateAuthSettings(adminID int64, settings systemsetting.AuthSettings, clientIP, userAgent string) error {
	if err := validateAuthSettings(settings); err != nil {
		return err
	}

	err := store.DB.Transaction(func(tx *gorm.DB) error {
		raw, err := json.Marshal(settings)
		if err != nil {
			return err
		}
		updatedBy := adminID
		row := model.SystemSetting{
			Key:       "auth",
			Value:     datatypes.JSON(raw),
			UpdatedBy: &updatedBy,
		}
		if err := tx.Where("key = ?", row.Key).Assign(row).FirstOrCreate(&row).Error; err != nil {
			return err
		}
		return recordOperationTx(tx, adminID, "auth_settings_update", "system_setting", "auth", settings, clientIP, userAgent)
	})
	if err != nil {
		return err
	}
	systemsetting.InvalidateAuthSettingsCache()
	return nil
}

func UpdateGroupSettings(adminID int64, settings systemsetting.GroupSettings, clientIP, userAgent string) error {
	if err := validateGroupSettings(settings); err != nil {
		return err
	}

	err := store.DB.Transaction(func(tx *gorm.DB) error {
		raw, err := json.Marshal(settings)
		if err != nil {
			return err
		}
		updatedBy := adminID
		row := model.SystemSetting{
			Key:       "group",
			Value:     datatypes.JSON(raw),
			UpdatedBy: &updatedBy,
		}
		if err := tx.Where("key = ?", row.Key).Assign(row).FirstOrCreate(&row).Error; err != nil {
			return err
		}
		return recordOperationTx(tx, adminID, "group_settings_update", "system_setting", "group", settings, clientIP, userAgent)
	})
	if err != nil {
		return err
	}
	systemsetting.InvalidateGroupSettingsCache()
	return nil
}

func validateAuthSettings(settings systemsetting.AuthSettings) error {
	if settings.AutoAddCustomerUserID < 0 {
		return errors.New("系统客户账户ID必须为非负整数")
	}
	if settings.AutoAddCustomerUserID == 0 {
		return nil
	}

	var user model.User
	if err := store.DB.Select("id", "status").First(&user, settings.AutoAddCustomerUserID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("系统客户账户不存在")
		}
		return err
	}
	if user.Status != model.UserStatusActive {
		return errors.New("系统客户账户已禁用")
	}
	return nil
}

func banUserTx(
	tx *gorm.DB,
	adminID, userID int64,
	reason string,
	now time.Time,
	clientIP, userAgent string,
) (bool, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "admin_disabled"
	}

	var user model.User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, userID).Error; err != nil {
		return false, err
	}
	if user.Status == model.UserStatusBanned {
		return false, nil
	}

	if err := tx.Model(&model.User{}).
		Where("id = ?", userID).
		Updates(map[string]any{
			"status":        model.UserStatusBanned,
			"banned_reason": reason,
			"banned_at":     now,
			"banned_by":     adminID,
			"updated_at":    now,
		}).Error; err != nil {
		return false, err
	}

	if err := tx.Model(&model.RefreshToken{}).
		Where("user_id = ? AND status IN ?", userID, []int16{
			model.RefreshTokenStatusActive,
			model.RefreshTokenStatusUsed,
		}).
		Updates(map[string]any{
			"status":     model.RefreshTokenStatusRevoked,
			"revoked_at": now,
			"updated_at": now,
		}).Error; err != nil {
		return false, err
	}

	if err := recordOperationTx(tx, adminID, "user_ban", "user", fmt.Sprintf("%d", userID), map[string]any{
		"reason": reason,
	}, clientIP, userAgent); err != nil {
		return false, err
	}

	return true, nil
}

func validateGroupSettings(settings systemsetting.GroupSettings) error {
	if settings.MemberInviteThreshold <= 0 {
		return errors.New("群成员邀请阈值必须为正整数")
	}
	return nil
}

func recordOperationTx(tx *gorm.DB, adminID int64, action, targetType, targetID string, detail any, clientIP, userAgent string) error {
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	return tx.Create(&model.AdminOperationLog{
		AdminID:    adminID,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Detail:     datatypes.JSON(raw),
		ClientIP:   truncate(clientIP, 45),
		UserAgent:  truncate(userAgent, 255),
	}).Error
}

func publishKickUser(ctx context.Context, userID int64, reason string) error {
	if store.RDB == nil {
		return nil
	}

	routeKey := fmt.Sprintf("im:ws:route:%d", userID)
	routes, err := store.RDB.HGetAll(ctx, routeKey).Result()
	if err != nil {
		return err
	}
	if len(routes) == 0 {
		return nil
	}

	payload, err := json.Marshal(map[string]any{
		"reason": reason,
	})
	if err != nil {
		return err
	}

	type envelope struct {
		UserID  int64          `json:"user_id"`
		Cmd     string         `json:"cmd"`
		Payload datatypes.JSON `json:"payload"`
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

		raw, marshalErr := json.Marshal(envelope{
			UserID:  userID,
			Cmd:     "kicked",
			Payload: datatypes.JSON(payload),
		})
		if marshalErr != nil {
			return marshalErr
		}
		if err := store.RDB.Publish(ctx, fmt.Sprintf("chan:%s", nodeID), raw).Err(); err != nil {
			return err
		}
	}
	return nil
}

func EnsureAdminBootstrapAvailable() error {
	var count int64
	if err := store.DB.Model(&model.AdminUser{}).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return errors.New("请先创建管理员账号")
	}
	return nil
}

func attachUserLockStatus(ctx context.Context, items []UserListItem) error {
	if len(items) == 0 || store.RDB == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	for i := range items {
		locked, remaining, err := security.NewUserLoginGuardByUserID(items[i].ID).IsLocked(ctx)
		if err != nil {
			return err
		}
		items[i].LoginLocked = locked
		if locked {
			items[i].LockRemaining = security.FormatLockTime(remaining)
		}
	}
	return nil
}

func clearUserLoginLock(ctx context.Context, user model.User) error {
	if ctx == nil {
		ctx = context.Background()
	}

	guards := []*security.LoginGuard{
		security.NewUserLoginGuardByUserID(user.ID),
	}

	username := strings.TrimSpace(user.Username)
	if username != "" {
		guards = append(guards, security.NewUserLoginGuardByAccount(username))
	}
	email := strings.TrimSpace(user.Email)
	if email != "" && email != username {
		guards = append(guards, security.NewUserLoginGuardByAccount(email))
	}

	for _, guard := range guards {
		if err := guard.ClearLock(ctx); err != nil {
			return err
		}
	}
	return nil
}

func attachUserModerationMuteStatus(items []UserListItem) error {
	if len(items) == 0 {
		return nil
	}

	userIDs := make([]int64, 0, len(items))
	for _, item := range items {
		if item.ID <= 0 {
			continue
		}
		userIDs = append(userIDs, item.ID)
	}
	if len(userIDs) == 0 {
		return nil
	}

	type moderationMuteRow struct {
		UserID       int64 `gorm:"column:user_id"`
		SessionCount int   `gorm:"column:session_count"`
	}

	var rows []moderationMuteRow
	if err := store.DB.Table("content_moderation_events AS e").
		Select("e.sender_id AS user_id, COUNT(DISTINCT e.session_id) AS session_count").
		Joins("JOIN session_members AS sm ON sm.session_id = e.session_id AND sm.member_id = e.sender_id AND sm.member_type = e.sender_type").
		Where(
			"e.sender_type = ? AND e.mute_applied = ? AND sm.is_speak_muted = ? AND e.sender_id IN ?",
			1,
			true,
			true,
			userIDs,
		).
		Group("e.sender_id").
		Scan(&rows).Error; err != nil {
		return err
	}

	rowByUserID := make(map[int64]moderationMuteRow, len(rows))
	for _, row := range rows {
		rowByUserID[row.UserID] = row
	}

	for i := range items {
		row, ok := rowByUserID[items[i].ID]
		if !ok {
			continue
		}
		items[i].ModerationMuted = row.SessionCount > 0
		items[i].ModerationMuteSessionCount = row.SessionCount
	}
	return nil
}
