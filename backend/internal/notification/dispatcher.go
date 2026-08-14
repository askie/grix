package notification

import (
	"context"
	"encoding/json"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/push/provider"
	"github.com/askie/grix/backend/internal/store"
	"github.com/nats-io/nats.go"
)

const (
	dispatcherQueueGroup = "notification-dispatcher-v1"
	dispatcherDurable    = "notification-dispatcher-v1"
	dispatcherAckWait    = 1 * time.Minute
)

// PushFunc delivers an assembled payload to a user's devices. The push service
// injects push.Worker.PushNotification.
type PushFunc func(ctx context.Context, userID int64, payload *provider.PushPayload) error

// Dispatcher consumes AgentNotificationEvent values from NATS, applies user
// preferences, mints action tokens for callbackable events, and fans out to the
// configured channels. It runs inside the push service process.
type Dispatcher struct {
	push     PushFunc
	presence *PresenceNotifier
	sub      *nats.Subscription
}

// NewDispatcher builds a dispatcher with the given push delivery function.
func NewDispatcher(push PushFunc) *Dispatcher {
	return &Dispatcher{push: push, presence: NewPresenceNotifier(push)}
}

// Start subscribes to the notification event subject. It returns an error if the
// subscription cannot be established; the subscription itself runs async.
func (d *Dispatcher) Start(ctx context.Context) error {
	// The presence ticker is Redis-driven (offline confirmation + trailing
	// merge) and independent of NATS, so start it even when JetStream is down.
	d.presence.StartTicker(ctx)
	if store.JS == nil {
		logger.L.Warn("notification dispatcher: jetstream unavailable, not starting")
		return nil
	}
	sub, err := store.JS.QueueSubscribe(
		NATSSubjectEvents,
		dispatcherQueueGroup,
		func(msg *nats.Msg) { d.handle(ctx, msg) },
		nats.ManualAck(),
		nats.Durable(dispatcherDurable),
		nats.DeliverNew(),
		nats.AckWait(dispatcherAckWait),
		nats.MaxDeliver(3),
	)
	if err != nil {
		return err
	}
	d.sub = sub
	logger.L.Infof("notification dispatcher subscribed subject=%s group=%s", NATSSubjectEvents, dispatcherQueueGroup)
	return nil
}

func (d *Dispatcher) handle(ctx context.Context, msg *nats.Msg) {
	var evt AgentNotificationEvent
	if err := json.Unmarshal(msg.Data, &evt); err != nil {
		logger.L.Errorf("notification dispatcher: bad event payload: %v", err)
		_ = msg.Ack() // poison message, don't redeliver
		return
	}

	// task_started is retired; drop stragglers published by older ws nodes
	// during a rolling deploy.
	if evt.EventKey == EventTaskStarted {
		_ = msg.Ack()
		return
	}

	// Agent presence signals are handled by the PresenceNotifier, which owns the
	// state machine + leading/trailing merge. Only agent_online arrives here
	// (offline is confirmed internally by the ticker after the flap window);
	// route it and ack. A stray agent_offline is dropped defensively.
	if evt.EventKey == EventAgentOnline {
		d.presence.OnOnlineSignal(ctx, evt.UserID, evt.AgentID)
		_ = msg.Ack()
		return
	}
	if evt.EventKey == EventAgentOffline {
		_ = msg.Ack()
		return
	}

	pref, err := ResolvePref(evt.UserID, evt.EventKey)
	if err != nil {
		logger.L.Warnf("notification dispatcher: resolve pref user=%d event=%s err=%v", evt.UserID, evt.EventKey, err)
		_ = msg.Ack()
		return
	}
	if !pref.Enabled {
		_ = msg.Ack()
		return
	}

	// Mint a single token shared across channels for callbackable events.
	var token string
	var exp int64
	if evt.Callbackable() {
		claims := buildClaims(&evt)
		t, err := GenerateToken(claims)
		if err != nil {
			logger.L.Warnf("notification dispatcher: token gen user=%d event=%s err=%v", evt.UserID, evt.EventKey, err)
		} else {
			token = t
			exp = claims.Exp
		}
	}

	if pref.HasChannel(ChannelPush) {
		claimed, claimErr := store.ClaimAgentNotificationReceipt(
			evt.IdempotencyKey,
			ChannelPush,
		)
		if claimErr != nil {
			logger.L.Warnf("notification dispatcher: claim push receipt key=%s err=%v", evt.IdempotencyKey, claimErr)
			return
		}
		if claimed {
			d.dispatchPush(ctx, &evt, token, exp)
		}
	}
	if pref.HasChannel(ChannelTTS) {
		claimed, claimErr := store.ClaimAgentNotificationReceipt(
			evt.IdempotencyKey,
			ChannelTTS,
		)
		if claimErr != nil {
			logger.L.Warnf("notification dispatcher: claim tts receipt key=%s err=%v", evt.IdempotencyKey, claimErr)
			return
		}
		if claimed {
			dispatchTTS(&evt)
		}
	}
	// voice_call: Phase 2.

	_ = msg.Ack()
}

func (d *Dispatcher) dispatchPush(ctx context.Context, evt *AgentNotificationEvent, token string, exp int64) {
	if d.push == nil {
		return
	}
	lang := userPreferredLanguage(evt.UserID)
	payload := &provider.PushPayload{
		Title:     pushTitle(evt, lang),
		Body:      pushBody(evt, lang),
		SessionID: evt.SessionID,
		EventKey:  evt.EventKey,
		// Agent notifications are opt-in and must reach the device even when a WS
		// session is live — the user may be on a different screen and still wants
		// to know an agent finished / needs them. Unlike chat messages, seeing the
		// app open does not make the notification redundant, so never skip online
		// devices here.
		ForcePush: true,
	}
	if evt.Callbackable() && token != "" {
		payload.Category = categoryFor(evt.EventKey)
		payload.ActionToken = token
		payload.AvailableActions = evt.ActionMeta.AvailableActions
		payload.Expiration = exp
		payload.TimeSensitive = true
	}
	if err := d.push(ctx, evt.UserID, payload); err != nil {
		logger.L.Warnf("notification dispatcher: push user=%d event=%s err=%v", evt.UserID, evt.EventKey, err)
	}
}

// dispatchTTS hands off to the ws service via NATS so it can broadcast a
// tts_broadcast WS cmd to the user's online clients (consumed in Phase 5).
func dispatchTTS(evt *AgentNotificationEvent) {
	if store.JS == nil {
		return
	}
	data, err := json.Marshal(evt)
	if err != nil {
		return
	}
	if _, err := store.JS.Publish(NATSSubjectTTS, data); err != nil {
		logger.L.Warnf("notification dispatcher: tts publish user=%d err=%v", evt.UserID, err)
	}
}

func buildClaims(evt *AgentNotificationEvent) ActionTokenClaims {
	target := ActionTarget{
		SessionID:  evt.SessionID,
		RunEventID: evt.RunEventID,
		RunID:      evt.RunID,
		AgentID:    evt.AgentID,
	}
	if evt.ActionMeta != nil {
		target.ApprovalCommandID = evt.ActionMeta.ApprovalCommandID
		target.QuestionID = evt.ActionMeta.QuestionID
		target.QuestionMessageID = evt.ActionMeta.QuestionMessageID
	}
	return ActionTokenClaims{
		UserID:           evt.UserID,
		EventKey:         evt.EventKey,
		AvailableActions: evt.ActionMeta.AvailableActions,
		Target:           target,
	}
}

func categoryFor(eventKey string) string {
	switch eventKey {
	case EventApprovalRequested:
		return "APPROVAL_REQUEST"
	case EventAgentQuestion:
		return "AGENT_QUESTION"
	default:
		return ""
	}
}
