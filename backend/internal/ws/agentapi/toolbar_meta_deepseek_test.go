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

func TestMergeToolbarMetaProviderChangeClearsStaleModels(t *testing.T) {
	dst := map[string]any{
		"provider_id":      "deepseek-official",
		"model_id":         "deepseek-v4-pro",
		"available_models": []any{map[string]any{"id": "deepseek-v4-pro"}},
	}
	dst = mergeToolbarMeta(dst, map[string]any{"provider_id": "opencode-go"})
	models, ok := dst["available_models"].([]any)
	if !ok || len(models) != 0 {
		t.Fatalf("available_models=%#v want empty after provider switch", dst["available_models"])
	}
	if _, ok := dst["model_id"]; ok {
		t.Fatalf("model_id=%#v want cleared after provider switch", dst["model_id"])
	}
	if dst["provider_id"] != "opencode-go" {
		t.Fatalf("provider_id=%#v", dst["provider_id"])
	}
}

func TestMergeToolbarMetaProviderChangeKeepsIncomingModels(t *testing.T) {
	dst := map[string]any{
		"provider_id":      "deepseek-official",
		"model_id":         "deepseek-v4-pro",
		"available_models": []any{map[string]any{"id": "deepseek-v4-pro"}},
	}
	dst = mergeToolbarMeta(dst, map[string]any{
		"provider_id":      "opencode-go",
		"model_id":         "go-model",
		"available_models": []any{map[string]any{"id": "go-model"}},
	})
	models, ok := dst["available_models"].([]any)
	if !ok || len(models) != 1 {
		t.Fatalf("available_models=%#v", dst["available_models"])
	}
	if dst["model_id"] != "go-model" {
		t.Fatalf("model_id=%#v", dst["model_id"])
	}
}

func TestMergeToolbarMetaFirstProviderDoesNotClearModels(t *testing.T) {
	dst := map[string]any{
		"model_id":         "deepseek-v4-pro",
		"available_models": []any{map[string]any{"id": "deepseek-v4-pro"}},
	}
	dst = mergeToolbarMeta(dst, map[string]any{"provider_id": "deepseek-official"})
	models, ok := dst["available_models"].([]any)
	if !ok || len(models) != 1 || dst["model_id"] != "deepseek-v4-pro" {
		t.Fatalf("meta=%#v", dst)
	}
}

func TestMergeToolbarMetaDeepSeekExplicitClears(t *testing.T) {
	dst := map[string]any{
		"available_models": []any{"old"}, "available_providers": []any{"old-provider"},
		"applied_model_id": "old-model", "applied_provider_id": "old-provider",
		"applied_mode_id": "old-mode", "applied_settings_revision": float64(9),
		"context_window": map[string]any{"usedPercentage": 50.0},
		"provider_quota": map[string]any{"success": true}, "settings_error_code": "old_error",
	}
	dst = mergeToolbarMeta(dst, map[string]any{
		"available_models": []any{}, "available_providers": []any{},
		"applied_model_id": nil, "applied_provider_id": nil, "applied_mode_id": nil,
		"applied_settings_revision": nil, "context_window": nil,
		"provider_quota": nil, "settings_error_code": nil,
	})
	if models, ok := dst["available_models"].([]any); !ok || len(models) != 0 {
		t.Fatalf("available_models=%#v", dst["available_models"])
	}
	if providers, ok := dst["available_providers"].([]any); !ok || len(providers) != 0 {
		t.Fatalf("available_providers=%#v", dst["available_providers"])
	}
	for _, key := range []string{"applied_model_id", "applied_provider_id", "applied_mode_id", "applied_settings_revision", "context_window", "provider_quota", "settings_error_code"} {
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
		"available_providers": []any{map[string]any{"id": "old-provider"}},
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
			"available_models":    []any{},
			"available_providers": []any{},
			"settingsRevision":    float64(5),
			"session_context": map[string]any{
				"applied_model_id": nil, "applied_mode_id": nil, "applied_provider_id": nil,
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
	if providers, ok := record.Meta["available_providers"].([]any); !ok || len(providers) != 0 {
		t.Fatalf("available_providers=%#v", record.Meta["available_providers"])
	}
}

func TestPersistToolbarBindingSetProviderRefreshesCatalog(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	originalDB := appstore.DB
	appstore.DB = testDB.DB
	t.Cleanup(func() { appstore.DB = originalDB })

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	conn := &agentConn{agentID: 9973, ownerID: 1073, clientID: "deepseek-provider", adapterID: "deepseek/grix-bridge-v1"}
	const sessionID = "sess-deepseek-provider"
	mgr.persistBindingFromCard(conn, sessionID, "/workspace/deepseek", "ready", map[string]any{
		"provider_id":         "deepseek-official",
		"model_id":            "deepseek-v4-pro",
		"applied_provider_id": "deepseek-official",
		"applied_model_id":    "deepseek-v4-pro",
		"available_providers": []any{map[string]any{"id": "deepseek-official", "displayName": "DeepSeek"}},
		"available_models":    []any{map[string]any{"id": "deepseek-v4-pro", "displayName": "DeepSeek-V4-Pro"}},
	})
	mgr.persistToolbarBinding(conn, &pendingLocalAction{
		agentID: conn.agentID, sessionID: sessionID, kind: "set_provider", referenceID: "opencode-go",
	}, protocol.LocalActionResultPayload{
		Status: "ok",
		Result: map[string]any{
			"provider_id":         "opencode-go",
			"available_providers": []any{map[string]any{"id": "deepseek-official"}, map[string]any{"id": "opencode-go"}},
			"available_models":    []any{map[string]any{"id": "go-model", "displayName": "Go Model"}},
			"session_context": map[string]any{
				"provider_id":               "opencode-go",
				"model_id":                  "go-model",
				"applied_provider_id":       "opencode-go",
				"applied_model_id":          "go-model",
				"applied_settings_revision": float64(8),
				"settings_revision":         float64(8),
				"settings_state":            "applied",
			},
		},
	})
	record, ok, err := toolstore.LoadBinding(context.Background(), conn.agentID, sessionID)
	if err != nil || !ok {
		t.Fatalf("LoadBinding ok=%v err=%v", ok, err)
	}
	if record.Meta["provider_id"] != "opencode-go" || record.Meta["model_id"] != "go-model" {
		t.Fatalf("selection=%#v", record.Meta)
	}
	if record.Meta["applied_provider_id"] != "opencode-go" || record.Meta["applied_model_id"] != "go-model" {
		t.Fatalf("applied=%#v", record.Meta)
	}
	models, ok := record.Meta["available_models"].([]any)
	if !ok || len(models) != 1 {
		t.Fatalf("available_models=%#v", record.Meta["available_models"])
	}
	providers, ok := record.Meta["available_providers"].([]any)
	if !ok || len(providers) != 2 {
		t.Fatalf("available_providers=%#v", record.Meta["available_providers"])
	}
}

func TestPersistToolbarBindingSetPresetLocksScene(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	originalDB := appstore.DB
	appstore.DB = testDB.DB
	t.Cleanup(func() { appstore.DB = originalDB })

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	conn := &agentConn{agentID: 9974, ownerID: 1074, clientID: "deepseek-preset", adapterID: "deepseek/grix-bridge-v1"}
	const sessionID = "sess-deepseek-preset"
	mgr.persistBindingFromCard(conn, sessionID, "/workspace/deepseek", "ready", map[string]any{
		"agent_preset_id":     "standard",
		"agent_preset_locked": false,
		"available_presets":   []any{map[string]any{"id": "standard", "displayName": "标准模式"}},
	})
	mgr.persistToolbarBinding(conn, &pendingLocalAction{
		agentID: conn.agentID, sessionID: sessionID, kind: "set_preset", referenceID: "code",
	}, protocol.LocalActionResultPayload{
		Status: "ok",
		Result: map[string]any{
			"agent_preset_id":     "code",
			"agent_preset_locked": false,
			"available_presets": []any{
				map[string]any{"id": "standard", "displayName": "标准模式"},
				map[string]any{"id": "code", "displayName": "PTC 模式"},
			},
			"session_context": map[string]any{
				"agent_preset_id":     "code",
				"agent_preset_locked": false,
			},
		},
	})
	record, ok, err := toolstore.LoadBinding(context.Background(), conn.agentID, sessionID)
	if err != nil || !ok {
		t.Fatalf("LoadBinding ok=%v err=%v", ok, err)
	}
	if record.Meta["agent_preset_id"] != "code" || record.Meta["agent_preset_locked"] != false {
		t.Fatalf("preset=%#v", record.Meta)
	}
	presets, ok := record.Meta["available_presets"].([]any)
	if !ok || len(presets) != 2 {
		t.Fatalf("available_presets=%#v", record.Meta["available_presets"])
	}
}

func TestPersistBindingFromCardSceneOnlyKeepsAppliedProjection(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	originalDB := appstore.DB
	appstore.DB = testDB.DB
	t.Cleanup(func() { appstore.DB = originalDB })

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	conn := &agentConn{agentID: 9976, ownerID: 1076, clientID: "deepseek-scene-echo", adapterID: "deepseek/grix-bridge-v1"}
	const sessionID = "sess-deepseek-scene-echo"
	mgr.persistBindingFromCard(conn, sessionID, "/workspace/deepseek", "ready", map[string]any{
		"settings_state":            "applied",
		"applied_model_id":          "deepseek-v4-pro",
		"applied_provider_id":       "deepseek-official",
		"applied_settings_revision": float64(4),
		"context_window":            map[string]any{"usedPercentage": 12.0},
		"provider_quota":            map[string]any{"success": true},
		"agent_preset_id":           "standard",
	})
	mgr.persistBindingFromCard(conn, sessionID, "/workspace/deepseek", "ready", map[string]any{
		"agent_preset_id":     "code",
		"agent_preset_locked": true,
		"available_presets":   []any{map[string]any{"id": "code", "displayName": "PTC 模式"}},
	})
	record, ok, err := toolstore.LoadBinding(context.Background(), conn.agentID, sessionID)
	if err != nil || !ok {
		t.Fatalf("LoadBinding ok=%v err=%v", ok, err)
	}
	if record.Meta["agent_preset_id"] != "code" || record.Meta["agent_preset_locked"] != true {
		t.Fatalf("preset=%#v", record.Meta)
	}
	if record.Meta["settings_state"] != "applied" || record.Meta["applied_model_id"] != "deepseek-v4-pro" {
		t.Fatalf("applied=%#v", record.Meta)
	}
	if record.Meta["context_window"] == nil || record.Meta["provider_quota"] == nil {
		t.Fatalf("quota/context cleared=%#v", record.Meta)
	}
}

func TestPersistToolbarBindingProjectsDshPlugins(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	originalDB := appstore.DB
	appstore.DB = testDB.DB
	t.Cleanup(func() { appstore.DB = originalDB })

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	conn := &agentConn{agentID: 9978, ownerID: 1078, clientID: "deepseek-plugins", adapterID: "deepseek/grix-bridge-v1"}
	const sessionID = "sess-deepseek-plugins"
	mgr.persistToolbarBinding(conn, &pendingLocalAction{
		agentID: conn.agentID, sessionID: sessionID, kind: "dsh_enable_plugin",
	}, protocol.LocalActionResultPayload{
		Status: "ok",
		Result: map[string]any{
			"outcome": "plugin_updated",
			"dsh_plugins": []any{
				map[string]any{"name": "@acme/dsh-notes", "enabled": true, "locked": false},
			},
			"dsh_plugin_restart_required": true,
			"session_context": map[string]any{
				"dsh_plugins": []any{
					map[string]any{"name": "@acme/dsh-notes", "enabled": true, "locked": false},
				},
				"dsh_plugin_restart_required": true,
			},
		},
	})
	record, ok, err := toolstore.LoadBinding(context.Background(), conn.agentID, sessionID)
	if err != nil || !ok {
		t.Fatalf("LoadBinding ok=%v err=%v", ok, err)
	}
	plugins, ok := record.Meta["dsh_plugins"].([]any)
	if !ok || len(plugins) != 1 {
		t.Fatalf("dsh_plugins=%#v", record.Meta["dsh_plugins"])
	}
	if record.Meta["dsh_plugin_restart_required"] != true {
		t.Fatalf("restart=%#v", record.Meta["dsh_plugin_restart_required"])
	}
}

func TestPersistToolbarBindingOpenResultStoresScene(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	originalDB := appstore.DB
	appstore.DB = testDB.DB
	t.Cleanup(func() { appstore.DB = originalDB })

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	conn := &agentConn{agentID: 9975, ownerID: 1075, clientID: "deepseek-open", adapterID: "deepseek/grix-bridge-v1"}
	const sessionID = "sess-deepseek-open"
	mgr.persistToolbarBinding(conn, &pendingLocalAction{
		agentID: conn.agentID, sessionID: sessionID, kind: "session_control",
	}, protocol.LocalActionResultPayload{
		Status: "ok",
		Result: map[string]any{
			"outcome": "opened",
			"binding": map[string]any{
				"cwd":                 "/workspace/deepseek",
				"workerStatus":        "ready",
				"agent_preset_id":     "code",
				"agent_preset_locked": false,
				"available_presets": []any{
					map[string]any{"id": "standard", "displayName": "标准模式"},
					map[string]any{"id": "code", "displayName": "PTC 模式"},
				},
			},
		},
	})
	record, ok, err := toolstore.LoadBinding(context.Background(), conn.agentID, sessionID)
	if err != nil || !ok {
		t.Fatalf("LoadBinding ok=%v err=%v", ok, err)
	}
	if record.Meta["agent_preset_id"] != "code" {
		t.Fatalf("preset=%#v", record.Meta)
	}
	presets, ok := record.Meta["available_presets"].([]any)
	if !ok || len(presets) != 2 {
		t.Fatalf("available_presets=%#v", record.Meta["available_presets"])
	}
}

func TestPersistToolbarBindingOpenResultKeepsAppliedProjection(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	originalDB := appstore.DB
	appstore.DB = testDB.DB
	t.Cleanup(func() { appstore.DB = originalDB })

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	conn := &agentConn{agentID: 9977, ownerID: 1077, clientID: "deepseek-open-keep", adapterID: "deepseek/grix-bridge-v1"}
	const sessionID = "sess-deepseek-open-keep"
	mgr.persistBindingFromCard(conn, sessionID, "/workspace/deepseek", "ready", map[string]any{
		"settings_state":            "applied",
		"applied_model_id":          "deepseek-v4-pro",
		"applied_provider_id":       "deepseek-official",
		"applied_settings_revision": float64(4),
		"context_window":            map[string]any{"usedPercentage": 12.0},
		"provider_quota":            map[string]any{"success": true},
		"agent_preset_id":           "standard",
	})
	mgr.persistToolbarBinding(conn, &pendingLocalAction{
		agentID: conn.agentID, sessionID: sessionID, kind: "session_control",
	}, protocol.LocalActionResultPayload{
		Status: "ok",
		Result: map[string]any{
			"outcome": "opened",
			"binding": map[string]any{
				"cwd":                 "/workspace/deepseek",
				"workerStatus":        "ready",
				"agent_preset_id":     "code",
				"agent_preset_locked": true,
				"available_presets":   []any{map[string]any{"id": "code", "displayName": "PTC 模式"}},
			},
		},
	})
	record, ok, err := toolstore.LoadBinding(context.Background(), conn.agentID, sessionID)
	if err != nil || !ok {
		t.Fatalf("LoadBinding ok=%v err=%v", ok, err)
	}
	if record.Meta["agent_preset_id"] != "code" || record.Meta["agent_preset_locked"] != true {
		t.Fatalf("preset=%#v", record.Meta)
	}
	if record.Meta["settings_state"] != "applied" || record.Meta["applied_model_id"] != "deepseek-v4-pro" {
		t.Fatalf("applied=%#v", record.Meta)
	}
	if record.Meta["context_window"] == nil || record.Meta["provider_quota"] == nil {
		t.Fatalf("quota/context cleared=%#v", record.Meta)
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
