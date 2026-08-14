package push

import (
	"context"
	"sync"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/push/provider"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
)

// fakeVendor 记录下发调用，并按预设结果回应，用于验证 worker 的厂商通道路由。
type fakeVendor struct {
	name         string
	result       *provider.PushResult
	tokenInvalid bool

	mu     sync.Mutex
	tokens []string
}

func (f *fakeVendor) Name() string { return f.name }

func (f *fakeVendor) Send(_ context.Context, deviceToken string, _ *provider.PushPayload) (*provider.PushResult, error) {
	f.mu.Lock()
	f.tokens = append(f.tokens, deviceToken)
	f.mu.Unlock()
	if f.result != nil {
		return f.result, nil
	}
	return &provider.PushResult{Success: true, StatusCode: 200}, nil
}

func (f *fakeVendor) IsTokenInvalid(*provider.PushResult) bool { return f.tokenInvalid }

func (f *fakeVendor) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.tokens...)
}

// 每台设备只应命中自己品牌的通道，不得串台。
func TestPushRoutesEachVendorPlatformToItsOwnProvider(t *testing.T) {
	logger.Init()
	setupPushWorkerTest(t)

	const (
		recipientID = int64(5100)
		senderID    = int64(5101)
		sessionID   = "session-vendor-routing"
	)
	seedPushWorkerTestData(t, recipientID, senderID, sessionID)
	mustCreateDevices(t, []model.Device{
		{UserID: recipientID, Platform: model.DevicePlatformAndroidHuawei, PushEnv: model.DevicePushEnvDefault, DeviceToken: "huawei-token", DeviceID: "huawei-device", IsActive: true},
		{UserID: recipientID, Platform: model.DevicePlatformAndroidXiaomi, PushEnv: model.DevicePushEnvDefault, DeviceToken: "xiaomi-token", DeviceID: "xiaomi-device", IsActive: true},
	})

	huawei := &fakeVendor{name: "huawei"}
	xiaomi := &fakeVendor{name: "xiaomi"}
	worker := NewWorker(nil, nil, nil, nil, nil, map[string]provider.VendorSender{
		model.DevicePlatformAndroidHuawei: huawei,
		model.DevicePlatformAndroidXiaomi: xiaomi,
	})

	if err := worker.processTask(context.Background(), buildPushTask(recipientID, senderID, sessionID, "hello vendors")); err != nil {
		t.Fatalf("processTask error: %v", err)
	}

	if got := huawei.calls(); len(got) != 1 || got[0] != "huawei-token" {
		t.Errorf("huawei provider calls = %v, want [huawei-token]", got)
	}
	if got := xiaomi.calls(); len(got) != 1 || got[0] != "xiaomi-token" {
		t.Errorf("xiaomi provider calls = %v, want [xiaomi-token]", got)
	}
}

// 未配置凭据的厂商：设备被跳过，不计入失败（不触发 NATS 重投），
// 也不计入已投递（漏配凭据不能表现为静默成功）。
func TestPushSkipsVendorPlatformWithoutProvider(t *testing.T) {
	logger.Init()
	setupPushWorkerTest(t)

	const (
		recipientID = int64(5200)
		senderID    = int64(5201)
		sessionID   = "session-vendor-missing"
	)
	seedPushWorkerTestData(t, recipientID, senderID, sessionID)
	mustCreateDevices(t, []model.Device{
		{UserID: recipientID, Platform: model.DevicePlatformAndroidOppo, PushEnv: model.DevicePushEnvDefault, DeviceToken: "oppo-token", DeviceID: "oppo-device", IsActive: true},
	})

	worker := NewWorker(nil, nil, nil, nil, nil, nil)
	if err := worker.processTask(context.Background(), buildPushTask(recipientID, senderID, sessionID, "no provider")); err != nil {
		t.Fatalf("processTask should not error for unconfigured vendor, got: %v", err)
	}

	var device model.Device
	if err := store.DB.Where("device_id = ?", "oppo-device").Take(&device).Error; err != nil {
		t.Fatalf("load device: %v", err)
	}
	if !device.IsActive {
		t.Error("device must stay active when its vendor channel is simply unconfigured")
	}
}

// 关闭某个厂商通道后，该品牌设备不再下发，其它品牌不受影响。
func TestPushSkipsDisabledVendorChannel(t *testing.T) {
	logger.Init()
	setupPushWorkerTest(t)

	const (
		recipientID = int64(5300)
		senderID    = int64(5301)
		sessionID   = "session-vendor-gate"
	)
	seedPushWorkerTestData(t, recipientID, senderID, sessionID)
	mustCreateDevices(t, []model.Device{
		{UserID: recipientID, Platform: model.DevicePlatformAndroidHuawei, PushEnv: model.DevicePushEnvDefault, DeviceToken: "huawei-token", DeviceID: "huawei-device", IsActive: true},
		{UserID: recipientID, Platform: model.DevicePlatformAndroidXiaomi, PushEnv: model.DevicePushEnvDefault, DeviceToken: "xiaomi-token", DeviceID: "xiaomi-device", IsActive: true},
	})

	huawei := &fakeVendor{name: "huawei"}
	xiaomi := &fakeVendor{name: "xiaomi"}
	worker := NewWorker(nil, nil, nil, nil, nil, map[string]provider.VendorSender{
		model.DevicePlatformAndroidHuawei: huawei,
		model.DevicePlatformAndroidXiaomi: xiaomi,
	})

	settings := systemsetting.DefaultPushChannelSettings()
	settings.AndroidHuaweiEnabled = false
	if err := systemsetting.SavePushChannelSettings(settings, nil); err != nil {
		t.Fatalf("save push channel settings: %v", err)
	}

	if err := worker.processTask(context.Background(), buildPushTask(recipientID, senderID, sessionID, "gate")); err != nil {
		t.Fatalf("processTask error: %v", err)
	}

	if got := huawei.calls(); len(got) != 0 {
		t.Errorf("disabled huawei channel must not send, got %v", got)
	}
	if got := xiaomi.calls(); len(got) != 1 {
		t.Errorf("xiaomi channel must still send, got %v", got)
	}
}

// 厂商 provider 报告 token 失效 → 设备解绑；不报告 → 设备保留。
func TestPushDeactivatesDeviceOnVendorTokenInvalid(t *testing.T) {
	logger.Init()
	setupPushWorkerTest(t)

	const (
		recipientID = int64(5400)
		senderID    = int64(5401)
		sessionID   = "session-vendor-invalid"
	)
	seedPushWorkerTestData(t, recipientID, senderID, sessionID)
	mustCreateDevices(t, []model.Device{
		{UserID: recipientID, Platform: model.DevicePlatformAndroidHuawei, PushEnv: model.DevicePushEnvDefault, DeviceToken: "stale", DeviceID: "huawei-device", IsActive: true},
		{UserID: recipientID, Platform: model.DevicePlatformAndroidXiaomi, PushEnv: model.DevicePushEnvDefault, DeviceToken: "fine", DeviceID: "xiaomi-device", IsActive: true},
	})

	failed := &provider.PushResult{Success: false, StatusCode: 200, Reason: "80300007"}
	worker := NewWorker(nil, nil, nil, nil, nil, map[string]provider.VendorSender{
		model.DevicePlatformAndroidHuawei: &fakeVendor{name: "huawei", result: failed, tokenInvalid: true},
		// 小米下发失败但不判定 token 失效（失效只能由异步回执获知）。
		model.DevicePlatformAndroidXiaomi: &fakeVendor{name: "xiaomi", result: failed, tokenInvalid: false},
	})

	// 小米通道属于可重试失败且无成功投递 → worker 返回错误以便 NATS 重投。
	if err := worker.processTask(context.Background(), buildPushTask(recipientID, senderID, sessionID, "invalid")); err == nil {
		t.Fatal("expected retryable failure error from the non-invalidating vendor")
	}

	var huaweiDevice, xiaomiDevice model.Device
	if err := store.DB.Where("device_id = ?", "huawei-device").Take(&huaweiDevice).Error; err != nil {
		t.Fatalf("load huawei device: %v", err)
	}
	if err := store.DB.Where("device_id = ?", "xiaomi-device").Take(&xiaomiDevice).Error; err != nil {
		t.Fatalf("load xiaomi device: %v", err)
	}

	if huaweiDevice.IsActive {
		t.Error("huawei device with invalid token must be deactivated")
	}
	if !xiaomiDevice.IsActive {
		t.Error("xiaomi device must stay active: its failure is not a token-invalid signal")
	}
}
