package agentapi

import (
	"context"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

var (
	chatQueuePollInterval        = 150 * time.Millisecond
	chatQueueListConfirmTimeout  = 2 * time.Second
	chatQueueWriteConfirmTimeout = 3 * time.Second
)

type chatQueueInvokeResult struct {
	SessionID  string                    `json:"session_id"`
	AgentID    int64                     `json:"agent_id"`
	Running    []string                  `json:"running"`
	Queued     []string                  `json:"queued"`
	Items      []queueSnapshotMirrorItem `json:"items"`
	UpdatedAt  int64                     `json:"updated_at"`
	Confirmed  bool                      `json:"confirmed"`
	Dispatched bool                      `json:"dispatched"`
	Stale      bool                      `json:"stale,omitempty"`
}

func dispatchChatQueueAction(agentID, ownerID int64, action string, params map[string]interface{}, hooks agentInvokeHooks) (interface{}, int, string) {
	sessionID, ok := paramString(params, "session_id")
	sessionID = strings.TrimSpace(sessionID)
	if !ok || sessionID == "" {
		return nil, 4001, "session_id required"
	}

	preferredAgentID := int64(0)
	if _, provided := params["agent_id"]; provided {
		preferredAgentID, ok = paramInt64(params, "agent_id")
		if !ok || preferredAgentID <= 0 {
			return nil, 4001, "agent_id invalid"
		}
	}

	var eventID, content string
	var hold bool
	var ttlMS int64
	var orderedEventIDs []string
	switch action {
	case "chat_queue_cancel", "chat_queue_hold", "chat_queue_edit":
		eventID, ok = paramString(params, "event_id")
		eventID = strings.TrimSpace(eventID)
		if !ok || eventID == "" {
			return nil, 4001, "event_id required"
		}
	}
	switch action {
	case "chat_queue_hold":
		hold, ok = paramBool(params, "hold")
		if !ok {
			return nil, 4001, "hold required"
		}
		if _, provided := params["ttl_ms"]; provided {
			ttlMS, ok = paramInt64(params, "ttl_ms")
			if !ok || ttlMS <= 0 {
				return nil, 4001, "ttl_ms invalid"
			}
		}
	case "chat_queue_edit":
		content, ok = paramString(params, "content")
		if !ok || strings.TrimSpace(content) == "" {
			return nil, 4001, "content required"
		}
	case "chat_queue_reorder":
		orderedEventIDs, ok = paramStringSlice(params, "ordered_event_ids")
		if !ok {
			return nil, 4001, "ordered_event_ids required"
		}
		trimmed := make([]string, 0, len(orderedEventIDs))
		for _, id := range orderedEventIDs {
			if id = strings.TrimSpace(id); id != "" {
				trimmed = append(trimmed, id)
			}
		}
		if len(trimmed) == 0 {
			return nil, 4001, "ordered_event_ids required"
		}
		orderedEventIDs = trimmed
	}

	if err := service.EnsureHumanSessionAccessible(context.Background(), ownerID, sessionID); err != nil {
		return classifySessionError(err)
	}

	targetAgentID := ResolveEventLifecycleAgentID(ownerID, sessionID, preferredAgentID)
	if action == "chat_queue_cancel" || action == "chat_queue_hold" || action == "chat_queue_edit" {
		mirror := loadLatestQueueSnapshotMirror(context.Background(), ownerID, sessionID, targetAgentID)
		if code, msg := validateChatQueueQueuedTarget(mirror, eventID); code != 0 {
			return nil, code, msg
		}
	}

	var cmd string
	var payload any
	var confirm func(*queueSnapshotMirror, int64) bool
	timeout := chatQueueWriteConfirmTimeout
	switch action {
	case "chat_queue_list":
		cmd = protocol.CmdQueueSnapshotQuery
		payload = struct {
			SessionID string `json:"session_id"`
			AgentID   int64  `json:"agent_id,string"`
		}{SessionID: sessionID, AgentID: targetAgentID}
		timeout = chatQueueListConfirmTimeout
		confirm = func(mirror *queueSnapshotMirror, requestedAt int64) bool {
			return mirror != nil && mirror.UpdatedAt > requestedAt
		}
	case "chat_queue_cancel":
		cmd = protocol.CmdEventCancel
		payload = protocol.EventCancelPayload{SessionID: sessionID, EventID: eventID, AgentID: targetAgentID}
		confirm = func(mirror *queueSnapshotMirror, _ int64) bool {
			return mirror != nil && !containsQueueEventID(mirror.Queued, eventID)
		}
	case "chat_queue_clear":
		cmd = protocol.CmdQueueClear
		payload = protocol.QueueClearPayload{SessionID: sessionID, AgentID: targetAgentID}
		confirm = func(mirror *queueSnapshotMirror, _ int64) bool {
			return mirror != nil && len(mirror.Queued) == 0
		}
	case "chat_queue_hold":
		cmd = protocol.CmdEventHold
		payload = protocol.EventHoldPayload{
			SessionID: sessionID,
			EventID:   eventID,
			Hold:      hold,
			Reason:    "manual",
			TTLMS:     ttlMS,
			AgentID:   targetAgentID,
		}
		confirm = func(mirror *queueSnapshotMirror, _ int64) bool {
			item := findQueuedQueueSnapshotItem(mirror, eventID)
			return item != nil && item.Held == hold
		}
	case "chat_queue_edit":
		cmd = protocol.CmdQueueEdit
		payload = protocol.QueueEditPayload{SessionID: sessionID, EventID: eventID, Content: content, AgentID: targetAgentID}
		expectedPreview := truncateQueueContentPreview(content)
		confirm = func(mirror *queueSnapshotMirror, _ int64) bool {
			item := findQueuedQueueSnapshotItem(mirror, eventID)
			return item != nil && item.ContentPreview == expectedPreview
		}
	case "chat_queue_reorder":
		cmd = protocol.CmdQueueReorder
		payload = protocol.QueueReorderPayload{SessionID: sessionID, OrderedEventIDs: orderedEventIDs, AgentID: targetAgentID}
		confirm = func(mirror *queueSnapshotMirror, _ int64) bool {
			return queueReorderConfirmed(mirror, orderedEventIDs)
		}
	}

	dispatch := hooks.dispatchEventLifecycleCommand
	if dispatch == nil {
		dispatch = DispatchEventLifecycleCommand
	}
	return dispatchChatQueueAndConfirm(ownerID, sessionID, targetAgentID, cmd, payload, timeout, dispatch, confirm)
}

func validateChatQueueQueuedTarget(mirror *queueSnapshotMirror, eventID string) (int, string) {
	if mirror != nil {
		if containsQueueEventID(mirror.Running, eventID) {
			return 4001, "event is running; use stop instead"
		}
		if containsQueueEventID(mirror.Queued, eventID) {
			return 0, ""
		}
	}
	return 4004, "event not found in queued"
}

func dispatchChatQueueAndConfirm(
	ownerID int64,
	sessionID string,
	agentID int64,
	cmd string,
	payload any,
	timeout time.Duration,
	dispatch func(agentID, ownerID int64, cmd string, payload any) bool,
	confirm func(*queueSnapshotMirror, int64) bool,
) (interface{}, int, string) {
	requestedAt := time.Now().UnixMilli()
	if !dispatch(agentID, ownerID, cmd, payload) {
		if cmd == protocol.CmdQueueSnapshotQuery {
			mirror := loadLatestQueueSnapshotMirror(context.Background(), ownerID, sessionID, agentID)
			return newChatQueueInvokeResult(sessionID, agentID, mirror, false, false, true), 0, ""
		}
		return nil, 5001, "agent channel unavailable"
	}

	deadline := time.Now().Add(timeout)
	var latest *queueSnapshotMirror
	for {
		latest = loadLatestQueueSnapshotMirror(context.Background(), ownerID, sessionID, agentID)
		if confirm(latest, requestedAt) {
			return newChatQueueInvokeResult(sessionID, agentID, latest, true, true, false), 0, ""
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		sleep := chatQueuePollInterval
		if sleep > remaining {
			sleep = remaining
		}
		time.Sleep(sleep)
	}
	return newChatQueueInvokeResult(sessionID, agentID, latest, false, true, false), 0, ""
}

func newChatQueueInvokeResult(sessionID string, agentID int64, mirror *queueSnapshotMirror, confirmed, dispatched, stale bool) chatQueueInvokeResult {
	result := chatQueueInvokeResult{
		SessionID:  sessionID,
		AgentID:    agentID,
		Running:    []string{},
		Queued:     []string{},
		Items:      []queueSnapshotMirrorItem{},
		Confirmed:  confirmed,
		Dispatched: dispatched,
		Stale:      stale,
	}
	if mirror == nil {
		return result
	}
	result.Running = append(result.Running, mirror.Running...)
	result.Queued = append(result.Queued, mirror.Queued...)
	result.Items = append(result.Items, mirror.Items...)
	result.UpdatedAt = mirror.UpdatedAt
	if result.AgentID == 0 {
		result.AgentID = mirror.AgentID
	}
	return result
}

func containsQueueEventID(eventIDs []string, eventID string) bool {
	for _, id := range eventIDs {
		if id == eventID {
			return true
		}
	}
	return false
}

func findQueuedQueueSnapshotItem(mirror *queueSnapshotMirror, eventID string) *queueSnapshotMirrorItem {
	if mirror == nil {
		return nil
	}
	for i := range mirror.Items {
		item := &mirror.Items[i]
		if item.EventID == eventID && item.State == "queued" {
			return item
		}
	}
	return nil
}

func queueReorderConfirmed(mirror *queueSnapshotMirror, orderedEventIDs []string) bool {
	if mirror == nil {
		return false
	}
	existing := make(map[string]struct{}, len(mirror.Queued))
	for _, id := range mirror.Queued {
		existing[id] = struct{}{}
	}
	expected := make([]string, 0, len(orderedEventIDs))
	for _, id := range orderedEventIDs {
		if _, ok := existing[id]; ok {
			expected = append(expected, id)
		}
	}
	if len(expected) != len(mirror.Queued) {
		return false
	}
	for i, id := range expected {
		if mirror.Queued[i] != id {
			return false
		}
	}
	return true
}
