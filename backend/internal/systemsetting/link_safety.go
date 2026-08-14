package systemsetting

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"golang.org/x/net/idna"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// LinkSafetySettings 链接安全（点击时校验）的塘主可配置项。
type LinkSafetySettings struct {
	Enabled             bool     `json:"enabled"`               // 总开关
	OwnDomainWhitelist  []string `json:"own_domain_whitelist"`  // 自家域名直通
	MaliciousCacheTTLMS int      `json:"malicious_cache_ttl_ms"`
	CleanCacheTTLMS     int      `json:"clean_cache_ttl_ms"`
	ExternalIntelEnable bool     `json:"external_intel_enable"` // 外部威胁情报（P2 起作用）
}

const linkSafetySettingKey = "link_safety"
const linkSafetySettingsCacheTTL = time.Minute

var linkSafetySettingsNow = time.Now

var linkSafetySettingsCache struct {
	mu        sync.RWMutex
	value     LinkSafetySettings
	expiresAt time.Time
	loaded    bool
}

// DefaultLinkSafetySettings 默认值：启用，自家域名预置 grix.dhf.pub。
func DefaultLinkSafetySettings() LinkSafetySettings {
	return LinkSafetySettings{
		Enabled:             true,
		OwnDomainWhitelist:  []string{"grix.dhf.pub"},
		MaliciousCacheTTLMS: 24 * 60 * 60 * 1000, // 24h
		CleanCacheTTLMS:     10 * 60 * 1000,      // 10min
		ExternalIntelEnable: false,
	}
}

// GetLinkSafetySettings 读取（带内存缓存）。
func GetLinkSafetySettings() (LinkSafetySettings, error) {
	now := linkSafetySettingsNow()
	if v, ok := getLinkSafetySettingsFromCache(now); ok {
		return v, nil
	}

	settings := DefaultLinkSafetySettings()
	var row model.SystemSetting
	if err := store.DB.First(&row, "key = ?", linkSafetySettingKey).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			settings = NormalizeLinkSafetySettings(settings)
			setLinkSafetySettingsCache(settings, now)
			return settings, nil
		}
		return LinkSafetySettings{}, err
	}
	if len(row.Value) > 0 {
		if err := json.Unmarshal(row.Value, &settings); err != nil {
			return LinkSafetySettings{}, err
		}
	}
	settings = NormalizeLinkSafetySettings(settings)
	setLinkSafetySettingsCache(settings, now)
	return settings, nil
}

// SaveLinkSafetySettings 写入并失效缓存。
func SaveLinkSafetySettings(settings LinkSafetySettings, updatedBy *int64) error {
	settings = NormalizeLinkSafetySettings(settings)
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	row := model.SystemSetting{
		Key:       linkSafetySettingKey,
		Value:     datatypes.JSON(raw),
		UpdatedBy: updatedBy,
	}
	if err := store.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_by", "updated_at"}),
	}).Create(&row).Error; err != nil {
		return err
	}
	setLinkSafetySettingsCache(settings, linkSafetySettingsNow())
	return nil
}

func InvalidateLinkSafetySettingsCache() {
	linkSafetySettingsCache.mu.Lock()
	linkSafetySettingsCache.loaded = false
	linkSafetySettingsCache.expiresAt = time.Time{}
	linkSafetySettingsCache.value = LinkSafetySettings{}
	linkSafetySettingsCache.mu.Unlock()
}

func getLinkSafetySettingsFromCache(now time.Time) (LinkSafetySettings, bool) {
	linkSafetySettingsCache.mu.RLock()
	defer linkSafetySettingsCache.mu.RUnlock()
	if !linkSafetySettingsCache.loaded {
		return LinkSafetySettings{}, false
	}
	if now.After(linkSafetySettingsCache.expiresAt) {
		return LinkSafetySettings{}, false
	}
	return linkSafetySettingsCache.value, true
}

func setLinkSafetySettingsCache(settings LinkSafetySettings, now time.Time) {
	linkSafetySettingsCache.mu.Lock()
	linkSafetySettingsCache.value = settings
	linkSafetySettingsCache.expiresAt = now.Add(linkSafetySettingsCacheTTL)
	linkSafetySettingsCache.loaded = true
	linkSafetySettingsCache.mu.Unlock()
}

// NormalizeLinkSafetySettings 标准化：去空、去重、小写化白名单域名。
func NormalizeLinkSafetySettings(settings LinkSafetySettings) LinkSafetySettings {
	defaults := DefaultLinkSafetySettings()
	if settings.MaliciousCacheTTLMS <= 0 {
		settings.MaliciousCacheTTLMS = defaults.MaliciousCacheTTLMS
	}
	if settings.CleanCacheTTLMS <= 0 {
		settings.CleanCacheTTLMS = defaults.CleanCacheTTLMS
	}
	settings.OwnDomainWhitelist = normalizeOwnDomains(settings.OwnDomainWhitelist)
	return settings
}

// normalizeOwnDomains 标准化白名单：
// - 去前后空白、`*.`、首尾点、转小写；
// - IDN 转 punycode（与后端规范化算法对齐，确保 host 比对一致）；
// - 拒绝少于 2 段的条目（如 `com`/`org`），防止误配做全 TLD 直通；
// - 去重保持原顺序。
func normalizeOwnDomains(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.ToLower(strings.TrimSpace(v))
		v = strings.TrimPrefix(v, "*.")
		v = strings.Trim(v, ".")
		// 压缩连续点（与后端 urlguard.Canonicalize 的 host 处理对齐）
		for strings.Contains(v, "..") {
			v = strings.ReplaceAll(v, "..", ".")
		}
		if v == "" {
			continue
		}
		// IDN -> punycode；失败时回退到原字符串（管理员通常输入 ASCII，
		// 失败也不影响 ASCII 域名的正常匹配）。
		if ascii, err := idna.Lookup.ToASCII(v); err == nil && ascii != "" {
			v = ascii
		}
		// 至少 2 段（domain.tld），不允许单段（防 com / org 误配）。
		if strings.Count(v, ".") < 1 {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
