package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
)

type liveActivityRequest struct {
	topic    string
	pushType string
	priority string
	aps      map[string]any
}

func sendLiveActivity(t *testing.T, payload *LiveActivityPayload) (liveActivityRequest, *PushResult) {
	t.Helper()
	logger.Init()

	var captured liveActivityRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			APS map[string]any `json:"aps"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		captured = liveActivityRequest{
			topic:    r.Header.Get("apns-topic"),
			pushType: r.Header.Get("apns-push-type"),
			priority: r.Header.Get("apns-priority"),
			aps:      body.APS,
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := NewAPNs(writeAPNsKey(t), "kid", "team", "pub.dhf.grix", false)
	p.baseURL = server.URL
	p.client = server.Client()
	p.nowFunc = func() time.Time { return time.Unix(1700000000, 0) }

	result, err := p.SendLiveActivity(context.Background(), "activity-token", payload)
	if err != nil {
		t.Fatalf("SendLiveActivity error: %v", err)
	}
	return captured, result
}

// 三个头必须都对：topic 走 .push-type.liveactivity 这条独立通道，push-type 必须是
// liveactivity，优先级按"是否需要主人马上看到"分档。任何一个错，APNs 直接拒收。
func TestSendLiveActivityStartHeadersAndBody(t *testing.T) {
	captured, result := sendLiveActivity(t, &LiveActivityPayload{
		Event:          "start",
		AttributesType: "GrixRunAttributes",
		Attributes: map[string]any{
			"session_id": "s1",
			"agent_id":   "4242",
			"agent_name": "开发员",
		},
		ContentState: map[string]any{"phase": "running", "title": "重构支付模块"},
		Timestamp:    1700000000,
	})

	if !result.Success {
		t.Fatalf("unexpected result: %#v", result)
	}
	if captured.topic != "pub.dhf.grix.push-type.liveactivity" {
		t.Fatalf("apns-topic = %s", captured.topic)
	}
	if captured.pushType != "liveactivity" {
		t.Fatalf("apns-push-type = %s", captured.pushType)
	}
	if captured.priority != "5" {
		t.Fatalf("apns-priority = %s, want 5 for a plain start", captured.priority)
	}
	if captured.aps["event"] != "start" {
		t.Fatalf("aps.event = %v", captured.aps["event"])
	}
	if captured.aps["timestamp"] != float64(1700000000) {
		t.Fatalf("aps.timestamp = %v", captured.aps["timestamp"])
	}
	if captured.aps["attributes-type"] != "GrixRunAttributes" {
		t.Fatalf("aps.attributes-type = %v", captured.aps["attributes-type"])
	}
	attrs, _ := captured.aps["attributes"].(map[string]any)
	if attrs["agent_name"] != "开发员" {
		t.Fatalf("aps.attributes = %#v", attrs)
	}
	state, _ := captured.aps["content-state"].(map[string]any)
	if state["phase"] != "running" {
		t.Fatalf("aps.content-state = %#v", state)
	}
	if _, ok := captured.aps["dismissal-date"]; ok {
		t.Fatal("start must not carry a dismissal date")
	}
}

func TestSendLiveActivityUpdateWithAlertIsHighPriority(t *testing.T) {
	captured, _ := sendLiveActivity(t, &LiveActivityPayload{
		Event:        "update",
		ContentState: map[string]any{"phase": "waiting_approval"},
		AlertTitle:   "审批请求",
		AlertBody:    "要删除生产数据库",
		HighPriority: true,
		Timestamp:    1700000005,
	})

	if captured.priority != "10" {
		t.Fatalf("apns-priority = %s, want 10 for a waiting alert", captured.priority)
	}
	if captured.aps["event"] != "update" {
		t.Fatalf("aps.event = %v", captured.aps["event"])
	}
	// 静态部分只在 start 下发一次，之后重复只是浪费包体。
	if _, ok := captured.aps["attributes"]; ok {
		t.Fatal("update must not carry attributes")
	}
	if _, ok := captured.aps["attributes-type"]; ok {
		t.Fatal("update must not carry attributes-type")
	}
	alert, _ := captured.aps["alert"].(map[string]any)
	if alert["title"] != "审批请求" || alert["body"] != "要删除生产数据库" {
		t.Fatalf("aps.alert = %#v", alert)
	}
}

func TestSendLiveActivityEndCarriesDismissalDate(t *testing.T) {
	captured, _ := sendLiveActivity(t, &LiveActivityPayload{
		Event:        "end",
		ContentState: map[string]any{"phase": "completed"},
		DismissalAt:  1700000300,
		HighPriority: true,
		Timestamp:    1700000010,
	})

	if captured.aps["event"] != "end" {
		t.Fatalf("aps.event = %v", captured.aps["event"])
	}
	if captured.aps["dismissal-date"] != float64(1700000300) {
		t.Fatalf("aps.dismissal-date = %v", captured.aps["dismissal-date"])
	}
	if captured.priority != "10" {
		t.Fatalf("apns-priority = %s, want 10 for the final state", captured.priority)
	}
}

func TestIsLiveActivityTokenInvalid(t *testing.T) {
	cases := []struct {
		name   string
		result *PushResult
		want   bool
	}{
		{"nil", nil, false},
		{"success", &PushResult{Success: true, StatusCode: 200}, false},
		{"bad device token", &PushResult{StatusCode: 400, Reason: "BadDeviceToken"}, true},
		{"unregistered", &PushResult{StatusCode: 410, Reason: "Unregistered"}, true},
		{"expired", &PushResult{StatusCode: 403, Reason: "ExpiredToken"}, true},
		{"throttled", &PushResult{StatusCode: 429, Reason: "TooManyRequests"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsLiveActivityTokenInvalid(tc.result); got != tc.want {
				t.Fatalf("IsLiveActivityTokenInvalid = %v, want %v", got, tc.want)
			}
		})
	}
}
