// Package notification implements the Agent notification and offline
// interaction channel: it turns agent-side events that need human attention
// (approval requests, questions, task completion/failure) into push
// notifications and, for actionable events, short-lived signed callback tokens
// that let the user respond without opening the app.
//
// See docs/architecture/32_agent_notification_offline_interaction_design.md.
package notification

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/nats-io/nats.go"
)

// NATSSubjectEvents is the JetStream subject that carries AgentNotificationEvent
// values from the producers (ws/agentapi hooks) to the dispatcher running inside
// the push service. It is registered on the shared stream in store.InitNATS.
const NATSSubjectEvents = "agent.notification.events"

// NATSSubjectTTS carries events the dispatcher routes to the TTS channel; the ws
// service subscribes and broadcasts a tts_broadcast WS cmd to online clients
// (Phase 5).
const NATSSubjectTTS = "agent.notification.tts"

// Event keys. These are stable identifiers persisted in notification_prefs and
// embedded in push payloads / action tokens.
const (
	EventApprovalRequested     = "approval_requested"
	EventAgentQuestion         = "agent_question"
	EventTaskCompleted         = "task_completed"
	EventTaskFailed            = "task_failed"
	EventTaskStoppedUnexpected = "task_stopped_unexpected"
	// EventAgentOnline / EventAgentOffline are agent presence notifications:
	// the agent's own connector coming online or dropping offline. See
	// presence.go.
	EventAgentOnline  = "agent_online"
	EventAgentOffline = "agent_offline"
	// EventTaskStarted is retired (太多余, per product decision) — no longer
	// published or listed in preferences. The constant stays so in-flight
	// events from older nodes still render during a rolling deploy.
	EventTaskStarted = "task_started"
)

// Actions a callback token may authorize.
const (
	ActionApprove = "approve"
	ActionDeny    = "deny"
	ActionReply   = "reply"
	ActionStop    = "stop"
)

// Notification channels (notification_prefs.channels values).
const (
	ChannelPush      = "push"
	ChannelTTS       = "tts"
	ChannelVoiceCall = "voice_call"
)

// AllEventKeys is the canonical ordered list of every event key, used to seed a
// user's default preferences on first read.
var AllEventKeys = []string{
	EventApprovalRequested,
	EventAgentQuestion,
	EventTaskCompleted,
	EventTaskFailed,
	EventTaskStoppedUnexpected,
	EventAgentOnline,
	EventAgentOffline,
}

// ActionMeta carries the operation context for callbackable events. It is nil
// for pure-notification events (task_completed, etc).
type ActionMeta struct {
	AvailableActions  []string `json:"available_actions"`
	ApprovalCommandID string   `json:"approval_command_id,omitempty"`
	QuestionID        string   `json:"question_id,omitempty"`
	QuestionMessageID int64    `json:"question_message_id,omitempty"`
}

// AgentNotificationEvent is the payload published to NATSSubjectEvents.
type AgentNotificationEvent struct {
	EventKey   string      `json:"event_key"`
	UserID     int64       `json:"user_id"`
	AgentID    int64       `json:"agent_id"`
	SessionID  string      `json:"session_id"`
	RunID      string      `json:"run_id,omitempty"`
	RunEventID string      `json:"run_event_id,omitempty"`
	Summary    string      `json:"summary"`
	ActionMeta *ActionMeta `json:"action_meta,omitempty"`
	// IdempotencyKey is both forwarded as Nats-Msg-Id and serialized for the
	// dispatcher's permanent per-channel receipt fence. JetStream de-duplication
	// is only a transport optimization, not the correctness boundary.
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// Callbackable reports whether the event supports an offline action.
func (e *AgentNotificationEvent) Callbackable() bool {
	return e.ActionMeta != nil && len(e.ActionMeta.AvailableActions) > 0
}

// Publish best-effort emits an event to the dispatcher. It never blocks the
// caller's hot path on failure — a dropped notification must not break agent
// message processing — so errors are logged and swallowed.
func Publish(evt AgentNotificationEvent) {
	if err := PublishReliable(evt); err != nil {
		logger.L.Warnf("notification publish err event=%s user=%d: %v", evt.EventKey, evt.UserID, err)
	}
}

// PublishReliable reports marshal/publish failures to durable outbox callers.
// A nil JetStream means notifications are disabled in this process (including
// unit tests), which remains a successful no-op just like Publish.
func PublishReliable(evt AgentNotificationEvent) error {
	if store.JS == nil {
		return nil
	}
	if evt.EventKey == "" || evt.UserID == 0 {
		return fmt.Errorf("invalid notification event")
	}
	data, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("marshal notification event: %w", err)
	}
	var opts []nats.PubOpt
	if evt.IdempotencyKey != "" {
		opts = append(opts, nats.MsgId(evt.IdempotencyKey))
	}
	if _, err := store.JS.Publish(NATSSubjectEvents, data, opts...); err != nil {
		return err
	}
	return nil
}

// PublishCtx is Publish with an explicit context for callers that have one.
func PublishCtx(_ context.Context, evt AgentNotificationEvent) { Publish(evt) }
