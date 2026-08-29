package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
)

func init() {
	_ = snowflake.Init(1)
}

func setupDeviceTest(t *testing.T) (*testutil.TestDB, func()) {
	t.Helper()
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	return testDB, func() { testDB.Close() }
}

func TestDeviceBind(t *testing.T) {
	testDB, cleanup := setupDeviceTest(t)
	defer cleanup()

	userID := int64(8001)

	t.Run("bind new ios sandbox device", func(t *testing.T) {
		err := DeviceBind(userID, model.DevicePlatformIOS, model.DevicePushEnvAPNsSandbox, "token-abc-123", "device-001", "")
		if err != nil {
			t.Fatalf("DeviceBind error = %v", err)
		}

		var found model.Device
		testDB.DB.Where("user_id = ? AND platform = ? AND push_env = ?", userID, model.DevicePlatformIOS, model.DevicePushEnvAPNsSandbox).First(&found)

		if found.DeviceToken != "token-abc-123" {
			t.Errorf("expected token 'token-abc-123', got '%s'", found.DeviceToken)
		}
		if found.PushEnv != model.DevicePushEnvAPNsSandbox {
			t.Errorf("expected push_env %q, got %q", model.DevicePushEnvAPNsSandbox, found.PushEnv)
		}
		if !found.IsActive {
			t.Error("expected device to be active")
		}
	})

	t.Run("deactivate old token only in same env", func(t *testing.T) {
		if err := testDB.DB.Create(&model.Device{
			UserID:      userID,
			Platform:    model.DevicePlatformIOS,
			PushEnv:     model.DevicePushEnvAPNsSandbox,
			DeviceToken: "old-sandbox-token",
			DeviceID:    "old-sandbox-device",
			IsActive:    true,
		}).Error; err != nil {
			t.Fatalf("seed old sandbox device error = %v", err)
		}
		if err := testDB.DB.Create(&model.Device{
			UserID:      userID,
			Platform:    model.DevicePlatformIOS,
			PushEnv:     model.DevicePushEnvAPNsProduction,
			DeviceToken: "prod-token",
			DeviceID:    "prod-device",
			IsActive:    true,
		}).Error; err != nil {
			t.Fatalf("seed prod device error = %v", err)
		}

		if err := DeviceBind(userID, model.DevicePlatformIOS, model.DevicePushEnvAPNsSandbox, "new-sandbox-token", "new-sandbox-device", ""); err != nil {
			t.Fatalf("DeviceBind error = %v", err)
		}

		var found model.Device
		testDB.DB.Where("user_id = ? AND device_token = ?", userID, "old-sandbox-token").First(&found)
		if found.IsActive {
			t.Error("expected old device to be deactivated")
		}

		var prodDevice model.Device
		testDB.DB.Where("user_id = ? AND device_token = ?", userID, "prod-token").First(&prodDevice)
		if !prodDevice.IsActive {
			t.Error("expected production device to remain active")
		}
	})

	t.Run("redis cache update", func(t *testing.T) {
		deviceID := "test-device-redis"
		err := DeviceBind(userID, model.DevicePlatformAndroidFCM, model.DevicePushEnvDefault, "token123", deviceID, "")
		if err != nil {
			t.Fatalf("DeviceBind error = %v", err)
		}

		val, err := store.RDB.HGet(context.Background(), "im:user:devices:8001", deviceID).Result()
		if err != nil {
			t.Fatalf("HGet error = %v", err)
		}

		var devInfo map[string]string
		if err := json.Unmarshal([]byte(val), &devInfo); err != nil {
			t.Fatalf("unmarshal redis device info error = %v", err)
		}
		if devInfo["platform"] != model.DevicePlatformAndroidFCM {
			t.Errorf("expected platform %q, got %q", model.DevicePlatformAndroidFCM, devInfo["platform"])
		}
		if devInfo["push_env"] != model.DevicePushEnvDefault {
			t.Errorf("expected push_env %q, got %q", model.DevicePushEnvDefault, devInfo["push_env"])
		}
		if devInfo["device_token"] != "token123" {
			t.Errorf("expected device_token %q, got %q", "token123", devInfo["device_token"])
		}
	})

	t.Run("bind should cleanup conflicting bindings from other users", func(t *testing.T) {
		oldUserID := int64(8101)
		newUserID := int64(8102)
		deviceID := "shared-ios-device"
		deviceToken := "shared-ios-token"

		if err := testDB.DB.Create(&model.Device{
			UserID:      oldUserID,
			Platform:    model.DevicePlatformIOS,
			PushEnv:     model.DevicePushEnvAPNsSandbox,
			DeviceToken: deviceToken,
			DeviceID:    deviceID,
			IsActive:    true,
		}).Error; err != nil {
			t.Fatalf("seed old user device error = %v", err)
		}

		if err := store.RDB.HSet(
			context.Background(),
			fmt.Sprintf("im:user:devices:%d", oldUserID),
			deviceID,
			`{"platform":"ios","push_env":"apns_sandbox","device_token":"shared-ios-token"}`,
		).Err(); err != nil {
			t.Fatalf("seed old user redis device cache error = %v", err)
		}

		if err := DeviceBind(
			newUserID,
			model.DevicePlatformIOS,
			model.DevicePushEnvAPNsSandbox,
			deviceToken,
			deviceID,
			"",
		); err != nil {
			t.Fatalf("DeviceBind error = %v", err)
		}

		var oldDevice model.Device
		if err := testDB.DB.
			Where("user_id = ? AND device_id = ?", oldUserID, deviceID).
			First(&oldDevice).Error; err != nil {
			t.Fatalf("query old user device error = %v", err)
		}
		if oldDevice.IsActive {
			t.Fatal("expected old user's conflicting device to be inactive")
		}

		if exists := store.RDB.HExists(
			context.Background(),
			fmt.Sprintf("im:user:devices:%d", oldUserID),
			deviceID,
		).Val(); exists {
			t.Fatal("expected old user's redis device cache entry removed")
		}

		var newDevice model.Device
		if err := testDB.DB.
			Where("user_id = ? AND device_id = ?", newUserID, deviceID).
			First(&newDevice).Error; err != nil {
			t.Fatalf("query new user device error = %v", err)
		}
		if !newDevice.IsActive {
			t.Fatal("expected new user's device to stay active")
		}
	})
}

func TestNormalizeDeviceBindingRejectsInvalidPushEnv(t *testing.T) {
	tests := []struct {
		name      string
		platform  string
		pushEnv   string
		wantError bool
	}{
		{
			name:      "ios sandbox accepted",
			platform:  model.DevicePlatformIOS,
			pushEnv:   model.DevicePushEnvAPNsSandbox,
			wantError: false,
		},
		{
			name:      "ios default rejected",
			platform:  model.DevicePlatformIOS,
			pushEnv:   model.DevicePushEnvDefault,
			wantError: true,
		},
		{
			name:      "android default accepted",
			platform:  model.DevicePlatformAndroidFCM,
			pushEnv:   model.DevicePushEnvDefault,
			wantError: false,
		},
		{
			name:      "android apns env rejected",
			platform:  model.DevicePlatformAndroidFCM,
			pushEnv:   model.DevicePushEnvAPNsProduction,
			wantError: true,
		},
		{
			name:      "web push default accepted",
			platform:  model.DevicePlatformWebPush,
			pushEnv:   model.DevicePushEnvDefault,
			wantError: false,
		},
		{
			name:      "web push apns env rejected",
			platform:  model.DevicePlatformWebPush,
			pushEnv:   model.DevicePushEnvAPNsProduction,
			wantError: true,
		},
		{
			name:      "vendor huawei default accepted",
			platform:  model.DevicePlatformAndroidHuawei,
			pushEnv:   model.DevicePushEnvDefault,
			wantError: false,
		},
		{
			name:      "vendor xiaomi default accepted",
			platform:  model.DevicePlatformAndroidXiaomi,
			pushEnv:   model.DevicePushEnvDefault,
			wantError: false,
		},
		{
			name:      "vendor honor default accepted",
			platform:  model.DevicePlatformAndroidHonor,
			pushEnv:   model.DevicePushEnvDefault,
			wantError: false,
		},
		{
			name:      "vendor oppo default accepted",
			platform:  model.DevicePlatformAndroidOppo,
			pushEnv:   model.DevicePushEnvDefault,
			wantError: false,
		},
		{
			name:      "vendor vivo default accepted",
			platform:  model.DevicePlatformAndroidVivo,
			pushEnv:   model.DevicePushEnvDefault,
			wantError: false,
		},
		{
			name:      "vendor apns env rejected",
			platform:  model.DevicePlatformAndroidHuawei,
			pushEnv:   model.DevicePushEnvAPNsProduction,
			wantError: true,
		},
		{
			name:      "unknown platform still rejected",
			platform:  "android_meizu",
			pushEnv:   model.DevicePushEnvDefault,
			wantError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := normalizeDeviceBinding(tc.platform, tc.pushEnv, "token-1", "device-1")
			if tc.wantError && err == nil {
				t.Fatal("expected validation error")
			}
			if !tc.wantError && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestDeviceModel(t *testing.T) {
	testDB, cleanup := setupDeviceTest(t)
	defer cleanup()

	t.Run("create and query device", func(t *testing.T) {
		device := model.Device{
			UserID:      1001,
			Platform:    model.DevicePlatformIOS,
			PushEnv:     model.DevicePushEnvAPNsProduction,
			DeviceToken: "test-token",
			DeviceID:    "test-device-id",
			IsActive:    true,
		}

		err := testDB.DB.Create(&device).Error
		if err != nil {
			t.Fatalf("create device error = %v", err)
		}

		var found model.Device
		testDB.DB.Where("user_id = ? AND platform = ?", 1001, model.DevicePlatformIOS).First(&found)

		if found.DeviceToken != "test-token" {
			t.Errorf("expected token 'test-token', got '%s'", found.DeviceToken)
		}
		if found.DeviceID != "test-device-id" {
			t.Errorf("expected device_id 'test-device-id', got '%s'", found.DeviceID)
		}
	})

	t.Run("device table name", func(t *testing.T) {
		device := model.Device{}
		if device.TableName() != "devices" {
			t.Errorf("expected table name 'devices', got '%s'", device.TableName())
		}
	})

	t.Run("multiple devices per user", func(t *testing.T) {
		userID := int64(2001)

		// Create multiple devices for same user
		devices := []model.Device{
			{UserID: userID, Platform: model.DevicePlatformIOS, PushEnv: model.DevicePushEnvAPNsSandbox, DeviceToken: "ios-sandbox-token", IsActive: true},
			{UserID: userID, Platform: model.DevicePlatformIOS, PushEnv: model.DevicePushEnvAPNsProduction, DeviceToken: "ios-production-token", IsActive: true},
			{UserID: userID, Platform: model.DevicePlatformAndroidFCM, PushEnv: model.DevicePushEnvDefault, DeviceToken: "android-token", IsActive: true},
		}

		for _, d := range devices {
			testDB.DB.Create(&d)
		}

		var count int64
		testDB.DB.Model(&model.Device{}).Where("user_id = ?", userID).Count(&count)

		if count != 3 {
			t.Errorf("expected 3 devices, got %d", count)
		}
	})
}

func TestDeviceEmptyValues(t *testing.T) {
	_, cleanup := setupDeviceTest(t)
	defer cleanup()

	t.Run("empty device id allowed", func(t *testing.T) {
		device := model.Device{
			UserID:      3001,
			Platform:    model.DevicePlatformIOS,
			PushEnv:     model.DevicePushEnvAPNsSandbox,
			DeviceToken: "token-test",
			DeviceID:    "", // Empty device ID should be allowed
			IsActive:    true,
		}

		err := store.DB.Create(&device).Error
		if err != nil {
			t.Fatalf("create with empty device_id error = %v", err)
		}
	})
}

func TestDeactivateOtherUsersDeviceBindingsTx(t *testing.T) {
	testDB, cleanup := setupDeviceTest(t)
	defer cleanup()

	const (
		userA    = int64(9101) // 原账号
		userB    = int64(9102) // 新登录账号
		deviceID = "shared-device-001"
		otherDev = "other-device-002"
	)

	// userA 在共享设备上的活跃推送绑定（应被失效）。
	if err := DeviceBind(userA, model.DevicePlatformIOS, model.DevicePushEnvAPNsSandbox, "token-a", deviceID, ""); err != nil {
		t.Fatalf("seed userA binding: %v", err)
	}
	// userB 在另一台设备上的活跃绑定（不应被动）。
	if err := DeviceBind(userB, model.DevicePlatformIOS, model.DevicePushEnvAPNsSandbox, "token-b-other", otherDev, ""); err != nil {
		t.Fatalf("seed userB other-device binding: %v", err)
	}

	// 模拟 userB 在共享设备登录。
	stale, err := DeactivateOtherUsersDeviceBindingsTx(testDB.DB, userB, deviceID)
	if err != nil {
		t.Fatalf("DeactivateOtherUsersDeviceBindingsTx error = %v", err)
	}
	if len(stale) != 1 || stale[0].UserID != userA {
		t.Fatalf("expected userA binding to be returned as stale, got %#v", stale)
	}

	// userA 在共享设备上的绑定应已失效，后端推送将不再命中。
	var aDevice model.Device
	testDB.DB.Where("user_id = ? AND device_id = ?", userA, deviceID).First(&aDevice)
	if aDevice.IsActive {
		t.Fatalf("expected userA binding on shared device to be inactive")
	}

	// userB 在另一台设备上的绑定不受影响。
	var bOther model.Device
	testDB.DB.Where("user_id = ? AND device_id = ?", userB, otherDev).First(&bOther)
	if !bOther.IsActive {
		t.Fatalf("expected userB binding on other device to remain active")
	}

	// 幂等：再次执行无残留可清，返回空。
	again, err := DeactivateOtherUsersDeviceBindingsTx(testDB.DB, userB, deviceID)
	if err != nil {
		t.Fatalf("second call error = %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("expected no stale bindings on second call, got %#v", again)
	}
}

// 边界场景：同一账号在多台设备上拥有活跃绑定时，本函数按 device_id 维度只动其他账号，
// 不应失效同账号在另一台设备上的绑定。
func TestDeactivateOtherUsersDeviceBindingsTx_SameUserMultiDevice(t *testing.T) {
	testDB, cleanup := setupDeviceTest(t)
	defer cleanup()

	const (
		user      = int64(9201)
		deviceOne = "user-device-001"
		deviceTwo = "user-device-002"
	)
	// 直接构造同账号两台设备的活跃绑定（绕开 DeviceBind 内部的"同 platform 旧 token 互踢"逻辑），
	// 单测目标只关心 DeactivateOtherUsersDeviceBindingsTx 自身的判定。
	if err := testDB.DB.Create(&model.Device{
		UserID: user, Platform: model.DevicePlatformIOS, PushEnv: model.DevicePushEnvAPNsSandbox,
		DeviceToken: "tok-one", DeviceID: deviceOne, IsActive: true,
	}).Error; err != nil {
		t.Fatalf("seed deviceOne: %v", err)
	}
	if err := testDB.DB.Create(&model.Device{
		UserID: user, Platform: model.DevicePlatformIOS, PushEnv: model.DevicePushEnvAPNsSandbox,
		DeviceToken: "tok-two", DeviceID: deviceTwo, IsActive: true,
	}).Error; err != nil {
		t.Fatalf("seed deviceTwo: %v", err)
	}

	stale, err := DeactivateOtherUsersDeviceBindingsTx(testDB.DB, user, deviceTwo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("expected no stale bindings when same user, got %#v", stale)
	}

	var one, two model.Device
	testDB.DB.Where("user_id = ? AND device_id = ?", user, deviceOne).First(&one)
	testDB.DB.Where("user_id = ? AND device_id = ?", user, deviceTwo).First(&two)
	if !one.IsActive || !two.IsActive {
		t.Fatalf("expected both same-user bindings active, got one=%v two=%v", one.IsActive, two.IsActive)
	}
}

// 边界场景：deviceID 为空或仅空白时直接 no-op，不会误伤其他账号的绑定。
func TestDeactivateOtherUsersDeviceBindingsTx_BlankDeviceID(t *testing.T) {
	testDB, cleanup := setupDeviceTest(t)
	defer cleanup()

	const (
		userA    = int64(9301)
		userB    = int64(9302)
		deviceID = "real-device-009"
	)
	if err := DeviceBind(userA, model.DevicePlatformIOS, model.DevicePushEnvAPNsSandbox, "tok-a", deviceID, ""); err != nil {
		t.Fatalf("seed userA: %v", err)
	}

	for _, empty := range []string{"", "   "} {
		stale, err := DeactivateOtherUsersDeviceBindingsTx(testDB.DB, userB, empty)
		if err != nil {
			t.Fatalf("blank deviceID error = %v", err)
		}
		if len(stale) != 0 {
			t.Fatalf("expected no-op for blank deviceID %q, got %#v", empty, stale)
		}
	}

	var aDev model.Device
	testDB.DB.Where("user_id = ? AND device_id = ?", userA, deviceID).First(&aDev)
	if !aDev.IsActive {
		t.Fatalf("expected userA binding untouched when deviceID blank")
	}
}

// 边界场景：userID 非法（<=0）直接 no-op。
func TestDeactivateOtherUsersDeviceBindingsTx_InvalidUserID(t *testing.T) {
	testDB, cleanup := setupDeviceTest(t)
	defer cleanup()

	const (
		userA    = int64(9401)
		deviceID = "real-device-019"
	)
	if err := DeviceBind(userA, model.DevicePlatformIOS, model.DevicePushEnvAPNsSandbox, "tok-a", deviceID, ""); err != nil {
		t.Fatalf("seed userA: %v", err)
	}

	for _, badID := range []int64{0, -1} {
		stale, err := DeactivateOtherUsersDeviceBindingsTx(testDB.DB, badID, deviceID)
		if err != nil {
			t.Fatalf("invalid userID error = %v", err)
		}
		if len(stale) != 0 {
			t.Fatalf("expected no-op for userID %d, got %#v", badID, stale)
		}
	}

	var aDev model.Device
	testDB.DB.Where("user_id = ? AND device_id = ?", userA, deviceID).First(&aDev)
	if !aDev.IsActive {
		t.Fatalf("expected userA binding untouched when userID invalid")
	}
}

// 边界场景：其他账号已有 inactive 历史记录不应被纳入返回，也不会被重复 update。
func TestDeactivateOtherUsersDeviceBindingsTx_IgnoresInactiveHistory(t *testing.T) {
	testDB, cleanup := setupDeviceTest(t)
	defer cleanup()

	const (
		userA    = int64(9501)
		userB    = int64(9502)
		deviceID = "shared-device-099"
	)

	// userA 上一次在本设备登录留下的、已经手动注销过的失活记录。
	// 注意：IsActive 有 `gorm:"default:true"`，Create 时 zero value 会被默认值覆盖，
	// 因此先 Create 再用 Update 把 is_active 改成 false。
	historyRow := &model.Device{
		UserID:      userA,
		Platform:    model.DevicePlatformIOS,
		PushEnv:     model.DevicePushEnvAPNsSandbox,
		DeviceToken: "tok-stale",
		DeviceID:    deviceID,
	}
	if err := testDB.DB.Create(historyRow).Error; err != nil {
		t.Fatalf("seed inactive history: %v", err)
	}
	if err := testDB.DB.Model(&model.Device{}).Where("id = ?", historyRow.ID).
		Update("is_active", false).Error; err != nil {
		t.Fatalf("force inactive history: %v", err)
	}
	// sanity：DB 里这条记录现在确实是 inactive。
	var check model.Device
	if err := testDB.DB.First(&check, historyRow.ID).Error; err != nil || check.IsActive {
		t.Fatalf("inactive seed not persisted: err=%v active=%v", err, check.IsActive)
	}

	stale, err := DeactivateOtherUsersDeviceBindingsTx(testDB.DB, userB, deviceID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("expected inactive history to be ignored, got %#v", stale)
	}
}

func TestDeviceBindRejectsDisabledChannel(t *testing.T) {
	testDB, cleanup := setupDeviceTest(t)
	defer cleanup()

	// 用完恢复默认：通道开关有进程级缓存，脏值会污染同包其它用例。
	defer func() {
		if err := systemsetting.SavePushChannelSettings(systemsetting.DefaultPushChannelSettings(), nil); err != nil {
			t.Fatalf("restore push channel settings error = %v", err)
		}
	}()

	userID := int64(8301)
	deviceID := "android-device-8301"

	if err := DeviceBind(userID, model.DevicePlatformAndroidFCM, model.DevicePushEnvDefault, "fcm-token-1", deviceID, ""); err != nil {
		t.Fatalf("DeviceBind(fcm) error = %v", err)
	}

	disabled := systemsetting.DefaultPushChannelSettings()
	disabled.AndroidFCMEnabled = false
	if err := systemsetting.SavePushChannelSettings(disabled, nil); err != nil {
		t.Fatalf("SavePushChannelSettings() error = %v", err)
	}

	err := DeviceBind(userID, model.DevicePlatformAndroidFCM, model.DevicePushEnvDefault, "fcm-token-2", deviceID, "")
	if !IsPushChannelDisabled(err) {
		t.Fatalf("DeviceBind(disabled fcm) error = %v, want push channel disabled", err)
	}

	var activeFCM int64
	testDB.DB.Model(&model.Device{}).
		Where("user_id = ? AND platform = ? AND is_active = true", userID, model.DevicePlatformAndroidFCM).
		Count(&activeFCM)
	if activeFCM != 0 {
		t.Fatalf("active android_fcm bindings = %d, want 0", activeFCM)
	}

	// 客户端排除该通道后改用极光重新注册，必须能正常落库。
	if err := DeviceBind(userID, model.DevicePlatformAndroidJPush, model.DevicePushEnvDefault, "jpush-token-1", deviceID, "android_fcm:channel_disabled"); err != nil {
		t.Fatalf("DeviceBind(jpush) error = %v", err)
	}

	var jpush model.Device
	if err := testDB.DB.Where("user_id = ? AND platform = ?", userID, model.DevicePlatformAndroidJPush).First(&jpush).Error; err != nil {
		t.Fatalf("load jpush binding error = %v", err)
	}
	if !jpush.IsActive {
		t.Fatalf("jpush binding is_active = false, want true")
	}
}
