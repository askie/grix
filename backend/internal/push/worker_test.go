package push

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/push/provider"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"golang.org/x/oauth2"
)

func TestWorkerProcessTaskRoutesProvidersByPlatform(t *testing.T) {
	logger.Init()
	setupPushWorkerTest(t)

	const (
		recipientID = int64(4001)
		senderID    = int64(9001)
		sessionID   = "session-routing"
	)

	seedPushWorkerTestData(t, recipientID, senderID, sessionID)
	webPushToken := `{"endpoint":"https://push.example.test/subscription-id","keys":{"p256dh":"p256dh-key","auth":"auth-key"}}`
	mustCreateDevices(t, []model.Device{
		{UserID: recipientID, Platform: model.DevicePlatformIOS, PushEnv: model.DevicePushEnvAPNsSandbox, DeviceToken: "ios-sandbox-token", DeviceID: "ios-sandbox-device", IsActive: true},
		{UserID: recipientID, Platform: model.DevicePlatformIOS, PushEnv: model.DevicePushEnvAPNsProduction, DeviceToken: "ios-production-token", DeviceID: "ios-production-device", IsActive: true},
		{UserID: recipientID, Platform: model.DevicePlatformAndroidFCM, PushEnv: model.DevicePushEnvDefault, DeviceToken: "fcm-token", DeviceID: "fcm-device", IsActive: true},
		{UserID: recipientID, Platform: model.DevicePlatformAndroidJPush, PushEnv: model.DevicePushEnvDefault, DeviceToken: "jpush-token", DeviceID: "jpush-device", IsActive: true},
		{UserID: recipientID, Platform: model.DevicePlatformWebPush, PushEnv: model.DevicePushEnvDefault, DeviceToken: webPushToken, DeviceID: "webpush-device", IsActive: true},
	})

	var apnsSandboxCalls int32
	apnsSandboxServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&apnsSandboxCalls, 1)
		if r.URL.Path != "/3/device/ios-sandbox-token" {
			t.Fatalf("unexpected sandbox apns path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode sandbox apns body: %v", err)
		}
		if body["recipient_id"] != "4001" {
			t.Fatalf("worker did not inject recipient_id, got %#v", body["recipient_id"])
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer apnsSandboxServer.Close()

	var apnsProductionCalls int32
	apnsProductionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&apnsProductionCalls, 1)
		if r.URL.Path != "/3/device/ios-production-token" {
			t.Fatalf("unexpected production apns path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer apnsProductionServer.Close()

	var fcmCalls int32
	fcmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&fcmCalls, 1)
		if r.URL.Path != "/v1/projects/demo-project/messages:send" {
			t.Fatalf("unexpected fcm path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer fcmServer.Close()

	var jpushCalls int32
	jpushServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&jpushCalls, 1)
		if r.URL.Path != "/v3/push" {
			t.Fatalf("unexpected jpush path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer jpushServer.Close()

	apnsSandboxProvider := provider.NewAPNs(writeAPNsKey(t), "kid", "team", "com.example.frontend", false)
	setUnexportedField(t, apnsSandboxProvider, "baseURL", apnsSandboxServer.URL)
	setUnexportedField(t, apnsSandboxProvider, "client", apnsSandboxServer.Client())
	setUnexportedField(t, apnsSandboxProvider, "nowFunc", func() time.Time { return time.Unix(1700000000, 0) })

	apnsProductionProvider := provider.NewAPNs(writeAPNsKey(t), "kid", "team", "com.example.frontend", true)
	setUnexportedField(t, apnsProductionProvider, "baseURL", apnsProductionServer.URL)
	setUnexportedField(t, apnsProductionProvider, "client", apnsProductionServer.Client())
	setUnexportedField(t, apnsProductionProvider, "nowFunc", func() time.Time { return time.Unix(1700000000, 0) })

	fcmProvider := provider.NewFCM(writeFCMCredentials(t))
	setUnexportedField(t, fcmProvider, "baseURL", fcmServer.URL)
	setUnexportedField(t, fcmProvider, "client", fcmServer.Client())
	setUnexportedField(t, fcmProvider, "tokenSource", oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: "test-access-token",
	}))

	jpushProvider := provider.NewJPush("app-key", "master-secret")
	setUnexportedField(t, jpushProvider, "baseURL", jpushServer.URL)
	setUnexportedField(t, jpushProvider, "client", jpushServer.Client())

	var webPushCalls int32
	webpushProvider := provider.NewWebPush("vapid-public", "vapid-private", "mailto:push@example.com")
	setUnexportedField(
		t,
		webpushProvider,
		"sendFunc",
		func(_ context.Context, _ []byte, sub *webpush.Subscription, _ *webpush.Options) (*http.Response, error) {
			atomic.AddInt32(&webPushCalls, 1)
			if sub.Endpoint != "https://push.example.test/subscription-id" {
				t.Fatalf("unexpected web push endpoint: %s", sub.Endpoint)
			}
			return &http.Response{StatusCode: http.StatusCreated, Body: http.NoBody}, nil
		},
	)

	worker := NewWorker(apnsSandboxProvider, apnsProductionProvider, fcmProvider, jpushProvider, webpushProvider, nil)
	if err := worker.processTask(context.Background(), buildPushTask(recipientID, senderID, sessionID, "hello from worker")); err != nil {
		t.Fatalf("processTask error: %v", err)
	}

	if got := atomic.LoadInt32(&apnsSandboxCalls); got != 1 {
		t.Fatalf("expected 1 sandbox apns call, got %d", got)
	}
	if got := atomic.LoadInt32(&apnsProductionCalls); got != 1 {
		t.Fatalf("expected 1 production apns call, got %d", got)
	}
	if got := atomic.LoadInt32(&fcmCalls); got != 1 {
		t.Fatalf("expected 1 fcm call, got %d", got)
	}
	if got := atomic.LoadInt32(&jpushCalls); got != 1 {
		t.Fatalf("expected 1 jpush call, got %d", got)
	}
	if got := atomic.LoadInt32(&webPushCalls); got != 1 {
		t.Fatalf("expected 1 web push call, got %d", got)
	}
}

func TestWorkerProcessTaskDoesNotSkipWebPushForOnlineRoute(t *testing.T) {
	logger.Init()
	setupPushWorkerTest(t)

	const (
		recipientID = int64(4011)
		senderID    = int64(9011)
		sessionID   = "session-webpush-online-route"
	)

	seedPushWorkerTestData(t, recipientID, senderID, sessionID)
	webPushToken := `{"endpoint":"https://push.example.test/subscription-id","keys":{"p256dh":"p256dh-key","auth":"auth-key"}}`
	mustCreateDevices(t, []model.Device{
		{UserID: recipientID, Platform: model.DevicePlatformAndroidFCM, PushEnv: model.DevicePushEnvDefault, DeviceToken: "fcm-online-token", DeviceID: "fcm-online-device", IsActive: true},
		{UserID: recipientID, Platform: model.DevicePlatformWebPush, PushEnv: model.DevicePushEnvDefault, DeviceToken: webPushToken, DeviceID: "webpush-online-device", IsActive: true},
	})
	if err := store.RDB.HSet(
		context.Background(),
		"im:ws:route:4011",
		"fcm-online-device",
		"node-a",
		"webpush-online-device",
		"node-a",
	).Err(); err != nil {
		t.Fatalf("seed online routes: %v", err)
	}
	if err := store.RDB.Set(context.Background(), "im:ws:alive:4011:fcm-online-device", "1", time.Minute).Err(); err != nil {
		t.Fatalf("seed fcm alive route: %v", err)
	}
	if err := store.RDB.Set(context.Background(), "im:ws:alive:4011:webpush-online-device", "1", time.Minute).Err(); err != nil {
		t.Fatalf("seed web push alive route: %v", err)
	}

	var fcmCalls int32
	fcmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&fcmCalls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer fcmServer.Close()
	fcmProvider := provider.NewFCM(writeFCMCredentials(t))
	setUnexportedField(t, fcmProvider, "baseURL", fcmServer.URL)
	setUnexportedField(t, fcmProvider, "client", fcmServer.Client())
	setUnexportedField(t, fcmProvider, "tokenSource", oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: "test-access-token",
	}))

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

	worker := NewWorker(nil, nil, fcmProvider, nil, webpushProvider, nil)
	if err := worker.processTask(context.Background(), buildPushTask(recipientID, senderID, sessionID, "hello web pwa")); err != nil {
		t.Fatalf("processTask error: %v", err)
	}

	if got := atomic.LoadInt32(&fcmCalls); got != 0 {
		t.Fatalf("expected online fcm device to be skipped, got %d calls", got)
	}
	if got := atomic.LoadInt32(&webPushCalls); got != 1 {
		t.Fatalf("expected online web push device to still receive push, got %d calls", got)
	}
}

func TestWorkerProcessTaskUsesDefaultTitleWhenSenderMissing(t *testing.T) {
	logger.Init()
	setupPushWorkerTest(t)

	const (
		recipientID = int64(4301)
		senderID    = int64(9301)
		sessionID   = "session-missing-sender"
	)

	if err := store.DB.Create(&model.User{
		ID:           recipientID,
		Username:     "recipient-default-title",
		Email:        "recipient-default-title@example.com",
		PasswordHash: "hash",
		Nickname:     "Recipient",
	}).Error; err != nil {
		t.Fatalf("create recipient user: %v", err)
	}
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     recipientID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	mustCreateDevices(t, []model.Device{
		{UserID: recipientID, Platform: model.DevicePlatformAndroidFCM, PushEnv: model.DevicePushEnvDefault, DeviceToken: "fcm-default-title-token", DeviceID: "fcm-default-title-device", IsActive: true},
	})

	var gotTitle string
	fcmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Message struct {
				Notification struct {
					Title string `json:"title"`
				} `json:"notification"`
			} `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode fcm body: %v", err)
		}
		gotTitle = body.Message.Notification.Title
		w.WriteHeader(http.StatusOK)
	}))
	defer fcmServer.Close()

	fcmProvider := provider.NewFCM(writeFCMCredentials(t))
	setUnexportedField(t, fcmProvider, "baseURL", fcmServer.URL)
	setUnexportedField(t, fcmProvider, "client", fcmServer.Client())
	setUnexportedField(t, fcmProvider, "tokenSource", oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: "test-access-token",
	}))

	worker := NewWorker(nil, nil, fcmProvider, nil, nil, nil)
	if err := worker.processTask(context.Background(), buildPushTask(recipientID, senderID, sessionID, "hello fallback title")); err != nil {
		t.Fatalf("processTask error: %v", err)
	}

	if gotTitle != defaultPushSenderTitle {
		t.Fatalf("push title mismatch: got=%q want=%q", gotTitle, defaultPushSenderTitle)
	}
}

func TestWorkerProcessTaskSkipsMutedSessionOfflinePush(t *testing.T) {
	logger.Init()
	setupPushWorkerTest(t)

	const (
		recipientID = int64(4311)
		senderID    = int64(9311)
		sessionID   = "session-muted-offline-push"
	)

	seedPushWorkerTestData(t, recipientID, senderID, sessionID)
	now := time.Now().UTC()
	mustCreateSessionMembers(t, []model.SessionMember{
		{
			SessionID:    sessionID,
			MemberID:     recipientID,
			MemberType:   1,
			IsMuted:      true,
			LastActiveAt: now,
			JoinedAt:     now,
		},
	})
	mustCreateDevices(t, []model.Device{
		{
			UserID:      recipientID,
			Platform:    model.DevicePlatformAndroidFCM,
			PushEnv:     model.DevicePushEnvDefault,
			DeviceToken: "fcm-muted-token",
			DeviceID:    "fcm-muted-device",
			IsActive:    true,
		},
	})

	var fcmCalls int32
	fcmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&fcmCalls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer fcmServer.Close()

	fcmProvider := provider.NewFCM(writeFCMCredentials(t))
	setUnexportedField(t, fcmProvider, "baseURL", fcmServer.URL)
	setUnexportedField(t, fcmProvider, "client", fcmServer.Client())
	setUnexportedField(t, fcmProvider, "tokenSource", oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: "test-access-token",
	}))

	worker := NewWorker(nil, nil, fcmProvider, nil, nil, nil)
	if err := worker.processTask(context.Background(), buildPushTask(recipientID, senderID, sessionID, "should be muted")); err != nil {
		t.Fatalf("processTask error: %v", err)
	}

	if got := atomic.LoadInt32(&fcmCalls); got != 0 {
		t.Fatalf("expected muted session to skip offline push, got %d fcm calls", got)
	}
}

func TestWorkerProcessTaskSkipsPeerMutedOfflinePush(t *testing.T) {
	logger.Init()
	setupPushWorkerTest(t)

	const (
		recipientID = int64(4312)
		senderID    = int64(9312)
		sessionID   = "session-peer-muted-offline-push"
	)

	seedPushWorkerTestData(t, recipientID, senderID, sessionID)
	now := time.Now().UTC()
	mustCreateSessionMembers(t, []model.SessionMember{
		{
			SessionID:    sessionID,
			MemberID:     recipientID,
			MemberType:   1,
			IsMuted:      false,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     senderID,
			MemberType:   1,
			LastActiveAt: now,
			JoinedAt:     now,
		},
	})
	if err := store.DB.Create(&model.UserPeerMute{
		ID:         431201,
		UserID:     recipientID,
		PeerUserID: senderID,
		IsMuted:    true,
		CreatedAt:  now,
		UpdatedAt:  now,
	}).Error; err != nil {
		t.Fatalf("create peer mute: %v", err)
	}
	mustCreateDevices(t, []model.Device{
		{
			UserID:      recipientID,
			Platform:    model.DevicePlatformAndroidFCM,
			PushEnv:     model.DevicePushEnvDefault,
			DeviceToken: "fcm-peer-muted-token",
			DeviceID:    "fcm-peer-muted-device",
			IsActive:    true,
		},
	})

	var fcmCalls int32
	fcmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&fcmCalls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer fcmServer.Close()

	fcmProvider := provider.NewFCM(writeFCMCredentials(t))
	setUnexportedField(t, fcmProvider, "baseURL", fcmServer.URL)
	setUnexportedField(t, fcmProvider, "client", fcmServer.Client())
	setUnexportedField(t, fcmProvider, "tokenSource", oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: "test-access-token",
	}))

	worker := NewWorker(nil, nil, fcmProvider, nil, nil, nil)
	if err := worker.processTask(context.Background(), buildPushTask(recipientID, senderID, sessionID, "should be peer muted")); err != nil {
		t.Fatalf("processTask error: %v", err)
	}

	if got := atomic.LoadInt32(&fcmCalls); got != 0 {
		t.Fatalf("expected peer-muted session to skip offline push, got %d fcm calls", got)
	}
}

func TestWorkerProcessTaskReturnsErrorWhenAllDeliveriesRetryableFail(t *testing.T) {
	logger.Init()
	setupPushWorkerTest(t)

	const (
		recipientID = int64(4321)
		senderID    = int64(9321)
		sessionID   = "session-retryable-fail"
	)

	seedPushWorkerTestData(t, recipientID, senderID, sessionID)
	mustCreateDevices(t, []model.Device{
		{
			UserID:      recipientID,
			Platform:    model.DevicePlatformAndroidFCM,
			PushEnv:     model.DevicePushEnvDefault,
			DeviceToken: "fcm-retryable-fail-token",
			DeviceID:    "fcm-retryable-fail-device",
			IsActive:    true,
		},
	})

	fcmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer fcmServer.Close()

	fcmProvider := provider.NewFCM(writeFCMCredentials(t))
	setUnexportedField(t, fcmProvider, "baseURL", fcmServer.URL)
	setUnexportedField(t, fcmProvider, "client", fcmServer.Client())
	setUnexportedField(t, fcmProvider, "tokenSource", oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: "test-access-token",
	}))

	worker := NewWorker(nil, nil, fcmProvider, nil, nil, nil)
	if err := worker.processTask(context.Background(), buildPushTask(recipientID, senderID, sessionID, "retryable fail")); err == nil {
		t.Fatal("expected processTask error when all deliveries are retryable failures")
	}
}

func TestWorkerProcessTaskAllowsAckOnPartialSuccess(t *testing.T) {
	logger.Init()
	setupPushWorkerTest(t)

	const (
		recipientID = int64(4331)
		senderID    = int64(9331)
		sessionID   = "session-partial-success"
	)

	seedPushWorkerTestData(t, recipientID, senderID, sessionID)
	webPushToken := `{"endpoint":"https://push.example.test/subscription-id","keys":{"p256dh":"p256dh-key","auth":"auth-key"}}`
	mustCreateDevices(t, []model.Device{
		{
			UserID:      recipientID,
			Platform:    model.DevicePlatformAndroidFCM,
			PushEnv:     model.DevicePushEnvDefault,
			DeviceToken: "fcm-partial-success-token",
			DeviceID:    "fcm-partial-success-device",
			IsActive:    true,
		},
		{
			UserID:      recipientID,
			Platform:    model.DevicePlatformWebPush,
			PushEnv:     model.DevicePushEnvDefault,
			DeviceToken: webPushToken,
			DeviceID:    "webpush-partial-success-device",
			IsActive:    true,
		},
	})

	fcmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer fcmServer.Close()

	fcmProvider := provider.NewFCM(writeFCMCredentials(t))
	setUnexportedField(t, fcmProvider, "baseURL", fcmServer.URL)
	setUnexportedField(t, fcmProvider, "client", fcmServer.Client())
	setUnexportedField(t, fcmProvider, "tokenSource", oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: "test-access-token",
	}))

	webpushProvider := provider.NewWebPush("vapid-public", "vapid-private", "mailto:push@example.com")
	setUnexportedField(
		t,
		webpushProvider,
		"sendFunc",
		func(_ context.Context, _ []byte, _ *webpush.Subscription, _ *webpush.Options) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusCreated, Body: http.NoBody}, nil
		},
	)

	worker := NewWorker(nil, nil, fcmProvider, nil, webpushProvider, nil)
	if err := worker.processTask(context.Background(), buildPushTask(recipientID, senderID, sessionID, "partial success")); err != nil {
		t.Fatalf("expected no processTask error on partial success, got: %v", err)
	}
}

func TestWorkerProcessTaskSetsAPNsBadgeFromUnmutedUnread(t *testing.T) {
	logger.Init()
	setupPushWorkerTest(t)

	const (
		recipientID  = int64(4201)
		senderID     = int64(9201)
		sessionID    = "session-badge-primary"
		secondarySID = "session-badge-secondary"
	)

	seedPushWorkerTestData(t, recipientID, senderID, sessionID)
	if err := store.DB.Create(&model.Session{
		SessionID:   secondarySID,
		OwnerID:     recipientID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create secondary session: %v", err)
	}

	now := time.Now().UTC()
	mustCreateSessionMembers(t, []model.SessionMember{
		{
			SessionID:    sessionID,
			MemberID:     recipientID,
			MemberType:   1,
			UnreadCount:  2,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    secondarySID,
			MemberID:     recipientID,
			MemberType:   1,
			IsMuted:      true,
			UnreadCount:  3,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     senderID,
			MemberType:   1,
			UnreadCount:  99,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    secondarySID,
			MemberID:     recipientID,
			MemberType:   2,
			UnreadCount:  50,
			LastActiveAt: now,
			JoinedAt:     now,
		},
	})
	mustCreateDevices(t, []model.Device{
		{
			UserID:      recipientID,
			Platform:    model.DevicePlatformIOS,
			PushEnv:     model.DevicePushEnvAPNsSandbox,
			DeviceToken: "ios-badge-token",
			DeviceID:    "ios-badge-device",
			IsActive:    true,
		},
	})

	var gotBadge int32
	apnsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			APS struct {
				Badge int `json:"badge"`
			} `json:"aps"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode apns body: %v", err)
		}
		atomic.StoreInt32(&gotBadge, int32(body.APS.Badge))
		w.WriteHeader(http.StatusOK)
	}))
	defer apnsServer.Close()

	apnsProvider := provider.NewAPNs(writeAPNsKey(t), "kid", "team", "com.example.frontend", false)
	setUnexportedField(t, apnsProvider, "baseURL", apnsServer.URL)
	setUnexportedField(t, apnsProvider, "client", apnsServer.Client())
	setUnexportedField(t, apnsProvider, "nowFunc", func() time.Time { return time.Unix(1700000000, 0) })

	worker := NewWorker(apnsProvider, nil, nil, nil, nil, nil)
	if err := worker.processTask(context.Background(), buildPushTask(recipientID, senderID, sessionID, "badge message")); err != nil {
		t.Fatalf("processTask error: %v", err)
	}

	if got := atomic.LoadInt32(&gotBadge); got != 2 {
		t.Fatalf("apns badge mismatch: got=%d want=2", got)
	}
}

func TestWorkerProcessTaskSessionMemberChangedAddPush(t *testing.T) {
	logger.Init()
	setupPushWorkerTest(t)

	const (
		recipientID = int64(4101)
		operatorID  = int64(9101)
		sessionID   = "session-member-changed-add"
	)

	seedPushWorkerTestData(t, recipientID, operatorID, sessionID)
	mustCreateDevices(t, []model.Device{
		{UserID: recipientID, Platform: model.DevicePlatformAndroidFCM, PushEnv: model.DevicePushEnvDefault, DeviceToken: "fcm-token", DeviceID: "fcm-device", IsActive: true},
	})

	var fcmCalls int32
	fcmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&fcmCalls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer fcmServer.Close()

	fcmProvider := provider.NewFCM(writeFCMCredentials(t))
	setUnexportedField(t, fcmProvider, "baseURL", fcmServer.URL)
	setUnexportedField(t, fcmProvider, "client", fcmServer.Client())
	setUnexportedField(t, fcmProvider, "tokenSource", oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: "test-access-token",
	}))

	worker := NewWorker(nil, nil, fcmProvider, nil, nil, nil)
	if err := worker.processTask(
		context.Background(),
		buildSessionMemberChangedPushTask(recipientID, sessionID, operatorID),
	); err != nil {
		t.Fatalf("processTask error: %v", err)
	}

	if got := atomic.LoadInt32(&fcmCalls); got != 1 {
		t.Fatalf("expected 1 fcm call for session_member_changed add, got %d", got)
	}
}

func TestWorkerProcessTaskSessionMemberChangedNonAddIgnored(t *testing.T) {
	logger.Init()
	setupPushWorkerTest(t)

	const (
		recipientID = int64(4102)
		operatorID  = int64(9102)
		sessionID   = "session-member-changed-remove"
	)

	seedPushWorkerTestData(t, recipientID, operatorID, sessionID)
	mustCreateDevices(t, []model.Device{
		{UserID: recipientID, Platform: model.DevicePlatformAndroidFCM, PushEnv: model.DevicePushEnvDefault, DeviceToken: "fcm-token-2", DeviceID: "fcm-device-2", IsActive: true},
	})

	var fcmCalls int32
	fcmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&fcmCalls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer fcmServer.Close()

	fcmProvider := provider.NewFCM(writeFCMCredentials(t))
	setUnexportedField(t, fcmProvider, "baseURL", fcmServer.URL)
	setUnexportedField(t, fcmProvider, "client", fcmServer.Client())
	setUnexportedField(t, fcmProvider, "tokenSource", oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: "test-access-token",
	}))

	task := buildSessionMemberChangedPushTask(recipientID, sessionID, operatorID)
	payload := protocol.SessionMemberChangedPayload{}
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		t.Fatalf("unmarshal session_member_changed task payload error: %v", err)
	}
	payload.Action = "remove"
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal session_member_changed remove payload error: %v", err)
	}
	task.Payload = raw

	worker := NewWorker(nil, nil, fcmProvider, nil, nil, nil)
	if err := worker.processTask(context.Background(), task); err != nil {
		t.Fatalf("processTask error: %v", err)
	}

	if got := atomic.LoadInt32(&fcmCalls); got != 0 {
		t.Fatalf("expected 0 fcm calls for non-add action, got %d", got)
	}
}

func TestWorkerProcessTaskDeactivatesInvalidAPNsToken(t *testing.T) {
	logger.Init()
	setupPushWorkerTest(t)

	const (
		recipientID = int64(4002)
		senderID    = int64(9002)
		sessionID   = "session-invalid-token"
		deviceID    = "ios-invalid-device"
	)

	seedPushWorkerTestData(t, recipientID, senderID, sessionID)
	mustCreateDevices(t, []model.Device{
		{UserID: recipientID, Platform: model.DevicePlatformIOS, PushEnv: model.DevicePushEnvAPNsSandbox, DeviceToken: "invalid-ios-token", DeviceID: deviceID, IsActive: true},
	})
	if err := store.RDB.HSet(context.Background(), "im:user:devices:4002", deviceID, `{"platform":"ios","push_env":"apns_sandbox","device_token":"invalid-ios-token"}`).Err(); err != nil {
		t.Fatalf("seed redis device info: %v", err)
	}

	apnsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
		_ = json.NewEncoder(w).Encode(map[string]string{"reason": "Unregistered"})
	}))
	defer apnsServer.Close()

	apnsProvider := provider.NewAPNs(writeAPNsKey(t), "kid", "team", "com.example.frontend", false)
	setUnexportedField(t, apnsProvider, "baseURL", apnsServer.URL)
	setUnexportedField(t, apnsProvider, "client", apnsServer.Client())
	setUnexportedField(t, apnsProvider, "nowFunc", func() time.Time { return time.Unix(1700000000, 0) })

	worker := NewWorker(apnsProvider, nil, nil, nil, nil, nil)
	if err := worker.processTask(context.Background(), buildPushTask(recipientID, senderID, sessionID, "invalid token should deactivate")); err != nil {
		t.Fatalf("processTask error: %v", err)
	}

	var device model.Device
	if err := store.DB.Where("user_id = ? AND device_id = ?", recipientID, deviceID).First(&device).Error; err != nil {
		t.Fatalf("query device: %v", err)
	}
	if device.IsActive {
		t.Fatal("expected device to be deactivated")
	}

	if exists := store.RDB.HExists(context.Background(), "im:user:devices:4002", deviceID).Val(); exists {
		t.Fatal("expected redis device route to be removed")
	}

	var auditCount int64
	if err := store.DB.Model(&model.AuditLog{}).Where("event_type = ? AND user_id = ?", "device_token_invalidated", recipientID).Count(&auditCount).Error; err != nil {
		t.Fatalf("count audit logs: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("expected 1 audit log, got %d", auditCount)
	}
}

func setupPushWorkerTest(t *testing.T) {
	t.Helper()
	testDB := testutil.NewTestDB()
	t.Cleanup(func() {
		testDB.Close()
	})
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	// 推送通道开关带进程级缓存，清掉以免跨用例泄漏（默认回落到全通道开启）。
	systemsetting.InvalidatePushChannelSettingsCache()
}

func seedPushWorkerTestData(t *testing.T, recipientID, senderID int64, sessionID string) {
	t.Helper()

	users := []model.User{
		{ID: recipientID, Username: "recipient", Email: "recipient@example.com", PasswordHash: "hash", Nickname: "Recipient"},
		{ID: senderID, Username: "sender", Email: "sender@example.com", PasswordHash: "hash", Nickname: "Sender Nick"},
	}
	for _, user := range users {
		if err := store.DB.Create(&user).Error; err != nil {
			t.Fatalf("create user %d: %v", user.ID, err)
		}
	}

	session := model.Session{
		SessionID:   sessionID,
		OwnerID:     recipientID,
		SessionType: 1,
	}
	if err := store.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
}

func mustCreateDevices(t *testing.T, devices []model.Device) {
	t.Helper()
	for _, device := range devices {
		if err := store.DB.Create(&device).Error; err != nil {
			t.Fatalf("create device %s: %v", device.DeviceID, err)
		}
	}
}

func mustCreateSessionMembers(t *testing.T, members []model.SessionMember) {
	t.Helper()
	for _, member := range members {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member session=%s member=%d type=%d: %v", member.SessionID, member.MemberID, member.MemberType, err)
		}
	}
}

func buildPushTask(userID, senderID int64, sessionID, content string) *pushTask {
	payload, err := json.Marshal(protocol.PushMsgPayload{
		// 用"刚生成"的雪花 ID，使消息不被陈旧门误判为旧消息。
		MsgID:     snowflakeIDForAge(0),
		SessionID: sessionID,
		SenderID:  senderID,
		Content:   content,
		MsgType:   1,
	})
	if err != nil {
		panic(err)
	}
	return &pushTask{
		UserID:  userID,
		Cmd:     "push_msg",
		Payload: payload,
	}
}

func buildSessionMemberChangedPushTask(userID int64, sessionID string, operatorID int64) *pushTask {
	payload, err := json.Marshal(protocol.SessionMemberChangedPayload{
		SessionID:  sessionID,
		Action:     "add",
		OperatorID: operatorID,
		Title:      "My Group",
		UpdatedAt:  time.Now().Unix(),
	})
	if err != nil {
		panic(err)
	}
	return &pushTask{
		UserID:  userID,
		Cmd:     protocol.CmdSessionMemberChanged,
		Payload: payload,
	}
}

func writeAPNsKey(t *testing.T) string {
	t.Helper()

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate apns private key: %v", err)
	}

	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal apns private key: %v", err)
	}

	block := &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	path := filepath.Join(t.TempDir(), "AuthKey_TEST.p8")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write apns private key: %v", err)
	}
	return path
}

func writeFCMCredentials(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "firebase.json")
	if err := os.WriteFile(path, []byte(`{"project_id":"demo-project"}`), 0o600); err != nil {
		t.Fatalf("write fcm credentials: %v", err)
	}
	return path
}

func TestSanitizeContentApprovalCard(t *testing.T) {
	tests := []struct {
		name    string
		content string
		msgType int16
		want    string
	}{
		{
			name:    "exec approval grix card link",
			content: "[Exec Approval] bash -lc 'echo hello' (hermes)\n/approve hd_abc123 allow-once(grix://card/exec_approval?d=%7B%22approval_id%22%3A%22hd_abc123%22%7D)",
			msgType: 1,
			want:    "有任务需要审批",
		},
		{
			name:    "exec approval bracket prefix",
			content: "[Exec Approval] rm -rf / (codex)\n/approve req_456 allow-once",
			msgType: 1,
			want:    "有任务需要审批",
		},
		{
			name:    "exec status grix card link",
			content: "[Exec Status] Exec approval allowed once.(grix://card/exec_status?d=%7B%22status%22%3A%22resolved-allow-once%22%7D)",
			msgType: 1,
			want:    "审批状态更新",
		},
		{
			name:    "exec status bracket prefix",
			content: "[Exec Status] Exec approval denied.",
			msgType: 1,
			want:    "审批状态更新",
		},
		{
			name:    "normal message untouched",
			content: "你好，这个任务完成了吗？",
			msgType: 1,
			want:    "你好，这个任务完成了吗？",
		},
		{
			name:    "image type",
			content: "",
			msgType: 2,
			want:    "[图片]",
		},
		{
			name:    "system notification type",
			content: "",
			msgType: 3,
			want:    "[系统通知]",
		},
		{
			name:    "ai message type",
			content: "",
			msgType: 4,
			want:    "[AI消息]",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeContent(tc.content, tc.msgType)
			if got != tc.want {
				t.Fatalf("sanitizeContent(%q, %d) = %q, want %q", tc.content, tc.msgType, got, tc.want)
			}
		})
	}
}

func TestShouldSuppressOfflinePush(t *testing.T) {
	toolExtra := json.RawMessage(`{"channel_data":{"grix":{"toolExecution":{"name":"bash"}}}}`)
	thinkingExtra := json.RawMessage(`{"channel_data":{"grix":{"thinking":"let me check"}}}`)
	mentionExtra := json.RawMessage(`{"mention_user_ids":["8479"]}`)

	tests := []struct {
		name    string
		payload pushMsgPayload
		want    bool
	}{
		{
			name:    "tool execution card is process noise",
			payload: pushMsgPayload{MsgType: 1, Content: "grix://card/tool", Extra: toolExtra},
			want:    true,
		},
		{
			name:    "thinking is process noise",
			payload: pushMsgPayload{MsgType: 1, Content: "reasoning...", Extra: thinkingExtra},
			want:    true,
		},
		{
			name:    "call segment transcript suppressed",
			payload: pushMsgPayload{MsgType: model.MsgTypeCallSegment, Content: "你好"},
			want:    true,
		},
		{
			name:    "empty streaming placeholder suppressed",
			payload: pushMsgPayload{MsgType: 4, Content: ""},
			want:    true,
		},
		{
			name:    "finalized streaming reply delivered",
			payload: pushMsgPayload{MsgType: 4, Content: "任务已完成"},
			want:    false,
		},
		{
			name:    "plain text reply delivered",
			payload: pushMsgPayload{MsgType: 1, Content: "你好，这个任务完成了吗？", Extra: mentionExtra},
			want:    false,
		},
		{
			name:    "approval card always delivered even with tool extra",
			payload: pushMsgPayload{MsgType: 1, Content: "[Exec Approval] bash (grix://card/exec_approval?d=x)", Extra: toolExtra},
			want:    false,
		},
		{
			name:    "image delivered",
			payload: pushMsgPayload{MsgType: 2, Content: ""},
			want:    false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldSuppressOfflinePush(tc.payload); got != tc.want {
				t.Fatalf("shouldSuppressOfflinePush(%+v) = %v, want %v", tc.payload, got, tc.want)
			}
		})
	}
}

func TestSanitizeContentStreamFinalRendersOriginal(t *testing.T) {
	// 过程噪音已在 shouldSuppressOfflinePush 拦截，到此的 msg_type=4 是终态文本回复，应推原文而非"[AI消息]"。
	got := sanitizeContent("**任务已完成**，共修改 3 个文件", 4)
	want := "任务已完成，共修改 3 个文件"
	if got != want {
		t.Fatalf("sanitizeContent finalized stream = %q, want %q", got, want)
	}
}

func setUnexportedField(t *testing.T, target any, fieldName string, value any) {
	t.Helper()

	field := reflect.ValueOf(target).Elem().FieldByName(fieldName)
	if !field.IsValid() {
		t.Fatalf("field %s not found", fieldName)
	}
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(value))
}

// snowflakeIDForAge 构造一个生成时刻为"now-age"的雪花消息 ID，用于陈旧门测试。
func snowflakeIDForAge(age time.Duration) int64 {
	createdMillis := time.Now().Add(-age).UnixMilli()
	return (createdMillis - idEpochMillis) << 22
}

func TestMessageAgeFromID(t *testing.T) {
	if got := messageAgeFromID(0); got != 0 {
		t.Fatalf("zero id should yield 0 age, got %s", got)
	}
	if got := messageAgeFromID(-5); got != 0 {
		t.Fatalf("negative id should yield 0 age, got %s", got)
	}
	// 2 分钟前生成的 ID，年龄应落在 [110s, 130s] 容差区间。
	age := messageAgeFromID(snowflakeIDForAge(2 * time.Minute))
	if age < 110*time.Second || age > 130*time.Second {
		t.Fatalf("expected ~2m age, got %s", age)
	}
	// 刚生成的 ID 年龄应远小于陈旧门阈值。
	if age := messageAgeFromID(snowflakeIDForAge(0)); age >= offlinePushStaleAge {
		t.Fatalf("fresh id age %s should be below stale threshold", age)
	}
}

func TestShouldSuppressStaleOfflinePush(t *testing.T) {
	store.RDB = testutil.NewMockRedis()
	ctx := context.Background()
	const onlineUser int64 = 7011
	const offlineUser int64 = 7012

	// onlineUser 有一台在线设备；offlineUser 无任何在线设备。
	if err := store.RDB.HSet(ctx, "im:ws:route:7011", "dev-a", "node-a").Err(); err != nil {
		t.Fatalf("seed route: %v", err)
	}
	if err := store.RDB.Set(ctx, "im:ws:alive:7011:dev-a", "1", time.Minute).Err(); err != nil {
		t.Fatalf("seed alive: %v", err)
	}

	staleID := snowflakeIDForAge(2 * time.Minute)
	freshID := snowflakeIDForAge(5 * time.Second)
	approval := "grix://card/exec_approval"

	tests := []struct {
		name    string
		userID  int64
		payload pushMsgPayload
		want    bool
	}{
		{
			name:    "在线+陈旧普通消息→压制",
			userID:  onlineUser,
			payload: pushMsgPayload{MsgType: 1, Content: "旧的结果", MsgID: staleID},
			want:    true,
		},
		{
			name:    "在线+新鲜普通消息→照弹",
			userID:  onlineUser,
			payload: pushMsgPayload{MsgType: 1, Content: "刚到的消息", MsgID: freshID},
			want:    false,
		},
		{
			name:    "离线+陈旧普通消息→照弹",
			userID:  offlineUser,
			payload: pushMsgPayload{MsgType: 1, Content: "旧的结果", MsgID: staleID},
			want:    false,
		},
		{
			name:    "在线+陈旧审批卡片→照弹",
			userID:  onlineUser,
			payload: pushMsgPayload{MsgType: 1, Content: approval, MsgID: staleID},
			want:    false,
		},
		{
			name:    "在线+陈旧ForcePush→照弹",
			userID:  onlineUser,
			payload: pushMsgPayload{MsgType: 1, Content: "旧的", MsgID: staleID, ForcePush: true},
			want:    false,
		},
		{
			name:    "在线+陈旧TimeSensitive→照弹",
			userID:  onlineUser,
			payload: pushMsgPayload{MsgType: 1, Content: "旧的", MsgID: staleID, TimeSensitive: true},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldSuppressStaleOfflinePush(ctx, tt.userID, tt.payload); got != tt.want {
				t.Fatalf("shouldSuppressStaleOfflinePush = %v, want %v", got, tt.want)
			}
		})
	}
}

// 语音通话振铃等 ForcePush/TimeSensitive 实时信号，即使人际会话节流窗口已开，
// 也必须即时推完整横幅（带 aps.alert），绝不被降级成 badge-only。
func TestForcePushBypassesThrottle(t *testing.T) {
	logger.Init()
	setupPushWorkerTest(t)

	const (
		recipientID = int64(4701)
		senderID    = int64(9701)
		sessionID   = "session-force-throttle"
	)
	if err := store.DB.Create(&model.User{ID: recipientID, Username: "r4701", Email: "r4701@example.com", PasswordHash: "h", Nickname: "R"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	// 人际会话（SessionType=1），节流路径本会生效。
	if err := store.DB.Create(&model.Session{SessionID: sessionID, SessionType: 1}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	mustCreateDevices(t, []model.Device{
		{UserID: recipientID, Platform: model.DevicePlatformIOS, PushEnv: model.DevicePushEnvAPNsSandbox, DeviceToken: "ios-force-token", DeviceID: "ios-force-device", IsActive: true},
	})
	// 预先占用节流窗口：使 ShouldThrottle 命中（SetNX 失败）。
	if err := store.RDB.Set(context.Background(), fmt.Sprintf("push:throttle:%d:%s", recipientID, sessionID), "1", time.Minute).Err(); err != nil {
		t.Fatalf("seed throttle key: %v", err)
	}

	var alertSeen, total int32
	apnsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&total, 1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if aps, ok := body["aps"].(map[string]any); ok {
			if _, hasAlert := aps["alert"]; hasAlert {
				atomic.AddInt32(&alertSeen, 1)
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer apnsServer.Close()
	apnsProv := provider.NewAPNs(writeAPNsKey(t), "kid", "team", "com.example.frontend", false)
	setUnexportedField(t, apnsProv, "baseURL", apnsServer.URL)
	setUnexportedField(t, apnsProv, "client", apnsServer.Client())
	setUnexportedField(t, apnsProv, "nowFunc", func() time.Time { return time.Unix(1700000000, 0) })

	worker := NewWorker(apnsProv, nil, nil, nil, nil, nil)

	payload, err := json.Marshal(protocol.PushMsgPayload{
		MsgID:     snowflakeIDForAge(0),
		SessionID: sessionID,
		SenderID:  senderID,
		Content:   "语音通话",
		MsgType:   1,
		ForcePush: true,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := worker.processTask(context.Background(), &pushTask{UserID: recipientID, Cmd: "push_msg", Payload: payload}); err != nil {
		t.Fatalf("processTask error: %v", err)
	}
	if got := atomic.LoadInt32(&total); got != 1 {
		t.Fatalf("expected exactly 1 apns call, got %d", got)
	}
	if got := atomic.LoadInt32(&alertSeen); got != 1 {
		t.Fatalf("ForcePush must deliver full alert despite throttle, got alert count %d", got)
	}
}

func TestIsImportantPush(t *testing.T) {
	cases := []struct {
		name string
		p    pushMsgPayload
		want bool
	}{
		{"真人文本→重要", pushMsgPayload{SenderType: 1, MsgType: 1, Content: "hi"}, true},
		{"真人图片→重要", pushMsgPayload{SenderType: 1, MsgType: 2, Content: "[图片]"}, true},
		{"真人视频/文件→重要", pushMsgPayload{SenderType: 1, MsgType: 1, Content: "grix://card/video"}, true},
		{"AI普通回复→降级", pushMsgPayload{SenderType: 2, MsgType: 4, Content: "处理完了"}, false},
		{"AI审批卡片→重要", pushMsgPayload{SenderType: 2, MsgType: 1, Content: "grix://card/exec_approval"}, true},
		{"AI呼叫卡片→重要", pushMsgPayload{SenderType: 2, MsgType: 1, Content: "grix://card/call_owner"}, true},
		{"AI带ForcePush→重要", pushMsgPayload{SenderType: 2, ForcePush: true}, true},
		{"AI带TimeSensitive→重要", pushMsgPayload{SenderType: 2, TimeSensitive: true}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isImportantPush(c.p); got != c.want {
				t.Fatalf("isImportantPush = %v, want %v", got, c.want)
			}
		})
	}
}

// TestStaleGateExemptsHumanMessages：真人发的陈旧消息即使用户在线也不被陈旧门压制；
// 只有 AI 的陈旧普通消息才压。
func TestStaleGateExemptsHumanMessages(t *testing.T) {
	store.RDB = testutil.NewMockRedis()
	ctx := context.Background()
	const onlineUser int64 = 7021
	if err := store.RDB.HSet(ctx, "im:ws:route:7021", "dev-a", "node-a").Err(); err != nil {
		t.Fatalf("seed route: %v", err)
	}
	if err := store.RDB.Set(ctx, "im:ws:alive:7021:dev-a", "1", time.Minute).Err(); err != nil {
		t.Fatalf("seed alive: %v", err)
	}
	staleID := snowflakeIDForAge(3 * time.Minute)

	human := pushMsgPayload{SenderType: 1, MsgType: 2, Content: "[图片]", MsgID: staleID}
	if shouldSuppressStaleOfflinePush(ctx, onlineUser, human) {
		t.Fatal("真人陈旧图片消息不应被陈旧门压制")
	}
	ai := pushMsgPayload{SenderType: 2, MsgType: 4, Content: "旧结果", MsgID: staleID}
	if !shouldSuppressStaleOfflinePush(ctx, onlineUser, ai) {
		t.Fatal("AI 陈旧普通消息在线时应被压制")
	}
}

func TestLoadUserUnreadBadgeExcludesInvisibleSessions(t *testing.T) {
	logger.Init()
	setupPushWorkerTest(t)

	const userID = int64(5301)
	now := time.Now().UTC()

	sessions := []model.Session{
		{SessionID: "badge-visible", OwnerID: userID, SessionType: 1},
		{SessionID: "badge-reset-empty", OwnerID: userID, SessionType: 1},
		{SessionID: "badge-reset-refilled", OwnerID: userID, SessionType: 1},
		{SessionID: "badge-deleted", OwnerID: userID, SessionType: 1, IsDeleted: true},
		{SessionID: "badge-banned-group", OwnerID: userID, SessionType: model.SessionTypeGroup, ModerationStatus: model.SessionModerationStatusBanned},
	}
	for i := range sessions {
		if err := store.DB.Create(&sessions[i]).Error; err != nil {
			t.Fatalf("create session %s: %v", sessions[i].SessionID, err)
		}
	}

	members := []model.SessionMember{
		{SessionID: "badge-visible", MemberID: userID, MemberType: 1, UnreadCount: 2, LastActiveAt: now, JoinedAt: now},
		// 清空过记录且重置点后无消息：app 内永不可见，角标不得计入。
		{SessionID: "badge-reset-empty", MemberID: userID, MemberType: 1, UnreadCount: 1, LastActiveAt: now, JoinedAt: now},
		// 清空过记录但之后又有新消息：会话在列表可见，未读照常计入。
		{SessionID: "badge-reset-refilled", MemberID: userID, MemberType: 1, UnreadCount: 3, LastActiveAt: now, JoinedAt: now},
		{SessionID: "badge-deleted", MemberID: userID, MemberType: 1, UnreadCount: 7, LastActiveAt: now, JoinedAt: now},
		{SessionID: "badge-banned-group", MemberID: userID, MemberType: 1, UnreadCount: 9, LastActiveAt: now, JoinedAt: now},
	}
	mustCreateSessionMembers(t, members)

	cutoff := now.Add(-time.Hour)
	resets := []model.SessionHistoryReset{
		{SessionID: "badge-reset-empty", UserID: userID, DeletedBefore: now},
		{SessionID: "badge-reset-refilled", UserID: userID, DeletedBefore: cutoff},
	}
	for i := range resets {
		if err := store.DB.Create(&resets[i]).Error; err != nil {
			t.Fatalf("create history reset %s: %v", resets[i].SessionID, err)
		}
	}

	// badge-reset-empty 的消息全部落在重置点之前；badge-reset-refilled 在重置点之后有可见消息。
	messages := []model.Message{
		{MsgID: 530101, SessionID: "badge-reset-empty", SenderID: 1, MsgType: 1, Content: "old", CreatedAt: now.Add(-2 * time.Hour)},
		{MsgID: 530102, SessionID: "badge-reset-refilled", SenderID: 1, MsgType: 1, Content: "new", CreatedAt: now.Add(-30 * time.Minute)},
	}
	for i := range messages {
		if err := store.DB.Create(&messages[i]).Error; err != nil {
			t.Fatalf("create message %d: %v", messages[i].MsgID, err)
		}
	}

	worker := NewWorker(nil, nil, nil, nil, nil, nil)
	if got := worker.loadUserUnreadBadge(context.Background(), userID); got != 5 {
		t.Fatalf("badge mismatch: got=%d want=5 (visible 2 + reset-refilled 3)", got)
	}
}
