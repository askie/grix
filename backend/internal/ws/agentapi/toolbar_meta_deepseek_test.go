package agentapi

import (
	"context"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/agenttoolbar/agents/shared"
	toolstore "github.com/askie/grix/backend/internal/agenttoolbar/store"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	appstore "github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestMergeToolbarMetaDeepSeekExplicitClears(t *testing.T) {
	dst := map[string]any{
		"available_models": []any{"old"}, "applied_model_id": "old-model",
		"applied_mode_id": "old-mode", "applied_settings_revision": float64(9),
		"context_window": map[string]any{"usedPercentage": 50.0},
		"provider_quota": map[string]any{"success": true}, "settings_error_code": "old_error",
	}
	dst = mergeToolbarMeta(dst, map[string]any{
		"available_models": []any{}, "applied_model_id": nil, "applied_mode_id": nil,
		"applied_settings_revision": nil, "context_window": nil,
		"provider_quota": nil, "settings_error_code": nil,
	})
	if models, ok := dst["available_models"].([]any); !ok || len(models) != 0 {
		t.Fatalf("available_models=%#v", dst["available_models"])
	}
	for _, key := range []string{"applied_model_id", "applied_mode_id", "applied_settings_revision", "context_window", "provider_quota", "settings_error_code"} {
		if value, ok := dst[key]; !ok || value != nil {
			t.Fatalf("%s=%#v present=%v", key, value, ok)
		}
	}
}

func TestDeepSeekMetaClearsThroughCardAndLocalActionPaths(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	originalDB := appstore.DB
	appstore.DB = testDB.DB
	t.Cleanup(func() { appstore.DB = originalDB })

	mgr := NewManager("", 30*time.Second, (&mockSendMessageHandler{}).handle, nil, nil, nil)
	conn := &agentConn{agentID: 9971, ownerID: 1071, clientID: "deepseek-meta", adapterID: "deepseek/jsonrpc-v1", send: make(chan []byte, 4)}
	const sessionID = "sess-deepseek-meta"
	initial := map[string]any{
		"available_models": []any{map[string]any{"id": "old"}},
		"applied_model_id": "old", "applied_mode_id": "approval",
		"applied_settings_revision": float64(4), "context_window": map[string]any{"usedPercentage": 10.0},
		"provider_quota": map[string]any{"success": true}, "settings_error_code": "runtime_failed",
	}
	mgr.persistBindingFromCard(conn, sessionID, "/workspace/deepseek", "idle", initial)
	mgr.persistBindingFromCard(conn, sessionID, "", "", map[string]any{
		"context_window": nil, "provider_quota": nil,
	})

	mgr.persistToolbarBinding(conn, &pendingLocalAction{agentID: conn.agentID, sessionID: sessionID}, protocol.LocalActionResultPayload{
		Status: "ok",
		Result: map[string]any{
			"available_models": []any{},
			"settingsRevision": float64(5),
			"session_context": map[string]any{
				"applied_model_id": nil, "applied_mode_id": nil,
				"applied_settings_revision": nil, "settings_state": "pending", "settings_error_code": nil,
			},
		},
	})
	record, ok, err := toolstore.LoadBinding(context.Background(), conn.agentID, sessionID)
	if err != nil || !ok {
		t.Fatalf("LoadBinding ok=%v err=%v", ok, err)
	}
	if record.Meta["context_window"] != nil || record.Meta["provider_quota"] != nil || record.Meta["applied_model_id"] != nil {
		t.Fatalf("meta=%#v", record.Meta)
	}
	if record.Meta["settings_state"] != "pending" || record.Meta["settings_revision"] != float64(5) {
		t.Fatalf("settings projection=%#v", record.Meta)
	}
	if models, ok := record.Meta["available_models"].([]any); !ok || len(models) != 0 {
		t.Fatalf("available_models=%#v", record.Meta["available_models"])
	}
}

func TestPersistRateLimitsResultStoresFailureAndExplicitClear(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	originalDB := appstore.DB
	appstore.DB = testDB.DB
	t.Cleanup(func() { appstore.DB = originalDB })

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	pending := &pendingLocalAction{agentID: 9972, sessionID: "sess-deepseek-quota"}
	mgr.persistRateLimitsResult(pending, protocol.LocalActionResultPayload{Status: "ok", Result: map[string]any{
		"contextWindow": map[string]any{"usedTokens": float64(12), "totalTokens": float64(100), "usedPercentage": float64(12)},
		"providerQuota": map[string]any{"provider": "deepseek", "providerLabel": "DeepSeek", "success": false, "error": "balance_unavailable"},
	}})
	record, ok, err := toolstore.LoadBinding(context.Background(), pending.agentID, pending.sessionID)
	if err != nil || !ok {
		t.Fatalf("LoadBinding ok=%v err=%v", ok, err)
	}
	quota := shared.ParseProviderQuota(record.Meta)
	if quota == nil || quota.Error != "balance_unavailable" {
		t.Fatalf("quota=%+v meta=%#v", quota, record.Meta)
	}

	mgr.persistRateLimitsResult(pending, protocol.LocalActionResultPayload{Status: "ok", Result: map[string]any{
		"contextWindow": nil, "providerQuota": nil,
	}})
	record, _, _ = toolstore.LoadBinding(context.Background(), pending.agentID, pending.sessionID)
	if record.Meta["context_window"] != nil || record.Meta["provider_quota"] != nil {
		t.Fatalf("cleared meta=%#v", record.Meta)
	}
}
