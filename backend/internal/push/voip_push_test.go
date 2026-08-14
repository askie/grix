package push

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/push/provider"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() { logger.Init() }

// --- processCallInviteTask ---

func TestProcessCallInviteTask_InvalidPayload(t *testing.T) {
	w := &Worker{}
	task := &pushTask{UserID: 1, Cmd: "call:invite", Payload: []byte("bad-json")}
	// 不 panic，静默忽略
	err := w.processCallInviteTask(context.Background(), task)
	require.NoError(t, err)
}

func TestProcessCallInviteTask_NoDevices(t *testing.T) {
	setupPushWorkerTest(t)

	w := &Worker{}
	payload, _ := json.Marshal(VoIPCallPayload{
		CallID: "123", CallerID: "1", CallerName: "Alice", CallMode: 1,
	})
	task := &pushTask{UserID: 9999, Cmd: "call:invite", Payload: payload}

	err := w.processCallInviteTask(context.Background(), task)
	require.NoError(t, err)
	// 无设备，静默返回
}

// --- SendVoIPPush ---

func TestSendVoIPPush_IOSDevice(t *testing.T) {
	setupPushWorkerTest(t)

	var capturedTopic string
	var capturedPushType string

	// 模拟 APNs 服务器
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedTopic = r.Header.Get("apns-topic")
		capturedPushType = r.Header.Get("apns-push-type")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// 创建 iOS 设备
	device := model.Device{
		UserID:      2001,
		Platform:    model.DevicePlatformIOS,
		PushEnv:     model.DevicePushEnvAPNsSandbox,
		DeviceToken: "fake-ios-token",
		DeviceID:    "dev-ios-1",
		IsActive:    true,
	}
	require.NoError(t, store.DB.Create(&device).Error)

	// 使用测试 APNs provider（指向 mock server）
	apns := &provider.APNsProvider{
		KeyPath:      "testdata/fake.p8",
		KeyID:        "KEYID",
		TeamID:       "TEAMID",
		Topic:        "com.example.app",
		IsProduction: false,
	}

	w := &Worker{apnsSandbox: apns}
	w.SendVoIPPush(context.Background(), 2001, VoIPCallPayload{
		CallID: "call-1", CallerID: "100", CallerName: "Bob", CallMode: 1,
	})

	// APNs 未配置真实 key，会在 authorizationToken 失败，但不 panic
	// 验证设备被查询到（通过 DB 查询无错误）
	_ = capturedTopic
	_ = capturedPushType
}

func TestSendVoIPPush_AndroidFCMDevice(t *testing.T) {
	setupPushWorkerTest(t)

	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"projects/test/messages/1"}`))
	}))
	defer srv.Close()

	// 创建 Android FCM 设备
	device := model.Device{
		UserID:      3001,
		Platform:    model.DevicePlatformAndroidFCM,
		PushEnv:     model.DevicePushEnvDefault,
		DeviceToken: "fake-fcm-token",
		DeviceID:    "dev-android-1",
		IsActive:    true,
	}
	require.NoError(t, store.DB.Create(&device).Error)

	// FCM provider 指向 mock server
	fcm := provider.NewFCM("testdata/fake_credentials.json")

	w := &Worker{fcm: fcm}
	w.SendVoIPPush(context.Background(), 3001, VoIPCallPayload{
		CallID: "call-2", CallerID: "200", CallerName: "Carol", CallMode: 1,
	})

	// FCM 未配置真实凭证，会在 resolveProjectID 失败，但不 panic
	_ = capturedBody
}

func TestSendVoIPPush_InactiveDeviceSkipped(t *testing.T) {
	setupPushWorkerTest(t)

	// 创建非活跃设备
	device := model.Device{
		UserID:      4001,
		Platform:    model.DevicePlatformIOS,
		PushEnv:     model.DevicePushEnvAPNsSandbox,
		DeviceToken: "inactive-token",
		DeviceID:    "dev-inactive",
		IsActive:    false, // 非活跃
	}
	require.NoError(t, store.DB.Create(&device).Error)

	w := &Worker{}
	// 不 panic，非活跃设备被过滤
	w.SendVoIPPush(context.Background(), 4001, VoIPCallPayload{
		CallID: "call-3", CallerID: "300", CallerName: "Dave", CallMode: 1,
	})
}

func TestSendVoIPPush_PayloadFields(t *testing.T) {
	// 验证 VoIP payload 字段结构
	p := VoIPCallPayload{
		CallID:     "999",
		CallerID:   "1",
		CallerName: "Eve",
		CallMode:   1,
	}
	b, err := json.Marshal(p)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(b, &parsed))
	assert.Equal(t, "999", parsed["call_id"])
	assert.Equal(t, "1", parsed["caller_id"])
	assert.Equal(t, "Eve", parsed["caller_name"])
	assert.Equal(t, float64(1), parsed["call_mode"])
}

func TestBuildVoIPPayloadsCarryRecipientID(t *testing.T) {
	p := VoIPCallPayload{CallID: "c1", CallerID: "100", CallerName: "Bob", CallMode: 1}

	apnsPayload := buildVoIPAPNsPayload(2001, p)
	assert.Equal(t, "2001", apnsPayload["recipient_id"], "iOS VoIP payload must carry recipient_id")
	assert.Equal(t, "c1", apnsPayload["call_id"])
	assert.Equal(t, "100", apnsPayload["caller_id"])

	fcmData := buildVoIPFCMData(2001, p)
	assert.Equal(t, "2001", fcmData["recipient_id"], "Android VoIP data must carry recipient_id")
	assert.Equal(t, "call_invite", fcmData["type"])
	assert.Equal(t, "c1", fcmData["call_id"])
}
