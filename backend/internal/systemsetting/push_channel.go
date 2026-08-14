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

const pushChannelSettingKey = "push_channel"
const pushChannelSettingsCacheTTL = time.Minute

var pushChannelSettingsNow = time.Now

var pushChannelSettingsCache struct {
	mu        sync.RWMutex
	value     PushChannelSettings
	expiresAt time.Time
	loaded    bool
}

// PushChannelSettings 控制各离线推送通道是否启用。
// 默认全部启用；塘主可在国内连不上 Google 时单独关掉 AndroidFCM / WebPush。
type PushChannelSettings struct {
	IOSAPNsEnabled    bool `json:"ios_apn_enabled"`
	AndroidFCMEnabled bool `json:"android_fcm_enabled"`
	WebPushEnabled    bool `json:"web_push_enabled"`
	JPushEnabled      bool `json:"jpush_enabled"`

	// 国产厂商系统级推送通道开关。
	AndroidHuaweiEnabled bool `json:"android_huawei_enabled"`
	AndroidHonorEnabled  bool `json:"android_honor_enabled"`
	AndroidXiaomiEnabled bool `json:"android_xiaomi_enabled"`
	AndroidOppoEnabled   bool `json:"android_oppo_enabled"`
	AndroidVivoEnabled   bool `json:"android_vivo_enabled"`
}

// DefaultPushChannelSettings 默认全部通道开启，保证未配置时行为与历史一致。
func DefaultPushChannelSettings() PushChannelSettings {
	return PushChannelSettings{
		IOSAPNsEnabled:    true,
		AndroidFCMEnabled: true,
		WebPushEnabled:    true,
		JPushEnabled:      true,

		AndroidHuaweiEnabled: true,
		AndroidHonorEnabled:  true,
		AndroidXiaomiEnabled: true,
		AndroidOppoEnabled:   true,
		AndroidVivoEnabled:   true,
	}
}

// GetPushChannelSettings 读取通道开关（带 1 分钟缓存）。
// 记录不存在或字段缺失时回落到默认（开启），因此 push 服务无需重启即可在 1 分钟内感知塘主的变更。
func GetPushChannelSettings() (PushChannelSettings, error) {
	now := pushChannelSettingsNow()
	if settings, ok := getPushChannelSettingsFromCache(now); ok {
		return settings, nil
	}
	return loadPushChannelSettings(now)
}

// GetPushChannelSettingsFresh 绕过缓存直接读库。
// 写接口须用它取存量做打底：多副本下缓存可能是别的 pod 一分钟前的值，
// 拿旧值打底会把这期间其它管理员的变更覆盖回去。
func GetPushChannelSettingsFresh() (PushChannelSettings, error) {
	return loadPushChannelSettings(pushChannelSettingsNow())
}

func loadPushChannelSettings(now time.Time) (PushChannelSettings, error) {
	settings := DefaultPushChannelSettings()
	var row model.SystemSetting
	if err := store.DB.First(&row, "key = ?", pushChannelSettingKey).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			setPushChannelSettingsCache(settings, now)
			return settings, nil
		}
		return PushChannelSettings{}, err
	}
	if len(row.Value) == 0 {
		setPushChannelSettingsCache(settings, now)
		return settings, nil
	}
	// 先以默认值打底再反序列化覆盖：DB 中缺失的字段保持默认开启。
	if err := json.Unmarshal(row.Value, &settings); err != nil {
		return PushChannelSettings{}, err
	}
	setPushChannelSettingsCache(settings, now)
	return settings, nil
}

func SavePushChannelSettings(settings PushChannelSettings, updatedBy *int64) error {
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}

	row := model.SystemSetting{
		Key:       pushChannelSettingKey,
		Value:     datatypes.JSON(raw),
		UpdatedBy: updatedBy,
	}
	if err := store.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_by", "updated_at"}),
	}).Create(&row).Error; err != nil {
		return err
	}
	setPushChannelSettingsCache(settings, pushChannelSettingsNow())
	return nil
}

func InvalidatePushChannelSettingsCache() {
	pushChannelSettingsCache.mu.Lock()
	pushChannelSettingsCache.loaded = false
	pushChannelSettingsCache.expiresAt = time.Time{}
	pushChannelSettingsCache.value = PushChannelSettings{}
	pushChannelSettingsCache.mu.Unlock()
}

func getPushChannelSettingsFromCache(now time.Time) (PushChannelSettings, bool) {
	pushChannelSettingsCache.mu.RLock()
	defer pushChannelSettingsCache.mu.RUnlock()

	if !pushChannelSettingsCache.loaded {
		return PushChannelSettings{}, false
	}
	if now.After(pushChannelSettingsCache.expiresAt) {
		return PushChannelSettings{}, false
	}
	return pushChannelSettingsCache.value, true
}

func setPushChannelSettingsCache(settings PushChannelSettings, now time.Time) {
	pushChannelSettingsCache.mu.Lock()
	pushChannelSettingsCache.value = settings
	pushChannelSettingsCache.expiresAt = now.Add(pushChannelSettingsCacheTTL)
	pushChannelSettingsCache.loaded = true
	pushChannelSettingsCache.mu.Unlock()
}
