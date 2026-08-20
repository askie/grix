package protocol

import "strings"

const (
	AgentAPIProtocolVersion              = "aibot-agent-api-v1"
	AgentAPIContractVersion              = 1
	AgentAPISessionSendQuoteCapability   = "session_send_quote_v1"
	HermesLocalActionResultAckCapability = "local_action_result_ack"
)

var (
	hermesPacketFields = []string{"cmd", "seq", "payload"}

	hermesRequiredAuthFields = []string{
		"agent_id",
		"api_key",
		"protocol_version",
		"contract_version",
	}

	hermesRequiredCapabilities = []string{"local_action_v1"}
	hermesStableCapabilities   = []string{"stream_chunk", "session_route", "thread_v1", "inbound_media_v1", "local_action_v1", "local_action_result_ack", "audit_replay_v2"}

	hermesPublicClientCommands = []string{
		CmdAuth,
		CmdPing,
		CmdPong,
		CmdEventAck,
		CmdEventResult,
		CmdEventStopAck,
		CmdEventStopResult,
		CmdEventState,
		CmdAuditState,
		CmdEventCancelResult,
		CmdQueueClearResult,
		CmdQueueReorderResult,
		CmdEventHoldResult,
		CmdQueueEditResult,
		CmdQueueSnapshot,
		CmdSendMsg,
		CmdClientStreamChunk,
		CmdEditMsg,
		CmdUpdateBindingCard,
		CmdSessionActivitySet,
		CmdLocalActionResult,
		CmdSessionRouteBind,
		CmdSessionRouteResolve,
		CmdError,
	}

	hermesPostAuthClientCommands = []string{
		CmdPing,
		CmdPong,
		CmdEventAck,
		CmdEventResult,
		CmdEventStopAck,
		CmdEventStopResult,
		CmdEventState,
		CmdAuditState,
		CmdEventCancelResult,
		CmdQueueClearResult,
		CmdQueueReorderResult,
		CmdEventHoldResult,
		CmdQueueEditResult,
		CmdQueueSnapshot,
		CmdSendMsg,
		CmdClientStreamChunk,
		CmdEditMsg,
		CmdUpdateBindingCard,
		CmdSessionActivitySet,
		CmdLocalActionResult,
		CmdSessionRouteBind,
		CmdSessionRouteResolve,
		CmdAgentInvoke,
		CmdAgentSkillsUpdate,
		CmdError,
	}

	hermesPublicServerCommands = []string{
		CmdAuthAck,
		CmdPing,
		CmdPong,
		CmdEventMsg,
		CmdEventStop,
		CmdEventCancel,
		CmdQueueClear,
		CmdQueueReorder,
		CmdEventHold,
		CmdQueueEdit,
		CmdQueueSnapshotQuery,
		CmdEventEdit,
		CmdEventRevoke,
		CmdSendAck,
		CmdSendNack,
		CmdLocalAction,
		CmdLocalActionAck,
		CmdAgentProfilePush,
		CmdError,
	}

	hermesSupportedLocalActions = []string{
		"exec_approve",
		"exec_reject",
		"file_list",
		"set_model",
		"get_session_usage",
		"get_rate_limits",
		"configure_gateway_provider",
		"skill_upload",
		"skill_enable",
		"skill_disable",
		"audit_get_manifest",
		"audit_list_spans",
		"audit_get_content_chunk",
	}

	// canceled：主动停止/取消/撤回的事件以 canceled 收口（插件 1.7.x 起已按
	// connector 语义上报，此处契约文档同步补齐）。
	hermesEventResultStatuses       = []string{AgentEventResultResponded, AgentEventResultFailed, AgentEventResultCanceled}
	hermesEventStopResultStatuses   = []string{"stopped", "already_finished", "failed"}
	hermesLocalActionResultStatuses = []string{"ok", "failed", "unsupported"}

	hermesErrorCodes = []string{
		"invalid_local_action",
		"unsupported_local_action",
		"missing_approval_id",
		"unsupported_decision",
		"approval_not_found",
		"stop_handler_failed",
	}

	hermesForbiddenPublicFields = []string{
		"chatid",
		"req_id",
		"markdown",
		"stream",
		"media_id",
		"upload_id",
	}

	hermesRecommendedPublicFields = []string{
		"agent_id",
		"session_id",
		"event_id",
		"msg_id",
		"thread_id",
		"route_session_key",
		"content",
		"quoted_message_id",
		"attachments",
		"mention_user_ids",
		"status",
		"code",
		"msg",
		"error_code",
		"error_msg",
	}

	hermesMinimalPluginSurface = []string{
		CmdAuth,
		CmdPing,
		CmdPong,
		CmdEventMsg,
		CmdEventAck,
		CmdEventResult,
		CmdEventStop,
		CmdEventStopAck,
		CmdEventStopResult,
		CmdSendMsg,
		CmdClientStreamChunk,
		CmdSendAck,
		CmdSendNack,
		CmdEditMsg,
		CmdUpdateBindingCard,
		CmdLocalAction,
		CmdLocalActionResult,
		CmdLocalActionAck,
		CmdSessionRouteBind,
		CmdSessionRouteResolve,
	}

	hermesPostAuthClientCommandSet   = buildStringSet(hermesPostAuthClientCommands)
	hermesEventResultStatusSet       = buildStringSet(hermesEventResultStatuses)
	hermesLocalActionResultStatusSet = buildStringSet(hermesLocalActionResultStatuses)
)

func HermesRequiredCapabilities() []string {
	return cloneStringSlice(hermesRequiredCapabilities)
}

func HermesStableCapabilities() []string {
	return cloneStringSlice(hermesStableCapabilities)
}

func HermesAllowsPostAuthClientCommand(cmd string) bool {
	_, ok := hermesPostAuthClientCommandSet[strings.TrimSpace(cmd)]
	return ok
}

func HermesAllowsEventResultStatus(status string) bool {
	_, ok := hermesEventResultStatusSet[strings.TrimSpace(status)]
	return ok
}

func HermesAllowsLocalActionResultStatus(status string) bool {
	_, ok := hermesLocalActionResultStatusSet[strings.TrimSpace(status)]
	return ok
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string(nil), values...)
}

func buildStringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}
