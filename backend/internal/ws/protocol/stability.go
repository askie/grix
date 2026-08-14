package protocol

// Phase 4.1 协议命令的稳定性分级。
// 接入方据此判断"哪些 cmd 是 stable 契约保护的"。
// 任何未在表中出现的 cmd 视为 internal,可能随时变更或移除。
type Stability string

const (
	// StabilityStable 稳定契约。变更需走 breaking change 流程,接入方可放心依赖。
	StabilityStable Stability = "stable"

	// StabilityBeta 功能完整但可能调整 payload 字段或语义。
	StabilityBeta Stability = "beta"

	// StabilityInternal 仅内部使用,随时可能变更或移除。
	StabilityInternal Stability = "internal"

	// StabilityDeprecated 已废弃,保留 1 个版本周期等待客户端迁移,
	// 不再有任何代码主动产生该 cmd（仅作为占位）。
	StabilityDeprecated Stability = "deprecated"
)

// CmdStability 列出每个 cmd 的稳定性等级。
// 此表是 Hermes profile 与对外接入文档的事实来源。
var CmdStability = map[string]Stability{
	// 认证与心跳
	CmdAuth:      StabilityStable,
	CmdAuthAck:   StabilityStable,
	CmdReAuth:    StabilityStable,
	CmdReAuthAck: StabilityStable,
	CmdPing:      StabilityStable,
	CmdPong:      StabilityStable,
	CmdKicked:    StabilityStable,

	// 错误与异常
	CmdError:    StabilityStable,
	CmdSendNack: StabilityStable,

	// 会话消息核心
	CmdSendMsg:           StabilityStable,
	CmdSendAck:           StabilityStable,
	CmdEditMsg:           StabilityStable,
	CmdUpdateBindingCard: StabilityStable,
	CmdRetryMsg:          StabilityStable,
	CmdRetryMsgAck:       StabilityStable,
	CmdPushMsg:           StabilityStable,
	CmdPushEdit:          StabilityStable,
	CmdPushAck:           StabilityStable,
	CmdEventMsg:          StabilityStable,
	CmdEventEdit:         StabilityStable,
	CmdEventRevoke:       StabilityStable,
	CmdEventAck:          StabilityStable,
	CmdEventResult:       StabilityStable,
	CmdEventStop:         StabilityStable,
	CmdEventStopAck:      StabilityStable,
	CmdEventStopResult:   StabilityStable,

	// 会话同步
	CmdPullSync:                     StabilityStable,
	CmdPullSyncResp:                 StabilityStable,
	CmdSessionRead:                  StabilityStable,
	CmdSessionReadAck:               StabilityStable,
	CmdSessionReadSync:              StabilityStable,
	CmdSessionHistoryReset:          StabilityStable,
	CmdSessionHistoryResetAck:       StabilityStable,
	CmdSessionHistoryResetSync:      StabilityStable,
	CmdSessionHistoryResetsQuery:    StabilityStable,
	CmdSessionHistoryResetsQueryAck: StabilityStable,
	CmdSessionMemberChanged:         StabilityStable,
	CmdSessionAccessRevoked:         StabilityStable,

	// 流式
	CmdStreamChunk:       StabilityStable,
	CmdStreamFinish:      StabilityStable,
	CmdStreamStop:        StabilityStable,
	CmdStreamError:       StabilityStable,
	CmdClientStreamChunk: StabilityStable,
	CmdOverrideStream:    StabilityStable,

	// Agent 投递与状态
	CmdAgentDeliveryStatus:      StabilityStable,
	CmdAgentDeliveryStatusBatch: StabilityStable,
	CmdAgentOutputGet:           StabilityStable,
	CmdAgentOutputGetResp:       StabilityStable,
	CmdAgentOutputStop:          StabilityStable,
	CmdAgentOutputStopAck:       StabilityStable,
	CmdAgentOutputStatus:        StabilityStable,
	CmdAgentStateSync:           StabilityStable,

	// Agent 工具栏
	CmdAgentToolbarGet:       StabilityStable,
	CmdAgentToolbarGetResp:   StabilityStable,
	CmdAgentToolbarSync:      StabilityStable,
	CmdAgentToolbarAction:    StabilityStable,
	CmdAgentToolbarActionAck: StabilityStable,

	// 对话审计开关（服务端持久化，Beta 稳定后提 Stable）
	CmdConversationAuditSet:     StabilityBeta,
	CmdConversationAuditSetResp: StabilityBeta,

	// Agent invoke / local action
	CmdAgentInvoke:       StabilityStable,
	CmdAgentInvokeResult: StabilityStable,
	CmdAgentSkillsUpdate: StabilityStable,
	CmdLocalAction:       StabilityStable,
	CmdLocalActionResult: StabilityStable,

	// Grix 中转凭证的 WS 请求-应答（connector 主动要 Key，服务端签发后随应答下发）
	CmdRelayCredentialRequest: StabilityBeta,
	CmdRelayCredentialResult:  StabilityBeta,

	// Grix 中转开关服务端化（migration 111）的 WS 对齐协议：connector 上线同步
	// desired、事件驱动回执上报 actual（docs/frontend/gateway_relay_mobile_design.md §2.4）
	CmdRelayStateSyncRequest: StabilityBeta,
	CmdRelayStateSyncResult:  StabilityBeta,
	CmdRelayStateReport:      StabilityBeta,

	// Agent 资料推送
	CmdAgentProfilePush: StabilityStable,

	// 技能库变更推送（docs/architecture/38 §6.2）
	CmdSkillSync: StabilityBeta,

	// 委托
	CmdDelegateStart:    StabilityStable,
	CmdDelegateStop:     StabilityStable,
	CmdDelegateAck:      StabilityStable,
	CmdDelegateList:     StabilityStable,
	CmdDelegateListResp: StabilityStable,

	// 应用状态
	CmdAppStateSet:             StabilityStable,
	CmdSessionRouteBind:        StabilityStable,
	CmdSessionRouteResolve:     StabilityStable,
	CmdSessionActivitySet:      StabilityStable,
	CmdSessionActivitySync:     StabilityStable,
	CmdSessionActivityList:     StabilityStable,
	CmdSessionActivityListResp: StabilityStable,

	// 好友同步
	CmdFriendSync:     StabilityStable,
	CmdFriendSyncResp: StabilityStable,

	// 队列控制
	CmdEventCancel:        StabilityStable,
	CmdQueueClear:         StabilityStable,
	CmdQueueReorder:       StabilityStable,
	CmdEventHold:          StabilityStable,
	CmdQueueEdit:          StabilityStable,
	CmdEventCancelResult:  StabilityStable,
	CmdQueueClearResult:   StabilityStable,
	CmdQueueReorderResult: StabilityStable,
	CmdEventHoldResult:    StabilityStable,
	CmdQueueEditResult:    StabilityStable,
	CmdQueueSnapshot:      StabilityStable,
	CmdQueueSnapshotQuery: StabilityStable,
	CmdEventState:         StabilityStable,
	CmdAuditState:         StabilityStable,
	CmdAuditStateAck:      StabilityStable,

	// 本地推理桥接
	CmdRelayLocalStreamStart:    StabilityBeta,
	CmdRelayLocalStreamStartAck: StabilityBeta,
	CmdRelayLocalStreamChunk:    StabilityBeta,
	CmdRelayLocalStreamFinish:   StabilityBeta,

	// 文件操作（Agent）
	CmdAgentFileList:         StabilityBeta,
	CmdAgentFileListResp:     StabilityBeta,
	CmdAgentCreateFolder:     StabilityBeta,
	CmdAgentCreateFolderResp: StabilityBeta,

	// 工具栏一键上传技能（docs/architecture/39）
	CmdAgentSkillUpload:     StabilityBeta,
	CmdAgentSkillUploadResp: StabilityBeta,

	// 技能弹窗下拉刷新（重扫本地技能并回执最新快照）
	CmdAgentSkillRefresh:     StabilityBeta,
	CmdAgentSkillRefreshResp: StabilityBeta,

	// Widget
	CmdWidgetSessionClosed: StabilityBeta,

	// 已废弃
	CmdAgentDeliveryError: StabilityDeprecated, // 由 agent_delivery_status(failed) 替代
	CmdUnreadSync:         StabilityDeprecated, // 合并入 session_read_sync.unread_count

	// Agent 私有事件（Internal,新接入 Agent 不应直接使用）
	CmdCodexEvent: StabilityInternal,
	CmdPiEvent:    StabilityInternal,
}

// LookupStability 按 cmd 名称返回稳定性等级,未注册的 cmd 视为 Internal。
func LookupStability(cmd string) Stability {
	if v, ok := CmdStability[cmd]; ok {
		return v
	}
	return StabilityInternal
}
