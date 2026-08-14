package provider

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"golang.org/x/oauth2"
)

func TestAPNsProviderSend(t *testing.T) {
	logger.Init()

	keyPath := writeAPNsKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("apns-topic"); got != "com.example.frontend" {
			t.Fatalf("unexpected apns-topic: %s", got)
		}
		if got := r.Header.Get("authorization"); !strings.HasPrefix(got, "bearer ") {
			t.Fatalf("missing bearer token: %s", got)
		}
		if r.URL.Path != "/3/device/device-token" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		aps, _ := body["aps"].(map[string]any)
		alert, _ := aps["alert"].(map[string]any)
		if alert["title"] != "hello" || alert["body"] != "world" {
			t.Fatalf("unexpected alert payload: %#v", alert)
		}
		if body["recipient_id"] != "42" {
			t.Fatalf("unexpected recipient_id: %#v", body["recipient_id"])
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := NewAPNs(keyPath, "kid", "team", "com.example.frontend", false)
	p.baseURL = server.URL
	p.client = server.Client()
	p.nowFunc = func() time.Time { return time.Unix(1700000000, 0) }

	result, err := p.Send(context.Background(), "device-token", &PushPayload{
		Title:       "hello",
		Body:        "world",
		SessionID:   "s1",
		RecipientID: 42,
	})
	if err != nil {
		t.Fatalf("send returned error: %v", err)
	}
	if result == nil || !result.Success || result.StatusCode != http.StatusOK {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestAPNsProviderMapsUnregisteredReason(t *testing.T) {
	logger.Init()

	keyPath := writeAPNsKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
		_ = json.NewEncoder(w).Encode(map[string]string{"reason": "Unregistered"})
	}))
	defer server.Close()

	p := NewAPNs(keyPath, "kid", "team", "com.example.frontend", true)
	p.baseURL = server.URL
	p.client = server.Client()
	p.nowFunc = func() time.Time { return time.Unix(1700000000, 0) }

	result, err := p.Send(context.Background(), "device-token", &PushPayload{
		Title:     "hello",
		Body:      "world",
		SessionID: "s1",
	})
	if err != nil {
		t.Fatalf("send returned error: %v", err)
	}
	if result == nil || result.Success {
		t.Fatalf("expected failed result, got %#v", result)
	}
	if result.StatusCode != http.StatusGone || result.Reason != "Unregistered" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestFCMProviderSend(t *testing.T) {
	logger.Init()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("authorization"); got != "Bearer access-token" {
			t.Fatalf("unexpected authorization header: %s", got)
		}
		if r.URL.Path != "/v1/projects/demo-project/messages:send" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		message := body["message"].(map[string]any)
		if message["token"] != "fcm-token" {
			t.Fatalf("unexpected token: %#v", message["token"])
		}
		data := message["data"].(map[string]any)
		if data["session_id"] != "s1" {
			t.Fatalf("unexpected data payload: %#v", data)
		}
		if data["recipient_id"] != "42" {
			t.Fatalf("unexpected recipient_id: %#v", data["recipient_id"])
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	credentialsPath := filepath.Join(t.TempDir(), "firebase.json")
	if err := os.WriteFile(credentialsPath, []byte(`{"project_id":"demo-project"}`), 0o600); err != nil {
		t.Fatalf("write firebase credentials: %v", err)
	}

	p := NewFCM(credentialsPath)
	p.baseURL = server.URL
	p.client = server.Client()
	p.tokenSource = oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "access-token"})

	result, err := p.Send(context.Background(), "fcm-token", &PushPayload{
		Title:       "hello",
		Body:        "world",
		SessionID:   "s1",
		RecipientID: 42,
	})
	if err != nil {
		t.Fatalf("send returned error: %v", err)
	}
	if result == nil || !result.Success || result.StatusCode != http.StatusOK {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestFCMProviderMapsUnregisteredError(t *testing.T) {
	logger.Init()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"status":  "NOT_FOUND",
				"message": "Requested entity was not found.",
				"details": []map[string]any{
					{"errorCode": "UNREGISTERED"},
				},
			},
		})
	}))
	defer server.Close()

	credentialsPath := filepath.Join(t.TempDir(), "firebase.json")
	if err := os.WriteFile(credentialsPath, []byte(`{"project_id":"demo-project"}`), 0o600); err != nil {
		t.Fatalf("write firebase credentials: %v", err)
	}

	p := NewFCM(credentialsPath)
	p.baseURL = server.URL
	p.client = server.Client()
	p.tokenSource = oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "access-token"})

	result, err := p.Send(context.Background(), "fcm-token", &PushPayload{
		Title:     "hello",
		Body:      "world",
		SessionID: "s1",
	})
	if err != nil {
		t.Fatalf("send returned error: %v", err)
	}
	if result == nil || result.Success {
		t.Fatalf("expected failed result, got %#v", result)
	}
	if result.Reason != "messaging/registration-token-not-registered" {
		t.Fatalf("unexpected reason: %#v", result)
	}
}

func TestJPushProviderSend(t *testing.T) {
	logger.Init()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("authorization"); got == "" {
			t.Fatalf("missing authorization header")
		}
		if r.URL.Path != "/v3/push" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		audience := body["audience"].(map[string]any)
		registrationIDs := audience["registration_id"].([]any)
		if len(registrationIDs) != 1 || registrationIDs[0] != "jpush-token" {
			t.Fatalf("unexpected audience payload: %#v", audience)
		}
		notification := body["notification"].(map[string]any)
		android := notification["android"].(map[string]any)
		extras := android["extras"].(map[string]any)
		if extras["session_id"] != "s1" {
			t.Fatalf("unexpected extras payload: %#v", extras)
		}
		if extras["recipient_id"] != "42" {
			t.Fatalf("unexpected recipient_id: %#v", extras["recipient_id"])
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := NewJPush("app-key", "master-secret")
	p.baseURL = server.URL
	p.client = server.Client()

	result, err := p.Send(context.Background(), "jpush-token", &PushPayload{
		Title:       "hello",
		Body:        "world",
		SessionID:   "s1",
		RecipientID: 42,
	})
	if err != nil {
		t.Fatalf("send returned error: %v", err)
	}
	if result == nil || !result.Success || result.StatusCode != http.StatusOK {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestJPushProviderMapsInvalidTokenCode(t *testing.T) {
	logger.Init()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    1011,
				"message": "invalid registration id",
			},
		})
	}))
	defer server.Close()

	p := NewJPush("app-key", "master-secret")
	p.baseURL = server.URL
	p.client = server.Client()

	result, err := p.Send(context.Background(), "jpush-token", &PushPayload{
		Title:     "hello",
		Body:      "world",
		SessionID: "s1",
	})
	if err != nil {
		t.Fatalf("send returned error: %v", err)
	}
	if result == nil || result.Success {
		t.Fatalf("expected failed result, got %#v", result)
	}
	if result.Reason != "1011" {
		t.Fatalf("unexpected reason: %#v", result)
	}
}

func TestWebPushProviderSend(t *testing.T) {
	logger.Init()

	var capturedPayload map[string]any
	provider := NewWebPush("vapid-public", "vapid-private", "mailto:push@example.com")
	provider.sendFunc = func(_ context.Context, message []byte, sub *webpush.Subscription, options *webpush.Options) (*http.Response, error) {
		if sub.Endpoint != "https://push.example.test/subscription-id" {
			t.Fatalf("unexpected subscription endpoint: %s", sub.Endpoint)
		}
		if sub.Keys.P256dh != "p256dh-key" || sub.Keys.Auth != "auth-key" {
			t.Fatalf("unexpected subscription keys: %#v", sub.Keys)
		}
		if options.Subscriber != "mailto:push@example.com" {
			t.Fatalf("unexpected subscriber: %s", options.Subscriber)
		}
		if options.VAPIDPublicKey != "vapid-public" || options.VAPIDPrivateKey != "vapid-private" {
			t.Fatalf("unexpected vapid keys")
		}
		httpClient, ok := options.HTTPClient.(*http.Client)
		if !ok {
			t.Fatalf("unexpected http client type: %T", options.HTTPClient)
		}
		if httpClient.Timeout != webPushSendTimeout {
			t.Fatalf("unexpected web push timeout: %s", httpClient.Timeout)
		}

		if err := json.Unmarshal(message, &capturedPayload); err != nil {
			t.Fatalf("decode message: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusCreated,
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	}

	deviceToken := `{"endpoint":"https://push.example.test/subscription-id","keys":{"p256dh":"p256dh-key","auth":"auth-key"}}`
	result, err := provider.Send(context.Background(), deviceToken, &PushPayload{
		Title:       "hello",
		Body:        "world",
		SessionID:   "s1",
		Badge:       3,
		RecipientID: 42,
	})
	if err != nil {
		t.Fatalf("send returned error: %v", err)
	}
	if result == nil || !result.Success || result.StatusCode != http.StatusCreated {
		t.Fatalf("unexpected result: %#v", result)
	}
	if capturedPayload["session_id"] != "s1" || capturedPayload["badge"] != float64(3) {
		t.Fatalf("unexpected payload: %#v", capturedPayload)
	}
	if capturedPayload["recipient_id"] != "42" {
		t.Fatalf("unexpected recipient_id: %#v", capturedPayload["recipient_id"])
	}
}

func TestWebPushProviderMapsExpiredSubscription(t *testing.T) {
	logger.Init()

	provider := NewWebPush("vapid-public", "vapid-private", "mailto:push@example.com")
	provider.sendFunc = func(_ context.Context, _ []byte, _ *webpush.Subscription, _ *webpush.Options) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusGone,
			Body:       io.NopCloser(strings.NewReader(`{"error":"expired"}`)),
		}, nil
	}

	deviceToken := `{"endpoint":"https://push.example.test/subscription-id","keys":{"p256dh":"p256dh-key","auth":"auth-key"}}`
	result, err := provider.Send(context.Background(), deviceToken, &PushPayload{
		Title: "hello",
		Body:  "world",
	})
	if err != nil {
		t.Fatalf("send returned error: %v", err)
	}
	if result == nil || result.Success {
		t.Fatalf("expected failed result, got %#v", result)
	}
	if result.Reason != "subscription-expired" {
		t.Fatalf("unexpected reason: %#v", result)
	}
}

func writeAPNsKey(t *testing.T) string {
	t.Helper()

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate private key: %v", err)
	}

	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}

	block := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: der,
	}

	path := filepath.Join(t.TempDir(), "AuthKey_TEST.p8")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}
	return path
}

// TestAPNsPriorityTiering 验证分级投递：高优先级信号 apns-priority=10 立即投递，
// 普通消息 apns-priority=5 降级投递，badge-only 不设 10。
func TestAPNsPriorityTiering(t *testing.T) {
	logger.Init()
	keyPath := writeAPNsKey(t)

	cases := []struct {
		name    string
		payload PushPayload
		want    string
	}{
		{"普通消息降级", PushPayload{Title: "t", Body: "b", SessionID: "s"}, "5"},
		{"HighPriority最高", PushPayload{Title: "t", Body: "b", SessionID: "s", HighPriority: true}, "10"},
		{"ForcePush最高", PushPayload{Title: "t", Body: "b", SessionID: "s", ForcePush: true}, "10"},
		{"TimeSensitive最高", PushPayload{Title: "t", Body: "b", SessionID: "s", TimeSensitive: true}, "10"},
		{"审批Category最高", PushPayload{Title: "t", Body: "b", SessionID: "s", Category: "APPROVAL_REQUEST"}, "10"},
		{"badge-only不设10", PushPayload{SessionID: "s", BadgeOnly: true}, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Get("apns-priority")
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			p := NewAPNs(keyPath, "kid", "team", "com.example.frontend", false)
			p.baseURL = server.URL
			p.client = server.Client()
			p.nowFunc = func() time.Time { return time.Unix(1700000000, 0) }

			pl := tc.payload
			if _, err := p.Send(context.Background(), "device-token", &pl); err != nil {
				t.Fatalf("send: %v", err)
			}
			if got != tc.want {
				t.Fatalf("apns-priority = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestFCMPriorityTiering 验证安卓分级投递：高优先级 high，普通消息 normal。
func TestFCMPriorityTiering(t *testing.T) {
	logger.Init()

	cases := []struct {
		name    string
		payload PushPayload
		want    string
	}{
		{"普通消息normal", PushPayload{Title: "t", Body: "b", SessionID: "s"}, "normal"},
		{"HighPriority high", PushPayload{Title: "t", Body: "b", SessionID: "s", HighPriority: true}, "high"},
		{"ForcePush high", PushPayload{Title: "t", Body: "b", SessionID: "s", ForcePush: true}, "high"},
		{"TimeSensitive high", PushPayload{Title: "t", Body: "b", SessionID: "s", TimeSensitive: true}, "high"},
		{"审批Category high", PushPayload{Title: "t", Body: "b", SessionID: "s", Category: "APPROVAL_REQUEST"}, "high"},
	}

	credentialsPath := filepath.Join(t.TempDir(), "firebase.json")
	if err := os.WriteFile(credentialsPath, []byte(`{"project_id":"demo-project"}`), 0o600); err != nil {
		t.Fatalf("write firebase credentials: %v", err)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				if msg, ok := body["message"].(map[string]any); ok {
					if android, ok := msg["android"].(map[string]any); ok {
						got, _ = android["priority"].(string)
					}
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			p := NewFCM(credentialsPath)
			p.baseURL = server.URL
			p.client = server.Client()
			p.tokenSource = oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "access-token"})

			pl := tc.payload
			if _, err := p.Send(context.Background(), "fcm-token", &pl); err != nil {
				t.Fatalf("send: %v", err)
			}
			if got != tc.want {
				t.Fatalf("android.priority = %q, want %q", got, tc.want)
			}
		})
	}
}
