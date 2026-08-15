// Package userpref 提供用户级偏好设置的统一读取入口，带进程内缓存。
// 目前只有语言偏好一项；后续如果有别的 user_settings 字段也要走"高频读、
// 低频改、可以短暂脏读"的路径，可以照这个模式加。
package userpref

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

// DefaultLanguage 是查不到偏好设置时的兜底语言，与 user_settings 表
// preferred_language 列的库表默认值保持一致。
const DefaultLanguage = "zh"

const languageCacheTTL = 5 * time.Minute

// supportedLanguages 是服务端认可的用户语言标识列表，顺序决定匹配优先级。
// 存储格式统一为小写 language code（不含 country code）。
var supportedLanguages = []string{
	"zh", "en", "ja", "ko", "de", "fr", "es", "pt", "ru", "ar", "hi",
}

// NormalizeLanguage 把任意 BCP-47 风格的语言字符串（如 "zh-CN"、"en_US"）
// 归一化为 supportedLanguages 中的一员；无法识别时回退 DefaultLanguage。
func NormalizeLanguage(raw string) string {
	lang, _ := MatchLanguage(raw)
	return lang
}

// MatchLanguage 同 NormalizeLanguage，但报告是否为「真命中」：raw 归一化后
// 命中 supportedLanguages 时 ok=true；raw 为空或不在集合内时 ok=false 并返回
// DefaultLanguage。写路径（校验用户输入是否合法）用 MatchLanguage，读路径用
// NormalizeLanguage。
func MatchLanguage(raw string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(raw))
	lower = strings.ReplaceAll(lower, "-", "_")
	for _, lang := range supportedLanguages {
		if lower == lang || strings.HasPrefix(lower, lang+"_") {
			return lang, true
		}
	}
	return DefaultLanguage, false
}

type languageCacheEntry struct {
	lang     string
	loadedAt time.Time
}

var (
	languageCacheMu sync.RWMutex
	languageCache   = make(map[int64]languageCacheEntry)
)

// PreferredLanguage 返回用户的语言偏好原始值（user_settings.preferred_language，
// 写入时已经过 NormalizeLanguage 归一化），未设置/查询失败时兜底
// DefaultLanguage。进程内缓存 5 分钟，避免每次调用都查库；用户改了语言设置后
// 最多 languageCacheTTL 内个别地方可能还是旧语言，这个代价可接受，不做主动失效
// 广播（多进程场景下广播也解决不了"当前进程本地缓存"之外的问题）。
func PreferredLanguage(ctx context.Context, userID int64) string {
	if userID <= 0 {
		return DefaultLanguage
	}
	if lang, ok := getCached(userID); ok {
		return lang
	}
	lang := loadFromDB(ctx, userID)
	setCached(userID, lang)
	return lang
}

// Language 是「读取用户当前语言」的统一入口：PreferredLanguage + NormalizeLanguage，
// 返回 supportedLanguages 中的一员。各调用方一律用它取用户语言，不要再各自查
// user_settings.preferred_language 或自写一份归一化；个别模块只支持部分语言
// （比如只做了中英双语）时，在 Language 的返回值之上再做自己的收窄。
func Language(ctx context.Context, userID int64) string {
	return NormalizeLanguage(PreferredLanguage(ctx, userID))
}

// InvalidatePreferredLanguage 清掉某个用户的语言偏好缓存，供修改设置的写路径
// 在事务提交成功后调用，让该用户后续读取尽快看到新值，不必等 TTL 过期。
func InvalidatePreferredLanguage(userID int64) {
	if userID <= 0 {
		return
	}
	languageCacheMu.Lock()
	delete(languageCache, userID)
	languageCacheMu.Unlock()
}

func getCached(userID int64) (string, bool) {
	languageCacheMu.RLock()
	defer languageCacheMu.RUnlock()
	entry, ok := languageCache[userID]
	if !ok || time.Since(entry.loadedAt) > languageCacheTTL {
		return "", false
	}
	return entry.lang, true
}

func setCached(userID int64, lang string) {
	languageCacheMu.Lock()
	languageCache[userID] = languageCacheEntry{lang: lang, loadedAt: time.Now()}
	languageCacheMu.Unlock()
}

func loadFromDB(ctx context.Context, userID int64) string {
	if store.DB == nil {
		return DefaultLanguage
	}
	var lang string
	err := store.DB.WithContext(ctx).
		Model(&model.UserSetting{}).
		Select("preferred_language").
		Where("user_id = ?", userID).
		Limit(1).
		Scan(&lang).Error
	if err != nil {
		logger.L.Warnf("userpref: load preferred_language failed user=%d err=%v", userID, err)
		return DefaultLanguage
	}
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return DefaultLanguage
	}
	return lang
}
