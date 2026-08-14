package systemsetting

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const groupSettingKey = "group"
const groupSettingsCacheTTL = time.Minute

var groupSettingsNow = time.Now

var groupSettingsCache struct {
	mu        sync.RWMutex
	value     GroupSettings
	expiresAt time.Time
	loaded    bool
}

type GroupSettings struct {
	MemberInviteThreshold int `json:"member_invite_threshold"`
}

func DefaultGroupSettings() GroupSettings {
	return GroupSettings{
		MemberInviteThreshold: 20,
	}
}

func GetGroupSettings() (GroupSettings, error) {
	now := groupSettingsNow()
	if settings, ok := getGroupSettingsFromCache(now); ok {
		return settings, nil
	}

	settings := DefaultGroupSettings()
	var row model.SystemSetting
	if err := store.DB.First(&row, "key = ?", groupSettingKey).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			setGroupSettingsCache(settings, now)
			return settings, nil
		}
		return GroupSettings{}, err
	}
	if len(row.Value) == 0 {
		setGroupSettingsCache(settings, now)
		return settings, nil
	}
	if err := json.Unmarshal(row.Value, &settings); err != nil {
		return GroupSettings{}, err
	}
	setGroupSettingsCache(settings, now)
	return settings, nil
}

func SaveGroupSettings(settings GroupSettings, updatedBy *int64) error {
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}

	row := model.SystemSetting{
		Key:       groupSettingKey,
		Value:     datatypes.JSON(raw),
		UpdatedBy: updatedBy,
	}
	if err := store.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_by", "updated_at"}),
	}).Create(&row).Error; err != nil {
		return err
	}
	setGroupSettingsCache(settings, groupSettingsNow())
	return nil
}

func InvalidateGroupSettingsCache() {
	groupSettingsCache.mu.Lock()
	groupSettingsCache.loaded = false
	groupSettingsCache.expiresAt = time.Time{}
	groupSettingsCache.value = GroupSettings{}
	groupSettingsCache.mu.Unlock()
}

func getGroupSettingsFromCache(now time.Time) (GroupSettings, bool) {
	groupSettingsCache.mu.RLock()
	defer groupSettingsCache.mu.RUnlock()

	if !groupSettingsCache.loaded {
		return GroupSettings{}, false
	}
	if now.After(groupSettingsCache.expiresAt) {
		return GroupSettings{}, false
	}
	return groupSettingsCache.value, true
}

func setGroupSettingsCache(settings GroupSettings, now time.Time) {
	groupSettingsCache.mu.Lock()
	groupSettingsCache.value = settings
	groupSettingsCache.expiresAt = now.Add(groupSettingsCacheTTL)
	groupSettingsCache.loaded = true
	groupSettingsCache.mu.Unlock()
}
