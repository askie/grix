package webhook

import (
	"context"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

type mockSender struct {
	lastReq SendRequest
	result  *SendResult
	err     error
}

func (m *mockSender) SendAsUser(_ context.Context, req SendRequest) (*SendResult, error) {
	m.lastReq = req
	if m.err != nil {
		return nil, m.err
	}
	if m.result != nil {
		return m.result, nil
	}
	return &SendResult{MessageID: 1, SentAt: time.Now().UTC()}, nil
}

func setupWebhookServiceTest(t *testing.T) (*Service, *testutil.TestDB) {
	t.Helper()
	tdb := testutil.NewTestDB()
	store.DB = tdb.DB
	_ = snowflake.Init(1)
	return NewService(), tdb
}

func seedSessionMember(t *testing.T, db *testutil.TestDB, userID int64, sessionID string) {
	t.Helper()
	if err := db.DB.Create(&model.User{
		ID:           userID,
		Username:     "u1",
		Email:        "u1@example.com",
		PasswordHash: "x",
		Nickname:     "u1",
	}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     userID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := db.DB.Create(&model.SessionMember{
		SessionID:  sessionID,
		MemberID:   userID,
		MemberType: 1,
		Role:       3,
		JoinedAt:   time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("create member: %v", err)
	}
}

func TestServiceCreateListDelete(t *testing.T) {
	svc, tdb := setupWebhookServiceTest(t)
	defer tdb.Close()
	seedSessionMember(t, tdb, 1001, "s-1")

	created, err := svc.CreateEndpoint(context.Background(), CreateRequest{
		UserID:    1001,
		SessionID: "s-1",
		BaseURL:   "https://example.com",
	})
	if err != nil {
		t.Fatalf("CreateEndpoint err: %v", err)
	}
	if created.URL == "" {
		t.Fatalf("expected webhook url")
	}

	items, err := svc.ListEndpoints(context.Background(), 1001, "s-1", "https://example.com")
	if err != nil {
		t.Fatalf("ListEndpoints err: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item got %d", len(items))
	}
	if items[0].URL == "" || items[0].URL == "https://example.com/v1/webhook/incoming/" {
		t.Fatalf("expected full url, got %q", items[0].URL)
	}

	if err := svc.DeleteEndpoint(context.Background(), 1001, created.ID); err != nil {
		t.Fatalf("DeleteEndpoint err: %v", err)
	}
	items, err = svc.ListEndpoints(context.Background(), 1001, "s-1", "https://example.com")
	if err != nil {
		t.Fatalf("ListEndpoints err after delete: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items after delete got %d", len(items))
	}
}

func TestServiceDeliver(t *testing.T) {
	svc, tdb := setupWebhookServiceTest(t)
	defer tdb.Close()
	seedSessionMember(t, tdb, 1002, "s-2")

	created, err := svc.CreateEndpoint(context.Background(), CreateRequest{
		UserID:    1002,
		SessionID: "s-2",
		BaseURL:   "https://example.com",
	})
	if err != nil {
		t.Fatalf("CreateEndpoint err: %v", err)
	}
	token := created.URL[len("https://example.com/v1/webhook/incoming/"):]

	sender := &mockSender{result: &SendResult{MessageID: 7788, SentAt: time.Now().UTC()}}
	res, err := svc.Deliver(context.Background(), token, DeliverRequest{Content: "hello"}, sender)
	if err != nil {
		t.Fatalf("Deliver err: %v", err)
	}
	if res.MessageID != 7788 {
		t.Fatalf("unexpected message id: %d", res.MessageID)
	}
	if sender.lastReq.UserID != 1002 || sender.lastReq.SessionID != "s-2" {
		t.Fatalf("unexpected send routing: %+v", sender.lastReq)
	}

	var ep model.WebhookEndpoint
	if err := tdb.DB.Where("id = ?", created.ID).First(&ep).Error; err != nil {
		t.Fatalf("load endpoint: %v", err)
	}
	if ep.LastUsedAt == nil {
		t.Fatalf("expected last_used_at updated")
	}
}

func TestServiceDeliverExpired(t *testing.T) {
	svc, tdb := setupWebhookServiceTest(t)
	defer tdb.Close()
	seedSessionMember(t, tdb, 1003, "s-3")

	soon := time.Now().UTC().Add(time.Minute)
	created, err := svc.CreateEndpoint(context.Background(), CreateRequest{
		UserID:    1003,
		SessionID: "s-3",
		ExpiresAt: &soon,
		BaseURL:   "https://example.com",
	})
	if err != nil {
		t.Fatalf("CreateEndpoint err: %v", err)
	}
	token := created.URL[len("https://example.com/v1/webhook/incoming/"):]
	// 创建时拒绝过去的时间，这里直接把库里的过期时间拨到过去模拟已过期。
	expired := time.Now().UTC().Add(-time.Minute)
	if err := tdb.DB.Model(&model.WebhookEndpoint{}).Where("id = ?", created.ID).Update("expires_at", expired).Error; err != nil {
		t.Fatalf("expire endpoint: %v", err)
	}

	_, err = svc.Deliver(context.Background(), token, DeliverRequest{Content: "hello"}, &mockSender{})
	if err != ErrExpired {
		t.Fatalf("expected ErrExpired got %v", err)
	}
}

func TestServiceCreateRejectsPastExpiry(t *testing.T) {
	svc, tdb := setupWebhookServiceTest(t)
	defer tdb.Close()
	seedSessionMember(t, tdb, 1004, "s-4")

	past := time.Now().UTC().Add(-time.Minute)
	_, err := svc.CreateEndpoint(context.Background(), CreateRequest{
		UserID:    1004,
		SessionID: "s-4",
		ExpiresAt: &past,
		BaseURL:   "https://example.com",
	})
	if err != ErrExpiresInPast {
		t.Fatalf("expected ErrExpiresInPast got %v", err)
	}
}

func TestServiceCreateCapsActiveEndpoints(t *testing.T) {
	svc, tdb := setupWebhookServiceTest(t)
	defer tdb.Close()
	seedSessionMember(t, tdb, 1005, "s-5")

	req := CreateRequest{UserID: 1005, SessionID: "s-5", BaseURL: "https://example.com"}
	for i := 0; i < MaxActiveEndpointsPerSession; i++ {
		if _, err := svc.CreateEndpoint(context.Background(), req); err != nil {
			t.Fatalf("create #%d: %v", i, err)
		}
	}
	if _, err := svc.CreateEndpoint(context.Background(), req); err != ErrLimitExceeded {
		t.Fatalf("expected ErrLimitExceeded got %v", err)
	}
	// 过期的入口不计入上限。
	if err := tdb.DB.Model(&model.WebhookEndpoint{}).Where("session_id = ?", "s-5").
		Update("expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
		t.Fatalf("expire endpoints: %v", err)
	}
	if _, err := svc.CreateEndpoint(context.Background(), req); err != nil {
		t.Fatalf("create after expiry: %v", err)
	}
}
