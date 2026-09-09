// Package traecli provides the adapter for the official TraeCode CLI
// (docs.trae.cn/cli, binary `traecli`/`trae-cli`), a distinct product from
// the TRAE IDE (Trae.app/Trae CN.app) and from bytedance/trae-agent.
//
// TraeCLI is bridged by grix-connector's generic ACP adapter (`traecli acp
// serve`, client_type=traecli / host_type=traecli / adapter_hint=traecli/base).
// Its ACP surface probed as standard (loadSession, mcpCapabilities.http/sse,
// three session modes default/plan/bypass_permissions, two harmless `_meta`
// extension flags) with no private tool-call/session extensions observed —
// unlike kimi/hermes/reasonix this adapter therefore holds no vendor-specific
// card normalization and delegates entirely to the generic acp.Adapter.
// Revisit once a real turn (post-login) surfaces traecli-specific event shapes.
package traecli

import (
	"github.com/askie/grix/backend/internal/agentadapter"
	acpadapter "github.com/askie/grix/backend/internal/agentadapter/acp"
)

const (
	Family    = "traecli"
	AdapterID = "traecli/base"
)

type Adapter struct {
	*acpadapter.Adapter
}

func NewAdapter() *Adapter {
	return &Adapter{Adapter: acpadapter.NewAdapter()}
}

func (a *Adapter) Family() string    { return Family }
func (a *Adapter) AdapterID() string { return AdapterID }

func (a *Adapter) Supports(meta agentadapter.AgentClientMeta) bool {
	family := meta.HostType
	if family == "" {
		family = meta.ClientType
	}
	return family == Family
}
