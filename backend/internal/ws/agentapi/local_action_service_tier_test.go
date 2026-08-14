package agentapi

import (
	"context"
	"testing"
	"time"

	toolstore "github.com/askie/grix/backend/internal/agenttoolbar/store"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	appstore "github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// set_service_tier 结果持久化：session_context.serviceTier 与 available_service_tiers
// 写入工具栏 binding meta；切回标准档（serviceTier=null）时靠 referenceID 兜底写 default。
func TestHandleLocalActionResult_CodexServiceTierPersistsToolbarMeta(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := appstore.DB
	appstore.DB = testDB.DB
	t.Cleanup(func() {
		appstore.DB = originalDB
	})

	sendHandler := &mockSendMessageHandler{}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	conn := &agentConn{
		agentID:      9994,
		ownerID:      1015,
		clientID:     "codex-service-tier",
		adapterID:    "codex/base",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"set_service_tier"},
		send:         make(chan []byte, 4),
	}
	mgr.putConnForTest(conn)

	// 1) 选择 priority：session_context 与 available_service_tiers 持久化
	mgr.storePendingLocalAction(&pendingLocalAction{
		actionID:    "act-codex-set-tier",
		kind:        "set_service_tier",
		agentID:     conn.agentID,
		ownerID:     conn.ownerID,
		sessionID:   "sess-codex-service-tier",
		referenceID: "priority",
	})
	mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
		ActionID: "act-codex-set-tier",
		Status:   "ok",
		Result: map[string]any{
			"binding": map[string]any{
				"aibotSessionId": "sess-codex-service-tier",
				"cwd":            "/workspace/codex-tier",
				"workerStatus":   "ready",
			},
			"session_context": map[string]any{
				"serviceTier": "priority",
			},
			"available_service_tiers": []any{
				map[string]any{"id": "priority", "displayName": "Fast", "description": "1.5x speed, increased usage"},
			},
		},
	}))

	record, ok, err := toolstore.LoadBinding(context.Background(), conn.agentID, "sess-codex-service-tier")
	if err != nil {
		t.Fatalf("LoadBinding() error = %v", err)
	}
	if !ok {
		t.Fatal("expected toolbar binding to be persisted")
	}
	if got := record.Meta["service_tier"]; got != "priority" {
		t.Fatalf("service_tier=%v want=priority", got)
	}
	tiers, ok := record.Meta["available_service_tiers"].([]any)
	if !ok || len(tiers) != 1 {
		t.Fatalf("available_service_tiers=%#v want len=1", record.Meta["available_service_tiers"])
	}

	// 2) 切回标准档：连接器返回 serviceTier=null，靠 referenceID 兜底写 default
	mgr.storePendingLocalAction(&pendingLocalAction{
		actionID:    "act-codex-reset-tier",
		kind:        "set_service_tier",
		agentID:     conn.agentID,
		ownerID:     conn.ownerID,
		sessionID:   "sess-codex-service-tier",
		referenceID: "default",
	})
	mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 2, protocol.LocalActionResultPayload{
		ActionID: "act-codex-reset-tier",
		Status:   "ok",
		Result: map[string]any{
			"session_context": map[string]any{
				"serviceTier": nil,
			},
		},
	}))

	record, ok, err = toolstore.LoadBinding(context.Background(), conn.agentID, "sess-codex-service-tier")
	if err != nil {
		t.Fatalf("LoadBinding() error = %v", err)
	}
	if !ok {
		t.Fatal("expected toolbar binding after reset")
	}
	if got := record.Meta["service_tier"]; got != "default" {
		t.Fatalf("service_tier=%v want=default (reset via referenceID fallback)", got)
	}
}

// 请求档位未被连接器采纳（ok 但 serviceTier=null）时不得把请求档位当成功持久化；
// 切模型后 serviceTier=null + available_service_tiers=[] 须归位并清空列表。
func TestHandleLocalActionResult_CodexServiceTierNotAppliedAndModelSwitchReset(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := appstore.DB
	appstore.DB = testDB.DB
	t.Cleanup(func() {
		appstore.DB = originalDB
	})

	sendHandler := &mockSendMessageHandler{}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	conn := &agentConn{
		agentID:      9995,
		ownerID:      1016,
		clientID:     "codex-service-tier-guard",
		adapterID:    "codex/base",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"set_service_tier", "set_model"},
		send:         make(chan []byte, 4),
	}
	mgr.putConnForTest(conn)

	// 1) 请求 priority，但连接器守卫归一为标准档（serviceTier=null）→ 不得写 priority
	mgr.storePendingLocalAction(&pendingLocalAction{
		actionID:    "act-tier-not-applied",
		kind:        "set_service_tier",
		agentID:     conn.agentID,
		ownerID:     conn.ownerID,
		sessionID:   "sess-codex-tier-guard",
		referenceID: "priority",
	})
	mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
		ActionID: "act-tier-not-applied",
		Status:   "ok",
		Result: map[string]any{
			"binding": map[string]any{
				"aibotSessionId": "sess-codex-tier-guard",
				"cwd":            "/workspace/tier-guard",
				"workerStatus":   "ready",
			},
			"session_context": map[string]any{
				"serviceTier": nil,
			},
		},
	}))

	record, ok, err := toolstore.LoadBinding(context.Background(), conn.agentID, "sess-codex-tier-guard")
	if err != nil {
		t.Fatalf("LoadBinding() error = %v", err)
	}
	if !ok {
		t.Fatal("expected toolbar binding to be persisted")
	}
	if got := record.Meta["service_tier"]; got != "default" {
		t.Fatalf("service_tier=%v want=default (requested tier not applied by connector)", got)
	}

	// 2) 先造一个已持久化 priority + 列表的状态，再切模型：
	//    结果 serviceTier=null 且 available_service_tiers=[] → 归位 default 并清空列表
	record.Meta["service_tier"] = "priority"
	record.Meta["available_service_tiers"] = []any{
		map[string]any{"id": "priority", "displayName": "Fast"},
	}
	if err := toolstore.UpsertBinding(context.Background(), record); err != nil {
		t.Fatalf("seed binding: %v", err)
	}
	mgr.storePendingLocalAction(&pendingLocalAction{
		actionID:    "act-tier-model-switch",
		kind:        "set_model",
		agentID:     conn.agentID,
		ownerID:     conn.ownerID,
		sessionID:   "sess-codex-tier-guard",
		referenceID: "gpt-5.4-mini",
	})
	mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 2, protocol.LocalActionResultPayload{
		ActionID: "act-tier-model-switch",
		Status:   "ok",
		Result: map[string]any{
			"session_context": map[string]any{
				"modelId":     "gpt-5.4-mini",
				"serviceTier": nil,
			},
			"available_service_tiers": []any{},
		},
	}))

	record, ok, err = toolstore.LoadBinding(context.Background(), conn.agentID, "sess-codex-tier-guard")
	if err != nil {
		t.Fatalf("LoadBinding() error = %v", err)
	}
	if !ok {
		t.Fatal("expected toolbar binding after model switch")
	}
	if got := record.Meta["service_tier"]; got != "default" {
		t.Fatalf("service_tier=%v want=default (model switch resets tier)", got)
	}
	tiers, isSlice := record.Meta["available_service_tiers"].([]any)
	if !isSlice || len(tiers) != 0 {
		t.Fatalf("available_service_tiers=%#v want empty (cleared on model switch)", record.Meta["available_service_tiers"])
	}
}
