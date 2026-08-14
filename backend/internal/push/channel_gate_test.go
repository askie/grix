package push

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/push/provider"
	"github.com/askie/grix/backend/internal/systemsetting"
)

func TestPushChannelEnabled(t *testing.T) {
	// 仅关闭走 Google 的 AndroidFCM / WebPush，保留 iOS / 极光。
	c := systemsetting.PushChannelSettings{
		IOSAPNsEnabled:    true,
		AndroidFCMEnabled: false,
		WebPushEnabled:    false,
		JPushEnabled:      true,

		// 厂商通道逐个可控：关小米不影响华为。
		AndroidHuaweiEnabled: true,
		AndroidHonorEnabled:  true,
		AndroidXiaomiEnabled: false,
		AndroidOppoEnabled:   true,
		AndroidVivoEnabled:   true,
	}
	cases := map[string]bool{
		model.DevicePlatformIOS:           true,
		model.DevicePlatformAndroidFCM:    false,
		model.DevicePlatformWebPush:       false,
		model.DevicePlatformAndroidJPush:  true,
		model.DevicePlatformAndroidHuawei: true,
		model.DevicePlatformAndroidHonor:  true,
		model.DevicePlatformAndroidXiaomi: false,
		model.DevicePlatformAndroidOppo:   true,
		model.DevicePlatformAndroidVivo:   true,
		"unknown_future_platform":         true, // 未知平台默认放行，避免误伤新通道
	}
	for platform, want := range cases {
		if got := pushChannelEnabled(platform, c); got != want {
			t.Errorf("pushChannelEnabled(%q) = %v, want %v", platform, got, want)
		}
	}
}

// TestPushToUserDevicesSkipsDisabledChannel 端到端验证：关闭某通道后，
// 该通道设备完全不发送，且不计入失败（不触发 NATS 重投，返回 nil）。
func TestPushToUserDevicesSkipsDisabledChannel(t *testing.T) {
	logger.Init()
	setupPushWorkerTest(t)
	systemsetting.InvalidatePushChannelSettingsCache()

	const (
		recipientID = int64(4711)
		senderID    = int64(9711)
		sessionID   = "session-channel-gate"
	)
	seedPushWorkerTestData(t, recipientID, senderID, sessionID)

	// 离线 WebPush 设备（不设 alive route，确保不会被在线跳过逻辑提前过滤）。
	webPushToken := `{"endpoint":"https://push.example.test/sub-gate","keys":{"p256dh":"p256dh-key","auth":"auth-key"}}`
	mustCreateDevices(t, []model.Device{
		{UserID: recipientID, Platform: model.DevicePlatformWebPush, PushEnv: model.DevicePushEnvDefault, DeviceToken: webPushToken, DeviceID: "webpush-gate-device", IsActive: true},
	})

	var webPushCalls int32
	webpushProvider := provider.NewWebPush("vapid-public", "vapid-private", "mailto:push@example.com")
	setUnexportedField(
		t,
		webpushProvider,
		"sendFunc",
		func(_ context.Context, _ []byte, _ *webpush.Subscription, _ *webpush.Options) (*http.Response, error) {
			atomic.AddInt32(&webPushCalls, 1)
			return &http.Response{StatusCode: http.StatusCreated, Body: http.NoBody}, nil
		},
	)
	worker := NewWorker(nil, nil, nil, nil, webpushProvider, nil)

	// 基线：通道开启时会真正发送一次。
	if err := worker.processTask(context.Background(), buildPushTask(recipientID, senderID, sessionID, "hello gate on")); err != nil {
		t.Fatalf("processTask (channel enabled) error: %v", err)
	}
	if got := atomic.LoadInt32(&webPushCalls); got != 1 {
		t.Fatalf("expected 1 webpush send while enabled, got %d", got)
	}

	// 关闭 WebPush 通道后再推一次：不应再调用发送，且不返回错误（不重投）。
	if err := systemsetting.SavePushChannelSettings(systemsetting.PushChannelSettings{
		IOSAPNsEnabled:    true,
		AndroidFCMEnabled: true,
		WebPushEnabled:    false,
		JPushEnabled:      true,
	}, nil); err != nil {
		t.Fatalf("save push channel settings: %v", err)
	}
	if err := worker.processTask(context.Background(), buildPushTask(recipientID, senderID, sessionID, "hello gate off")); err != nil {
		t.Fatalf("processTask (channel disabled) should not error, got: %v", err)
	}
	if got := atomic.LoadInt32(&webPushCalls); got != 1 {
		t.Fatalf("expected no further webpush send after disabling channel, total calls = %d", got)
	}
}
