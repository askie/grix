package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/agentadapter"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/agentstream"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/gorilla/websocket"
)

var fallbackAgentStatusRevision atomic.Int64

func nextAgentStatusRevision(kind, eventID string) int64 {
	candidate := time.Now().UnixMilli()
	eventID = strings.TrimSpace(eventID)
	if store.RDB != nil && eventID != "" {
		ctx := context.Background()
		key := fmt.Sprintf("agentapi:status_revision:%s:%s", kind, eventID)
		if initialized, err := store.RDB.SetNX(ctx, key, candidate, 72*time.Hour).Result(); err == nil && initialized {
			return candidate
		}
		if revision, err := store.RDB.Incr(ctx, key).Result(); err == nil {
			_ = store.RDB.Expire(ctx, key, 72*time.Hour).Err()
			return revision
		}
	}
	for {
		current := fallbackAgentStatusRevision.Load()
		if candidate <= current {
			candidate = current + 1
		}
		if fallbackAgentStatusRevision.CompareAndSwap(current, candidate) {
			return candidate
		}
	}
}

type DelegateEventPayload struct {
	EventID             string                           `json:"event_id"`
	TerminalCommitToken string                           `json:"terminal_commit_token,omitempty"`
	EventType           string                           `json:"event_type"`
	MirrorMode          string                           `json:"mirror_mode,omitempty"`
	AgentID             int64                            `json:"agent_id,string"`
	OwnerID             int64                            `json:"owner_id,string"`
	SessionID           string                           `json:"session_id"`
	ThreadID            string                           `json:"thread_id,omitempty"`
	SessionType         int16                            `json:"session_type"`
	MsgID               int64                            `json:"msg_id,string"`
	QuotedMessageID     int64                            `json:"quoted_message_id,string,omitempty"`
	SenderID            int64                            `json:"sender_id,string"`
	MsgType             int16                            `json:"msg_type,omitempty"`
	Content             string                           `json:"content"`
	Extra               json.RawMessage                  `json:"extra,omitempty"`
	Attachments         []AttachmentPayload              `json:"attachments,omitempty"`
	BizCard             json.RawMessage                  `json:"biz_card,omitempty"`     // OpenClaw-specific; migrate to adapter
	ChannelData         json.RawMessage                  `json:"channel_data,omitempty"` // OpenClaw-specific; migrate to adapter
	MentionUserIDs      protocol.StringInt64s            `json:"mention_user_ids,omitempty"`
	ContextMessages     []protocol.ContextMessagePayload `json:"context_messages,omitempty"`
	CreatedAt           int64                            `json:"created_at"`
	// Command 标记 fire-and-forget 命令式事件（如工具栏 /stop）：照常下发给连接器，
	// 但后端不注册 active run、不登记 pending ack（避免被当作新一轮对话或触发超时重发）。
	Command bool `json:"command,omitempty"`
}

type AuthPayload struct {
	AgentID         int64           `json:"agent_id,string"`
	APIKey          string          `json:"api_key"`
	Client          string          `json:"client,omitempty"`
	ClientType      string          `json:"client_type,omitempty"`
	ContractVersion int             `json:"contract_version,omitempty"`
	ClientVersion   string          `json:"client_version,omitempty"`
	HostType        string          `json:"host_type,omitempty"`
	HostVersion     string          `json:"host_version,omitempty"`
	ProtocolVersion string          `json:"protocol_version,omitempty"`
	Capabilities    []string        `json:"capabilities,omitempty"`
	LocalActions    []string        `json:"local_actions,omitempty"`
	Skills          json.RawMessage `json:"skills,omitempty"`
	// LibrarySkills 是技能库全集 + 各作用域启用状态（技能库启用，方案 v2），
	// 与 Skills（当前已生效技能清单）并列上报，语义见 toolruntime.LibrarySkillEntry。
	LibrarySkills json.RawMessage `json:"library_skills,omitempty"`
	AdapterHint   string          `json:"adapter_hint,omitempty"`
	HostMeta      json.RawMessage `json:"host_meta,omitempty"`
	// TailnetIP 是 Node 的 Tailscale IP（100.x.x.x），仅在支持 tailnet_file_v1 时上报。
	TailnetIP string `json:"tailnet_ip,omitempty"`
	// SharedOwnerID 用于「agent 共享」：connector 为某个被共享者建立独立连接时，
	// 携带主人 api_key + 此字段(被共享者 user_id)。后端校验主人 key + 共享授权后，
	// 把本连接身份认定为该被共享者。为 0 表示主连接（主人本人）。
	SharedOwnerID int64 `json:"shared_owner_id,string,omitempty"`
}

type AuthAckPayload struct {
	Code                    int      `json:"code"`
	Msg                     string   `json:"msg"`
	AgentID                 int64    `json:"agent_id,string,omitempty"`
	OwnerID                 int64    `json:"owner_id,string,omitempty"`
	Protocol                string   `json:"protocol,omitempty"`
	HeartbeatSec            int      `json:"heartbeat_sec,omitempty"`
	ContractVersion         int      `json:"contract_version,omitempty"`
	AdapterID               string   `json:"adapter_id,omitempty"`
	SupportedCapabilities   []string `json:"supported_capabilities,omitempty"`
	DegradedCapabilities    []string `json:"degraded_capabilities,omitempty"`
	UnsupportedCapabilities []string `json:"unsupported_capabilities,omitempty"`
	AgentName               string   `json:"agent_name,omitempty"`
	Introduction            string   `json:"introduction,omitempty"`
	SystemPrompt            string   `json:"system_prompt"`
}

type SendMsgPayload struct {
	EventID         string          `json:"event_id,omitempty"`
	SessionID       string          `json:"session_id"`
	ThreadID        string          `json:"thread_id,omitempty"`
	ClientMsgID     string          `json:"client_msg_id"`
	MsgType         int16           `json:"msg_type"`
	Content         string          `json:"content"`
	Extra           json.RawMessage `json:"extra,omitempty"`
	MediaURL        string          `json:"media_url,omitempty"`
	QuotedMessageID int64           `json:"quoted_message_id,string,omitempty"`
}

type AgentStreamChunkPayload struct {
	EventID      string `json:"event_id,omitempty"`
	SessionID    string `json:"session_id"`
	ThreadID     string `json:"thread_id,omitempty"`
	DeltaContent string `json:"delta_content"`
	// chunk_seq 是连续小整数,保留 number 形式以减少 Agent 接入负担。
	ChunkSeq        int64  `json:"chunk_seq"`
	IsFinish        bool   `json:"is_finish"`
	ClientMsgID     string `json:"client_msg_id,omitempty"`
	QuotedMessageID int64  `json:"quoted_message_id,string,omitempty"`
	// IsThinking 显式标记该流为"思考过程"流。connector 应在 thinking 流上显式置 true;
	// 后端同时兼容旧约定:client_msg_id 以 _thinking 结尾亦视为 thinking 流(见 handleAgentAPIStreamChunk)。
	IsThinking bool `json:"is_thinking,omitempty"`
}

type SendNackPayload struct {
	ClientMsgID string `json:"client_msg_id,omitempty"`
	// Cmd/SessionID echo the rejected packet so fire-and-forget senders
	// (session_activity_set has no ack on success) can attribute the nack
	// without tracking outbound seq numbers.
	Cmd       string `json:"cmd,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Code      int    `json:"code"`
	Msg       string `json:"msg"`
}

type CodexEventPayload struct {
	EventID         string          `json:"event_id"`
	SessionID       string          `json:"session_id"`
	ThreadID        string          `json:"thread_id,omitempty"`
	QuotedMessageID int64           `json:"quoted_message_id,string,omitempty"`
	CodexEventType  string          `json:"codex_event_type"`
	CodexMethod     string          `json:"codex_method,omitempty"`
	CodexSequence   int64           `json:"codex_sequence"`
	CodexPayload    json.RawMessage `json:"codex_payload"`
	CodexAt         string          `json:"codex_at"`
	streamKey       string
}

type PiEventPayload struct {
	EventID     string          `json:"event_id"`
	SessionID   string          `json:"session_id"`
	ThreadID    string          `json:"thread_id,omitempty"`
	PiEventType string          `json:"pi_event_type"`
	PiSequence  int64           `json:"pi_sequence"`
	PiPayload   json.RawMessage `json:"pi_payload"`
	PiAt        string          `json:"pi_at"`
	streamKey   string
}

type EventAckPayload struct {
	EventID    string `json:"event_id"`
	SessionID  string `json:"session_id,omitempty"`
	MsgID      int64  `json:"msg_id,string,omitempty"`
	ReceivedAt int64  `json:"received_at,omitempty"`
}

type EventResultPayload struct {
	EventID             string `json:"event_id"`
	TerminalCommitToken string `json:"terminal_commit_token,omitempty"`
	Status              string `json:"status"`
	Code                string `json:"code,omitempty"`
	Msg                 string `json:"msg,omitempty"`
	UpdatedAt           int64  `json:"updated_at,omitempty"`
}

type SendMessageReq struct {
	EventID  string
	AgentID  int64
	OwnerID  int64
	CallerID int64 // 仅 ModeCaller 使用，作为消息的 sender_id
	// IdentityMode is for trusted in-process callers only. External Agent API
	// requests leave it empty and continue to use ModeAgentAPI permission checks.
	IdentityMode    string
	SessionID       string
	ThreadID        string
	ClientMsgID     string
	MsgType         int16
	Content         string
	Extra           json.RawMessage
	VisibleTo       []int64
	MediaURL        string
	QuotedMessageID int64
}

type SendMessageResult struct {
	MsgID     int64
	InboxSeq  int64
	CreatedAt int64
}

type SendMessageHandler func(ctx context.Context, req SendMessageReq) (*SendMessageResult, error)

type StreamChunkHandler func(ctx context.Context, agentID, ownerID int64, payload AgentStreamChunkPayload) error

type DeleteMsgPayload struct {
	SessionID string `json:"session_id"`
	MsgID     int64  `json:"msg_id,string"`
}

type DeleteMsgHandler func(ctx context.Context, agentID, ownerID int64, payload DeleteMsgPayload) error

type EditMsgPayload struct {
	SessionID string          `json:"session_id"`
	MsgID     int64           `json:"msg_id,string"`
	Content   string          `json:"content"`
	Extra     json.RawMessage `json:"extra,omitempty"`
}

type EditMsgHandler func(ctx context.Context, agentID, ownerID int64, payload EditMsgPayload) error

type ReactMsgPayload struct {
	SessionID string `json:"session_id"`
	MsgID     int64  `json:"msg_id,string"`
	Emoji     string `json:"emoji"`
	Op        string `json:"op,omitempty"`
}

type ReactMsgHandler func(ctx context.Context, agentID, ownerID int64, payload ReactMsgPayload) error

type MediaUploadInitPayload struct {
	UploadID  string `json:"upload_id"`
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	Mime      string `json:"mime,omitempty"`
	Purpose   string `json:"purpose,omitempty"`
}

type MediaUploadInitResult struct {
	UploadID  string            `json:"upload_id"`
	UploadURL string            `json:"upload_url"`
	Method    string            `json:"method,omitempty"`
	MediaURL  string            `json:"media_url,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}

type MediaUploadInitHandler func(ctx context.Context, agentID, ownerID int64, payload MediaUploadInitPayload) (*MediaUploadInitResult, error)

type SessionActivityHandler func(ctx context.Context, agentID, ownerID int64, payload protocol.SessionActivitySetPayload) error

type DeliveryStatusHandler func(payload protocol.AgentDeliveryStatusPayload)
type OutputStatusHandler func(payload protocol.AgentOutputStatusPayload)
type EventLifecyclePacketHandler func(ownerID int64, cmd string, payload json.RawMessage)

type AgentStateHandler func(ownerID int64, payload protocol.AgentStateSyncPayload)

type ConnectionEpochAllocator func(ctx context.Context, ownerID, agentID int64) (int64, error)

type StreamDisconnectHandler func(ctx context.Context, agentID, ownerID int64)

// ForceFinalizeStreamsHandler force-finalizes all active streaming sessions
// for a given agent+session so that subsequent chunks start fresh messages.
type ForceFinalizeStreamsHandler func(ctx context.Context, agentID, ownerID int64, sessionID string)

type SendError struct {
	Code     int
	Msg      string
	NotFound bool // event_id 不在 pending 表，可能是 agent 传错了 id
}

const agentAPIDeliveryMaxAttempts = 3

func (e *SendError) Error() string {
	if e == nil {
		return ""
	}
	return e.Msg
}

type Manager struct {
	upgrader               websocket.Upgrader
	heartbeat              time.Duration
	eventAckWait           time.Duration
	eventResultWait        time.Duration
	disconnectRecoveryWait time.Duration
	staleRunReapWait       time.Duration
	pendingTrackingTTL     time.Duration
	nodeID                 string
	adapterRegistry        *agentadapter.Registry
	sendFn                 SendMessageHandler
	streamChunkFn          StreamChunkHandler
	deleteMsgFn            DeleteMsgHandler
	editMsgFn              EditMsgHandler
	reactMsgFn             ReactMsgHandler
	mediaUploadInitFn      MediaUploadInitHandler
	activityFn             SessionActivityHandler
	statusFn               DeliveryStatusHandler
	outputStatusFn         OutputStatusHandler
	eventLifecycleFn       EventLifecyclePacketHandler
	stateFn                AgentStateHandler
	connectionEpochFn      ConnectionEpochAllocator
	disconnectFn           StreamDisconnectHandler
	forceFinalizeStreamsFn ForceFinalizeStreamsHandler

	// bg 汇总本 Manager 派生的全部后台工作（在服务的连接、后台协程、超时定时器），
	// Shutdown 据此关连接、停定时器并等协程退出。见 lifecycle.go。
	bg backgroundGroup

	// attachMu serializes the Redis authority claim with the local connection
	// table update. Epochs are reserved before the rest of authentication, so a
	// lower epoch handshake may arrive here after a higher epoch handshake.
	// Keeping claim+attach ordered prevents that delayed handshake from
	// replacing (and closing) its already-current successor on the same node.
	attachMu sync.Mutex
	mu       sync.RWMutex
	// conns 按 (agentID -> ownerID -> conn) 组织：同一个 agent 可同时挂多条连接，
	// 每条连接在握手时定死一个 ownerID（主人或被共享者），实现 agent 共享的物理隔离。
	conns                map[int64]map[int64]*agentConn
	acksMu               sync.Mutex
	pending              map[string]*pendingEventAck
	eventResultsMu       sync.Mutex
	eventResultsInFlight map[string]struct{}
	eventResultVerdicts  map[string]string
	eventResultsSettled  map[string]string
	localActionsMu       sync.Mutex
	pendingLocalActions  map[string]*pendingLocalAction
	runsMu               sync.Mutex
	// outboundVis caches the latest trigger visibility per agent+group session.
	outboundVisMu sync.Mutex
	outboundVis   map[string]outboundVisibilityEntry
	runs          map[string]*activeAgentRun
	runBySX       map[string]string

	codexChunkSeq sync.Map // event_id -> *int64, per-turn chunk sequence counter
	piChunkSeq    sync.Map // event_id -> *int64, per-turn chunk sequence counter
	piThinkingBuf sync.Map // event_id -> *strings.Builder, accumulated thinking content

	// streamChunkTrackers 跟踪 client_stream_chunk 的 chunk_seq 与累计数量,
	// 用于强制单调递增 + 防止单 event 无限累积导致下游被打爆。
	streamChunkTrackers chunkTrackers

	stopFenceTTL   time.Duration
	stopFenceUntil map[string]int64

	// crossNodeStopUntil 记录跨节点停止挂起状态：当用户所在节点无 in-memory run
	// 时，RequestOutputStop 走 durable 路径接受停止请求，在此记录 eventID 和过期时间。
	// LoadRunState 读取此标记，在 durable 快照上覆盖为 "stopping" 状态，使工具栏立即
	// 推出 stopping 快照，而非等到 connector 真正停止后才清除 loading。
	// key: activeRunSessionOwnerKey(sessionID, ownerID) + ":" + eventID
	crossNodeStopUntil map[string]int64

	delegateEventInterceptors []delegateEventInterceptor

	// tailnetCoordinator 负责 Tailnet 直传文件的协调逻辑。
	tailnetCoordinator *TailnetTransferCoordinator

	// mcpSessions 管理活跃的 MCP 透传会话（APP 内置 MCP Server 桥接）。
	mcpSessions *mcpSessionStore
	// humanWsSendFn 用于向指定 owner 的 Human WS 连接推帧。
	humanWsSendFn func(ownerID int64, data []byte)
}

type agentConn struct {
	ws      *websocket.Conn
	agentID int64
	ownerID int64
	// isPrimary 表示这条连接的身份是 agent 主人本人（ownerID == agent.OwnerID）。
	// 共享连接（被共享者）为 false。用于在只有 agentID 的旧调用点优先取主连接。
	isPrimary  bool
	clientID   string
	clientType string
	send       chan []byte
	// done 在 close() 时关闭，作为「本连接已终止」的唯一信号。
	// send 通道本身永不关闭：它有多个并发生产者（重放、事件下推、local_action…），
	// 关闭它会让「先判 closed 再写」的生产者踩到 send on closed channel 而 panic。
	done chan struct{}
	// sendMu 让 sendPayload 的「判活 + 入队」与 close() 互斥，
	// 使「报告已送达」与「连接仍存活」原子成立——既不丢消息也不重复投递。
	sendMu sync.Mutex
	// wg 跟踪本连接派生的协程（writePump / 积压重放 / agent_invoke），
	// ServeWS 返回前等它们退出，保证连接的收尾早于注销与进程/测试收尾。
	wg sync.WaitGroup
	// stateMu serializes lease refresh, replacement, and unregister for this
	// connection so the same generation cannot publish online after offline.
	stateMu sync.Mutex

	// Adapter metadata (set during auth)
	adapter       agentadapter.AgentAdapter
	adapterID     string
	hostVersion   string
	capabilities  []string
	localActions  []string
	skills        []toolruntime.SkillEntry
	librarySkills []toolruntime.LibrarySkillEntry

	// TailnetIP 是 Node 上报的 Tailscale IP（100.x.x.x），空表示不在 Tailnet。
	tailnetIP string

	// 连接来源（阶段0 安全观测）：真实客户端 IP、地理归属、连接日志行 ID、建连时间。
	clientIP    string
	ipLocation  string
	connLogID   int64
	connectedAt time.Time
	// connectionEpoch is a Redis-coordinated generation for this owner+agent
	// identity. It deliberately does not derive from connectedAt because clocks
	// can move backwards or disagree across websocket nodes.
	connectionEpoch int64
	// finalizeOnce 保证断开回填（日志 + Redis 清理）只执行一次；
	// kick 等主动断开先带原因执行，读循环退出的兜底 finalize 即成为空操作。
	finalizeOnce sync.Once

	closeOnce sync.Once
	closed    atomic.Bool
	// shutdownClose 由 Manager.Shutdown 置位：writePump 退出时写 1001 going away
	// 关闭帧（默认空帧），让连接器区分「服务端主动关停」并立即重连。
	shutdownClose atomic.Bool
	seq           int64

	// violations 用于 Phase 1.3 的累计违规熔断：
	// 每次返回 4xxx 业务错误时累加, 60s 滑动窗口内累计阈值后服务端主动 close 连接。
	violations           atomic.Int32
	violationWindowStart atomic.Int64 // unix milliseconds
}

func NewManager(allowedWebOrigins string, heartbeat time.Duration, sendFn SendMessageHandler, streamChunkFn StreamChunkHandler, deleteMsgFn DeleteMsgHandler, reactMsgFn ReactMsgHandler) *Manager {
	if heartbeat <= 0 {
		heartbeat = 30 * time.Second
	}
	manager := &Manager{
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			// 与 connector 协商 permessage-deflate（RFC 7692）。connector 的 ws 客户端
			// 默认就会请求该扩展，服务端开启后握手才能压缩收发的 JSON/文本帧，显著节省
			// agent 通道流量。协商失败时自动回退为不压缩，无副作用。
			EnableCompression: true,
			CheckOrigin: func(r *http.Request) bool {
				return security.IsAllowedWebOrigin(r, allowedWebOrigins)
			},
		},
		heartbeat:              heartbeat,
		eventAckWait:           5 * time.Second,
		eventResultWait:        resolveEventResultWait(),
		disconnectRecoveryWait: 2 * time.Minute,
		staleRunReapWait:       resolveStaleRunReapWait(),
		pendingTrackingTTL:     durablePendingDelegateTTL,
		sendFn:                 sendFn,
		streamChunkFn:          streamChunkFn,
		deleteMsgFn:            deleteMsgFn,
		reactMsgFn:             reactMsgFn,
		conns:                  make(map[int64]map[int64]*agentConn),
		pending:                make(map[string]*pendingEventAck),
		pendingLocalActions:    make(map[string]*pendingLocalAction),
		runs:                   make(map[string]*activeAgentRun),
		runBySX:                make(map[string]string),
		stopFenceTTL:           agentstream.DefaultStoppedFenceTTL,
		stopFenceUntil:         make(map[string]int64),
	}
	manager.tailnetCoordinator = newTailnetTransferCoordinator(manager)
	manager.mcpSessions = newMcpSessionStore()
	manager.registerDefaultDelegateEventInterceptors()
	return manager
}

func (m *Manager) SetDeliveryStatusHandler(fn DeliveryStatusHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statusFn = fn
}

func (m *Manager) SetHumanWsSendFn(fn func(ownerID int64, data []byte)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.humanWsSendFn = fn
}

// SetAdapterRegistry sets the adapter registry used for auth-phase adapter selection.
func (m *Manager) SetAdapterRegistry(registry *agentadapter.Registry) {
	m.adapterRegistry = registry
}

func resolveEventResultWait() time.Duration {
	sec := config.C.LLM.EventResultWaitSec
	if sec <= 0 {
		return 15 * time.Minute
	}
	d := time.Duration(sec) * time.Second
	if d < 10*time.Second {
		return 10 * time.Second
	}
	if d > 30*time.Minute {
		return 30 * time.Minute
	}
	return d
}

// resolveStaleRunReapWait keeps the historical config name but now controls
// only when a silent run is logged as an observation before new dispatch.
// Silence never settles or stops the run.
func resolveStaleRunReapWait() time.Duration {
	sec := config.C.LLM.StaleRunReapSec
	if sec <= 0 {
		return 10 * time.Minute
	}
	d := time.Duration(sec) * time.Second
	if d < time.Minute {
		return time.Minute
	}
	if d > 30*time.Minute {
		return 30 * time.Minute
	}
	return d
}

func normalizeDeclaredNames(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func hasDeclaredName(values []string, target string) bool {
	normalizedTarget := strings.ToLower(strings.TrimSpace(target))
	if normalizedTarget == "" {
		return false
	}
	for _, value := range values {
		if strings.ToLower(strings.TrimSpace(value)) == normalizedTarget {
			return true
		}
	}
	return false
}

func (m *Manager) SetOutputStatusHandler(fn OutputStatusHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outputStatusFn = fn
}

func (m *Manager) SetEventLifecyclePacketHandler(fn EventLifecyclePacketHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventLifecycleFn = fn
}

func (m *Manager) SetSessionActivityHandler(fn SessionActivityHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activityFn = fn
}

func (m *Manager) SetEditMsgHandler(fn EditMsgHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.editMsgFn = fn
}

func (m *Manager) SetMediaUploadInitHandler(fn MediaUploadInitHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mediaUploadInitFn = fn
}

func (m *Manager) SetAgentStateHandler(fn AgentStateHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stateFn = fn
}

// SetConnectionEpochAllocator configures cross-node generation allocation.
// Production installs the Redis-backed allocator. A nil allocator keeps epoch
// zero only for isolated manager users and legacy tests that do not publish
// through the production Server.
func (m *Manager) SetConnectionEpochAllocator(fn ConnectionEpochAllocator) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connectionEpochFn = fn
}

func (m *Manager) SetStreamDisconnectHandler(fn StreamDisconnectHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.disconnectFn = fn
}

func (m *Manager) SetForceFinalizeStreamsHandler(fn ForceFinalizeStreamsHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.forceFinalizeStreamsFn = fn
}

// forceFinalizeSessionStreams 强制收尾该 agent 在指定会话里仍未收尾的流。
// 调用方只在事件终态（结算成功）时触发；处理器本身按流幂等：没有未收尾的流时
// 零副作用，已正常收尾的流不会被二次广播。
func (m *Manager) forceFinalizeSessionStreams(conn *agentConn, sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if conn == nil || conn.agentID <= 0 || sessionID == "" {
		return
	}
	m.mu.RLock()
	fn := m.forceFinalizeStreamsFn
	m.mu.RUnlock()
	if fn == nil {
		return
	}
	fn(context.Background(), conn.agentID, conn.ownerID, sessionID)
}

func (m *Manager) SetNodeID(nodeID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodeID = strings.TrimSpace(nodeID)
}

// ForEachLocalAgentConn 遍历本节点所有本地 agent 连接（主连接 + 共享连接，
// 同一 agent 可有多条 owner 连接）。回调返回 false 停止遍历。
// 快照在 RLock 下取得，回调不应阻塞；遍历时连接可能已断开，由发送方自行判活。
func (m *Manager) ForEachLocalAgentConn(fn func(conn *agentConn) bool) {
	m.mu.RLock()
	conns := make([]*agentConn, 0, len(m.conns))
	for _, owners := range m.conns {
		for _, c := range owners {
			if c != nil {
				conns = append(conns, c)
			}
		}
	}
	m.mu.RUnlock()
	for _, c := range conns {
		if !fn(c) {
			break
		}
	}
}

// CountConns 返回本节点当前 agent 连接总数（每个 agent 可有多条 owner 连接）。
// 供 metrics 采样器读取，反映本节点 agent 侧 WS 连接负载。
func (m *Manager) CountConns() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, owners := range m.conns {
		n += len(owners)
	}
	return n
}

func resolveDelegateEventScope(evt DelegateEventPayload) string {
	if evt.OwnerID == evt.SenderID {
		return protocol.AgentDeliveryScopeDirect
	}
	return protocol.AgentDeliveryScopeDelegate
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func marshalPayload(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

func sendPacketDirect(wsConn *websocket.Conn, pkt protocol.Packet) {
	data, _ := json.Marshal(pkt)
	_ = wsConn.WriteMessage(websocket.TextMessage, data)
}
