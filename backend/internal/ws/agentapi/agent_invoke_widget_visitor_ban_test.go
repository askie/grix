package agentapi

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/store"
)

func seedWidgetSessionForBanTest(t *testing.T, ownerID int64, sessionID string) {
	t.Helper()
	seedWidgetSessionWithIPForBanTest(t, ownerID, sessionID, "")
}

func seedWidgetSessionWithIPForBanTest(t *testing.T, ownerID int64, sessionID, lastInitIP string) {
	t.Helper()
	if err := store.DB.Create(&model.WidgetSession{
		SiteID:       990001,
		OwnerUserID:  ownerID,
		VisitorID:    990002,
		VisitorKey:   "vkf_ban_test_" + sessionID,
		SessionID:    sessionID,
		LastInitIP:   lastInitIP,
		Status:       model.WidgetSessionStatusActive,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		LastActiveAt: time.Now().UTC(),
		LastInitAt:   time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("seed widget session: %v", err)
	}
}

func TestDispatchWidgetVisitorBan(t *testing.T) {
	const (
		ownerID = int64(44501)
		agentID = int64(44502)
	)
	_, cleanup := setupAgentInvokeDispatchTest(t)
	defer cleanup()

	t.Run("requires scope", func(t *testing.T) {
		_, code, msg := dispatchAgentInvokeWithHooks(agentID, ownerID, "widget_visitor_ban", map[string]interface{}{
			"session_id": "ws-ban-1",
		}, agentInvokeHooks{})
		if code != 4003 {
			t.Fatalf("without scope code=%d msg=%q, want 4003", code, msg)
		}
	})

	seedAgentInvokeDispatchScope(t, agentID, agentscope.ScopeWidgetVisitorBan)

	t.Run("rejects missing session_id", func(t *testing.T) {
		_, code, _ := dispatchAgentInvokeWithHooks(agentID, ownerID, "widget_visitor_ban", map[string]interface{}{}, agentInvokeHooks{})
		if code != 4001 {
			t.Fatalf("code=%d want 4001", code)
		}
	})

	t.Run("rejects session not owned", func(t *testing.T) {
		_, code, _ := dispatchAgentInvokeWithHooks(agentID, ownerID, "widget_visitor_ban", map[string]interface{}{
			"session_id": "ws-ban-not-exist",
		}, agentInvokeHooks{})
		if code != 4004 {
			t.Fatalf("code=%d want 4004", code)
		}
	})

	t.Run("rejects session owned by another owner", func(t *testing.T) {
		seedWidgetSessionForBanTest(t, ownerID+1000, "ws-ban-other-owner")
		_, code, _ := dispatchAgentInvokeWithHooks(agentID, ownerID, "widget_visitor_ban", map[string]interface{}{
			"session_id": "ws-ban-other-owner",
		}, agentInvokeHooks{})
		if code != 4004 {
			t.Fatalf("code=%d want 4004", code)
		}
	})

	t.Run("bans owned visitor session", func(t *testing.T) {
		seedWidgetSessionForBanTest(t, ownerID, "ws-ban-ok")
		data, code, msg := dispatchAgentInvokeWithHooks(agentID, ownerID, "widget_visitor_ban", map[string]interface{}{
			"session_id": "ws-ban-ok",
		}, agentInvokeHooks{})
		if code != 0 {
			t.Fatalf("code=%d msg=%q, want 0", code, msg)
		}
		m, ok := data.(map[string]interface{})
		if !ok {
			t.Fatalf("unexpected data type %T", data)
		}
		if m["status"] != int16(model.WidgetSessionStatusBanned) {
			t.Fatalf("status=%v want %d", m["status"], model.WidgetSessionStatusBanned)
		}
		var got model.WidgetSession
		if err := store.DB.Where("session_id = ?", "ws-ban-ok").First(&got).Error; err != nil {
			t.Fatalf("reload session: %v", err)
		}
		if got.Status != model.WidgetSessionStatusBanned {
			t.Fatalf("db status=%d want %d", got.Status, model.WidgetSessionStatusBanned)
		}
	})

	t.Run("ban also writes owner-wide IP ban", func(t *testing.T) {
		seedWidgetSessionWithIPForBanTest(t, ownerID, "ws-ban-ip", "203.0.113.77")
		_, code, msg := dispatchAgentInvokeWithHooks(agentID, ownerID, "widget_visitor_ban", map[string]interface{}{
			"session_id": "ws-ban-ip",
		}, agentInvokeHooks{})
		if code != 0 {
			t.Fatalf("code=%d msg=%q, want 0", code, msg)
		}
		var ruleCount int64
		if err := store.DB.Model(&model.WidgetIPBan{}).
			Where("owner_user_id = ? AND ip_cidr = ?", ownerID, "203.0.113.77").
			Count(&ruleCount).Error; err != nil {
			t.Fatalf("count ip bans: %v", err)
		}
		if ruleCount != 1 {
			t.Fatalf("widget_ip_bans rows=%d want 1", ruleCount)
		}
	})
}
