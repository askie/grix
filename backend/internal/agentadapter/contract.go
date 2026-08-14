// Package agentadapter provides the AI agent adapter abstraction layer.
//
// This package defines the stable contract between the backend and external AI
// agents (OpenClaw, Claude, Codex, etc.). It is the single source of truth for
// adapter interfaces, metadata types, and domain events.
//
// Layering:
//   - contract.go  — pure type definitions, no behavior
//   - adapter.go   — AgentAdapter interface definition
//   - registry.go  — adapter registration and lookup
//   - selector.go  — version-range matching and adapter selection
package agentadapter

import (
	"encoding/json"

	"github.com/askie/grix/backend/internal/ws/protocol"
)

// ---- Metadata types (auth phase) ----

// AgentClientMeta carries the identity and capability metadata that a plugin
// sends during the auth phase. The backend uses this to select an adapter.
type AgentClientMeta struct {
	AgentID         int64    `json:"agent_id,string"`
	Client          string   `json:"client,omitempty"`
	ClientType      string   `json:"client_type"`
	ClientVersion   string   `json:"client_version,omitempty"`
	HostType        string   `json:"host_type,omitempty"`
	HostVersion     string   `json:"host_version,omitempty"`
	ProtocolVersion string   `json:"protocol_version,omitempty"`
	ContractVersion int      `json:"contract_version,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`
	AdapterHint     string   `json:"adapter_hint,omitempty"`
}

// ---- Inbound payload types ----

// InboundSendMsgPayload is the adapter-level view of a send_msg payload.
// It captures the fields that adapters need for card normalization.
// BizCard and ChannelData are top-level protocol fields, symmetric with
// DomainOutboundEvent's layout.
type InboundSendMsgPayload struct {
	SessionID   string          `json:"session_id"`
	ThreadID    string          `json:"thread_id,omitempty"`
	Content     string          `json:"content"`
	Extra       json.RawMessage `json:"extra,omitempty"`
	BizCard     json.RawMessage `json:"biz_card,omitempty"`
	ChannelData json.RawMessage `json:"channel_data,omitempty"`
}

// ---- Normalized domain events (inbound) ----

// NormalizedInboundEvent is the backend-agnostic representation of an inbound
// agent event, after adapter-specific parsing is complete.
type NormalizedInboundEvent struct {
	SessionID string          `json:"session_id,omitempty"`
	ThreadID  string          `json:"thread_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	Extra     json.RawMessage `json:"extra,omitempty"`
	Drop      bool            `json:"drop,omitempty"`
}

// ---- Normalized domain events (outbound) ----

type AttachmentPayload struct {
	AttachmentType string `json:"attachment_type,omitempty"`
	MediaURL       string `json:"media_url,omitempty"`
	FileName       string `json:"file_name,omitempty"`
	ContentType    string `json:"content_type,omitempty"`
}

// DomainOutboundEvent is the backend's generic representation of a message
// to be sent to an external agent. The adapter translates this into the
// agent-specific wire format.
type DomainOutboundEvent struct {
	EventID         string                           `json:"event_id"`
	EventType       string                           `json:"event_type"`
	MirrorMode      string                           `json:"mirror_mode,omitempty"`
	AgentID         int64                            `json:"agent_id,string"`
	OwnerID         int64                            `json:"owner_id,string"`
	SessionID       string                           `json:"session_id"`
	ThreadID        string                           `json:"thread_id,omitempty"`
	SessionType     int16                            `json:"session_type"`
	MsgID           int64                            `json:"msg_id,string"`
	QuotedMessageID int64                            `json:"quoted_message_id,string,omitempty"`
	SenderID        int64                            `json:"sender_id,string"`
	MsgType         int16                            `json:"msg_type,omitempty"`
	Content         string                           `json:"content"`
	Extra           json.RawMessage                  `json:"extra,omitempty"`
	Attachments     []AttachmentPayload              `json:"attachments,omitempty"`
	BizCard         json.RawMessage                  `json:"biz_card,omitempty"`
	ChannelData     json.RawMessage                  `json:"channel_data,omitempty"`
	MentionUserIDs  protocol.StringInt64s            `json:"mention_user_ids,omitempty"`
	ContextMessages []protocol.ContextMessagePayload `json:"context_messages,omitempty"`
	CreatedAt       int64                            `json:"created_at"`
}

// DomainApprovalEvent represents a backend approval request that the adapter
// should translate into an agent-specific local_action or equivalent.
type DomainApprovalEvent struct {
	EventType  string          `json:"event_type"`
	SessionID  string          `json:"session_id"`
	ActionType string          `json:"action_type"`
	ActionID   string          `json:"action_id"`
	Params     json.RawMessage `json:"params,omitempty"`
	TimeoutMs  int             `json:"timeout_ms,omitempty"`
}

// DomainStatusEvent represents a status update to be sent to the agent.
type DomainStatusEvent struct {
	EventType string          `json:"event_type"`
	SessionID string          `json:"session_id"`
	Status    string          `json:"status"`
	Extra     json.RawMessage `json:"extra,omitempty"`
}

// DomainRevokeEvent represents a backend delete notification that an adapter
// can translate into agent-specific revoke handling.
type DomainRevokeEvent struct {
	EventID     string `json:"event_id,omitempty"`
	SessionID   string `json:"session_id"`
	ThreadID    string `json:"thread_id,omitempty"`
	SessionType int16  `json:"session_type"`
	MsgID       int64  `json:"msg_id,string"`
	SenderID    int64  `json:"sender_id,string,omitempty"`
	IsRevoked   bool   `json:"is_revoked"`
}

// ---- Adapter output packets ----

// AdapterOutboundPacket is the wire-format packet produced by an adapter
// after translating a DomainOutboundEvent.
type AdapterOutboundPacket struct {
	Cmd     string          `json:"cmd"`
	Payload json.RawMessage `json:"payload"`
}

// AdapterApprovalPacket is the wire-format packet produced by an adapter
// after translating a DomainApprovalEvent.
type AdapterApprovalPacket struct {
	Cmd     string          `json:"cmd"`
	Payload json.RawMessage `json:"payload"`
}

// AdapterStatusPacket is the wire-format packet produced by an adapter
// after translating a DomainStatusEvent.
type AdapterStatusPacket struct {
	Cmd     string          `json:"cmd"`
	Payload json.RawMessage `json:"payload"`
}
