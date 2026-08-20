package protocol

import "testing"

// TestHermesPublicCommandsAreStable 保证 Hermes 公共面（对外 Agent 接入命令）
// 全部都被 CmdStability 表标记为 stable, 防止接入方在不知情的情况下依赖到非稳定 cmd。
func TestHermesPublicCommandsAreStable(t *testing.T) {
	check := func(label string, cmds []string) {
		for _, cmd := range cmds {
			s := LookupStability(cmd)
			if s != StabilityStable {
				t.Errorf("hermes %s contains non-stable cmd=%s stability=%s", label, cmd, s)
			}
		}
	}
	check("PublicClientCommands", hermesPublicClientCommands)
	check("PostAuthClientCommands", hermesPostAuthClientCommands)
	check("PublicServerCommands", hermesPublicServerCommands)
	check("MinimalPluginSurface", hermesMinimalPluginSurface)
}

// TestStabilityTableContainsAllRegisteredCmds 防止协议增加新 cmd 但忘记在
// CmdStability 中登记。这是 Phase 4.1 的守护机制：协议改动必须配合稳定性分级。
func TestStabilityTableContainsAllRegisteredCmds(t *testing.T) {
	allCmds := []string{
		CmdAuth, CmdAuthAck, CmdReAuth, CmdReAuthAck,
		CmdPing, CmdPong, CmdError, CmdSendNack, CmdKicked,
		CmdEventMsg, CmdEventEdit, CmdEventRevoke,
		CmdEventAck, CmdEventResult,
		CmdEventStop, CmdEventStopAck, CmdEventStopResult,
		CmdEventCancel, CmdEventCancelResult,
		CmdEventState, CmdQueueClear, CmdQueueClearResult, CmdQueueSnapshot,
		CmdSendMsg, CmdEditMsg, CmdUpdateBindingCard, CmdSendAck, CmdRetryMsg, CmdRetryMsgAck,
		CmdPushMsg, CmdPushEdit, CmdPushAck,
		CmdAppStateSet,
		CmdPullSync, CmdPullSyncResp,
		CmdSessionRead, CmdSessionReadAck, CmdSessionReadSync, CmdUnreadSync,
		CmdSessionHistoryReset, CmdSessionHistoryResetAck, CmdSessionHistoryResetSync,
		CmdSessionHistoryResetsQuery, CmdSessionHistoryResetsQueryAck,
		CmdSessionMemberChanged, CmdSessionAccessRevoked,
		CmdFriendSync, CmdFriendSyncResp,
		CmdSessionRouteBind, CmdSessionRouteResolve,
		CmdSessionActivitySet, CmdSessionActivitySync,
		CmdSessionActivityList, CmdSessionActivityListResp,
		CmdStreamChunk, CmdStreamFinish, CmdStreamStop, CmdStreamError,
		CmdOverrideStream, CmdAgentStateSync,
		CmdClientStreamChunk,
		CmdDelegateStart, CmdDelegateStop, CmdDelegateAck,
		CmdDelegateList, CmdDelegateListResp,
		CmdAgentDeliveryError, CmdAgentDeliveryStatus,
		CmdAgentOutputGet, CmdAgentOutputGetResp,
		CmdAgentOutputStop, CmdAgentOutputStopAck, CmdAgentOutputStatus,
		CmdAgentToolbarGet, CmdAgentToolbarGetResp, CmdAgentToolbarSync,
		CmdAgentToolbarAction, CmdAgentToolbarActionAck,
		CmdConversationAuditSet, CmdConversationAuditSetResp,
		CmdRelayLocalStreamStart, CmdRelayLocalStreamStartAck,
		CmdRelayLocalStreamChunk, CmdRelayLocalStreamFinish,
		CmdAgentInvoke, CmdAgentInvokeResult,
		CmdAgentSkillsUpdate,
		CmdLocalAction, CmdLocalActionResult, CmdLocalActionAck,
		CmdAgentProfilePush,
		CmdSkillSync,
		CmdCodexEvent, CmdPiEvent,
		CmdAgentFileList, CmdAgentFileListResp,
		CmdAgentCreateFolder, CmdAgentCreateFolderResp,
		CmdWidgetSessionClosed,
	}
	for _, cmd := range allCmds {
		if _, ok := CmdStability[cmd]; !ok {
			t.Errorf("cmd=%s missing from CmdStability table", cmd)
		}
	}
}

// TestDeprecatedCmdsAreNotInHermesPublic 验证已废弃 cmd（agent_delivery_error / unread_sync）
// 不再出现在 Hermes 任何公共命令面里。新接入 Agent 不应感知到它们。
func TestDeprecatedCmdsAreNotInHermesPublic(t *testing.T) {
	contains := func(haystack []string, needle string) bool {
		for _, v := range haystack {
			if v == needle {
				return true
			}
		}
		return false
	}
	deprecated := []string{CmdAgentDeliveryError, CmdUnreadSync}
	for _, cmd := range deprecated {
		if contains(hermesPublicClientCommands, cmd) ||
			contains(hermesPostAuthClientCommands, cmd) ||
			contains(hermesPublicServerCommands, cmd) ||
			contains(hermesMinimalPluginSurface, cmd) {
			t.Errorf("deprecated cmd=%s should not appear in any hermes public surface", cmd)
		}
		if LookupStability(cmd) != StabilityDeprecated {
			t.Errorf("deprecated cmd=%s should be marked StabilityDeprecated", cmd)
		}
	}
}
