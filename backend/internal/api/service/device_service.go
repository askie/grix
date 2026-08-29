package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/textutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// errPushChannelDisabled 表示设备想绑定的推送通道被塘主关掉了。
// 客户端据此把该通道排除后沿降级链继续取下一条通道的 token，而不是把设备绑在一条投不出去的通道上。
var errPushChannelDisabled = errors.New("push channel disabled")

func IsPushChannelDisabled(err error) bool {
	return errors.Is(err, errPushChannelDisabled)
}

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

	// 通道被关时直接拒绝绑定，并把该设备上这条通道的存量绑定置为失效：
	// 投递侧遇到关闭的通道只会跳过，不会自行改投别的通道，
	// 留着绑定等于让这台设备彻底收不到离线推送。
	if err := rejectDisabledPushChannel(userID, binding); err != nil {
		return err
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

func rejectDisabledPushChannel(userID int64, binding normalizedDeviceBinding) error {
	channels, err := systemsetting.GetPushChannelSettings()
	if err != nil {
		// 读不到开关时按历史行为放行，避免设置服务抖动把所有设备注册一起打挂。
		if logger.L != nil {
			logger.L.Warnf("load push channel settings failed, allow bind user=%d platform=%s: %v", userID, binding.platform, err)
		}
		return nil
	}
	if channels.EnabledFor(binding.platform) {
		return nil
	}

	if err := deactivateUserDevicePlatformBinding(userID, binding); err != nil {
		return err
	}
	if logger.L != nil {
		logger.L.Infof("reject device bind on disabled channel user=%d platform=%s", userID, binding.platform)
	}
	return errPushChannelDisabled
}

// deactivateUserDevicePlatformBinding 只失效该用户在这台设备上这条通道的绑定，
// 不动同一设备上其它通道的绑定（客户端马上就会带着下一条通道的 token 再来注册）。
func deactivateUserDevicePlatformBinding(userID int64, binding normalizedDeviceBinding) error {
	var stale []model.Device
	if err := store.DB.
		Select("id", "user_id", "device_id").
		Where(
			"user_id = ? AND device_id = ? AND platform = ? AND is_active = true",
			userID,
			binding.deviceID,
			binding.platform,
		).
		Find(&stale).Error; err != nil {
		return err
	}
	if len(stale) == 0 {
		return nil
	}

	ids := make([]int64, 0, len(stale))
	for _, device := range stale {
		ids = append(ids, device.ID)
	}
	if err := store.DB.Model(&model.Device{}).
		Where("id IN ?", ids).
		Update("is_active", false).Error; err != nil {
		return err
	}
	return removeUserDeviceCacheEntries(stale)
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
