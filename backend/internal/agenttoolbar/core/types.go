package core

import (
	"context"

	"github.com/askie/grix/backend/internal/agentslashcmd"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
)

type SessionInfo struct {
	SessionID   string
	SessionType int16
}

type AgentInfo struct {
	AgentID      int64
	OwnerID      int64
	ProviderType int16
	ClientType   string
}

type BindingInfo struct {
	ProviderKey  string
	BindingID    string
	Cwd          string
	Status       string
	WorkerStatus string
	Meta         map[string]any
}

type MatchContext struct {
	OwnerID int64
	Session SessionInfo
	Agent   AgentInfo
	Runtime toolruntime.Profile
}

type BuildInput struct {
	OwnerID  int64
	Session  SessionInfo
	Agent    AgentInfo
	Language string
	// LanguageFull 是用户语言偏好的完整值（userpref.Language 的返回值，
	// zh/en/ja/ko/de/fr/es/pt/ru/ar/hi 十一选一），不像 Language 那样收窄到
	// 工具栏历史上只支持的 zh/en 二选一。目前只用于斜杠命令说明的 11 语解析
	// （见 agentslashcmd.DescriptionFor），其余字段仍按 Language 走既有的
	// zh/en 双语 tooli18n 路径。
	LanguageFull string
	Runtime      toolruntime.Profile
	Binding      BindingInfo
	Run          toolruntime.RunState
	// CustomSlashCommands 是主人给该 agent 加的自定义斜杠命令（按创建顺序）。
	// 各 Package.Build() 不感知它，统一由 normalizeSnapshot 追加到内置命令之后。
	CustomSlashCommands []agentslashcmd.SlashCommand
	// Relay 是"Grix中转"开关的只读投影（resolver 填充）。nil = 不展示中转按钮：
	// 功能 flag 关闭 / client type 不支持 / 请求人不是 agent 主人 / 投影查询失败。
	// 各 Package.Build() 不感知它，统一由 core 注入按钮项并拦截其动作。
	Relay *RelayInfo
}

// RelayInfo 是"Grix中转"开关在工具栏上的展示原料，与
// service.GatewayAgentRelayToolbarProjection 一一对应（core 不反向依赖 service，
// 字段拷贝由 resolver 完成）。
type RelayInfo struct {
	// Enabled 是 desired 期望开关，也是按钮 Selected 的唯一来源。
	Enabled bool
	// RelayModel 是解析后的展示/开启用模型（服务端已保存值：state > 活跃Key > 钱包兜底）。
	RelayModel string
	// Revision 是 state 行乐观锁版本；0 = 无行。
	Revision int64
	// Applied 是 connector 最近一次有效回执的实际态，只用于"待生效"提示。
	Applied bool
	// StateKnown 表示服务端能否确知实际态；false 时 Applied 不可信。
	StateKnown bool
	// BalancePositive 表示钱包余额 > 0；已开启时即使为 false 也必须保留关闭入口。
	BalancePositive bool
}

type LocalActionRequest struct {
	OwnerID    int64
	AgentID    int64
	SessionID  string
	ActionType string
	Params     map[string]any
	TimeoutMs  int
}

type StopOutputRequest struct {
	AgentID   int64
	OwnerID   int64
	SessionID string
	RunID     string
}

type Executor interface {
	DispatchLocalAction(ctx context.Context, req LocalActionRequest) error
	StopOutput(ctx context.Context, req StopOutputRequest) error
	// SendStopText 复用本地停止效果（标记 run stopping / 清 composing），
	// 但把对连接器的派发改为下发一条 /stop 文本命令，而非 event_stop。
	SendStopText(ctx context.Context, req StopOutputRequest) error
}

type ComposingStateClearer interface {
	ClearComposingState(ctx context.Context, req StopOutputRequest) error
}

// CommandTextRequest 是以主人身份向 agent 下发一条命令文本的请求。
// 命令文本不产生聊天消息，agent 收到的是一条 Command 事件。
type CommandTextRequest struct {
	OwnerID   int64
	AgentID   int64
	SessionID string
	Content   string
}

// CommandTextSender 是 Executor 的可选能力：把工具栏动作变成一条发给 agent 的
// 命令文本。通用 ACP 的自定义下拉框用它回传用户的选择——ACP 协议没有对应的
// RPC，而 local_action 没有 run 上下文（连接器转发 agent 输出依赖 run.eventId），
// agent 收到后回的话一个字也发不出来。
//
// 按可选接口而非 Executor 方法提供：加进 Executor 会要求每一个既有实现和测试
// 替身同步补一个方法，而这条能力只有通用 ACP 用得到。
type CommandTextSender interface {
	SendCommandText(ctx context.Context, req CommandTextRequest) error
}

type ActionInput struct {
	BuildInput BuildInput
	Snapshot   toolprotocol.Snapshot
	Item       toolprotocol.Item
	Request    toolprotocol.ActionRequest
	Executor   Executor
}

type ActionAck struct {
	SessionID       string
	ToolbarID       string
	ClientActionID  string
	Accepted        bool
	Duplicate       bool
	Code            string
	Message         string
	CurrentRevision int64
	UpdatedAt       int64
}

type Package interface {
	Key() string
	Match(ctx MatchContext) bool
	Build(ctx context.Context, in BuildInput) (toolprotocol.Snapshot, error)
	HandleAction(ctx context.Context, in ActionInput) (toolprotocol.ActionResult, error)
}
