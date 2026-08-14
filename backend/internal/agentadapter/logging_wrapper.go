package agentadapter

import (
	"context"
	"encoding/json"
	"time"

	"github.com/askie/grix/backend/internal/pkg/adapterlog"
)

type LoggingAdapter struct {
	inner      AgentAdapter
	family     string
	adapterID  string
	logMgr     *adapterlog.Manager
}

func NewLoggingAdapter(inner AgentAdapter, mgr *adapterlog.Manager) AgentAdapter {
	return &LoggingAdapter{
		inner:     inner,
		family:    inner.Family(),
		adapterID: inner.AdapterID(),
		logMgr:    mgr,
	}
}

func (la *LoggingAdapter) Family() string      { return la.family }
func (la *LoggingAdapter) AdapterID() string    { return la.adapterID }
func (la *LoggingAdapter) Supports(meta AgentClientMeta) bool { return la.inner.Supports(meta) }

func (la *LoggingAdapter) NormalizeInbound(ctx context.Context, rawPayload []byte) (*NormalizedInboundEvent, error) {
	sessionID := extractSessionID(rawPayload)
	input, _ := json.Marshal(json.RawMessage(rawPayload))

	result, err := la.inner.NormalizeInbound(ctx, rawPayload)

	entry := adapterlog.LogEntry{
		Ts:        time.Now().Format(time.RFC3339Nano),
		Dir:       "inbound",
		Method:    "NormalizeInbound",
		Family:    la.family,
		AdapterID: la.adapterID,
		SessionID: sessionID,
		Input:     input,
	}
	if err != nil {
		entry.Error = err.Error()
	} else if result != nil {
		entry.SessionID = result.SessionID
		out, _ := json.Marshal(result)
		entry.Output = out
	}
	la.logMgr.WriteEntry(la.family, entry.SessionID, entry)

	return result, err
}

func (la *LoggingAdapter) NormalizeOutbound(ctx context.Context, event DomainOutboundEvent) (*AdapterOutboundPacket, error) {
	input, _ := json.Marshal(event)

	result, err := la.inner.NormalizeOutbound(ctx, event)

	entry := adapterlog.LogEntry{
		Ts:        time.Now().Format(time.RFC3339Nano),
		Dir:       "outbound",
		Method:    "NormalizeOutbound",
		Family:    la.family,
		AdapterID: la.adapterID,
		SessionID: event.SessionID,
		Input:     input,
	}
	if err != nil {
		entry.Error = err.Error()
	} else if result != nil {
		out, _ := json.Marshal(result)
		entry.Output = out
	}
	la.logMgr.WriteEntry(la.family, event.SessionID, entry)

	return result, err
}

func (la *LoggingAdapter) NormalizeApproval(ctx context.Context, event DomainApprovalEvent) (*AdapterApprovalPacket, error) {
	input, _ := json.Marshal(event)

	result, err := la.inner.NormalizeApproval(ctx, event)

	entry := adapterlog.LogEntry{
		Ts:        time.Now().Format(time.RFC3339Nano),
		Dir:       "outbound",
		Method:    "NormalizeApproval",
		Family:    la.family,
		AdapterID: la.adapterID,
		SessionID: event.SessionID,
		Input:     input,
	}
	if err != nil {
		entry.Error = err.Error()
	} else if result != nil {
		out, _ := json.Marshal(result)
		entry.Output = out
	}
	la.logMgr.WriteEntry(la.family, event.SessionID, entry)

	return result, err
}

func (la *LoggingAdapter) NormalizeStatus(ctx context.Context, event DomainStatusEvent) (*AdapterStatusPacket, error) {
	input, _ := json.Marshal(event)

	result, err := la.inner.NormalizeStatus(ctx, event)

	entry := adapterlog.LogEntry{
		Ts:        time.Now().Format(time.RFC3339Nano),
		Dir:       "outbound",
		Method:    "NormalizeStatus",
		Family:    la.family,
		AdapterID: la.adapterID,
		SessionID: event.SessionID,
		Input:     input,
	}
	if err != nil {
		entry.Error = err.Error()
	} else if result != nil {
		out, _ := json.Marshal(result)
		entry.Output = out
	}
	la.logMgr.WriteEntry(la.family, event.SessionID, entry)

	return result, err
}

// NormalizeRevoke delegates to the inner adapter if it implements RevokeEventAdapter.
func (la *LoggingAdapter) NormalizeRevoke(ctx context.Context, event DomainRevokeEvent) (*AdapterOutboundPacket, error) {
	revoker, ok := la.inner.(RevokeEventAdapter)
	if !ok {
		return nil, nil
	}

	input, _ := json.Marshal(event)

	result, err := revoker.NormalizeRevoke(ctx, event)

	entry := adapterlog.LogEntry{
		Ts:        time.Now().Format(time.RFC3339Nano),
		Dir:       "outbound",
		Method:    "NormalizeRevoke",
		Family:    la.family,
		AdapterID: la.adapterID,
		SessionID: event.SessionID,
		Input:     input,
	}
	if err != nil {
		entry.Error = err.Error()
	} else if result != nil {
		out, _ := json.Marshal(result)
		entry.Output = out
	}
	la.logMgr.WriteEntry(la.family, event.SessionID, entry)

	return result, err
}

// Compile-time interface checks.
var (
	_ AgentAdapter     = (*LoggingAdapter)(nil)
	_ RevokeEventAdapter = (*LoggingAdapter)(nil)
)

func extractSessionID(raw []byte) string {
	var v struct {
		SessionID string `json:"session_id"`
	}
	json.Unmarshal(raw, &v)
	return v.SessionID
}
