package core

import (
	"context"
	"testing"

	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	"github.com/askie/grix/backend/internal/model"
)

func visibleSnapshot() toolprotocol.Snapshot {
	return toolprotocol.Snapshot{Visible: true, Items: []toolprotocol.Item{{ItemID: "x"}}}
}

func relayInput(relay *RelayInfo) BuildInput {
	return BuildInput{
		OwnerID: 1001,
		Session: SessionInfo{SessionID: "sess-1"},
		Agent:   AgentInfo{AgentID: 9001, OwnerID: 1001, ClientType: model.AgentClientTypeKimi},
		Relay:   relay,
	}
}

// 注入规则：无投影/不可见/余额不足且未开启 → 不注入；余额不足但已开启 → 必须保留关闭入口。
func TestAppendGatewayRelayToggle_VisibilityRules(t *testing.T) {
	if got := appendGatewayRelayToggle(visibleSnapshot(), relayInput(nil)); len(got.Items) != 1 {
		t.Fatalf("nil relay should not inject, got %d items", len(got.Items))
	}
	invisible := toolprotocol.Snapshot{Visible: false, Items: []toolprotocol.Item{}}
	relay := &RelayInfo{BalancePositive: true}
	if got := appendGatewayRelayToggle(invisible, relayInput(relay)); len(got.Items) != 0 {
		t.Fatalf("invisible snapshot should not inject, got %d items", len(got.Items))
	}
	if got := appendGatewayRelayToggle(visibleSnapshot(), relayInput(&RelayInfo{})); len(got.Items) != 1 {
		t.Fatalf("no balance + disabled should not inject, got %d items", len(got.Items))
	}
	got := appendGatewayRelayToggle(visibleSnapshot(), relayInput(&RelayInfo{Enabled: true, BalancePositive: false, Revision: 3}))
	if len(got.Items) != 2 {
		t.Fatalf("enabled with empty balance must keep the disable entry, got %d items", len(got.Items))
	}
	if got.Items[0].ActionID != "gateway_relay_toggle:disable:3" {
		t.Fatalf("enabled state must offer disable action, got %q", got.Items[0].ActionID)
	}
}

// 两态文案与版本化 ActionID：关闭态给 enable 动作，开启态 Selected + 模型徽标 + disable 动作。
func TestAppendGatewayRelayToggle_TwoStates(t *testing.T) {
	off := appendGatewayRelayToggle(visibleSnapshot(), relayInput(&RelayInfo{BalancePositive: true, RelayModel: "kimi-k2", Revision: 0}))
	item := off.Items[0]
	if item.ItemID != "gateway_relay_toggle" || item.Kind != toolprotocol.ItemKindButton {
		t.Fatalf("unexpected item: %+v", item)
	}
	if item.ActionID != "gateway_relay_toggle:enable:0" {
		t.Fatalf("off state action = %q", item.ActionID)
	}
	if item.Selected || item.Label != "官方" || item.Variant != "neutral" {
		t.Fatalf("off state render = %+v", item)
	}
	if item.BadgeText != "" {
		t.Fatalf("off state must not show the model badge, got %q", item.BadgeText)
	}

	on := appendGatewayRelayToggle(visibleSnapshot(), relayInput(&RelayInfo{
		Enabled: true, BalancePositive: true, RelayModel: "kimi-k2", Revision: 7, Applied: true, StateKnown: true,
	}))
	item = on.Items[0]
	if item.BadgeText != "kimi-k2" {
		t.Fatalf("on state should show the relay model badge, got %q", item.BadgeText)
	}
	if item.ActionID != "gateway_relay_toggle:disable:7" || !item.Selected || item.Label != "中转" || item.Variant != "primary" {
		t.Fatalf("on state render = %+v", item)
	}
	if item.Tooltip != "中转模式已生效，点击切回官方模式" {
		t.Fatalf("applied tooltip = %q", item.Tooltip)
	}

	offUnknown := appendGatewayRelayToggle(visibleSnapshot(), relayInput(&RelayInfo{BalancePositive: true}))
	if offUnknown.Items[0].Tooltip != "官方模式，实际状态未知（设备离线或连接器过旧），点击切换为中转模式" {
		t.Fatalf("off unknown tooltip = %q", offUnknown.Items[0].Tooltip)
	}
	offKnown := appendGatewayRelayToggle(visibleSnapshot(), relayInput(&RelayInfo{BalancePositive: true, StateKnown: true}))
	if offKnown.Items[0].Tooltip != "官方模式，点击切换为中转模式" {
		t.Fatalf("off known tooltip = %q", offKnown.Items[0].Tooltip)
	}
	// 关闭已写 desired 但 connector 尚未回执：不能说已回官方，要显示待生效。
	// 关闭已写 desired 但回执未覆盖当前 revision：不能说已回官方，要显示待生效。
	// （applied_revision 才是确认依据；失败/超时/断连也会写 applied_at，不可信。）
	offPending := appendGatewayRelayToggle(visibleSnapshot(), relayInput(&RelayInfo{BalancePositive: true, StateKnown: true, Revision: 2}))
	if offPending.Items[0].Tooltip != "官方模式待生效，等待连接器确认" {
		t.Fatalf("off pending tooltip = %q", offPending.Items[0].Tooltip)
	}
	offConfirmed := appendGatewayRelayToggle(visibleSnapshot(), relayInput(&RelayInfo{BalancePositive: true, StateKnown: true, Revision: 2, AppliedRevision: 2}))
	if offConfirmed.Items[0].Tooltip != "官方模式，点击切换为中转模式" {
		t.Fatalf("off confirmed tooltip = %q", offConfirmed.Items[0].Tooltip)
	}

	pending := appendGatewayRelayToggle(visibleSnapshot(), relayInput(&RelayInfo{Enabled: true, StateKnown: true}))
	if pending.Items[0].Tooltip != "中转模式待生效，点击切回官方模式" {
		t.Fatalf("pending tooltip = %q", pending.Items[0].Tooltip)
	}
	unknown := appendGatewayRelayToggle(visibleSnapshot(), relayInput(&RelayInfo{Enabled: true}))
	if unknown.Items[0].Tooltip != "中转实际状态未知（设备离线或连接器过旧），点击切回官方模式" {
		t.Fatalf("unknown tooltip = %q", unknown.Items[0].Tooltip)
	}
}

// 旧快照点击（ActionID 的 relay revision 与当前不符）必须拒绝并强推新快照，绝不反向取反。
func TestHandleGatewayRelayToggle_StaleActionRejected(t *testing.T) {
	cache := &testCache{reserveOK: true, snapshot: toolprotocol.Snapshot{Revision: 2}}
	notifier := &recordingNotifier{}
	svc := NewService(
		testResolver{buildInput: relayInput(&RelayInfo{BalancePositive: true, Revision: 5})},
		testRegistry{pkg: testPackage{snapshot: toolprotocol.Snapshot{
			Visible: true,
			Items:   []toolprotocol.Item{{ItemID: "x"}},
		}}},
		cache,
		notifier,
		noopExecutor{},
	)
	ack, err := svc.HandleAction(context.Background(), 1001, toolprotocol.ActionRequest{
		SessionID:      "sess-1",
		TargetAgentID:  9001,
		ToolbarID:      "agent-toolbar:test:v1",
		ItemID:         "gateway_relay_toggle",
		ActionID:       "gateway_relay_toggle:enable:3",
		ClientActionID: "c1",
		Event:          "click",
	})
	if err != nil {
		t.Fatalf("HandleAction() err = %v", err)
	}
	if ack.Accepted || ack.Code != "relay_state_stale" {
		t.Fatalf("stale click should be rejected as relay_state_stale, got %+v", ack)
	}
	if notifier.calls == 0 {
		t.Fatal("stale click must force-push the current snapshot")
	}
}

// 版本化 ActionID 与当前快照匹配时动作被 core 拦截、不派发给 agent 包
// （测试进程里 relay_state flag 默认关，service 返回"功能未开放"，正好验证走到了 service）。
func TestHandleGatewayRelayToggle_InterceptedBeforePackage(t *testing.T) {
	cache := &testCache{reserveOK: true, snapshot: toolprotocol.Snapshot{Revision: 2}}
	pkg := &countingPackage{testPackage: testPackage{snapshot: toolprotocol.Snapshot{
		Visible: true,
		Items:   []toolprotocol.Item{{ItemID: "x"}},
	}}}
	svc := NewService(
		testResolver{buildInput: relayInput(&RelayInfo{BalancePositive: true, Revision: 5})},
		testRegistry{pkg: pkg},
		cache,
		noopNotifier{},
		noopExecutor{},
	)
	ack, err := svc.HandleAction(context.Background(), 1001, toolprotocol.ActionRequest{
		SessionID:      "sess-1",
		TargetAgentID:  9001,
		ToolbarID:      "agent-toolbar:test:v1",
		ItemID:         "gateway_relay_toggle",
		ActionID:       "gateway_relay_toggle:enable:5",
		ClientActionID: "c2",
		Event:          "click",
	})
	if err != nil {
		t.Fatalf("HandleAction() err = %v", err)
	}
	if pkg.handled != 0 {
		t.Fatal("relay toggle must be intercepted by core, not dispatched to the agent package")
	}
	if ack.Accepted {
		t.Fatalf("flag-off environment should reject the write, got %+v", ack)
	}
}

func TestParseGatewayRelayToggleActionID(t *testing.T) {
	target, rev, ok := parseGatewayRelayToggleActionID("gateway_relay_toggle:enable:42")
	if !ok || target != "enable" || rev != 42 {
		t.Fatalf("valid parse = %q,%d,%v", target, rev, ok)
	}
	for _, bad := range []string{"", "gateway_relay_toggle", "gateway_relay_toggle:", "gateway_relay_toggle:up:1", "gateway_relay_toggle:enable:-1", "gateway_relay_toggle:enable:x", "gateway_relay_toggle:enable:1:2"} {
		if _, _, ok := parseGatewayRelayToggleActionID(bad); ok {
			t.Fatalf("should reject %q", bad)
		}
	}
}
