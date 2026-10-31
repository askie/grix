package quotaremind_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	"github.com/askie/grix/backend/internal/agenttoolbar/quotaremind"
	"github.com/askie/grix/backend/internal/pkg/testutil"
)

type mockMessageSender struct {
	mu       sync.Mutex
	sentMsgs []sentCall
}

type sentCall struct {
	ownerID   int64
	sessionID string
	agentID   int64
	content   string
}

func (m *mockMessageSender) SendOwnerMessage(_ context.Context, ownerID int64, sessionID string, agentID int64, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentMsgs = append(m.sentMsgs, sentCall{
		ownerID:   ownerID,
		sessionID: sessionID,
		agentID:   agentID,
		content:   content,
	})
	return nil
}

func (m *mockMessageSender) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sentMsgs)
}

func (m *mockMessageSender) last() sentCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sentMsgs) == 0 {
		return sentCall{}
	}
	return m.sentMsgs[len(m.sentMsgs)-1]
}

func TestService_LanguageFallbacks(t *testing.T) {
	cases := []struct {
		name         string
		userLang     string
		windowType   string
		remaining    float64
		wantLanguage string // "zh" or "en"
		wantKeyword  string
	}{
		{
			name:         "Unset language defaults to English",
			userLang:     "",
			windowType:   quotaremind.WindowFiveHour,
			remaining:    8.5,
			wantLanguage: "en",
			wantKeyword:  "I noticed your 5-hour usage limit has less than 10%",
		},
		{
			name:         "English user setting sends English",
			userLang:     "en-US",
			windowType:   quotaremind.WindowWeekly,
			remaining:    5.0,
			wantLanguage: "en",
			wantKeyword:  "I noticed your weekly usage limit has less than 10%",
		},
		{
			name:         "Chinese user setting sends Chinese",
			userLang:     "zh-CN",
			windowType:   quotaremind.WindowFiveHour,
			remaining:    7.2,
			wantLanguage: "zh",
			wantKeyword:  "看到你当前的 5小时 用量限额只剩不到 10%",
		},
		{
			name:         "Non-Chinese unsupported language defaults to English",
			userLang:     "ja",
			windowType:   quotaremind.WindowFiveHour,
			remaining:    9.0,
			wantLanguage: "en",
			wantKeyword:  "I noticed your 5-hour usage limit has less than 10%",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sender := &mockMessageSender{}
			svc := quotaremind.NewService(sender, func(_ context.Context, _ int64) string {
				return tc.userLang
			}, quotaremind.NewMemoryLimiter(time.Hour))

			snap := toolprotocol.Snapshot{
				SessionID: "sess-1",
				AgentID:   2001,
				Items: []toolprotocol.Item{
					{
						ItemID:                "test_limit",
						Kind:                  toolprotocol.ItemKindProgress,
						CenterText:            "5H",
						ProgressWindowMinutes: 300,
						Percent:               100.0 - tc.remaining,
						ProgressDetail:        "2026-10-31T18:00:00Z",
					},
				},
			}
			if tc.windowType == quotaremind.WindowWeekly {
				snap.Items[0].CenterText = "7D"
				snap.Items[0].ProgressWindowMinutes = 10080
			}

			err := svc.CheckSnapshot(context.Background(), 1001, snap)
			if err != nil {
				t.Fatalf("CheckSnapshot err: %v", err)
			}
			if sender.count() != 1 {
				t.Fatalf("call count=%d want 1", sender.count())
			}
			msg := sender.last()
			if !strings.Contains(msg.content, tc.wantKeyword) {
				t.Fatalf("message content %q does not contain keyword %q", msg.content, tc.wantKeyword)
			}
		})
	}
}

func TestService_DeduplicationAndResetCycle(t *testing.T) {
	sender := &mockMessageSender{}
	limiter := quotaremind.NewMemoryLimiter(time.Millisecond) // short throttle for testing cycle change
	svc := quotaremind.NewService(sender, func(_ context.Context, _ int64) string {
		return "zh"
	}, limiter)
	svc.SetThrottleTTL(time.Millisecond)

	snap1 := toolprotocol.Snapshot{
		SessionID: "sess-1",
		AgentID:   2001,
		Items: []toolprotocol.Item{
			{
				ItemID:                "rate_limit_primary",
				Kind:                  toolprotocol.ItemKindProgress,
				CenterText:            "5H",
				ProgressWindowMinutes: 300,
				Percent:               92.0,
				ProgressDetail:        "2026-10-31T12:00:00Z", // Cycle A
			},
		},
	}

	// 1. First trigger in cycle A
	if err := svc.CheckSnapshot(context.Background(), 1001, snap1); err != nil {
		t.Fatalf("first CheckSnapshot err: %v", err)
	}
	if sender.count() != 1 {
		t.Fatalf("first trigger count=%d want 1", sender.count())
	}

	// 2. Subsequent call in same cycle A (percent even higher: 95%) -> should be suppressed
	snap1.Items[0].Percent = 95.0
	if err := svc.CheckSnapshot(context.Background(), 1001, snap1); err != nil {
		t.Fatalf("second CheckSnapshot err: %v", err)
	}
	if sender.count() != 1 {
		t.Fatalf("second call must not send again, count=%d want 1", sender.count())
	}

	// Wait past throttle
	time.Sleep(5 * time.Millisecond)

	// 3. New cycle arrives (Cycle B) -> should trigger again
	snap2 := toolprotocol.Snapshot{
		SessionID: "sess-1",
		AgentID:   2001,
		Items: []toolprotocol.Item{
			{
				ItemID:                "rate_limit_primary",
				Kind:                  toolprotocol.ItemKindProgress,
				CenterText:            "5H",
				ProgressWindowMinutes: 300,
				Percent:               93.0,
				ProgressDetail:        "2026-10-31T17:00:00Z", // Cycle B
			},
		},
	}
	if err := svc.CheckSnapshot(context.Background(), 1001, snap2); err != nil {
		t.Fatalf("new cycle CheckSnapshot err: %v", err)
	}
	if sender.count() != 2 {
		t.Fatalf("new cycle count=%d want 2", sender.count())
	}
}

func TestService_ThrottlingBlocksRapidCallsAcrossCycles(t *testing.T) {
	sender := &mockMessageSender{}
	limiter := quotaremind.NewMemoryLimiter(1 * time.Hour) // 1 hour throttle
	svc := quotaremind.NewService(sender, func(_ context.Context, _ int64) string {
		return "zh"
	}, limiter)

	snap1 := toolprotocol.Snapshot{
		SessionID: "sess-1",
		AgentID:   2001,
		Items: []toolprotocol.Item{
			{
				ItemID:                "rate_limit_primary",
				Kind:                  toolprotocol.ItemKindProgress,
				CenterText:            "5H",
				ProgressWindowMinutes: 300,
				Percent:               95.0,
				ProgressDetail:        "2026-10-31T12:00:00Z",
			},
		},
	}

	// First trigger
	if err := svc.CheckSnapshot(context.Background(), 1001, snap1); err != nil {
		t.Fatalf("first CheckSnapshot err: %v", err)
	}
	if sender.count() != 1 {
		t.Fatalf("first trigger count=%d want 1", sender.count())
	}

	// Even if cycle changes immediately, throttle must block it
	snap2 := snap1
	snap2.Items[0].ProgressDetail = "2026-10-31T13:00:00Z"
	if err := svc.CheckSnapshot(context.Background(), 1001, snap2); err != nil {
		t.Fatalf("second CheckSnapshot err: %v", err)
	}
	if sender.count() != 1 {
		t.Fatalf("throttle should have blocked second trigger, count=%d want 1", sender.count())
	}
}

func TestService_ConcurrencySafety(t *testing.T) {
	sender := &mockMessageSender{}
	limiter := quotaremind.NewMemoryLimiter(1 * time.Hour)
	svc := quotaremind.NewService(sender, func(_ context.Context, _ int64) string {
		return "zh"
	}, limiter)

	snap := toolprotocol.Snapshot{
		SessionID: "sess-concur",
		AgentID:   2001,
		Items: []toolprotocol.Item{
			{
				ItemID:                "rate_limit_primary",
				Kind:                  toolprotocol.ItemKindProgress,
				CenterText:            "5H",
				ProgressWindowMinutes: 300,
				Percent:               95.0,
				ProgressDetail:        "2026-10-31T12:00:00Z",
			},
		},
	}

	// Run 20 concurrent checks on the exact same snapshot
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = svc.CheckSnapshot(context.Background(), 1001, snap)
		}()
	}
	wg.Wait()

	if sender.count() != 1 {
		t.Fatalf("concurrent checks must only send exactly 1 message, got %d", sender.count())
	}
}

func TestRedisLimiter_AtomicAcquisitionAndThrottle(t *testing.T) {
	mockRedis := testutil.NewMockRedis()
	defer func() { _ = mockRedis.Close() }()

	limiter := quotaremind.NewRedisLimiter(mockRedis, 1*time.Hour)
	sender := &mockMessageSender{}
	svc := quotaremind.NewService(sender, func(_ context.Context, _ int64) string {
		return "zh"
	}, limiter)

	snap := toolprotocol.Snapshot{
		SessionID: "sess-redis",
		AgentID:   2001,
		Items: []toolprotocol.Item{
			{
				ItemID:                "rate_limit_primary",
				Kind:                  toolprotocol.ItemKindProgress,
				CenterText:            "5H",
				ProgressWindowMinutes: 300,
				Percent:               95.0,
				ProgressDetail:        "2026-10-31T12:00:00Z",
			},
		},
	}

	// 1. First trigger succeeds
	if err := svc.CheckSnapshot(context.Background(), 1001, snap); err != nil {
		t.Fatalf("first CheckSnapshot err: %v", err)
	}
	if sender.count() != 1 {
		t.Fatalf("count=%d want 1", sender.count())
	}

	// 2. Immediate second call is rejected by Redis Lua atomic lock
	if err := svc.CheckSnapshot(context.Background(), 1001, snap); err != nil {
		t.Fatalf("second CheckSnapshot err: %v", err)
	}
	if sender.count() != 1 {
		t.Fatalf("second call must be rejected by Redis, count=%d want 1", sender.count())
	}

	// 3. Concurrent calls on Redis limiter
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = svc.CheckSnapshot(context.Background(), 1001, snap)
		}()
	}
	wg.Wait()

	if sender.count() != 1 {
		t.Fatalf("concurrent checks with Redis must only send exactly 1 message, got %d", sender.count())
	}
}
