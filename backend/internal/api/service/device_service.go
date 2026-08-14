package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/textutil"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// maxChannelTrailLogLen 限制 channelTrail 打入日志的长度，防止客户端传入超长字符串把日志刷爆。
const maxChannelTrailLogLen = 256

func DeviceBind(userID int64, platform, pushEnv, deviceToken, deviceID, channelTrail string) error {
	binding, err := normalizeDeviceBinding(platform, pushEnv, deviceToken, deviceID)
	if err != nil {
		return err
	}

	// channelTrail 非空说明客户端推送通道降级链上有过失败：先尝试了别的通道
	// （多半是厂商通道，如 android_huawei）才落到 binding.platform。
	// 只在真发生过降级时打日志，避免日常正常注册把日志刷满。
	if trail := strings.TrimSpace(channelTrail); trail != "" {
		logger.L.Warnf("push channel fallback user=%d final_platform=%s trail=%s", userID, binding.platform, textutil.TruncateRunes(trail, maxChannelTrailLogLen))
	}

	if err := deactivateConflictingDeviceBindings(userID, binding); err != nil {
		return err
	}

	device := model.Device{
		UserID:      userID,
		Platform:    binding.platform,
		PushEnv:     binding.pushEnv,
		DeviceToken: binding.deviceToken,
		DeviceID:    binding.deviceID,
		IsActive:    true,
	}

	// Upsert: if same user+platform+token exists, update
	err = store.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "user_id"},
			{Name: "platform"},
			{Name: "push_env"},
			{Name: "device_token"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"is_active", "device_id", "updated_at"}),
	}).Create(&device).Error
	if err != nil {
		return err
	}

	if err := deactivateOldTokensForUser(userID, binding); err != nil {
		return err
	}

	if err := cacheUserDeviceBinding(userID, binding); err != nil {
		return err
	}

	return nil
}

func deactivateConflictingDeviceBindings(userID int64, binding normalizedDeviceBinding) error {
	var conflictingDevices []model.Device
	if err := store.DB.
		Select("id", "user_id", "device_id").
		Where(
			`user_id <> ? AND is_active = true AND (
				device_id = ? OR
				(platform = ? AND push_env = ? AND device_token = ?)
			)`,
			userID,
			binding.deviceID,
			binding.platform,
			binding.pushEnv,
			binding.deviceToken,
		).
		Find(&conflictingDevices).Error; err != nil {
		return err
	}
	if len(conflictingDevices) == 0 {
		return nil
	}

	ids := make([]int64, 0, len(conflictingDevices))
	for _, device := range conflictingDevices {
		ids = append(ids, device.ID)
	}

	if err := store.DB.Model(&model.Device{}).
		Where("id IN ?", ids).
		Update("is_active", false).Error; err != nil {
		return err
	}

	return removeUserDeviceCacheEntries(conflictingDevices)
}

func deactivateOldTokensForUser(userID int64, binding normalizedDeviceBinding) error {
	// Deactivate old tokens only within the same platform+push_env namespace.
	return store.DB.Model(&model.Device{}).
		Where(
			"user_id = ? AND platform = ? AND push_env = ? AND device_token != ? AND is_active = true",
			userID,
			binding.platform,
			binding.pushEnv,
			binding.deviceToken,
		).
		Update("is_active", false).Error
}

func cacheUserDeviceBinding(userID int64, binding normalizedDeviceBinding) error {
	if store.RDB == nil {
		return nil
	}

	devInfo, err := json.Marshal(map[string]interface{}{
		"platform":     binding.platform,
		"push_env":     binding.pushEnv,
		"device_token": binding.deviceToken,
	})
	if err != nil {
		return err
	}
	return store.RDB.HSet(
		context.Background(),
		fmt.Sprintf("im:user:devices:%d", userID),
		binding.deviceID,
		string(devInfo),
	).Err()
}

func deactivateUserDeviceBinding(userID int64, deviceID string) error {
	normalizedDeviceID := strings.TrimSpace(deviceID)
	if userID <= 0 || normalizedDeviceID == "" {
		return nil
	}

	if err := store.DB.Model(&model.Device{}).
		Where("user_id = ? AND device_id = ? AND is_active = true", userID, normalizedDeviceID).
		Update("is_active", false).Error; err != nil {
		return err
	}

	if store.RDB == nil {
		return nil
	}

	return store.RDB.HDel(
		context.Background(),
		fmt.Sprintf("im:user:devices:%d", userID),
		normalizedDeviceID,
	).Err()
}

// DeactivateOtherUsersDeviceBindingsTx 在某账号于本设备登录时，将同一物理设备
// （device_id 相同）上属于其他账号的推送绑定置为失效。这样后端给原账号推送选设备时
// 就不会再命中这台已易主的设备，从源头避免把消息推给已登录别人的设备。
//
// 注意：APNs/FCM 的设备 token 是按 App 安装维度、跨账号共用的，给原账号推送等价于
// 推到这台设备。本函数收紧的是登录这一刻起的绑定归属；登录前已交付给推送服务商的
// "在途"通知无法事后撤回，仍需客户端按 recipient_id 兜底丢弃。
func DeactivateOtherUsersDeviceBindingsTx(tx *gorm.DB, userID int64, deviceID string) ([]model.Device, error) {
	normalizedDeviceID := strings.TrimSpace(deviceID)
	if userID <= 0 || normalizedDeviceID == "" {
		return nil, nil
	}

	var staleDevices []model.Device
	if err := tx.Model(&model.Device{}).
		Select("id", "user_id", "device_id").
		Where("device_id = ? AND user_id != ? AND is_active = true", normalizedDeviceID, userID).
		Find(&staleDevices).Error; err != nil {
		return nil, err
	}
	if len(staleDevices) == 0 {
		return nil, nil
	}

	ids := make([]int64, 0, len(staleDevices))
	for _, device := range staleDevices {
		ids = append(ids, device.ID)
	}
	if err := tx.Model(&model.Device{}).
		Where("id IN ?", ids).
		Update("is_active", false).Error; err != nil {
		return nil, err
	}
	return staleDevices, nil
}

func removeUserDeviceCacheEntries(devices []model.Device) error {
	if store.RDB == nil || len(devices) == 0 {
		return nil
	}

	userDeviceIDs := make(map[int64]map[string]struct{}, len(devices))
	for _, device := range devices {
		normalizedDeviceID := strings.TrimSpace(device.DeviceID)
		if normalizedDeviceID == "" {
			continue
		}
		if _, exists := userDeviceIDs[device.UserID]; !exists {
			userDeviceIDs[device.UserID] = make(map[string]struct{})
		}
		userDeviceIDs[device.UserID][normalizedDeviceID] = struct{}{}
	}

	ctx := context.Background()
	for userID, deviceIDs := range userDeviceIDs {
		if len(deviceIDs) == 0 {
			continue
		}
		fields := make([]string, 0, len(deviceIDs))
		for deviceID := range deviceIDs {
			fields = append(fields, deviceID)
		}
		if err := store.RDB.HDel(ctx, fmt.Sprintf("im:user:devices:%d", userID), fields...).Err(); err != nil {
			return err
		}
	}

	return nil
}
