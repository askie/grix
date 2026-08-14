package systemsetting

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const contentModerationSettingKey = "content_moderation"
const contentModerationSettingsCacheTTL = time.Minute

var contentModerationSettingsNow = time.Now

var contentModerationSettingsCache struct {
	mu        sync.RWMutex
	value     ContentModerationSettings
	expiresAt time.Time
	loaded    bool
}

type ContentModerationSettings struct {
	Enabled            bool     `json:"enabled"`
	Keywords           []string `json:"keywords"`
	HumanMuteThreshold int      `json:"human_mute_threshold"`
}

func DefaultContentModerationSettings() ContentModerationSettings {
	return ContentModerationSettings{
		Enabled:            false,
		HumanMuteThreshold: 3,
	}
}

func GetContentModerationSettings() (ContentModerationSettings, error) {
	now := contentModerationSettingsNow()
	if settings, ok := getContentModerationSettingsFromCache(now); ok {
		return settings, nil
	}

	settings := DefaultContentModerationSettings()
	var row model.SystemSetting
	if err := store.DB.First(&row, "key = ?", contentModerationSettingKey).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			settings = NormalizeContentModerationSettings(settings)
			setContentModerationSettingsCache(settings, now)
			return settings, nil
		}
		return ContentModerationSettings{}, err
	}
	if len(row.Value) > 0 {
		if err := json.Unmarshal(row.Value, &settings); err != nil {
			return ContentModerationSettings{}, err
		}
	}
	settings = NormalizeContentModerationSettings(settings)
	setContentModerationSettingsCache(settings, now)
	return settings, nil
}

func SaveContentModerationSettings(settings ContentModerationSettings, updatedBy *int64) error {
	settings = NormalizeContentModerationSettings(settings)

	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}

	row := model.SystemSetting{
		Key:       contentModerationSettingKey,
		Value:     datatypes.JSON(raw),
		UpdatedBy: updatedBy,
	}
	if err := store.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_by", "updated_at"}),
	}).Create(&row).Error; err != nil {
		return err
	}

	setContentModerationSettingsCache(settings, contentModerationSettingsNow())
	return nil
}

func InvalidateContentModerationSettingsCache() {
	contentModerationSettingsCache.mu.Lock()
	contentModerationSettingsCache.loaded = false
	contentModerationSettingsCache.expiresAt = time.Time{}
	contentModerationSettingsCache.value = ContentModerationSettings{}
	contentModerationSettingsCache.mu.Unlock()
}

func getContentModerationSettingsFromCache(now time.Time) (ContentModerationSettings, bool) {
	contentModerationSettingsCache.mu.RLock()
	defer contentModerationSettingsCache.mu.RUnlock()

	if !contentModerationSettingsCache.loaded {
		return ContentModerationSettings{}, false
	}
	if now.After(contentModerationSettingsCache.expiresAt) {
		return ContentModerationSettings{}, false
	}
	return contentModerationSettingsCache.value, true
}

func setContentModerationSettingsCache(settings ContentModerationSettings, now time.Time) {
	contentModerationSettingsCache.mu.Lock()
	contentModerationSettingsCache.value = settings
	contentModerationSettingsCache.expiresAt = now.Add(contentModerationSettingsCacheTTL)
	contentModerationSettingsCache.loaded = true
	contentModerationSettingsCache.mu.Unlock()
}

func NormalizeContentModerationSettings(settings ContentModerationSettings) ContentModerationSettings {
	defaults := DefaultContentModerationSettings()
	if settings.HumanMuteThreshold <= 0 {
		settings.HumanMuteThreshold = defaults.HumanMuteThreshold
	}
	settings.Keywords = normalizeContentModerationKeywords(settings.Keywords)
	return settings
}

func normalizeContentModerationKeywords(keywords []string) []string {
	if len(keywords) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(keywords))
	normalized := make([]string, 0, len(keywords))
	for _, keyword := range keywords {
		value := strings.ToLower(strings.TrimSpace(keyword))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}
