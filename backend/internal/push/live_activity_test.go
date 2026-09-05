package push

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/liveactivity"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/push/provider"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

type apnsCapture struct {
	mu    sync.Mutex
	paths []string
	types []string
	prios []string
	aps   []map[string]any
}

func (c *apnsCapture) record(r *http.Request) {
	var body struct {
		APS map[string]any `json:"aps"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.paths = append(c.paths, r.URL.Path)
	c.types = append(c.types, r.Header.Get("apns-push-type"))
	c.prios = append(c.prios, r.Header.Get("apns-priority"))
	c.aps = append(c.aps, body.APS)
}

func (c *apnsCapture) snapshot() ([]string, []map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	paths := make([]string, len(c.paths))
	copy(paths, c.paths)
	aps := make([]map[string]any, len(c.aps))
	copy(aps, c.aps)
	return paths, aps
}

// newLiveActivityWorker 起一个只接 APNs 沙盒的 worker，并返回抓到的请求。
// failReasons 让用例按 device token 指定一个 APNs 拒绝原因（模拟 token 失效）。
func newLiveActivityWorker(t *testing.T, failReasons map[string]string) (*Worker, *apnsCapture) {
	t.Helper()
	capture := &apnsCapture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.record(r)
		token := r.URL.Path[len("/3/device/"):]
		if reason, bad := failReasons[token]; bad {
			w.WriteHeader(http.StatusGone)
			_, _ = w.Write([]byte(`{"reason":"` + reason + `"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	apns := provider.NewAPNs(writeAPNsKey(t), "kid", "team", "pub.dhf.grix", false)
	setUnexportedField(t, apns, "baseURL", server.URL)
	setUnexportedField(t, apns, "client", server.Client())
	setUnexportedField(t, apns, "nowFunc", func() time.Time { return time.Unix(1700000000, 0) })
	return NewWorker(apns, nil, nil, nil, nil, nil), capture
}

func liveActivityTask(t *testing.T, userID int64, payload protocol.LiveActivityPayload) *pushTask {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal live activity payload: %v", err)
	}
	return &pushTask{UserID: userID, Cmd: protocol.CmdLiveActivity, Payload: raw}
}

func seedActivityToken(t *testing.T, userID int64, sessionID, deviceID, activityID, token string) {
	t.Helper()
	entry, err := json.Marshal(liveactivity.TokenEntry{ActivityID: activityID, Token: token})
	if err != nil {
		t.Fatalf("marshal token entry: %v", err)
	}
	if err := store.RDB.HSet(
		context.Background(),
		liveactivity.TokenKey(userID, sessionID),
		deviceID,
		string(entry),
	).Err(); err != nil {
		t.Fatalf("seed activity token: %v", err)
	}
}

func startPayload(sessionID string) protocol.LiveActivityPayload {
	return protocol.LiveActivityPayload{
		Event:     protocol.LiveActivityEventStart,
		SessionID: sessionID,
		Attributes: protocol.LiveActivityAttributes{
			SessionID: sessionID,
			AgentID:   4242,
			AgentName: "开发员",
		},
		ContentState: protocol.LiveActivityContentState{
			Phase:       protocol.LiveActivityPhaseRunning,
			Title:       "重构支付模块",
			UpdatedAtMs: 1700000000000,
		},
	}
}

// start 只发给报过 push-to-start token 的设备：其它 iOS 设备要么版本旧、
// 要么用户关了实时活动，推过去只会被系统丢掉。
func TestLiveActivityStartOnlyTargetsDevicesWithStartToken(t *testing.T) {
	logger.Init()
	setupPushWorkerTest(t)

	const userID = int64(5101)
	mustCreateDevices(t, []model.Device{
		{UserID: userID, Platform: model.DevicePlatformIOS, PushEnv: model.DevicePushEnvAPNsSandbox, DeviceToken: "ios-a", DeviceID: "device-a", IsActive: true, LiveActivityToken: "start-token-a"},
		{UserID: userID, Platform: model.DevicePlatformIOS, PushEnv: model.DevicePushEnvAPNsSandbox, DeviceToken: "ios-b", DeviceID: "device-b", IsActive: true},
		{UserID: userID, Platform: model.DevicePlatformAndroidFCM, PushEnv: model.DevicePushEnvDefault, DeviceToken: "fcm", DeviceID: "device-c", IsActive: true},
	})

	worker, capture := newLiveActivityWorker(t, nil)
	if err := worker.processTask(context.Background(), liveActivityTask(t, userID, startPayload("session-1"))); err != nil {
		t.Fatalf("processTask: %v", err)
	}

	paths, apsList := capture.snapshot()
	if len(paths) != 1 || paths[0] != "/3/device/start-token-a" {
		t.Fatalf("start went to %v, want only the push-to-start token", paths)
	}
	aps := apsList[0]
	if aps["event"] != "start" {
		t.Fatalf("aps.event = %v", aps["event"])
	}
	if aps["attributes-type"] != liveActivityAttributesType {
		t.Fatalf("aps.attributes-type = %v", aps["attributes-type"])
	}
	attrs, _ := aps["attributes"].(map[string]any)
	if attrs["agent_id"] != "4242" {
		t.Fatalf("attributes.agent_id = %v, want the id as a string", attrs["agent_id"])
	}
	state, _ := aps["content-state"].(map[string]any)
	if state["phase"] != protocol.LiveActivityPhaseRunning {
		t.Fatalf("content-state.phase = %v", state["phase"])
	}
}

// update / end 用 Redis 里那张活动自己的 token，而不是设备的启动 token。
func TestLiveActivityUpdateUsesStoredActivityTokens(t *testing.T) {
	logger.Init()
	setupPushWorkerTest(t)

	const userID = int64(5102)
	const sessionID = "session-2"
	mustCreateDevices(t, []model.Device{
		{UserID: userID, Platform: model.DevicePlatformIOS, PushEnv: model.DevicePushEnvAPNsSandbox, DeviceToken: "ios-a", DeviceID: "device-a", IsActive: true, LiveActivityToken: "start-token-a"},
	})
	seedActivityToken(t, userID, sessionID, "device-a", "activity-1", "activity-token-a")

	payload := protocol.LiveActivityPayload{
		Event:     protocol.LiveActivityEventUpdate,
		SessionID: sessionID,
		ContentState: protocol.LiveActivityContentState{
			Phase:       protocol.LiveActivityPhaseWaitingApproval,
			Title:       "部署",
			Detail:      "要删除生产数据库",
			UpdatedAtMs: 1700000005000,
		},
		Alert: &protocol.LiveActivityAlert{Title: "审批请求", Body: "要删除生产数据库"},
	}

	worker, capture := newLiveActivityWorker(t, nil)
	if err := worker.processTask(context.Background(), liveActivityTask(t, userID, payload)); err != nil {
		t.Fatalf("processTask: %v", err)
	}

	paths, apsList := capture.snapshot()
	if len(paths) != 1 || paths[0] != "/3/device/activity-token-a" {
		t.Fatalf("update went to %v, want the stored activity token", paths)
	}
	if apsList[0]["event"] != "update" {
		t.Fatalf("aps.event = %v", apsList[0]["event"])
	}
	if _, ok := apsList[0]["alert"]; !ok {
		t.Fatal("a waiting update must carry aps.alert")
	}
	if _, ok := apsList[0]["attributes"]; ok {
		t.Fatal("update must not repeat the static attributes")
	}
}

func TestLiveActivityEndClearsStoredTokens(t *testing.T) {
	logger.Init()
	setupPushWorkerTest(t)

	const userID = int64(5103)
	const sessionID = "session-3"
	mustCreateDevices(t, []model.Device{
		{UserID: userID, Platform: model.DevicePlatformIOS, PushEnv: model.DevicePushEnvAPNsSandbox, DeviceToken: "ios-a", DeviceID: "device-a", IsActive: true, LiveActivityToken: "start-token-a"},
	})
	seedActivityToken(t, userID, sessionID, "device-a", "activity-1", "activity-token-a")

	payload := protocol.LiveActivityPayload{
		Event:     protocol.LiveActivityEventEnd,
		SessionID: sessionID,
		ContentState: protocol.LiveActivityContentState{
			Phase:       protocol.LiveActivityPhaseCompleted,
			UpdatedAtMs: 1700000010000,
		},
		DismissalAtMs: 1700000310000,
	}

	worker, capture := newLiveActivityWorker(t, nil)
	if err := worker.processTask(context.Background(), liveActivityTask(t, userID, payload)); err != nil {
		t.Fatalf("processTask: %v", err)
	}

	_, apsList := capture.snapshot()
	if len(apsList) != 1 {
		t.Fatalf("expected 1 end push, got %d", len(apsList))
	}
	if apsList[0]["dismissal-date"] != float64(1700000310) {
		t.Fatalf("aps.dismissal-date = %v, want the payload's dismissal in seconds", apsList[0]["dismissal-date"])
	}

	left, err := store.RDB.Exists(context.Background(), liveactivity.TokenKey(userID, sessionID)).Result()
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if left != 0 {
		t.Fatal("end must delete the session's activity tokens")
	}
}

func TestLiveActivityDropsInvalidStartToken(t *testing.T) {
	logger.Init()
	setupPushWorkerTest(t)

	const userID = int64(5104)
	mustCreateDevices(t, []model.Device{
		{UserID: userID, Platform: model.DevicePlatformIOS, PushEnv: model.DevicePushEnvAPNsSandbox, DeviceToken: "ios-a", DeviceID: "device-a", IsActive: true, LiveActivityToken: "dead-start-token"},
	})

	worker, _ := newLiveActivityWorker(t, map[string]string{"dead-start-token": "BadDeviceToken"})
	if err := worker.processTask(context.Background(), liveActivityTask(t, userID, startPayload("session-4"))); err != nil {
		t.Fatalf("processTask: %v", err)
	}

	var device model.Device
	if err := store.DB.Where("user_id = ? AND device_id = ?", userID, "device-a").Take(&device).Error; err != nil {
		t.Fatalf("reload device: %v", err)
	}
	if device.LiveActivityToken != "" {
		t.Fatalf("invalid start token was kept: %q", device.LiveActivityToken)
	}
	if !device.IsActive {
		t.Fatal("a dead live activity token must not retire the device itself")
	}
}

func TestLiveActivityDropsInvalidActivityToken(t *testing.T) {
	logger.Init()
	setupPushWorkerTest(t)

	const userID = int64(5105)
	const sessionID = "session-5"
	mustCreateDevices(t, []model.Device{
		{UserID: userID, Platform: model.DevicePlatformIOS, PushEnv: model.DevicePushEnvAPNsSandbox, DeviceToken: "ios-a", DeviceID: "device-a", IsActive: true, LiveActivityToken: "start-token-a"},
	})
	seedActivityToken(t, userID, sessionID, "device-a", "activity-1", "dead-activity-token")

	worker, _ := newLiveActivityWorker(t, map[string]string{"dead-activity-token": "ExpiredToken"})
	payload := protocol.LiveActivityPayload{
		Event:        protocol.LiveActivityEventUpdate,
		SessionID:    sessionID,
		ContentState: protocol.LiveActivityContentState{Phase: protocol.LiveActivityPhaseRunning},
	}
	if err := worker.processTask(context.Background(), liveActivityTask(t, userID, payload)); err != nil {
		t.Fatalf("processTask: %v", err)
	}

	left, err := store.RDB.HGetAll(context.Background(), liveactivity.TokenKey(userID, sessionID)).Result()
	if err != nil {
		t.Fatalf("hgetall: %v", err)
	}
	if len(left) != 0 {
		t.Fatalf("expired activity token was kept: %v", left)
	}
}

// 灰度期间老 push 节点会收到还不认识的 cmd：只记日志并 ack，既不 panic 也不重投，
// 否则一条新任务会把整条离线推送队列卡在重投循环里。
func TestUnknownPushCmdIsAckedWithoutError(t *testing.T) {
	logger.Init()
	setupPushWorkerTest(t)

	worker, capture := newLiveActivityWorker(t, nil)
	task := &pushTask{UserID: 5106, Cmd: "some_future_cmd", Payload: json.RawMessage(`{"anything":1}`)}
	if err := worker.processTask(context.Background(), task); err != nil {
		t.Fatalf("unknown cmd must not fail the task: %v", err)
	}
	if paths, _ := capture.snapshot(); len(paths) != 0 {
		t.Fatalf("unknown cmd must not send anything, got %v", paths)
	}
}
