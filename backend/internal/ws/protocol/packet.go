package protocol

import "encoding/json"

// Packet is the standard WebSocket JSON packet format.
type Packet struct {
	Cmd     string          `json:"cmd"`
	Seq     int64           `json:"seq"`
	Payload json.RawMessage `json:"payload"`
}

// Command constants
const (
	CmdAuth                         = "auth"
	CmdAuthAck                      = "auth_ack"
	CmdPing                         = "ping"
	CmdPong                         = "pong"
	CmdError                        = "error"
	CmdEventMsg                     = "event_msg"
	CmdEventRevoke                  = "event_revoke"
	CmdSendMsg                      = "send_msg"
	CmdEditMsg                      = "edit_msg"
	CmdUpdateBindingCard            = "update_binding_card"
	CmdSendAck                      = "send_ack"
	CmdSendNack                     = "send_nack"
	CmdRetryMsg                     = "retry_msg"
	CmdRetryMsgAck                  = "retry_msg_ack"
	CmdEventAck                     = "event_ack"
	CmdPushMsg                      = "push_msg"
	CmdPushEdit                     = "push_edit"
	CmdPushAck                      = "push_ack"
	CmdAppStateSet                  = "app_state_set"
	CmdPullSync                     = "pull_sync"
	CmdPullSyncResp                 = "pull_sync_resp"
	CmdSessionRead                  = "session_read"
	CmdSessionReadAck               = "session_read_ack"
	CmdSessionReadSync              = "session_read_sync"
	CmdUnreadSync                   = "unread_sync"
	CmdSessionHistoryReset          = "session_history_reset"
	CmdSessionHistoryResetAck       = "session_history_reset_ack"
	CmdSessionHistoryResetSync      = "session_history_reset_sync"
	CmdSessionHistoryResetsQuery    = "session_history_resets_query"
	CmdSessionHistoryResetsQueryAck = "session_history_resets_query_ack"
	CmdSessionMemberChanged         = "session_member_changed"
	CmdSessionAccessRevoked         = "session_access_revoked"
	CmdFriendSync                   = "friend_sync"
	CmdFriendSyncResp               = "friend_sync_resp"
	CmdEventResult                  = "event_result"
	CmdSessionRouteBind             = "session_route_bind"
	CmdSessionRouteResolve          = "session_route_resolve"
	CmdSessionActivitySet           = "session_activity_set"
	CmdSessionActivitySync          = "session_activity_sync"
	CmdSessionActivityList          = "session_activity_list"
	CmdSessionActivityListResp      = "session_activity_list_resp"
	CmdStreamChunk                  = "stream_chunk"
	CmdStreamFinish                 = "stream_finish"
	CmdStreamStop                   = "stream_stop"
	CmdStreamError                  = "stream_error"
	CmdOverrideStream               = "override_stream"
	CmdAgentStateSync               = "agent_state_sync"
	CmdReAuth                       = "re_auth"
	CmdReAuthAck                    = "re_auth_ack"
	CmdKicked                       = "kicked"
	CmdClientStreamChunk            = "client_stream_chunk"
	CmdDelegateStart                = "delegate_start"
	CmdDelegateStop                 = "delegate_stop"
	CmdDelegateAck                  = "delegate_ack"
	CmdDelegateList                 = "delegate_list"
	CmdDelegateListResp             = "delegate_list_resp"
	CmdAgentDeliveryError           = "agent_delivery_error"
	CmdAgentDeliveryStatus          = "agent_delivery_status"
	CmdAgentDeliveryStatusBatch     = "agent_delivery_status_batch"
	CmdAgentOutputGet               = "agent_output_get"
	CmdAgentOutputGetResp           = "agent_output_get_resp"
	CmdAgentOutputStop              = "agent_output_stop"
	CmdAgentOutputStopAck           = "agent_output_stop_ack"
	CmdAgentOutputStatus            = "agent_output_status"
	CmdAgentToolbarGet              = "agent_toolbar_get"
	CmdAgentToolbarGetResp          = "agent_toolbar_get_resp"
	CmdAgentToolbarSync             = "agent_toolbar_sync"
	CmdAgentToolbarAction           = "agent_toolbar_action"
	CmdAgentToolbarActionAck        = "agent_toolbar_action_ack"
	CmdConversationAuditSet         = "conversation_audit_set"
	CmdConversationAuditSetResp     = "conversation_audit_set_resp"
	CmdEventCancel                  = "event_cancel"
	CmdQueueClear                   = "queue_clear"
	CmdQueueReorder                 = "queue_reorder"
	CmdEventHold                    = "event_hold"
	CmdQueueEdit                    = "queue_edit"
	CmdRelayLocalStreamStart        = "relay_local_stream_start"
	CmdRelayLocalStreamStartAck     = "relay_local_stream_start_ack"
	CmdRelayLocalStreamChunk        = "relay_local_stream_chunk"
	CmdRelayLocalStreamFinish       = "relay_local_stream_finish"
	CmdEventEdit                    = "event_edit"
	CmdEventStop                    = "event_stop"
	CmdEventStopAck                 = "event_stop_ack"
	CmdEventStopResult              = "event_stop_result"
	CmdEventState                   = "event_state"
	CmdEventCancelResult            = "event_cancel_result"
	CmdQueueClearResult             = "queue_clear_result"
	CmdQueueReorderResult           = "queue_reorder_result"
	CmdEventHoldResult              = "event_hold_result"
	CmdQueueEditResult              = "queue_edit_result"
	CmdQueueSnapshot                = "queue_snapshot"
	CmdQueueSnapshotQuery           = "queue_snapshot_query"
	CmdAgentInvoke                  = "agent_invoke"
	CmdAgentInvokeResult            = "agent_invoke_result"
	CmdLocalAction                  = "local_action"
	CmdLocalActionResult            = "local_action_result"
	CmdAuditState                   = "audit_state"
	CmdAuditStateAck                = "audit_state_ack"
	CmdAuditGetManifest             = "audit_get_manifest"
	CmdAuditGetManifestResp         = "audit_get_manifest_resp"
	CmdAuditListSpans               = "audit_list_spans"
	CmdAuditListSpansResp           = "audit_list_spans_resp"
	CmdAuditGetContentChunk         = "audit_get_content_chunk"
	CmdAuditGetContentChunkResp     = "audit_get_content_chunk_resp"
	CmdCodexEvent                   = "codex_event"
	CmdPiEvent                      = "pi_event"
	CmdAgentFileList                = "agent_file_list"
	CmdAgentFileListResp            = "agent_file_list_resp"
	CmdAgentSessionBindingsList     = "agent_session_bindings_list"
	CmdAgentSessionBindingsListResp = "agent_session_bindings_list_resp"
	CmdAgentSessionBind             = "agent_session_bind"
	CmdAgentSessionBindResp         = "agent_session_bind_resp"
	CmdAgentCreateFolder            = "agent_create_folder"
	CmdAgentCreateFolderResp        = "agent_create_folder_resp"
	CmdAgentSkillsUpdate            = "agent_skills_update"
	CmdAgentSkillUpload             = "agent_skill_upload"
	CmdAgentSkillUploadResp         = "agent_skill_upload_resp"
	CmdAgentSkillDelete             = "agent_skill_delete"
	CmdAgentSkillDeleteResp         = "agent_skill_delete_resp"
	CmdAgentSkillEnable             = "agent_skill_enable"
	CmdAgentSkillEnableResp         = "agent_skill_enable_resp"
	CmdAgentSkillDisable            = "agent_skill_disable"
	CmdAgentSkillDisableResp        = "agent_skill_disable_resp"
	CmdAgentSkillRefresh            = "agent_skill_refresh"
	CmdAgentSkillRefreshResp        = "agent_skill_refresh_resp"
	CmdRelayCredentialRequest       = "relay_credential_request"
	CmdRelayCredentialResult        = "relay_credential_result"
	CmdRelayStateSyncRequest        = "relay_state_sync_request"
	CmdRelayStateSyncResult         = "relay_state_sync_result"
	CmdRelayStateReport             = "relay_state_report"
	CmdWidgetSessionClosed          = "widget_session_closed"
)

const (
	SessionActivityKindComposing = "composing"
	SessionActivityKindViewing   = "viewing"

	SessionActivityActorTypeHuman = "human"
	SessionActivityActorTypeAgent = "agent"

	SessionActivitySourceHumanInput  = "human_input"
	SessionActivitySourceLLMDirect   = "llm_direct"
	SessionActivitySourceLLMDelegate = "llm_delegate"
	SessionActivitySourceLocalAgent  = "local_agent"
	SessionActivitySourceAgentAPI    = "agent_api"

	AgentDeliveryScopeDelegate = "delegate"
	AgentDeliveryScopeDirect   = "direct"

	// AgentEventStopScopeSession identifies a composing-only stop that has no
	// tracked event ID. Its ACK/result report command handling only and must not
	// be interpreted as terminal evidence for an event run.
	AgentEventStopScopeSession = "session"

	AgentDeliveryCodeChannelUnavailable = "agent_api_channel_unavailable"
	AgentDeliveryCodeAckTimeout         = "agent_api_event_ack_timeout"
	AgentDeliveryCodeResultTimeout      = "agent_api_event_result_timeout"
	AgentDeliveryCodeProcessingFailed   = "agent_api_event_processing_failed"
	AgentDeliveryCodeCanceled           = "agent_api_event_canceled"
	AgentDeliveryCodeAgentStopFailure   = "agent_stop_failure"
	// AgentDeliveryCodeEventStale is reported by grix-connector when it drops a
	// stale queued event on reconnect — a literal contract with the connector.
	AgentDeliveryCodeEventStale = "event_stale"

	AgentDeliveryStatusQueued    = "queued"
	AgentDeliveryStatusReceived  = "received"
	AgentDeliveryStatusResponded = "responded"
	AgentDeliveryStatusCanceled  = "canceled"
	AgentDeliveryStatusTimeout   = "timeout"
	AgentDeliveryStatusFailed    = "failed"

	AgentOutputStateQueued    = "queued"
	AgentOutputStateReceived  = "received"
	AgentOutputStateStreaming = "streaming"
	AgentOutputStateStopping  = "stopping"
	AgentOutputStateStopped   = "stopped"
	AgentOutputStateCompleted = "completed"
	AgentOutputStateFailed    = "failed"

	AgentEventResultResponded = "responded"
	AgentEventResultFailed    = "failed"
	AgentEventResultCanceled  = "canceled"

	AgentStateOnline  = "online"
	AgentStateOffline = "offline"
)

// Auth payloads
type AuthPayload struct {
	Token    string `json:"token"`
	DeviceID string `json:"device_id"`
	Platform string `json:"platform"`
}

// Phase 4.2: AckPolicyPayload 定义服务端期望客户端遵守的 ACK 行为。
// 在 auth_ack 中下发,客户端可据此动态调整,例如在弱网时主动降级到 pull_sync。
type AckPolicyPayload struct {
	PushAckTimeoutMs int    `json:"push_ack_timeout_ms,omitempty"`
	MaxRetries       int    `json:"max_retries,omitempty"`
	TimeoutAction    string `json:"timeout_action,omitempty"` // "disconnect" | "degrade"
}

// auth_ack / re_auth_ack 的 code 契约。
//
// 客户端据此决定「清掉本地会话回登录页」还是「保留会话继续重连」，绝不能靠 msg
// 文案去猜——同一句文案既可能是真的凭证终态，也可能只是存储层抖动被兜底成了它。
// 服务端有义务把这两类分清楚：把自己的故障报成凭证失效，会让所有在线客户端在一次
// 数据库抖动里被集体踢下线。
const (
	AuthCodeOK = 0
	// AuthCodeFatal 凭证或账号本身的问题（失效、被禁用、设备会话不匹配）。
	// 重连不可能自愈，客户端应清会话回登录页。
	AuthCodeFatal = 10001
	// ReAuthCodeFatal 同上，re_auth 通道的终态码。
	ReAuthCodeFatal = 10002
	// AuthCodeRetryable 服务端自己暂时不可用（存储层故障等），凭证没有问题。
	// 客户端应保留会话、继续退避重连，等服务端恢复后自愈。
	AuthCodeRetryable = 10003
)

type AuthAckPayload struct {
	Code           int               `json:"code"`
	UserID         int64             `json:"user_id,string,omitempty"`
	LatestInboxSeq int64             `json:"latest_inbox_seq,string,omitempty"`
	Msg            string            `json:"msg"`
	AckPolicy      *AckPolicyPayload `json:"ack_policy,omitempty"`
}

// Send message payload
type SendMsgPayload struct {
	SessionID       string          `json:"session_id"`
	ThreadID        string          `json:"thread_id,omitempty"`
	ClientMsgID     string          `json:"client_msg_id"`
	MsgType         int16           `json:"msg_type"`
	Content         string          `json:"content"`
	Extra           json.RawMessage `json:"extra,omitempty"`
	QuotedMessageID int64           `json:"quoted_message_id,string,omitempty"`
	VisibleTo       StringInt64s    `json:"visible_to,omitempty"` // 群聊消息仅指定人可见
}

type SendAckPayload struct {
	SessionID      string              `json:"session_id,omitempty"`
	MsgID          int64               `json:"msg_id,string,omitempty"`
	ClientMsgID    string              `json:"client_msg_id"`
	InboxSeq       int64               `json:"inbox_seq,string,omitempty"`
	CreatedAt      int64               `json:"created_at"`
	LocalInference *LocalInferenceHint `json:"local_inference,omitempty"`
}

type EditMsgPayload struct {
	SessionID string `json:"session_id"`
	MsgID     int64  `json:"msg_id,string"`
	Content   string `json:"content"`
}

// LocalInferenceHint tells the client to perform local LLM inference.
type ContextMessagePayload struct {
	MsgID           int64        `json:"msg_id,string"`
	SenderID        int64        `json:"sender_id,string"`
	SenderType      int16        `json:"sender_type"`
	MsgType         int16        `json:"msg_type"`
	Content         string       `json:"content"`
	QuotedMessageID int64        `json:"quoted_message_id,string,omitempty"`
	MentionUserIDs  StringInt64s `json:"mention_user_ids,omitempty"`
	CreatedAt       int64        `json:"created_at"`
}

type LocalInferenceHint struct {
	AgentID         int64                   `json:"agent_id,string"`
	SessionID       string                  `json:"session_id"`
	TriggerMsgID    int64                   `json:"trigger_msg_id,string"`
	Endpoint        string                  `json:"endpoint"`
	ModelName       string                  `json:"model_name"`
	SystemPrompt    string                  `json:"system_prompt,omitempty"`
	ContextMessages []ContextMessagePayload `json:"context_messages,omitempty"`
}

// RelayLocalStream payloads — three-phase streaming from local LLM devices.
type RelayLocalStreamStartPayload struct {
	SessionID    string `json:"session_id"`
	AgentID      int64  `json:"agent_id,string"`
	TriggerMsgID int64  `json:"trigger_msg_id,string"`
}

type RelayLocalStreamStartAckPayload struct {
	Code         int    `json:"code"`
	SessionID    string `json:"session_id,omitempty"`
	AgentID      int64  `json:"agent_id,string,omitempty"`
	TriggerMsgID int64  `json:"trigger_msg_id,string,omitempty"`
	MsgID        int64  `json:"msg_id,string"`
	Msg          string `json:"msg,omitempty"`
}

type RelayLocalStreamChunkPayload struct {
	SessionID    string `json:"session_id"`
	MsgID        int64  `json:"msg_id,string"`
	DeltaContent string `json:"delta_content"`
	// chunk_seq 是连续小整数,不存在 JS 精度问题,保留 number 形式以减少 Agent 接入负担。
	ChunkSeq int64 `json:"chunk_seq"`
}

type RelayLocalStreamFinishPayload struct {
	SessionID    string `json:"session_id"`
	MsgID        int64  `json:"msg_id,string"`
	FinalContent string `json:"final_content"`
}

// Phase 3.4: ErrorPayload 是异步/主动错误（cmd="error"）的标准 payload。
// 与 send_nack（请求-响应错误）不同, error 用于服务端在没有特定上下文 ack 路径时,
// 主动反馈协议层面的错误。RefCmd/RefID 用于关联原始命令。
type ErrorPayload struct {
	Code   int    `json:"code"`
	Msg    string `json:"msg"`
	RefCmd string `json:"ref_cmd,omitempty"` // 关联的原始命令（如 "send_msg"）
	RefID  string `json:"ref_id,omitempty"`  // 关联的 client_msg_id 或 event_id
}

type SendNackPayload struct {
	ClientMsgID string `json:"client_msg_id,omitempty"`
	Code        int    `json:"code"`
	Msg         string `json:"msg"`
}

type RetryMsgPayload struct {
	SessionID string `json:"session_id"`
	MsgID     int64  `json:"msg_id,string"`
}

type RetryMsgAckPayload struct {
	SessionID string `json:"session_id"`
	MsgID     int64  `json:"msg_id,string,omitempty"`
	Code      int    `json:"code"`
	Msg       string `json:"msg,omitempty"`
}

// Push message payload
type PushMsgPayload struct {
	InboxSeq        int64           `json:"inbox_seq,string"`
	MsgID           int64           `json:"msg_id,string"`
	SessionID       string          `json:"session_id"`
	ThreadID        string          `json:"thread_id,omitempty"`
	SessionType     int16           `json:"session_type,omitempty"`
	SenderID        int64           `json:"sender_id,string"`
	SenderType      int16           `json:"sender_type"`
	MsgType         int16           `json:"msg_type"`
	Content         string          `json:"content"`
	Extra           json.RawMessage `json:"extra,omitempty"`
	QuotedMessageID int64           `json:"quoted_message_id,string,omitempty"`
	IsRevoked       bool            `json:"is_revoked"`
	IsStreaming     bool            `json:"is_streaming,omitempty"`
	SyncEvent       string          `json:"sync_event,omitempty"`
	CreatedAt       int64           `json:"created_at"`
	VisibleTo       StringInt64s    `json:"visible_to,omitempty"`     // 群聊消息仅指定人可见
	ForcePush       bool            `json:"force_push,omitempty"`     // 跳过在线设备检查（语音通话等场景）
	TimeSensitive   bool            `json:"time_sensitive,omitempty"` // 高优先级推送（来电等时间敏感场景）
}

type EditEventPayload struct {
	InboxSeq        int64           `json:"inbox_seq,string,omitempty"`
	MsgID           int64           `json:"msg_id,string"`
	SessionID       string          `json:"session_id"`
	ThreadID        string          `json:"thread_id,omitempty"`
	SessionType     int16           `json:"session_type,omitempty"`
	SenderID        int64           `json:"sender_id,string"`
	SenderType      int16           `json:"sender_type"`
	MsgType         int16           `json:"msg_type"`
	Content         string          `json:"content"`
	Extra           json.RawMessage `json:"extra,omitempty"`
	QuotedMessageID int64           `json:"quoted_message_id,string,omitempty"`
	SyncEvent       string          `json:"sync_event,omitempty"`
	CreatedAt       int64           `json:"created_at"`
}

type RevokeSystemEventPayload struct {
	Text       string `json:"text"`
	ContextKey string `json:"context_key,omitempty"`
}

type AgentRevokeEventPayload struct {
	EventID     string                    `json:"event_id,omitempty"`
	SessionID   string                    `json:"session_id"`
	ThreadID    string                    `json:"thread_id,omitempty"`
	SessionType int16                     `json:"session_type,omitempty"`
	MsgID       int64                     `json:"msg_id,string"`
	SenderID    int64                     `json:"sender_id,string,omitempty"`
	IsRevoked   bool                      `json:"is_revoked"`
	SystemEvent *RevokeSystemEventPayload `json:"system_event,omitempty"`
}

type PushAckPayload struct {
	MsgID int64 `json:"msg_id,string"`
}

type AppStateSetPayload struct {
	AppState  string `json:"app_state"`
	UpdatedAt int64  `json:"updated_at,omitempty"`
}

// Pull sync payloads
type PullSyncPayload struct {
	LastInboxSeq int64 `json:"last_inbox_seq,string"`
}

type PullSyncRespPayload struct {
	HasMore        bool             `json:"has_more"`
	Messages       []PushMsgPayload `json:"messages"`
	UnreadSnapshot map[string]int   `json:"unread_snapshot"`
	// Phase 2.3: 快照生成时的最大 inbox_seq;客户端用于判定 unread_snapshot 是否仍是新鲜的。
	// 当 local_max_inbox_seq > snapshot_seq 时,客户端应忽略 unread_snapshot,仅追加 Messages。
	SnapshotSeq int64 `json:"snapshot_seq,string,omitempty"`
	// ActiveVoiceCalls 是该用户作为 owner 当前仍在进行中的 AI 代接通话全量快照,
	// 与 unread_snapshot 同属"以服务端为准、客户端整份覆盖"的对账语义:
	// 客户端在 has_more=false 时用它覆盖本地"语音中"徽标集合,
	// 离线期间漏收的 call:voice_status_end / call:ai_delegated 由此自愈。
	ActiveVoiceCalls []CallAiDelegatedPayload `json:"active_voice_calls"`
}

type SessionReadPayload struct {
	SessionID     string `json:"session_id"`
	LastReadMsgID int64  `json:"last_read_msg_id,string"`
}

type SessionReadAckPayload struct {
	SessionID     string `json:"session_id"`
	Code          int    `json:"code"`
	Msg           string `json:"msg,omitempty"`
	LastReadMsgID int64  `json:"last_read_msg_id,string,omitempty"`
}

type SessionReadSyncPayload struct {
	SessionID     string `json:"session_id"`
	ReaderID      int64  `json:"reader_id,string"`
	LastReadMsgID int64  `json:"last_read_msg_id,string"`
	// Phase 3.2: 仅推给 reader 自己的其他设备时填充, 用于多端未读数同步;
	// 其他成员收到的群聊已读广播 UnreadCount 为 nil。
	UnreadCount *int64 `json:"unread_count,string,omitempty"`
	UpdatedAt   int64  `json:"updated_at"`
}

type UnreadSyncPayload struct {
	SessionID   string `json:"session_id"`
	UnreadCount int64  `json:"unread_count"`
}

type SessionHistoryResetPayload struct {
	SessionID string `json:"session_id"`
	DeletedAt int64  `json:"deleted_at,omitempty"`
}

type SessionHistoryResetAckPayload struct {
	SessionID string `json:"session_id"`
	Code      int    `json:"code"`
	Msg       string `json:"msg,omitempty"`
}

type SessionHistoryResetsQueryAckPayload struct {
	Resets []SessionHistoryResetPayload `json:"resets"`
}

type SessionMemberChangedPayload struct {
	SessionID      string       `json:"session_id"`
	Action         string       `json:"action"`
	OperatorID     int64        `json:"operator_id,string,omitempty"`
	MemberID       int64        `json:"member_id,string,omitempty"`
	RemovedUserIDs StringInt64s `json:"removed_user_ids,omitempty"`
	Title          string       `json:"title,omitempty"`
	GroupNickname  string       `json:"group_nickname,omitempty"`
	UpdatedAt      int64        `json:"updated_at"`
}

// InternalCmdSessionTypeInvalidate 是跨节点失效各 ws 进程内 session_type 缓存的内部广播 cmd。
// 会话类型变更（如私聊转群）后由 api 服务发往 chan:broadcast，所有 ws 节点据此丢弃本地缓存。
const InternalCmdSessionTypeInvalidate = "internal:session_type_invalidate"

type SessionTypeInvalidatePayload struct {
	SessionID string `json:"session_id"`
}

type SessionAccessRevokedPayload struct {
	SessionID string `json:"session_id"`
	Reason    string `json:"reason,omitempty"`
	Message   string `json:"message,omitempty"`
	UpdatedAt int64  `json:"updated_at"`
}

type FriendSyncPayload struct {
	LastEventSeq int64 `json:"last_event_seq,string"`
}

type FriendSyncRespPayload struct {
	HasMore     bool              `json:"has_more"`
	Events      []json.RawMessage `json:"events"`
	MaxEventSeq int64             `json:"max_event_seq,string"`
}

type SessionActivitySetPayload struct {
	SessionID  string          `json:"session_id"`
	Kind       string          `json:"kind"`
	Active     bool            `json:"active"`
	TTLMS      int64           `json:"ttl_ms,omitempty"`
	RefMsgID   string          `json:"ref_msg_id,omitempty"`
	RefEventID string          `json:"ref_event_id,omitempty"`
	Activity   json.RawMessage `json:"activity,omitempty"`
}

type SessionActivityPayload struct {
	SessionID    string          `json:"session_id"`
	Kind         string          `json:"kind"`
	Active       bool            `json:"active"`
	ActorID      int64           `json:"actor_id,string"`
	ActorType    string          `json:"actor_type"`
	ExecutorID   int64           `json:"executor_id,string,omitempty"`
	ExecutorType string          `json:"executor_type,omitempty"`
	Source       string          `json:"source"`
	RefMsgID     string          `json:"ref_msg_id,omitempty"`
	RefEventID   string          `json:"ref_event_id,omitempty"`
	Activity     json.RawMessage `json:"activity,omitempty"`
	UpdatedAt    int64           `json:"updated_at"`
	ExpiresAt    int64           `json:"expires_at,omitempty"`
}

type SessionActivityListPayload struct {
	SessionID string `json:"session_id"`
}

type SessionActivityListRespPayload struct {
	SessionID  string                   `json:"session_id"`
	Activities []SessionActivityPayload `json:"activities"`
}

// Stream payloads
type StreamChunkPayload struct {
	MsgID        int64  `json:"msg_id,string"`
	SessionID    string `json:"session_id"`
	ThreadID     string `json:"thread_id,omitempty"`
	SenderID     int64  `json:"sender_id,string"`
	SenderType   int16  `json:"sender_type"`
	DeltaContent string `json:"delta_content"`
	// chunk_seq 是连续小整数,保留 number 形式。
	ChunkSeq  int64        `json:"chunk_seq,omitempty"`
	IsFinish  bool         `json:"is_finish"`
	CreatedAt int64        `json:"created_at,omitempty"`
	VisibleTo StringInt64s `json:"visible_to,omitempty"`
	// IsThinking 标记该流为"思考过程"流(由后端按 client_msg_id 的 _thinking 后缀派生),
	// 供前端在流式期即把气泡渲染为思考卡片,而非等 finalize 收尾才归类。
	IsThinking bool `json:"is_thinking,omitempty"`
	// QuotedMessageID 让前端流式期就能显示"引用回复"目标(与 finalize 落库的引用一致)。
	// 缺省 0 时 omitempty 不下发,前端按无引用处理。
	QuotedMessageID int64 `json:"quoted_message_id,string,omitempty"`
}

type StreamFinishPayload struct {
	MsgID           int64        `json:"msg_id,string"`
	SessionID       string       `json:"session_id"`
	ThreadID        string       `json:"thread_id,omitempty"`
	SenderID        int64        `json:"sender_id,string,omitempty"`
	SenderType      int16        `json:"sender_type"`
	FinalContent    string       `json:"final_content"`
	QuotedMessageID int64        `json:"quoted_message_id,string,omitempty"`
	LastChunkSeq    int64        `json:"last_chunk_seq,omitempty"`
	IsFinish        bool         `json:"is_finish"`
	CreatedAt       int64        `json:"created_at,omitempty"`
	VisibleTo       StringInt64s `json:"visible_to,omitempty"`
}

type StreamStopPayload struct {
	MsgID     int64  `json:"msg_id,string"`
	SessionID string `json:"session_id"`
}

type StreamErrorPayload struct {
	MsgID     int64  `json:"msg_id,string"`
	SessionID string `json:"session_id"`
	SenderID  int64  `json:"sender_id,string,omitempty"`
	ErrorCode int    `json:"error_code"`
	ErrorMsg  string `json:"error_msg"`
	CreatedAt int64  `json:"created_at,omitempty"`
}

// Override stream
type OverrideStreamPayload struct {
	SessionID   string `json:"session_id"`
	TargetMsgID int64  `json:"target_msg_id,string"`
}

// Re-auth
type ReAuthPayload struct {
	Token string `json:"token"`
}

type ReAuthAckPayload struct {
	Code      int    `json:"code"`
	Msg       string `json:"msg"`
	ExpiresIn int64  `json:"expires_in,omitempty"`
}

// Client stream chunk
type ClientStreamChunkPayload struct {
	SessionID    string `json:"session_id"`
	SenderID     int64  `json:"sender_id,string"`
	DeltaContent string `json:"delta_content"`
	IsFinish     bool   `json:"is_finish"`
}

// Delegate payloads
type DelegateStartPayload struct {
	SessionID             string `json:"session_id"`
	AgentID               int64  `json:"agent_id,string"`
	MaxConsecutiveReplies int    `json:"max_consecutive_replies,omitempty"`
}

type DelegateStopPayload struct {
	SessionID string `json:"session_id"`
}

type EventCancelPayload struct {
	EventID   string `json:"event_id"`
	SessionID string `json:"session_id"`
}

type QueueClearPayload struct {
	SessionID string `json:"session_id"`
}

// QueueReorderPayload 队列重排请求（app→server→agent）。
// OrderedEventIDs 为该会话排队事件的期望新顺序（队头在前，不含 running 项），
// agent 侧按愿望清单语义应用：已消失的 id 忽略、途中新入队的排尾，绝不报错。
type QueueReorderPayload struct {
	SessionID       string   `json:"session_id"`
	OrderedEventIDs []string `json:"ordered_event_ids"`
}

// EventHoldPayload 暂停/恢复排队任务（app→server→agent）。
// hold=true 施加持有：被持有的任务排到队头时整条队列原地等待（不跳过、不变序）；
// hold=false 解除。重复发 hold=true 即续期（重置 TTL）。
// reason 仅用于展示区分（editing=编辑流程自动 hold / manual=用户手动暂停），解除时不校验。
// ttl_ms 可选，agent 侧 clamp 到 [60s, 30min]，缺省 10min，到期自动解除。
type EventHoldPayload struct {
	SessionID string `json:"session_id"`
	EventID   string `json:"event_id"`
	Hold      bool   `json:"hold"`
	Reason    string `json:"reason,omitempty"`
	TTLMS     int64  `json:"ttl_ms,omitempty"`
}

// QueueEditPayload 改写排队任务文本（app→server→agent）。
// 仅命中 agent 队列 queued[] 中的项；命中则改写任务全文、重建预览并自动解除该任务的 hold，
// 成功后 agent 紧跟推一次权威 queue_snapshot。
type QueueEditPayload struct {
	SessionID string `json:"session_id"`
	EventID   string `json:"event_id"`
	Content   string `json:"content"`
}

type DelegateAckPayload struct {
	SessionID             string `json:"session_id"`
	AgentID               int64  `json:"agent_id,string"`
	Active                bool   `json:"active"`
	MaxConsecutiveReplies int    `json:"max_consecutive_replies,omitempty"`
}

type DelegateReplyPayload struct {
	SessionID string `json:"session_id"`
	AgentID   int64  `json:"agent_id,string"`
	Content   string `json:"content"`
	AutoSend  bool   `json:"auto_send"`
	Error     string `json:"error,omitempty"`
}

type AgentStateSyncPayload struct {
	AgentID int64           `json:"agent_id,string"`
	State   string          `json:"state"`
	Extra   json.RawMessage `json:"extra,omitempty"`
}

type DelegateListPayload struct{}

type DelegateListRespPayload struct {
	Delegates []DelegateItem `json:"delegates"`
}

type AgentDeliveryErrorPayload struct {
	SessionID    string `json:"session_id"`
	OwnerID      int64  `json:"owner_id,string,omitempty"`
	AgentID      int64  `json:"agent_id,string,omitempty"`
	TriggerMsgID int64  `json:"trigger_msg_id,string,omitempty"`
	Scope        string `json:"scope,omitempty"`
	Code         string `json:"code"`
	Msg          string `json:"msg,omitempty"`
}

type AgentEventAckPayload struct {
	EventID    string `json:"event_id"`
	SessionID  string `json:"session_id,omitempty"`
	MsgID      int64  `json:"msg_id,string,omitempty"`
	ReceivedAt int64  `json:"received_at,omitempty"`
}

type AgentEventResultPayload struct {
	EventID             string `json:"event_id"`
	TerminalCommitToken string `json:"terminal_commit_token,omitempty"`
	Status              string `json:"status"`
	Code                string `json:"code,omitempty"`
	Msg                 string `json:"msg,omitempty"`
	UpdatedAt           int64  `json:"updated_at,omitempty"`
}

type AgentOutputStopPayload struct {
	SessionID string `json:"session_id"`
	RunID     string `json:"run_id,omitempty"`
}

type AgentOutputGetPayload struct {
	SessionID string `json:"session_id"`
}

type AgentOutputGetRespPayload struct {
	SessionID  string                    `json:"session_id"`
	Active     bool                      `json:"active"`
	ResolvedAt int64                     `json:"resolved_at"`
	Status     *AgentOutputStatusPayload `json:"status,omitempty"`
}

type AgentOutputStopAckPayload struct {
	SessionID string `json:"session_id"`
	RunID     string `json:"run_id,omitempty"`
	StopID    string `json:"stop_id,omitempty"`
	Accepted  bool   `json:"accepted"`
	Msg       string `json:"msg,omitempty"`
	UpdatedAt int64  `json:"updated_at"`
}

type AgentOutputStatusPayload struct {
	RunID              string `json:"run_id"`
	SessionID          string `json:"session_id"`
	DispatchGeneration int64  `json:"dispatch_generation,string,omitempty"`
	Revision           int64  `json:"revision,omitempty"`
	Scope              string `json:"scope,omitempty"`
	OwnerID            int64  `json:"owner_id,string,omitempty"`
	AgentID            int64  `json:"agent_id,string,omitempty"`
	TriggerMsgID       int64  `json:"trigger_msg_id,string,omitempty"`
	StreamMsgID        int64  `json:"stream_msg_id,string,omitempty"`
	State              string `json:"state"`
	CanStop            bool   `json:"can_stop"`
	StopReason         string `json:"stop_reason,omitempty"`
	UpdatedAt          int64  `json:"updated_at"`
}

type AgentToolbarGetPayload struct {
	SessionID     string `json:"session_id"`
	TargetAgentID int64  `json:"target_agent_id,string,omitempty"`
}

type AgentToolbarOptionPayload struct {
	OptionID string `json:"option_id"`
	Label    string `json:"label"`
	Disabled bool   `json:"disabled"`
}

type AgentToolbarCommandItemPayload struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Exec        string `json:"exec"`
	// Source 是技能作用域（global/project/plugin/connector/...），供前端分组与图标。
	Source string `json:"source,omitempty"`
	// Path 是 SKILL.md 展示路径，home 已由 connector 折叠为 ~。
	Path string `json:"path,omitempty"`
	// Managed 为 true 表示系统托管技能（connector 投影/第三方插件/CLI 系统缓存），
	// 前端不得为其渲染上传按钮。
	Managed bool `json:"managed,omitempty"`
	// SyncState: synced(已同步) | modified(本地改过) | unsynced(未同步)，托管技能不带该字段。
	SyncState string `json:"sync_state,omitempty"`
}

type AgentToolbarItemPayload struct {
	ItemID       string                      `json:"item_id"`
	GroupID      string                      `json:"group_id,omitempty"`
	Kind         string                      `json:"kind"`
	ActionID     string                      `json:"action_id"`
	Label        string                      `json:"label"`
	Icon         string                      `json:"icon,omitempty"`
	Variant      string                      `json:"variant,omitempty"`
	Disabled     bool                        `json:"disabled"`
	Loading      bool                        `json:"loading"`
	Selected     bool                        `json:"selected"`
	Tooltip      string                      `json:"tooltip,omitempty"`
	BadgeText    string                      `json:"badge_text,omitempty"`
	ConfirmTitle string                      `json:"confirm_title,omitempty"`
	ConfirmText  string                      `json:"confirm_text,omitempty"`
	Value        string                      `json:"value,omitempty"`
	Placeholder  string                      `json:"placeholder,omitempty"`
	Options      []AgentToolbarOptionPayload `json:"options,omitempty"`

	// Progress-specific fields (kind == "progress")
	Percent        float64 `json:"percent,omitempty"`
	CenterText     string  `json:"center_text,omitempty"`
	ProgressDesc   string  `json:"progress_desc,omitempty"`
	ProgressDetail string  `json:"progress_detail,omitempty"`

	LocalAction string                           `json:"local_action,omitempty"`
	Commands    []AgentToolbarCommandItemPayload `json:"commands,omitempty"`
	Toggles     []AgentToolbarToggleItemPayload  `json:"toggles,omitempty"`
}

type AgentToolbarToggleItemPayload struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Version    string `json:"version,omitempty"`
	Enabled    bool   `json:"enabled"`
	Locked     bool   `json:"locked,omitempty"`
	LockReason string `json:"lock_reason,omitempty"`
}

type AgentToolbarSnapshotPayload struct {
	SessionID     string                            `json:"session_id"`
	AgentID       int64                             `json:"agent_id,string"`
	ToolbarID     string                            `json:"toolbar_id"`
	Revision      int64                             `json:"revision"`
	Visible       bool                              `json:"visible"`
	UpdatedAt     int64                             `json:"updated_at"`
	Items         []AgentToolbarItemPayload         `json:"items"`
	LibrarySkills []AgentToolbarLibrarySkillPayload `json:"library_skills,omitempty"`
	// AuditEnabled 对话审计开关服务端状态；nil 时字段不下发（旧后端等价），前端按本地回退处理。
	AuditEnabled *bool `json:"audit_enabled,omitempty"`
}

// AgentToolbarLibrarySkillPayload 透传 connector 上报的技能库项（技能库启用到 Agent）。
type AgentToolbarLibrarySkillPayload struct {
	Name         string                                `json:"name"`
	Description  string                                `json:"description,omitempty"`
	Digest       string                                `json:"digest,omitempty"`
	Dir          string                                `json:"dir,omitempty"`
	OwnerID      int64                                 `json:"owner_id,string,omitempty"`
	System       bool                                  `json:"system,omitempty"`
	EnableScopes AgentToolbarLibrarySkillScopesPayload `json:"enable_scopes"`
}

type AgentToolbarLibrarySkillScopesPayload struct {
	Global  string `json:"global,omitempty"`
	Project string `json:"project,omitempty"`
}

type AgentToolbarGetRespPayload struct {
	Code     int                         `json:"code"`
	Msg      string                      `json:"msg,omitempty"`
	Snapshot AgentToolbarSnapshotPayload `json:"snapshot"`
}

// ConversationAuditSetPayload 设置当前用户对指定 Agent 的对话审计开关
// （user+agent 维度，服务端持久化，启用后该 Agent 的所有会话生效）。
type ConversationAuditSetPayload struct {
	SessionID string `json:"session_id"`
	AgentID   int64  `json:"agent_id,string"`
	Enabled   bool   `json:"enabled"`
}

// ConversationAuditSetRespPayload 返回落库后的实际状态，供前端校准乐观更新。
type ConversationAuditSetRespPayload struct {
	Code      int    `json:"code"`
	Msg       string `json:"msg,omitempty"`
	SessionID string `json:"session_id"`
	AgentID   int64  `json:"agent_id,string"`
	Enabled   bool   `json:"enabled"`
}

type AgentToolbarActionPayload struct {
	SessionID      string `json:"session_id"`
	TargetAgentID  int64  `json:"target_agent_id,string,omitempty"`
	ToolbarID      string `json:"toolbar_id"`
	Revision       int64  `json:"revision"`
	ItemID         string `json:"item_id"`
	ActionID       string `json:"action_id"`
	ClientActionID string `json:"client_action_id"`
	Event          string `json:"event"`
	OptionID       string `json:"option_id,omitempty"`
}

type AgentToolbarActionAckPayload struct {
	SessionID       string `json:"session_id"`
	ToolbarID       string `json:"toolbar_id"`
	ClientActionID  string `json:"client_action_id"`
	Accepted        bool   `json:"accepted"`
	Duplicate       bool   `json:"duplicate,omitempty"`
	Code            string `json:"code,omitempty"`
	Msg             string `json:"msg,omitempty"`
	CurrentRevision int64  `json:"current_revision,omitempty"`
	UpdatedAt       int64  `json:"updated_at"`
}

type AgentEventStopPayload struct {
	StopID              string `json:"stop_id"`
	EventID             string `json:"event_id,omitempty"`
	TerminalCommitToken string `json:"terminal_commit_token,omitempty"`
	SessionID           string `json:"session_id"`
	Scope               string `json:"scope,omitempty"`
	OwnerID             int64  `json:"owner_id,string,omitempty"`
	AgentID             int64  `json:"agent_id,string,omitempty"`
	TriggerMsgID        int64  `json:"trigger_msg_id,string,omitempty"`
	StreamMsgID         int64  `json:"stream_msg_id,string,omitempty"`
	Reason              string `json:"reason,omitempty"`
	RequestedAt         int64  `json:"requested_at,omitempty"`
}

type AgentEventStopAckPayload struct {
	StopID    string `json:"stop_id"`
	EventID   string `json:"event_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Scope     string `json:"scope,omitempty"`
	Accepted  bool   `json:"accepted"`
	UpdatedAt int64  `json:"updated_at,omitempty"`
}

type AgentEventStopResultPayload struct {
	StopID              string `json:"stop_id"`
	EventID             string `json:"event_id,omitempty"`
	TerminalCommitToken string `json:"terminal_commit_token,omitempty"`
	SessionID           string `json:"session_id,omitempty"`
	Scope               string `json:"scope,omitempty"`
	Status              string `json:"status"`
	Code                string `json:"code,omitempty"`
	Msg                 string `json:"msg,omitempty"`
	UpdatedAt           int64  `json:"updated_at,omitempty"`
}

type AgentDeliveryStatusPayload struct {
	SessionID          string `json:"session_id"`
	DispatchGeneration int64  `json:"dispatch_generation,string,omitempty"`
	Revision           int64  `json:"revision,omitempty"`
	OwnerID            int64  `json:"owner_id,string,omitempty"`
	AgentID            int64  `json:"agent_id,string,omitempty"`
	TriggerMsgID       int64  `json:"trigger_msg_id,string,omitempty"`
	EventID            string `json:"event_id,omitempty"`
	Scope              string `json:"scope,omitempty"`
	Status             string `json:"status"`
	Code               string `json:"code,omitempty"`
	Msg                string `json:"msg,omitempty"`
	ReceivedAt         int64  `json:"received_at,omitempty"`
	UpdatedAt          int64  `json:"updated_at"`
}

// AgentDeliveryStatusBatchPayload is the payload for CmdAgentDeliveryStatusBatch.
// Sent once on auth/reauth to replay stored statuses; client applies them in
// a single IndexedDB transaction to avoid serial-write lag on Web.
type AgentDeliveryStatusBatchPayload struct {
	Items []AgentDeliveryStatusPayload `json:"items"`
}

type DelegateItem struct {
	SessionID             string `json:"session_id"`
	AgentID               int64  `json:"agent_id,string"`
	MaxConsecutiveReplies int    `json:"max_consecutive_replies,omitempty"`
}

// AgentInvokePayload is the inbound request from plugin via WS.
type AgentInvokePayload struct {
	InvokeID  string                 `json:"invoke_id"`
	Action    string                 `json:"action"`
	Params    map[string]interface{} `json:"params"`
	TimeoutMS int                    `json:"timeout_ms,omitempty"`
}

// AgentInvokeResultPayload is the outbound response to plugin via WS.
type AgentInvokeResultPayload struct {
	InvokeID string      `json:"invoke_id"`
	Code     int         `json:"code"`
	Msg      string      `json:"msg"`
	Data     interface{} `json:"data,omitempty"`
}

// LocalActionPayload is the server→client local action command.
type LocalActionPayload struct {
	ActionID   string                 `json:"action_id"`
	EventID    string                 `json:"event_id,omitempty"`
	ActionType string                 `json:"action_type"`
	Params     map[string]interface{} `json:"params,omitempty"`
	TimeoutMs  int                    `json:"timeout_ms,omitempty"`
}

// LocalActionResultPayload is the client→server local action result.
type LocalActionResultPayload struct {
	ActionID  string      `json:"action_id"`
	Status    string      `json:"status"` // ok | failed | unsupported | timeout
	Result    interface{} `json:"result,omitempty"`
	ErrorCode string      `json:"error_code,omitempty"`
	ErrorMsg  string      `json:"error_msg,omitempty"`
}

// AuditStatePayload is connector metadata for one audited event. It contains
// correlation and lifecycle information only; audit content is always fetched
// lazily through local actions.
type AuditStatePayload struct {
	EventID      string `json:"event_id"`
	SessionID    string `json:"session_id"`
	MsgID        int64  `json:"msg_id,string,omitempty"`
	AuditID      string `json:"audit_id,omitempty"`
	TurnID       string `json:"turn_id,omitempty"`
	State        string `json:"state"`
	Revision     int    `json:"revision,omitempty"`
	Quality      string `json:"quality,omitempty"`
	Truncated    bool   `json:"truncated,omitempty"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	UpdatedAt    int64  `json:"updated_at"`
}

// AuditTurnRequest identifies an audited message owned by the current user.
// The server resolves the immutable audit coordinates from its state table.
type AuditTurnRequest struct {
	SessionID string `json:"session_id"`
	MsgID     int64  `json:"msg_id,string"`
	AgentID   int64  `json:"agent_id,string,omitempty"`
	Revision  *int   `json:"revision,omitempty"`
	Cursor    string `json:"cursor,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	ContentID string `json:"content_id,omitempty"`
	MaxBytes  int    `json:"max_bytes,omitempty"`
}

// AuditTurnTarget is the content-free agent selector returned when a message
// was processed by multiple audited agents.
type AuditTurnTarget struct {
	AgentID  int64  `json:"agent_id,string"`
	State    string `json:"state"`
	Revision int    `json:"revision"`
}

// AuditTurnResponse carries a connector local-action result without turning
// it into a server-side replay store.
type AuditTurnResponse struct {
	State        string            `json:"state,omitempty"`
	AuditID      string            `json:"audit_id,omitempty"`
	TurnID       string            `json:"turn_id,omitempty"`
	Revision     int               `json:"revision,omitempty"`
	Result       interface{}       `json:"result,omitempty"`
	Targets      []AuditTurnTarget `json:"targets,omitempty"`
	ErrorCode    string            `json:"error_code,omitempty"`
	ErrorMessage string            `json:"error_message,omitempty"`
}

// AgentSkillUploadPayload is the client→server request to upload a local skill
// from the toolbar into the owner's skill library via the
// target agent's connector.
type AgentSkillUploadPayload struct {
	AgentID   int64  `json:"agent_id,string"`
	SessionID string `json:"session_id"`
	Name      string `json:"name"`
}

// AgentSkillUploadRespPayload is the server→client response.
type AgentSkillUploadRespPayload struct {
	Error     string `json:"error,omitempty"`
	Name      string `json:"name,omitempty"`
	SyncState string `json:"sync_state,omitempty"` // 成功后恒为 "synced"
}

// AgentSkillDeletePayload is the client→server request to delete a local skill
// directory/file via the target agent's connector.
type AgentSkillDeletePayload struct {
	AgentID   int64  `json:"agent_id,string"`
	SessionID string `json:"session_id"`
	Name      string `json:"name"`
}

// AgentSkillDeleteRespPayload is the server→client response.
type AgentSkillDeleteRespPayload struct {
	Error string `json:"error,omitempty"`
	Name  string `json:"name,omitempty"`
}

// AgentSkillEnablePayload is the client→server request to enable a skill from
// the agent's local skill library (技能库启用，方案 v2) at a given scope, by
// linking it into the target agent connector's active skill directory.
type AgentSkillEnablePayload struct {
	AgentID   int64  `json:"agent_id,string"`
	SessionID string `json:"session_id"`
	Name      string `json:"name"`
	Scope     string `json:"scope"`           // global | project
	Force     string `json:"force,omitempty"` // optional: replace_link | replace_with_link，用于覆盖冲突项
}

// AgentSkillEnableRespPayload is the server→client response.
type AgentSkillEnableRespPayload struct {
	Name          string `json:"name,omitempty"`
	Scope         string `json:"scope,omitempty"`
	EnableState   string `json:"enable_state,omitempty"` // 成功后如 "link"/"unmanaged" 等，回显 connector 上报的 enable_scopes 取值
	Uninstallable bool   `json:"uninstallable,omitempty"`
	Error         string `json:"error,omitempty"`
	ConflictKind  string `json:"conflict_kind,omitempty"`
}

// AgentSkillDisablePayload is the client→server request to disable a
// previously-enabled library skill at a given scope.
type AgentSkillDisablePayload struct {
	AgentID   int64  `json:"agent_id,string"`
	SessionID string `json:"session_id"`
	Name      string `json:"name"`
	Scope     string `json:"scope"` // global | project
}

// AgentSkillDisableRespPayload is the server→client response.
type AgentSkillDisableRespPayload struct {
	Name          string `json:"name,omitempty"`
	Scope         string `json:"scope,omitempty"`
	EnableState   string `json:"enable_state,omitempty"` // 成功后如 "none"
	Uninstallable bool   `json:"uninstallable,omitempty"`
	Error         string `json:"error,omitempty"`
	ConflictKind  string `json:"conflict_kind,omitempty"`
}

// AgentSkillRefreshPayload 是客户端→服务端的「技能下拉刷新」请求：让目标 agent
// 的 connector/插件立即重扫本地 skills + 技能库并重新上报，成功后回执里带上
// 最新工具栏快照，前端据此一次性更新技能弹窗两个 Tab。
type AgentSkillRefreshPayload struct {
	AgentID   int64  `json:"agent_id,string"`
	SessionID string `json:"session_id"`
}

// AgentSkillRefreshRespPayload 是服务端→客户端的回执。Error 非空表示失败；
// 成功时 Snapshot 为刷新后的完整工具栏快照（含 commands 与 library_skills），
// 语义与 agent_toolbar_get_resp.snapshot 完全一致。
type AgentSkillRefreshRespPayload struct {
	Error    string                      `json:"error,omitempty"`
	Snapshot AgentToolbarSnapshotPayload `json:"snapshot"`
}

// AgentFileListPayload is the client→server file list request.
type AgentFileListPayload struct {
	AgentID           int64    `json:"agent_id,string"`
	SessionID         string   `json:"session_id"`
	ParentID          *string  `json:"parent_id,omitempty"`
	ShowHidden        bool     `json:"show_hidden"`
	AllowedExtensions []string `json:"allowed_extensions,omitempty"`
}

// AgentFileListRespPayload is the server→client file list response.
type AgentFileListRespPayload struct {
	Error       string                   `json:"error,omitempty"`
	Files       []map[string]interface{} `json:"files,omitempty"`
	CurrentPath string                   `json:"current_path,omitempty"`
	// MachineName is the host/machine name the agent runs on, surfaced so the
	// client can tag favorites with their owning machine and filter by it.
	MachineName string `json:"machine_name,omitempty"`
}

// AgentCreateFolderPayload is the client→server create folder request.
type AgentCreateFolderPayload struct {
	AgentID   int64   `json:"agent_id,string"`
	SessionID string  `json:"session_id"`
	ParentID  *string `json:"parent_id,omitempty"`
	Name      string  `json:"name"`
}

// AgentCreateFolderRespPayload is the server→client create folder response.
type AgentCreateFolderRespPayload struct {
	Error  string                 `json:"error,omitempty"`
	Folder map[string]interface{} `json:"folder,omitempty"`
}

// WidgetSessionClosedPayload notifies a widget visitor that their session has been closed.
type WidgetSessionClosedPayload struct {
	SessionID string `json:"session_id"`
	Reason    string `json:"reason,omitempty"`
}

// AgentSessionBindingsListPayload is the client→server session bindings list request.
type AgentSessionBindingsListPayload struct {
	AgentID   int64  `json:"agent_id,string"`
	SessionID string `json:"session_id"`
}

// AgentSessionBindingsListRespPayload is the server→client session bindings list response.
type AgentSessionBindingsListRespPayload struct {
	Error    string                   `json:"error,omitempty"`
	Bindings []map[string]interface{} `json:"bindings,omitempty"`
}

// AgentSessionBindPayload is the client→server request to create/import an Agent session binding.
type AgentSessionBindPayload struct {
	AgentID        int64  `json:"agent_id,string"`
	ProviderKey    string `json:"provider_key,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
	Cwd            string `json:"cwd"`
	AgentSessionID string `json:"agent_session_id,omitempty"`
	Title          string `json:"title,omitempty"`
}

// AgentSessionBindRespPayload is the server→client response for session binding/import.
type AgentSessionBindRespPayload struct {
	Error     string                 `json:"error,omitempty"`
	Code      string                 `json:"code,omitempty"`
	SessionID string                 `json:"session_id,omitempty"`
	IsNew     bool                   `json:"is_new,omitempty"`
	Status    string                 `json:"status,omitempty"`
	Binding   map[string]interface{} `json:"binding,omitempty"`
}

// AgentTaskQueryRespPayload is the server→agent response for chat_state_query.
type AgentTaskQueryRespPayload struct {
	Tasks    []AgentTaskItem `json:"tasks"`
	Total    int64           `json:"total,string"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

// AgentTaskFinalResult is the single clean text reply attached to an exact
// completed chat_state_query. Found=false means the task completed without a
// persisted plain-text agent reply.
type AgentTaskFinalResult struct {
	Found     bool   `json:"found"`
	MsgID     int64  `json:"msg_id,string,omitempty"`
	Content   string `json:"content,omitempty"`
	CreatedAt int64  `json:"created_at,omitempty"`
}

// AgentTaskItem is the per-session task state returned to an agent. State is a
// single mutually-exclusive value (running / waiting_approval / waiting_question
// / completed / failed / idle); the consumer decides what to do from it alone.
type AgentTaskItem struct {
	SessionID     string                `json:"session_id"`
	AgentID       int64                 `json:"agent_id,string"`
	State         string                `json:"state"`
	TaskTitle     string                `json:"task_title,omitempty"`
	LastRunID     string                `json:"last_run_id,omitempty"`
	StopReason    string                `json:"stop_reason,omitempty"`
	FinalResult   *AgentTaskFinalResult `json:"final_result,omitempty"`
	StartedAtMs   *int64                `json:"started_at,omitempty"`
	CompletedAtMs *int64                `json:"completed_at,omitempty"`
	UpdatedAtMs   int64                 `json:"updated_at"`
}
