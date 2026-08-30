package agentapi

import (
	"strings"
	"testing"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/webhook"
)

func seedWebhookSessionMember(t *testing.T, sessionID string, memberID int64, memberType int16) {
	t.Helper()
	if err := store.DB.Create(&model.SessionMember{
		SessionID:  sessionID,
		MemberID:   memberID,
		MemberType: memberType,
	}).Error; err != nil {
		t.Fatalf("seed session member: %v", err)
	}
}

func TestDispatchWebhookCreate(t *testing.T) {
	const (
		ownerID = int64(46501)
		agentID = int64(46502)
	)
	_, cleanup := setupAgentInvokeDispatchTest(t)
	defer cleanup()
	prevDomain, prevSecret := config.C.Server.AgentAPIDomain, config.C.Server.WebhookTokenSecret
	config.C.Server.AgentAPIDomain = "wss://api.example.com/ws"
	config.C.Server.WebhookTokenSecret = "test-webhook-secret"
	defer func() {
		config.C.Server.AgentAPIDomain, config.C.Server.WebhookTokenSecret = prevDomain, prevSecret
	}()

	invoke := func(params map[string]interface{}) (interface{}, int, string) {
		return dispatchAgentInvokeWithHooks(agentID, ownerID, "webhook_create", params, agentInvokeHooks{})
	}

	t.Run("requires scope", func(t *testing.T) {
		_, code, msg := invoke(map[string]interface{}{"session_id": "wh-sess-1"})
		if code != 4003 {
			t.Fatalf("without scope code=%d msg=%q, want 4003", code, msg)
		}
	})

	seedAgentInvokeDispatchScope(t, agentID, agentscope.ScopeWebhookCreate)

	t.Run("rejects missing session_id", func(t *testing.T) {
		_, code, _ := invoke(map[string]interface{}{})
		if code != 4001 {
			t.Fatalf("code=%d want 4001", code)
		}
	})

	t.Run("rejects invalid expires_at", func(t *testing.T) {
		_, code, _ := invoke(map[string]interface{}{"session_id": "wh-sess-1", "expires_at": "tomorrow"})
		if code != 4001 {
			t.Fatalf("code=%d want 4001", code)
		}
	})

	t.Run("rejects session the agent is not in", func(t *testing.T) {
		seedWebhookSessionMember(t, "wh-sess-owner-only", ownerID, 1)
		_, code, msg := invoke(map[string]interface{}{"session_id": "wh-sess-owner-only"})
		if code != 4003 || !strings.Contains(msg, "agent is not a member") {
			t.Fatalf("code=%d msg=%q, want 4003 agent-not-member", code, msg)
		}
	})

	t.Run("rejects session the owner is not in", func(t *testing.T) {
		seedWebhookSessionMember(t, "wh-sess-agent-only", agentID, 2)
		_, code, msg := invoke(map[string]interface{}{"session_id": "wh-sess-agent-only"})
		if code != 4003 || !strings.Contains(msg, "owner is not a member") {
			t.Fatalf("code=%d msg=%q, want 4003 owner-not-member", code, msg)
		}
	})

	t.Run("creates endpoint for a shared session", func(t *testing.T) {
		seedWebhookSessionMember(t, "wh-sess-ok", ownerID, 1)
		seedWebhookSessionMember(t, "wh-sess-ok", agentID, 2)
		data, code, msg := invoke(map[string]interface{}{
			"session_id": "wh-sess-ok",
			"expires_at": "2030-01-02T03:04:05Z",
		})
		if code != 0 {
			t.Fatalf("code=%d msg=%q, want 0", code, msg)
		}
		view, ok := data.(*webhook.EndpointView)
		if !ok {
			t.Fatalf("unexpected data type %T", data)
		}
		if !strings.HasPrefix(view.URL, "https://api.example.com/v1/webhook/incoming/") {
			t.Fatalf("url=%q", view.URL)
		}
		if view.SessionID != "wh-sess-ok" || view.ExpiresAt == nil || view.Status != "active" {
			t.Fatalf("view=%+v", view)
		}
		var got model.WebhookEndpoint
		if err := store.DB.Where("session_id = ? AND user_id = ?", "wh-sess-ok", ownerID).First(&got).Error; err != nil {
			t.Fatalf("reload endpoint: %v", err)
		}
	})
}
