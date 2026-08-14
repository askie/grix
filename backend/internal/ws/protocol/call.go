package protocol

// 语音通话信令命令常量（Phase 1）
const (
	// 客户端 → 服务端
	CmdCallInvite     = "call:invite"      // A 发起呼叫
	CmdCallAnswer     = "call:answer"      // B 真人接听
	CmdCallReject     = "call:reject"      // B 拒接
	CmdCallHangup     = "call:hangup"      // 任一方挂断
	CmdCallClientDiag = "call:client_diag" // 客户端媒体连接诊断

	// 服务端 → 客户端
	CmdCallInviteAck    = "call:invite_ack"    // 发起确认（含 call_id）
	CmdCallRing         = "call:ring"          // 通知 B 来电
	CmdCallPeerAnswered = "call:peer_answered" // 通知 A 对方已接
	CmdCallState        = "call:state"         // 状态广播（双方）
	CmdCallTimeout      = "call:timeout"       // 超时未接
	CmdCallBusy         = "call:busy"          // 对方忙线

	// Phase 2 预留（AI 托管）
	CmdCallAnswerWithAI = "call:answer_with_ai"
	CmdCallTakeover     = "call:takeover"
	CmdCallHandBack     = "call:hand_back"

	// 多访客客服：owner 旁听/离开某通 AI 代接通话（人单线接管）
	CmdCallListen    = "call:listen"     // C→S owner 请求进房旁听（抢参与锁、签发 callee token）
	CmdCallListenAck = "call:listen_ack" // S→C 旁听确认（含 room token）
	CmdCallLeave     = "call:leave"      // C→S owner 离开通话但不结束（已接管则先交回 AI），访客继续与 AI 通话

	// 直接拨打语音大模型 agent（owner-only，直拨 AI 通话）
	CmdCallDirectAI = "call:direct_ai"

	// 语音大脑（owner-only）：在"我↔文字 agent"私聊里，用用户级语音大脑(type=4)当语音通道，
	// 语音转写驱动该文字 agent、文字回复念回。通话锚定到文字 agent 工作会话。
	CmdCallVoiceBrain = "call:voice_brain"

	// 会话级语音托管（电话秘书）：用户为某会话绑定/解绑语音模型自动代接
	CmdCallVoiceDelegateStart = "call:voice_delegate_start"
	CmdCallVoiceDelegateStop  = "call:voice_delegate_stop"
	CmdCallVoiceDelegateAck   = "call:voice_delegate_ack"

	// 通知 owner：其语音托管 AI 正在代接一通来电，可随时接管
	CmdCallAiDelegated = "call:ai_delegated"

	// 通知 owner：某通语音通话已结束，用于清除会话列表"语音中"徽标
	CmdCallVoiceStatusEnd = "call:voice_status_end"

	// Widget 访客排队等待（线路满时）
	CmdCallQueued       = "call:queued"        // S→C 访客进入等待队列（含 call_id + position）
	CmdCallQueueUpdate  = "call:queue_update"  // S→C 队列位置更新
	CmdCallQueueExpired = "call:queue_expired" // S→C 排队超时取消
)

// 通话状态值
const (
	CallStateRinging  = 0
	CallStateActive   = 1
	CallStateEnded    = 2
	CallStateRejected = 3
	CallStateMissed   = 4
	CallStateError    = 5
)

// CallInvitePayload C→S 发起呼叫
type CallInvitePayload struct {
	PeerID   string `json:"peer_id"`
	PeerType string `json:"peer_type"` // "user"
	CallMode int    `json:"call_mode"` // 1=voice
}

// ICEServer 描述一个 WebRTC ICE 服务器（TURN/STUN），下发给客户端用于 NAT 穿透。
type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// CallInviteAckPayload S→C 发起确认（含 call_id + room token + ICE 服务器）
type CallInviteAckPayload struct {
	CallID     string      `json:"call_id"`
	RoomToken  string      `json:"room_token"`            // LiveKit JWT（caller 用）
	RoomURL    string      `json:"room_url"`              // LiveKit server URL
	ICEServers []ICEServer `json:"ice_servers,omitempty"` // TURN/STUN 服务器
}

// CallAnswerPayload C→S 接听
type CallAnswerPayload struct {
	CallID string `json:"call_id"`
}

// CallRejectPayload C→S 拒接
type CallRejectPayload struct {
	CallID string `json:"call_id"`
	Reason string `json:"reason,omitempty"`
}

// CallHangupPayload C→S 挂断
type CallHangupPayload struct {
	CallID string `json:"call_id"`
}

// CallClientDiagPayload C→S 客户端媒体连接阶段诊断。
type CallClientDiagPayload struct {
	CallID string `json:"call_id,omitempty"`
	Stage  string `json:"stage"`
	Detail string `json:"detail,omitempty"`
}

// CallRingPayload S→C 来电通知
type CallRingPayload struct {
	CallID     string `json:"call_id"`
	CallerID   string `json:"caller_id"`
	CallerName string `json:"caller_name,omitempty"`
	CallMode   int    `json:"call_mode"`
}

// CallPeerAnsweredPayload S→C 对方已接
type CallPeerAnsweredPayload struct {
	CallID     string      `json:"call_id"`
	Mode       string      `json:"mode"`                  // "human" | "ai_delegated"
	RoomToken  string      `json:"room_token"`            // LiveKit JWT
	RoomURL    string      `json:"room_url"`              // LiveKit server URL
	ICEServers []ICEServer `json:"ice_servers,omitempty"` // TURN/STUN 服务器
}

// CallStatePayload S→C 状态广播
type CallStatePayload struct {
	CallID           string `json:"call_id"`
	State            int    `json:"state"`
	Reason           string `json:"reason,omitempty"`
	Ts               int64  `json:"ts"`
	AnsweredDeviceID string `json:"answered_device_id,omitempty"`
}

// --- Phase 2: AI 托管信令 ---

// CallAnswerWithAIPayload C→S B 选择 AI 代接
type CallAnswerWithAIPayload struct {
	CallID  string `json:"call_id"`
	AgentID string `json:"agent_id"` // 语音 Agent ID（int64 as string）
}

// CallTakeoverPayload C→S B 接管（AI 闭嘴）
type CallTakeoverPayload struct {
	CallID string `json:"call_id"`
}

// CallHandBackPayload C→S B 将通话交回给 AI
type CallHandBackPayload struct {
	CallID string `json:"call_id"`
}

// CallAIStatePayload S→C AI 托管状态变更通知
type CallAIStatePayload struct {
	CallID string `json:"call_id"`
	Mode   string `json:"mode"` // "ai_delegated" | "human_active"
	Ts     int64  `json:"ts"`
}

// CallDirectAIPayload C→S owner 直接拨打语音大模型 agent
type CallDirectAIPayload struct {
	AgentID string `json:"agent_id"`
}

// CallVoiceBrainPayload C→S owner 发起语音大脑通话。
// AgentID 是文字 agent；SessionID 是发起时所在会话，转写落此会话。
type CallVoiceBrainPayload struct {
	AgentID   string `json:"agent_id"`
	SessionID string `json:"session_id"`
}

// CallVoiceDelegateStartPayload C→S 为会话绑定语音托管 agent
type CallVoiceDelegateStartPayload struct {
	SessionID string `json:"session_id"`
	AgentID   string `json:"agent_id"`
}

// CallVoiceDelegateStopPayload C→S 解绑会话语音托管
type CallVoiceDelegateStopPayload struct {
	SessionID string `json:"session_id"`
}

// CallVoiceDelegateAckPayload S→C 语音托管设置结果
type CallVoiceDelegateAckPayload struct {
	SessionID string `json:"session_id"`
	AgentID   string `json:"agent_id"`
	Active    bool   `json:"active"`
}

// CallAiDelegatedPayload S→C 通知 owner：AI 正在代接，可随时通过 call:listen 进房旁听。
// room token/url 不在此下发——等 owner 发 call:listen 时再按需签发，避免 token 广播到所有设备。
type CallAiDelegatedPayload struct {
	CallID    string `json:"call_id"`
	SessionID string `json:"session_id"` // 该通话所属会话（驱动会话列表"语音中"徽标 + 定位接管入口）
	PeerName  string `json:"peer_name"`
}

// CallListenPayload C→S owner 请求进房旁听某通 AI 代接通话
type CallListenPayload struct {
	CallID string `json:"call_id"`
}

// CallListenAckPayload S→C 旁听确认（含 callee room token）
type CallListenAckPayload struct {
	CallID     string      `json:"call_id"`
	RoomToken  string      `json:"room_token"`
	RoomURL    string      `json:"room_url"`
	ICEServers []ICEServer `json:"ice_servers,omitempty"`
}

// CallLeavePayload C→S owner 离开通话但不结束（访客继续与 AI 通话）
type CallLeavePayload struct {
	CallID string `json:"call_id"`
}

// CallVoiceStatusEndPayload S→C 通话结束，清除会话列表"语音中"徽标
type CallVoiceStatusEndPayload struct {
	CallID    string `json:"call_id"`
	SessionID string `json:"session_id"`
}

// CallQueuedPayload S→C 访客进入等待队列
type CallQueuedPayload struct {
	CallID   string `json:"call_id"`
	Position int    `json:"position"`
}

// CallQueueUpdatePayload S→C 队列位置更新
type CallQueueUpdatePayload struct {
	Position int `json:"position"`
}

// CallQueueExpiredPayload S→C 排队超时取消
type CallQueueExpiredPayload struct{}
