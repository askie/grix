package agentadapter

import "context"

// AgentAdapter is the interface that every AI family adapter must implement.
// Each adapter handles the translation between backend domain events and
// the agent-specific wire format.
//
// Implementations:
//   - openclaw/adapter.go  — OpenClaw adapter
//   - claude/adapter.go    — Claude adapter
//   - hermes/adapter.go    — Hermes adapter
//   - (future) codex/      — Codex adapter
type AgentAdapter interface {
	// Family returns the AI family identifier (e.g. "openclaw", "claude").
	Family() string

	// AdapterID returns the unique adapter identifier (e.g. "openclaw/base", "openclaw/v2").
	AdapterID() string

	// Supports returns true if this adapter can handle the given client metadata.
	// The registry uses this for version-range matching.
	Supports(meta AgentClientMeta) bool

	// NormalizeInbound translates an agent-specific inbound packet into a
	// backend-agnostic domain event.
	NormalizeInbound(ctx context.Context, rawPayload []byte) (*NormalizedInboundEvent, error)

	// NormalizeOutbound translates a backend domain event into an agent-specific
	// outbound packet.
	NormalizeOutbound(ctx context.Context, event DomainOutboundEvent) (*AdapterOutboundPacket, error)

	// NormalizeApproval translates a backend approval event into an agent-specific
	// approval packet (e.g. local_action for OpenClaw).
	NormalizeApproval(ctx context.Context, event DomainApprovalEvent) (*AdapterApprovalPacket, error)

	// NormalizeStatus translates a backend status event into an agent-specific
	// status packet.
	NormalizeStatus(ctx context.Context, event DomainStatusEvent) (*AdapterStatusPacket, error)
}

// RevokeEventAdapter is an optional extension for adapters that want to
// normalize delete notifications before they reach the plugin.
type RevokeEventAdapter interface {
	NormalizeRevoke(ctx context.Context, event DomainRevokeEvent) (*AdapterOutboundPacket, error)
}

// AdapterMeta returns static metadata about an adapter. Adapters can optionally
// implement this interface to declare version ranges and capabilities.
type AdapterMeta interface {
	AgentAdapter

	// VersionRange returns the supported host version range (e.g. ">=2026.1 <2027.0").
	// Empty string means all versions.
	VersionRange() string

	// RequiredCapabilities returns capabilities that must be present for this adapter.
	RequiredCapabilities() []string

	// OptionalCapabilities returns capabilities this adapter can use if present.
	OptionalCapabilities() []string

	// DegradePolicy returns the degradation behavior when requirements aren't met.
	DegradePolicy() DegradePolicy
}

// DegradePolicy defines what happens when an adapter can't fully support a connection.
type DegradePolicy int

const (
	// DegradeNone means no degradation is allowed; reject the connection.
	DegradeNone DegradePolicy = iota
	// DegradeToReadOnly means degraded connections can only receive events, not send.
	DegradeToReadOnly
	// DegradeToBasic means use the most generic behavior possible.
	DegradeToBasic
)
