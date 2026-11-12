package ws

import (
	"context"
	"testing"
	"time"

	agenttoolbar "github.com/askie/grix/backend/internal/agenttoolbar"
	"github.com/askie/grix/backend/internal/agenttoolbar/quotaremind"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	toolstore "github.com/askie/grix/backend/internal/agenttoolbar/store"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	appstore "github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"go.uber.org/zap"
)

type quotaReminderSendCall struct {
	ownerID   int64
	sessionID string
	agentID   int64
	content   string
}

type quotaReminderSender struct {
	calls []quotaReminderSendCall
}

func (s *quotaReminderSender) SendOwnerMessage(_ context.Context, ownerID int64, sessionID string, agentID int64, content string) error {
	s.calls = append(s.calls, quotaReminderSendCall{ownerID: ownerID, sessionID: sessionID, agentID: agentID, content: content})
	return nil
}

func setupTerminalQuotaReminder(t *testing.T) *quotaReminderSender {
	t.Helper()
	testDB := testutil.NewTestDB()
	mockRedis := testutil.NewMockRedis()
	previousDB, previousRDB := appstore.DB, appstore.RDB
	previousToolbar := agenttoolbar.GetGlobal()
	previousLogger := logger.L
	logger.L = zap.NewNop().Sugar()
	appstore.DB, appstore.RDB = testDB.DB, mockRedis
	t.Cleanup(func() {
		agenttoolbar.SetGlobal(previousToolbar)
		logger.L = previousLogger
		appstore.DB, appstore.RDB = previousDB, previousRDB
		testDB.Close()
		mockRedis.Close()
	})

	const (
		ownerID   int64 = 7101
		agentID   int64 = 7201
		sessionID       = "sess-terminal-quota"
	)
	now := time.Now().UTC()
	if err := appstore.DB.Create(&model.Session{
		SessionID: sessionID, OwnerID: ownerID, SessionType: model.SessionTypeDirect,
		ModerationStatus: model.SessionModerationStatusActive,
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
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
		LocalActions: []string{"get_rate_limits"}, Online: true,
	}, 0); err != nil {
		t.Fatalf("store profile: %v", err)
	}
	if err := toolstore.UpsertBinding(context.Background(), toolstore.BindingRecord{
		AgentID: agentID, SessionID: sessionID, ProviderKey: "codex", Cwd: "/workspace/quota", WorkerStatus: "ready",
		Meta: map[string]any{"rate_limits": map[string]any{
			"sampledAt": now.UnixMilli(),
			"primary": map[string]any{
				"usedPercent": 95.0, "windowMinutes": 300.0, "resetsAt": now.Add(time.Hour).Format(time.RFC3339),
			},
		}},
	}); err != nil {
		t.Fatalf("seed binding: %v", err)
	}
	if err := appstore.DB.Create(&model.Message{
		MsgID: 1, SessionID: sessionID, SenderID: ownerID, Content: "continue the task", CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed conversation: %v", err)
	}

	sender := &quotaReminderSender{}
	agenttoolbar.SetGlobal(agenttoolbar.NewService(agenttoolbar.Dependencies{
		MessageSender: sender,
		LanguageResolver: func(context.Context, int64) string {
			return "zh"
		},
		Limiter: quotaremind.NewMemoryLimiter(time.Hour),
	}))
	return sender
}

func TestNotifyAgentOutputStatusQuotaReminderTerminalOnly(t *testing.T) {
	t.Run("non terminal states stay silent", func(t *testing.T) {
		sender := setupTerminalQuotaReminder(t)
		server := NewServer(0, "quota-remind-test", "", "", 0, "", false)
		payload := protocol.AgentOutputStatusPayload{
			RunID: "run-quota", SessionID: "sess-terminal-quota", OwnerID: 7101, AgentID: 7201,
		}
		for _, state := range []string{
			protocol.AgentOutputStateQueued,
			protocol.AgentOutputStateReceived,
			protocol.AgentOutputStateStreaming,
			protocol.AgentOutputStateStopping,
			protocol.AgentOutputStateStopped,
		} {
			payload.State = state
			server.notifyAgentOutputStatus(payload)
			if len(sender.calls) != 0 {
				t.Fatalf("state=%q sent %d reminders, want 0", state, len(sender.calls))
			}
		}
	})

	for _, state := range []string{protocol.AgentOutputStateCompleted, protocol.AgentOutputStateFailed} {
		t.Run(state, func(t *testing.T) {
			sender := setupTerminalQuotaReminder(t)
			server := NewServer(0, "quota-remind-test", "", "", 0, "", false)
			payload := protocol.AgentOutputStatusPayload{
				RunID: "run-quota", SessionID: "sess-terminal-quota", OwnerID: 7101, AgentID: 7201, State: state,
			}
			server.notifyAgentOutputStatus(payload)
			server.notifyAgentOutputStatus(payload)
			if len(sender.calls) != 1 {
				t.Fatalf("state=%q sent %d reminders, want 1 including duplicate suppression", state, len(sender.calls))
			}
			call := sender.calls[0]
			if call.ownerID != payload.OwnerID || call.sessionID != payload.SessionID || call.agentID != payload.AgentID {
				t.Fatalf("reminder call=%+v want payload identity", call)
			}
		})
	}
}
