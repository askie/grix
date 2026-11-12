package agentapi

import (
	"context"
	"fmt"
	"testing"
	"time"

	agenttoolbar "github.com/askie/grix/backend/internal/agenttoolbar"
	"github.com/askie/grix/backend/internal/agenttoolbar/quotaremind"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	toolstore "github.com/askie/grix/backend/internal/agenttoolbar/store"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	appstore "github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

type localQuotaReminderSendCall struct {
	ownerID   int64
	sessionID string
	agentID   int64
	content   string
}

type localQuotaReminderSender struct {
	calls []localQuotaReminderSendCall
}

func (s *localQuotaReminderSender) SendOwnerMessage(_ context.Context, ownerID int64, sessionID string, agentID int64, content string) error {
	s.calls = append(s.calls, localQuotaReminderSendCall{ownerID: ownerID, sessionID: sessionID, agentID: agentID, content: content})
	return nil
}

func setupGroupQuotaReminder(t *testing.T, sessionID string) *localQuotaReminderSender {
	t.Helper()
	prevSendEnabled := quotaremind.SendOwnerMessagesEnabled
	quotaremind.SendOwnerMessagesEnabled = true
	testDB := testutil.NewTestDB()
	mockRedis := testutil.NewMockRedis()
	previousDB, previousRDB := appstore.DB, appstore.RDB
	previousToolbar := agenttoolbar.GetGlobal()
	appstore.DB, appstore.RDB = testDB.DB, mockRedis
	t.Cleanup(func() {
		quotaremind.SendOwnerMessagesEnabled = prevSendEnabled
		agenttoolbar.SetGlobal(previousToolbar)
		appstore.DB, appstore.RDB = previousDB, previousRDB
		testDB.Close()
		mockRedis.Close()
	})

	const (
		ownerID int64 = 7301
		agentID int64 = 7401
	)
	now := time.Now().UTC()
	if err := appstore.DB.Create(&model.Session{
		SessionID: sessionID, OwnerID: ownerID, SessionType: model.SessionTypeGroup,
		ModerationStatus: model.SessionModerationStatusActive,
	}).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	for _, member := range []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
	} {
		if err := appstore.DB.Create(&member).Error; err != nil {
			t.Fatalf("create member: %v", err)
		}
	}
	if err := appstore.DB.Create(&model.Agent{
		ID: agentID, AgentName: "Codex", OwnerID: ownerID, ProviderType: model.AgentProviderAPI,
		AgentClientType: model.AgentClientTypeCodex, Status: model.AgentStatusActive,
	}).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := toolruntime.StoreProfile(context.Background(), toolruntime.Profile{
		AgentID: agentID, OwnerID: ownerID, ClientType: model.AgentClientTypeCodex,
		LocalActions: []string{"get_rate_limits", "thread_compact"}, Online: true,
	}, 0); err != nil {
		t.Fatalf("store profile: %v", err)
	}
	if err := toolstore.UpsertBinding(context.Background(), toolstore.BindingRecord{
		AgentID: agentID, SessionID: sessionID, ProviderKey: "codex", Cwd: "/workspace/quota", WorkerStatus: "ready",
		Meta: quotaReminderRateLimitsMeta(now),
	}); err != nil {
		t.Fatalf("seed binding: %v", err)
	}
	if err := appstore.DB.Create(&model.Message{
		MsgID: 1, SessionID: sessionID, SenderID: ownerID, Content: "continue the group task", CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed conversation: %v", err)
	}

	sender := &localQuotaReminderSender{}
	agenttoolbar.SetGlobal(agenttoolbar.NewService(agenttoolbar.Dependencies{
		MessageSender: sender,
		LanguageResolver: func(context.Context, int64) string {
			return "zh"
		},
		Limiter: quotaremind.NewMemoryLimiter(time.Hour),
	}))
	return sender
}

func quotaReminderRateLimitsMeta(now time.Time) map[string]any {
	return map[string]any{"rate_limits": map[string]any{
		"sampledAt": float64(now.UnixMilli()),
		"primary": map[string]any{
			"usedPercent": 95.0, "windowMinutes": 300.0, "resetsAt": now.Add(time.Hour).Format(time.RFC3339),
		},
	}}
}

func newQuotaReminderManager(t *testing.T) (*Manager, *agentConn) {
	t.Helper()
	sendHandler := &mockSendMessageHandler{result: &SendMessageResult{MsgID: 1, InboxSeq: 1, CreatedAt: time.Now().UnixMilli()}}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	t.Cleanup(mgr.Shutdown)
	conn := &agentConn{
		agentID: 7401, ownerID: 7301, clientID: "quota-remind-test", adapterID: "codex/base",
		capabilities: []string{"local_action_v1"}, localActions: []string{"get_rate_limits", "thread_compact"},
		send: make(chan []byte, 4),
	}
	mgr.putConnForTest(conn)
	return mgr, conn
}

func storeQuotaReminderAction(mgr *Manager, actionID, kind, sessionID string) {
	mgr.storePendingLocalAction(&pendingLocalAction{
		actionID: actionID, kind: kind, agentID: 7401, ownerID: 7301, sessionID: sessionID, actionType: kind,
	})
}

func TestHandleLocalActionResultGroupQuotaReminder(t *testing.T) {
	for _, kind := range []string{"get_rate_limits", "thread_compact"} {
		t.Run(kind, func(t *testing.T) {
			sessionID := "sess-group-quota-" + kind
			sender := setupGroupQuotaReminder(t, sessionID)
			mgr, conn := newQuotaReminderManager(t)
			result := map[string]any{}
			if kind == "get_rate_limits" {
				result["rateLimits"] = quotaReminderRateLimitsMeta(time.Now().UTC())["rate_limits"]
			}
			for i := 1; i <= 2; i++ {
				actionID := fmt.Sprintf("act-group-quota-%s-%d", kind, i)
				storeQuotaReminderAction(mgr, actionID, kind, sessionID)
				mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, int64(i), protocol.LocalActionResultPayload{
					ActionID: actionID, Status: "ok", Result: result,
				}))
			}
			if len(sender.calls) != 1 {
				t.Fatalf("kind=%q sent %d reminders, want 1 with duplicate suppression", kind, len(sender.calls))
			}
			if sender.calls[0].sessionID != sessionID || sender.calls[0].agentID != conn.agentID {
				t.Fatalf("reminder call=%+v want group target", sender.calls[0])
			}
		})
	}
}

func TestHandleUpdateBindingCardGroupQuotaReminder(t *testing.T) {
	t.Run("quota metadata triggers once", func(t *testing.T) {
		sessionID := "sess-group-binding-quota"
		sender := setupGroupQuotaReminder(t, sessionID)
		mgr, conn := newQuotaReminderManager(t)
		meta := quotaReminderRateLimitsMeta(time.Now().UTC())
		for i := 1; i <= 2; i++ {
			mgr.handleUpdateBindingCard(conn, makePacket(t, protocol.CmdUpdateBindingCard, int64(i), map[string]any{
				"session_id": sessionID, "metadata_only": true, "meta": meta,
			}))
		}
		if len(sender.calls) != 1 {
			t.Fatalf("binding quota updates sent %d reminders, want 1", len(sender.calls))
		}
	})

	t.Run("non quota metadata stays silent", func(t *testing.T) {
		sessionID := "sess-group-binding-model"
		sender := setupGroupQuotaReminder(t, sessionID)
		mgr, conn := newQuotaReminderManager(t)
		mgr.handleUpdateBindingCard(conn, makePacket(t, protocol.CmdUpdateBindingCard, 1, map[string]any{
			"session_id": sessionID, "metadata_only": true, "meta": map[string]any{"model_id": "gpt-5.4"},
		}))
		if len(sender.calls) != 0 {
			t.Fatalf("non quota metadata sent %d reminders, want 0", len(sender.calls))
		}
	})
}
