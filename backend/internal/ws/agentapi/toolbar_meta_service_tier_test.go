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

// 绑定卡推送路径必须和 local_action 结果路径守同一条规则：
// session_context 里 service_tier 键存在即权威，null 要把旧档位归位成 default。
// 漏掉这条会让用户在 agent 侧换模型后，工具栏仍显示上一个模型的「快速」档。
func TestPersistBindingFromCard_ServiceTierNullResetsToDefault(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := appstore.DB
	appstore.DB = testDB.DB
	t.Cleanup(func() {
		appstore.DB = originalDB
	})

	mgr := NewManager("", 30*time.Second, (&mockSendMessageHandler{}).handle, nil, nil, nil)
	conn := &agentConn{
		agentID:   9995,
		ownerID:   1016,
		clientID:  "codex-card-tier",
		adapterID: "codex/base",
		send:      make(chan []byte, 4),
	}
	const sessionID = "sess-codex-card-tier"

	// 先落一个具体档位 + 可用列表，模拟用户已经选了「快速」。
	mgr.persistBindingFromCard(conn, sessionID, "/workspace/codex-card", "ready", map[string]any{
		"service_tier": "priority",
		"available_service_tiers": []any{
			map[string]any{"id": "priority", "displayName": "Fast"},
		},
	})

	record, ok, err := toolstore.LoadBinding(context.Background(), conn.agentID, sessionID)
	if err != nil || !ok {
		t.Fatalf("LoadBinding() ok=%v err=%v", ok, err)
	}
	if got := record.Meta["service_tier"]; got != "priority" {
		t.Fatalf("service_tier=%v want=priority", got)
	}

	// 换到不支持速度档的模型：连接器推送 service_tier=null + 空列表。
	// 两个键都必须落库覆盖，不能被当成「没提到」而保留旧值。
	mgr.persistBindingFromCard(conn, sessionID, "", "", map[string]any{
		"service_tier":            nil,
		"available_service_tiers": []any{},
	})

	record, ok, err = toolstore.LoadBinding(context.Background(), conn.agentID, sessionID)
	if err != nil || !ok {
		t.Fatalf("LoadBinding() ok=%v err=%v", ok, err)
	}
	if got := record.Meta["service_tier"]; got != defaultServiceTierID {
		t.Fatalf("service_tier=%v want=%s（null 必须归位标准档，不能残留 priority）", got, defaultServiceTierID)
	}
	tiers, isSlice := record.Meta["available_service_tiers"].([]any)
	if !isSlice || len(tiers) != 0 {
		t.Fatalf("available_service_tiers=%#v want=空数组（旧列表必须被清掉）", record.Meta["available_service_tiers"])
	}
}

// 普通键仍是「有值才覆盖」：插件只上报变化字段，缺省即沿用旧值。
// 这条和上面的 nullable 规则是一体两面，一起锁住才不会被改回去。
func TestMergeToolbarMeta_NonNullableKeysKeepOldValueWhenEmpty(t *testing.T) {
	dst := map[string]any{
		"model_id":     "gpt-5-codex",
		"service_tier": "priority",
	}
	dst = mergeToolbarMeta(dst, map[string]any{
		"model_id":     "",  // 空值 = 没提到，保留旧模型
		"service_tier": nil, // 空值 = 归位标准档
	})

	if got := dst["model_id"]; got != "gpt-5-codex" {
		t.Fatalf("model_id=%v want=gpt-5-codex（普通键的空值不该覆盖旧值）", got)
	}
	if got := dst["service_tier"]; got != defaultServiceTierID {
		t.Fatalf("service_tier=%v want=%s（nullable 键的空值必须覆盖）", got, defaultServiceTierID)
	}
}

func TestMergeToolbarMeta_ExplicitEmptyRateLimitKeysClearStaleValues(t *testing.T) {
	dst := map[string]any{
		"rate_limits": map[string]any{
			"primary":   map[string]any{"usedPercent": 81.0},
			"sampledAt": 1787200800000.0,
		},
		"extra_limits": []any{
			map[string]any{"label": "Claude 5H", "usedPercent": 72.0},
		},
	}
	dst = mergeToolbarMeta(dst, map[string]any{
		"rate_limits":  map[string]any{},
		"extra_limits": []any{},
	})

	if limits, ok := dst["rate_limits"].(map[string]any); !ok || len(limits) != 0 {
		t.Fatalf("rate_limits=%#v want empty map", dst["rate_limits"])
	}
	if extras, ok := dst["extra_limits"].([]any); !ok || len(extras) != 0 {
		t.Fatalf("extra_limits=%#v want empty slice", dst["extra_limits"])
	}
}

// available_efforts 和 available_service_tiers 同构：都是「选项非空才渲染」，
// 空列表必须清掉旧值，否则切到不支持推理力度的模型后，选择器继续显示上一个模型的
// 选项，点下去发的是当前模型不认的值。
// 两条路径都要覆盖：卡片路径靠 nullable 名单，local_action 路径还要求 meta 构造处
// 不能用 != nil 把空值提前滤掉——少一头这个用例就会挂。
func TestAvailableEffortsEmptyClearsStaleOptions(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := appstore.DB
	appstore.DB = testDB.DB
	t.Cleanup(func() {
		appstore.DB = originalDB
	})

	mgr := NewManager("", 30*time.Second, (&mockSendMessageHandler{}).handle, nil, nil, nil)

	// 路径一：卡片推送。先落一份旧的推理力度列表，再收到空数组。
	connCard := &agentConn{
		agentID: 9993, ownerID: 1018, clientID: "codex-efforts-card",
		adapterID: "codex/base", send: make(chan []byte, 4),
	}
	const cardSession = "sess-codex-efforts-card"
	mgr.persistBindingFromCard(connCard, cardSession, "/workspace/e", "ready", map[string]any{
		"available_efforts": []any{"low", "high"},
	})
	mgr.persistBindingFromCard(connCard, cardSession, "/workspace/e", "ready", map[string]any{
		"available_efforts": []any{},
	})
	record, _, _ := toolstore.LoadBinding(context.Background(), connCard.agentID, cardSession)
	if efforts, ok := record.Meta["available_efforts"].([]any); !ok || len(efforts) != 0 {
		t.Fatalf("卡片路径 available_efforts=%v want=空数组（旧选项必须清掉）", record.Meta["available_efforts"])
	}

	// 路径二：local_action 结果。同样先落旧列表，再收到 null。
	connAction := &agentConn{
		agentID: 9992, ownerID: 1019, clientID: "codex-efforts-action",
		adapterID: "codex/base", send: make(chan []byte, 4),
	}
	const actionSession = "sess-codex-efforts-action"
	mgr.persistBindingFromCard(connAction, actionSession, "/workspace/e", "ready", map[string]any{
		"available_efforts": []any{"low", "high"},
	})
	mgr.persistToolbarBinding(connAction, &pendingLocalAction{agentID: connAction.agentID, sessionID: actionSession},
		protocol.LocalActionResultPayload{
			Status: "ok",
			Result: map[string]any{
				"verb":              "set_model",
				"outcome":           "ok",
				"available_efforts": nil, // 切到不支持推理力度的模型
			},
		})
	record, _, _ = toolstore.LoadBinding(context.Background(), connAction.agentID, actionSession)
	if got := record.Meta["available_efforts"]; got != nil {
		t.Fatalf("local_action 路径 available_efforts=%v want=nil（null 必须穿透守卫并清掉旧列表）", got)
	}
}

// local_action 结果路径必须和绑定卡路径走同一个 mergeToolbarMeta：
// 它曾经是一份内联循环 + 单独一句 available_service_tiers 覆盖，行为虽等价，
// 却让「往 toolbarMetaNullableKeys 加个键就两条路径都生效」变成假话——
// 下一个加键的人会以为收口了，实际只有卡片那条生效。这里锁住收口本身。
func TestPersistToolbarBinding_UsesSharedMergeRule(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := appstore.DB
	appstore.DB = testDB.DB
	t.Cleanup(func() {
		appstore.DB = originalDB
	})

	mgr := NewManager("", 30*time.Second, (&mockSendMessageHandler{}).handle, nil, nil, nil)
	conn := &agentConn{
		agentID:   9994,
		ownerID:   1017,
		clientID:  "codex-local-action-tier",
		adapterID: "codex/base",
		send:      make(chan []byte, 4),
	}
	const sessionID = "sess-codex-shared-merge"

	// 先经卡片路径落一个具体档位 + 可用列表 + 一个普通键，模拟用户已选「快速」。
	mgr.persistBindingFromCard(conn, sessionID, "/workspace/codex-shared", "ready", map[string]any{
		"service_tier": "priority",
		"available_service_tiers": []any{
			map[string]any{"id": "priority", "displayName": "Fast"},
		},
		"model_id": "gpt-5-codex",
	})

	// 再走 local_action 路径：用户切到不支持速度档的模型，档位归 null、列表清空。
	mgr.persistToolbarBinding(conn, &pendingLocalAction{agentID: conn.agentID, sessionID: sessionID},
		protocol.LocalActionResultPayload{
			Status: "ok",
			Result: map[string]any{
				"verb":                    "set_model",
				"outcome":                 "ok",
				"available_service_tiers": []any{},
				"session_context": map[string]any{
					"service_tier": nil,
				},
			},
		})

	record, ok, err := toolstore.LoadBinding(context.Background(), conn.agentID, sessionID)
	if err != nil || !ok {
		t.Fatalf("LoadBinding() ok=%v err=%v", ok, err)
	}
	if got := record.Meta["service_tier"]; got != defaultServiceTierID {
		t.Fatalf("service_tier=%v want=%s（走共用规则后 null 必须归位）", got, defaultServiceTierID)
	}
	if tiers, ok := record.Meta["available_service_tiers"].([]any); !ok || len(tiers) != 0 {
		t.Fatalf("available_service_tiers=%v want=空数组（键存在即覆盖）", record.Meta["available_service_tiers"])
	}
	if got := record.Meta["model_id"]; got != "gpt-5-codex" {
		t.Fatalf("model_id=%v want=gpt-5-codex（普通键未提及即沿用）", got)
	}
}
