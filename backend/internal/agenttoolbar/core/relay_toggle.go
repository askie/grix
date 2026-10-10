package core

// "Grix中转"两态开关按钮：core 统一注入与动作拦截（平台管理能力，不散到各 agent 包）。
//
// 防并发取反的关键设计：ActionID 版本化（gateway_relay_toggle:<enable|disable>:<relay
// revision>）。core 的动作校验要求请求 ActionID 与"当前最新快照"的 item 完全相等，
// 旧快照的点击在 matchesToolbarItemAction 处即被拒绝并强推新快照——两台设备都看到
// "官方"并点击时，后到的请求绝不会对最新状态反向取反。

import (
	"context"
	"strconv"
	"strings"

	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	"github.com/askie/grix/backend/internal/api/service"
)

const (
	gatewayRelayToggleItemID = "gateway_relay_toggle"
	// gatewayRelayToggleActionPrefix 是版本化动作前缀，后接 "<enable|disable>:<revision>"。
	gatewayRelayToggleActionPrefix = "gateway_relay_toggle:"
	// gatewayRelayToggleRefreshReason 既是刷新 reason（在 forceSyncOnReason 名单内），
	// 也与 service.GatewaySetAgentRelay 写成功后的刷新钩子 reason 一致。
	gatewayRelayToggleRefreshReason = "gateway_relay_toggle"
)

// appendGatewayRelayToggle 注入"中转开关"按钮项。Selected 由 desired（Enabled）决定；
// 余额不足且未开启时不给入口，已开启则始终保留关闭入口——余额耗尽不能困住用户。
// 仅在快照可见时注入：未绑定会话等 Visible=false 的场景不出现（与平台其它按钮一致）。
func appendGatewayRelayToggle(snapshot toolprotocol.Snapshot, in BuildInput) toolprotocol.Snapshot {
	relay := in.Relay
	if relay == nil || !snapshot.Visible {
		return snapshot
	}
	if !relay.BalancePositive && !relay.Enabled {
		return snapshot
	}

	target := "enable"
	if relay.Enabled {
		target = "disable"
	}
	item := toolprotocol.Item{
		ItemID:   gatewayRelayToggleItemID,
		GroupID:  "gateway_relay",
		Kind:     toolprotocol.ItemKindButton,
		ActionID: gatewayRelayToggleActionPrefix + target + ":" + strconv.FormatInt(relay.Revision, 10),
		Icon:     "swap",
		Selected: relay.Enabled,
		// BadgeText 始终带解析后的模型：开启时展示当前所用，关闭时预告开启将用。
		BadgeText: relay.RelayModel,
		Tooltip:   relayToggleTooltip(relay),
	}
	if relay.Enabled {
		item.Label = "中转"
		item.Variant = "primary"
	} else {
		item.Label = "官方"
		item.Variant = "neutral"
	}
	snapshot.Items = append([]toolprotocol.Item{item}, snapshot.Items...)
	return snapshot
}

// relayToggleTooltip 表达"写成功 ≠ 已生效"：desired 变了但 connector 未回执时必须
// 如实显示待生效/未知，避免"官方"按钮误导用户以为流量已恢复。
func relayToggleTooltip(relay *RelayInfo) string {
	if !relay.Enabled {
		return "官方模式，点击切换为中转模式"
	}
	switch {
	case !relay.StateKnown:
		return "中转实际状态未知（设备离线或连接器过旧），点击切回官方模式"
	case !relay.Applied:
		return "中转模式待生效，点击切回官方模式"
	default:
		return "中转模式已生效，点击切回官方模式"
	}
}

// handleGatewayRelayToggle 处理按钮点击。能走到这里说明 ActionID 已与最新快照
// 完全匹配（目标态与 revision 都是当前的），revision 再校验一次仅作防御。
func (s *Service) handleGatewayRelayToggle(
	ctx context.Context,
	ownerID int64,
	req toolprotocol.ActionRequest,
	snapshot toolprotocol.Snapshot,
	buildInput BuildInput,
) toolprotocol.ActionResult {
	relay := buildInput.Relay
	if relay == nil {
		return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeRejected, Code: "relay_unavailable", Message: "中转开关不可用"}
	}
	target, revision, ok := parseGatewayRelayToggleActionID(req.ActionID)
	if !ok {
		return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeRejected, Code: "invalid_action", Message: "工具栏动作无效"}
	}
	if revision != relay.Revision {
		s.forcePushRelaySnapshot(ctx, ownerID, req.SessionID, snapshot.AgentID)
		return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeRejected, Code: "relay_state_stale", Message: "中转开关状态已变化，请按最新状态重试"}
	}

	enabled := target == "enable"
	// 注意：版本化 ActionID 恒指向当前态的反态（enable 只在当前为关时出现在快照里），
	// 所以"目标态已是现状"在动作匹配阶段就不可能发生，无需幂等分支。

	// 开启带解析后的模型（state 已选 > 活跃Key > 钱包兜底），关闭传空——
	// service 层负责关闭时保留已选模型、开启时的最终校验与下发。
	relayModel := ""
	if enabled {
		relayModel = relay.RelayModel
	}
	// 写入传真实请求用户：GatewaySetAgentRelay 内部做归属复核，非主人在这里被拒。
	if _, ec := service.GatewaySetAgentRelay(ctx, ownerID, buildInput.Agent.AgentID, enabled, relayModel, &revision); ec != nil {
		// 409 等失败同样强推最新快照，让前端按现状重试。
		s.forcePushRelaySnapshot(ctx, ownerID, req.SessionID, snapshot.AgentID)
		return toolprotocol.ActionResult{Outcome: toolprotocol.ActionOutcomeRejected, Code: strconv.Itoa(ec.BizCode), Message: ec.Msg}
	}
	// 成功后的快照刷新由 GatewaySetAgentRelay 的 refreshAgentToolbar 钩子统一触发
	// （设置页 REST 操作也走它），这里不重复刷。
	return toolprotocol.ActionResult{
		Outcome: toolprotocol.ActionOutcomeAcceptedNoStateChange,
		Code:    "accepted",
		Message: relayToggleDoneMessage(enabled),
	}
}

func relayToggleDoneMessage(enabled bool) string {
	if enabled {
		return "已切换为中转模式"
	}
	return "已切换为官方模式"
}

// forcePushRelaySnapshot 强制把当前快照推给客户端（无论内容是否变化）：
// 陈旧点击被拒绝后，客户端必须立刻拿到带新 revision 的按钮。
func (s *Service) forcePushRelaySnapshot(ctx context.Context, ownerID int64, sessionID string, agentID int64) {
	_ = s.RefreshSessionForAgent(ctx, ownerID, sessionID, agentID, gatewayRelayToggleRefreshReason)
}

func parseGatewayRelayToggleActionID(actionID string) (target string, revision int64, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(actionID), gatewayRelayToggleActionPrefix)
	if !found {
		return "", 0, false
	}
	parts := strings.Split(rest, ":")
	if len(parts) != 2 || (parts[0] != "enable" && parts[0] != "disable") {
		return "", 0, false
	}
	revision, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || revision < 0 {
		return "", 0, false
	}
	return parts[0], revision, true
}
