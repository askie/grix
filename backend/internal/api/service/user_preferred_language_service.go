package service

import (
	"context"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/userpref"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	preferredLanguageZH = "zh"
	preferredLanguageEN = "en"
)

// supportedLanguages 是服务端认可的语言标识列表，顺序决定匹配优先级。
// 存储格式统一为小写 language code（不含 country code）。
var supportedLanguages = []string{
	"zh", "en", "ja", "ko", "de", "fr", "es", "pt", "ru", "ar", "hi",
}

func normalizePreferredLanguage(raw string) string {
	lower := strings.ToLower(strings.TrimSpace(raw))
	lower = strings.ReplaceAll(lower, "-", "_")
	for _, lang := range supportedLanguages {
		if lower == lang || strings.HasPrefix(lower, lang+"_") {
			return lang
		}
	}
	return preferredLanguageZH
}

// updateUserPreferredLanguageTx 只负责事务内写库，不在这里失效缓存——事务
// 提交前失效会被并发读者用未提交的旧值把缓存重新填脏（本文件
// cleanStaleDeviceCacheAfterCommit 上方注释讲过同样的坑）。调用方必须在
// 自己的事务真正提交成功后，自行调用 userpref.InvalidatePreferredLanguage。
func updateUserPreferredLanguageTx(tx *gorm.DB, userID int64, language string) error {
	if tx == nil || userID <= 0 {
		return nil
	}

	now := time.Now()
	normalized := normalizePreferredLanguage(language)
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"preferred_language": normalized,
			"updated_at":         now,
		}),
	}).Table(model.UserSetting{}.TableName()).Create(map[string]any{
		"user_id":            userID,
		"preferred_language": normalized,
		"friend_add_setting": model.FriendAddSettingNeedApproval,
		"allow_group_invite": true,
		"created_at":         now,
		"updated_at":         now,
	}).Error
}

func updateUserPreferredLanguage(userID int64, language string) error {
	if store.DB == nil {
		return nil
	}
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		return updateUserPreferredLanguageTx(tx, userID, language)
	}); err != nil {
		return err
	}
	userpref.InvalidatePreferredLanguage(userID)
	return nil
}

// loadUserPreferredLanguage 是非事务场景下的读取入口，走 internal/pkg/userpref
// 的缓存。事务内需要读最新值的场景（比如同一事务里刚写完又要读回）请用
// loadUserPreferredLanguageWithDB 直接查库，不要引入缓存导致读到脏值。
func loadUserPreferredLanguage(userID int64) (string, error) {
	return normalizePreferredLanguage(userpref.PreferredLanguage(context.Background(), userID)), nil
}

func loadUserPreferredLanguageWithDB(db *gorm.DB, userID int64) (string, error) {
	if db == nil {
		return preferredLanguageZH, nil
	}

	setting, exists, err := loadUserSettingWithDB(db, userID)
	if err != nil {
		return "", err
	}
	if !exists {
		return preferredLanguageZH, nil
	}
	return normalizePreferredLanguage(setting.PreferredLanguage), nil
}
