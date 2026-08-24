package agentapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

// Cross-process session history sync.
//
// The REST API process has no agent websocket Manager: agents connect to the
// ws nodes, so wsagentapi.GetGlobal() is always nil there. Any caller that
// needs provider-native history imported from a process without a Manager
// forwards the whole-session sync to the ws node that currently holds the
// agent connection. That node runs the sync locally (its Manager already
// knows how to reach the agent, including cross-node local_action forwarding)
// and publishes the outcome back on a per-request reply channel.

const (
	redisCmdSessionHistorySyncRequest = "_agent_api_session_history_sync"
	sessionHistorySyncReplyPrefix     = "chan:session_history_sync:"
)

// remoteSessionHistorySyncTimeout bounds one forwarded whole-session sync. It
// must cover a full page loop on the ws side (pages * per-action timeout), so
// a slow connector is reported as a timeout instead of hanging the caller.
var remoteSessionHistorySyncTimeout = 5 * time.Minute

var ErrSessionHistorySyncHandlerUnavailable = errors.New("session history sync handler unavailable")

// SessionHistorySyncHandler runs a whole-session native history sync on a
// process that owns a Manager. Wired by the ws server to the orchestrator.
type SessionHistorySyncHandler func(ctx context.Context, ownerID int64, sessionID string) (int, error)

var (
	sessionHistorySyncHandlerMu sync.RWMutex
	sessionHistorySyncHandler   SessionHistorySyncHandler
)

// SetSessionHistorySyncHandler registers the local sync executor used when a
// forwarded session history sync request arrives on this node.
func SetSessionHistorySyncHandler(fn SessionHistorySyncHandler) {
	sessionHistorySyncHandlerMu.Lock()
	sessionHistorySyncHandler = fn
	sessionHistorySyncHandlerMu.Unlock()
}

func getSessionHistorySyncHandler() SessionHistorySyncHandler {
	sessionHistorySyncHandlerMu.RLock()
	defer sessionHistorySyncHandlerMu.RUnlock()
	return sessionHistorySyncHandler
}

type sessionHistorySyncRemoteRequest struct {
	CorrelationID string `json:"correlation_id"`
	ReplyChannel  string `json:"reply_channel"`
	OwnerID       int64  `json:"owner_id,string"`
	SessionID     string `json:"session_id"`
}

type sessionHistorySyncRemoteResponse struct {
	CorrelationID string `json:"correlation_id"`
	Imported      int    `json:"imported"`
	ErrorMsg      string `json:"error_msg,omitempty"`
}

// RemoteSyncBoundSessionHistory forwards a whole-session native history sync
// to the ws node holding one of the session's agents and waits for its result.
// agentIDs are the agents bound to the session; the first one with a live
// route decides the target node. Returns ErrSessionHistorySyncAgentOffline when
// no agent is routable, ErrSessionHistorySyncTimeout when the node never
// answers.
func RemoteSyncBoundSessionHistory(ctx context.Context, ownerID int64, sessionID string, agentIDs []int64) (int, error) {
	sessionID = strings.TrimSpace(sessionID)
	if ownerID <= 0 || sessionID == "" {
		return 0, nil
	}
	if store.RDB == nil {
		return 0, ErrSessionHistorySyncAgentOffline
	}
	if ctx == nil {
		ctx = context.Background()
	}

	targetNode := ""
	for _, agentID := range agentIDs {
		if node := loadAgentRouteForOwner(ctx, agentID, ownerID); node != "" {
			targetNode = node
			break
		}
	}
	if targetNode == "" {
		return 0, ErrSessionHistorySyncAgentOffline
	}

	correlation := fmt.Sprintf("hs-%d-%s-%d", ownerID, sessionID, forwardedCorrelationSeq.Add(1))
	replyChannel := sessionHistorySyncReplyPrefix + correlation

	// Subscribe before publishing so a fast reply cannot be missed.
	sub := store.RDB.Subscribe(ctx, replyChannel)
	defer func() { _ = sub.Close() }()
	if _, err := sub.Receive(ctx); err != nil {
		return 0, fmt.Errorf("subscribe session history sync reply: %w", err)
	}

	req := sessionHistorySyncRemoteRequest{
		CorrelationID: correlation,
		ReplyChannel:  replyChannel,
		OwnerID:       ownerID,
		SessionID:     sessionID,
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return 0, err
	}
	envelope, err := json.Marshal(map[string]any{
		"cmd":     redisCmdSessionHistorySyncRequest,
		"payload": json.RawMessage(payload),
	})
	if err != nil {
		return 0, err
	}
	if err := store.RDB.Publish(ctx, fmt.Sprintf("chan:%s", targetNode), envelope).Err(); err != nil {
		return 0, fmt.Errorf("publish session history sync request: %w", err)
	}
	logger.L.Infof("session history sync forwarded owner=%d session=%s target_node=%s correlation=%s", ownerID, sessionID, targetNode, correlation)

	timer := time.NewTimer(remoteSessionHistorySyncTimeout)
	defer timer.Stop()
	msgCh := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-timer.C:
			return 0, ErrSessionHistorySyncTimeout
		case msg, ok := <-msgCh:
			if !ok {
				return 0, ErrSessionHistorySyncTimeout
			}
			var resp sessionHistorySyncRemoteResponse
			if err := json.Unmarshal([]byte(msg.Payload), &resp); err != nil {
				logger.L.Warnf("decode session history sync reply failed correlation=%s err=%v", correlation, err)
				continue
			}
			if resp.CorrelationID != correlation {
				continue
			}
			if strings.TrimSpace(resp.ErrorMsg) != "" {
				return resp.Imported, errors.New(resp.ErrorMsg)
			}
			return resp.Imported, nil
		}
	}
}

// HandleSessionHistorySyncDispatch executes a forwarded whole-session sync on
// this node and publishes the outcome to the requester's reply channel.
// Returns false when cmd is not a session history sync request.
func HandleSessionHistorySyncDispatch(cmd string, payload json.RawMessage) bool {
	if cmd != redisCmdSessionHistorySyncRequest {
		return false
	}
	var req sessionHistorySyncRemoteRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		logger.L.Warnf("decode forwarded session history sync request failed: %v", err)
		return true
	}
	if strings.TrimSpace(req.ReplyChannel) == "" || strings.TrimSpace(req.CorrelationID) == "" {
		logger.L.Warnf("drop forwarded session history sync request without reply channel session=%s", req.SessionID)
		return true
	}
	run := func() { runForwardedSessionHistorySync(req) }
	// The sync reads and writes the DB; run it in the Manager's background
	// group so it cannot outlive shutdown. Without a Manager the agent cannot be
	// reached from this node either, so answer offline right away.
	if mgr := GetGlobalManager(); mgr != nil {
		mgr.goBackground(run)
	} else {
		publishSessionHistorySyncReply(req, 0, ErrSessionHistorySyncAgentOffline)
	}
	return true
}

func runForwardedSessionHistorySync(req sessionHistorySyncRemoteRequest) {
	handler := getSessionHistorySyncHandler()
	if handler == nil {
		publishSessionHistorySyncReply(req, 0, ErrSessionHistorySyncHandlerUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteSessionHistorySyncTimeout)
	defer cancel()
	imported, err := handler(ctx, req.OwnerID, req.SessionID)
	publishSessionHistorySyncReply(req, imported, err)
}

func publishSessionHistorySyncReply(req sessionHistorySyncRemoteRequest, imported int, err error) {
	if store.RDB == nil {
		return
	}
	resp := sessionHistorySyncRemoteResponse{CorrelationID: req.CorrelationID, Imported: imported}
	if err != nil {
		resp.ErrorMsg = err.Error()
	}
	data, marshalErr := json.Marshal(resp)
	if marshalErr != nil {
		return
	}
	if pubErr := store.RDB.Publish(context.Background(), req.ReplyChannel, data).Err(); pubErr != nil {
		logger.L.Warnf("publish session history sync reply failed correlation=%s err=%v", req.CorrelationID, pubErr)
	}
}
