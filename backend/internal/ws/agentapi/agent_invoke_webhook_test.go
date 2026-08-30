package agentapi

import (
	"strconv"
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

	t.Run("rejects expires_at in the past", func(t *testing.T) {
		seedWebhookSessionMember(t, "wh-sess-past", ownerID, 1)
		seedWebhookSessionMember(t, "wh-sess-past", agentID, 2)
		_, code, msg := invoke(map[string]interface{}{"session_id": "wh-sess-past", "expires_at": "2020-01-01T00:00:00Z"})
		if code != 4001 || !strings.Contains(msg, "past") {
			t.Fatalf("code=%d msg=%q, want 4001 past", code, msg)
		}
	})

	t.Run("caps active endpoints per session", func(t *testing.T) {
		seedWebhookSessionMember(t, "wh-sess-cap", ownerID, 1)
		seedWebhookSessionMember(t, "wh-sess-cap", agentID, 2)
		for i := 0; i < webhook.MaxActiveEndpointsPerSession; i++ {
			if _, code, msg := invoke(map[string]interface{}{"session_id": "wh-sess-cap"}); code != 0 {
				t.Fatalf("create #%d code=%d msg=%q", i, code, msg)
			}
		}
		_, code, msg := invoke(map[string]interface{}{"session_id": "wh-sess-cap"})
		if code != 4001 || !strings.Contains(msg, "too many") {
			t.Fatalf("code=%d msg=%q, want 4001 limit", code, msg)
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

func TestDispatchWebhookListAndDelete(t *testing.T) {
	const (
		ownerID = int64(46601)
		agentID = int64(46602)
		otherID = int64(46603)
	)
	_, cleanup := setupAgentInvokeDispatchTest(t)
	defer cleanup()
	prevDomain, prevSecret := config.C.Server.AgentAPIDomain, config.C.Server.WebhookTokenSecret
	config.C.Server.AgentAPIDomain = "https://api.example.com"
	config.C.Server.WebhookTokenSecret = "test-webhook-secret"
	defer func() {
		config.C.Server.AgentAPIDomain, config.C.Server.WebhookTokenSecret = prevDomain, prevSecret
	}()
	call := func(agent int64, action string, params map[string]interface{}) (interface{}, int, string) {
		return dispatchAgentInvokeWithHooks(agent, ownerID, action, params, agentInvokeHooks{})
	}

	t.Run("list and delete require scope", func(t *testing.T) {
		if _, code, _ := call(agentID, "webhook_list", map[string]interface{}{"session_id": "wl-1"}); code != 4003 {
			t.Fatalf("list code=%d want 4003", code)
		}
		if _, code, _ := call(agentID, "webhook_delete", map[string]interface{}{"id": "1"}); code != 4003 {
			t.Fatalf("delete code=%d want 4003", code)
		}
	})

	seedAgentInvokeDispatchScope(t, agentID, agentscope.ScopeWebhookCreate)
	seedAgentInvokeDispatchScope(t, otherID, agentscope.ScopeWebhookCreate)
	seedWebhookSessionMember(t, "wl-1", ownerID, 1)
	seedWebhookSessionMember(t, "wl-1", agentID, 2)

	t.Run("list is empty before create", func(t *testing.T) {
		data, code, msg := call(agentID, "webhook_list", map[string]interface{}{"session_id": "wl-1"})
		if code != 0 {
			t.Fatalf("code=%d msg=%q", code, msg)
		}
		if items := data.(map[string]interface{})["items"].([]webhook.EndpointView); len(items) != 0 {
			t.Fatalf("items=%d want 0", len(items))
		}
	})

	created, code, msg := call(agentID, "webhook_create", map[string]interface{}{"session_id": "wl-1"})
	if code != 0 {
		t.Fatalf("create code=%d msg=%q", code, msg)
	}
	createdView := created.(*webhook.EndpointView)

	t.Run("list returns the reusable url", func(t *testing.T) {
		data, code, msg := call(agentID, "webhook_list", map[string]interface{}{"session_id": "wl-1"})
		if code != 0 {
			t.Fatalf("code=%d msg=%q", code, msg)
		}
		items := data.(map[string]interface{})["items"].([]webhook.EndpointView)
		if len(items) != 1 || items[0].URL != createdView.URL || items[0].SessionID != "wl-1" || items[0].Status != "active" {
			t.Fatalf("items=%+v", items)
		}
	})

	t.Run("list rejects agent outside the session", func(t *testing.T) {
		if _, code, _ := call(otherID, "webhook_list", map[string]interface{}{"session_id": "wl-1"}); code != 4003 {
			t.Fatalf("code=%d want 4003", code)
		}
	})

	t.Run("delete rejects agent outside the endpoint session", func(t *testing.T) {
		if _, code, _ := call(otherID, "webhook_delete", map[string]interface{}{"id": strconv.FormatInt(createdView.ID, 10)}); code != 4003 {
			t.Fatalf("code=%d want 4003", code)
		}
	})

	t.Run("delete unknown id", func(t *testing.T) {
		if _, code, _ := call(agentID, "webhook_delete", map[string]interface{}{"id": "1"}); code != 4004 {
			t.Fatalf("code=%d want 4004", code)
		}
	})

	t.Run("delete then list is empty", func(t *testing.T) {
		if _, code, msg := call(agentID, "webhook_delete", map[string]interface{}{"id": strconv.FormatInt(createdView.ID, 10)}); code != 0 {
			t.Fatalf("code=%d msg=%q", code, msg)
		}
		data, _, _ := call(agentID, "webhook_list", map[string]interface{}{"session_id": "wl-1"})
		if items := data.(map[string]interface{})["items"].([]webhook.EndpointView); len(items) != 0 {
			t.Fatalf("items=%d want 0 after delete", len(items))
		}
		if _, code, _ := call(agentID, "webhook_delete", map[string]interface{}{"id": strconv.FormatInt(createdView.ID, 10)}); code != 4004 {
			t.Fatalf("second delete code=%d want 4004", code)
		}
	})
}

func TestCreateEndpointLazilyReapsExpired(t *testing.T) {
	const (
		ownerID = int64(46701)
		agentID = int64(46702)
	)
	_, cleanup := setupAgentInvokeDispatchTest(t)
	defer cleanup()
	prevDomain, prevSecret := config.C.Server.AgentAPIDomain, config.C.Server.WebhookTokenSecret
	config.C.Server.AgentAPIDomain = "https://api.example.com"
	config.C.Server.WebhookTokenSecret = "test-webhook-secret"
	defer func() {
		config.C.Server.AgentAPIDomain, config.C.Server.WebhookTokenSecret = prevDomain, prevSecret
	}()
	seedAgentInvokeDispatchScope(t, agentID, agentscope.ScopeWebhookCreate)
	seedWebhookSessionMember(t, "wr-1", ownerID, 1)
	seedWebhookSessionMember(t, "wr-1", agentID, 2)

	first, code, msg := dispatchAgentInvokeWithHooks(agentID, ownerID, "webhook_create", map[string]interface{}{"session_id": "wr-1"}, agentInvokeHooks{})
	if code != 0 {
		t.Fatalf("code=%d msg=%q", code, msg)
	}
	firstID := first.(*webhook.EndpointView).ID
	if err := store.DB.Model(&model.WebhookEndpoint{}).Where("id = ?", firstID).Update("expires_at", "2020-01-01 00:00:00").Error; err != nil {
		t.Fatalf("expire: %v", err)
	}
	if _, code, msg := dispatchAgentInvokeWithHooks(agentID, ownerID, "webhook_create", map[string]interface{}{"session_id": "wr-1"}, agentInvokeHooks{}); code != 0 {
		t.Fatalf("second create code=%d msg=%q", code, msg)
	}
	var reaped model.WebhookEndpoint
	if err := store.DB.Unscoped().Where("id = ?", firstID).First(&reaped).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reaped.DeletedAt == nil {
		t.Fatalf("expired endpoint was not reaped")
	}
}
