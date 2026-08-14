package core

import (
	"context"

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
	Runtime  toolruntime.Profile
	Binding  BindingInfo
	Run      toolruntime.RunState
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
