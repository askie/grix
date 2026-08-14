package systemsetting

import (
	"context"
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

const authSettingKey = "auth"
const authSettingsCacheTTL = time.Minute

var authSettingsNow = time.Now

var authSettingsCache struct {
	mu        sync.RWMutex
	value     AuthSettings
	expiresAt time.Time
	loaded    bool
}

type AuthSettings struct {
	AutoAddCustomerUserID int64 `json:"auto_add_customer_user_id"`
}

func DefaultAuthSettings() AuthSettings {
	return AuthSettings{}
}

func GetAuthSettings() (AuthSettings, error) {
	return GetAuthSettingsWithContext(context.Background())
}

func GetAuthSettingsWithContext(ctx context.Context) (AuthSettings, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	now := authSettingsNow()
	if settings, ok := getAuthSettingsFromCache(now); ok {
		return settings, nil
	}

	settings := DefaultAuthSettings()
	var row model.SystemSetting
	if err := store.DB.WithContext(ctx).First(&row, "key = ?", authSettingKey).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			setAuthSettingsCache(settings, now)
			return settings, nil
		}
		return AuthSettings{}, err
	}
	if len(row.Value) == 0 {
		setAuthSettingsCache(settings, now)
		return settings, nil
	}
	if err := json.Unmarshal(row.Value, &settings); err != nil {
		return AuthSettings{}, err
	}
	setAuthSettingsCache(settings, now)
	return settings, nil
}

func SaveAuthSettings(settings AuthSettings, updatedBy *int64) error {
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}

	row := model.SystemSetting{
		Key:       authSettingKey,
		Value:     datatypes.JSON(raw),
		UpdatedBy: updatedBy,
	}
	if err := store.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_by", "updated_at"}),
	}).Create(&row).Error; err != nil {
		return err
	}
	setAuthSettingsCache(settings, authSettingsNow())
	return nil
}

func InvalidateAuthSettingsCache() {
	authSettingsCache.mu.Lock()
	authSettingsCache.loaded = false
	authSettingsCache.expiresAt = time.Time{}
	authSettingsCache.value = AuthSettings{}
	authSettingsCache.mu.Unlock()
}

func getAuthSettingsFromCache(now time.Time) (AuthSettings, bool) {
	authSettingsCache.mu.RLock()
	defer authSettingsCache.mu.RUnlock()

	if !authSettingsCache.loaded {
		return AuthSettings{}, false
	}
	if now.After(authSettingsCache.expiresAt) {
		return AuthSettings{}, false
	}
	return authSettingsCache.value, true
}

func setAuthSettingsCache(settings AuthSettings, now time.Time) {
	authSettingsCache.mu.Lock()
	authSettingsCache.value = settings
	authSettingsCache.expiresAt = now.Add(authSettingsCacheTTL)
	authSettingsCache.loaded = true
	authSettingsCache.mu.Unlock()
}
