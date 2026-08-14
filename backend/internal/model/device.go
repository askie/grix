package model

import "time"

const (
	DevicePlatformIOS          = "ios"
	DevicePlatformAndroidFCM   = "android_fcm"
	DevicePlatformAndroidJPush = "android_jpush"
	DevicePlatformWebPush      = "web_push"

	// 国产厂商系统级推送通道。设备按 ROM 品牌绑定其中之一，
	// 应用进程被杀后仍由系统通道投递通知。
	DevicePlatformAndroidHuawei = "android_huawei"
	DevicePlatformAndroidHonor  = "android_honor"
	DevicePlatformAndroidXiaomi = "android_xiaomi"
	DevicePlatformAndroidOppo   = "android_oppo"
	DevicePlatformAndroidVivo   = "android_vivo"

	DevicePushEnvDefault        = "default"
	DevicePushEnvAPNsSandbox    = "apns_sandbox"
	DevicePushEnvAPNsProduction = "apns_production"
	DevicePushEnvUnknown        = "unknown"
)

// AndroidVendorPlatforms 列出全部国产厂商推送平台标识。
var AndroidVendorPlatforms = []string{
	DevicePlatformAndroidHuawei,
	DevicePlatformAndroidHonor,
	DevicePlatformAndroidXiaomi,
	DevicePlatformAndroidOppo,
	DevicePlatformAndroidVivo,
}

// IsAndroidVendorPlatform 判断平台标识是否为国产厂商推送通道。
func IsAndroidVendorPlatform(platform string) bool {
	for _, p := range AndroidVendorPlatforms {
		if p == platform {
			return true
		}
	}
	return false
}

type Device struct {
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID      int64     `gorm:"not null;uniqueIndex:idx_unique_devices" json:"user_id"`
	Platform    string    `gorm:"size:20;not null;uniqueIndex:idx_unique_devices" json:"platform"`
	PushEnv     string    `gorm:"size:32;not null;uniqueIndex:idx_unique_devices" json:"push_env"`
	DeviceToken string    `gorm:"size:2048;not null;uniqueIndex:idx_unique_devices" json:"device_token"`
	DeviceID    string    `gorm:"size:100" json:"device_id"`
	IsActive    bool      `gorm:"default:true" json:"is_active"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (Device) TableName() string { return "devices" }
